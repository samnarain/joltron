package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"sync"

	"github.com/gamejolt/joltron/game/data"
	"github.com/gamejolt/joltron/network/messages/incoming"
	"github.com/gamejolt/joltron/network/messages/outgoing"
	OS "github.com/gamejolt/joltron/os"
)

var mu sync.Mutex

// GetManifest gets the .manifest in a given directory
func GetManifest(dir string, os2 OS.OS) (*data.Manifest, error) {
	mu.Lock()
	defer mu.Unlock()

	if os2 == nil {
		temp, err := OS.NewFileScope(dir, true)
		if err != nil {
			return nil, err
		}
		os2 = temp
	}

	// Attempt to read manifest file
	manifestFile := filepath.Join(dir, ".manifest")
	bytes, err := os2.IOUtilReadFile(manifestFile)
	if err != nil {
		return nil, err
	}

	//log.Printf("Reading .manifest file:\n%s\n", string(bytes))

	manifest := &data.Manifest{}
	if err = json.Unmarshal(bytes, manifest); err != nil {
		return nil, err
	}

	return manifest, nil
}

// WriteManifest writes the .manifest in a given directory
func WriteManifest(manifest *data.Manifest, dir string, os2 OS.OS) error {
	mu.Lock()
	defer mu.Unlock()

	if os2 == nil {
		temp, err := OS.NewFileScope(dir, true)
		if err != nil {
			return err
		}
		os2 = temp
	}

	bytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	//log.Printf("Saving .manifest file:\n%s\n", string(bytes))

	if err = os2.IOUtilWriteFile(filepath.Join(dir, ".manifest"), bytes, 0644); err != nil {
		return err
	}
	return nil
}

// IsGameRunning checks if the game installed in the given dir is currently running. The check may take up to the duration in checkDuration
func IsGameRunning(myPort uint16, dir string, checkDuration time.Duration) bool {
	manifest, err := GetManifest(dir, nil)
	if err != nil || manifest.PlayingInfo == nil || manifest.RunningInfo == nil || myPort == manifest.RunningInfo.Port {
		return false
	}

	runningState, err := getRunnerState(manifest.RunningInfo.Port, checkDuration)
	if err != nil {
		return false
	}

	if runningState.Manifest == nil || runningState.Manifest.RunningInfo == nil || runningState.Manifest.RunningInfo.Pid != manifest.RunningInfo.Pid {
		return false
	}

	return runningState.IsRunning || (runningState.Manifest != nil && runningState.Manifest.PlayingInfo != nil)
}

// IsRunnerRunning checks if the game installed in the given dir is currently running. The check may take up to the duration in checkDuration
func IsRunnerRunning(myPort uint16, dir string, checkDuration time.Duration) bool {
	manifest, err := GetManifest(dir, nil)
	if err != nil || manifest.RunningInfo == nil || myPort == manifest.RunningInfo.Port {
		return false
	}

	log.Printf("Got manifest: %v", manifest)
	log.Println("Getting runner state...")
	runningState, err := getRunnerState(manifest.RunningInfo.Port, checkDuration)
	if err != nil {
		log.Println("Couldnt get state: " + err.Error())
		return false
	}

	if runningState.Manifest == nil || runningState.Manifest.RunningInfo == nil || runningState.Manifest.RunningInfo.Pid != manifest.RunningInfo.Pid {
		return false
	}

	return true
}

func getRunnerState(port uint16, checkDuration time.Duration) (*outgoing.OutMsgState, error) {
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(checkDuration))

	expectedMsgID := time.Now().String()

	enc := json.NewEncoder(conn)
	if err := incoming.EncodeMsg(enc, &incoming.InMsgState{}, expectedMsgID); err != nil {
		return nil, err
	}

	dec := json.NewDecoder(conn)
	msgResponse, msgID, err := outgoing.DecodeMsg(dec)
	if err != nil {
		return nil, err
	}

	if msgID != expectedMsgID {
		return nil, fmt.Errorf("Expecting msg id: %s but got: %s", expectedMsgID, msgID)
	}

	switch msgResponse.(type) {
	case *outgoing.OutMsgState:
		return msgResponse.(*outgoing.OutMsgState), nil
	default:
		return nil, fmt.Errorf("Expecting message response type to be *OutMsgState")
	}
}

