package extract

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"math"

	"compress/gzip"

	"github.com/gamejolt/joltron/concurrency"
	"github.com/gamejolt/joltron/fs"
	OS "github.com/gamejolt/joltron/os"
	"github.com/gamejolt/joltron/stream"
	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"
)

// Extraction comment
type Extraction struct {
	*concurrency.Resumable
	os OS.OS

	Archive string
	Dir     string

	result   Result
	doneCh   chan bool
	finished bool
}

// Result comment
type Result struct {
	Err    error
	Result []string
}

// Finish comment
func (e *Extraction) Finish(result Result) Result {
	if e.finished {
		return e.result
	}
	e.result = result
	e.finished = true

	e.Cancel()
	close(e.doneCh)

	return e.result
}

// Done returns a channel that closes when the download is done and cleaned up
func (e *Extraction) Done() <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		if e.finished {
			close(ch)
		} else {
			<-e.doneCh
			close(ch)
		}
	}()

	return ch
}

// Result comment
func (e *Extraction) Result() Result {
	return e.result
}

// ProgressCallback is a callback to report the extraction progress
type ProgressCallback func(extracted, totalExtracted int64, sample *stream.Sample)

// NewExtraction comment
func NewExtraction(resumable *concurrency.Resumable, archive string, dir string, os2 OS.OS, progress ProgressCallback) (*Extraction, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("Extraction dir is not absolute")
	}

	if os2 == nil {
		tmp, err := OS.NewFileScope(dir, false)
		if err != nil {
			return nil, err
		}
		os2 = tmp
	}
	e := &Extraction{
		Resumable: concurrency.NewResumable(resumable),
		os:        os2,
		Archive:   archive,
		Dir:       dir,
		doneCh:    make(chan bool),
	}

	log.Printf("Extracting from %s to %s\n", e.Archive, e.Dir)
	go func() {
		if resume := <-e.Wait(); resume == concurrency.OpCancel {
			e.Finish(Result{context.Canceled, nil})
			return
		}
		e.Finish(e.extract(progress))
	}()

	return e, nil
}

func (e *Extraction) extract(progress ProgressCallback) Result {
	archiveFile, err := e.os.Open(e.Archive)
	if err != nil {
		return Result{err, nil}
	}
	defer archiveFile.Close()

	// Make a file reader with buffer size of 32kb to match the resumable copy buffer size.
	// This is to make pausing an extraction mid file more usable
	reader := bufio.NewReaderSize(archiveFile, 32*1024)

	archiveSize, err := fs.Filesize(e.Archive)
	if err != nil {
		return Result{err, nil}
	}

	var readerType string
	headerWriter := bytes.NewBuffer(make([]byte, 0, int(math.Max(lzma.HeaderLen, xz.HeaderLen))))
	if _, err = io.CopyN(headerWriter, reader, int64(headerWriter.Cap())); err != nil {
		return Result{err, nil}
	}

	headerBytes := headerWriter.Bytes()
	lzmaHeaderBytes := headerBytes[:lzma.HeaderLen]
	if lzma.ValidHeader(lzmaHeaderBytes) {
		readerType = "lzma"
	} else {
		xzHeaderBytes := headerBytes[:xz.HeaderLen]
		if xz.ValidHeader(xzHeaderBytes) {
			readerType = "xz"
		} else {
			if _, err = archiveFile.Seek(0, 0); err != nil {
				return Result{errors.New("Failed to seek the compressed file reader"), nil}
			}
			reader.Reset(archiveFile)

			if _, err := gzip.NewReader(reader); err == nil {
				readerType = "gz"
			} else {
				return Result{errors.New("Invalid or unsupported compression format"), nil}
			}
		}
	}

	if _, err = archiveFile.Seek(0, 0); err != nil {
		return Result{errors.New("Failed to seek the compressed file reader"), nil}
	}
	reader.Reset(archiveFile)

	sampler, err := stream.NewSpeedSampler(reader, func(sampler *stream.SpeedSampler, sample *stream.Sample) {
		if progress != nil {
			progress(sampler.Total(), archiveSize, sample)
		}
	})
	if err != nil {
		return Result{Err: err}
	}
	defer sampler.Close()

	var compressionReader io.Reader
	if readerType == "lzma" {
		compressionReader, err = lzma.ReaderConfig{DictCap: lzma.MinDictCap}.NewReader(sampler)
	} else if readerType == "xz" {
		compressionReader, err = xz.NewReader(sampler)
	} else {
		compressionReader, err = gzip.NewReader(sampler)
	}

	if err != nil {
		return Result{err, nil}
	}

	tarReader := tar.NewReader(compressionReader)

	var extracted = make([]string, 0, 64)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return Result{err, nil}
		}

		path := filepath.Join(e.Dir, header.Name)
		if !filepath.IsAbs(path) || !strings.HasPrefix(path, e.Dir) {
			return Result{fmt.Errorf("Attempted to extract a file outside the game dir: %s", path), nil}
		}
		info := header.FileInfo()
		if info.IsDir() {
			if err = e.os.MkdirAll(path, info.Mode()); err != nil {
				return Result{err, nil}
			}
			continue
		}

		dir := filepath.Dir(path)
		if err = e.os.MkdirAll(dir, 0755); err != nil {
			return Result{err, nil}
		}

		if err := e.os.Remove(path); err != nil && !os.IsNotExist(err) {
			return Result{err, nil}
		}
		extracted = append(extracted, header.Name)
		if info.Mode()&os.ModeSymlink != 0 {
			if err = e.os.Symlink(filepath.Join(dir, filepath.FromSlash(header.Linkname)), path); err != nil {
				return Result{err, nil}
			}
			continue
		}

		log.Println("Extracting file: " + path)
		file, err := e.os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return Result{err, nil}
		}
		_, err = fs.ResumableCopy(e, file, tarReader, nil)
		file.Close()
		if err != nil {
			return Result{err, nil}
		}
	}

	return Result{nil, extracted}
}
