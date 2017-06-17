package patcher

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	BinaryDiff "github.com/gamejolt/joltron/binarydiff"
	"github.com/gamejolt/joltron/broadcast"
	"github.com/gamejolt/joltron/concurrency"
	"github.com/gamejolt/joltron/download"
	"github.com/gamejolt/joltron/extract"
	"github.com/gamejolt/joltron/fs"
	"github.com/gamejolt/joltron/game"
	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/launcher"
	"github.com/gamejolt/joltron/network/jsonnet"
	"github.com/gamejolt/joltron/network/messages/outgoing"
	OS "github.com/gamejolt/joltron/os"
	"github.com/gamejolt/joltron/stream"
	"github.com/kardianos/osext"
)

// Patch comment
type Patch struct {
	*concurrency.Resumable
	os OS.OS

	UpdateMetadata *data.UpdateMetadata
	Dir            string
	Manual         bool
	dataDir        string
	newDataDir     string
	diffDir        string
	tempPatchDir   string
	installing     bool

	mu              sync.Mutex
	state           State
	broadcaster     *broadcast.Broadcaster
	jsonNet         *jsonnet.Listener
	manifest        *data.Manifest
	remoteSize      int64
	downloader      *download.Download
	extractor       *extract.Extraction
	wasFirstInstall bool

	result    error
	doneCh    chan bool
	finishing bool
	finished  int
}

// State determines what the state of the patch operation is
type State int

const (
	// StateStart is the state for when an operation is starting up, before it does any changes to local disk
	StateStart State = iota

	// StatePreparing is when the patch started working. It's the earliest step in the goroutine
	StatePreparing

	// StateDownload is the state for downloading
	StateDownload

	// StatePrepareExtract is the state for preparing an Extraction
	StatePrepareExtract

	// StateUpdateReady is the state for when the updater waits for a signal from the child to continue
	StateUpdateReady

	// StateExtract is the state for extracting
	StateExtract

	// StateCleanup is the state for cleaning up an interrupted operation
	StateCleanup

	// StateUninstall is the state for uninstalling
	StateUninstall

	// StateFinished is the state for a finished operation
	StateFinished
)

// StateChangeMsg is the message sent to subscribers when the patch state changes
type StateChangeMsg struct {
	State State
}

func (p *Patch) changeState(state State) {
	if p.state == state {
		return
	}

	//log.Printf("State changing to %d", state)
	p.state = state
	p.broadcaster.Broadcast(StateChangeMsg{State: state})

	if p.jsonNet != nil {
		p.jsonNet.Broadcast(&outgoing.OutMsgUpdate{
			Message: "patcherState",
			Payload: state,
		})

		if state == StateUpdateReady {
			p.jsonNet.Broadcast(&outgoing.OutMsgUpdate{
				Message: "updateReady",
			})
		}
	}
}

// Subscribe returns a channel used to receive messages from the patcher
func (p *Patch) Subscribe() (broadcast.Subscriber, error) {
	return p.broadcaster.Join()
}

// Unsubscribe unsubscribes from the patcher
func (p *Patch) Unsubscribe(subscriber broadcast.Subscriber) error {
	return p.broadcaster.Leave(subscriber)
}

// Finish comment
func (p *Patch) Finish(result error) error {
	if p.finished != 0 {
		return p.result
	}
	log.Println("Done")
	p.changeState(StateFinished)
	p.result = result
	p.finished = 1

	if p.broadcaster != nil {
		p.broadcaster.Close()
	}

	// First cancel and then cleanup because cleanup has to wait for the download/extraction tasks to finish first,
	// otherwise their resources may be in use.
	p.Cancel()
	p.cleanup()

	close(p.doneCh)

	p.finished = 2
	return p.result
}

// Done returns a channel that closes when the patcher is done and cleaned up
func (p *Patch) Done() <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		if p.finished == 2 {
			close(ch)
		} else {
			<-p.doneCh
			close(ch)
		}
	}()

	return ch
}

