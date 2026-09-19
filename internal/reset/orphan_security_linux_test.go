//go:build linux

package reset

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestOrphanBoundAliases(t *testing.T) {
	for _, kind := range []string{"relative", "alias"} {
		t.Run(kind, func(t *testing.T) {
			f, socket, l := orphanFixture(t)
			l.Close()
			if err := os.Remove(socket); err != nil {
				t.Fatal(err)
			}
			name := socket
			if kind == "relative" {
				t.Chdir(filepath.Dir(socket))
				name = "aegis.sock"
			} else {
				alias := filepath.Join(f.home, "alias")
				if err := os.Symlink(filepath.Dir(socket), alias); err != nil {
					t.Fatal(err)
				}
				name = filepath.Join(alias, "aegis.sock")
			}
			fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(fd)
			if err := unix.Bind(fd, &unix.SockaddrUnix{Name: name}); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(socket, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Plan(context.Background(), f.config); err == nil {
				t.Fatal("bound non-listening socket accepted")
			}
			if _, err := os.Lstat(socket); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOrphanStartupLockContention(t *testing.T) {
	f, socket, l := orphanFixture(t)
	l.Close()
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	// Pause startup after its real lifecycle flock, before replacing/binding.
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Apply(context.Background(), plan); err == nil {
		t.Fatal("reset bypassed startup lifecycle lock")
	}
	if _, err := os.Lstat(socket); err != nil {
		t.Fatal("reset deleted startup's transport", err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
}

func TestOrphanRecoveryLockExcludesStartup(t *testing.T) {
	f, socket, l := orphanFixture(t)
	l.Close()
	_ = f
	dir, err := unix.Open(filepath.Dir(socket), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(dir)
	unlock, err := lockOrphanTransport(dir, filepath.Base(socket))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// API startup uses exactly this open + nonblocking flock before replacement.
	startup, err := os.OpenFile(socket+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer startup.Close()
	if err := unix.Flock(int(startup.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("startup did not contend: %v", err)
	}
}

func TestOrphanUnsafeLifecycleLock(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "writable", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f, socket, l := orphanFixture(t)
			l.Close()
			plan, err := f.service.Plan(context.Background(), f.config)
			if err != nil {
				t.Fatal(err)
			}
			path := socket + ".lock"
			switch kind {
			case "symlink":
				err = os.Symlink(socket, path)
			case "directory":
				err = os.Mkdir(path, 0700)
			default:
				err = os.WriteFile(path, nil, 0600)
				if err == nil && kind == "hardlink" {
					err = os.Link(path, path+".alias")
				}
				if err == nil && kind == "writable" {
					err = os.Chmod(path, 0666)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = f.service.Apply(context.Background(), plan); err == nil {
				t.Fatal("unsafe lifecycle lock accepted")
			}
			if _, err = os.Lstat(socket); err != nil {
				t.Fatal("socket not preserved", err)
			}
		})
	}
}
