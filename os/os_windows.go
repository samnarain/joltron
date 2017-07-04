// +build windows

package os

import (
	"errors"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// GetArch gets the current arch (32 or 64)
func GetArch() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `System\CurrentControlSet\Control\Session Manager\Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}

	result, _, err := k.GetStringValue("PROCESSOR_ARCHITECTURE")
	if err != nil {
		return "", err
	}
	if strings.Contains(string(result), "64") {
		return "64", nil
	}
	return "32", nil
}

var (
	// ErrNoSuchMutex is returned when trying to release a mutex that is not held by this process
	ErrNoSuchMutex = errors.New("No such mutex")
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
	procCloseHandle = kernel32.NewProc("CloseHandle")
	heldMutexes     = map[string]uintptr{}
	mu              = sync.Mutex{}
)

// CreateMutex creates a named system level mutex
func CreateMutex(name string) error {
	mu.Lock()
	defer mu.Unlock()

	ret, _, err := procCreateMutex.Call(
		0,
		1,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))),
	)
	switch int(err.(syscall.Errno)) {
	case 0:
		heldMutexes[name] = ret
		return nil
	default:
		return err
	}
}

// ReleaseMutex releases the mutex created by CreateMutex
func ReleaseMutex(name string) error {
	mu.Lock()
	defer mu.Unlock()

	mutexH, ok := heldMutexes[name]
	if !ok {
		return ErrNoSuchMutex
	}

	ret, _, err := procCloseHandle.Call(mutexH)
	switch int(err.(syscall.Errno)) {
	case 0:
		if ret != 0 {
			delete(heldMutexes, name)
			return nil
		}
		return errors.New("Could not release mutex")
	default:
		return err
	}
}
