package binarydiff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"io"

	"github.com/gamejolt/joltron/concurrency"
	"github.com/gamejolt/joltron/fs"
	"github.com/gamejolt/joltron/game/data"
	OS "github.com/gamejolt/joltron/os"
	"github.com/kr/binarydist"
)

// Patcher applies binary diff patches to a directory to update bring it to the next directory.
type Patcher struct {
	*concurrency.Resumable
	os OS.OS

	DataDir      string
	PatchDir     string
	DiffDir      string
	sideBySide   bool
	dynamicFiles []string
	oldMetadata  *data.BuildMetadata
	newMetadata  *data.BuildMetadata
	diffMetadata *data.DiffMetadata

	currentMetadata  *data.BuildMetadata
	conflictMetadata *conflictMetadata
	sizeToPatch      int64
	sizePatched      int64

	result   error
	doneCh   chan bool
	finished bool
}

// Finish comment
func (p *Patcher) Finish(result error) error {
	if p.finished {
		return p.result
	}
	p.result = result
	p.finished = true

	// Cancel before cleanup for the same reason in patcher/patcher
	p.Cancel()
	p.cleanup()

	close(p.doneCh)

	return p.result
}

// Done returns a channel that closes when the patching is done and cleaned up
func (p *Patcher) Done() <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		if p.finished {
			close(ch)
		} else {
			<-p.doneCh
			close(ch)
		}
	}()

	return ch
}

// Result comment
func (p *Patcher) Result() error {
	return p.result
}

// NewPatcher comment
func NewPatcher(resumable *concurrency.Resumable, dataDir, patchDir, diffDir string, sideBySide bool, dynamicFiles []string, oldMetadata *data.BuildMetadata, newMetadata *data.BuildMetadata, diffMetadata *data.DiffMetadata, os2 OS.OS) (*Patcher, error) {
	if os2 == nil {
		tmp, err := OS.NewDefaultOS()
		if err != nil {
			return nil, err
		}
		os2 = tmp
	}

	// sideBySide := dataDir == patchDir
	// if sideBySide {
	// 	temp, err := ioutil.TempDir(filepath.Dir(dataDir), "patch-")
	// 	if err != nil {
	// 		return nil, fmt.Errorf("Could not assign a new temporary patch directory for this diff operation: %s", err.Error())
	// 	}
	// 	patchDir = temp
	// }

	p := &Patcher{
		Resumable:    concurrency.NewResumable(resumable),
		os:           os2,
		DataDir:      dataDir,
		PatchDir:     patchDir,
		DiffDir:      diffDir,
		sideBySide:   sideBySide,
		dynamicFiles: dynamicFiles,
		oldMetadata:  oldMetadata,
		newMetadata:  newMetadata,
		diffMetadata: diffMetadata,
		doneCh:       make(chan bool),
	}

	log.Printf("Patching %s to %s using %s\n", p.DataDir, p.PatchDir, p.DiffDir)
	go func() {
		if p.sideBySide {
			tempDir := p.DataDir + ".swap-" + filepath.Base(p.PatchDir)
			finfo, err := p.os.Lstat(tempDir)
			if err == nil && finfo.IsDir() && finfo.Mode()&os.ModeSymlink == 0 {

				// If the patch dir exists, rename it to data dir
				finfo, err = p.os.Lstat(p.PatchDir)
				if err == nil {
					if finfo.IsDir() && finfo.Mode()&os.ModeSymlink == 0 {
						if err = p.os.Rename(p.PatchDir, p.DataDir); err != nil {
							p.Finish(fmt.Errorf("Could not swap in current data dir %s to %s: %s", p.PatchDir, p.DataDir, err.Error()))
							return
						}
					} else {
						p.Finish(fmt.Errorf("Patch dir exists but is not a valid directory %s", p.PatchDir))
						return
					}
				}

				// if err := p.os.RemoveAll(p.DiffDir); err != nil && !os.IsNotExist(err) {
				// 	p.Finish(fmt.Errorf("Could not clean up diff dir %s: %s", p.DiffDir, err.Error()))
				// 	return
				// }

				// // Remove the temp dir
				// if err = p.os.RemoveAll(tempDir); err != nil {
				// 	p.Finish(fmt.Errorf("Could not clean up old game dir"))
				// 	return
				// }

				p.Finish(nil)
				return
			}
		}

		result := concurrency.ChainResumableTasks(p,

			// Fetch current metadata
			func() error {
				tmp, err := p.getCurrentMetadata()
				if err != nil {
					return err
				}
				p.currentMetadata = tmp
				bytes, err := json.Marshal(p.currentMetadata)
				if err == nil {
					log.Printf("current metadata: %s", bytes)
				}

				// Figure out filesizes that need to be patched.
				var sizeToPatch int64
				if p.diffMetadata.Identical != nil && len(p.diffMetadata.Identical) != 0 {
					for oldFile, newFiles := range p.diffMetadata.Identical {
						if currentFile, ok := p.currentMetadata.Files[oldFile]; ok {
							sizeToPatch += currentFile.Size * int64(len(newFiles))
						}
					}
				}

				if p.diffMetadata.Similar != nil && len(p.diffMetadata.Similar) != 0 {
					for _, similarFiles := range p.diffMetadata.Similar {
						for _, similarFile := range similarFiles {
							sizeToPatch += similarFile.DiffSize
						}
					}
				}
				p.sizeToPatch = sizeToPatch
				return nil
			},

			// Create directories in the temp patch dir
			func() error {
				log.Println("Ensuring directories")
				return p.ensureDirs()
			},

			// Patch identical files
			func() error {
				log.Println("Patching identical files")
				return p.patchIdenticalFiles()
			},

			// Patch similar files
			func() error {
				log.Println("Patching similar files")
				return p.patchSimilarFiles()
			},

			// Patch created files
			func() error {
				log.Println("Creating new files")
				return p.patchCreatedFiles()
			},

			// Attempt to copy over dynamic files
			func() error {
				log.Println("Patching dynamic files")
				return p.patchDynamicFiles()
			},

			// Create the symlinks
			func() error {
				log.Println("Creating symlinks")
				return p.patchSymlinks()
			},

			// Swap the patch directory with the data directory if needed
			func() error {
				log.Println("Swapping directories")
				return p.swapDirs()
			})

		p.Finish(<-result)
	}()

	return p, nil
}

