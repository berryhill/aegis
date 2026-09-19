//go:build linux

package reset

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/initialize"
)

func orphanFixture(t *testing.T) (fixture, string, *net.UnixListener) {
	t.Helper()
	f := newFixture(t)
	// Short Unix path independent of the test name length.
	f.service.RepositoryResetRoot = filepath.Join(f.home, "repo", ".aegis")
	f.config = filepath.Join(f.service.RepositoryResetRoot, "aegis.yaml")
	f.state = filepath.Join(f.service.RepositoryResetRoot, "state")
	socket := filepath.Join(f.state, "transport", "aegis.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	t.Cleanup(func() { l.Close() })
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	return f, socket, l
}

func TestOrphanResetThenInitialize(t *testing.T) {
	f, socket, l := orphanFixture(t)
	l.Close()
	init := initialize.New()
	if _, err := init.Plan(f.config, f.state); err == nil {
		t.Fatal("bootstrap accepted existing socket")
	}
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range plan.Artifacts {
		if a.Path == socket && a.Kind == "orphan-socket" {
			found = true
		}
	}
	if !found {
		t.Fatal("review omits socket")
	}
	if err := f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	next, err := init.Plan(f.config, f.state)
	if err != nil {
		t.Fatal(err)
	}
	next, err = init.EnrollPrincipalPassword(next, []byte("synthetic-orphan-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err := init.Apply(context.Background(), next); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanDenials(t *testing.T) {
	for _, kind := range []string{"live", "symlink", "regular", "writable", "foreign", "ancestor"} {
		t.Run(kind, func(t *testing.T) {
			f, socket, l := orphanFixture(t)
			if kind != "live" {
				l.Close()
			}
			switch kind {
			case "symlink":
				os.Remove(socket)
				if err := os.Symlink("missing", socket); err != nil {
					t.Fatal(err)
				}
			case "regular":
				os.Remove(socket)
				if err := os.WriteFile(socket, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(socket, 0666); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				f.service.RepositoryResetRoot = filepath.Join(f.home, "other")
				p, err := f.service.Plan(context.Background(), f.config)
				if err != nil {
					t.Fatal(err)
				}
				if len(p.Artifacts) != 0 {
					t.Fatal("foreign scope inventoried")
				}
				return
			case "ancestor":
				dir := filepath.Dir(socket)
				if err := os.Rename(dir, dir+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dir+"-saved", dir); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.service.Plan(context.Background(), f.config); err == nil {
				t.Fatal("unsafe orphan accepted")
			}
			if _, err := os.Lstat(socket); err != nil {
				t.Fatal("transport removed")
			}
		})
	}
}

func TestOrphanReplacementAfterReview(t *testing.T) {
	for _, kind := range []string{"socket", "ancestor", "live"} {
		t.Run(kind, func(t *testing.T) {
			f, socket, l := orphanFixture(t)
			l.Close()
			plan, err := f.service.Plan(context.Background(), f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.service.BeforeApply = func(Plan) {
				if kind == "ancestor" {
					dir := filepath.Dir(socket)
					if err := os.Rename(dir, dir+"-old"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(dir, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(socket); err != nil {
						t.Fatal(err)
					}
				}
				replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				replacement.SetUnlinkOnClose(false)
				t.Cleanup(func() { replacement.Close() })
				os.Chmod(socket, 0600)
				if kind != "live" {
					replacement.Close()
				}
			}
			if err := f.service.Apply(context.Background(), plan); err == nil {
				t.Fatal("raced transport accepted")
			}
			if _, err := os.Lstat(socket); err != nil {
				t.Fatal("replacement removed")
			}
		})
	}
}
