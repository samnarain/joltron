package binarydiff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/test"
)

const (
	currentFile     = ".gj-testDiff-Build1.tar.xz"
	currentChecksum = "bb008c0036e7aaa2781417c995c45ad2"

	diffFile     = ".gj-testDiff-Diff.tar.xz"
	diffChecksum = "c81516f083d574f166618c6cbc123305"

	crashEnsuringDirsFile     = "crashEnsuringDirs.tar.xz"
	crashEnsuringDirsChecksum = "edc9b5bf9036e9b3ce7858251406057f"

	crashIdenticalFile     = "crashIdentical.tar.xz"
	crashIdenticalChecksum = "e9093d076d0a13529dc2076668881f32"

	crashSimilarFile     = "crashSimilar.tar.xz"
	crashSimilarChecksum = "1f558f2d17030890aedaa5a02e479896"

	crashCreatedFile     = "crashCreated.tar.xz"
	crashCreatedChecksum = "d9fce507bddb345d0f4182d76a4844de"

	crashDynamicConflictFile     = "crashDynamicConflict.tar.xz"
	crashDynamicConflictChecksum = "44426b6b2cba61b3405341901f88e7fa"

	crashDynamicFile     = "crashDynamic.tar.xz"
	crashDynamicChecksum = "04b3386acd4a12bbd327759efd6a67ae"

	crashSymlinksFile     = "crashSymlinks.tar.xz"
	crashSymlinksChecksum = "bcf7f0cd5ed43d23d4adbb4b9abd2e24"

	patchedBuildFile     = "patchedBuild.tar.xz"
	patchedBuildChecksum = "492774fa62e41398b94cf8a76a7d917f"
)

var (
	oldMetadata  data.BuildMetadata
	newMetadata  data.BuildMetadata
	diffMetadata data.DiffMetadata
	dynamicFiles []string
)

func TestMain(m *testing.M) {
	if err := <-test.DownloadFixture(currentFile, currentChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(diffFile, diffChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashEnsuringDirsFile, crashEnsuringDirsChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashEnsuringDirsFile, crashEnsuringDirsChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashIdenticalFile, crashIdenticalChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashSimilarFile, crashSimilarChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashCreatedFile, crashCreatedChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashDynamicConflictFile, crashDynamicConflictChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashDynamicFile, crashDynamicChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(crashSymlinksFile, crashSymlinksChecksum); err != nil {
		panic(err.Error())
	}

	if err := <-test.DownloadFixture(patchedBuildFile, patchedBuildChecksum); err != nil {
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

	dynamicFiles = []string{
		"./metadata (invalid).txt/level (dynamic).txt",
		"./saves (dynamic)/save1 (dynamic).txt",
	}

	os.Exit(m.Run())
}

func prepareNextTest(t *testing.T) (dir, dataDir, patchDir, diffDir string) {
	_, dir = test.PrepareNextTest(t)
	patchDir = filepath.Join(dir, "patch")
	dataDir = filepath.Join(dir, "data")
	diffDir = filepath.Join(dir, "diff")

	test.RequireExtractedFixture(t, currentFile, dir, "temp", dataDir)
	test.RequireExtractedFixture(t, diffFile, dir, "temp2", diffDir)

	return dir, dataDir, patchDir, diffDir
}

func TestPatcher(t *testing.T) {
	_, dataDir, patchDir, diffDir := prepareNextTest(t)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringEnsureDirs(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashEnsuringDirsFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchIdentical(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashIdenticalFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchSimilar(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashSimilarFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchCreated(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashCreatedFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchDynamicConflict(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashDynamicConflictFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchDynamic(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashDynamicFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringPatchSymlinks(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, crashSymlinksFile, dir, "temp3", patchDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringSwap(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	test.RequireExtractedFixture(t, patchedBuildFile, dir, "temp3", patchDir)

	swappedDir := dataDir + ".swap-" + filepath.Base(patchDir)
	if err := test.OS.Rename(dataDir, swappedDir); err != nil {
		t.Error("Could not prepare test because data dir could not be cleared")
		return
	}

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func TestPatcherCrashDuringSwap2(t *testing.T) {
	dir, dataDir, patchDir, diffDir := prepareNextTest(t)

	swappedDir := dataDir + ".swap-" + filepath.Base(patchDir)
	if err := test.OS.Rename(dataDir, swappedDir); err != nil {
		t.Error("Could not prepare test because data dir could not be cleared")
		return
	}

	test.RequireExtractedFixture(t, patchedBuildFile, dir, "temp3", dataDir)

	patchAndAssert(t, dataDir, patchDir, diffDir)
}

func patchAndAssert(t *testing.T, dataDir, patchDir, diffDir string) {
	patch, err := NewPatcher(nil, dataDir, patchDir, diffDir, true, dynamicFiles, &oldMetadata, &newMetadata, &diffMetadata, test.OS)
	if err != nil {
		t.Fatal(err.Error())
	}
	<-patch.Done()

	if err := patch.Result(); err != nil {
		t.Fatal(err.Error())
	}

	assertPatchState(t, dataDir)
}

func assertPatchState(t *testing.T, dataDir string) {

}