// getCurrentMetadata gets the metadata in the current local build dir.
// Note: The file paths will be relative to the game data dir same as returned by server for old/new build metadata.
// symlink targets are left as is
func (p *Patcher) getCurrentMetadata() (*data.BuildMetadata, error) {
	m := &data.BuildMetadata{
		Dirs:     make([]string, 0),
		Files:    make(map[string]data.FileMetadata, 0),
		Symlinks: make(map[string]string, 0),
	}

	err := filepath.Walk(p.DataDir, func(path string, finfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if finfo == nil {
			return nil
		}

		// Force paths to be relative to the game data dir.
		// This allows us to compare it later with entries in our manifest more easily.
		rel, err := filepath.Rel(p.DataDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Handle symlinks
		if finfo.Mode()&os.ModeSymlink != 0 {
			target, err := p.os.Readlink(path)
			if err != nil {
				return err
			}

			targetRel, err := filepath.Rel(filepath.Dir(path), target)
			if err != nil {
				return err
			}
			m.Symlinks[rel] = targetRel
			return nil
		}

		// Handle directories
		if finfo.IsDir() {
			if filepath.Base(path) == ".conflicts" {
				return filepath.SkipDir
			}

			m.Dirs = append(m.Dirs, rel)
			return nil
		}

		// Handle files
		size, err := fs.Filesize(path)
		if err != nil {
			return err
		}

		checksum, err := fs.ResumableMD5File(p, path, p.os)
		if err != nil {
			return err
		}

		m.Files[rel] = data.FileMetadata{
			Size:     size,
			Checksum: checksum,
		}
		return nil
	})

	if err != nil && err != filepath.SkipDir {
		return nil, err
	}

	return m, nil
}

func (p *Patcher) ensureDirs() error {
	// Ensure all directories exist
	for _, dir := range p.newMetadata.Dirs {
		if filepath.Base(dir) == ".conflicts" {
			return errors.New("The developer is trying to be cheeky with us, bundling in a .conflicts directory")
		}

		dir = filepath.Join(p.PatchDir, dir)
		if err := fs.EnsureFolder(dir, p.os); err != nil {
			return fmt.Errorf("Could not ensure directory %s: %s", dir, err.Error())
		}
	}
	return nil
}

// patchIdenticalFiles handles identical files when patching.
// For a side-by-side update, all identical files are created by copying them over from the old directory.
// For an in-place update, the first match of an identical file will be renamed to the new location,
// and any more get copied
func (p *Patcher) patchIdenticalFiles() error {

	if p.diffMetadata.Identical == nil || len(p.diffMetadata.Identical) == 0 {
		return nil
	}

	for oldFile, newFiles := range p.diffMetadata.Identical {
		fileMetadata, ok := p.oldMetadata.Files[oldFile]
		if !ok {
			return fmt.Errorf("Old file metadata for %s does not exist", oldFile)
		}

		// Check if the current file is still valid for the patch.
		// an identical file is valid if it hasn't been modified from the old archive (path, size and checksum are identical to old metadata)
		// If the file is invalid we don't error right away but save the relevant error message
		// because we don't want to error out if this file created all of it's identical ones in the new patch dir.
		currentFileMetadata, ok := p.currentMetadata.Files[oldFile]
		currentFileInvalidMsg := ""
		if !ok {
			currentFileInvalidMsg = fmt.Sprintf("Current file metadata for %s does not exist", oldFile)
		} else if currentFileMetadata.Size != fileMetadata.Size {
			currentFileInvalidMsg = fmt.Sprintf("Filesize of %s expected to be %d but was %d", oldFile, fileMetadata.Size, currentFileMetadata.Size)
		} else if currentFileMetadata.Checksum != fileMetadata.Checksum {
			currentFileInvalidMsg = fmt.Sprintf("Checksum of %s expected to be %s but was %s", oldFile, fileMetadata.Checksum, currentFileMetadata.Checksum)
		}

		for _, newFile := range newFiles {
			newFileFullpath := filepath.Join(p.PatchDir, newFile)
			finfo, err := p.os.Lstat(newFileFullpath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("Could not check if new file exists %s: %s", newFileFullpath, err.Error())
			}

			// If err is nil, it means a file exists. We delete it if it doesn't match the expected identical file.
			fileExists := finfo != nil && err == nil
			isFileValid := fileExists
			if fileExists {
				size := finfo.Size()
				if size != fileMetadata.Size {
					isFileValid = false
				} else {
					checksum, err := fs.ResumableMD5File(p, newFileFullpath, p.os)
					if err != nil {
						return fmt.Errorf("Could not read identical file's md5 %s: %s", newFileFullpath, err.Error())
					} else if checksum != fileMetadata.Checksum {
						isFileValid = false
					}
				}

				if !isFileValid {
					if err = p.os.Remove(newFileFullpath); err != nil {
						return fmt.Errorf("Could not clean possibly corrupt identical file %s: %s", newFileFullpath, err.Error())
					}
				}
			}

			// If we reached here and the file is invalid it means it didn't exist or was bad and has been removed.
			// We are free to create the file again
			if !isFileValid {

				// The current file must be valid here because it is used as the base for this patch operation.
				if currentFileInvalidMsg != "" {
					return errors.New(currentFileInvalidMsg)
				}

				currentFileFullpath := filepath.Join(p.DataDir, oldFile)
				currentFileStat, err := p.os.Lstat(currentFileFullpath)
				if err != nil {
					return fmt.Errorf("Could not get file stat for the current identical file %s: %s", currentFileFullpath, err.Error())
				}

				srcFile, err := p.os.Open(currentFileFullpath)
				if err != nil {
					return fmt.Errorf("Could not open current file for copying %s: %s", currentFileFullpath, err.Error())
				}

				destFile, err := p.os.OpenFile(newFileFullpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, currentFileStat.Mode())
				if err != nil {
					srcFile.Close()
					return fmt.Errorf("Could not create the new identical file %s from %s: %s", newFileFullpath, currentFileFullpath, err.Error())
				}

				size, err := fs.ResumableCopy(p, destFile, srcFile, nil)
				srcFile.Close()
				destFile.Close()
				if err != nil {
					return fmt.Errorf("Could not copy identical file from %s to %s: %s", currentFileFullpath, newFileFullpath, err.Error())
				}
				if size != fileMetadata.Size {
					return fmt.Errorf("Could not copy the entire identical file from %s to %s. Copied %d, but expected %d bytes", currentFileFullpath, newFileFullpath, size, fileMetadata.Size)
				}
			}
			p.sizePatched += currentFileMetadata.Size
		}
	}
	return nil
}

// patchSimilarFiles handles similar files when patching.
func (p *Patcher) patchSimilarFiles() error {

	if p.diffMetadata.Similar == nil || len(p.diffMetadata.Similar) == 0 {
		return nil
	}

	for oldFile, newFiles := range p.diffMetadata.Similar {
		fileMetadata, ok := p.oldMetadata.Files[oldFile]
		if !ok {
			return fmt.Errorf("Old file metadata for %s does not exist", oldFile)
		}

		// Check if the current file is still valid for the patch.
		// a similar file is valid if it hasn't been modified from the old archive (path, size and checksum are identical to old metadata)
		// If the file is invalid we don't error right away but save the relevant error message
		// because we don't want to error out if this file created all of it's similar ones in the new patch dir.
		currentFileMetadata, ok := p.currentMetadata.Files[oldFile]
		currentFileInvalidMsg := ""
		if !ok {
			currentFileInvalidMsg = fmt.Sprintf("Current file metadata for %s does not exist", oldFile)
		} else if currentFileMetadata.Size != fileMetadata.Size {
			currentFileInvalidMsg = fmt.Sprintf("Filesize of %s expected to be %d but was %d", oldFile, fileMetadata.Size, currentFileMetadata.Size)
		} else if currentFileMetadata.Checksum != fileMetadata.Checksum {
			currentFileInvalidMsg = fmt.Sprintf("Checksum of %s expected to be %s but was %s", oldFile, fileMetadata.Checksum, currentFileMetadata.Checksum)
		}

		for _, newFileMetadata := range newFiles {
			newFile := newFileMetadata.New
			newFinalFileMetadata, ok := p.newMetadata.Files[newFile]
			if !ok {
				return fmt.Errorf("New file metadata for %s does not exist", newFile)
			}

			newFileFullpath := filepath.Join(p.PatchDir, newFile)
			finfo, err := p.os.Lstat(newFileFullpath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("Could not check if new file exists %s: %s", newFileFullpath, err.Error())
			}

			// If err is nil, it means a file exists. We delete it if it doesn't match the expected similar file.
			fileExists := finfo != nil && err == nil
			isFileValid := fileExists
			if fileExists {
				size := finfo.Size()
				if size != fileMetadata.Size {
					isFileValid = false
				} else {
					checksum, err := fs.ResumableMD5File(p, newFileFullpath, p.os)
					if err != nil {
						return fmt.Errorf("Could not read similar file's md5 %s: %s", newFileFullpath, err.Error())
					} else if checksum != fileMetadata.Checksum {
						isFileValid = false
					}
				}

				if !isFileValid {
					if err = p.os.Remove(newFileFullpath); err != nil {
						return fmt.Errorf("Could not clean possibly corrupt similar file %s: %s", newFileFullpath, err.Error())
					}
				}
			}

			// If we reached here and the file is invalid it means it didn't exist or was bad and has been removed.
			// We are free to create the file again
			if !isFileValid {

				// The current file must be valid here because it is used as the base for this patch operation.
				if currentFileInvalidMsg != "" {
					return errors.New(currentFileInvalidMsg)
				}

				currentFileFullpath := filepath.Join(p.DataDir, oldFile)

				if err := p.patchSimilarFile(currentFileFullpath, newFileFullpath, newFileMetadata); err != nil {
					return err
				}
				p.sizePatched += newFileMetadata.DiffSize

				size, err := fs.Filesize(newFileFullpath)
				if err != nil {
					return fmt.Errorf("Could not check filesize of patched file %s from %s: %s", newFileFullpath, currentFileFullpath, err.Error())
				}
				if size != newFinalFileMetadata.Size {
					return fmt.Errorf("Filesize for patched file %s from %s expected to be %d but was %d", newFileFullpath, currentFileFullpath, newFinalFileMetadata.Size, size)
				}

				checksum, err := fs.ResumableMD5File(p, newFileFullpath, p.os)
				if err != nil {
					return fmt.Errorf("Could not read similar file's md5 %s from %s: %s", newFileFullpath, currentFileFullpath, err.Error())
				}
				if checksum != newFinalFileMetadata.Checksum {
					return fmt.Errorf("Checksum for patched file %s from %s expected to be %s but was %s", newFileFullpath, currentFileFullpath, newFinalFileMetadata.Checksum, checksum)
				}
			}
		}
	}
	return nil
}

// patchCreatedFiles handles new created files when patching.
func (p *Patcher) patchCreatedFiles() error {

	if p.diffMetadata.Created == nil || len(p.diffMetadata.Created) == 0 {
		return nil
	}

	for _, newFile := range p.diffMetadata.Created {
		fileMetadata, ok := p.newMetadata.Files[newFile]
		if !ok {
			return fmt.Errorf("New file metadata for %s does not exist", newFile)
		}

		newFileFullpath := filepath.Join(p.PatchDir, newFile)
		createdFileFullpath := filepath.Join(p.DiffDir, newFile)
		if err := p.safeCopy(createdFileFullpath, newFileFullpath, fileMetadata.Size, fileMetadata.Checksum); err != nil {
			return fmt.Errorf("Could not copy created file from %s to %s: %s", createdFileFullpath, newFileFullpath, err.Error())
		}
	}
	return nil
}

func (p *Patcher) patchSimilarFile(currentFileFullpath, newFileFullpath string, similarFileMetadata data.SimilarFile) error {
	currentFileStat, err := p.os.Lstat(currentFileFullpath)
	if err != nil {
		return fmt.Errorf("Could not get file stat for the current similar file %s: %s", currentFileFullpath, err.Error())
	}
	currentFileMode := currentFileStat.Mode()

	// Recreate the temporary split and join directories always because we can't safely use thier contents from last time.
	// We have no checksums of what each individual split/patch should look like.
	destDir := filepath.Join(filepath.Dir(p.DataDir), "temp-split")
	if err = p.recreateDir(destDir); err != nil {
		return err
	}
	defer p.os.RemoveAll(destDir)

	joinDir := filepath.Join(filepath.Dir(p.DataDir), "temp-join")
	if err = p.recreateDir(joinDir); err != nil {
		return err
	}
	defer p.os.RemoveAll(joinDir)

	maxChunks := 0
	if similarFileMetadata.Patches != nil {
		maxChunks = len(similarFileMetadata.Patches)
	}
	currentParts, err := p.splitFile(currentFileFullpath, destDir, similarFileMetadata.ChunkSize, maxChunks)
	if err != nil {
		return err
	}

	if len(currentParts) != len(similarFileMetadata.Patches) {
		return fmt.Errorf("Number of split parts %d doesn't match the expected number of patch parts %d", len(currentParts), len(similarFileMetadata.Patches))
	}

	filesToJoin := []string{}

	for i, partPath := range currentParts {
		srcFile, err := p.os.Open(partPath)
		if err != nil {
			return fmt.Errorf("Could not open current part file %d for %s: %s", i, partPath, err.Error())
		}

		patchPartMetadata := similarFileMetadata.Patches[i]
		patchPartFullFilepath := filepath.Join(p.DiffDir, patchPartMetadata.File)
		patchFile, err := p.os.Open(patchPartFullFilepath)
		if err != nil {
			srcFile.Close()
			return fmt.Errorf("Could not open patch part file %d for %s: %s", i, patchPartFullFilepath, err.Error())
		}

		newPartPath := filepath.Join(joinDir, strconv.Itoa(i))
		destFile, err := p.os.OpenFile(newPartPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			srcFile.Close()
			patchFile.Close()
			return fmt.Errorf("Could not create the new patch part file %d for %s: %s", i, newPartPath, err.Error())
		}

		err = binarydist.Patch(srcFile, destFile, patchFile)
		srcFile.Close()
		patchFile.Close()
		destFile.Close()

		if err != nil {
			return fmt.Errorf("Could not patch part %d for %s to %s using %s: %s", i, partPath, newPartPath, patchPartFullFilepath, err.Error())
		}

		filesToJoin = append(filesToJoin, newPartPath)
	}

	if similarFileMetadata.Tails != nil {
		for _, tailPartMetadata := range similarFileMetadata.Tails {
			filesToJoin = append(filesToJoin, filepath.Join(p.DiffDir, tailPartMetadata.File))
		}
	}

	resultFile, err := p.os.OpenFile(newFileFullpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, currentFileMode)
	if err != nil {
		return fmt.Errorf("Could not create the result patched file %s from %s: %s", newFileFullpath, currentFileFullpath, err.Error())
	}

	for _, fileToJoinPath := range filesToJoin {
		fileToJoin, err := p.os.Open(fileToJoinPath)
		if err != nil {
			return fmt.Errorf("Could not join file %s to %s: %s", fileToJoinPath, newFileFullpath, err.Error())
		}

		_, err = fs.ResumableCopy(p, resultFile, fileToJoin, nil)
		fileToJoin.Close()

		if err != nil {
			return fmt.Errorf("Could not join file %s to %s: %s", fileToJoinPath, newFileFullpath, err.Error())
		}
	}

	return nil
}

func (p *Patcher) patchDynamicFiles() error {

	// If the data directory contains a .conflicts folder we need to first copy it over to the patch directory.
	// We don't want to modify the .conflicts in the existing directory because we want to keep it clean.
	conflictsDir := filepath.Join(p.DataDir, ".conflicts")
	finfo, err := p.os.Lstat(conflictsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Could not check if conflicts dir exists %s: %s", conflictsDir, err.Error())
	}
	if err == nil {
		// If a file exists, we need to make sure its a valid directory. Otherwise the developer most likely is trying to be a smartass.
		if !finfo.IsDir() || finfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Conflicts dir exists but is not a valid directory: %s", conflictsDir)
		}

		newConflictsDir := filepath.Join(p.PatchDir, ".conflicts")
		if err := p.recreateDir(newConflictsDir); err != nil {
			return fmt.Errorf("Could not recreate the conflicts dir: %s", newConflictsDir)
		}

		_, err := fs.ResumableCopyDir(p, newConflictsDir, conflictsDir, false, p.os)
		if err != nil {
			return fmt.Errorf("Could not copy the old conflicts dir from %s to %s: %s", conflictsDir, newConflictsDir, err.Error())
		}
	}

	for _, dynamicFile := range p.dynamicFiles {

		// Dynamic files are given as ./ prefixed, / separated strings relative to the data directory.
		// We remove the prefix and to it to the local system file separator so we can work with it more easily.
		// e.g. on windows: ./saves/fDynamic => saves\fDynamic
		dynamicFile = filepath.FromSlash(strings.TrimPrefix(dynamicFile, "./"))
		log.Println("Checking dynamic file", dynamicFile)

		fileMetadata, ok := p.currentMetadata.Files[dynamicFile]
		if !ok {
			return fmt.Errorf("Dynamic file metadata for %s does not exist", dynamicFile)
		}

		// If it was overriden by a new file, directory or symlink in the new archive we need to move this dynamic file aside.
		bytes, _ := json.Marshal(p.newMetadata)
		log.Println(string(bytes))

		dynamicFileParts := strings.Split(dynamicFile, "/")
		dynamicFileTest := ""
		isDynamicFileValid := true
		for i := 0; i < len(dynamicFileParts); i++ {
			if dynamicFileTest == "" {
				dynamicFileTest = dynamicFileParts[0]
			} else {
				dynamicFileTest += "/" + dynamicFileParts[i]
			}

			_, newFileExists := p.newMetadata.Files[dynamicFileTest]
			_, newSymlinkExists := p.newMetadata.Symlinks[dynamicFileTest]
			if newFileExists || newSymlinkExists {
				isDynamicFileValid = false
				break
			}
		}

		if !isDynamicFileValid || inArray(p.newMetadata.Dirs, dynamicFile) {
			log.Println("Patching aside")
			if err := p.patchDynamicFileAside(dynamicFile, fileMetadata); err != nil {
				return err
			}
		} else {
			log.Println("Patching")
			if err := p.patchDynamicFile(dynamicFile, fileMetadata); err != nil {
				return err
			}
		}
	}

	if err := p.writeConflictMetadata(); err != nil {
		return err
	}
	return nil
}

