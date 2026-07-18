//go:build !linux

package command

import (
	"context"
	"os"

	"golang.org/x/term"
)

const protectedIntakeCancellationSafe = false

func readProtectedTerminalLine(ctx context.Context, file *os.File, _ int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return term.ReadPassword(int(file.Fd()))
}
