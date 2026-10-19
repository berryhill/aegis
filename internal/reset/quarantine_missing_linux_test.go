//go:build linux

package reset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineTransportMissingDirectory(t *testing.T) {
	for _, kind := range []string{"missing", "denied", "cancelled", "unsafe-anchor"} {
		t.Run(kind, func(t *testing.T) {
			anchor := t.TempDir()
			if err := os.Chmod(anchor, 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "unsafe-anchor" {
				if err := os.Chmod(anchor, 0777); err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(anchor, "transport")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			validated := false
			err := WithOfflineTransport(ctx, filepath.Join(dir, "s"), func() error {
				if kind == "denied" {
					return errors.New("denied")
				}
				return nil
			}, func() error { validated = true; return nil })
			if kind == "missing" {
				if err != nil || !validated {
					t.Fatalf("missing directory: %v", err)
				}
				info, e := os.Stat(dir)
				if e != nil || info.Mode().Perm() != 0700 {
					t.Fatal("missing directory was not prepared privately")
				}
			} else {
				if err == nil || validated {
					t.Fatalf("unsafe missing directory accepted: %v", err)
				}
				if _, e := os.Lstat(dir); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("denied directory preparation mutated state")
				}
			}
		})
	}
}
