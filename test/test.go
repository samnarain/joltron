package test

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/gamejolt/joltron/download"
	"github.com/gamejolt/joltron/extract"
	_OS "github.com/gamejolt/joltron/os"
)

const (
	// Dir is the directory where tests are executed
	Dir = "test-dir"
	// AWS is the base url for the aws test bucket
	AWS = "https://s3-us-west-2.amazonaws.com/ylivay-gj-test-oregon/data/test-files/"
)

var (
	fixtureDir string
	nextID     uint32

	// OS is an os interface scoped to the test dir
	OS = createOS()
)

func createOS() _OS.OS {
	os2, err := _OS.NewFileScope(Dir, false)
	if err != nil {
		panic(err.Error())
	}

	os2.SetVerbose(false)
	return os2
}

func prepareFixtures() {
	if fixtureDir != "" {
		return
	}

	dir, err := filepath.Abs(filepath.Join(Dir, "Fixtures"))
	if err != nil {
		panic(fmt.Sprintf("Test dir could not be resolved: %s", err.Error()))
	}
	fixtureDir = dir
}

// DownloadFixture downloads a file to be required later by tests via copy.
func DownloadFixture(fixture, checksum string) chan error {
	prepareFixtures()

	ch := make(chan error)

	go func() {
		d1 := download.NewDownload(nil, AWS+fixture, filepath.Join(fixtureDir, fixture), checksum, 0, OS, nil)
		<-d1.Done()
		if d1.Result() != nil {
			ch <- fmt.Errorf("Failed to download fixtures: %s", d1.Result().Error())
		}
		close(ch)
	}()

	return ch
}

// RequireFixture copies a downloaded fixture by name into the expected dir and name.
func RequireFixture(t *testing.T, fixture, dir, name string) {
	prepareFixtures()

	log.Println(filepath.Join(fixtureDir, fixture))
	reader, err := os.Open(filepath.Join(fixtureDir, fixture))
	if err != nil {
		panic(fmt.Sprintf("Could not require fixture1: %s", err.Error()))
	}
	defer reader.Close()

	if err = os.MkdirAll(dir, 0755); err != nil {
		panic(fmt.Sprintf("Could not require fixture2: %s", err.Error()))
	}

	writer, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		panic(fmt.Sprintf("Could not require fixture3: %s", err.Error()))
	}
	defer writer.Close()

	_, err = io.Copy(writer, reader)
	if err != nil {
		panic(fmt.Sprintf("Could not require fixture4: %s", err.Error()))
	}
}

// RequireExtractedFixture works like RequireFixture, only it extracts it too.
func RequireExtractedFixture(t *testing.T, fixture, dir, archive, extractTo string) {
	RequireFixture(t, fixture, dir, archive)
	e, err := extract.NewExtraction(nil, filepath.Join(dir, archive), extractTo, OS, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-e.Done()
	if e.Result().Err != nil {
		t.Fatal(e.Result().Err.Error())
	}
}

// PrepareNextTest creates a new test dir and gets an unused port to use for the network stuff.
// This ensures that tests can be parallelized without conflicting.
func PrepareNextTest(t *testing.T) (port uint16, dir string) {
	// t.Parallel()

	port = GetNextPort()
	dir, err := filepath.Abs(filepath.Join(Dir, fmt.Sprintf("patch-%d", port)))
	if err != nil {
		panic(fmt.Sprintf("Test dir could not be resolved: %s", err.Error()))
	}
	if err = os.RemoveAll(dir); err != nil {
		panic(fmt.Sprintf("Test dir could not be removed: %s", err.Error()))
	}

	return port, dir
}

// GetNextPort gets and increments the next port. This allows a single test to use multiple ports without conflict.
func GetNextPort() (port uint16) {
	id := atomic.AddUint32(&nextID, 1)

	return 1024 + uint16(id)
}