// cleanup comment
func (p *Patch) cleanup() {
	if !p.installing {
		return
	}

	if p.result == context.Canceled || p.result == nil {
		if p.downloader != nil {
			<-p.downloader.Done()
			p.downloader.Cleanup()
		}
	}

	if p.result == context.Canceled {
		log.Printf("Cleaning up. First install: %t\n", p.wasFirstInstall)

		if p.wasFirstInstall {
			if p.extractor != nil {
				<-p.extractor.Done()
				log.Println("Removing all " + p.extractor.Dir)
				if err := p.os.RemoveAll(p.extractor.Dir); err != nil {
					log.Println(err.Error())
				} else {
					log.Println("Remove all REPORTED to work.")
				}
			}
			log.Println("Removing " + filepath.Join(p.Dir, ".manifest"))
			p.os.Remove(filepath.Join(p.Dir, ".manifest"))
		}
		log.Println("Cleanup finished")
	}

	if p.result == nil {
		// Finally, we remove all directories starting with "data" in the patch directory that aren't our current one.
		// We do this after the manifest is updated, in the cleanup, because if we do it before and fail with a partial directory we might not be able to update from a patch correctly.
		// The lower risk is to have leftovers until the next patch operation, rather than doing the next update wrong.

		// First we list the immediate children of the patch dir, we don't want to list recursively
		files, err := p.os.IOUtilReadDir(p.Dir)
		if err == nil {

			// Filter out all entries who aren't data directories.
			dirs := make([]string, 0, 5)
			for _, finfo := range files {
				if finfo == nil || !finfo.IsDir() {
					continue
				}

				if finfo.Name() != p.manifest.Info.Dir && strings.HasPrefix(finfo.Name(), "data") {
					dirs = append(dirs, filepath.Join(p.Dir, finfo.Name()))
				}
			}

			for _, dir := range dirs {
				log.Printf("Removing %s\n", dir)
				if err := p.os.RemoveAll(dir); err != nil {
					log.Println("Failed to remove ", dir)
				}
			}
		}
	}
}

// Result comment
func (p *Patch) Result() error {
	return p.result
}

// State gets the current patcher state
func (p *Patch) State() State {
	return p.state
}

func newPatcher(resumable *concurrency.Resumable, installing bool, dir string, manual bool, jsonNet *jsonnet.Listener, updateMetadata *data.UpdateMetadata, os2 OS.OS) (*Patch, error) {
	if os2 == nil {
		temp, err := OS.NewFileScope(dir, false)
		if err != nil {
			return nil, err
		}
		os2 = temp
	}

	p := &Patch{
		Resumable:      concurrency.NewResumable(resumable),
		os:             os2,
		UpdateMetadata: updateMetadata,
		Dir:            dir,
		Manual:         manual,
		installing:     installing,
		jsonNet:        jsonNet,
		doneCh:         make(chan bool),
		state:          StateStart,
		broadcaster:    broadcast.NewBroadcaster(),
	}

	if updateMetadata != nil {
		if updateMetadata.SideBySide != nil && *updateMetadata.SideBySide == true {
			updateMetadata.DataDir = fmt.Sprintf("data-%s", updateMetadata.GameUID)
		} else {
			updateMetadata.DataDir = "data"
		}
	}

	if !filepath.IsAbs(dir) {
		p.Finish(errors.New("Patch dir is invalid: not absolute"))
	}

	if _, err := p.os.Stat(dir); err != nil && !os.IsNotExist(err) {
		p.Finish(errors.New("Patch dir is invalid: " + err.Error()))
	}
	return p, nil
}

