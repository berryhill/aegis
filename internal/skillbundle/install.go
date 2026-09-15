package skillbundle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const managedBundles = ".aegis-skill-bundles"
const maxInstallArchiveBytes = 32 << 20

type InstalledInventory struct {
	EvidenceClass    string   `json:"evidence_class"`
	ArchiveDigest    string   `json:"archive_digest"`
	SourceRevision   string   `json:"source_revision"`
	Version          string   `json:"version"`
	ContentDigest    string   `json:"content_digest"`
	Skills           []string `json:"skills"`
	AuthorityGranted bool     `json:"authority_granted"`
}

type installReceipt struct {
	ArchiveDigest  string `json:"archive_digest"`
	SourceRevision string `json:"source_revision"`
}

// InstallArchive manages a complete, dedicated official skills inventory. It
// never adopts an existing unmanaged skills directory. The caller must select
// the destination explicitly; no environment/default profile is consulted.
// The atomic pointer affects discovery on subsequent reads, not running agents.
func InstallArchive(ctx context.Context, archive, digest, revision, home string) (InstalledInventory, error) {
	return installArchive(ctx, archive, digest, revision, home, nil)
}

func installArchive(ctx context.Context, archive, digest, revision, home string, beforePublish func() error) (InstalledInventory, error) {
	if err := ctx.Err(); err != nil {
		return InstalledInventory{}, err
	}
	if !digestPattern.MatchString(digest) || !exactRevision(revision) {
		return InstalledInventory{}, deny("invalid_install_identity", "", "exact sha256: digest and source revision required")
	}
	if err := privateDirectory(home); err != nil {
		return InstalledInventory{}, err
	}
	store := filepath.Join(home, managedBundles)
	if err := os.Mkdir(store, 0700); err != nil && !os.IsExist(err) {
		return InstalledInventory{}, err
	}
	if err := privateDirectory(store); err != nil {
		return InstalledInventory{}, err
	}
	unlock, err := lockInventory(filepath.Join(store, "lock"))
	if err != nil {
		return InstalledInventory{}, err
	}
	defer unlock()
	if _, err := os.Lstat(filepath.Join(home, "skills")); err == nil {
		if _, err := inspectInstalled(home); err != nil {
			return InstalledInventory{}, err
		}
	} else if !os.IsNotExist(err) {
		return InstalledInventory{}, err
	}

	stage, err := os.MkdirTemp(store, ".stage-")
	if err != nil {
		return InstalledInventory{}, err
	}
	defer os.RemoveAll(stage)
	// Freeze caller bytes before verification/extraction to avoid verifying one
	// archive and installing a subsequently replaced caller file.
	info, err := os.Lstat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxInstallArchiveBytes {
		return InstalledInventory{}, deny("unsafe_install_archive", "", "bounded regular archive required")
	}
	input, err := os.Open(archive)
	if err != nil {
		return InstalledInventory{}, err
	}
	content, readErr := io.ReadAll(io.LimitReader(input, maxInstallArchiveBytes+1))
	closeErr := input.Close()
	if readErr != nil {
		return InstalledInventory{}, readErr
	}
	if closeErr != nil {
		return InstalledInventory{}, closeErr
	}
	if len(content) > maxInstallArchiveBytes || sha256Digest(content) != digest {
		return InstalledInventory{}, deny("install_checksum_mismatch", "", "archive bytes do not match exact expected digest")
	}
	stagedArchive := filepath.Join(stage, "bundle.tar.gz")
	if err := syncedFile(stagedArchive, content, 0600); err != nil {
		return InstalledInventory{}, err
	}
	manifest, err := verifyArchiveInto(stagedArchive, revision, stage)
	if err != nil {
		return InstalledInventory{}, err
	}
	if err := os.Mkdir(filepath.Join(store, "hub"), 0700); err != nil && !os.IsExist(err) {
		return InstalledInventory{}, err
	}
	if err := privateDirectory(filepath.Join(store, "hub")); err != nil {
		return InstalledInventory{}, err
	}
	if err := os.Symlink("../../hub", filepath.Join(stage, "skills", ".hub")); err != nil {
		return InstalledInventory{}, err
	}
	receipt, err := json.Marshal(installReceipt{digest, revision})
	if err != nil {
		return InstalledInventory{}, err
	}
	if err := syncedFile(filepath.Join(stage, "receipt.json"), receipt, 0600); err != nil {
		return InstalledInventory{}, err
	}
	if err := syncTree(stage); err != nil {
		return InstalledInventory{}, err
	}
	generation := filepath.Join(store, strings.TrimPrefix(digest, "sha256:"))
	if _, err := os.Lstat(generation); os.IsNotExist(err) {
		if err := os.Rename(stage, generation); err != nil {
			return InstalledInventory{}, err
		}
	} else if err != nil {
		return InstalledInventory{}, err
	}
	if _, err := inspectGeneration(home, generation); err != nil {
		return InstalledInventory{}, err
	}
	if err := syncDirectory(store); err != nil {
		return InstalledInventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstalledInventory{}, err
	}
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return InstalledInventory{}, err
		}
	}
	// Recheck immediately before publication; never overwrite even a late local
	// edit. Same-user hostile filesystem writers are not a host-sandbox boundary.
	if _, err := os.Lstat(filepath.Join(home, "skills")); err == nil {
		if _, err := inspectInstalled(home); err != nil {
			return InstalledInventory{}, err
		}
	} else if !os.IsNotExist(err) {
		return InstalledInventory{}, err
	}
	linkDir, err := os.MkdirTemp(store, ".pointer-")
	if err != nil {
		return InstalledInventory{}, err
	}
	defer os.RemoveAll(linkDir)
	link := filepath.Join(linkDir, "skills")
	target := filepath.Join(managedBundles, filepath.Base(generation), "skills")
	if err := os.Symlink(target, link); err != nil {
		return InstalledInventory{}, err
	}
	if err := ctx.Err(); err != nil {
		return InstalledInventory{}, err
	}
	if err := os.Rename(link, filepath.Join(home, "skills")); err != nil {
		return InstalledInventory{}, err
	}
	if err := syncDirectory(home); err != nil {
		return InstalledInventory{}, fmt.Errorf("inventory publication outcome requires inventory readback: %w", err)
	}
	result, err := inspectInstalled(home)
	if err != nil {
		return InstalledInventory{}, err
	}
	if result.SourceRevision != revision || result.ContentDigest != manifest.Bundle.ContentDigest || result.ArchiveDigest != digest {
		return InstalledInventory{}, deny("installed_readback_mismatch", "", "published inventory differs from verified artifact")
	}
	return result, nil
}

