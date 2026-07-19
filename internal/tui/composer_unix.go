//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package tui

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func readByteContext(ctx context.Context, file *os.File, target []byte) (int, error) {
	poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		ready, err := unix.Poll(poll, 50)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready > 0 {
			return file.Read(target)
		}
	}
}
