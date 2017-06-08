package patcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gamejolt/joltron/concurrency"
	"github.com/gamejolt/joltron/game"
	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/launcher"
	"github.com/gamejolt/joltron/test"
)

const (
	bigDownloadFile     = ".gj-bigTempFile.tar.xz"
	bigDownloadURL      = test.AWS + bigDownloadFile
	bigDownloadChecksum = "ca292a1cfa2d93f6e07feffa6d53e836"

	patch1File     = ".gj-testPatcher1.tar.xz"
	patch1Url      = test.AWS + patch1File
	patch1Checksum = "f530baefafcc5e2158b3d680b60df62d"

	patch2File     = ".gj-testPatcher2.tar.xz"
	patch2Url      = test.AWS + patch2File
	patch2Checksum = "a49fbb734965fb92ffbf990e7cfa9c14"

	build1File     = ".gj-testDiff-Build1-clean.tar.xz"
	build1Checksum = "039652f51d6cf49328bffa19f2d18743"

	currentFile     = ".gj-testDiff-Build1.tar.xz"
	currentChecksum = "bb008c0036e7aaa2781417c995c45ad2"

	diffFile     = ".gj-testDiff-Diff.tar.xz"
	diffChecksum = "c81516f083d574f166618c6cbc123305"
)

var (
	oldMetadata  data.BuildMetadata
	newMetadata  data.BuildMetadata
	diffMetadata data.DiffMetadata
)

func getUpdateMetadata(version, url, checksum string, remoteSize int64, sideBySide bool) *data.UpdateMetadata {
	updateMetadata := &data.UpdateMetadata{
		GameUID:    "game-" + version,
		URL:        url,
		Checksum:   checksum,
		RemoteSize: remoteSize,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Executable: "./",
		SideBySide: &sideBySide,
	}

	if *updateMetadata.SideBySide == true {
		updateMetadata.DataDir = fmt.Sprintf("data-%s", updateMetadata.GameUID)
	} else {
		updateMetadata.DataDir = "data"
	}

	return updateMetadata
}

