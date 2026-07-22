//go:build linux

package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const protectedIntakeCancellationSafe = true

var (
	protectedPasteStart = []byte("\x1b[200~")
	protectedPasteEnd   = []byte("\x1b[201~")
)

func discardProtectedTerminalInput(file *os.File) {
	_ = unix.IoctlSetInt(int(file.Fd()), unix.TCFLSH, unix.TCIFLUSH)
}

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
			if len(value) == 0 && one[0] == protectedPasteStart[0] {
				sequence, sequenceErr := readProtectedSequence(ctx, file, poll, protectedPasteStart[1:])
				if sequenceErr != nil {
					wipeSecret(value)
					return nil, sequenceErr
				}
				if bytes.Equal(sequence, protectedPasteStart[1:]) {
					wipeSecret(sequence)
					return readProtectedPaste(ctx, file, poll, value, maximum)
				}
				if len(value)+1+len(sequence) > maximum {
					wipeSecret(sequence)
					return nil, errors.New("protected value exceeds the configured limit")
				}
				value = append(value, one[0])
				value = append(value, sequence...)
				wipeSecret(sequence)
				continue
			}
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

func readProtectedPaste(ctx context.Context, file *os.File, poll []unix.PollFd, value []byte, maximum int) ([]byte, error) {
	matched := 0
	one := make([]byte, 1)
	for {
		count, err := readProtectedByte(ctx, file, poll, one)
		if err != nil {
			wipeSecret(value)
			return nil, err
		}
		if count != 1 {
			continue
		}
		current := one[0]
		if current == protectedPasteEnd[matched] {
			matched++
			if matched == len(protectedPasteEnd) {
				if err = discardProtectedLineRemainder(ctx, file, poll); err != nil {
					wipeSecret(value)
					return nil, err
				}
				return value, nil
			}
			continue
		}
		if matched > 0 {
			if len(value)+matched > maximum {
				wipeSecret(value)
				return nil, errors.New("protected value exceeds the configured limit")
			}
			value = append(value, protectedPasteEnd[:matched]...)
			matched = 0
			if current == protectedPasteEnd[0] {
				matched = 1
				continue
			}
		}
		if len(value) >= maximum {
			wipeSecret(value)
			return nil, errors.New("protected value exceeds the configured limit")
		}
		value = append(value, current)
	}
}

func readProtectedSequence(ctx context.Context, file *os.File, poll []unix.PollFd, expected []byte) ([]byte, error) {
	sequence := make([]byte, 0, len(expected))
	one := make([]byte, 1)
	for range expected {
		count, err := readProtectedByte(ctx, file, poll, one)
		if err != nil {
			wipeSecret(sequence)
			return nil, err
		}
		if count == 1 {
			sequence = append(sequence, one[0])
		}
	}
	return sequence, nil
}

func discardProtectedLineRemainder(ctx context.Context, file *os.File, poll []unix.PollFd) error {
	one := make([]byte, 1)
	for {
		count, err := readProtectedByte(ctx, file, poll, one)
		if err != nil {
			return err
		}
		if count == 1 && (one[0] == '\n' || one[0] == '\r') {
			return nil
		}
	}
}

func readProtectedByte(ctx context.Context, file *os.File, poll []unix.PollFd, one []byte) (int, error) {
	for {
		if err := ctx.Err(); err != nil {
			_ = unix.IoctlSetInt(int(file.Fd()), unix.TCFLSH, unix.TCIFLUSH)
			return 0, err
		}
		ready, err := unix.Poll(poll, 50)
		if errors.Is(err, unix.EINTR) || ready == 0 {
			continue
		}
		if err != nil {
			return 0, err
		}
		count, readErr := file.Read(one)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return 0, io.EOF
			}
			return 0, readErr
		}
		return count, nil
	}
}
