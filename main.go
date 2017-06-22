package main

// TODO: use gopkg wherever possible
import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"strconv"

	"os/exec"

	"github.com/droundy/goopt"
	"github.com/gamejolt/joltron/game"
	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/launcher"
	"github.com/gamejolt/joltron/network/jsonnet"
	"github.com/gamejolt/joltron/network/messages/incoming"
	"github.com/gamejolt/joltron/network/messages/outgoing"
	OS "github.com/gamejolt/joltron/os"
	"github.com/gamejolt/joltron/patcher"
	"github.com/gamejolt/joltron/project"
	"github.com/kardianos/osext"
)

func main() {
	// f, err := os.Create("./pprof")
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// pprof.StartCPUProfile(f)
	// defer pprof.StopCPUProfile()

	c := make(chan os.Signal, 5)
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-c
		fmt.Println("Killing all children")
		launcher.KillAll()
		os.Exit(1)
	}()
	defer launcher.KillAll()

	log.Printf("Starting %v\n", os.Args)

	portArg := goopt.IntWithLabel([]string{"--port"}, 0, "PORT", "The port the runner should communicate on. If not specified, one will be chosen randomly")
	dirArg := goopt.StringWithLabel([]string{"--dir"}, "", "DIR", "The directory the runner should work in")

	gameArg := goopt.StringWithLabel([]string{"--game"}, "", "UID", "The game unique identifier[1]")
	platformURLArg := goopt.StringWithLabel([]string{"--platform-url"}, "", "URL", "The url for getting metadata about the next build")
	authTokenArg := goopt.StringWithLabel([]string{"--auth-token"}, "", "TOKEN", "An optional token that grants additional access permissions[2]")
	metadataArg := goopt.StringWithLabel([]string{"--metadata"}, "", "METADATA", "Any extra data the platform might need to fetch the fetch the game build")

	waitForConnectionArg := goopt.IntWithLabel([]string{"--wait-for-connection"}, 0, "TIMEOUT", "Wait until a first connection is made. If none connected until timeout, abort")
	hideLoaderArg := goopt.Flag([]string{"--no-loader"}, []string{}, "In an install operation, do not display the loader UI. Update silently", "")
	launchArg := goopt.Flag([]string{"--launch"}, []string{}, "In an install operation, launch the game immediately after installation", "")
	symbioteArg := goopt.Flag([]string{"--symbiote"}, []string{}, "Makes this operation terminate itself when the first connection that was made to it closes", "")
	mutexArg := goopt.StringWithLabel([]string{"--mutex"}, "", "MUTEX", "A name for a mutex that must be acquired[3]")

	urlArg := goopt.StringWithLabel([]string{"--url"}, "", "URL", "The url for getting the next game build")
	checksumArg := goopt.StringWithLabel([]string{"--checksum"}, "", "MD5", "The md5 checksum of the expected downloaded patch. Optional")
	remoteSizeArg := goopt.StringWithLabel([]string{"--remote-size"}, "", "SIZE", "The expected downloaded patch size in bytes. Optional")
	useBuildDirsArg := goopt.Flag([]string{"--side-by-side"}, []string{}, "If specified, the game will update in a new directory instead of the default \"data\" dir", "")
	inPlaceArg := goopt.Flag([]string{"--in-place"}, []string{}, "If specified, the game will update in-place in the default \"data\" dir", "")
	osArg := goopt.StringWithLabel([]string{"--os"}, "", "OS", "The build's target OS[4] - windows, linux or mac")
	archArg := goopt.StringWithLabel([]string{"--arch"}, "", "ARCH", "The build's target arch bits[4] - 32 or 64")
	executableArg := goopt.StringWithLabel([]string{"--executable"}, "", "EXE", "The path to the executable in the build[5]")

	versionArg := goopt.Flag([]string{"-v", "--version"}, []string{}, "Displays the version", "")

	goopt.Version = project.Version
	goopt.Summary = "joltron [options] (run [args] | install | uninstall | noop)"
	oldUsage := goopt.Usage
	goopt.Usage = func() string {
		return oldUsage() + `
Notes:
  [1] Game uid is platform specific. Game Jolt's uid for example is just the package ID
  [2] For security reasons, the auth token is recommended to be short lived and usable only by the desired user.
      The runner makes no special attempts to secure it's transmission.
  [3] Mutexes are currently only supported on Windows platform.
  [4] OS/arch should be the OS/arch you are running under currently, not what the build was targeted for
  [5] The path is relative to the archive, and may be omitted if the build is not an archive
`
	}
	goopt.Parse(nil)

	if *versionArg {
		fmt.Println("joltron", goopt.Version)
		finishRun(nil, 0)
	}

	if *mutexArg != "" {
		if err := OS.CreateMutex(*mutexArg); err != nil {
			fmt.Fprintf(os.Stderr, "Could not acquire the %s mutex\n", *mutexArg)
			finishRun(nil, 1)
		}
	}

	// If directory is not specified, attempt to use the directory of the currently launched executable.
	// This is used for standalone games when they are executed with double click or shortcuts without args etc.
	dir := *dirArg
	if dir == "" {
		_dir, err := osext.ExecutableFolder()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Couldn't figure out executable directory: "+err.Error())
			finishRun(nil, 1)
		}
		dir = _dir
	}

	// Attempt to validate the common options to all executions
	manifest, net, dir, err := validateCommonOptions(*portArg, dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		finishRun(nil, 1)
	}

	// What action to take for this execution.
	// Should be run, install or uninstall.
	cmd := ""
	runArgs := []string{}

	// Standalone use - when the runner is simply double clicked or executed with no arguments.
	// It should look for the manifest next to the executable.
	// If found it should run it, otherwise it should display help and quit.
	if len(goopt.Args) == 0 {
		if manifest == nil {
			cmd = "help"
		} else {
			cmd = "run"
		}
	} else {
		cmd = goopt.Args[0]
		runArgs = goopt.Args[1:]
	}

	if cmd == "help" {
		fmt.Println(goopt.Usage())
		finishRun(net, 1)
	}

	if *symbioteArg {
		symbiote(net)
	}

	if *waitForConnectionArg != 0 {
		if err := <-waitForConnection(net, time.Duration(*waitForConnectionArg)*time.Second); err != nil {
			panic(err.Error())
		}
	}

	result, err := func() (error, error) {

		// Any other operation besides "help" and "version" (both should be handled by now) mutates the game data in some way.
		// Therefore we should reject the operation if we're already in the middle of an operation.
		if game.IsRunnerRunning(net.Port(), dir, 3*time.Second) {
			return nil, errors.New("Game is already being managed by another runner instance")
		}

		// If the manifest already exists, set running info on it.
		// The only case the manifest will not exist is if we're doing a fresh install
		// In that case, the patcher will set the running info on it when it creates it for the first time.
		if manifest != nil {
			manifest.RunningInfo = &data.RunningInfo{
				Pid:   os.Getpid(),
				Port:  net.Port(),
				Since: time.Now().Unix(),
			}
			if err = game.WriteManifest(manifest, dir, nil); err != nil {
				return nil, errors.New("Failed to update game manifest with running info: " + err.Error())
			}
		}

		switch cmd {
		case "run":
			return run(net, dir, runArgs), nil

		case "install":
			var updateMetadata *data.UpdateMetadata

			gameUID := *gameArg
			if gameUID == "" {

				// TODO get the data from the manifest
				return nil, errors.New("Game UID must be specified")
			}

			var sideBySide bool
			if *useBuildDirsArg == true && *inPlaceArg == true {
				return nil, errors.New("Side-by-side and in-place are mutual exclusive, they can't be used at the same time")
			} else if *useBuildDirsArg == false && *inPlaceArg == false {
				// By default we prefer updating in place
				sideBySide = manifest != nil && manifest.Info.Dir != "data"
			} else {
				sideBySide = *useBuildDirsArg
			}

			if *platformURLArg == "" && (*urlArg == "" || *osArg == "" || *archArg == "" || *executableArg == "") {
				return nil, errors.New("Platform url or game data (url, os, arch and executable) must be specified")
			}

			if *platformURLArg != "" {
				// If we're already in the middle of a patch, finish it first.
				nextGameUID := ""
				if manifest != nil && manifest.PatchInfo != nil {
					nextGameUID = manifest.PatchInfo.GameUID
				}

				updateMetadata, err = game.GetMetadata(gameUID, nextGameUID, *platformURLArg, *authTokenArg, *metadataArg)
				if err != nil {
					return nil, err
				}
				updateMetadata.PlatformURL = *platformURLArg

				if updateMetadata.SideBySide == nil {
					updateMetadata.SideBySide = &sideBySide
				}
			} else {
				var remoteSize int64
				if *remoteSizeArg != "" {
					remoteSize, err = strconv.ParseInt(*remoteSizeArg, 10, 64)
					if err != nil || remoteSize < 0 {
						return nil, errors.New("Remote file size must be a positive integer")
					}
				}

				updateMetadata = &data.UpdateMetadata{
					GameUID:    gameUID,
					URL:        *urlArg,
					Checksum:   *checksumArg,
					RemoteSize: remoteSize,
					OS:         *osArg,
					Arch:       *archArg,
					Executable: *executableArg,
					SideBySide: &sideBySide,
				}
			}

			if updateMetadata == nil {
				return nil, errors.New("Installation arguments are invalid")
			}

			if updateMetadata.OS != "windows" && updateMetadata.OS != "mac" && updateMetadata.OS != "linux" {
				return nil, errors.New("OS must be either 'windows', 'mac' or 'linux'")
			}
			if updateMetadata.Arch != "32" && updateMetadata.Arch != "64" {
				return nil, errors.New("Arch must be either '32' or '64'")
			}
			if updateMetadata.RemoteSize != 0 && updateMetadata.RemoteSize < 1 {
				return nil, errors.New("Remote file size must be a positive integer")
			}

			return update(net, dir, updateMetadata, *hideLoaderArg, *launchArg), nil

		case "uninstall":
			return uninstall(net, dir), nil

		case "noop":
			if !*symbioteArg {
				return nil, errors.New("Noop operation has to have symbiote specified. It's meant to be used to idle while holding a mutex")
			}

			// Idle forever
			make(chan bool) <- true

			return nil, nil

		default:
			return nil, fmt.Errorf("%q is not a valid command. See \"joltron help\" for more info", cmd)
		}
	}()

	var endErrMessageType string
	if err != nil {
		endErrMessageType = "abort"
	} else if result != nil {
		endErrMessageType = "error"
	}

	// If the manifest exists by the end of this runner run, clear its running info
	manifest, err2 := game.GetManifest(dir, nil)
	if err2 == nil && manifest.RunningInfo != nil {
		manifest.RunningInfo = nil
		if err2 = game.WriteManifest(manifest, dir, nil); err2 != nil {
			result = errors.New("Failed to clear game manifest's running info: " + err2.Error())
		}
	}

	if result != nil {
		err = result
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		net.Broadcast(&outgoing.OutMsgUpdate{
			Message: endErrMessageType,
			Payload: err.Error(),
		})

		finishRun(net, 1)
	}

	finishRun(net, 0)
}