func (p *Patcher) patchDynamicFile(dynamicFile string, fileMetadata data.FileMetadata) error {
	newDynamicFileFullpath := filepath.Join(p.PatchDir, dynamicFile)
	currentDynamicFileFullpath := filepath.Join(p.DataDir, dynamicFile)

	if err := fs.EnsureFolder(filepath.Dir(newDynamicFileFullpath), p.os); err != nil {
		return fmt.Errorf("Could not create directories for dynamic file %s: %s", newDynamicFileFullpath, err.Error())
	}
	return p.safeCopy(currentDynamicFileFullpath, newDynamicFileFullpath, fileMetadata.Size, fileMetadata.Checksum)
}

func (p *Patcher) patchDynamicFileAside(dynamicFile string, fileMetadata data.FileMetadata) error {
	if err := p.ensureConflictMetadata(); err != nil {
		return err
	}

	conflictsDir := filepath.Join(p.PatchDir, ".conflicts")
	currentDynamicFileFullpath := filepath.Join(p.DataDir, dynamicFile)

	tempFile, err := p.os.IOUtilTempFile(conflictsDir, "")
	if err != nil {
		return fmt.Errorf("Could not create temp file in conflicts dir %s for %s: %s", conflictsDir, dynamicFile, err.Error())
	}
	tempFilename := tempFile.Name()
	tempFile.Close()

	if err := p.safeCopy(currentDynamicFileFullpath, tempFilename, fileMetadata.Size, fileMetadata.Checksum); err != nil {
		return fmt.Errorf("Could not copy conflicting dynamic file %s to %s: %s", currentDynamicFileFullpath, tempFilename, err.Error())
	}

	p.conflictMetadata.entries = append(p.conflictMetadata.entries, conflictMetadataEntry{
		file: dynamicFile,
		temp: filepath.Base(tempFilename),
	})
	return nil
}

