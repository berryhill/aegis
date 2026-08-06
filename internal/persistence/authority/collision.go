// Package authority owns the typed persistence boundary for session authority.
package authority

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var LegacyDirectories = [...]string{"mandates", "authority-contexts", "authority-revocations"}

type CollisionState string

const (
	CollisionAbsent CollisionState = "absent"
	CollisionEmpty  CollisionState = "securely-empty"
	CollisionUnsafe CollisionState = "collision"
)

type Collision struct {
	State CollisionState
	Paths []string
}

// ClassifyLegacyAuthority is the shared clean-install gate. Existing authority
// JSON paths are acceptable only when every entry is a real, operator-owned,
// mode-0700 directory and the complete tree contains no non-directory entry.
func ClassifyLegacyAuthority(stateRoot string) (Collision, error) {
	absolute, err := filepath.Abs(stateRoot)
	if err != nil || filepath.Clean(absolute) != absolute || absolute == filepath.VolumeName(absolute)+string(filepath.Separator) {
		return Collision{State: CollisionUnsafe}, errors.New("authority state root must be a clean absolute non-root path")
	}
	if err = verifyRootIfPresent(absolute); err != nil {
		return Collision{State: CollisionUnsafe, Paths: []string{absolute}}, err
	}
	result := Collision{State: CollisionAbsent}
	for _, name := range LegacyDirectories {
		path := filepath.Join(absolute, name)
		present, inspectErr := proveSecureEmpty(path)
		if inspectErr != nil {
			result.State = CollisionUnsafe
			result.Paths = append(result.Paths, path)
			return result, fmt.Errorf("legacy authority collision at %s: %w", path, inspectErr)
		}
		if present {
			result.State = CollisionEmpty
			result.Paths = append(result.Paths, path)
		}
	}
	return result, nil
}

func verifyRootIfPresent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect authority state root: %w", err)
	}
	return secureOwnedDirectory(info)
}

func proveSecureEmpty(root string) (bool, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = secureOwnedDirectory(info); err != nil {
		return true, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		child, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if !entry.IsDir() {
			return errors.New("authority directory is populated")
		}
		return secureOwnedDirectory(child)
	})
	return true, err
}

func secureOwnedDirectory(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("path is not a real directory")
	}
	if int(stat.Uid) != os.Geteuid() {
		return errors.New("directory is not owned by the effective operator")
	}
	if info.Mode().Perm() != 0700 {
		return fmt.Errorf("directory mode is %04o, want 0700", info.Mode().Perm())
	}
	return nil
}
