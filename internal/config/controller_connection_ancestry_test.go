//go:build linux

package config

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestControllerCodexPrivateSearchAncestor(t *testing.T) {
	for _, private := range []bool{true, false} {
		t.Run(map[bool]string{true: "protected", false: "accessible"}[private], func(t *testing.T) {
			// /tmp provides no implicit private-home barrier for the denial case.
			root, err := os.MkdirTemp("/tmp", "aegis-connection-search-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			mode := os.FileMode(0755)
			if private {
				mode = 0700
			}
			if err := os.Chmod(root, mode); err != nil {
				t.Fatal(err)
			}
			shared := filepath.Join(root, "workspace")
			if err := os.Mkdir(shared, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(shared, 0775); err != nil {
				t.Fatal(err)
			}
			leaf := filepath.Join(shared, "state")
			if err := os.Mkdir(leaf, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(leaf, "aegis.yaml")
			original := []byte("# exact operator bytes\n")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ConnectionSnapshot(path)
			if private {
				if err != nil || string(got) != string(original) {
					t.Fatalf("private search barrier rejected: %v", err)
				}
			} else if err == nil {
				t.Fatal("accessible writable ancestry admitted")
			}
		})
	}
}

func TestControllerCodexPrivateSearchRejectsAccessACL(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "aegis-connection-acl-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	path := filepath.Join(root, "aegis.yaml")
	if err := os.WriteFile(path, []byte("# fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// An explicit named entry with a zero mask retains 0700 mode bits.
	acl := make([]byte, 4+5*8)
	binary.LittleEndian.PutUint32(acl, 2)
	for i, entry := range [][3]uint32{{1, 7, 0xffffffff}, {2, 5, 1001}, {4, 0, 0xffffffff}, {16, 0, 0xffffffff}, {32, 0, 0xffffffff}} {
		at := 4 + i*8
		binary.LittleEndian.PutUint16(acl[at:], uint16(entry[0]))
		binary.LittleEndian.PutUint16(acl[at+2:], uint16(entry[1]))
		binary.LittleEndian.PutUint32(acl[at+4:], entry[2])
	}
	if err := unix.Setxattr(root, "system.posix_acl_access", acl, 0); err != nil {
		t.Fatal("real ACL fixture unavailable:", err)
	}
	if _, err := ConnectionSnapshot(path); err == nil {
		t.Fatal("access ACL accepted as a private search barrier")
	}
}
