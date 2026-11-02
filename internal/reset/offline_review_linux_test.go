//go:build linux

package reset

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOfflineTransportReviewRejectsLateReplacement(t *testing.T) {
	for _, disappearance := range []bool{false, true} {
		t.Run(map[bool]string{false: "replacement", true: "disappearance"}[disappearance], func(t *testing.T) {
			dir, err := os.MkdirTemp("", "or-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(dir, "s")
			bind := func() {
				t.Helper()
				fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if err := unix.Bind(fd, &unix.SockaddrUnix{Name: socket}); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(socket, 0600); err != nil {
					t.Fatal(err)
				}
			}
			bind()
			review, err := ReviewOfflineTransport(context.Background(), socket)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			validated := false
			err = review.Run(context.Background(), func() error {
				calls++
				if calls == 2 {
					if err := os.Rename(socket, socket+".original"); err != nil {
						return err
					}
					if !disappearance {
						bind()
					}
				}
				return nil
			}, func() error { validated = true; return nil })
			if err == nil || validated {
				t.Fatalf("replacement accepted: %v", err)
			}
			matches, err := filepath.Glob(socket + ".upgrade-orphan-*")
			if err != nil || len(matches) != 0 {
				t.Fatalf("replacement quarantined: %v %v", matches, err)
			}
			if !disappearance {
				if _, err := os.Lstat(socket); err != nil {
					t.Fatal("replacement lost", err)
				}
			}
		})
	}
}