type conflictMetadata struct {
	entries []conflictMetadataEntry
}

type conflictMetadataEntry struct {
	file  string
	temp  string
	ready string
}

func (p *Patcher) ensureConflictMetadata() error {
	if p.conflictMetadata != nil {
		return nil
	}

	conflictsDir := filepath.Join(p.PatchDir, ".conflicts")
	if err := fs.EnsureFolder(conflictsDir, p.os); err != nil {
		return fmt.Errorf("Could not create file conflicts dir at %s: %s", conflictsDir, err.Error())
	}

	// If failed and the file exists we error out.
	conflictsFilepath := filepath.Join(conflictsDir, ".metadata")
	conflictsFile, err := p.os.OpenFile(conflictsFilepath, os.O_CREATE|os.O_RDONLY, 0644)
	if err != nil {
		return fmt.Errorf("Could not open or create the conflicts metadata file at %s: %s", conflictsFilepath, err.Error())
	}
	conflictsFile.Close()

	bytes, err := p.os.IOUtilReadFile(conflictsFilepath)
	if err != nil {
		return fmt.Errorf("Could not read metadata file at %s: %s", conflictsFilepath, err.Error())
	}

	var file, temp, ready string
	entries := []conflictMetadataEntry{}

	for _, line := range strings.Split(string(bytes), "\n") {
		if strings.TrimSpace(line) == "" {
			if file != "" && temp != "" {
				entries = append(entries, conflictMetadataEntry{
					file:  file,
					temp:  temp,
					ready: ready,
				})
			}

			file, temp, ready = "", "", "true"
			continue
		}

		split := strings.SplitN(line, "|", 2)
		log.Println(split)
		if split == nil || len(split) != 2 {
			return fmt.Errorf("Invalid line in conflicts metadata file (1): \"%s\". File may be outdated or corrupt", line)
		}

		switch split[0] {
		case "file":
			file = split[1]
		case "temp":
			temp = split[1]
		case "ready":
			ready = split[1]
		default:
			return fmt.Errorf("Invalid line in conflicts metadata file (2): \"%s\". File may be outdated or corrupt", line)
		}

	}

	p.conflictMetadata = &conflictMetadata{
		entries: entries,
	}
	return nil
}

