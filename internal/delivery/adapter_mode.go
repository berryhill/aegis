package delivery

import (
	"context"
	"sync/atomic"
)

// adapterMode is minted only by the closed adapter entrypoints. It is neither
// productive admission nor a caller-selectable option. Lifetime is synchronous.
type adapterModeKey struct{}
type adapterMode struct {
	intent Intent
	read   bool
	parent *adapterMode
	active atomic.Bool
}

func enterAdapterMode(ctx context.Context, i Intent, read bool) (context.Context, *adapterMode) {
	m := &adapterMode{intent: i, read: read}
	if ctx != nil {
		m.parent, _ = ctx.Value(adapterModeKey{}).(*adapterMode)
	} else {
		ctx = context.Background()
	}
	m.active.Store(true)
	return context.WithValue(ctx, adapterModeKey{}, m), m
}
func (m *adapterMode) live() bool {
	for p := m; p != nil; p = p.parent {
		if !p.active.Load() {
			return false
		}
	}
	return m != nil
}

// CheckHistoricalReadMode is a read-only predicate, not a permit constructor.
// Every enclosing adapter call must also be a live read of this entire intent.
// Thus a nested Reconcile cannot launder an Apply into cold credential use.
func CheckHistoricalReadMode(ctx context.Context, i Intent) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrDenied
	}
	m, _ := ctx.Value(adapterModeKey{}).(*adapterMode)
	if !m.live() {
		return ErrDenied
	}
	for p := m; p != nil; p = p.parent {
		if !p.read || p.intent != i {
			return ErrDenied
		}
	}
	return nil
}
func checkAdapterHTTP(ctx context.Context, method string) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrDenied
	}
	m, _ := ctx.Value(adapterModeKey{}).(*adapterMode)
	if !m.live() {
		return ErrDenied
	}
	for p := m; p != nil; p = p.parent {
		if p.read && method != "GET" {
			return ErrDenied
		}
	}
	return nil
}