// InspectInstalled rechecks every distributed byte, not just SKILL.md or a
// remembered version. It does not qualify runtime capabilities or behavior.
func InspectInstalled(home string) (InstalledInventory, error) {
	if err := privateDirectory(home); err != nil {
		return InstalledInventory{}, err
	}
	store := filepath.Join(home, managedBundles)
	if err := privateDirectory(store); err != nil {
		return InstalledInventory{}, err
	}
	unlock, err := lockInventory(filepath.Join(store, "lock"))
	if err != nil {
		return InstalledInventory{}, err
	}
	defer unlock()
	return inspectInstalled(home)
}

func inspectInstalled(home string) (InstalledInventory, error) {
	target, err := os.Readlink(filepath.Join(home, "skills"))
	if err != nil {
		return InstalledInventory{}, deny("unmanaged_skill_inventory", "skills", "refusing to adopt or replace an unmanaged inventory")
	}
	parts := strings.Split(filepath.ToSlash(target), "/")
	if len(parts) != 3 || parts[0] != managedBundles || !digestPattern.MatchString("sha256:"+parts[1]) || parts[2] != "skills" {
		return InstalledInventory{}, deny("unsafe_inventory_pointer", "skills", "pointer must name one exact managed generation")
	}
	return inspectGeneration(home, filepath.Join(home, managedBundles, parts[1]))
}

