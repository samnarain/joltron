package fs

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"path/filepath"

	"strings"

	"github.com/gamejolt/joltron/concurrency"
	OS "github.com/gamejolt/joltron/os"
)

// Filesize returns the filesize of a given file
// Just a utility function to make sure with get the actual filesize and not 0 for symlinks
func Filesize(path string) (int64, error) {
	finfo, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if finfo.IsDir() {
		return 0, fmt.Errorf("Not computing filesize for directory %s", path)
	}

	return finfo.Size(), nil
}

// IsInDir checks if a path is in a given directory.
func IsInDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator))
}

// EnsureFolder checks if a folder exists, and if not attempts to create it.
func EnsureFolder(dir string, os2 OS.OS) error {
	if os2 == nil {
		tmp, err := OS.NewDefaultOS()
		if err != nil {
			return err
		}
		os2 = tmp
	}

	stat, err := os2.Lstat(dir)
	if err == nil {
		if !stat.IsDir() {
			return errors.New("Directory is not a valid directory")
		}
		return nil
	}

	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err = os2.MkdirAll(dir, 0777); err != nil {
		return err
	}

	return nil
}

// FilenamesDiff finds the files in arr1 that do not exist in arr2. The comparison's case sensitivity depends on the OS.
func FilenamesDiff(arr1, arr2 []string) []string {
	result := make([]string, 0, 64)
	caseSensitive := runtime.GOOS == "linux"
	var compare func(string, string) bool
	if caseSensitive {
		compare = func(a, b string) bool {
			return a == strings.TrimPrefix(b, "./")
		}
	} else {
		compare = func(a, b string) bool {
			// We only need to transform b because a is already transformed in the loop
			return a == strings.TrimPrefix(strings.ToLower(b), "./")
		}
	}

	for _, a1 := range arr1 {
		var compareA1 string
		if caseSensitive {
			compareA1 = a1
		} else {
			compareA1 = strings.ToLower(a1)
		}
		compareA1 = strings.TrimPrefix(compareA1, "./")

		found := false
		for _, a2 := range arr2 {
			if compare(compareA1, a2) {
				found = true
				break
			}
		}

		if !found {
			result = append(result, a1)
		}
	}
	return result
}

// ResumableMD5File calculates the md5 of a file in a resumable way
func ResumableMD5File(r concurrency.Resumer, path string, os2 OS.OS) (string, error) {
	if os2 == nil {
		tmp, err := OS.NewDefaultOS()
		if err != nil {
			return "", err
		}
		os2 = tmp
	}

	file, err := os2.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := ResumableCopy(r, hash, file, nil); err != nil {
		return "", err
	}

	var result []byte
	result = hash.Sum(result)
	return hex.EncodeToString(result), nil
}

// ResumableCopy is a modification of io.copyBuffer.
// It works in chunks of 32kb and checks the resumable state before continuing or abandoning the copy.
func ResumableCopy(r concurrency.Resumer, dst io.Writer, src io.Reader, buf []byte) (written int64, err error) {
	if buf == nil {
		buf = make([]byte, 32*1024)
	}

	for {
		if resume := <-r.Wait(); resume == concurrency.OpCancel {
			return written, context.Canceled
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			err = er
			break
		}
	}
	return written, err
}

type symlink struct {
	path   string
	target string
}