func TestMain(m *testing.M) {
	if err := <-test.DownloadFixture(bigDownloadFile, bigDownloadChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(patch1File, patch1Checksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(patch2File, patch2Checksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(build1File, build1Checksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(currentFile, currentChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(diffFile, diffChecksum); err != nil {
		panic(err.Error())
	}

	oldMetadata = data.BuildMetadata{
		Files: map[string]data.FileMetadata{
			"assets/icon/game-icon (identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "35afb487d7a128c9f3006ab1b2ae8e6a",
			},
			"assets/maps/map1 (deleted).txt": data.FileMetadata{
				Size:     259,
				Checksum: "49136019440a92076f106396cf87d010",
			},
			"assets/sounds/jump (2 similar).txt": data.FileMetadata{
				Size:     259,
				Checksum: "f8929a0ae50c3561a9be1de4da9d6e74",
			},
			"assets/sprites/enemy (renamed).txt": data.FileMetadata{
				Size:     259,
				Checksum: "b08c52291b88b33911d82569588ecb0f",
			},
			"assets/sprites/player (identical).txt": data.FileMetadata{
				Size:     260,
				Checksum: "cc369cef611012ff4b40664d9d14f7a5",
			},
			"framework/current/license_mit (2 identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "32ae4d79f8ef9233f90e8f8a76ac217a",
			},
			"framework/current/runner (similar).txt": data.FileMetadata{
				Size:     259,
				Checksum: "718999ce58d76eeeb998730c3b6a8cc8",
			},
			"framework/v1/license_mit (2 identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "32ae4d79f8ef9233f90e8f8a76ac217a",
			},
			"framework/v1/runner (similar).txt": data.FileMetadata{
				Size:     259,
				Checksum: "718999ce58d76eeeb998730c3b6a8cc8",
			},
			"settings (invalid).txt": data.FileMetadata{
				Size:     259,
				Checksum: "5b12b816453874500c8fbc515609e9f7",
			},
		},
		Dirs: []string{"assets", "assets/icon", "assets/maps", "assets/sounds", "assets/sprites", "framework", "framework/v1"},
		Symlinks: map[string]string{
			"framework/current":                       "v1/",
			"game (indirect)":                         "framework/current/runner (similar).txt",
			"icon (unchanged)":                        "assets/icon/game-icon (identical).txt",
			"license_mit (2 identical) (removed).txt": "framework/v1/license_mit (2 identical).txt",
		},
	}

	newMetadata = data.BuildMetadata{
		Files: map[string]data.FileMetadata{
			"assets/backgrounds/forest (created).txt": data.FileMetadata{
				Size:     259,
				Checksum: "341750ed999d0409493b3e39c054a76e",
			},
			"assets/icon/game-icon (identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "35afb487d7a128c9f3006ab1b2ae8e6a",
			},
			"assets/sounds/jump1 (2 similar).txt": data.FileMetadata{
				Size:     268,
				Checksum: "1391fc6089d2c7b49d4e023319dbde09",
			},
			"assets/sounds/jump2 (2 similar).txt": data.FileMetadata{
				Size:     268,
				Checksum: "4d80eb06f7528743d6c73c311ae8b371",
			},
			"assets/sprites/ogre (created).txt": data.FileMetadata{
				Size:     259,
				Checksum: "c12294dd3ed128ca486ee4ceec62d54d",
			},
			"assets/sprites/player (identical).txt": data.FileMetadata{
				Size:     260,
				Checksum: "cc369cef611012ff4b40664d9d14f7a5",
			},
			"assets/sprites/slime (renamed).txt": data.FileMetadata{
				Size:     259,
				Checksum: "b08c52291b88b33911d82569588ecb0f",
			},
			"framework/current/license_mit (2 identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "32ae4d79f8ef9233f90e8f8a76ac217a",
			},
			"framework/current/runner (similar).txt": data.FileMetadata{
				Size:     267,
				Checksum: "4addc5bec539ad76738585193783879a",
			},
			"framework/v2/license_mit (2 identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "32ae4d79f8ef9233f90e8f8a76ac217a",
			},
			"framework/v2/runner (similar).txt": data.FileMetadata{
				Size:     267,
				Checksum: "4addc5bec539ad76738585193783879a",
			},
			"license_mit (2 identical).txt": data.FileMetadata{
				Size:     259,
				Checksum: "32ae4d79f8ef9233f90e8f8a76ac217a",
			},
			"metadata (invalid).txt": data.FileMetadata{
				Size:     259,
				Checksum: "2ef852c4936c7d2feac1bbbcd38338d8",
			},
			"settings (invalid).txt/settings (invalid).txt": data.FileMetadata{
				Size:     268,
				Checksum: "958341cbfcfdf1b672cc995a5cb3abd3",
			},
		},
		Dirs: []string{"assets", "assets/backgrounds", "assets/icon", "assets/sounds", "assets/sprites", "framework", "framework/v2", "settings (invalid).txt"},
		Symlinks: map[string]string{
			"framework/current": "v2/",
			"game (indirect)":   "framework/current/runner (similar).txt",
			"icon (unchanged)":  "assets/icon/game-icon (identical).txt",
		},
	}

	diffMetadata = data.DiffMetadata{
		Identical: map[string][]string{
			"assets/icon/game-icon (identical).txt":      []string{"assets/icon/game-icon (identical).txt"},
			"assets/sprites/enemy (renamed).txt":         []string{"assets/sprites/slime (renamed).txt"},
			"assets/sprites/player (identical).txt":      []string{"assets/sprites/player (identical).txt"},
			"framework/v1/license_mit (2 identical).txt": []string{"framework/v2/license_mit (2 identical).txt", "license_mit (2 identical).txt"},
		},
		Similar: map[string][]data.SimilarFile{
			"assets/sounds/jump (2 similar).txt": []data.SimilarFile{
				data.SimilarFile{
					ChunkSize: 51200000,
					New:       "assets/sounds/jump1 (2 similar).txt",
					Size:      268,
					Patches: []data.SimilarFilePart{
						data.SimilarFilePart{
							File:     "assets/sounds/jump1 (2 similar).txt",
							Size:     169,
							Checksum: "f622d518c1a4fa27e9f1a32d667d359e",
						},
					},
					Tails:    []data.SimilarFilePart{},
					DiffSize: 169,
				},
				data.SimilarFile{
					ChunkSize: 51200000,
					New:       "assets/sounds/jump2 (2 similar).txt",
					Size:      268,
					Patches: []data.SimilarFilePart{
						data.SimilarFilePart{
							File:     "assets/sounds/jump2 (2 similar).txt",
							Size:     169,
							Checksum: "77c98073e183101706dbb60af74aa335",
						},
					},
					Tails:    []data.SimilarFilePart{},
					DiffSize: 169,
				},
			},
			"framework/v1/runner (similar).txt": []data.SimilarFile{
				data.SimilarFile{
					ChunkSize: 51200000,
					New:       "framework/v2/runner (similar).txt",
					Size:      267,
					Patches: []data.SimilarFilePart{
						data.SimilarFilePart{
							File:     "framework/v2/runner (similar).txt",
							Size:     164,
							Checksum: "6a7116149401e7d9c99219c4de2d4b13",
						},
					},
					Tails:    []data.SimilarFilePart{},
					DiffSize: 164,
				},
			},
			"settings (invalid).txt": []data.SimilarFile{
				data.SimilarFile{
					ChunkSize: 51200000,
					New:       "settings (invalid).txt/settings (invalid).txt",
					Size:      268,
					Patches: []data.SimilarFilePart{
						data.SimilarFilePart{
							File:     "settings (invalid).txt/settings (invalid).txt",
							Size:     168,
							Checksum: "f0945928560bebbf5785046a42b81220",
						},
					},
					Tails:    []data.SimilarFilePart{},
					DiffSize: 168,
				},
			},
		},
		Created: []string{"assets/backgrounds/forest (created).txt", "assets/sprites/ogre (created).txt", "metadata (invalid).txt"},
		Removed: []string{"assets/maps/map1 (deleted).txt"},
		Dirs:    []string{"assets", "assets/backgrounds", "assets/icon", "assets/sounds", "assets/sprites", "framework", "framework/v2", "settings (invalid).txt"},
		Symlinks: map[string]string{
			"framework/current": "v2/",
			"game (indirect)":   "framework/current/runner (similar).txt",
			"icon (unchanged)":  "assets/icon/game-icon (identical).txt",
		},
	}

	os.Exit(m.Run())
}

func TestPatchDiffExistingSameDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", test.AWS+build1File, build1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", test.AWS+diffFile, diffChecksum, 0, false)
	updateMetadata.OldBuildMetadata, updateMetadata.NewBuildMetadata, updateMetadata.DiffMetadata = &oldMetadata, &newMetadata, &diffMetadata
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}
}

func TestPatchFirstSameDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	dataDir := filepath.Join(dir, "data")
	if patch.newDataDir != dataDir {
		t.Fatalf("Expecting patch.newDataDir to be:\n%s\nreceived:\n%s\n", dataDir, patch.newDataDir)
	}

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingSameDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, false)
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingSameDirSameBuild(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v1", patch2Url, patch2Checksum, 0, false)
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, true)
}

func TestPatchUninstallSameDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}

	test.GetNextPort()
	patch, err = NewUninstall(nil, dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}

	assertFileNotExist(t, patch.dataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchFirstBuildDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	dataDir := filepath.Join(dir, fmt.Sprintf("data-%s", updateMetadata.GameUID))
	if patch.newDataDir != dataDir {
		t.Fatalf("Expecting patch.newDataDir to be:\n%s\nreceived:\n%s\n", dataDir, patch.newDataDir)
	}

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingBuildDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, true)
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
	assertFileNotExist(t, patch.dataDir, ".")
}

func TestPatchExistingSameBuildDirSameBuild(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v1", patch2Url, patch2Checksum, 0, true)
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, true)
}

func TestPatchUninstallBuildDir(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}

	test.GetNextPort()
	patch, err = NewUninstall(nil, dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}

	assertFileNotExist(t, patch.dataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchRelocateSameBuild(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	//TODO this unit test might be wrong, do we need to use patch 2 or patch 1 here?
	// the original unit test attempted to upgrade to patch 2 but assert the first patch state.
	// is it because the game uid is the exact same so it doesnt attempt doing an update?
	updateMetadata = getUpdateMetadata("v1", patch2Url, patch2Checksum, 0, true)
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, true)
}

func TestPatchFirstRecoverCrashDuringDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)

	// Mock the update dir to look like it crashed during first installation at download
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
			IsDirty: false,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchFirstRecoverCrashDuringExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)

	// Mock the update dir to look like it crashed during first installation at extract
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:          "data",
			GameUID:      updateMetadata.GameUID,
			DynamicFiles: []string{},
			IsDirty:      true,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Mock extracting some files
	assertMkdirAll(t, filepath.Join(dir, "data"))
	assertWriteFile(t, filepath.Join(dir, "data"), "./fToPreserve", "")
	assertWriteFile(t, filepath.Join(dir, "data"), "./fToUpdate", "test\n")

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchCancelFirstAfterRecoverCrashDuringDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)

	// Mock the update dir to look like it crashed during first installation at download
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
			IsDirty: false,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	assertPatchStateTransition(t, patch, StateDownload, 3*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchCancelFirstAfterRecoverCrashDuringExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)

	// Mock the update dir to look like it crashed during first installation at extract
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data",
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:          "data",
			GameUID:      updateMetadata.GameUID,
			DynamicFiles: []string{},
			IsDirty:      true,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Mock extracting some files
	assertMkdirAll(t, filepath.Join(dir, "data"))
	assertWriteFile(t, filepath.Join(dir, "data"), "./fToPreserve", "")
	assertWriteFile(t, filepath.Join(dir, "data"), "./fToUpdate", "test\n")

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	assertPatchStateTransition(t, patch, StateExtract, 3*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchExistingRecoverCrashDuringDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	// Mock the update dir to look like it crashed during an update to the second build at download
	test.RequireFixture(t, patch2File, dir, ".tempDownload")
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, false)
	manifest.PatchInfo = &data.PatchInfo{
		Dir:     manifest.Info.Dir,
		GameUID: updateMetadata.GameUID,
		IsDirty: false,

		LaunchOptions: &data.LaunchOptions{
			OS:         updateMetadata.OS,
			Arch:       updateMetadata.Arch,
			Executable: updateMetadata.Executable,
		},
		DownloadSize:     updateMetadata.RemoteSize,
		DownloadChecksum: updateMetadata.Checksum,
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	test.GetNextPort()
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingRecoverCrashDuringExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	// Mock the update dir to look like it crashed during an update to the second build at extract
	test.RequireFixture(t, patch2File, dir, ".tempDownload")
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, false)
	manifest.PatchInfo = &data.PatchInfo{
		Dir:          manifest.Info.Dir,
		GameUID:      updateMetadata.GameUID,
		DynamicFiles: []string{"./fDynamic", "./fDynamicCase"},
		IsDirty:      true,

		LaunchOptions: &data.LaunchOptions{
			OS:         updateMetadata.OS,
			Arch:       updateMetadata.Arch,
			Executable: updateMetadata.Executable,
		},
		DownloadSize:     updateMetadata.RemoteSize,
		DownloadChecksum: updateMetadata.Checksum,
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Mock extracting some files
	assertMkdirAll(t, filepath.Join(dir, "data"))
	assertWriteFile(t, filepath.Join(dir, "data"), "./fToUpdate", "update\n")

	test.GetNextPort()
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchCancelFirstBuildDirAfterRecoverCrashDuringDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)

	// Mock the update dir to look like it crashed during first installation at download
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data-" + updateMetadata.GameUID,
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:     "data-" + updateMetadata.GameUID,
			GameUID: updateMetadata.GameUID,
			IsDirty: false,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	assertPatchStateTransition(t, patch, StateDownload, 3*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchCancelFirstBuildDirAfterRecoverCrashDuringExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)

	// Mock the update dir to look like it crashed during first installation at extract
	test.RequireFixture(t, patch1File, dir, ".tempDownload")
	launchOptions := &data.LaunchOptions{
		OS:         updateMetadata.OS,
		Arch:       updateMetadata.Arch,
		Executable: updateMetadata.Executable,
	}
	manifest := &data.Manifest{
		Info: &data.Info{
			Dir:     "data-" + updateMetadata.GameUID,
			GameUID: updateMetadata.GameUID,
		},
		LaunchOptions:  launchOptions,
		IsFirstInstall: true,
		PatchInfo: &data.PatchInfo{
			Dir:          "data-" + updateMetadata.GameUID,
			GameUID:      updateMetadata.GameUID,
			DynamicFiles: []string{},
			IsDirty:      true,

			LaunchOptions:    launchOptions,
			DownloadSize:     updateMetadata.RemoteSize,
			DownloadChecksum: updateMetadata.Checksum,
		},
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Mock extracting some files
	assertMkdirAll(t, filepath.Join(dir, "data-"+updateMetadata.GameUID))
	assertWriteFile(t, filepath.Join(dir, "data-"+updateMetadata.GameUID), "./fToPreserve", "")
	assertWriteFile(t, filepath.Join(dir, "data-"+updateMetadata.GameUID), "./fToUpdate", "test\n")

	// Attempt to patch normally
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	assertPatchStateTransition(t, patch, StateExtract, 3*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	log.Println(patch.dataDir)
	log.Println(patch.newDataDir)
	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchExistingBuildDirRecoverCrashDuringDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	// Mock the update dir to look like it crashed during an update to the second build at download
	test.RequireFixture(t, patch2File, dir, ".tempDownload")
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, true)
	manifest.PatchInfo = &data.PatchInfo{
		Dir:     "data-" + updateMetadata.GameUID,
		GameUID: updateMetadata.GameUID,
		IsDirty: false,

		LaunchOptions: &data.LaunchOptions{
			OS:         updateMetadata.OS,
			Arch:       updateMetadata.Arch,
			Executable: updateMetadata.Executable,
		},
		DownloadSize:     updateMetadata.RemoteSize,
		DownloadChecksum: updateMetadata.Checksum,
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	test.GetNextPort()
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingBuildDirRecoverCrashDuringExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	// Mock the update dir to look like it crashed during an update to the second build at extract
	test.RequireFixture(t, patch2File, dir, ".tempDownload")
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, true)
	manifest.PatchInfo = &data.PatchInfo{
		Dir:          "data-" + updateMetadata.GameUID,
		GameUID:      updateMetadata.GameUID,
		DynamicFiles: []string{"./fDynamic", "./fDynamicCase"},
		IsDirty:      true,

		LaunchOptions: &data.LaunchOptions{
			OS:         updateMetadata.OS,
			Arch:       updateMetadata.Arch,
			Executable: updateMetadata.Executable,
		},
		DownloadSize:     updateMetadata.RemoteSize,
		DownloadChecksum: updateMetadata.Checksum,
	}
	if err := game.WriteManifest(manifest, dir, test.OS); err != nil {
		t.Fatal("Failed to mock manifest: ", err.Error())
	}

	// Mock extracting some files
	assertMkdirAll(t, filepath.Join(dir, "data-"+updateMetadata.GameUID))
	assertWriteFile(t, filepath.Join(dir, "data-"+updateMetadata.GameUID), "./fToUpdate", "update\n")

	test.GetNextPort()
	patch, err = NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchFirstManualSameDirWithoutChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, true, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should reach the extract phase eventually
	assertPatchStateTransition(t, patch, StateExtract, 10*time.Second)

	<-patch.Done()

	dataDir := filepath.Join(dir, "data")
	if patch.newDataDir != dataDir {
		t.Fatalf("Expecting patch.newDataDir to be:\n%s\nreceived:\n%s\n", dataDir, patch.newDataDir)
	}

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchFirstManualBuildDirWithoutChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, true, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should eventually finish the patch in a reasonable time
	select {
	case <-patch.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("Expecting patch to finish. It may be stuck waiting for a signal from a child that will never arive")
	}

	dataDir := filepath.Join(dir, "data-"+updateMetadata.GameUID)
	if patch.newDataDir != dataDir {
		t.Fatalf("Expecting patch.newDataDir to be:\n%s\nreceived:\n%s\n", dataDir, patch.newDataDir)
	}

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingManualSameDirWithoutChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, false)
	patch, err = NewInstall(nil, dir, true, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should reach the extract phase eventually
	assertPatchStateTransition(t, patch, StateExtract, 10*time.Second)

	<-patch.Done()

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingManualBuildDirWithoutChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, true)
	patch, err = NewInstall(nil, dir, true, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should eventually finish the patch in a reasonable time
	select {
	case <-patch.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("Expecting patch to finish. It may be stuck waiting for a signal from a child that will never arive")
	}

	assertSecondPatchState(t, updateMetadata, patch, false)
	assertFileNotExist(t, patch.dataDir, ".")
}

func TestPatchExistingManualSameDirWithChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata = getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, false)

	// Creating and tracking a mock launcher would make the installation halt right before extraction
	mockLauncher := &launcher.Launcher{}
	launcher.TrackInstance(mockLauncher)

	patch, err = NewInstall(nil, dir, true, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should stop the installation waiting for launched children
	assertPatchStateTransition(t, patch, StateUpdateReady, 10*time.Second)
	// Make sure it stays in that state and doesn't transition away
	assertPatchStateNoTransition(t, patch, StateUpdateReady, 5*time.Second)

	// Killing the mock launcher should remove it from track list, allowing patch to detect it and continue patching.
	mockLauncher.Kill()

	// It should eventually finish the patch in a reasonable time
	select {
	case <-patch.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("Expecting patch to finish. It may be stuck waiting for a signal from a child that will never arive")
	}

	assertSecondPatchState(t, updateMetadata, patch, false)
}

