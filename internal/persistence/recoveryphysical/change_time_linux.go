package recoveryphysical

import (
	"os"
	"syscall"
)

func SameChangeTime(a, b os.FileInfo) bool {
	return a.Sys().(*syscall.Stat_t).Ctim == b.Sys().(*syscall.Stat_t).Ctim
}
