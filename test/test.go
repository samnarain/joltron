package test

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"sync"

	"github.com/gamejolt/joltron/download"
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

// DoOrDie takes an operation in the form of a channel that will send an error on failure, and will panic if it receives one.
// It can work in parallel by passing in a wait group. Pass in nil for no parallelism
func DoOrDie(ch <-chan error, wg *sync.WaitGroup) *sync.WaitGroup {

	// If no wait group is passed, make a new one.
	if wg == nil {
		wg = &sync.WaitGroup{}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := <-ch; err != nil {
			panic(err.Error())
		}
	}()

	return wg
}

// RequireFixture copies a downloaded fixture by name into the expected dir and name.
func RequireFixture(tb testing.TB, fixture, dir, name string) {
	prepareFixtures()

	log.Println(filepath.Join(fixtureDir, fixture))
	reader, err := os.Open(filepath.Join(fixtureDir, fixture))
	if err != nil {
		tb.Fatal(fmt.Sprintf("Could not require fixture1: %s", err.Error()))
	}
	defer reader.Close()

	if err = os.MkdirAll(dir, 0755); err != nil {
		tb.Fatal(fmt.Sprintf("Could not require fixture2: %s", err.Error()))
	}

	writer, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0755)
	if err != nil {
		tb.Fatal(fmt.Sprintf("Could not require fixture3: %s", err.Error()))
	}
	defer writer.Close()

	_, err = io.Copy(writer, reader)
	if err != nil {
		tb.Fatal(fmt.Sprintf("Could not require fixture4: %s", err.Error()))
	}
}

// PrepareNextTest creates a new test dir and gets an unused port to use for the network stuff.
// This ensures that tests can be parallelized without conflicting.
func PrepareNextTest(t *testing.T) (port uint16, dir string) {
	// t.Parallel()

	return prepareNextTest(t)
}

// PrepareNextBenchTest works just like PrepareNextTest only for benchmarks
func PrepareNextBenchTest(b *testing.B) (port uint16, dir string) {
	return prepareNextTest(b)
}

func prepareNextTest(tb testing.TB) (port uint16, dir string) {
	port = GetNextPort()
	dir, err := filepath.Abs(filepath.Join(Dir, fmt.Sprintf("patch-%d", port)))
	if err != nil {
		tb.Fatal(fmt.Sprintf("Test dir could not be resolved: %s", err.Error()))
	}
	if err = os.RemoveAll(dir); err != nil {
		tb.Fatal(fmt.Sprintf("Test dir could not be removed: %s", err.Error()))
	}

	return port, dir
}

// GetNextPort gets and increments the next port. This allows a single test to use multiple ports without conflict.
func GetNextPort() (port uint16) {
	id := atomic.AddUint32(&nextID, 1)

	return 1024 + uint16(id)
}
