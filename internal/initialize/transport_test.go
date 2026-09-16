package initialize

import (
	"context"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
)

// Keep real Unix socket paths below the platform sockaddr limit.
func transportTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "aegis-init-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func placeTransport(t *testing.T, path, kind string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "regular":
		if err := os.WriteFile(path, []byte("owned transport placeholder"), 0600); err != nil {
			t.Fatal(err)
		}
	case "symlink":
		if err := os.Symlink("missing-owner-target", path); err != nil {
			t.Fatal(err)
		}
	case "live", "stale":
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		listener.SetUnlinkOnClose(false)
		if kind == "stale" {
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Cleanup(func() { _ = listener.Close() })
		}
	}
}

type artifactSnapshot struct {
	Mode    fs.FileMode
	Content string
}

func snapshotInitialization(t *testing.T, root string) map[string]artifactSnapshot {
	t.Helper()
	result := map[string]artifactSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := artifactSnapshot{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value.Content = string(data)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			value.Content = target
		}
		result[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPlanRejectsPresentTransportWithoutMutation(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "live", "stale"} {
		t.Run(kind, func(t *testing.T) {
			root := transportTestRoot(t)
			state := filepath.Join(root, "state")
			socket := filepath.Join(state, "transport", "aegis.sock")
			placeTransport(t, socket, kind)
			before := snapshotInitialization(t, root)
			_, err := testService(t).Plan(filepath.Join(root, "aegis.yaml"), state)
			if err == nil || !strings.Contains(err.Error(), "bootstrap_gateway_recovery_required") {
				t.Fatalf("expected transport denial, got %v", err)
			}
			if after := snapshotInitialization(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("Plan changed existing artifacts")
			}
		})
	}
}

func TestApplyRejectsTransportAppearingAfterPlanWithoutMutation(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "live", "stale"} {
		t.Run(kind, func(t *testing.T) {
			root := transportTestRoot(t)
			partial := filepath.Join(root, config.InitializationTemporaryPrefix+"retained")
			if err := os.WriteFile(partial, []byte("retained partial configuration\n"), 0600); err != nil {
				t.Fatal(err)
			}
			service := testService(t)
			plan, err := service.Plan(filepath.Join(root, "aegis.yaml"), filepath.Join(root, "state"))
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Partials) != 1 {
				t.Fatalf("expected recognized partial, got %v", plan.Partials)
			}
			plan, err = service.EnrollPrincipalPassword(plan, []byte("principal-password"))
			if err != nil {
				t.Fatal(err)
			}
			placeTransport(t, plan.UnixSocket, kind)
			if err := os.WriteFile(plan.TokenPath, plan.token, 0600); err != nil {
				t.Fatal(err)
			}
			before := snapshotInitialization(t, root)
			err = service.Apply(context.Background(), plan)
			if err == nil || !strings.Contains(err.Error(), "bootstrap_gateway_recovery_required") {
				t.Fatalf("expected transport denial, got %v", err)
			}
			if after := snapshotInitialization(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("Apply changed artifacts, modes, or retained partial configuration")
			}
		})
	}
}

func TestInitializationWithoutTransportSucceeds(t *testing.T) {
	root := transportTestRoot(t)
	service := testService(t)
	plan, err := service.Plan(filepath.Join(root, "aegis.yaml"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err = service.EnrollPrincipalPassword(plan, []byte("principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateValid || inspection.Config.API.UnixSocket != plan.UnixSocket {
		t.Fatalf("unexpected initialized configuration: %+v", inspection)
	}
	if _, err := os.Lstat(plan.UnixSocket); !os.IsNotExist(err) {
		t.Fatalf("initialization created transport: %v", err)
	}
}