func TestPatchExistingManualBuildDirWithChildren(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, true)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)

	dataDir := patch.newDataDir
	assertWriteFile(t, dataDir, "./fDynamic", "test\n")
	assertWriteFile(t, dataDir, "./fDynamicCase", "test\n")

	test.GetNextPort()
	updateMetadata2 := getUpdateMetadata("v2", patch2Url, patch2Checksum, 0, true)

	// Creating and tracking a mock launcher would make the installation halt right before extraction.
	mockLauncher := &launcher.Launcher{}
	launcher.TrackInstance(mockLauncher)

	patch, err = NewInstall(nil, dir, true, nil, updateMetadata2, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// It should stop the installation waiting for launched children.
	assertPatchStateTransition(t, patch, StateUpdateReady, 10*time.Second)
	// Make sure it stays in that state and doesn't transition away.
	assertPatchStateNoTransition(t, patch, StateUpdateReady, 5*time.Second)

	// When patching in a build dir the patcher pauses after already preparing the next directory completely, so we can test the validity of the second patch state.
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}
	dataDir = patch.newDataDir

	extractResult := patch.extractor.Result()
	if extractResult.Err != nil {
		t.Fatal(extractResult.Err)
	}

	expectedExtractedFiles := []string{
		"./fDynamiccase",
		"./fToPreserve",
		"./fToPreservecase",
		"./fToUpdate",
		"./fToUpdatecase",
		"./toAdd/file1",
		"./toAdd/file2",
		"./toRemove/file1",
	}
	assertSlicesEqual(t, extractResult.Result, expectedExtractedFiles)

	// Common
	assertFileContentEquals(t, dataDir, "./fDynamic", "test\n")
	assertFileContentEquals(t, dataDir, "./fToPreserve", "")
	assertFileContentEquals(t, dataDir, "./fToUpdate", "update\n")
	assertFileContentEquals(t, dataDir, "./toAdd/file1", "")
	assertFileContentEquals(t, dataDir, "./toRemove/file1", "")

	// Files haven't been cleared yet
	assertFileContentEquals(t, dataDir, "./toClear/file1", "")
	assertFileContentEquals(t, dataDir, "./toRemove/file2", "")

	assertFolderExists(t, dataDir, "./toClear")
	assertFolderExists(t, dataDir, "./empty")
	assertFolderExists(t, dataDir, "./newEmpty")

	if runtime.GOOS == "linux" {

		// Linux is case sensitive, so if the exact same filename doesn't exist in the new archive we expect it to be removed.
		// Dynamic files are not part of the old build so we expect to see them even if theres a file in the new archive with the the same name but different case.
		assertFileContentEquals(t, dataDir, "./fDynamiccase", "update\n")
		assertFileContentEquals(t, dataDir, "./fDynamicCase", "test\n") // This is the old dynamic file which shouldn't be updated
		assertFileContentEquals(t, dataDir, "./fToPreservecase", "")
		assertFileContentEquals(t, dataDir, "./fToUpdatecase", "update\n")

		assertFileContentEquals(t, dataDir, "./fToPreserveCase", "")
		assertFileContentEquals(t, dataDir, "./fToUpdateCase", "test\n")
	} else {

		// Windows and Mac are case insensitive, so we expect to see the exact old filename even if it has a different case in the new archive
		assertFileContentEquals(t, dataDir, "./fDynamicCase", "update\n")
		assertFileContentEquals(t, dataDir, "./fToPreserveCase", "")
		assertFileContentEquals(t, dataDir, "./fToUpdateCase", "update\n")
	}

	// The manifest shouldn't have updated yet, so we need to make sure it is correct for the old build data
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.Info.Dir != updateMetadata.DataDir ||
		manifest.Info.GameUID != updateMetadata.GameUID ||
		manifest.IsFirstInstall != false ||
		manifest.PatchInfo.Dir != updateMetadata2.DataDir ||
		manifest.PatchInfo.GameUID != updateMetadata2.GameUID ||

		// This should still be false because the game hasn't actually starting extrating until after the mocked launched instance is closed.
		manifest.PatchInfo.IsDirty != false {
		bytes, _ := json.Marshal(manifest)
		t.Fatalf("Game manifest is invalid:\n%s", string(bytes))
	}

	// Archive files should not have changed from the first patch until after the patch is finished.
	assertSlicesEqual(t, manifest.Info.ArchiveFiles, []string{
		"./fToPreserve",
		"./fToPreserveCase",
		"./fToRemove",
		"./fToRemoveCase",
		"./fToUpdate",
		"./fToUpdateCase",
		"./toAdd/file1",
		"./toClear/file1",
		"./toRemove/file1",
		"./toRemove/file2",
	})

	// Dynamic files should have been written to contain the files created between patch1 and patch2.
	assertSlicesEqual(t, manifest.PatchInfo.DynamicFiles, []string{
		"./fDynamic",
		"./fDynamicCase",
	})

	// Download file should still exist
	assertFileExists(t, dir, ".tempDownload")

	// The folder of the current patch data dir should still exist tho.
	assertFolderExists(t, patch.dataDir, ".")

	// Killing the mock launcher should remove it from track list, allowing patch to detect it and continue patching.
	mockLauncher.Kill()

	// It should eventually finish the patch in a reasonable time.
	select {
	case <-patch.Done():
	case <-time.After(10 * time.Second):
		t.Fatalf("Expecting patch to finish. It may be stuck waiting for a signal from a child that will never arive")
	}

	// Now that the patch is finished, some files should have cleaned up.
	assertFileNotExist(t, dataDir, "./toClear/file1")
	assertFileNotExist(t, dataDir, "./toRemove/file2")
	if runtime.GOOS == "linux" {
		assertFileNotExist(t, dataDir, "./fToPreserveCase")
		assertFileNotExist(t, dataDir, "./fToUpdateCase")
	}
	assertFileNotExist(t, dir, ".tempDownload")

	// Check again for redundancy.
	assertSecondPatchState(t, updateMetadata2, patch, false)

	// This time the folder of the current patch data dir should not exist because cleanup should have run.
	assertFileNotExist(t, patch.dataDir, ".")
}

