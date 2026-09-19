package reset

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Only the executable-selected development profile grants this narrow scope.
// Missing configuration is never authority to inventory or wipe its state tree.
func (s *Service) addOrphanTransport(ctx context.Context, plan *Plan, home string) error {
	if s.RepositoryResetRoot == "" || plan.ConfigPath != filepath.Join(s.RepositoryResetRoot, "aegis.yaml") {
		return nil
	}
	socket := filepath.Join(s.RepositoryResetRoot, "state", "transport", "aegis.sock")
	if err := validateScopedPath(socket, home, s.RepositoryResetRoot); err != nil {
		return deny(err)
	}
	if _, err := os.Lstat(socket); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return deny(err)
	}
	artifact, err := inspectOrphan(ctx, socket)
	if err != nil {
		return deny(err)
	}
	for dir := filepath.Dir(socket); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return deny(err)
		}
		id, err := identity(info, true)
		if err != nil {
			return deny(err)
		}
		plan.Artifacts = append(plan.Artifacts, Artifact{Path: dir, Kind: "directory", Status: "preserve-anchor", identity: id})
		if dir == s.RepositoryResetRoot {
			break
		}
	}
	plan.Artifacts = append(plan.Artifacts, artifact)
	return nil
}

func socketIdentity(info os.FileInfo) (Identity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || info.Mode()&os.ModeSymlink != 0 || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || stat.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
		return Identity{}, errors.New("orphan transport must be a single-link operator-owned non-writable-by-others Unix socket")
	}
	return Identity{Device: uint64(stat.Dev), Inode: stat.Ino, Mode: uint32(info.Mode()), UID: stat.Uid, GID: stat.Gid, Links: uint64(stat.Nlink), Size: info.Size(), ModTime: info.ModTime().UnixNano()}, nil
}

func inspectOrphan(ctx context.Context, path string) (Artifact, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Artifact{}, err
	}
	if err := requireUnboundSocket(path); err != nil {
		return Artifact{}, err
	}
	id, err := socketIdentity(info)
	if err != nil {
		return Artifact{}, err
	}
	connection, err := (&net.Dialer{Timeout: 250 * time.Millisecond}).DialContext(ctx, "unix", path)
	if err == nil {
		connection.Close()
		return Artifact{}, errors.New("transport has a live listener; stop its verified owner before reset")
	}
	// Timeout, permission denial, wrong socket type, and disappearance are not stale evidence.
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return Artifact{}, fmt.Errorf("transport liveness is unverified: %w", err)
	}
	after, err := os.Lstat(path)
	if err != nil {
		return Artifact{}, err
	}
	fresh, err := socketIdentity(after)
	if err != nil || fresh != id {
		return Artifact{}, errors.New("transport identity changed during liveness probe")
	}
	return Artifact{Path: path, Kind: "orphan-socket", Status: "delete", identity: id}, nil
}