func (p *Patcher) writeConflictMetadata() error {
	if err := p.ensureConflictMetadata(); err != nil {
		return err
	}

	str := ""
	for _, entry := range p.conflictMetadata.entries {
		str += fmt.Sprintf("file|%s\ntemp|%s\n\n", entry.file, entry.temp)
	}

	if str != "" {
		conflictsFilepath := filepath.Join(p.PatchDir, ".conflicts", ".metadata")
		if err := p.os.IOUtilWriteFile(conflictsFilepath, []byte(str), 0644); err != nil {
			return fmt.Errorf("Could not write conflict metadata file in %s: %s", conflictsFilepath, err.Error())
		}
	}
	return nil
}

func (p *Patcher) patchSymlinks() error {
	for link, target := range p.newMetadata.Symlinks {
		link = filepath.Join(p.PatchDir, link)
		actualTarget := filepath.Join(link, target)
		linkTarget, err := p.os.Readlink(link)
		if (err != nil && !os.IsNotExist(err)) || (err == nil && linkTarget != actualTarget) {
			if err := p.os.RemoveAll(link); err != nil {
				return fmt.Errorf("Could not clear file %s to recreate as a symlink to %s: %s", link, target, err.Error())
			}
		}

		if err := p.os.Symlink(target, link); err != nil {
			return fmt.Errorf("Could not create symlink from %s to %s: %s", link, target, err.Error())
		}
	}
	return nil
}

