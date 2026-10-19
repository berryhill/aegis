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

// WithOfflineTransport serializes authenticated offline maintenance with API
// startup. admit must freshly validate the caller's exact stopped service and
// authority; this helper grants no reset authority. Orphans are retained, never
// deleted. The startup lock remains held until validate returns.
func WithOfflineTransport(ctx context.Context, socket string, admit func() error, validate func() error) error {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || admit == nil || validate == nil {
		return errors.New("exact offline transport and admission required")
	}
	if err := admit(); err != nil {
		return err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { unix.Close(fd) }()
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(socket), "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(err, unix.ENOENT) {
			// A never-started gateway may have no transport directory. Prepare
			// only its configured path beneath an owned, non-writable anchor;
			// API startup uses the same lock after directory preparation.
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := admit(); err != nil {
				return err
			}
			var anchor unix.Stat_t
			if err := unix.Fstat(fd, &anchor); err != nil {
				return err
			}
			if anchor.Uid != uint32(os.Geteuid()) || anchor.Gid != uint32(os.Getegid()) || anchor.Mode&0022 != 0 {
				return fmt.Errorf("unsafe missing transport directory anchor: uid=%d gid=%d mode=%o", anchor.Uid, anchor.Gid, anchor.Mode&0777)
			}
			if err := unix.Mkdirat(fd, part, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
				return err
			}
			if err := unix.Fsync(fd); err != nil {
				return err
			}
			next, err = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		if err != nil {
			return err
		}
		unix.Close(fd)
		fd = next
	}
	var parent unix.Stat_t
	if err := unix.Fstat(fd, &parent); err != nil {
		return err
	}
	if parent.Uid != uint32(os.Geteuid()) || parent.Gid != uint32(os.Getegid()) || parent.Mode&0022 != 0 {
		return errors.New("unsafe transport directory")
	}
	name := filepath.Base(socket)
	unlock, err := lockOrphanTransport(fd, name)
	if err != nil {
		return err
	}
	defer unlock()
	pinned := fmt.Sprintf("/proc/self/fd/%d/%s", fd, name)
	if err := admit(); err != nil {
		return err
	}
	if _, err := os.Lstat(pinned); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		artifact, err := inspectOrphan(ctx, pinned)
		if err != nil {
			return err
		}
		if err := admit(); err != nil {
			return err
		}
		fresh, err := inspectOrphan(ctx, pinned)
		if err != nil {
			return err
		}
		if fresh.identity != artifact.identity {
			return errors.New("transport changed before quarantine")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		evidence := fmt.Sprintf("%s.upgrade-orphan-%x-%x", name, artifact.identity.Device, artifact.identity.Inode)
		if err := unix.Renameat2(fd, name, fd, evidence, unix.RENAME_NOREPLACE); err != nil {
			return err
		}
		if err := unix.Fsync(fd); err != nil {
			return err
		}
		info, err := os.Lstat(fmt.Sprintf("/proc/self/fd/%d/%s", fd, evidence))
		if err != nil {
			return err
		}
		id, err := socketIdentity(info)
		if err != nil || id.Device != artifact.identity.Device || id.Inode != artifact.identity.Inode {
			return errors.New("quarantine identity mismatch")
		}
	}
	if _, err := os.Lstat(pinned); !errors.Is(err, os.ErrNotExist) {
		return errors.New("offline transport absence unverified")
	}
	if err := admit(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return validate()
}
