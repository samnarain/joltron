package os

import (
	"errors"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	// ErrNotScoped is thrown when a file is accessed out of scope
	ErrNotScoped = errors.New("File out of scope")

	// ErrInvalidScope is thrown when a scoped OS is defined with an invalid scope
	ErrInvalidScope = errors.New("Scope is invalid")

	// ErrReadOnly is thrown when a file is accessed in write mode for a read only OS
	ErrReadOnly = errors.New("OS is in read-only")
)

// OS is an interface for the os functionality needed by this program. It is not complete.
type OS interface {
	AllowedRead(path string) bool
	AllowedWrite(path string) bool
	Remove(path string) error
	RemoveAll(path string) error
	MkdirAll(path string, mode os.FileMode) error
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Open(name string) (*os.File, error)
	Rename(oldpath, newpath string) error
	Symlink(oldname, newname string) error
	Stat(name string) (os.FileInfo, error)
	Lstat(name string) (os.FileInfo, error)
	Chmod(name string, mode os.FileMode) error
	Readlink(name string) (string, error)
	IOUtilReadDir(dirname string) ([]os.FileInfo, error)
	IOUtilReadFile(filename string) ([]byte, error)
	IOUtilWriteFile(filename string, data []byte, perm os.FileMode) error
	IOUtilTempFile(dir, prefix string) (*os.File, error)
}

// DefaultOS is the default implementation of the OS struct using the built in os package directly.
type DefaultOS struct{}

// NewDefaultOS creates a new DefaultOS.
func NewDefaultOS() (*DefaultOS, error) {
	return &DefaultOS{}, nil
}

// AllowedRead returns true if the location is allowed to be read from
func (o *DefaultOS) AllowedRead(path string) bool {
	return true
}

// AllowedWrite returns true if the location is allowed to be written to
func (o *DefaultOS) AllowedWrite(path string) bool {
	return true
}

// Remove is the same as os.Remove
func (o *DefaultOS) Remove(path string) error {
	return os.Remove(path)
}