func TestPatchInvalidRelativeDir(t *testing.T) {
	test.PrepareNextTest(t)

	var invalidDir string
	if runtime.GOOS == "windows" {
		invalidDir = ".\\test-dir\\patch"
	} else {
		invalidDir = "./test-dir/patch"
	}

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, invalidDir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	err = patch.Result()
	if err == nil {
		t.Fatal("Expecting patch to error out on invalid dir, received no error")
	} else if err.Error() != "Patch dir is invalid: not absolute" {
		t.Fatalf("Expecting patch to error out on invalid dir, received: %s\n", err.Error())
	}
}

func TestPatchInvalidPathDir(t *testing.T) {
	test.PrepareNextTest(t)

	invalidDir := filepath.Join(test.Dir, "\x00invalid?")
	dir, err := filepath.Abs(invalidDir)
	if err != nil {
		t.Fatalf("Could not get absolute path to %s", invalidDir)
	}
	invalidDir = dir

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, invalidDir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	err = patch.Result()
	if err == nil {
		t.Fatal("Expecting patch to error out on invalid dir, received no error")
	} else if !strings.HasPrefix(err.Error(), "Patch dir is invalid:") {
		t.Fatalf("Expecting patch to error out on invalid dir, received: %s\n", err.Error())
	}
}

func TestPatchInvalidDirOutOfScope(t *testing.T) {
	test.PrepareNextTest(t)

	invalidDir := filepath.Join(test.Dir, "..")
	dir, err := filepath.Abs(invalidDir)
	if err != nil {
		t.Fatalf("Could not get absolute path to %s", invalidDir)
	}
	invalidDir = dir

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, invalidDir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	err = patch.Result()
	if err == nil {
		t.Fatal("Expecting patch to error out on invalid dir, received no error")
	} else if err.Error() != "Failed to prepare patch: File out of scope" {
		t.Fatalf("Expecting patch to error out on out of scope, received: %s\n", err.Error())
	}
}

func TestPatchInvalidChecksum(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, "wrong", 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()
	err = patch.Result()
	if err == nil {
		t.Fatal("Expecting patch to error out on invalid checksum, received no error")
	} else if err.Error() != fmt.Sprintf("Failed to download update: Invalid checksum. Expected wrong, got %s", patch1Checksum) {
		t.Fatalf("Expecting patch to error out on invalid checksum, received: %s\n", err.Error())
	}
}