// NewInstall comment
func NewInstall(resumable *concurrency.Resumable, dir string, manual bool, jsonNet *jsonnet.Listener, updateMetadata *data.UpdateMetadata, os2 OS.OS) (*Patch, error) {
	p, err := newPatcher(resumable, true, dir, manual, jsonNet, updateMetadata, os2)
	if err != nil {
		return nil, err
	}

	log.Printf("Patching from %s to %s\n", p.UpdateMetadata.URL, p.Dir)
	go func() {
		result := <-concurrency.ChainResumableTasks(p,

			// Signal that the patch has started processing.
			// If the patcher was created from an already paused or canceled resumable this will not run until it is resumed.
			func() error {
				p.changeState(StatePreparing)
				return nil
			},

			// Preparation
			func() error {
				if err := p.prepare(); err != nil {
					return fmt.Errorf("Failed to prepare patch: %s", err.Error())
				}

				// We now have the manifest.
				// Patcher.Manifest.Info = currently installed
				// Patcher.Manifest.PatchInfo = patch in progress of being installed
				// Patcher.UpdateMetadata = this operation's target update

				// If this operation's target update is different than the one in progress we have to clean up traces of the old patch instance.
				// the handlePreviousPatch function will also update the manifest.PatchInfo to be this operation's target update
				if p.manifest.PatchInfo.GameUID != p.UpdateMetadata.GameUID {
					if err := p.handlePreviousPatch(); err != nil {
						return fmt.Errorf("Failed to handle the previous patch state: %s", err.Error())
					}
				}

				// If we're trying to upgrade to a build we're already on we may be able to early-out.
				// Note: for first installation the manifest.Info.GameUID will be set prematurely, and shouldn't be early outed.
				if p.manifest.Info.GameUID == p.UpdateMetadata.GameUID {

					// The data dirs may be different if the useBuildDirs changed.
					// In this case we can simply rename the current dir to the new dir name and continue
					if p.manifest.Info.Dir != p.UpdateMetadata.DataDir {
						if err := p.os.Rename(p.dataDir, p.newDataDir); err != nil {
							return fmt.Errorf("Failed to relocate the data dir: %s", err.Error())
						}

						// Update the .manifest file to have the new dir.
						p.manifest.Info.Dir = p.UpdateMetadata.DataDir

						if p.manifest.IsFirstInstall {

							// For first installations, we need to update the patch info instead of clearing it, because we need to continue updating after relocating the data dirs.
							p.manifest.PatchInfo.Dir = p.UpdateMetadata.DataDir
							p.setManifest(p.manifest)
						} else {

							// We need to clear out the patch info from the manifest so that the patch will be considered finished
							p.manifest.PatchInfo = nil

							// We don't do setManifest here because we want to be able to refer to this operation's dataDirs and other information after we're finished.
							// p.setManifest(p.manifest)
						}

						if err := p.writeManifest(); err != nil {
							// If renaming the dir worked but updating the manifest didn't, try renaming the dir back to avoid corrupting stuff
							// If we fail to rename back there's no clean way to recover and the game would have to be reinstalled.
							// This should never happen - what kind of OS allows renaming a dir  and not renaming it back??
							if err2 := p.os.Rename(p.newDataDir, p.dataDir); err2 != nil {
								return fmt.Errorf("Failed to update manifest after relocating the data dir (%s), then failed relocating back too: %s", err.Error(), err2.Error())
							}
							return fmt.Errorf("Failed to update manifest after relocating the data dir: %s", err.Error())
						}
					}

					// For first installations, we need to continue the update instead of just relocating the data dir.
					if !p.manifest.IsFirstInstall {
						p.Finish(nil)
					}
				}
				return nil
			},

			// Download
			func() error {

				// We write the .manifest file so that the PatchInfo would be saved.
				// Do this before changing the state to download because we need the manifest in fs to reflect the update has actually started by the time the download starts.
				if err := p.writeManifest(); err != nil {
					return fmt.Errorf("Failed to write manifest before downloading the update: %s", err.Error())
				}

				p.changeState(StateDownload)
				if err := p.download(); err != nil {
					// We have to return context cancelation errors as-is so that we can check if the failure is due to context cancelation later on.
					if err == context.Canceled {
						return err
					}
					return fmt.Errorf("Failed to download update: %s", err.Error())
				}
				return nil
			},

			// Prepare for extraction
			// We pause before starting extraction if we're updating in same directory because in this case extraction would impact the currently running child.
			func() error {
				p.changeState(StatePrepareExtract)

				if p.dataDir != p.newDataDir {
					if stat, err := p.os.Stat(p.dataDir); err == nil && stat.IsDir() {
						_, err := fs.ResumableCopyDir(p, p.newDataDir, p.dataDir, false, p.os)
						if err != nil {
							// We have to return context cancelation errors as-is so that we can check if the failure is due to context cancelation later on.
							if err == context.Canceled {
								return err
							}
							return fmt.Errorf("Failed to prepare update for extraction, couldn't copy the old data dir to the new one: %s", err.Error())
						}
					}
				}

				if p.dataDir == p.newDataDir && p.Manual && launcher.InstanceCount() > 0 {
					p.Pause()
					p.changeState(StateUpdateReady)

					// We need to autoresume if we don't have any child instances running because it means we should be clear to update.
					go func() {
						for {
							time.Sleep(1 * time.Second)
							if p.state != StateUpdateReady {
								break
							}

							if launcher.InstanceCount() == 0 {
								p.Resume()
								break
							}
						}
					}()
				}
				return nil
			},

			// Populate patch files.
			// These are files that existed in the old build but not the new build and should be removed.
			func() error {
				// Set the state back to prepare extract because we may have changed it to StateUpdateReady
				p.changeState(StatePrepareExtract)
				dynamicFiles, err := p.findDynamicallyCreatedFiles()
				if err != nil {
					return fmt.Errorf("Failed to list the dynamically created files: %s", err.Error())
				}

				p.manifest.PatchInfo.DynamicFiles = dynamicFiles
				log.Printf("Dynamic files: %v\n", p.manifest.PatchInfo.DynamicFiles)

				if err = p.writeManifest(); err != nil {
					return fmt.Errorf("Failed to update the manifest with the dynamic file list: %s", err.Error())
				}
				return nil
			},

			// Extract
			func() error {
				p.changeState(StateExtract)
				var err error
				if p.manifest.PatchInfo.DiffMetadata == nil {
					err = p.extract()
				} else {
					err = p.binaryDiff()
				}

				if err != nil {
					// We have to return context cancelation errors as-is so that we can check if the failure is due to context cancelation later on.
					if err == context.Canceled {
						return err
					}
					return fmt.Errorf("Failed to extract or apply the update: %s", err.Error())
				}
				return nil
			},

			func() error {
				if p.dataDir != p.newDataDir && p.Manual && launcher.InstanceCount() > 0 {
					p.Pause()
					p.changeState(StateUpdateReady)

					// We need to autoresume if we don't have any child instances running because it means we should be clear to update.
					go func() {
						for {
							time.Sleep(1 * time.Second)
							if p.state != StateUpdateReady {
								break
							}

							if launcher.InstanceCount() == 0 {
								p.Resume()
								break
							}
						}
					}()
				}
				return nil
			},

			// Cleanup old build files.
			func() error {
				p.changeState(StateCleanup)

				newBuildFiles := p.extractor.Result().Result
				log.Printf("Extracted files: %v\n", newBuildFiles)

				// Files we want to remove are files that used to exist in the archive file list that don't exist in the new patch archive file list or the dynamically created files.
				filesToRemove := fs.FilenamesDiff(p.manifest.Info.ArchiveFiles, append(newBuildFiles, p.manifest.PatchInfo.DynamicFiles...))
				for i, fileToRemove := range filesToRemove {
					var fileToRemove = filepath.Join(p.extractor.Dir, fileToRemove)
					if !fs.IsInDir(fileToRemove, p.extractor.Dir) {
						return fmt.Errorf("Attempted to remove a file outside the game dir: %s", filesToRemove[i])
					}
					filesToRemove[i] = fileToRemove
				}
				log.Printf("Files to remove: %v\n", filesToRemove)
				for _, file := range filesToRemove {
					if err := p.os.Remove(file); err != nil && !os.IsNotExist(err) {
						return fmt.Errorf("Failed to clean up an old build file (%s): %s", file, err.Error())
					}
				}

				// Writing manifest file
				// Pull the game info and archive file list from the patchinfo to the game info now that the patch is fully installed
				p.manifest.Info = &data.Info{
					Dir:          p.manifest.PatchInfo.Dir,
					GameUID:      p.manifest.PatchInfo.GameUID,
					ArchiveFiles: newBuildFiles,
				}

				p.manifest.LaunchOptions = p.manifest.PatchInfo.LaunchOptions

				// Remove the patch info to signal the patch has finished and turn off the first installation flag so that next installations won't remove the game on cancellation.
				p.manifest.IsFirstInstall = false
				p.manifest.PatchInfo = nil

				if err := p.writeManifest(); err != nil {
					return fmt.Errorf("Failed to clean up after update because writing the new manifest failed: %s", err.Error())
				}
				return nil
			},
		)
		p.Finish(result)
	}()

	return p, nil
}

