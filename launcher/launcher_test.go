package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gamejolt/joltron/extract"
	"github.com/gamejolt/joltron/game"
	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/test"
)

var (
	gameFile      string
	gameChecksum  string
	launchOptions *data.LaunchOptions
)

func TestMain(m *testing.M) {
	switch runtime.GOOS {
	case "windows":
		gameFile = "eggnoggplus-win.tar.xz"
		gameChecksum = "6fd1c12545b7b10c5777dea227526915"
		launchOptions = &data.LaunchOptions{
			Executable: "./eggnoggplus-win/eggnoggplus.exe",
		}
	case "linux":
		gameFile = "eggnoggplus-linux-64.tar.xz"
		gameChecksum = "f9ea1d46976519c30cbaa77656be73d8"
		launchOptions = &data.LaunchOptions{
			Executable: "./eggnoggplus-linux/eggnoggplus",
		}
	case "mac":
		gameFile = "eggnoggplus-osx.tar.xz"
		gameChecksum = "bdf747dc64aadf1df152ca6bb0d934c2"
		launchOptions = &data.LaunchOptions{
			Executable: "././eggnoggplus.app",
		}
	}

	if err := <-test.DownloadFixture(gameFile, gameChecksum); err != nil {
		panic(err.Error())
	}

	os.Exit(m.Run())
}

func TestLaunch(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	test.RequireFixture(t, gameFile, dir, "temp")
	e, err := extract.NewExtraction(nil, filepath.Join(dir, "temp"), filepath.Join(dir, "data"), test.OS, nil)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-e.Done()
	if e.Result().Err != nil {
		t.Fatal(e.Result().Err.Error())
	}

	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data",
			GameUID: "game-v1",
		},
		IsFirstInstall: true,
		LaunchOptions:  launchOptions,
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal(err.Error())
	}

	_, err = NewLauncher(dir, []string{}, nil, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	<-time.After(30 * time.Second)
}
