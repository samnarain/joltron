package data

const (
	// ManifestVersion is the version in the manifest.
	// It should be updated every time theres a change in the manifest format.
	ManifestVersion = 1
)

// UpdateMetadata is an incoming message from a child saying an update for the currently running game is available.
// The runner re-broadcasts this to all connected children as an UpdateMsg with Message being "updateAvailable"
type UpdateMetadata struct {
	GameUID    string `json:"gameUID"`
	URL        string `json:"url"`
	Checksum   string `json:"checksum,omitempty"`
	RemoteSize int64  `json:"remoteSize,omitempty"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Executable string `json:"executable"`

	OldBuildMetadata *BuildMetadata `json:"oldBuildMetadata,omitempty"`
	NewBuildMetadata *BuildMetadata `json:"newBuildMetadata,omitempty"`
	DiffMetadata     *DiffMetadata  `json:"diffMetadata,omitempty"`

	// Use *bool here because we need null to distinguish between an excplicit false value.
	// This is to tell the runner to not modify the current setting.
	SideBySide *bool `json:"sideBySide,omitempty"`

	// DataDir is only used internally by patcher to hold the relative data dir name to use (depending on SideBySide)
	DataDir string

	// PlatformURL, same as DataDir is only used internally by patcher to hold the platform url relevant to this metadata request/response
	PlatformURL string
}

// BuildMetadata contains information about a build's structure so it could be validated for integrity.
type BuildMetadata struct {
	Files    map[string]FileMetadata `json:"files"`
	Dirs     []string                `json:"dirs"`
	Symlinks map[string]string       `json:"symlinks"`
}

// FileMetadata holds information about an individual file in the build metadata
type FileMetadata struct {
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

// DiffMetadata contains information about the diff and how it is applied to the existing build.
type DiffMetadata struct {
	Identical map[string][]string      `json:"identical"`
	Similar   map[string][]SimilarFile `json:"similar"`
	Created   []string                 `json:"created"`
	Removed   []string                 `json:"removed"`
	Dirs      []string                 `json:"dirs"`
	Symlinks  map[string]string        `json:"symlinks"`
}

// SimilarFile contains information about how to patch a single file.
// Patching is done by breaking up the old file into ChunkSize bytes parts, then applying patches to them one by one,
// and finally concatenating the patched bits with the tail bits to form the new file.
type SimilarFile struct {
	ChunkSize int64             `json:"chunkSize"`
	New       string            `json:"new"`
	Size      int64             `json:"size"`
	Patches   []SimilarFilePart `json:"patches"`
	Tails     []SimilarFilePart `json:"tails"`
	DiffSize  int64             `json:"diffSize"`
}

// SimilarFilePart describes a part of a patch (may be either a patch or a tail)
type SimilarFilePart struct {
	File     string `json:"file"`
	Size     int64  `json:"size"`
	Checksum string `json:"checksum"`
}

// Info is the game's info.
type Info struct {
	Dir          string   `json:"dir"`
	GameUID      string   `json:"uid"`
	ArchiveFiles []string `json:"archiveFiles,omitempty"`
	PlatformURL  string   `json:"platformUrl,omitempty"`
}

// LaunchOptions has information about how to launch the game.
type LaunchOptions struct {
	Executable string `json:"executable"`

	// We don't not need to care about the os/arch since
	// we only care about the target os/arch which should not be overriden by
	// the build os/arch so we could keep fetching builds that are matching
	// our target instead of builds that are matching our currently installed one.
	// OS and Arch instead are specified directly under the manifest
	// OS   string `json:"os"`
	// Arch string `json:"arch"`
}

// PatchInfo has information needed to install/update/uninstall a game.
type PatchInfo struct {
	Dir          string   `json:"dir"`
	GameUID      string   `json:"uid"`
	IsDirty      bool     `json:"isDirty"`
	DynamicFiles []string `json:"dynamicFiles,omitempty"`
	PlatformURL  string   `json:"platformUrl,omitempty"`

	// We freeze the following info which is given from the arguments or platform url so we can continue patching
	// without having to refetch that info. This allows us to finish a patch operation even if the build is no longer valid.
	DownloadSize     int64          `json:"downloadSize,omitempty"`
	DownloadChecksum string         `json:"downloadChecksum,omitempty"`
	LaunchOptions    *LaunchOptions `json:"launchOptions,omitempty"`
	OS               string         `json:"os,omitempty"`
	Arch             string         `json:"arch,omitempty"`

	OldBuildMetadata *BuildMetadata `json:"oldBuildMetadata,omitempty"`
	NewBuildMetadata *BuildMetadata `json:"newBuildMetadata,omitempty"`
	DiffMetadata     *DiffMetadata  `json:"diffMetadata,omitempty"`
}

// RunningInfo has information about the currently running patch operation
type RunningInfo struct {
	Port  uint16 `json:"port"`
	Pid   int    `json:"pid"`
	Since int64  `json:"since"`
}

// PlayingInfo has information about the currently running game
type PlayingInfo struct {
	Args  []string `json:"args,omitempty"`
	Since int64    `json:"since"`
}

// Manifest is the information that ends up getting written to the filesystem.
type Manifest struct {
	Version        int            `json:"version"`
	Info           *Info          `json:"gameInfo"`
	LaunchOptions  *LaunchOptions `json:"launchOptions"`
	OS             string         `json:"os"`
	Arch           string         `json:"arch"`
	IsFirstInstall bool           `json:"isFirstInstall"`
	PatchInfo      *PatchInfo     `json:"patchInfo,omitempty"`
	RunningInfo    *RunningInfo   `json:"runningInfo,omitempty"`
	PlayingInfo    *PlayingInfo   `json:"playingInfo,omitempty"`
}
