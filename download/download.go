package download

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gamejolt/joltron/concurrency"
	"github.com/gamejolt/joltron/fs"
	OS "github.com/gamejolt/joltron/os"
	"github.com/gamejolt/joltron/stream"
)

// Download downloads a file from a url in a resumable fashion.
type Download struct {
	*concurrency.Resumable
	os OS.OS

	URL      string
	File     string
	Checksum string

	client   *http.Client
	result   error
	doneCh   chan bool
	finished bool

	localSize  int64
	remoteSize int64
	downloaded int64
}

// Finish comment
func (d *Download) Finish(result error) error {
	if d.finished {
		return d.result
	}
	d.result = result
	d.finished = true

	d.Cancel()
	close(d.doneCh)
	d.doneCh = nil

	return d.result
}

// Done returns a channel that closes when the download is done and cleaned up
func (d *Download) Done() <-chan struct{} {
	ch := make(chan struct{})

	go func() {
		if d.finished {
			close(ch)
		} else {
			<-d.doneCh
			close(ch)
		}
	}()

	return ch
}

// Result comment
func (d *Download) Result() error {
	return d.result
}

// ProgressCallback is a callback to report the download progress
type ProgressCallback func(downloaded, total int64, sample *stream.Sample)

// NewDownload creates a new download instance.
func NewDownload(resumable *concurrency.Resumable, url, file, checksum string, remoteSize int64, os2 OS.OS, progress ProgressCallback) *Download {
	noIdleTimeoutTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       0, // Override default transport to allow connections to idle forever for resumable support.
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if os2 == nil {
		os2, _ = OS.NewDefaultOS()
	}

	d := &Download{
		Resumable: concurrency.NewResumable(resumable),
		os:        os2,
		client: &http.Client{
			Transport: noIdleTimeoutTransport,
		},
		doneCh:     make(chan bool),
		URL:        url,
		File:       file,
		Checksum:   checksum,
		remoteSize: remoteSize,
	}

	log.Printf("Downloading from %s to %s\n", d.URL, d.File)
	go func() {
		result := concurrency.ChainResumableTasks(d,

			// Get local and remote file sizes
			func() error {
				localSize, remoteSize, err := d.GetFilesizes()
				if err != nil {
					return err
				}

				d.localSize, d.remoteSize = localSize, remoteSize
				return nil
			},

			// Validate filesizes, prepare for new download if necessary
			func() error {
				// Checks for invalid filesizes. Will remove target file if invalid.
				if d.remoteSize == 0 {
					return errors.New("Remote file is empty?")
				}

				// If local file size is larger than remote file size, something went wrong.
				// Most likely the file was changed either locally or in remote and has to be re-downloaded.
				if d.localSize > d.remoteSize {
					log.Printf("Local size (%d) is bigger than remote size (%d), attempting to remove local file and download again\n", d.localSize, d.remoteSize)
					if err := d.Cleanup(); err != nil {
						return err
					}
				}

				// If the filesize is exactly the same as the remote filesize the file is most likely downloaded successfully,
				// but we still compare the checksums to be safe
				if d.localSize == d.remoteSize {
					log.Println("Local and remote filesizes match, comparing checksums...")
					if err := d.ValidateChecksums(); err == nil {
						return d.Finish(nil)
					}

					log.Println("Attempting to remove local file and download again")
					if err := d.Cleanup(); err != nil {
						return err
					}

				} else if d.localSize > 0 {
					log.Println("Resuming download...")
				}
				return nil
			},

			// Download
			func() error {
				// Attempt to create the directory for the file if it doesn't exist
				if err := d.os.MkdirAll(filepath.Dir(d.File), 0755); err != nil {
					return err
				}

				// Open or create the file in append mode
				destFile, err := d.os.OpenFile(d.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					return err
				}
				defer destFile.Close()

				// Create a get request with the range header
				req, err := http.NewRequest("GET", d.URL, nil)
				if err != nil {
					return err
				}
				req.WithContext(d.Context())
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", d.localSize))

				// Send the request off
				res, err := d.client.Do(req)
				if err != nil {
					return err
				}

				// If the status code isn't 206 or didn't respond with a Content-Range header it means the server didn't accept our byte ranges.
				if (res.StatusCode != 200 && res.StatusCode != 206) || (res.StatusCode == 206 && res.Header.Get("Content-Range") == "") {
					return fmt.Errorf("Bad status code (%d) or empty Content-Range response header", res.StatusCode)
				}

				// Pipe the response to the file through the stream speed sampler.
				sampler, err := stream.NewSpeedSampler(res.Body, func(sampler *stream.SpeedSampler, sample *stream.Sample) {
					if progress != nil {
						progress(d.localSize+sampler.Total(), d.remoteSize, sample)
					}
				})
				if err != nil {
					return err
				}
				defer sampler.Close()

				// We pass in a resumable that will cancel immeidately when paused instead of pausing the stream,
				// because we don't want to keep the session alive forever. We'd rather resume by starting a new download process.
				// fs.ResumableCopy will work in chunks of 32k until the whole thing is downloaded.
				d.downloaded, err = fs.ResumableCopy(concurrency.NewInterruptible(d.Resumable), destFile, sampler, nil)
				if err != nil {
					return err
				}

				return nil
			}, func() error {
				// If after downloading the filesize doesnt match up to the remote file size, something is wrong.
				if d.localSize+d.downloaded != d.remoteSize {
					d.Cleanup()
					return fmt.Errorf("File size after downloading %d bytes is %d, but expecting %d", d.downloaded, d.localSize+d.downloaded, d.remoteSize)
				}

				// Check checksums
				log.Println("Download complete, comparing checksums...")
				err := d.ValidateChecksums()
				if err != nil {
					d.Cleanup()
				}

				return d.Finish(err)
			})
		d.Finish(<-result)
	}()

	return d
}

