// +build windows

package fs

import (
	"errors"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	OS "github.com/gamejolt/joltron/os"
)

const (
	createLink = iota
	getLinkTarget
)

var (
	scriptMap = map[int]string{
		createLink: `
Set shell = WScript.CreateObject("WScript.Shell")
Set shortcut = shell.CreateShortcut(WScript.Arguments(0))
shortcut.TargetPath = WScript.Arguments(1)
If WScript.Arguments.Count > 2 Then
    shortcut.IconLocation = WScript.Arguments(2)
End If 
shortcut.Save
WScript.Echo "Ok"
`,
		getLinkTarget: `
Set shell = WScript.CreateObject("WScript.Shell")
Set shortcut = shell.CreateShortcut(WScript.Arguments(0))
WScript.Echo shortcut.TargetPath
WScript.Echo "Ok"
`,
	}

	scriptFileMap = map[int]string{}
)

func executeScript(id int, args ...string) (string, error) {
	tmpDir := os.TempDir()
	script, ok := scriptFileMap[id]
	if !ok {
		code, ok := scriptMap[id]
		if !ok {
			return "", errors.New("No such script id")
		}

		f, err := ioutil.TempFile(tmpDir, "gjolt-script")
		if err != nil {
			return "", err
		}

		n, err := f.WriteString(code)
		if err != nil {
			return "", err
		}
		if n != len(code) {
			return "", errors.New("Could not write temp script file")
		}

		script = f.Name()
		if err = f.Close(); err != nil {
			return "", err
		}
		scriptFileMap[id] = script
	}

	if script == "" {
		return "", errors.New("Could not find temp script file")
	}

	scriptArgs := append([]string{"/E:vbs", "/NoLogo", script}, args...)
	cmd := exec.Command("cscript.exe", scriptArgs...)
	result, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func executeScriptOk(id int, args ...string) (string, error) {
	result, err := executeScript(id, args...)
	if err != nil {
		return "", err
	}

	_lines := strings.Split(result, "\n")
	lines := []string{}
	for _, line := range _lines {
		trimmed := strings.Trim(line, " \r\n\t")
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}

	if len(lines) == 0 {
		return "", errors.New("Command failed, no error output")
	}

	result = strings.Join(lines[:len(lines)-1], "\n")
	if lines[len(lines)-1] != "Ok" {
		return "", errors.New("Command failed: " + result)
	}
	return result, nil
}

// CreateLink creates a shortcut from path to target
func CreateLink(target, path string, os2 OS.OS) error {
	return CreateLinkWithIcon(target, path, GetExecutableIconLocation(target), os2)
}

// CreateLinkWithIcon creates a shortcut from path to target with an icon
func CreateLinkWithIcon(target, path, icon string, os2 OS.OS) error {
	if !os2.AllowedRead(target) || !os2.AllowedWrite(path) {
		return OS.ErrNotScoped
	}
	_, err := executeScriptOk(createLink, path, target, icon)
	return err
}

// GetExecutableIconLocation gets the executable's icon location
func GetExecutableIconLocation(path string) string {
	return path + ",0"
}

// GetLinkTarget gets the target of a shortcut
func GetLinkTarget(path string, os2 OS.OS) (string, error) {
	if !os2.AllowedRead(path) {
		return "", OS.ErrNotScoped
	}

	target, err := executeScriptOk(getLinkTarget, path)
	if err != nil {
		return "", err
	}

	if target == "" {
		return "", errors.New("Could not retrieve the link target")
	}
	return target, nil
}

// IsLink returns true if the given file is a link
func IsLink(path string, os2 OS.OS) bool {
	ext := filepath.Ext(path)
	if ext != ".lnk" && ext != ".url" {
		return false
	}

	target, err := GetLinkTarget(path, os2)
	if err != nil || target == "" {
		return false
	}

	return true
}