func finishRun(net *jsonnet.Listener, exitCode int) {
	<-time.After(1 * time.Second)
	if net != nil {
		net.Close()
	}
	os.Exit(exitCode)
}

func waitForConnection(net *jsonnet.Listener, timeout time.Duration) <-chan error {
	ch := make(chan error)
	go func() {
		defer close(ch)

		// If we already have a connection, do nothing
		if net.ConnectionCount() != 0 {
			ch <- nil
			return
		}

		// Otherwise wait for a new connection
		s, err := net.OnConnection()
		if err != nil {
			ch <- err
			return
		}
		defer s.Close()

		for {
			select {
			case <-time.After(timeout):
				ch <- errors.New("Connection wasn't made in time")
				return
			case _, open := <-s.Next():
				if !open {
					break
				}

				ch <- nil
				return
			}
		}
	}()
	return ch
}

func symbiote(net *jsonnet.Listener) {
	go func() {
		var conn *jsonnet.Connection

		// If we already have a connection, use it
		conns := net.Connections()
		if len(conns) != 0 {
			// Connection at position 0 is the first connection
			conn = conns[0]
		} else {
			// Otherwise wait for a new connection
			s, err := net.OnConnection()
			if err != nil {
				panic(fmt.Sprintf("Could not subscribe to net connection events: %s\n", err.Error()))
			}

			tmp := <-s.Next()
			s.Close()
			if tmp == nil {
				return
			}
			conn = tmp.(*jsonnet.Connection)
		}

		<-conn.Done()
		panic("Symbiote host connection closed, aborting")
	}()
}

