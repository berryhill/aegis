//go:build linux

// Package safefs provides descriptor-anchored no-follow filesystem mutations.
package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// RemoveRelative removes one already-inventoried descendant relative to an
// anchored root descriptor. Each parent is opened with O_NOFOLLOW.
func RemoveRelative(root, relative string, directory bool) error {
	return removeRelative(root, relative, directory, 0, 0, false)
}

func RemoveRelativeIdentity(root, relative string, directory bool, device, inode uint64) error {
	return removeRelative(root, relative, directory, device, inode, true)
}

func removeRelative(root, relative string, directory bool, device, inode uint64, verify bool) error {
	if relative == "" || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("invalid anchored relative path")
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if verify {
		var stat unix.Stat_t
		if err = unix.Fstat(fd, &stat); err != nil || uint64(stat.Dev) != device || stat.Ino != inode {
			return errors.New("anchored root identity changed")
		}
	}
	parts := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	current := fd
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if current != fd {
			_ = unix.Close(current)
		}
		if openErr != nil {
			return openErr
		}
		current = next
	}
	if current != fd {
		defer unix.Close(current)
	}
	flags := 0
	if directory {
		flags = unix.AT_REMOVEDIR
	}
	if err = unix.Unlinkat(current, parts[len(parts)-1], flags); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	return unix.Fsync(current)
}

// EmptyDirectory removes every entry below path without re-resolving descendants
// through attacker-controlled parent pathnames. The root itself is retained.
func EmptyDirectory(path string) error {
	return emptyDirectory(path, 0, 0, false)
}

func EmptyDirectoryIdentity(path string, device, inode uint64) error {
	return emptyDirectory(path, device, inode, true)
}

func emptyDirectory(path string, device, inode uint64, verify bool) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open anchored directory %s: %w", path, err)
	}
	defer unix.Close(fd)
	if verify {
		var stat unix.Stat_t
		if err = unix.Fstat(fd, &stat); err != nil || uint64(stat.Dev) != device || stat.Ino != inode {
			return errors.New("anchored root identity changed")
		}
	}
	if err = empty(fd); err != nil {
		return fmt.Errorf("empty anchored directory %s: %w", path, err)
	}
	return unix.Fsync(fd)
}

func empty(fd int) error {
	dup, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dup), "anchored-directory")
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." {
			return errors.New("invalid directory entry")
		}
		var stat unix.Stat_t
		if err = unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			return fmt.Errorf("refuse symlink entry %s", name)
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			child, openErr := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
			if openErr != nil {
				return openErr
			}
			childErr := empty(child)
			if childErr == nil {
				childErr = unix.Fsync(child)
			}
			_ = unix.Close(child)
			if childErr != nil {
				return childErr
			}
			if err = unix.Unlinkat(fd, name, unix.AT_REMOVEDIR); err != nil {
				return err
			}
		} else {
			if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
				return fmt.Errorf("refuse non-regular or hard-linked entry %s", name)
			}
			if err = unix.Unlinkat(fd, name, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveFile opens and validates parentDir without following it and unlinks one
// basename relative to that descriptor.
func RemoveFile(parentDir, name string) error {
	fd, err := unix.Open(parentDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err = unix.Fstatat(fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 {
		return errors.New("refuse non-regular or hard-linked file")
	}
	if err = unix.Unlinkat(fd, name, 0); err != nil {
		return err
	}
	return unix.Fsync(fd)
}
