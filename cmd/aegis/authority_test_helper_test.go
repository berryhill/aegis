package main

import (
	"context"
	"path/filepath"
	"testing"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

func initializeOperationalAuthority(t *testing.T, statePath string) {
	t.Helper()
	if _, err := authoritybadger.Initialize(context.Background(), filepath.Join(statePath, "persistence", "authority-v1")); err != nil {
		t.Fatal(err)
	}
}
