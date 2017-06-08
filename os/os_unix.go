// +build linux darwin

package os

import (
	"errors"
	"os/exec"
	"strings"
)

// GetArch gets the current arch (32 or 64)
func GetArch() (string, error) {
	cmd := exec.Command("uname", []string{"-m"}...)
	result, err := cmd.Output()
	if err != nil {
		return "", err
	}

	str := strings.Trim(string(result), " \r\n\t")
	if str == "x86_64" {
		return "64", nil
	} else if str == "i386" || str == "i686" {
		return "32", nil
	}
	return str, errors.New("Could not determine architecture bit-ness")
}
