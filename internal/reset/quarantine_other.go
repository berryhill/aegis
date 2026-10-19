//go:build !linux

package reset

import (
	"context"
	"errors"
)

func WithOfflineTransport(_ context.Context, _ string, _ func() error, _ func() error) error {
	return errors.New("authenticated offline transport quarantine requires Linux")
}
