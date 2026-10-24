//go:build unix

package implementation

import (
	"golang.org/x/sys/unix"
	"os"
)

// No-follow rejects a replaced symlink before opening it; nonblock prevents a
// raced FIFO replacement from blocking before the descriptor's type is checked.
func openRepositoryContextFile(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