func validateCommonOptions(_port int, dir string) (*data.Manifest, *jsonnet.Listener, string, error) {
	port := uint16(_port)
	if port != 0 && port < 1024 || port > 0xffff {
		return nil, nil, "", errors.New("Invalid port number")
	} else if port == 0 {
		temp, err := getRandomPort()
		if err != nil {
			return nil, nil, "", err
		}
		port = temp
	}

	if dir == "" {
		return nil, nil, "", errors.New("Directory must be specified")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, "", err
	}

	manifest, err := game.GetManifest(dir, nil)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, "", err
	}

	net, err := jsonnet.NewListener(port, nil)
	if err != nil {
		return nil, nil, "", err
	}

	return manifest, net, dir, nil
}

func getRandomPort() (uint16, error) {
	for i := 0; i < 10; i++ {
		port := 1024 + rand.Intn(0xffff-1024)
		addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}

		listener, err := net.ListenTCP("tcp", addr)
		if err != nil {
			continue
		}
		if err = listener.Close(); err != nil {
			return 0, err
		}
		return uint16(port), nil
	}
	return 0, errors.New("Could not find an unused port in a reasonable amount of retries")
}

func run(net *jsonnet.Listener, dir string, args []string) error {
	var mu sync.Mutex

	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "gameLaunchBegin",
		Payload: map[string]interface{}{
			"dir":  dir,
			"args": args,
		},
	})

	launch, err := launcher.NewLauncher(dir, args, net, nil)
	if err != nil {
		net.Broadcast(&outgoing.OutMsgUpdate{
			Message: "gameLaunchFailed",
			Payload: err.Error(),
		})
		return err
	}

	go func() {
		_launch := launch
		launchErr := <-_launch.Done()
		if launchErr == nil {
			net.Broadcast(&outgoing.OutMsgUpdate{
				Message: "gameClosed",
			})
		} else {
			switch launchErr.(type) {
			case *exec.ExitError:
				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "gameCrashed",
					Payload: launchErr.Error(),
				})
			default:
				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "gameLaunchFailed",
					Payload: launchErr.Error(),
				})
			}
		}

		mu.Lock()
		if _launch == launch {
			launch = nil
		}
		mu.Unlock()
	}()

	var currentGameUID = launch.GameUID()
	var patch *patcher.Patch
	var availableUpdateMsg *data.UpdateMetadata

	go func() {
		for {
			msg, open := <-net.IncomingMessages
			if !open {
				break
			}

			switch msg.Payload.(type) {
			case *incoming.InMsgCheckForUpdates:
				if patch != nil || launch == nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Already running an update",
					})
					break
				}

				checkUpdateInfo := msg.Payload.(*incoming.InMsgCheckForUpdates)
				updateData, err := game.GetMetadata(checkUpdateInfo.GameUID, "", checkUpdateInfo.PlatformURL, checkUpdateInfo.AuthToken, checkUpdateInfo.Metadata)
				if err != nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   err.Error(),
					})
					break
				}

				// If we're already on the latest build, do nothing
				if updateData.GameUID == currentGameUID {
					break
				}

				// Check again because while getting the metadata a patch may have started.
				if patch != nil || launch == nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Already running an update",
					})
					break
				}

				availableUpdateMsg = updateData
				msg.Respond(&outgoing.OutMsgResult{
					Success: true,
				})
				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "updateAvailable",
					Payload: availableUpdateMsg,
				})

			case *data.UpdateMetadata:
				if patch != nil || launch == nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Already running an update",
					})
					break
				}

				availableUpdateMsg = msg.Payload.(*data.UpdateMetadata)
				msg.Respond(&outgoing.OutMsgResult{
					Success: true,
				})
				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "updateAvailable",
					Payload: availableUpdateMsg,
				})
			case *incoming.InMsgUpdateBegin:
				// We lock to make sure patch remains unset and launch remain set while we validate stuff and create the new patch instance
				mu.Lock()

				if patch != nil || availableUpdateMsg == nil || launch == nil {
					mu.Unlock()
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "No update available to begin",
					})
					break
				}

				updateMetadata := &data.UpdateMetadata{
					GameUID:    availableUpdateMsg.GameUID,
					URL:        availableUpdateMsg.URL,
					Checksum:   availableUpdateMsg.Checksum,
					RemoteSize: availableUpdateMsg.RemoteSize,
					OS:         availableUpdateMsg.OS,
					Arch:       availableUpdateMsg.Arch,
					Executable: availableUpdateMsg.Executable,

					OldBuildMetadata: availableUpdateMsg.OldBuildMetadata,
					NewBuildMetadata: availableUpdateMsg.NewBuildMetadata,
					DiffMetadata:     availableUpdateMsg.DiffMetadata,

					SideBySide: availableUpdateMsg.SideBySide,
				}
				availableUpdateMsg = nil

				if updateMetadata.SideBySide == nil {
					log.Println("Build dirs were not specified, using currently running instance's settings")
					currentBuildDirs, err := launch.UsesBuildDirs()
					if err != nil {
						mu.Unlock()
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   err.Error(),
						})
						break
					}
					updateMetadata.SideBySide = &currentBuildDirs
				}

				if err != nil {
					mu.Unlock()
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   err.Error(),
					})
					break
				}

				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "updateBegin",
					Payload: map[string]interface{}{
						"dir":      dir,
						"metadata": updateMetadata,
					},
				})

				patch, err = patcher.NewInstall(nil, dir, true, net, updateMetadata, nil)
				mu.Unlock()

				if err != nil {
					net.Broadcast(&outgoing.OutMsgUpdate{
						Message: "updateFailed",
						Payload: err.Error(),
					})
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   err.Error(),
					})
					break
				}
				go func() {
					_patch := patch
					<-_patch.Done()
					if err = patch.Result(); err != nil {
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "updateFailed",
							Payload: err.Error(),
						})
					} else {
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "updateFinished",
						})
					}

					mu.Lock()
					if _patch == patch {
						patch = nil
					}
					mu.Unlock()
				}()

				msg.Respond(&outgoing.OutMsgResult{
					Success: true,
				})
			case *incoming.InMsgUpdateApply:
				// Set to a temp variable to make sure we still have a reference, ensuring we never run patch.State() when patch is nil
				_patch := patch
				if _patch == nil || _patch.State() != patcher.StateUpdateReady || launch == nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "No update ready to apply",
					})
					break
				}

				relaunchInfo := msg.Payload.(*incoming.InMsgUpdateApply)

				// We need to wait for children to exit before resuming.
				// They will be killed if they linger for more than 5 seconds after signaling.
				net.Broadcast(&outgoing.OutMsgUpdate{
					Message: "updateApply",
					Payload: relaunchInfo,
				})

				go func() {
					for i := 0; i < 5; i++ {
						time.Sleep(1 * time.Second)

						// Set to a temp variable, same reason as above.
						_patch = patch

						// Patch may have been canceled, in which case it'll be set to nil
						if _patch == nil {
							break
						}

						// If the state changed, the child may have quit or the patch has been canceled.
						if _patch.State() != patcher.StateUpdateReady {
							break
						}

						if launcher.InstanceCount() == 0 {
							break
						}
					}

					// If after a few seconds the game hasn't quit on it's own - force kill it.
					instancesCount := launcher.InstanceCount()
					if _patch != nil && _patch.State() == patcher.StateUpdateReady && instancesCount != 0 {
						errors := launcher.KillAll()
						instancesCount -= len(errors)
						for instancesCount > 0 {
							net.Broadcast(&outgoing.OutMsgUpdate{
								Message: "gameKilled",
							})
							instancesCount--
						}
					}

					var result error
					if _patch != nil {
						<-_patch.Done()
						result = _patch.Result()
					} else {
						result = nil
					}

					if result != nil {
						// No need to send the result of the patch here because we do it elsewhere.
						// Currently, it's in the go func we created shortly after starting the new patch operation.
						// net.Broadcast(&outgoing.OutMsgUpdate{
						// 	Message: "updateFailed",
						// 	Payload: result.Error(),
						// })
						return
					}

					net.Broadcast(&outgoing.OutMsgUpdate{
						Message: "gameRelaunchBegin",
						Payload: map[string]interface{}{
							"dir":  dir,
							"args": args,
						},
					})

					mu.Lock()
					args := relaunchInfo.Args
					if args == nil {
						args = []string{}
					}
					launch, err = launcher.NewLauncher(dir, args, net, nil)
					if err != nil {
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "gameRelaunchFailed",
							Payload: err.Error(),
						})
					} else {
						currentGameUID = launch.GameUID()

						go func() {
							_launch := launch
							launchErr := <-_launch.Done()
							if launchErr == nil {
								net.Broadcast(&outgoing.OutMsgUpdate{
									Message: "gameClosed",
								})
							} else {
								switch launchErr.(type) {
								case *exec.ExitError:
									net.Broadcast(&outgoing.OutMsgUpdate{
										Message: "gameCrashed",
										Payload: launchErr.Error(),
									})
								default:
									net.Broadcast(&outgoing.OutMsgUpdate{
										Message: "gameRelaunchFailed",
										Payload: launchErr.Error(),
									})
								}
							}

							mu.Lock()
							if _launch == launch {
								launch = nil
							}
							mu.Unlock()
						}()
					}
					mu.Unlock()
				}()

				msg.Respond(&outgoing.OutMsgResult{
					Success: true,
				})
			case *incoming.InMsgState:
				var isPaused bool
				patcherState := -1
				state := "running"
				if patch != nil {
					state = "updating"
					isPaused = !patch.IsRunning()
					patcherState = int(patch.State())
				}

				cmd := msg.Payload.(*incoming.InMsgState)
				manifest, err := game.GetManifest(dir, nil)
				if err != nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   err.Error(),
					})
					break
				}

				if !cmd.IncludePatchInfo {
					manifest.PatchInfo = nil
				}

				msg.Respond(&outgoing.OutMsgState{
					Version:      project.Version,
					State:        state,
					PatcherState: patcherState,
					Pid:          os.Getpid(),
					IsRunning:    true,
					IsPaused:     isPaused,
					Manifest:     manifest,
				})
			case *incoming.InMsgControlCommand:
				cmd := msg.Payload.(*incoming.InMsgControlCommand)
				switch cmd.Command {
				case "kill":
					// temp variable to spare a lock
					_launch := launch
					if _launch == nil {
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   "Launcher isn't running",
						})
						break
					}

					err := _launch.Kill()
					if err != nil {
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   err.Error(),
						})
					} else {
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "gameKilled",
						})
					}
				case "pause":
					// Temp variable to spare a lock
					_patch := patch
					if _patch == nil {
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   "Patcher isn't running",
						})
						break
					}

					_patch.Pause()
					msg.Respond(&outgoing.OutMsgResult{
						Success: true,
					})
					net.Broadcast(&outgoing.OutMsgUpdate{
						Message: "paused",
					})

				case "resume":
					// Temp variable to spare a lock
					_patch := patch
					if _patch == nil {
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   "Patcher isn't running",
						})
						break
					}

					if cmd.ExtraData != nil && len(cmd.ExtraData) != 0 {
						if authToken, ok := cmd.ExtraData["authToken"]; ok {
							_patch.SetAuthToken(authToken)
						}
						if extraMetadata, ok := cmd.ExtraData["extraMetadata"]; ok {
							_patch.SetExtraMetadata(extraMetadata)
						}
					}

					_patch.Resume()
					msg.Respond(&outgoing.OutMsgResult{
						Success: true,
					})
					net.Broadcast(&outgoing.OutMsgUpdate{
						Message: "resumed",
					})
				case "cancel":
					// TODO: why can't we cancel while game is running?
					// Is this still the case?
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Can't cancel an update while a game is running",
					})
					break
				default:
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Invalid input",
					})
				}
			}
		}
	}()

	for {
		time.Sleep(1 * time.Second)
		if launch == nil && patch == nil {
			break
		}
	}

	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "gameLaunchFinished",
	})

	return nil
}

