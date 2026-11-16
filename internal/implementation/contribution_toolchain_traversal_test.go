package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestToolchainTraversalDirectoryCustody(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin/go", "VERSION", "go.env", "src/runtime/runtime.go", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/compile", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/link", "src/extra/input.go"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	driver := filepath.Join(root, "bin/go")
	if _, _, err := freezeToolchain(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink-directory", "writable-directory", "hardlink-file", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			extra := filepath.Join(root, "src/extra")
			ctx := context.Background()
			switch kind {
			case "symlink-directory":
				held := filepath.Join(t.TempDir(), "held")
				if err := os.Rename(extra, held); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(held, extra); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Remove(extra); _ = os.Rename(held, extra) }()
			case "writable-directory":
				if err := os.Chmod(extra, 0777); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(extra, 0700)
			case "hardlink-file":
				alias := filepath.Join(extra, "alias.go")
				if err := os.Link(filepath.Join(extra, "input.go"), alias); err != nil {
					t.Fatal(err)
				}
				defer os.Remove(alias)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, _, err := freezeToolchain(ctx, driver); err == nil {
				t.Fatal("unsafe traversal accepted")
			} else if kind == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