// GetFilesizes comment
func (d *Download) GetFilesizes() (int64, int64, error) {
	localSize, err := fs.Filesize(d.File)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, fmt.Errorf("Could not get size of local file %s", d.File)
	}

	// If the remote file size is provided, just use that
	if d.remoteSize != 0 {
		return localSize, d.remoteSize, nil
	}

	// Otherwise, attempt to get the remote file size
	req, err := http.NewRequest("HEAD", d.URL, nil)
	if err != nil {
		return localSize, 0, fmt.Errorf("Could not create request to get remote file size of %s", d.URL)
	}
	req.WithContext(d.Context())

	res, err := d.client.Do(req)
	if err != nil {
		return localSize, 0, fmt.Errorf("Could not get remote file size of %s - %s", d.URL, err.Error())
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return 0, 0, fmt.Errorf("Could not get remote file size of %s - Result status code is %d", d.URL, res.StatusCode)
	}

	if res.ContentLength <= 0 {
		return 0, 0, fmt.Errorf("Could not get remote file size of %s - ContentLength wasn't returned", d.URL)
	}

	return localSize, res.ContentLength, nil
}

// Cleanup comment
func (d *Download) Cleanup() error {
	if err := d.os.Remove(d.File); err != nil && !os.IsNotExist(err) {
		log.Println("Could not clean up temp download file")
		return err
	}
	d.localSize = 0
	return nil
}

// ValidateChecksums comment
func (d *Download) ValidateChecksums() error {

	// If we didn't get a checksum, we simply don't check
	if d.Checksum == "" {
		return nil
	}

	resultStr, err := fs.ResumableMD5File(d, d.File, d.os)
	if err != nil {
		return err
	}

	if resultStr != d.Checksum {
		err := fmt.Errorf("Invalid checksum. Expected %s, got %s", d.Checksum, resultStr)
		log.Println(err.Error())
		return err
	}
	return nil
}
