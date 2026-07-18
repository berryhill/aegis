//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package command

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func readFileLineContext(ctx context.Context, file *os.File, maximum int) (string, bool, error) {
	buffer := make([]byte, 0, min(maximum, 4096))
	poll := []unix.PollFd{{Fd: int32(file.Fd()), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
	one := make([]byte, 1)
	for {
		if err := ctx.Err(); err != nil {
			wipeSecret(buffer)
			return "", false, err
		}
		ready, err := unix.Poll(poll, 50)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			wipeSecret(buffer)
			return "", false, err
		}
		if ready == 0 {
			continue
		}
		count, readErr := file.Read(one)
		if count == 1 {
			if one[0] == '\n' {
				if len(buffer) > 0 && buffer[len(buffer)-1] == '\r' {
					buffer = buffer[:len(buffer)-1]
				}
				return string(buffer), false, nil
			}
			if len(buffer) >= maximum {
				wipeSecret(buffer)
				return "", false, errTerminalLineTooLong
			}
			buffer = append(buffer, one[0])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if len(buffer) == 0 {
					return "", true, nil
				}
				return string(buffer), false, nil
			}
			wipeSecret(buffer)
			return "", false, readErr
		}
	}
}