func update(net *jsonnet.Listener, dir string, updateMetadata *data.UpdateMetadata, hideLoader, launchLater bool) error {
	if updateMetadata.URL == "" {
		return errors.New("Url must be specified when in update mode")
	}

	if !hideLoader {
		err := launchLoader(net)
		if err != nil {
			log.Println(err)
		}
	}

	os2, err := OS.NewFileScope(dir, true)
	if err != nil {
		return err
	}

	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "updateBegin",
		Payload: map[string]interface{}{
			"dir":      dir,
			"metadata": updateMetadata,
		},
	})

	patch, err := patcher.NewInstall(nil, dir, false, net, updateMetadata, os2)
	if err != nil {
		net.Broadcast(&outgoing.OutMsgUpdate{
			Message: "updateFailed",
			Payload: err.Error(),
		})
		return err
	}

	go func() {
		for {
			select {
			case <-patch.Done():
				break
			case msg, open := <-net.IncomingMessages:
				if !open {
					break
				}

				switch msg.Payload.(type) {
				case *incoming.InMsgControlCommand:
					cmd := msg.Payload.(*incoming.InMsgControlCommand)
					switch cmd.Command {
					case "pause":
						patch.Pause()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "paused",
						})
					case "resume":
						if cmd.ExtraData != nil && len(cmd.ExtraData) != 0 {
							if authToken, ok := cmd.ExtraData["authToken"]; ok {
								patch.SetAuthToken(authToken)
							}
							if extraMetadata, ok := cmd.ExtraData["extraMetadata"]; ok {
								patch.SetExtraMetadata(extraMetadata)
							}
						}
						patch.Resume()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "resumed",
						})
					case "cancel":
						patch.Cancel()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "canceled",
						})
					default:
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   "Unknown command",
						})
					}
				case *incoming.InMsgState:
					cmd := msg.Payload.(*incoming.InMsgState)
					manifest, err := game.GetManifest(dir, nil)
					if err != nil && !os.IsNotExist(err) {
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   err.Error(),
						})
						break
					}

					if manifest != nil && !cmd.IncludePatchInfo {
						manifest.PatchInfo = nil
					}
					msg.Respond(&outgoing.OutMsgState{
						Version:      project.Version,
						State:        "updating",
						PatcherState: int(patch.State()),
						Pid:          os.Getpid(),
						IsRunning:    false,
						IsPaused:     !patch.IsRunning(),
						Manifest:     manifest,
					})
				default:
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Invalid input",
					})
				}
			}
		}
	}()

	<-patch.Done()
	if err = patch.Result(); err != nil {
		net.Broadcast(&outgoing.OutMsgUpdate{
			Message: "updateFailed",
			Payload: err.Error(),
		})
		return err
	}
	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "updateFinished",
	})

	if launchLater {
		return run(net, dir, goopt.Args[1:])
	}

	return nil
}