// ResumableCopyDir copies a directory with resumable support.
// It attempts to copy files preserving permissions.
// Symlinks that point to files in the src directory gets re-created pointing to the files in the dst directory.
// This errors out if attempting to overwrite a directory with a file while yolo is false
func ResumableCopyDir(r concurrency.Resumer, dst, src string, yolo bool, os2 OS.OS) ([]string, error) {
	files := make([]string, 0, 32)

	// Since filepath.Walk returns files in alphabetical order, the directories are always listed before the files they contain,
	// so we don't have to ensure they exist by the time the files are handled. If this changes, uncommit the following code block to ensure directories
	// ensuredDirs := make(map[string]bool)

	// Make sure we're working with sane looking paths.
	// NOTE: the resulting copied file list will be incomparable to the input file list if the given src dir path isn't clean
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if IsInDir(dst, src) {
		return nil, fmt.Errorf("Copying a dir to a subdir of itself is not supported (%s => %s)", src, dst)
	}

	deferredSymlinks := make([]symlink, 0, 32)
	err := filepath.Walk(src, func(path string, finfo os.FileInfo, err error) error {
		log.Println("Path: ", path)
		if err != nil {
			return err
		}

		// If we're paused wait for resume or abort on cancel
		if resume := <-r.Wait(); resume == concurrency.OpCancel {
			return context.Canceled
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, rel)

		// If the src file is a directory, we create a directory in the destination (and all directories in the way)
		if finfo.IsDir() {
			log.Println("Making dirs to ", dstPath)
			if err = os2.MkdirAll(dstPath, finfo.Mode()); err != nil {
				return err
			}
			return nil
		}

		// Since filepath.Walk returns files in alphabetical order, the directories are always listed before the files they contain,
		// so we don't have to ensure they exist by the time the files are handled. If this changes, uncommit the following code block to ensure directories
		// // Make sure the directory for the file exists.
		// dir := filepath.Dir(dstPath)
		// if val, ok := ensuredDirs[dir]; !val || !ok {
		// 	log.Println("Ensuring dirs up to ", dir)
		// 	// if err = os2.MkdirAll(dir, 0755); err != nil {
		// 	// 	return err
		// 	// }
		// 	ensuredDirs[dir] = true
		// }

		dstStat, err := os2.Stat(dstPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			dstStat = nil
		} else if !dstStat.Mode().IsRegular() {
			return fmt.Errorf("Cannot overwrite non-regular file %s with file %s", dstPath, path)
		}

		// If the file is a symlink, we need to re-create it.
		// Get the symlink target.
		if IsLink(path, os2) {
			// Get the symlink target.
			linkTarget, err := GetLinkTarget(path, os2)
			if err != nil {
				return err
			}

			// If the symlink is in the source directory, we need to create the new link pointing to the equivalent file in the dest directory.
			// /path/to/src/link -> /path/to/src/dir/target => /path/to/dst/link -> /path/to/dst/dir/target
			if strings.HasPrefix(linkTarget, src+string(os.PathSeparator)) {
				linkTargetRel, err := filepath.Rel(src, linkTarget)
				if err != nil {
					return err
				}

				linkTarget = filepath.Join(dst, linkTargetRel)

				// The file at the dest directory might not exist yet if it hasn't been copied over yet.
				// In this case, we defer creating the symlink for later.
				deferredSymlinks = append(deferredSymlinks, symlink{
					path:   dstPath,
					target: linkTarget,
				})

				log.Println("Deferring symlink creation for ", dstPath, " => ", linkTarget)
				return nil
			}

			log.Println("Creating symlink for ", dstPath, " => ", linkTarget)
			if err = CreateLink(linkTarget, dstPath, os2); err != nil {
				return err
			}
		} else if finfo.Mode().IsRegular() {

			if dstStat != nil {
				if dstStat.IsDir() {
					// In strict mode, we don't allow overwriting a directory with a file because we don't want to lose any files that is not explicitly overwritten by another file in the source dir.
					if !yolo {
						return fmt.Errorf("Cannnot overwrite directory %s with file %s", dstPath, path)
					}

					// YOLO
					log.Println("Removing all ", dstPath)
					if err := os2.RemoveAll(dstPath); err != nil && !os.IsNotExist(err) {
						return err
					}
					dstStat = nil
				} else if !os.SameFile(finfo, dstStat) {

					// If the source and destination are the same file we can skip the copy.
					// Otherwise we need to remove the destination before overwriting it because we had some weird edge cases when truncating existing files.
					log.Println("Removing ", dstPath)
					if err := os2.Remove(dstPath); err != nil && !os.IsNotExist(err) {
						return err
					}
					dstStat = nil
				} else {
					log.Println("Equal files: ", path, " and ", dstPath, " - not copying")
				}
			}

			// We can only copy if the destination doesn't already exist.
			if dstStat == nil {

				// Simple copy using read stream from src to a write stream to dst using the same
				log.Println("Copying from ", path, " to ", dstPath)
				srcFile, err := os2.Open(path)
				if err != nil {
					return err
				}
				defer srcFile.Close()

				dstFile, err := os2.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, finfo.Mode())
				if err != nil {
					return err
				}
				defer dstFile.Close()

				_, err = ResumableCopy(r, dstFile, srcFile, nil)
				if err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("Cannot copy non-regular file %s", path)
		}

		files = append(files, path)
		return nil
	})

	if err == nil {
		// Time to create the deferred symlinks
		for _, deferredSymlink := range deferredSymlinks {
			log.Println("Creating deferred symlink for ", deferredSymlink.path, " => ", deferredSymlink.target)
			if err = CreateLink(deferredSymlink.target, deferredSymlink.path, os2); err != nil {
				return files, err
			}
			files = append(files, deferredSymlink.path)
		}
	}

	return files, err
}
