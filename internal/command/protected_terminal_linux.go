//go:build linux

package command

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const protectedIntakeCancellationSafe = true

func readProtectedTerminalLine(ctx context.Context, file *os.File, maximum int) ([]byte, error) {
	value := make([]byte, 0, min(maximum, 4096))
	poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	one := make([]byte, 1)
	for {
		if err := ctx.Err(); err != nil {
			wipeSecret(value)
			_ = unix.IoctlSetInt(int(file.Fd()), unix.TCFLSH, unix.TCIFLUSH)
			return nil, err
		}
		ready, err := unix.Poll(poll, 50)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			wipeSecret(value)
			return nil, err
		}
		if ready == 0 {
			continue
		}
		count, readErr := file.Read(one)
		if count == 1 {
			if one[0] == '\n' || one[0] == '\r' {
				return value, nil
			}
			if len(value) >= maximum {
				wipeSecret(value)
				_ = unix.IoctlSetInt(int(file.Fd()), unix.TCFLSH, unix.TCIFLUSH)
				return nil, errors.New("protected value exceeds the configured limit")
			}
			value = append(value, one[0])
		}
		if readErr != nil {
			wipeSecret(value)
			if errors.Is(readErr, io.EOF) {
				return nil, io.EOF
			}
			return nil, readErr
		}
	}
}
