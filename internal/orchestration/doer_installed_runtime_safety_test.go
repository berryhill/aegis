//go:build linux

package orchestration

import (
	"encoding/binary"
	"github.com/berryhill/aegis/internal/config"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Metadata-only fixture tests qualification, not real Laya availability.
func TestInstalledLocalLayaSourceQualification(t *testing.T) {
	for _, which := range []string{"qualified", "native-qualified", "native-missing", "explicit-decision", "explicit-laya", "foreign-asset", "missing-asset", "writable-library", "library-acl"} {
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
			case "native-missing":
				os.Remove(filepath.Join(site, "laya-0.3.20.dist-info", "METADATA"))
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
			adapter := &hermesruntime.Adapter{}
			native := config.Implementation{GoBinary: filepath.Join(home, "trusted-go"), AuthorizedContracts: []string{"sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58"}}
			if which == "native-qualified" || which == "native-missing" || which == "explicit-decision" || which == "explicit-laya" {
				if which == "explicit-laya" {
					native.LayaPython, native.LayaHome = filepath.Join(home, "custom-python"), filepath.Join(home, "custom-home")
				}
				if err := w.ConfigureImplementation(native, state, adapter); err != nil {
					t.Fatal(err)
				}
			}
			controller := w.implementation
			if which == "explicit-decision" {
				controller.decision = &LayaDecisionAdapter{}
			}
			if which == "explicit-decision" || which == "explicit-laya" {
				decision := controller.decision
				// Invalid discovery inputs prove explicit decisions/config bypass discovery.
				if err := w.prepareInstalledDoerRuntime("/missing", "relative", nil); err != nil {
					t.Fatal(err)
				}
				if w.implementation != controller || controller.decision != decision || !reflect.DeepEqual(controller.config, native) {
					t.Fatal("explicit configuration replaced")
				}
				return
			}
			err = w.prepareInstalledDoerRuntime(home, state, &hermesruntime.Adapter{})
			if (err == nil) != (which == "qualified" || which == "native-qualified") {
				t.Fatalf("qualification %s: %v", which, err)
			}
			if controller != nil {
				if w.implementation != controller || controller.adapter != adapter || controller.root != filepath.Join(state, "persistence", "fleet-v1") || controller.config.GoBinary != native.GoBinary || !reflect.DeepEqual(controller.config.AuthorizedContracts, native.AuthorizedContracts) {
					t.Fatal("native authority or bindings changed")
				}
				if which == "native-missing" {
					if controller.decision != nil || !reflect.DeepEqual(controller.config, native) {
						t.Fatal("missing library changed configuration")
					}
				} else if controller.decision == nil || controller.config.LayaPython != filepath.Join(venv, "bin", "python") || controller.config.LayaHome == "" || !controller.managedLocalDoer {
					t.Fatal("qualified backend not filled")
				}
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
