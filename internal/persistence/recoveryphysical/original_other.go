//go:build !linux

package recoveryphysical

import (
	"context"
	"errors"
)

func OriginalCopy(context.Context, string, string) (string, error) {
	return "", errors.New("original engine custody qualification requires Linux")
}
func OriginalDirectory(context.Context, string, string) (string, error) {
	return "", errors.New("original engine custody qualification requires Linux")
}
