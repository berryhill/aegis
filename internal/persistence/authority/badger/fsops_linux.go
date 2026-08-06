//go:build linux

package badger

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func writeFileAtomicAt(parent, name string, content []byte) error {
	dir, err := openDirectoryNoFollow(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	var nonce [12]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := "." + name + "-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(int(dir.Fd()), temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), temporary)
	defer func() { _ = unix.Unlinkat(int(dir.Fd()), temporary, 0) }()
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = unix.Renameat(int(dir.Fd()), temporary, int(dir.Fd()), name); err != nil {
		return err
	}
	return unix.Fsync(int(dir.Fd()))
}

func openDirectoryNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func removeFileAt(parent, name string) error {
	dir, err := openDirectoryNoFollow(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	var stat unix.Stat_t
	if err = unix.Fstatat(int(dir.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return errors.New("refuse to remove non-regular or hard-linked authority file")
	}
	if err = unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
		return err
	}
	return unix.Fsync(int(dir.Fd()))
}

func removeTreeAt(parent, name string) error {
	return removeTreeAtIdentity(parent, name, 0, 0, false)
}

func removeTreeAtVerified(parent, name string, device, inode uint64) error {
	return removeTreeAtIdentity(parent, name, device, inode, true)
}

func removeTreeAtIdentity(parent, name string, device, inode uint64, verify bool) error {
	dir, err := openDirectoryNoFollow(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = removeTreeFDIdentity(int(dir.Fd()), name, device, inode, verify); err != nil {
		return err
	}
	return unix.Fsync(int(dir.Fd()))
}

func fileIdentity(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("authority generation identity unavailable")
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func removeTreeFD(parentFD int, name string) error {
	return removeTreeFDIdentity(parentFD, name, 0, 0, false)
}

func removeTreeFDIdentity(parentFD int, name string, device, inode uint64, verify bool) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return errors.New("refuse to collect non-directory authority generation")
	}
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	if verify {
		var opened unix.Stat_t
		if err = unix.Fstat(fd, &opened); err != nil || uint64(opened.Dev) != device || opened.Ino != inode {
			_ = unix.Close(fd)
			return errors.New("authority generation identity changed before collection")
		}
	}
	file := os.NewFile(uintptr(fd), name)
	entries, readErr := file.ReadDir(-1)
	if readErr != nil {
		_ = file.Close()
		return readErr
	}
	for _, entry := range entries {
		child := entry.Name()
		if child == "." || child == ".." || filepath.Base(child) != child {
			_ = file.Close()
			return errors.New("invalid authority generation entry")
		}
		var childStat unix.Stat_t
		if err = unix.Fstatat(fd, child, &childStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			_ = file.Close()
			return err
		}
		switch childStat.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			err = removeTreeFD(fd, child)
		case unix.S_IFREG:
			if childStat.Nlink != 1 {
				err = errors.New("refuse hard-linked authority generation file")
			} else {
				err = unix.Unlinkat(fd, child, 0)
			}
		default:
			err = fmt.Errorf("refuse special authority generation entry %s", child)
		}
		if err != nil {
			_ = file.Close()
			return err
		}
	}
	if err = unix.Fsync(fd); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}

func availableBytes(path string) (uint64, error) {
	dir, err := openDirectoryNoFollow(path)
	if err != nil {
		return 0, err
	}
	defer dir.Close()
	var stat unix.Statfs_t
	if err = unix.Fstatfs(int(dir.Fd()), &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