func TestPatchNoChecksum(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, "", 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchCancelImmediately(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	// Create a resumable that gets canceled even before patch starts
	// This'll mean the patcher should never even start preparation and finish right away
	r := concurrency.NewResumable(nil)
	r.Cancel()

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(r, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	log.Println(patch.Dir)
	assertFileNotExist(t, patch.Dir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchCancelFirstInstallationDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	assertPatchStateTransition(t, patch, StateDownload, 10*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result().Error())
	}

	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchCancelFirstInstallationExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	assertPatchStateTransition(t, patch, StateExtract, 10*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != context.Canceled {
		t.Fatalf("Expected patch result to be the context cancelation, received %s\n", patch.Result())
	}

	assertFileNotExist(t, patch.newDataDir, ".")
	assertFileNotExist(t, dir, ".manifest")
	assertFileNotExist(t, dir, ".tempDownload")
}

func TestPatchCancelFirstInstallationCleanup(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	assertPatchStateTransition(t, patch, StateCleanup, 10*time.Second)
	patch.Cancel()

	<-patch.Done()
	if patch.Result() != nil {
		t.Fatalf("Expected patch result to be successful even tho it was canceled. Received %s\n", patch.Result().Error())
	}

	// Even tho we canceled, we're expecting the installation to go through because it was canceled at the last state, which is not meant to be cancellable.
	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchPauselImmediately(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	// Create a resumable that gets paused  even before patch starts
	// This'll mean the patcher should not even start preparation until it is resumed
	r := concurrency.NewResumable(nil)
	r.Pause()

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(r, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Wait for one second to make sure preparation didn't start and respected being paused.
	assertPatchStateNoTransition(t, patch, StateStart, 1*time.Second)

	r.Resume()
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchPauseDownload(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	updateMetadata := getUpdateMetadata("v1", patch1Url, patch1Checksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Wait for it to start downloading
	assertPatchStateTransition(t, patch, StateDownload, 10*time.Second)
	patch.Pause()

	// Wait for 5 seconds to make sure download didn't go through.
	assertPatchStateNoTransition(t, patch, StateDownload, 5*time.Second)

	patch.Resume()
	<-patch.Done()

	assertFirstPatchState(t, updateMetadata, patch, false)
}

func TestPatchPauseExtract(t *testing.T) {
	_, dir := test.PrepareNextTest(t)

	// Requiring this fixture will allow us to test on a bigger file without having to download it every time.
	test.RequireFixture(t, bigDownloadFile, dir, ".tempDownload")

	updateMetadata := getUpdateMetadata("v1", bigDownloadURL, bigDownloadChecksum, 0, false)
	patch, err := NewInstall(nil, dir, false, nil, updateMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}

	// Wait for it to start extracting
	assertPatchStateTransition(t, patch, StateExtract, 10*time.Second)
	patch.Pause()
	log.Println("Paused extraction...")

	// Wait for 5 seconds to make sure extract didn't go through.
	assertPatchStateNoTransition(t, patch, StateExtract, 5*time.Second)

	log.Println("Resuming extraction...")
	patch.Resume()
	<-patch.Done()
	if patch.Result() != nil {
		t.Fatal(patch.Result())
	}
}

func waitForPatchState(p *Patch, state State, timeout time.Duration) <-chan bool {

	ch := make(chan bool)
	go func() {

		sub, err := p.Subscribe()
		if err != nil {
			ch <- false
			close(ch)
			return
		}

		defer p.Unsubscribe(sub)
		expire := time.After(timeout)
		defer close(ch)

		// We loop until we get the state changed message we are waiting for.
		// The loop also breaks on the subscriber closing or reaching the time limit.
		for {
			select {
			// Attempt to get a value from the subscriber
			case msg, open := <-sub:
				// Break if the subscriber closed
				if !open {
					ch <- false
					return
				}

				// We need to check if the message type we received is a state change message
				switch msg.(type) {
				case StateChangeMsg:
					newState := msg.(StateChangeMsg)
					log.Printf("Received new state %d, waiting state %d", newState.State, state)
					if state == newState.State {
						ch <- true
						return
					}
				// We don't care about any other message type
				default:
				}
			// Break if we reached the time limit
			case <-expire:
				ch <- false
				return
			}
		}
	}()
	return ch
}

func assertPatchStateTransition(t *testing.T, p *Patch, state State, timeout time.Duration) {
	if !<-waitForPatchState(p, state, timeout) {
		t.Fatalf("Expecting patch state to transition to %d after %s, but patch state is %d\n", state, timeout.String(), p.state)
	}
}

func assertPatchStateNoTransition(t *testing.T, p *Patch, state State, timeout time.Duration) {
	<-time.After(timeout)
	if p.state != state {
		t.Fatalf("Expecting patch state to remain %d after %s, but patch state is %d\n", state, timeout.String(), p.state)
	}
}

func assertSlicesEqual(t *testing.T, a, b []string) {
	slice1 := make([]string, len(a), len(a))
	copy(slice1, a)
	sort.Strings(slice1)
	slice2 := make([]string, len(b), len(b))
	copy(slice2, b)
	sort.Strings(slice2)

	if !reflect.DeepEqual(slice1, slice2) {
		t.Fatal(fmt.Errorf("Expecting slice \n%s\nto be equal\n%s", strings.Join(slice1, ","), strings.Join(slice2, ",")))
	}
}

func assertFileContentEquals(t *testing.T, dir, path, expect string) {
	path, err := filepath.Abs(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}

	file, err := test.OS.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	bytes, err := ioutil.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	if str := string(bytes); str != expect {
		t.Fatal(fmt.Errorf("File content %s expected:\n%s\ngot: \n%s", path, expect, str))
	}
}

func assertMkdirAll(t *testing.T, dir string) {
	stat, err := test.OS.Stat(dir)
	if err == nil {
		if !stat.IsDir() {
			t.Fatal(dir, " is already a file")
		}
		return
	}

	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	if err = test.OS.MkdirAll(dir, 0777); err != nil {
		t.Fatal(err)
	}
}

func assertWriteFile(t *testing.T, dir, path, content string) {
	path, err := filepath.Abs(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}

	if err := test.OS.IOUtilWriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertFileNotExist(t *testing.T, dir, path string) {
	path, err := filepath.Abs(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}

	_, err = test.OS.Stat(path)
	if err == nil {
		t.Fatalf("Expecting file %s to not exist", path)
	}

	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func assertFileExists(t *testing.T, dir, path string) {
	path, err := filepath.Abs(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}

	stat, err := test.OS.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if stat.IsDir() {
		t.Fatal(fmt.Errorf("Expected file, got directory in %s", path))
	}
}

func assertFolderExists(t *testing.T, dir, path string) {
	path, err := filepath.Abs(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}

	stat, err := test.OS.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if !stat.IsDir() {
		t.Fatal(fmt.Errorf("Expected directory, got file in %s", path))
	}
}

// assertFirstPatchState assets wether the given patch and info was correctly executed as the first patch.
// if noop is true, we're checking if the update operation ended up being a no-op (if upgrading to the same build for example).
func assertFirstPatchState(t *testing.T, updateMetadata *data.UpdateMetadata, patch *Patch, noop bool) {
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}
	dir := patch.Dir
	dataDir := patch.newDataDir

	expectedExtractedFiles := []string{
		"./fToPreserve",
		"./fToPreserveCase",
		"./fToRemove",
		"./fToRemoveCase",
		"./fToUpdate",
		"./fToUpdateCase",
		"./toAdd/file1",
		"./toClear/file1",
		"./toRemove/file1",
		"./toRemove/file2",
	}

	// When noop is true we shouldn't reach the extraction step
	if !noop {
		extractResult := patch.extractor.Result()
		if extractResult.Err != nil {
			t.Fatal(extractResult.Err)
		}

		assertSlicesEqual(t, extractResult.Result, expectedExtractedFiles)
	} else {
		if patch.extractor != nil {
			t.Fatal("Expecting the patch extractor to be nil")
		}
	}

	assertFileContentEquals(t, dataDir, "./fToPreserve", "")
	assertFileContentEquals(t, dataDir, "./fToPreserveCase", "")
	assertFileContentEquals(t, dataDir, "./fToRemove", "test\n")
	assertFileContentEquals(t, dataDir, "./fToRemoveCase", "test\n")
	assertFileContentEquals(t, dataDir, "./fToUpdate", "test\n")
	assertFileContentEquals(t, dataDir, "./fToUpdateCase", "test\n")
	assertFileContentEquals(t, dataDir, "./toAdd/file1", "")
	assertFileContentEquals(t, dataDir, "./toClear/file1", "")
	assertFileContentEquals(t, dataDir, "./toRemove/file1", "")
	assertFileContentEquals(t, dataDir, "./toRemove/file2", "")

	assertFolderExists(t, dataDir, "./empty")

	// Attempt to read manifest file
	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.Info.Dir != updateMetadata.DataDir ||
		manifest.Info.GameUID != updateMetadata.GameUID ||
		manifest.IsFirstInstall != false ||
		manifest.PatchInfo != nil {
		bytes, _ := json.Marshal(manifest)
		t.Fatalf("Game manifest is invalid:\n%s", string(bytes))
	}

	assertSlicesEqual(t, manifest.Info.ArchiveFiles, expectedExtractedFiles)

	assertFileNotExist(t, dir, "./.tempDownload")
}

// assertSecondPatchState assets wether the given patch and info was correctly executed as the second patch.
// if noop is true, we're checking if the update operation ended up being a no-op (if upgrading to the same build for example).
func assertSecondPatchState(t *testing.T, updateMetadata *data.UpdateMetadata, patch *Patch, noop bool) {
	if err := patch.Result(); err != nil {
		t.Fatal(err)
	}
	dir := patch.Dir
	dataDir := patch.newDataDir

	expectedExtractedFiles := []string{
		"./fDynamiccase",
		"./fToPreserve",
		"./fToPreservecase",
		"./fToUpdate",
		"./fToUpdatecase",
		"./toAdd/file1",
		"./toAdd/file2",
		"./toRemove/file1",
	}

	// When noop is true we shouldn't reach the extraction step
	if !noop {
		extractResult := patch.extractor.Result()
		if extractResult.Err != nil {
			t.Fatal(extractResult.Err)
		}

		assertSlicesEqual(t, extractResult.Result, expectedExtractedFiles)
	} else {
		if patch.extractor != nil {
			t.Fatal("Expecting the patch extractor to be nil")
		}
	}

	// Common
	assertFileContentEquals(t, dataDir, "./fDynamic", "test\n")
	assertFileContentEquals(t, dataDir, "./fToPreserve", "")
	assertFileContentEquals(t, dataDir, "./fToUpdate", "update\n")
	assertFileContentEquals(t, dataDir, "./toAdd/file1", "")
	assertFileContentEquals(t, dataDir, "./toRemove/file1", "")

	assertFileNotExist(t, dataDir, "./toClear/file1")
	assertFileNotExist(t, dataDir, "./toRemove/file2")

	assertFolderExists(t, dataDir, "./toClear")
	assertFolderExists(t, dataDir, "./empty")
	assertFolderExists(t, dataDir, "./newEmpty")

	if runtime.GOOS == "linux" {

		// Linux is case sensitive, so if the exact same filename doesn't exist in the new archive we expect it to be removed.
		// Dynamic files are not part of the old build so we expect to see them even if theres a file in the new archive with the the same name but different case.
		assertFileContentEquals(t, dataDir, "./fDynamiccase", "update\n")
		assertFileContentEquals(t, dataDir, "./fDynamicCase", "test\n") // This is the old dynamic file which shouldn't be updated
		assertFileContentEquals(t, dataDir, "./fToPreservecase", "")
		assertFileContentEquals(t, dataDir, "./fToUpdatecase", "update\n")
		assertFileNotExist(t, dataDir, "./fToPreserveCase")
		assertFileNotExist(t, dataDir, "./fToUpdateCase")
	} else {

		// Windows and Mac are case insensitive, so we expect to see the exact old filename even if it has a different case in the new archive
		assertFileContentEquals(t, dataDir, "./fDynamicCase", "update\n")
		assertFileContentEquals(t, dataDir, "./fToPreserveCase", "")
		assertFileContentEquals(t, dataDir, "./fToUpdateCase", "update\n")
	}

	manifest, err := game.GetManifest(dir, test.OS)
	if err != nil {
		t.Fatal(err)
	}

	if manifest.Info.Dir != updateMetadata.DataDir ||
		manifest.Info.GameUID != updateMetadata.GameUID ||
		manifest.IsFirstInstall != false ||
		manifest.PatchInfo != nil {
		bytes, _ := json.Marshal(manifest)
		t.Fatalf("Game manifest is invalid:\n%s", string(bytes))
	}

	assertSlicesEqual(t, manifest.Info.ArchiveFiles, expectedExtractedFiles)

	assertFileNotExist(t, dir, ".tempDownload")
}