// NewUninstall comment
func NewUninstall(resumable *concurrency.Resumable, dir string, jsonNet *jsonnet.Listener, os2 OS.OS) (*Patch, error) {
	p, err := newPatcher(resumable, false, dir, false, jsonNet, nil, os2)
	if err != nil {
		return nil, err
	}

	log.Printf("Uninstalling from %s\n", p.Dir)
	go func() {
		result := <-concurrency.ChainResumableTasks(p,

			// Signal that the patch has started processing.
			// If the patcher was created from an already paused or canceled resumable this will not run until it is resumed.
			func() error {
				p.changeState(StatePreparing)

				// This is skippable, we don't even read the manifest, damnit.
				// if err := p.readManifest(); err != nil {
				// 	return err
				// }

				return nil
			},

			func() error {
				p.changeState(StateUninstall)

				manifestFile := filepath.Join(p.Dir, ".manifest")
				log.Printf("Removing %s\n", manifestFile)
				if err := p.os.Remove(manifestFile); err != nil && !os.IsNotExist(err) {
					return err
				}

				tempDownload := filepath.Join(p.Dir, ".tempDownload")
				log.Printf("Removing %s\n", tempDownload)
				if err := p.os.Remove(tempDownload); err != nil && !os.IsNotExist(err) {
					return err
				}

				// We remove all directories starting with "data" in the patch directory.
				// First we list the immediate children of the patch dir, we don't want to list recursively
				files, err := p.os.IOUtilReadDir(p.Dir)
				if err != nil {
					return err
				}

				// Filter out all entries who aren't data directories.
				dirs := make([]string, 0, 5)
				for _, finfo := range files {
					if finfo == nil || !finfo.IsDir() {
						continue
					}

					if strings.HasPrefix(finfo.Name(), "data") {
						dirs = append(dirs, filepath.Join(p.Dir, finfo.Name()))
					}
				}

				for _, dir := range dirs {
					log.Printf("Removing %s\n", dir)
					if err := p.os.RemoveAll(dir); err != nil {
						return err
					}
				}

				return nil
			},
		)
		p.Finish(result)
	}()

	return p, nil
}

