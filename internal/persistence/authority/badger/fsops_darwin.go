//go:build darwin

package badger

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func writeFileAtomicAt(parent, name string, content []byte) error {
	file, err := os.CreateTemp(parent, "."+name+"-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(content)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary, parent+string(os.PathSeparator)+name)
	}
	if err != nil {
		return err
	}
	return syncDirectory(parent)
}

// Darwin remains a build target but is not in the qualified persistence
// envelope. These helpers preserve current behavior; Linux supplies the
// descriptor-anchored implementation used for authority qualification.
func openDirectoryNoFollow(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("unsafe directory")
	}
	return os.Open(path)
}

func removeFileAt(parent, name string) error {
	info, err := os.Lstat(parent + string(os.PathSeparator) + name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Sys().(*unix.Stat_t).Nlink != 1 {
		return errors.New("refuse unsafe authority file")
	}
	if err = os.Remove(parent + string(os.PathSeparator) + name); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func removeTreeAt(parent, name string) error {
	return removeTreeAtIdentity(parent, name, 0, 0, false)
}

func removeTreeAtVerified(parent, name string, device, inode uint64) error {
	return removeTreeAtIdentity(parent, name, device, inode, true)
}

func removeTreeAtIdentity(parent, name string, device, inode uint64, verify bool) error {
	info, err := os.Lstat(parent + string(os.PathSeparator) + name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refuse unsafe authority generation")
	}
	if verify {
		gotDevice, gotInode, identityErr := fileIdentity(info)
		if identityErr != nil || gotDevice != device || gotInode != inode {
			return errors.New("authority generation identity changed before collection")
		}
	}
	if err = os.RemoveAll(parent + string(os.PathSeparator) + name); err != nil {
		return err
	}
	return syncDirectory(parent)
}

func fileIdentity(info os.FileInfo) (uint64, uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("authority generation identity unavailable")
	}
	return uint64(stat.Dev), stat.Ino, nil
}

func availableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}
