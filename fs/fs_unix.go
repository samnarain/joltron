// +build linux darwin

package fs

import (
	"os"

	OS "github.com/gamejolt/joltron/os"
)

// CreateLink creates a shortcut from path to target
func CreateLink(target, path string, os2 OS.OS) error {
	return CreateLinkWithIcon(target, path, "", os2)
}

// CreateLinkWithIcon creates a shortcut from path to target with an icon
func CreateLinkWithIcon(target, path, icon string, os2 OS.OS) error {
	if err := os2.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os2.Symlink(target, path)
}

// GetExecutableIconLocation gets the executable's icon location
func GetExecutableIconLocation(path string) string {
	return ""
}

// GetLinkTarget gets the target of a shortcut
func GetLinkTarget(path string, os2 OS.OS) (string, error) {
	return os2.Readlink(path)
}

// IsLink returns true if the given file is a link
func IsLink(path string, os2 OS.OS) bool {
	stat, err := os2.Lstat(path)
	if err != nil {
		return false
	}

	return stat.Mode()&os.ModeSymlink != 0
}
