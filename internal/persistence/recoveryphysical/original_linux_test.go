//go:build linux

package recoveryphysical

import (
	"context"
	"encoding/binary"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func originalFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "store")
	if e := os.Mkdir(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "00001.mem")
	if e := os.WriteFile(path, []byte("controlled fixture bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0664); e != nil {
		t.Fatal(e)
	}
	return dir, path
}
func originalACL(t *testing.T, path, name string) {
	t.Helper()
	acl := make([]byte, 44)
	binary.LittleEndian.PutUint32(acl, 2)
	for i, e := range [][3]uint32{{1, 7, 0xffffffff}, {2, 5, 1001}, {4, 0, 0xffffffff}, {16, 0, 0xffffffff}, {32, 0, 0xffffffff}} {
		at := 4 + i*8
		binary.LittleEndian.PutUint16(acl[at:], uint16(e[0]))
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(e[1]))
		binary.LittleEndian.PutUint32(acl[at+4:], e[2])
	}
	if e := unix.Setxattr(path, name, acl, 0); e != nil {
		t.Fatal("real ACL fixture unavailable:", e)
	}
}
func TestOriginalEngineCustody(t *testing.T) {
	for _, kind := range []string{"private", "public-leaf", "world-write", "executable", "hardlink", "symlink", "ancestor-symlink", "file-acl", "ancestor-acl", "default-acl", "unknown", "control", "manifest-mode"} {
		t.Run(kind, func(t *testing.T) {
			dir, path := originalFixture(t)
			must := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			switch kind {
			case "public-leaf":
				must(os.Chmod(dir, 0755))
			case "world-write":
				must(os.Chmod(path, 0666))
			case "executable":
				must(os.Chmod(path, 0764))
			case "hardlink":
				must(os.Link(path, filepath.Join(filepath.Dir(dir), "alias")))
			case "symlink":
				must(os.Rename(path, path+".real"))
				must(os.Symlink(path+".real", path))
			case "ancestor-symlink":
				alias := dir + "-alias"
				must(os.Symlink(dir, alias))
				path = filepath.Join(alias, filepath.Base(path))
			case "file-acl":
				originalACL(t, path, "system.posix_acl_access")
			case "ancestor-acl":
				originalACL(t, dir, "system.posix_acl_access")
			case "default-acl":
				originalACL(t, dir, "system.posix_acl_default")
			case "unknown", "control", "manifest-mode":
				name := map[string]string{"unknown": "extra", "control": "DIRTY", "manifest-mode": "MANIFEST"}[kind]
				target := filepath.Join(dir, name)
				must(os.Rename(path, target))
				path = target
			}
			sum, e := OriginalCopy(context.Background(), path, "")
			if kind == "private" {
				if e != nil || sum == "" {
					t.Fatal(e)
				}
				if _, e = Copy(context.Background(), path, ""); e == nil {
					t.Fatal("strict copy admitted permissive file")
				}
			} else if e == nil {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
}

type originalMutationContext struct {
	context.Context
	once   sync.Once
	mutate func()
	calls  int
}

func (c *originalMutationContext) Err() error {
	c.calls++
	if c.calls > 1 {
		c.once.Do(c.mutate)
	}
	return c.Context.Err()
}
func TestOriginalEngineSnapshotMutationDenied(t *testing.T) {
	for _, kind := range []string{"mode", "bytes", "link", "replace", "directory-replace", "acl"} {
		t.Run(kind, func(t *testing.T) {
			dir, path := originalFixture(t)
			ctx := &originalMutationContext{Context: context.Background(), mutate: func() {
				var e error
				switch kind {
				case "mode":
					e = os.Chmod(path, 0600)
				case "bytes":
					e = os.WriteFile(path, []byte("different fixture bytes"), 0600)
				case "link":
					e = os.Link(path, filepath.Join(filepath.Dir(dir), "alias"))
				case "replace":
					e = os.Rename(path, path+".old")
					if e == nil {
						e = os.WriteFile(path, []byte("controlled fixture bytes"), 0600)
					}
				case "directory-replace":
					e = os.Rename(dir, dir+".old")
					if e == nil {
						e = os.Mkdir(dir, 0700)
					}
				case "acl":
					originalACL(t, path, "system.posix_acl_access")
				}
				if e != nil {
					t.Fatal(e)
				}
			}}
			if _, e := OriginalCopy(ctx, path, ""); e == nil {
				t.Fatalf("accepted changed %s", kind)
			}
		})
	}
}
func TestRecoveryPairKeepsCandidateStrict(t *testing.T) {
	dir, path := originalFixture(t)
	work := filepath.Join(filepath.Dir(dir), "work")
	original, e := OriginalDirectory(context.Background(), dir, work)
	if e != nil {
		t.Fatal(e)
	}
	candidate, e := Directory(context.Background(), work, "")
	if e != nil {
		t.Fatal(e)
	}
	// Equal byte digests still require the strict side, never permissive promotion.
	post, e := RecoveryPair(context.Background(), dir, work, original, candidate)
	if e != nil || post {
		t.Fatalf("pre: %v %v", post, e)
	}
	if e = os.Chmod(filepath.Join(work, filepath.Base(path)), 0664); e != nil {
		t.Fatal(e)
	}
	if _, e = RecoveryPair(context.Background(), dir, work, original, candidate); e == nil {
		t.Fatal("permissive candidate accepted")
	}
}
