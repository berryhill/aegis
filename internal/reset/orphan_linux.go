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
// Lock the same inode used by API startup; never unlink this coordination file.
func lockOrphanTransport(dir int, name string) (func(), error) {
	fd, err := unix.Openat(dir, name+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	closeLock := func() { unix.Close(fd) }
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		closeLock()
		return nil, errors.New("transport lifecycle lock is held; stop its verified owner")
	}
	var held, named unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		closeLock()
		return nil, err
	}
	if err := unix.Fstatat(dir, name+".lock", &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		closeLock()
		return nil, err
	}
	if held.Dev != named.Dev || held.Ino != named.Ino || held.Mode&unix.S_IFMT != unix.S_IFREG || held.Mode&0777 != 0600 || held.Uid != uint32(os.Geteuid()) || held.Gid != uint32(os.Getegid()) || held.Nlink != 1 {
		closeLock()
		return nil, errors.New("unsafe or replaced transport lifecycle lock")
	}
	return closeLock, nil
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
	unlock, err := lockOrphanTransport(fd, filepath.Base(artifact.Path))
	if err != nil {
		return err
	}
	defer unlock()
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
	if err := unix.Fsync(fd); err != nil {
		return err
	}
	if err := unix.Fstatat(fd, filepath.Base(artifact.Path), &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		return errors.New("transport absence postcondition failed")
	}
	return nil
}