func (p *Patcher) swapDirs() error {
	if p.sideBySide {
		if err := p.os.RemoveAll(p.DiffDir); err != nil {
			return fmt.Errorf("Could not clean up diff dir %s: %s", p.DiffDir, err.Error())
		}
		return nil
	}

	tempDir := p.DataDir + ".swap-" + filepath.Base(p.PatchDir)
	if err := p.os.RemoveAll(tempDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Could not ensure temp dir %s does not exist: %s", tempDir, err.Error())
	}

	if err := p.os.Rename(p.DataDir, tempDir); err != nil {
		return fmt.Errorf("Could not swap out current data dir %s to %s: %s", p.DataDir, tempDir, err.Error())
	}

	if err := p.os.Rename(p.PatchDir, p.DataDir); err != nil {
		return fmt.Errorf("Could not swap in current data dir %s to %s: %s", p.PatchDir, p.DataDir, err.Error())
	}

	if err := p.os.RemoveAll(p.DiffDir); err != nil {
		return fmt.Errorf("Could not clean up diff dir %s: %s", p.DiffDir, err.Error())
	}

	if err := p.os.RemoveAll(tempDir); err != nil {
		return fmt.Errorf("Could not clean up old game dir")
	}

	return nil
}

// cleanup removes all temporary files created by this binary diff.
func (p *Patcher) cleanup() error {
	if err := p.os.RemoveAll(p.DiffDir); err != nil {
		return fmt.Errorf("Could not clean up diff dir %s: %s", p.DiffDir, err.Error())
	}

	tempDir := p.DataDir + ".swap-" + filepath.Base(p.PatchDir)
	if err := p.os.RemoveAll(tempDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Could not ensure temp dir %s does not exist: %s", tempDir, err.Error())
	}

	// We need clean up the patch directory only if it is a temporary directory in an in place update or a canceled side by side update.
	if !p.sideBySide || p.result == context.Canceled {
		if err := p.os.RemoveAll(p.PatchDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("Could not clean up patch dir %s: %s", p.PatchDir, err.Error())
		}
	}
	return nil
}