func (p *Patch) prepare() error {
	dir, err := p.getExecutableFolder()
	if err != nil {
		return err
	}
	p.Dir = dir

	if err = p.ensureFolder(); err != nil {
		return err
	}

	if err = p.ensureManifest(); err != nil {
		return err
	}
	return nil
}

func (p *Patch) handlePreviousPatch() error {
	// We need to clean up traces from the abandoned update: temp download file and in case we're using versioned dirs - the abandoned data dir.
	log.Println("Cleaning up previous patch temp download for build: ", p.manifest.PatchInfo.GameUID)

	downloadFile := filepath.Join(p.Dir, ".tempDownload")
	log.Println("Cleaning up local file ", downloadFile)
	if err := p.os.Remove(downloadFile); err != nil && !os.IsNotExist(err) {
		return err
	}

	// If the game dir is dirty now (because the previous patch started extracting into the currently active game dir) we need to reject the operation.
	// The only way to go forwards from this scenario is either reinstall or complete the old build update first.
	if p.manifest.PatchInfo.IsDirty {
		return fmt.Errorf("Game already begun updating in-place to: %q and cannot safely abort the update. Must uninstall or complete update to the previous target build", p.manifest.PatchInfo.GameUID)
	}

	log.Println("Cleaning up previous patch files for build: ", p.manifest.PatchInfo.GameUID)
	if err := p.os.RemoveAll(filepath.Join(p.Dir, p.manifest.PatchInfo.Dir)); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Update the .manifest file to not include the old PatchInfo anymore.
	p.manifest.PatchInfo = nil
	if err := p.writeManifest(); err != nil {
		return err
	}

	// We do need the new PatchInfo in the in-memory manifest - making it match the behaviour of readManifest when the manifest file already exists.
	// This is so that we can continue the operation after discarding the old patch info as if it never existed:
	// The manifest is expected to exist in fs without the patch info, and in-memory should contain the new installation/update target.
	p.manifest.PatchInfo = &data.PatchInfo{
		Dir:     p.UpdateMetadata.DataDir,
		GameUID: p.UpdateMetadata.GameUID,
		IsDirty: false,

		DownloadSize:     p.UpdateMetadata.RemoteSize,
		DownloadChecksum: p.UpdateMetadata.Checksum,
		LaunchOptions: &data.LaunchOptions{
			Executable: p.UpdateMetadata.Executable,
		},

		OldBuildMetadata: p.UpdateMetadata.OldBuildMetadata,
		NewBuildMetadata: p.UpdateMetadata.NewBuildMetadata,
		DiffMetadata:     p.UpdateMetadata.DiffMetadata,
	}
	p.setManifest(p.manifest)
	return nil
}