func inspectGeneration(home, generation string) (InstalledInventory, error) {
	if err := privateDirectory(generation); err != nil {
		return InstalledInventory{}, err
	}
	receiptBytes, err := readRegular(filepath.Join(generation, "receipt.json"), 4096)
	if err != nil {
		return InstalledInventory{}, err
	}
	var receipt installReceipt
	if err := decodeStrictJSON(receiptBytes, &receipt); err != nil {
		return InstalledInventory{}, err
	}
	if receipt.ArchiveDigest != "sha256:"+filepath.Base(generation) || !exactRevision(receipt.SourceRevision) {
		return InstalledInventory{}, deny("installed_receipt_mismatch", "", "receipt does not name the immutable generation")
	}
	archive := filepath.Join(generation, "bundle.tar.gz")
	archiveBytes, err := readRegular(archive, maxInstallArchiveBytes)
	if err != nil {
		return InstalledInventory{}, err
	}
	if sha256Digest(archiveBytes) != receipt.ArchiveDigest {
		return InstalledInventory{}, deny("installed_archive_drift", "", "retained archive digest differs")
	}
	check, err := os.MkdirTemp(filepath.Join(home, managedBundles), ".inspect-")
	if err != nil {
		return InstalledInventory{}, err
	}
	defer os.RemoveAll(check)
	expected, err := verifyArchiveInto(archive, receipt.SourceRevision, check)
	if err != nil {
		return InstalledInventory{}, err
	}
	files, err := archiveFiles(check)
	if err != nil {
		return InstalledInventory{}, err
	}
	if err := checkInstalledLayout(home, generation, files); err != nil {
		return InstalledInventory{}, err
	}
	for _, file := range files {
		want, err := readRegular(filepath.Join(check, file), MaxManifestBytes)
		if err != nil {
			return InstalledInventory{}, err
		}
		got, err := readRegular(filepath.Join(generation, file), MaxManifestBytes)
		if err != nil {
			return InstalledInventory{}, err
		}
		if !bytes.Equal(want, got) {
			return InstalledInventory{}, deny("installed_inventory_drift", file, "installed bytes differ from exact archive")
		}
	}
	result := InstalledInventory{EvidenceClass: "installed_inventory_verification", ArchiveDigest: receipt.ArchiveDigest, SourceRevision: receipt.SourceRevision, Version: expected.Bundle.Version, ContentDigest: expected.Bundle.ContentDigest}
	for _, skill := range expected.Skills {
		result.Skills = append(result.Skills, skill.Slug)
	}
	return result, nil
}

// Hermes owns .hub operational metadata, not bundle source or authority. Its
// one fixed link is outside generation content and survives update/rollback.
// No metadata contents are read or treated as inventory/provenance evidence.
func checkInstalledLayout(home, generation string, files []string) error {
	if err := privateDirectory(filepath.Join(home, managedBundles, "hub")); err != nil {
		return err
	}
	link, err := os.Readlink(filepath.Join(generation, "skills", ".hub"))
	if err != nil || link != "../../hub" {
		return deny("unsafe_hub_metadata", "skills/.hub", "exact managed metadata pointer required")
	}
	allowed := map[string]bool{"skills": true}
	for _, file := range files {
		allowed[filepath.ToSlash(file)] = true
		for dir := filepath.Dir(file); dir != "."; dir = filepath.Dir(dir) {
			allowed[filepath.ToSlash(dir)] = true
		}
	}
	return filepath.WalkDir(filepath.Join(generation, "skills"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(generation, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "skills/.hub" {
			return nil
		}
		if !allowed[rel] {
			return deny("installed_inventory_drift", rel, "undeclared installed path")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && (!info.Mode().IsRegular() || info.Mode().Perm()&0111 != 0)) {
			return deny("unsafe_inventory_file", rel, "regular non-executable content and real directories required")
		}
		return nil
	})
}

func privateDirectory(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return deny("unsafe_inventory_path", "", "absolute clean destination path required")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if resolved != path || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return deny("unsafe_inventory_path", "", "destination and ancestors must not be symlinks; destination must not be group/world writable")
	}
	return nil
}

func readRegular(path string, limit int) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, deny("unsafe_inventory_file", "", "bounded regular file required")
	}
	return readBounded(path, int64(limit))
}

func syncedFile(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncTree(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		syncErr := file.Sync()
		closeErr := file.Close()
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := syncDirectory(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
