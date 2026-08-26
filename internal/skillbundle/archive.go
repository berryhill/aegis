package skillbundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const archiveMode = 0o644

func BuildArchive(root, destination, bundleVersion, sourceRevision string) (string, error) {
	manifest, err := Validate(root)
	if err != nil {
		return "", err
	}
	if !semverPattern.MatchString(bundleVersion) {
		return "", deny("invalid_bundle_version", destination, "release archive requires one exact stable version")
	}
	if !exactRevision(sourceRevision) {
		return "", deny("invalid_source_revision", destination, "release archive requires one exact lowercase 40-hex Git revision")
	}
	manifest.Bundle.Version = bundleVersion
	manifest.Bundle.SourceRevision = sourceRevision
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	manifestBytes = append(manifestBytes, '\n')
	if info, statErr := os.Lstat(destination); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", deny("unsafe_archive_destination", destination, "destination must not be a symlink")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	files, err := archiveFiles(root)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(parent, ".aegis-skills-*.tar.gz")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	committed := false
	defer func() {
		temp.Close()
		if !committed {
			_ = os.Remove(tempName)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return "", err
	}
	gzipWriter, err := gzip.NewWriterLevel(temp, gzip.BestCompression)
	if err != nil {
		return "", err
	}
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	prefix := "aegis-skills-v" + manifest.Bundle.Version
	for _, rel := range files {
		content := manifestBytes
		if rel != ManifestName {
			var readErr error
			content, readErr = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
			if readErr != nil {
				return "", readErr
			}
		}
		header := &tar.Header{
			Name: path.Join(prefix, rel), Mode: archiveMode, Size: int64(len(content)),
			ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC(),
			Uid: 0, Gid: 0, Uname: "", Gname: "", Format: tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return "", err
		}
		if _, err := tarWriter.Write(content); err != nil {
			return "", err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return "", err
	}
	if err := gzipWriter.Close(); err != nil {
		return "", err
	}
	if err := temp.Sync(); err != nil {
		return "", err
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempName, destination); err != nil {
		return "", err
	}
	committed = true
	return sha256File(destination)
}

func exactRevision(value string) bool {
	return len(value) == 40 && revisionPattern.MatchString(value) && value != "repository"
}

func VerifyArchive(archive, expectedSourceRevision string) (Manifest, error) {
	if !exactRevision(expectedSourceRevision) {
		return Manifest{}, deny("invalid_source_revision", archive, "verification requires one expected lowercase 40-hex Git revision")
	}
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, deny("unsafe_archive", archive, "archive must be a regular non-symlink file")
	}
	opened, err := os.Open(archive)
	if err != nil {
		return Manifest{}, err
	}
	defer opened.Close()
	gz, err := gzip.NewReader(opened)
	if err != nil {
		return Manifest{}, deny("archive_malformed", archive, err.Error())
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	work, err := os.MkdirTemp(filepath.Dir(archive), ".aegis-skills-verify-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(work)
	var prefix string
	seen := map[string]bool{}
	for {
		header, nextErr := reader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return Manifest{}, deny("archive_malformed", archive, nextErr.Error())
		}
		if header.Typeflag != tar.TypeReg || header.Mode != archiveMode || header.Linkname != "" {
			return Manifest{}, deny("unsafe_archive_member", header.Name, "members must be regular mode-0644 files")
		}
		clean := path.Clean(header.Name)
		if clean != header.Name || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
			return Manifest{}, deny("archive_path_traversal", header.Name, "member path is unsafe")
		}
		parts := strings.SplitN(clean, "/", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "aegis-skills-v") || !safeRelative(parts[1]) {
			return Manifest{}, deny("archive_layout", header.Name, "member must be beneath one versioned root")
		}
		if parts[1] != ManifestName && parts[1] != EvaluationsName && !strings.HasPrefix(parts[1], "skills/") {
			return Manifest{}, deny("undeclared_archive_member", header.Name, "archive members must belong to the canonical skills inventory")
		}
		if prefix == "" {
			prefix = parts[0]
		}
		if parts[0] != prefix || seen[parts[1]] {
			return Manifest{}, deny("archive_layout", header.Name, "root differs or member is duplicated")
		}
		seen[parts[1]] = true
		if header.Size < 0 || header.Size > MaxManifestBytes {
			return Manifest{}, deny("archive_member_too_large", header.Name, "member exceeds the bounded archive limit")
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, MaxManifestBytes+1))
		if readErr != nil || int64(len(content)) != header.Size {
			return Manifest{}, deny("archive_malformed", header.Name, "member size mismatch")
		}
		destination := filepath.Join(work, filepath.FromSlash(parts[1]))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return Manifest{}, err
		}
		if err := os.WriteFile(destination, content, archiveMode); err != nil {
			return Manifest{}, err
		}
	}
	if len(seen) == 0 {
		return Manifest{}, deny("archive_empty", archive, "archive has no members")
	}
	manifest, err := Validate(work)
	if err != nil {
		return Manifest{}, err
	}
	if manifest.Bundle.SourceRevision != expectedSourceRevision {
		return Manifest{}, deny("archive_provenance_mismatch", archive, fmt.Sprintf("manifest source revision %q does not match expected revision %q", manifest.Bundle.SourceRevision, expectedSourceRevision))
	}
	if prefix != "aegis-skills-v"+manifest.Bundle.Version {
		return Manifest{}, deny("archive_version_mismatch", archive, "root does not match manifest version")
	}
	return manifest, nil
}

func archiveFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(root, "skills"), func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func sha256File(file string) (string, error) {
	opened, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer opened.Close()
	// Use the same bounded-independent digest representation as manifest content.
	content, err := io.ReadAll(opened)
	if err != nil {
		return "", err
	}
	return sha256Digest(content), nil
}

func ArchiveFilename(version string) (string, error) {
	if !semverPattern.MatchString(version) {
		return "", fmt.Errorf("invalid stable version %q", version)
	}
	return "aegis-skills_v" + version + ".tar.gz", nil
}
