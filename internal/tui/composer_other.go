//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package tui

import (
	"context"
	"errors"
	"os"
)

func readByteContext(context.Context, *os.File, []byte) (int, error) {
	return 0, errors.New("rich cancellation-safe composer is unavailable on this platform")
}
