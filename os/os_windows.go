// +build windows

package os

import (
	"os"
)

// GetArch gets the current arch (32 or 64)
func GetArch() (string, error) {
	if os.Getenv("PROCESSOR_ARCHITEW6432") != "" {
		return "64", nil
	}
	return "32", nil
}
