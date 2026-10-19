//go:build linux

package reset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOfflineTransportQuarantine(t *testing.T) {
	for _, kind := range []string{"orphan", "bound", "relative", "alias", "unsafe-lock", "lock-held", "denied", "changed"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(dir, "s")
			fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			name := socket
			if kind == "relative" {
				t.Chdir(dir)
				name = "s"
			}
			if kind == "alias" {
				alias := filepath.Join(dir, "alias")
				if err := os.Symlink(dir, alias); err != nil {
					t.Fatal(err)
				}
				name = filepath.Join(alias, "s")
			}
			if err = unix.Bind(fd, &unix.SockaddrUnix{Name: name}); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(socket, 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "bound" || kind == "relative" || kind == "alias" {
				defer unix.Close(fd)
			} else {
				unix.Close(fd)
			}
			var lock *os.File
			if kind == "unsafe-lock" {
				if err = os.Symlink(socket, socket+".lock"); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "lock-held" {
				lock, err = os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			validated := false
			err = WithOfflineTransport(context.Background(), socket, func() error {
				calls++
				if kind == "denied" {
					return errors.New("denied")
				}
				if kind == "changed" && calls == 3 {
					if err := os.Remove(socket); err != nil {
						return err
					}
					return os.WriteFile(socket, []byte("replacement"), 0600)
				}
				return nil
			}, func() error {
				validated = true
				lock, e := os.OpenFile(socket+".lock", os.O_RDWR, 0600)
				if e != nil {
					return e
				}
				defer lock.Close()
				if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != unix.EWOULDBLOCK {
					t.Fatalf("startup not excluded: %v", e)
				}
				return nil
			})
			if kind == "orphan" {
				if err != nil || !validated {
					t.Fatalf("quarantine: %v", err)
				}
				matches, _ := filepath.Glob(socket + ".upgrade-orphan-*")
				if len(matches) != 1 {
					t.Fatal(matches)
				}
				if err = WithOfflineTransport(context.Background(), socket, func() error { return nil }, func() error { return nil }); err != nil {
					t.Fatal("resume", err)
				}
			} else {
				if err == nil || validated {
					t.Fatalf("unsafe accepted: %v", err)
				}
				if _, e := os.Lstat(socket); e != nil {
					t.Fatal("transport lost", e)
				}
			}
		})
	}
}
