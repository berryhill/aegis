// Package update securely replaces a release-built Aegis executable.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIURL      = "https://api.github.com/repos/berryhill/aegis/releases/latest"
	defaultDownloadURL = "https://github.com/berryhill/aegis/releases/download"
	maxArchiveSize     = 128 << 20
	maxBinarySize      = 256 << 20
)

type Result struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	Updated         bool   `json:"updated"`
	Executable      string `json:"executable,omitempty"`
}

type Updater struct {
	CurrentVersion string
	APIURL         string
	DownloadURL    string
	Client         *http.Client
	Executable     func() (string, error)
	GOOS           string
	GOARCH         string
}

func New(currentVersion string) *Updater {
	return &Updater{
		CurrentVersion: currentVersion,
		APIURL:         defaultAPIURL,
		DownloadURL:    defaultDownloadURL,
		Client:         &http.Client{Timeout: 2 * time.Minute},
		Executable:     os.Executable,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
	}
}

func (u *Updater) Run(ctx context.Context, checkOnly bool) (Result, error) {
	if u.GOOS != "linux" && u.GOOS != "darwin" {
		return Result{}, fmt.Errorf("self-update is unsupported on %s; install the new release manually", u.GOOS)
	}
	if u.GOARCH != "amd64" && u.GOARCH != "arm64" {
		return Result{}, fmt.Errorf("self-update is unsupported on %s/%s; install the new release manually", u.GOOS, u.GOARCH)
	}
	tag, latest, err := u.latest(ctx)
	if err != nil {
		return Result{}, err
	}
	result := Result{CurrentVersion: normalize(u.CurrentVersion), LatestVersion: latest}
	comparison, comparable := compare(result.CurrentVersion, latest)
	result.UpdateAvailable = !comparable || comparison < 0
	if !result.UpdateAvailable || checkOnly {
		return result, nil
	}

	asset := fmt.Sprintf("aegis_%s_%s_%s.tar.gz", tag, u.GOOS, u.GOARCH)
	checksums, err := u.download(ctx, u.DownloadURL+"/"+tag+"/SHA256SUMS", 1<<20)
	if err != nil {
		return Result{}, fmt.Errorf("download checksums: %w", err)
	}
	want, err := checksumFor(checksums, asset)
	if err != nil {
		return Result{}, err
	}
	archive, err := u.download(ctx, u.DownloadURL+"/"+tag+"/"+asset, maxArchiveSize)
	if err != nil {
		return Result{}, fmt.Errorf("download release archive: %w", err)
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(want, hex.EncodeToString(got[:])) {
		return Result{}, errors.New("release archive checksum mismatch")
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return Result{}, err
	}
	executable, err := u.Executable()
	if err != nil {
		return Result{}, fmt.Errorf("resolve current executable: %w", err)
	}
	if err = replaceExecutable(executable, binary); err != nil {
		return Result{}, fmt.Errorf("replace %s: %w (install manually if this executable is managed by a package manager or requires elevated permissions)", executable, err)
	}
	result.Updated = true
	result.Executable = executable
	return result, nil
}

func (u *Updater) latest(ctx context.Context) (tag, version string, err error) {
	body, err := u.download(ctx, u.APIURL, 1<<20)
	if err != nil {
		return "", "", fmt.Errorf("discover latest release: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err = json.Unmarshal(body, &release); err != nil {
		return "", "", fmt.Errorf("decode latest release: %w", err)
	}
	version = normalize(release.TagName)
	if release.TagName != "v"+version {
		return "", "", fmt.Errorf("latest release tag %q is not vMAJOR.MINOR.PATCH", release.TagName)
	}
	if _, ok := parse(version); !ok {
		return "", "", fmt.Errorf("latest release tag %q is not vMAJOR.MINOR.PATCH", release.TagName)
	}
	return release.TagName, version, nil
}

func (u *Updater) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "aegis/"+normalize(u.CurrentVersion))
	response, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	reader := io.LimitReader(response.Body, limit+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return body, nil
}

func checksumFor(data []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				break
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("SHA256SUMS has no valid checksum for %s", asset)
}

func extractBinary(archive []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var binary []byte
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read release archive: %w", nextErr)
		}
		if header.Name != "aegis" || header.Typeflag != tar.TypeReg || binary != nil || header.Size < 1 || header.Size > maxBinarySize {
			return nil, fmt.Errorf("release archive contains unexpected entry %q", header.Name)
		}
		binary, err = io.ReadAll(io.LimitReader(tarReader, maxBinarySize+1))
		if err != nil || int64(len(binary)) != header.Size {
			return nil, errors.New("release archive contains an invalid aegis binary")
		}
	}
	if len(binary) == 0 {
		return nil, errors.New("release archive does not contain aegis")
	}
	return binary, nil
}

func replaceExecutable(path string, binary []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".aegis-update-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = temporary.Write(binary); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Chmod(temporaryPath, info.Mode().Perm()); err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if directoryHandle, openErr := os.Open(directory); openErr == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func normalize(version string) string { return strings.TrimPrefix(strings.TrimSpace(version), "v") }

func compare(left, right string) (int, bool) {
	a, ok := parse(left)
	if !ok {
		return 0, false
	}
	b, ok := parse(right)
	if !ok {
		return 0, false
	}
	for index := range a {
		if a[index] < b[index] {
			return -1, true
		}
		if a[index] > b[index] {
			return 1, true
		}
	}
	return 0, true
}

func parse(version string) ([3]uint64, bool) {
	var parsed [3]uint64
	parts := strings.Split(version, ".")
	if len(parts) != len(parsed) {
		return parsed, false
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return parsed, false
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsed, false
		}
		parsed[index] = value
	}
	return parsed, true
}
