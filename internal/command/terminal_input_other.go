//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package command

import (
	"context"
	"os"
)

func readFileLineContext(ctx context.Context, file *os.File, maximum int) (string, bool, error) {
	return readBufferedLineContext(ctx, newTerminalInput(file).reader, maximum)
}
