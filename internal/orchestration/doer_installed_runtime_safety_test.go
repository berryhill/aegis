//go:build linux

package orchestration

import (
	"encoding/binary"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

// Metadata-only fixture tests qualification, not real Laya availability.
func TestInstalledLocalLayaSourceQualification(t *testing.T) {
	for _, which := range []string{"qualified", "foreign-asset", "missing-asset", "writable-library", "library-acl"} {
		t.Run(which, func(t *testing.T) {
			cwd, _ := os.Getwd()
			base, _ := filepath.Abs(filepath.Join(cwd, "..", "..", ".candidate"))
			os.MkdirAll(base, 0700)
			home, err := os.MkdirTemp(base, "runtime-safety-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(home)
			put := func(p string, content []byte, mode os.FileMode) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, content, mode); err != nil {
					t.Fatal(err)
				}
			}
			venv := filepath.Join(home, ".hermes", "services", "doer", ".venv")
			put(filepath.Join(venv, "bin", "python"), []byte("#!/bin/sh\nexit 1\n"), 0700)
			put(filepath.Join(venv, "pyvenv.cfg"), []byte("include-system-site-packages = false\n"), 0600)
			site := filepath.Join(venv, "lib", "python3.12", "site-packages")
			put(filepath.Join(site, "laya-0.3.20.dist-info", "METADATA"), []byte("Name: laya\nVersion: 0.3.20\n"), 0600)
			library := filepath.Join(site, "laya", "__init__.py")
			put(library, []byte("# qualification fixture only\n"), 0600)
			revision := "0123456789012345678901234567890123456789"
			repo := filepath.Join(home, ".cache", "huggingface", "hub", "models--convaiinnovations--laya")
			put(filepath.Join(repo, "refs", "main"), []byte(revision), 0600)
			for _, name := range installedAssets {
				put(filepath.Join(repo, "snapshots", revision, name), []byte("public fixture"), 0600)
			}
			asset := filepath.Join(repo, "snapshots", revision, "model.safetensors")
			switch which {
			case "foreign-asset":
				os.Remove(asset)
				if err := os.Symlink("/etc/passwd", asset); err != nil {
					t.Fatal(err)
				}
			case "missing-asset":
				os.Remove(asset)
			case "writable-library":
				os.Chmod(library, 0666)
			case "library-acl":
				// A valid POSIX access ACL must not be mistaken for an owned private source.
				acl := make([]byte, 4+5*8)
				binary.LittleEndian.PutUint32(acl, 2)
				for i, e := range [][3]uint32{{1, 6, 0xffffffff}, {2, 4, 1001}, {4, 0, 0xffffffff}, {16, 0, 0xffffffff}, {32, 0, 0xffffffff}} {
					at := 4 + i*8
					binary.LittleEndian.PutUint16(acl[at:], uint16(e[0]))
					binary.LittleEndian.PutUint16(acl[at+2:], uint16(e[1]))
					binary.LittleEndian.PutUint32(acl[at+4:], e[2])
				}
				if err := unix.Setxattr(library, "system.posix_acl_access", acl, 0); err != nil {
					t.Fatal(err)
				}
			}
			state := filepath.Join(home, "state")
			os.MkdirAll(state, 0700)
			w := &QueueWorker{}
			err = w.prepareInstalledDoerRuntime(home, state, &hermesruntime.Adapter{})
			if (err == nil) != (which == "qualified") {
				t.Fatalf("qualification %s: %v", which, err)
			}
			if which == "qualified" {
				// Materialize only public cache assets; no provider/auth or user cache files.
				p := w.implementation.decision.process.(*installedLayaProcess)
				if err := p.prepare(); err != nil {
					t.Fatal(err)
				}
				if !w.implementation.managedLocalDoer || len(w.implementation.config.AuthorizedContracts) != 0 {
					t.Fatal("dependency preparation became authority")
				}
				if trustedLayaExecutable(p.python) != nil {
					t.Fatal("qualified interpreter lost")
				}
			}
		})
	}
}