func (p *Patcher) safeCopy(from, to string, expectedSize int64, expectedChecksum string) error {
	toStat, err := p.os.Lstat(to)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Could not check if target file exists %s: %s", to, err.Error())
	}

	// If err is nil, it means a file exists. We delete it if it doesn't match the expected identical file.
	fileExists := toStat != nil && err == nil
	isFileValid := fileExists
	if fileExists {
		size := toStat.Size()
		if size != expectedSize {
			isFileValid = false
		} else {
			checksum, err := fs.ResumableMD5File(p, to, p.os)
			if err != nil {
				return fmt.Errorf("Could not read target file's md5 %s: %s", to, err.Error())
			} else if checksum != expectedChecksum {
				isFileValid = false
			}
		}

		if !isFileValid {
			if err = p.os.Remove(to); err != nil {
				return fmt.Errorf("Could not clean possibly corrupt target file %s: %s", to, err.Error())
			}
		}
	}

	// If we reached here and the file is invalid it means it didn't exist or was bad and has been removed.
	// We are free to create the file again
	if !isFileValid {

		fromStat, err := p.os.Lstat(from)
		if err != nil {
			return fmt.Errorf("Could not get file stat for the source file %s: %s", from, err.Error())
		}

		srcFile, err := p.os.Open(from)
		if err != nil {
			return fmt.Errorf("Could not open source file for copying %s: %s", from, err.Error())
		}

		destFile, err := p.os.OpenFile(to, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fromStat.Mode())
		if err != nil {
			srcFile.Close()
			return fmt.Errorf("Could not create the target file %s: %s", to, err.Error())
		}

		size, err := fs.ResumableCopy(p, destFile, srcFile, nil)
		srcFile.Close()
		destFile.Close()
		if err != nil {
			return fmt.Errorf("Could not copy file from %s to %s: %s", from, to, err.Error())
		}
		if size != expectedSize {
			return fmt.Errorf("Could not copy the entire file from %s to %s. Copied %d, but expected %d bytes", from, to, size, expectedSize)
		}
	}

	return nil
}