// RemoveAll is the same as os.RemoveAll
func (o *DefaultOS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// MkdirAll is the same as os.MkdirAll
func (o *DefaultOS) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

// OpenFile is the same as os.OpenFile
func (o *DefaultOS) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

// Open is the same as os.Open
func (o *DefaultOS) Open(name string) (*os.File, error) {
	return os.Open(name)
}

// Rename is the same as os.Rename
func (o *DefaultOS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// Symlink is the same as os.Symlink
func (o *DefaultOS) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

// Stat is the same as os.Stat
func (o *DefaultOS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Lstat is the same as os.Lstat
func (o *DefaultOS) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Chmod is the same as os.Chmod
func (o *DefaultOS) Chmod(name string, mode os.FileMode) error {
	return os.Chmod(name, mode)
}

// Readlink is the same as os.Readlink
func (o *DefaultOS) Readlink(name string) (string, error) {
	return os.Readlink(name)
}

// IOUtilReadDir is the same as ioutil.ReadDir
func (o *DefaultOS) IOUtilReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}

// IOUtilReadFile is the same as ioutil.ReadFile
func (o *DefaultOS) IOUtilReadFile(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

// IOUtilWriteFile is the same as ioutil.WriteFile
func (o *DefaultOS) IOUtilWriteFile(filename string, data []byte, perm os.FileMode) error {
	return ioutil.WriteFile(filename, data, perm)
}

// IOUtilTempFile is the same as ioutil.TempFile
func (o *DefaultOS) IOUtilTempFile(dir, prefix string) (*os.File, error) {
	return ioutil.TempFile(dir, prefix)
}

// ReadOnlyOS is the default implementation of the OS struct using the built in os package directly.
type ReadOnlyOS struct{}

// NewReadOnlyOS creates a new ReadOnlyOS.
func NewReadOnlyOS() (*ReadOnlyOS, error) {
	return &ReadOnlyOS{}, nil
}

// AllowedRead returns true if the location is allowed to be read from
func (o *ReadOnlyOS) AllowedRead(path string) bool {
	return true
}

// AllowedWrite returns true if the location is allowed to be written to
func (o *ReadOnlyOS) AllowedWrite(path string) bool {
	return false
}

// Remove is the same as os.Remove
func (o *ReadOnlyOS) Remove(path string) error {
	return ErrReadOnly
}

// RemoveAll is the same as os.RemoveAll
func (o *ReadOnlyOS) RemoveAll(path string) error {
	return ErrReadOnly
}

// MkdirAll is the same as os.MkdirAll
func (o *ReadOnlyOS) MkdirAll(path string, mode os.FileMode) error {
	return ErrReadOnly
}

// OpenFile is the same as os.OpenFile
func (o *ReadOnlyOS) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	p := int(perm)
	write := p&os.O_APPEND != 0 || p&os.O_CREATE != 0 || p&os.O_RDWR != 0 || p&os.O_TRUNC != 0 || p&os.O_WRONLY != 0
	if write {
		return nil, ErrReadOnly
	}
	return os.OpenFile(name, flag, perm)
}

// Open is the same as os.Open
func (o *ReadOnlyOS) Open(name string) (*os.File, error) {
	return os.Open(name)
}

// Rename is the same as os.Rename
func (o *ReadOnlyOS) Rename(oldpath, newpath string) error {
	return ErrReadOnly
}

// Symlink is the same as os.Symlink
func (o *ReadOnlyOS) Symlink(oldname, newname string) error {
	return ErrReadOnly
}

// Stat is the same as os.Stat
func (o *ReadOnlyOS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Lstat is the same as os.Lstat
func (o *ReadOnlyOS) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Chmod is the same as os.Chmod
func (o *ReadOnlyOS) Chmod(name string, mode os.FileMode) error {
	return ErrReadOnly
}

// Readlink is the same as os.Readlink
func (o *ReadOnlyOS) Readlink(name string) (string, error) {
	return os.Readlink(name)
}

// IOUtilReadDir is the same as ioutil.ReadDir
func (o *ReadOnlyOS) IOUtilReadDir(dirname string) ([]os.FileInfo, error) {
	return ioutil.ReadDir(dirname)
}

// IOUtilReadFile is the same as ioutil.ReadFile
func (o *ReadOnlyOS) IOUtilReadFile(filename string) ([]byte, error) {
	return ioutil.ReadFile(filename)
}

// IOUtilWriteFile is the same as ioutil.WriteFile
func (o *ReadOnlyOS) IOUtilWriteFile(filename string, data []byte, perm os.FileMode) error {
	return ErrReadOnly
}

// IOUtilTempFile is the same as ioutil.TempFile
func (o *ReadOnlyOS) IOUtilTempFile(dir, prefix string) (*os.File, error) {
	return nil, ErrReadOnly
}

// FileScope provides fs methods that fail when executed outside of the scoped directory.
type FileScope struct {
	dir     string
	strict  bool
	verbose bool
}

// NewFileScope creates a new file scope.
func NewFileScope(dir string, strict bool) (*FileScope, error) {
	if dir == "" {
		return nil, ErrInvalidScope
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	return &FileScope{
		dir:     dir,
		strict:  strict,
		verbose: false,
	}, nil
}

// Dir returns the scope directory.
func (s *FileScope) Dir() string {
	return s.dir
}

// Strict returns wether the FileScope is in strict mode
func (s *FileScope) Strict() bool {
	return s.strict
}

// SetVerbose sets the FileScope's verbosity.
func (s *FileScope) SetVerbose(verbose bool) {
	s.verbose = verbose
}

// IsScoped returns true if the given file is in the dir scope.
func (s *FileScope) IsScoped(path string) bool {
	if s.dir == "" {
		return false
	}
	return isInDir(path, s.dir)
}

func isInDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator))
}

func (s *FileScope) validate(path string, write bool) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		if s.verbose {
			log.Println("Can't get absolute file of ", path)
		}
		return "", err
	}

	if (write || s.strict) && !s.IsScoped(path) {
		if s.verbose {
			log.Println("File ", path, " is not in scoped dir ", s.dir)
		}
		return "", ErrNotScoped
	}

	return path, nil
}

// AllowedRead returns true if the location is allowed to be read from
func (s *FileScope) AllowedRead(path string) bool {
	if _, err := s.validate(path, false); err != nil {
		return false
	}
	return true
}

// AllowedWrite returns true if the location is allowed to be written to
func (s *FileScope) AllowedWrite(path string) bool {
	if _, err := s.validate(path, true); err != nil {
		return false
	}
	return true
}

// Remove is the same as os.Remove only for a scope dir
func (s *FileScope) Remove(path string) error {
	if s.verbose {
		log.Println("Remove ", path)
	}

	path, err := s.validate(path, true)
	if err != nil {
		return err
	}

	return os.Remove(path)
}

// RemoveAll is the same as os.RemoveAll only for a scope dir.
func (s *FileScope) RemoveAll(path string) error {
	if s.verbose {
		log.Println("RemoveAll ", path)
	}

	path, err := s.validate(path, true)
	if err != nil {
		return err
	}

	return os.RemoveAll(path)
}

// MkdirAll is the same as os.MkdirAll only for a scope dir.
func (s *FileScope) MkdirAll(path string, mode os.FileMode) error {
	if s.verbose {
		log.Println("MkdirAll ", path, " mode ", mode)
	}

	path, err := s.validate(path, true)
	if err != nil {
		return err
	}

	return os.MkdirAll(path, mode)
}