func (p *Patch) download() error {
	// When a patch is paused the download session is actually terminated compeltely, but we want to resume operation later.
	// So while the patch context is not canceled, we run download in a loop.
	// We break on errors that are not raised by cancelling the download to avoid retrying forever
	for {
		if resume := <-p.Wait(); resume == concurrency.OpCancel {
			return context.Canceled
		}

		// TODO: request new metadata if called using a platform url
		p.downloader = download.NewDownload(p.Resumable, p.UpdateMetadata.URL, filepath.Join(p.Dir, ".tempDownload"), p.UpdateMetadata.Checksum, p.UpdateMetadata.RemoteSize, p.os, func(downloaded, total int64, sample *stream.Sample) {
			if p.jsonNet != nil {
				p.jsonNet.Broadcast(&outgoing.OutMsgProgress{
					Type:    "download",
					Current: downloaded,
					Total:   total,
					Percent: int(float64(downloaded) / float64(total) * 100),
					Sample:  sample,
				})
			}
		})
		<-p.downloader.Done()
		result := p.downloader.Result()

		// If our context is canceled, it may be due to parent canceling or pausing.
		// If its just a pause we want to try again, otherwise we bubble context.Canceled back out
		if result == context.Canceled {
			continue
		}

		// Any other non nil result means an actual failure that should not be retried automatically.
		if result != nil {
			p.downloader.Cleanup()
			return result
		}

		// If the download completed successfuly, just break out of the loop, we're done here.
		break
	}
	return nil
}

func (p *Patch) findDynamicallyCreatedFiles() ([]string, error) {
	if p.manifest.PatchInfo.DynamicFiles != nil && len(p.manifest.PatchInfo.DynamicFiles) > 0 {
		log.Printf("Dynamic files found from previous incomplete patch: %v\n", p.manifest.PatchInfo.DynamicFiles)
		return p.manifest.PatchInfo.DynamicFiles, nil
	}

	log.Printf("Populating patch file lists (current files + dynamic files) from dir: %s\n", p.dataDir)
	currentFiles := make([]string, 0, 64)

	dirinfo, err := p.os.Stat(p.dataDir)
	if p.dataDir != "" && err == nil && dirinfo.IsDir() {
		var errListing error
		filepath.Walk(p.dataDir, func(path string, finfo os.FileInfo, err error) error {
			if err != nil {
				errListing = err
				return err
			}

			if finfo == nil || finfo.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(p.dataDir, path)
			if err != nil {
				return err
			}

			currentFiles = append(currentFiles, "./"+filepath.ToSlash(rel))
			return nil
		})
		if errListing != nil {
			return nil, errListing
		}
	}

	log.Printf("Current files: %v\n", currentFiles)

	var oldBuildFiles []string
	if p.manifest.Info.ArchiveFiles == nil || len(p.manifest.Info.ArchiveFiles) == 0 {
		log.Println("Old builds don't exist, using current files")
		oldBuildFiles = currentFiles
	} else {
		oldBuildFiles = p.manifest.Info.ArchiveFiles
		log.Printf("Old build files: %v\n", oldBuildFiles)
	}

	dynamicFiles := fs.FilenamesDiff(currentFiles, oldBuildFiles)
	return dynamicFiles, nil
}

