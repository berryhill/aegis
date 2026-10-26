package recoveryphysical

import (
	"errors"
	"os"
	"syscall"
)

func ownedByCurrentEUID(i os.FileInfo) bool {
	s, ok := i.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Geteuid())
}
func requireSecureDirectory(p string) error {
	i, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if !i.IsDir() || i.Mode().Perm() != 0700 || !ownedByCurrentEUID(i) {
		return errors.New("unsafe recovery directory")
	}
	return nil
}
func syncDirectory(p string) error {
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