// OpenFile is the same as os.OpenFile only for a scope dir.
func (s *FileScope) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if s.verbose {
		log.Println("OpenFile ", name, " flag ", flag, " perm ", perm)
	}

	p := int(perm)
	write := p&os.O_APPEND != 0 || p&os.O_CREATE != 0 || p&os.O_RDWR != 0 || p&os.O_TRUNC != 0 || p&os.O_WRONLY != 0
	name, err := s.validate(name, write)
	if err != nil {
		return nil, err
	}

	return os.OpenFile(name, flag, perm)
}

// Open is the same as os.Open only for a scope dir.
func (s *FileScope) Open(name string) (*os.File, error) {
	if s.verbose {
		log.Println("Open ", name)
	}

	name, err := s.validate(name, false)
	if err != nil {
		return nil, err
	}

	return os.Open(name)
}

// Rename is the same as os.Rename
func (s *FileScope) Rename(oldpath, newpath string) error {
	if s.verbose {
		log.Println("Rename ", oldpath, " to ", newpath)
	}

	oldpath, err := s.validate(oldpath, true)
	if err != nil {
		return err
	}

	newpath, err = s.validate(newpath, true)
	if err != nil {
		return err
	}

	return os.Rename(oldpath, newpath)
}

// Symlink is the same as os.Symlink only for a scope dir.
func (s *FileScope) Symlink(oldname, newname string) error {
	if s.verbose {
		log.Println("Symlink ", newname, " => ", oldname)
	}

	newname, err := s.validate(newname, false)
	if err != nil {
		return err
	}

	var nameToValidate string
	if filepath.IsAbs(oldname) {
		nameToValidate = oldname
	} else {
		nameToValidate = filepath.Join(newname, oldname)
	}

	if _, err = s.validate(nameToValidate, true); err != nil {
		return err
	}

	return os.Symlink(oldname, newname)
}

// Stat is the same as os.Stat only for a scope dir.
func (s *FileScope) Stat(name string) (os.FileInfo, error) {
	if s.verbose {
		log.Println("Stat ", name)
	}

	name, err := s.validate(name, false)
	if err != nil {
		return nil, err
	}

	return os.Stat(name)
}

// Lstat is the same as os.Lstat only for a scope dir.
func (s *FileScope) Lstat(name string) (os.FileInfo, error) {
	if s.verbose {
		log.Println("Lstat ", name)
	}

	name, err := s.validate(name, false)
	if err != nil {
		return nil, err
	}

	return os.Lstat(name)
}

// Chmod is the same as os.Chmod only for a scope dir.
func (s *FileScope) Chmod(name string, mode os.FileMode) error {
	if s.verbose {
		log.Println("Remove ", name, " mode ", mode)
	}

	name, err := s.validate(name, true)
	if err != nil {
		return err
	}

	return os.Chmod(name, mode)
}

// Readlink is the same as os.Readlink
func (s *FileScope) Readlink(name string) (string, error) {
	if s.verbose {
		log.Println("Readlink ", name)
	}

	name, err := s.validate(name, false)
	if err != nil {
		return "", err
	}
	return os.Readlink(name)
}

// IOUtilReadDir is the same as ioutil.ReadDir
func (s *FileScope) IOUtilReadDir(dirname string) ([]os.FileInfo, error) {
	if s.verbose {
		log.Println("ioutil.ReadDir ", dirname)
	}

	dirname, err := s.validate(dirname, false)
	if err != nil {
		return nil, err
	}
	return ioutil.ReadDir(dirname)
}

// IOUtilReadFile is the same as ioutil.ReadFile
func (s *FileScope) IOUtilReadFile(filename string) ([]byte, error) {
	if s.verbose {
		log.Println("ioutil.ReadFile ", filename)
	}

	filename, err := s.validate(filename, false)
	if err != nil {
		return nil, err
	}

	return ioutil.ReadFile(filename)
}

// IOUtilWriteFile is the same as ioutil.WriteFile
func (s *FileScope) IOUtilWriteFile(filename string, data []byte, perm os.FileMode) error {
	if s.verbose {
		log.Println("ioutil.WriteFile ", filename, " data ", string(data), " perm ", perm)
	}

	filename, err := s.validate(filename, true)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filename, data, perm)
}

// IOUtilTempFile is the same as ioutil.TempFile
func (s *FileScope) IOUtilTempFile(dir, prefix string) (*os.File, error) {
	if s.verbose {
		log.Println("ioutil.TempFile ", dir, " prefix ", prefix)
	}

	if _, err := s.validate(dir, true); err != nil {
		return nil, err
	}

	return ioutil.TempFile(dir, prefix)
}

// GetOS returns the current OS string or "other" for unsupported ones.
func GetOS() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return "windows", nil
	case "linux":
		return "linux", nil
	case "darwin":
		return "mac", nil
	default:
		return "other", errors.New("OS not supported")
	}
}
