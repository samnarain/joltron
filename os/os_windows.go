// +build windows

package os

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// GetArch gets the current arch (32 or 64)
func GetArch() (string, error) {
	if os.Getenv("PROCESSOR_ARCHITEW6432") != "" {
		return "64", nil
	}
	return "32", nil
}

var (
	ErrNoSuchMutex = errors.New("No such mutex")
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
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

	ret, _, err := procCreateMutex.Call(mutexH)
	switch int(err.(syscall.Errno)) {
	case 0:
		delete(heldMutexes, name)
		return nil
	default:
		return err
	}
}