func (p *Patch) extract() error {
	if p.newDataDir == "" {
		return errors.New("Can't extract without a destination directory specified")
	}

	extractor, err := extract.NewExtraction(p.Resumable, p.downloader.File, p.newDataDir, p.os, func(extracted, total int64, sample *stream.Sample) {
		if p.jsonNet != nil {
			p.jsonNet.Broadcast(&outgoing.OutMsgProgress{
				Type:    "extract",
				Current: extracted,
				Total:   total,
				Percent: int(float64(extracted) / float64(total) * 100),
				Sample:  sample,
			})
		}
	})
	if err != nil {
		return err
	}

	p.extractor = extractor
	<-p.extractor.Done()
	result := p.extractor.Result()
	return result.Err
}

func (p *Patch) binaryDiff() error {
	if p.newDataDir == "" {
		return errors.New("Can't diff without a destination directory specified")
	}

	if p.diffDir == "" || p.tempPatchDir == "" {
		return errors.New("Can't diff without a diff dir or temp patch dir")
	}

	extractor, err := extract.NewExtraction(p.Resumable, p.downloader.File, p.diffDir, p.os, nil)
	if err != nil {
		return err
	}

	p.extractor = extractor
	<-p.extractor.Done()
	if result := p.extractor.Result(); result.Err != nil {
		return result.Err
	}

	patcher, err := BinaryDiff.NewPatcher(p.Resumable, p.newDataDir, p.tempPatchDir, p.diffDir, p.dataDir != p.newDataDir, p.manifest.PatchInfo.DynamicFiles, p.manifest.PatchInfo.OldBuildMetadata, p.manifest.PatchInfo.NewBuildMetadata, p.manifest.PatchInfo.DiffMetadata, p.os)
	if err != nil {
		return err
	}

	<-patcher.Done()
	return patcher.Result()
}

func (p *Patch) getExecutableFolder() (string, error) {
	// Find the working directory for this patch
	if p.Dir != "" {
		return p.Dir, nil
	}

	dir, err := osext.ExecutableFolder()
	if err != nil {
		return "", err
	}
	return dir, nil
}

func (p *Patch) ensureFolder() error {
	return fs.EnsureFolder(p.Dir, p.os)
}

func (p *Patch) readManifest() error {
	// Attempt to read manifest file
	manifest, err := game.GetManifest(p.Dir, p.os)
	if err != nil {
		return err
	}

	p.setManifest(manifest)
	return nil
}

func (p *Patch) writeManifest() error {
	return game.WriteManifest(p.manifest, p.Dir, p.os)
}

// setManifest sets the patch's manifest and related data to it
func (p *Patch) setManifest(manifest *data.Manifest) {
	p.manifest = manifest

	// Set data dirs for easy access to the full paths of the old and new dirs
	p.dataDir = filepath.Join(p.Dir, p.manifest.Info.Dir)
	if p.manifest.PatchInfo != nil {
		p.newDataDir = filepath.Join(p.Dir, p.manifest.PatchInfo.Dir)
		if p.manifest.Info.GameUID != p.manifest.PatchInfo.GameUID {
			p.diffDir = filepath.Join(p.Dir, "diff-"+p.manifest.Info.GameUID+"-"+p.manifest.PatchInfo.GameUID)
			if p.dataDir == p.newDataDir {
				p.tempPatchDir = filepath.Join(p.Dir, "patch-"+p.manifest.Info.GameUID+"-"+p.manifest.PatchInfo.GameUID)
			} else {
				p.tempPatchDir = p.newDataDir
			}
		} else {
			p.diffDir, p.tempPatchDir = "", ""
		}

		// The game dir is dirty if an old patch started extracting files into it.
		// When this happens the dynamic files list will be populated with the half extracted files from the old in-place directory.
		p.manifest.PatchInfo.IsDirty = p.dataDir == p.newDataDir && p.manifest.PatchInfo.DynamicFiles != nil
	} else {
		p.newDataDir, p.diffDir, p.tempPatchDir = "", "", ""
	}

	// Find wether this is a first installation
	p.wasFirstInstall = p.manifest.IsFirstInstall
}

