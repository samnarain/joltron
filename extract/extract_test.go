package extract

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gamejolt/joltron/test"
)

const (
	xzFile     = ".gj-bigTempFile.tar.xz"
	xzURL      = test.AWS + xzFile
	xzChecksum = "ca292a1cfa2d93f6e07feffa6d53e836"

	gzipFile     = ".gj-bigTempFile.tar.gz"
	gzipURL      = test.AWS + gzipFile
	gzipChecksum = "9c48bcb8e17b16e835b1b7c051ce4ef0"
)

func getFixtureOrDie(wg *sync.WaitGroup, url, checksum string) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := <-test.DownloadFixture(url, checksum); err != nil {
			panic(err.Error())
		}
	}()
}
func TestMain(m *testing.M) {

	// Parallelize fixture downloading
	wg := &sync.WaitGroup{}
	test.DoOrDie(test.DownloadFixture(xzFile, xzChecksum), wg)
	test.DoOrDie(test.DownloadFixture(gzipFile, gzipChecksum), wg)
	wg.Wait()

	os.Exit(m.Run())
}

func TestBenchmarkGzip(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	now := time.Now()
	benchmarkExtract(t, dir, gzipFile)
	delta := time.Now().Sub(now).Seconds()
	log.Printf("Delta extractor gzip: %f\n", delta)
}

func TestBenchmarkOsGzip(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	test.RequireFixture(t, gzipFile, dir, ".tempDownload")

	now := time.Now()

	for i := 0; i < 100; i++ {
		cmd := exec.Command("tar", "-xzf", filepath.Join(dir, ".tempDownload"), "--warning=none", "-C", dir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err.Error() + ":" + string(output))
		}

		test.OS.RemoveAll(dir)
		test.RequireFixture(t, gzipFile, dir, ".tempDownload")
	}

	delta := time.Now().Sub(now).Seconds()
	log.Printf("Delta os gzip: %f\n", delta)
}

func TestBenchmarkXz(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	now := time.Now()
	benchmarkExtract(t, dir, xzFile)
	delta := time.Now().Sub(now).Seconds()
	log.Printf("Delta extractor xz: %f\n", delta)
}

func TestBenchmarkOsXz(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	test.RequireFixture(t, xzFile, dir, ".tempDownload")

	now := time.Now()

	for i := 0; i < 100; i++ {
		cmd := exec.Command("tar", "-xJf", filepath.Join(dir, ".tempDownload"), "--warning=none", "-C", dir)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(err.Error() + ":" + string(output))
		}

		test.OS.RemoveAll(dir)
		test.RequireFixture(t, xzFile, dir, ".tempDownload")
	}

	delta := time.Now().Sub(now).Seconds()
	log.Printf("Delta os xz: %f\n", delta)
}

func benchmarkExtract(t *testing.T, dir, fixture string) {

	test.RequireFixture(t, fixture, dir, ".tempDownload")

	for i := 0; i < 100; i++ {
		extract, err := NewExtraction(nil, filepath.Join(dir, ".tempDownload"), dir, test.OS, nil)
		if err != nil {
			t.Fatal(err.Error())
		}
		<-extract.Done()
		if err := extract.Result().Err; err != nil {
			t.Fatal(err.Error())
		}

		test.OS.RemoveAll(dir)
		test.RequireFixture(t, fixture, dir, ".tempDownload")
	}
}