// Metadata is the json read from the platform url
type Metadata struct {
	Success bool   `json:"success"`
	Message string `json:"message"`

	GameUID       string `json:"gameUID"`
	URL           string `json:"url"`
	Checksum      string `json:"checksum"`
	RemoteSize    int64  `json:"remoteSize,omitempty"`
	RemoteSizeStr string `json:"remoteSizeStr,omitempty"` // Some languages can't express large enough integers for big files, so also accept it as a string
	SideBySide    *bool  `json:"sideBySide"`              // See comment in UpdateMetadata for why this is *bool
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Executable    string `json:"executable"`

	OldBuildMetadata *data.BuildMetadata `json:"oldBuildMetadata,omitempty"`
	NewBuildMetadata *data.BuildMetadata `json:"newBuildMetadata,omitempty"`
	DiffMetadata     *data.DiffMetadata  `json:"diffMetadata,omitempty"`
}

// GetMetadata gets the metadata for a requested game build from the given url
// Note: we use *bool for SideBySide, see comment on UpdateMetadata struct
func GetMetadata(gameUID, nextGameUID, platformURL, authToken, metadataStr string) (update *data.UpdateMetadata, err error) {
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

	client := &http.Client{
		Transport: noIdleTimeoutTransport,
	}

	os, err := OS.GetOS()
	if err != nil {
		return nil, err
	}

	arch, err := OS.GetArch()
	if err != nil {
		return nil, err
	}

	postData := map[string][]string{
		"gameUID":    []string{gameUID},
		"os":         []string{os},
		"arch":       []string{arch},
		"authToken":  []string{authToken},
		"metadata":   []string{metadataStr},
		"binaryDiff": []string{"no"},
	}
	if nextGameUID != "" {
		postData["nextGameUID"] = []string{nextGameUID}
	}

	res, err := client.PostForm(platformURL, postData)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)
	metadata := &Metadata{}
	if err = decoder.Decode(metadata); err != nil {
		return nil, err
	}

	log.Println("Got metadata: ", metadata)

	if !metadata.Success {
		if metadata.Message != "" {
			return nil, errors.New(metadata.Message)
		}
		return nil, errors.New("Failed to fetch game metadata")
	}

	var remoteSize int64
	if metadata.RemoteSize != 0 {
		remoteSize = metadata.RemoteSize
	} else if metadata.RemoteSizeStr != "" {
		remoteSize, err = strconv.ParseInt(metadata.RemoteSizeStr, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("Game metadata returned an invalid \"remoteSizeStr\": %s", metadata.RemoteSizeStr)
		}
	}

	if metadata.OS != "windows" && metadata.OS != "mac" && metadata.OS != "linux" {
		return nil, errors.New("OS must be either 'windows', 'mac' or 'linux'")
	}
	if metadata.Arch != "32" && metadata.Arch != "64" {
		return nil, errors.New("Arch must be either '32' or '64'")
	}
	if remoteSize != 0 && remoteSize < 1 {
		return nil, errors.New("Remote file size must be a positive integer")
	}

	update = &data.UpdateMetadata{
		GameUID:    metadata.GameUID,
		URL:        metadata.URL,
		Checksum:   metadata.Checksum,
		RemoteSize: remoteSize,
		OS:         metadata.OS,
		Arch:       metadata.Arch,
		Executable: metadata.Executable,
		SideBySide: metadata.SideBySide,
	}

	if metadata.DiffMetadata != nil {
		update.OldBuildMetadata, update.NewBuildMetadata, update.DiffMetadata = metadata.OldBuildMetadata, metadata.NewBuildMetadata, metadata.DiffMetadata
	}
	return update, nil
}