// ensureManifest gets the .manifest in a given directory or creates it if it doesn't exist
func (p *Patch) ensureManifest() error {
	err := p.readManifest()

	launchOptions := &data.LaunchOptions{
		Executable: p.UpdateMetadata.Executable,
	}

	// If we failed to read the manifest it most likely doesn't exist and we need to create it.
	if err != nil {
		// If failed and the file exists we error out.
		manifestFile := filepath.Join(p.Dir, ".manifest")
		if _, err2 := p.os.Stat(manifestFile); !os.IsNotExist(err2) {
			return err
		}

		// The manifest doesn't exist, so we create it for the first time
		log.Println("Creating .manifest file")

		var port uint16
		if p.jsonNet != nil {
			port = p.jsonNet.Port()
		}

		// The manifest isn't expected to have any archive files or patch files at first
		// because it expects a clean installation (these are populated after the first installation)
		manifest := &data.Manifest{
			Version: data.ManifestVersion,
			Info: &data.Info{
				Dir:     p.UpdateMetadata.DataDir,
				GameUID: p.UpdateMetadata.GameUID,
			},
			LaunchOptions:  launchOptions,
			OS:             p.UpdateMetadata.OS,
			Arch:           p.UpdateMetadata.Arch,
			IsFirstInstall: true,
			PatchInfo: &data.PatchInfo{
				Dir:     p.UpdateMetadata.DataDir,
				GameUID: p.UpdateMetadata.GameUID,
				IsDirty: false,

				DownloadSize:     p.UpdateMetadata.RemoteSize,
				DownloadChecksum: p.UpdateMetadata.Checksum,
				LaunchOptions:    launchOptions,

				// No need to set these
				// OS:               p.UpdateMetadata.OS,
				// Arch:             p.UpdateMetadata.Arch,

				OldBuildMetadata: p.UpdateMetadata.OldBuildMetadata,
				NewBuildMetadata: p.UpdateMetadata.NewBuildMetadata,
				DiffMetadata:     p.UpdateMetadata.DiffMetadata,
			},
			RunningInfo: &data.RunningInfo{
				Pid:   os.Getpid(),
				Port:  port,
				Since: time.Now().Unix(),
			},
		}

		if err = p.writeManifest(); err != nil {
			return err
		}
		p.setManifest(manifest)
	} else {

		// If the patch info didn't exist, we're starting a new patch operation.
		// Otherwise, we may be resuming a patch we've started earlier or abandoning a previous patch in the middle.
		// If the patch info describes a different patch than we're aiming for, we may have to reject the patch if it already started extracting
		if p.manifest.PatchInfo == nil {
			log.Println("Setting new patch info")
			p.manifest.PatchInfo = &data.PatchInfo{
				Dir:     p.UpdateMetadata.DataDir,
				GameUID: p.UpdateMetadata.GameUID,
				IsDirty: false,

				DownloadSize:     p.UpdateMetadata.RemoteSize,
				DownloadChecksum: p.UpdateMetadata.Checksum,
				LaunchOptions:    launchOptions,

				OldBuildMetadata: p.UpdateMetadata.OldBuildMetadata,
				NewBuildMetadata: p.UpdateMetadata.NewBuildMetadata,
				DiffMetadata:     p.UpdateMetadata.DiffMetadata,
			}
		}

		// We set manifest here again to trigger checks for new data dir after modifying the PatchInfo
		p.setManifest(p.manifest)
	}
	return nil
}
