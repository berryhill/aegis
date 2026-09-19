//go:build linux

package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Walk from / without following any ancestor; retain descriptors while checking
// the approved private profile anchors. Same-UID hostile processes are not a
// sandbox boundary, but path substitution must never redirect deletion.
func requireUnboundSocket(path string) error {
	data, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return errors.New("cannot verify kernel Unix socket inventory")
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && strings.Join(fields[7:], " ") == path {
			return errors.New("transport is still bound in the kernel; stop its verified owner")
		}
	}
	return nil
}

func removeOrphan(ctx context.Context, artifact Artifact, artifacts []Artifact) error {
	anchors := map[string]Identity{}
	for _, a := range artifacts {
		if a.Status == "preserve-anchor" {
			anchors[a.Path] = a.identity
		}
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { unix.Close(fd) }()
	path := ""
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(artifact.Path), "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		unix.Close(fd)
		fd = next
		path += "/" + part
		if expected, ok := anchors[path]; ok {
			var st unix.Stat_t
			if err := unix.Fstat(fd, &st); err != nil {
				return err
			}
			if uint64(st.Dev) != expected.Device || st.Ino != expected.Inode || st.Uid != expected.UID || st.Gid != expected.GID || st.Mode&0022 != 0 {
				return errors.New("approved transport ancestor changed")
			}
		}
	}
	if err := requireUnboundSocket(artifact.Path); err != nil {
		return err
	}
	// Probe via the pinned directory, not the mutable absolute pathname.
	pinned := fmt.Sprintf("/proc/self/fd/%d/%s", fd, filepath.Base(artifact.Path))
	fresh, err := inspectOrphan(ctx, pinned)
	if err != nil {
		return err
	}
	if fresh.identity != artifact.identity {
		return errors.New("approved transport identity changed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstatat(fd, filepath.Base(artifact.Path), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if uint64(st.Dev) != artifact.identity.Device || st.Ino != artifact.identity.Inode || st.Mode&unix.S_IFMT != unix.S_IFSOCK {
		return errors.New("transport changed before removal")
	}
	if err := unix.Unlinkat(fd, filepath.Base(artifact.Path), 0); err != nil {
		return err
	}
	return unix.Fsync(fd)
}
