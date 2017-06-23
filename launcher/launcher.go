package launcher

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sync"

	plist "github.com/DHowett/go-plist"
	"github.com/gamejolt/joltron/broadcast"
	"github.com/gamejolt/joltron/game"
	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/network/jsonnet"
	OS "github.com/gamejolt/joltron/os"
)

var (
	mu        sync.Mutex
	instances = make([]*Launcher, 0, 2)
)

// Launcher comment
type Launcher struct {
	os   OS.OS
	dir  string
	args []string
	cmd  *exec.Cmd

	dataDir  string
	jsonNet  *jsonnet.Listener
	manifest *data.Manifest

	finished    bool
	result      error
	doneCh      chan bool
	broadcaster *broadcast.Broadcaster
}

// NewLauncher comment
func NewLauncher(dir string, args []string, jsonNet *jsonnet.Listener, os2 OS.OS) (*Launcher, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("Launch dir is not absolute")
	}

	if os2 == nil {
		temp, err := OS.NewFileScope(dir, false)
		if err != nil {
			return nil, err
		}
		os2 = temp
	}

	l := &Launcher{
		os:          os2,
		dir:         dir,
		args:        args,
		jsonNet:     jsonNet,
		doneCh:      make(chan bool),
		broadcaster: broadcast.NewBroadcaster(),
	}

	log.Printf("Launching game at dir %s\n", l.dir)

	// Attempt to read manifest file
	manifest, err := game.GetManifest(l.dir, l.os)
	if err != nil {
		return nil, err
	}
	l.manifest = manifest

	if l.manifest.LaunchOptions == nil {
		return nil, errors.New("Game is already running")
	}

	l.dataDir = filepath.Join(l.dir, l.manifest.Info.Dir)
	if stat, err := l.os.Stat(l.dataDir); err != nil || !stat.IsDir() {
		return nil, errors.New("Game data directory doesn't exist")
	}

	path := filepath.Join(l.dataDir, l.manifest.LaunchOptions.Executable)
	if !filepath.IsAbs(path) || !strings.HasPrefix(path, l.dataDir) {
		return nil, fmt.Errorf("Attempted to launch a file outside the game dir: %s", path)
	}

	var cmd *exec.Cmd
	switch l.manifest.OS {
	case "windows":
		cmd, err = LaunchWindows(path, l.args, l.os)
	case "linux":
		cmd, err = LaunchLinux(path, l.args, l.os)
	case "mac":
		cmd, err = LaunchDarwin(path, l.args, l.os)
	}

	if err != nil {
		return nil, err
	}

	l.runCommand(cmd)

	l.manifest.PlayingInfo = &data.PlayingInfo{
		// Port:  port,
		// Pid:   os.Getpid(),
		Args:  args,
		Since: time.Now().Unix(),
	}

	if err := game.WriteManifest(l.manifest, l.dir, l.os); err != nil {
		return nil, err
	}

	// if err := l.prepareNetworkHandler(); err != nil {
	// 	return nil, err
	// }

	TrackInstance(l)

	return l, nil
}

// GameUID returns the game UID this launcher instance is launching
func (l *Launcher) GameUID() string {
	return l.manifest.Info.GameUID
}

// UsesBuildDirs returns true if the game uses build dirs. It does this by looking at the manifest and seeing if the data dir for that build is versioned.
func (l *Launcher) UsesBuildDirs() (bool, error) {
	if l.dataDir == "" {
		return false, errors.New("Cannot determine if the game uses versioned build dirs until the manifest is parsed")
	}

	return filepath.Base(l.dataDir) != "data", nil
}

// Cmd gets the launcher's exec.Cmd
func (l *Launcher) Cmd() *exec.Cmd {
	return l.cmd
}

func (l *Launcher) runCommand(cmd *exec.Cmd) error {
	cmd.Dir = filepath.Dir(cmd.Path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		io.Copy(os.Stdout, stdout)
		log.Println("stdout closed")
	}()
	go func() {
		io.Copy(os.Stderr, stderr)
		log.Println("stderr closed")
	}()

	go func() {
		io.Copy(stdin, os.Stdin)
		log.Println("stdin closed")
	}()

	go func() {
		result := cmd.Run()
		l.result = result
		l.finished = true
		l.kill(false)

		l.manifest.PlayingInfo = nil
		game.WriteManifest(l.manifest, l.dir, l.os)

		l.broadcaster.Broadcast(result)
		close(l.doneCh)
	}()

	l.cmd = cmd
	return nil
}

// Done returns a channel that resolves with the result once the game finished running
func (l *Launcher) Done() <-chan error {
	ch := make(chan error)

	go func() {
		if l.finished {
			ch <- l.result
			close(ch)
		} else {
			<-l.doneCh
			close(ch)
		}
	}()

	return ch
}

func (l *Launcher) ensureExecutable(filename string) error {
	return l.os.Chmod(filename, 0755)
}

// LaunchExecutable launches an executable with given arguments
func LaunchExecutable(executable string, args []string, os2 OS.OS) (*exec.Cmd, error) {
	_os, err := OS.GetOS()
	if err != nil {
		return nil, err
	}

	switch _os {
	case "windows":
		return LaunchWindows(executable, args, os2)
	case "linux":
		return LaunchLinux(executable, args, os2)
	case "mac":
		return LaunchDarwin(executable, args, os2)
	default:
		return nil, errors.New("OS is not supported")
	}
}

