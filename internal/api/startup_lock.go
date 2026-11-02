package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type startupLockKey struct{}
type startupLease struct {
	socket string
	file   *os.File
}

// AcquireStartupLock excludes offline maintenance before service construction.
// Keep release until all service stores have closed. Serve reuses only this
// private context-bound lease, never a caller-provided file or boolean.
func AcquireStartupLock(ctx context.Context, socket string) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if socket == "" {
		return ctx, func() {}, nil
	}
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return ctx, nil, errors.New("exact API transport required")
	}
	if err := ensureSocketDirectory(filepath.Dir(socket)); err != nil {
		return ctx, nil, err
	}
	file, err := acquireSingleton(socket + ".lock")
	if err != nil {
		return ctx, nil, err
	}
	lease := &startupLease{socket: socket, file: file}
	var once sync.Once
	release := func() { once.Do(func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }) }
	return context.WithValue(ctx, startupLockKey{}, lease), release, nil
}

func startupLockHeld(ctx context.Context, socket string) bool {
	lease, ok := ctx.Value(startupLockKey{}).(*startupLease)
	if !ok || lease.socket != socket {
		return false
	}
	// Closed leases cannot be reused after service cleanup.
	_, err := lease.file.Stat()
	return err == nil
}