func (p *Patcher) recreateDir(dir string) error {
	stat, err := p.os.Lstat(dir)
	if err == nil && stat.IsDir() {
		p.os.RemoveAll(dir)
	}
	if err = fs.EnsureFolder(dir, p.os); err != nil {
		return fmt.Errorf("Could not recreate directory %s: %s", dir, err.Error())
	}
	return nil
}

func (p *Patcher) splitFile(path string, dir string, chunkSize int64, maxChunks int) ([]string, error) {
	finfo, err := p.os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("Could not query the filesize for a file to split %s: %s", path, err.Error())
	}
	size := finfo.Size()

	srcFile, err := p.os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Could not open file for split %s: %s", path, err.Error())
	}
	defer srcFile.Close()

	parts := []string{}

	var copied int64
	for nextChunk := 0; copied < size && nextChunk < maxChunks; nextChunk++ {

		newPath := filepath.Join(dir, strconv.Itoa(nextChunk))
		parts = append(parts, newPath)
		destFile, err := p.os.OpenFile(newPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("Could not create part %d of file %s to %s: %s", nextChunk, path, newPath, err.Error())
		}

		reader := io.LimitReader(srcFile, chunkSize)
		written, err := fs.ResumableCopy(p, destFile, reader, nil)
		if written == 0 || (err != nil && err != io.EOF) {
			destFile.Close()
			return nil, fmt.Errorf("Could not copy part %d of file %s to %s entirely. Copied %d bytes: %s", nextChunk, path, newPath, written, err.Error())
		}
		copied += written
	}
	return parts, nil
}

func inArray(array []string, value string) bool {
	for _, v := range array {
		if v == value {
			return true
		}
	}
	return false
}