// LaunchWindows launches a Windows executable
func LaunchWindows(executable string, args []string, os2 OS.OS) (*exec.Cmd, error) {
	stat, err := os2.Stat(executable)
	if err != nil {
		return nil, err
	}

	if stat.IsDir() {
		return nil, errors.New("Can't launch a directory as an executable")
	}

	if err := os2.Chmod(executable, 0755); err != nil {
		return nil, err
	}

	if filepath.Ext(executable) == "jar" {
		javaExecutable, err := exec.LookPath("java")
		if err != nil {
			return nil, errors.New("Java is not installed")
		}

		args = append([]string{"-jar", executable}, args...)
		cmd := exec.Command(javaExecutable, args...)
		return cmd, nil
	}

	cmd := exec.Command(executable, args...)
	return cmd, nil
}

// LaunchLinux launches a Linux executable
func LaunchLinux(executable string, args []string, os2 OS.OS) (*exec.Cmd, error) {
	stat, err := os2.Stat(executable)
	if err != nil {
		return nil, err
	}

	if stat.IsDir() {
		return nil, errors.New("Can't launch a directory as an executable")
	}

	if err := os2.Chmod(executable, 0755); err != nil {
		return nil, err
	}

	if filepath.Ext(executable) == "jar" {
		javaExecutable, err := exec.LookPath("java")
		if err != nil {
			return nil, errors.New("Java is not installed")
		}

		args = append([]string{"-jar", executable}, args...)
		cmd := exec.Command(javaExecutable, args...)
		return cmd, nil
	}

	cmd := exec.Command(executable, args...)
	return cmd, nil
}

// LaunchDarwin launches a Mac Executable
func LaunchDarwin(executable string, args []string, os2 OS.OS) (*exec.Cmd, error) {
	stat, err := os2.Stat(executable)
	if err != nil {
		return nil, err
	}

	if !stat.IsDir() {
		if err := os2.Chmod(executable, 0755); err != nil {
			return nil, err
		}

		if filepath.Ext(executable) == "jar" {
			javaExecutable, err := exec.LookPath("java")
			if err != nil {
				return nil, errors.New("Java is not installed")
			}

			args = append([]string{"-jar", executable}, args...)
			cmd := exec.Command(javaExecutable, args...)
			return cmd, nil
		}

		cmd := exec.Command(executable, args...)
		return cmd, nil
	}

	if !strings.HasSuffix(executable, ".app") && !strings.HasSuffix(executable, ".app/") {
		return nil, errors.New("That doesn't look like a valid Mac OS X bundle. Expecting .app folder")
	}

	plistPath := filepath.Join(executable, "Contents", "Info.plist")
	plistStat, err := os2.Lstat(plistPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to stat the plist file at %s: %s", plistPath, err.Error())
	}

	if plistStat.IsDir() {
		return nil, fmt.Errorf("That doesn't look like a valid Mac OS X bundle. Info.plist at %s isn't a valid file", plistPath)
	}

	bytes, err := os2.IOUtilReadFile(plistPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read the plist file at %s", plistPath)
	}
	log.Println("Plist:\n" + string(bytes))

	var val interface{}
	_, err = plist.Unmarshal(bytes, &val)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse Info.plist at %s: %s", plistPath, err.Error())
	}

	var valMap map[string]interface{}
	switch val.(type) {
	case map[string]interface{}:
		valMap = val.(map[string]interface{})
	default:
		return nil, errors.New("Plist is invalid. Expected a dictionary type")
	}

	bundleExecutable, ok := valMap["CFBundleExecutable"]
	if !ok {
		return nil, errors.New("Plist is invalid. Missing the CFBundleExecutable field")
	}

	switch bundleExecutable.(type) {
	case string:
		if err := os2.Chmod(filepath.Join(executable, "Contents", "MacOS", bundleExecutable.(string)), 0755); err != nil {
			return nil, errors.New("Failed to make the app executable: " + err.Error())
		}
	default:
		return nil, errors.New("Plist is invalid. Expected CFBundleExecutable field to be a string")
	}

	cmd := exec.Command("open", "-W", executable)
	return cmd, nil
}

// InstanceCount returns the number of currently active child instances
func InstanceCount() int {
	return len(instances)
}

// TrackInstance adds a launcher instance to the tracked instances list. It'll automatically kill it when the updater is interrupted or when KillAll is called.
func TrackInstance(l *Launcher) {
	mu.Lock()
	defer mu.Unlock()

	instances = append(instances, l)
}

// KillAll kills all the current processes
func KillAll() map[*Launcher]error {
	mu.Lock()
	defer mu.Unlock()

	errors := map[*Launcher]error{}
	for _, l := range instances {
		if err := l.kill(true); err != nil {
			errors[l] = err
		}
	}
	return errors
}

// Kill kills a launcher process. It doesn't error out if the process wasn't running
func (l *Launcher) Kill() error {
	return l.kill(false)
}

func (l *Launcher) kill(skipLock bool) error {

	if !skipLock {
		mu.Lock()
	}

	for i, val := range instances {
		if val == l {
			// Fast way of removing an element when we don't care about order - swap removed element with the last element and trim the slice.
			instances[len(instances)-1], instances[i] = instances[i], instances[len(instances)-1]
			instances = instances[:len(instances)-1]
			break
		}
	}

	if !skipLock {
		mu.Unlock()
	}

	if l.finished || l.cmd == nil || l.cmd.Process == nil || (l.cmd.ProcessState != nil && l.cmd.ProcessState.Exited()) {
		return nil
	}

	return l.cmd.Process.Kill()
}