func uninstall(net *jsonnet.Listener, dir string) error {
	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "uninstallBegin",
		Payload: dir,
	})

	patch, err := patcher.NewUninstall(nil, dir, net, nil)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-patch.Done():
				break
			case msg, open := <-net.IncomingMessages:
				if !open {
					break
				}

				if patch == nil {
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Not ready yet, try again in a sec",
					})
				}

				switch msg.Payload.(type) {
				case *incoming.InMsgControlCommand:
					cmd := msg.Payload.(*incoming.InMsgControlCommand)
					switch cmd.Command {
					case "pause":
						patch.Pause()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "paused",
						})
					case "resume":
						patch.Resume()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "resumed",
						})
					case "cancel":
						patch.Cancel()
						msg.Respond(&outgoing.OutMsgResult{
							Success: true,
						})
						net.Broadcast(&outgoing.OutMsgUpdate{
							Message: "canceled",
						})
					default:
						msg.Respond(&outgoing.OutMsgResult{
							Success: false,
							Error:   "Unknown command",
						})
					}
				case *incoming.InMsgState:
					cmd := msg.Payload.(*incoming.InMsgState)
					manifest, _ := game.GetManifest(dir, nil)
					if manifest != nil && !cmd.IncludePatchInfo {
						manifest.PatchInfo = nil
					}
					msg.Respond(&outgoing.OutMsgState{
						Version:      project.Version,
						State:        "uninstalling",
						PatcherState: int(patch.State()),
						Pid:          os.Getpid(),
						IsRunning:    false,
						IsPaused:     !patch.IsRunning(),
						Manifest:     manifest,
					})
				default:
					msg.Respond(&outgoing.OutMsgResult{
						Success: false,
						Error:   "Invalid input",
					})
				}
			}
		}
	}()

	<-patch.Done()
	if err = patch.Result(); err != nil {
		net.Broadcast(&outgoing.OutMsgUpdate{
			Message: "uninstallFailed",
			Payload: err.Error(),
		})
		return err
	}

	net.Broadcast(&outgoing.OutMsgUpdate{
		Message: "uninstallFinished",
	})
	return nil
}

func launchLoader(net *jsonnet.Listener) error {
	dir, err := osext.ExecutableFolder()
	if err != nil {
		return err
	}

	_os, err := OS.NewDefaultOS()
	if err != nil {
		return err
	}

	cmd, err := launcher.LaunchExecutable(filepath.Join(dir, "game-loader"), []string{string(net.Port())}, _os)
	if err != nil {
		return err
	}

	return cmd.Run()
}
