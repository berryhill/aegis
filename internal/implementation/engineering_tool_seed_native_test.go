package implementation

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// Small checksum-locked fixture for fault injection, not the real templ tool or
// a claim of successful compilation. Full real-tool acceptance is separate.
func toolSeedFaultInputs(t *testing.T) (SourceInventory, loop.DependencyBundle) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mod := []byte("module github.com/a-h/templ\ngo 1.24\n")
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	f, err := zw.Create("github.com/a-h/templ@v0.0.1/go.mod")
	must(err)
	_, err = f.Write(mod)
	must(err)
	must(zw.Close())
	modHash, err := hashMod(mod)
	must(err)
	zipHash, err := hashZip(archive.Bytes())
	must(err)
	sum := []byte(fmt.Sprintf("github.com/a-h/templ v0.0.1 %s\ngithub.com/a-h/templ v0.0.1/go.mod %s\n", zipHash, modHash))
	var wire bytes.Buffer
	tw := tar.NewWriter(&wire)
	for _, entry := range []struct {
		name string
		data []byte
	}{{".mod", mod}, {".zip", archive.Bytes()}, {".info", []byte(`{"Version":"v0.0.1"}`)}} {
		must(tw.WriteHeader(&tar.Header{Name: "github.com/a-h/templ/@v/v0.0.1" + entry.name, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(entry.data))}))
		_, err = tw.Write(entry.data)
		must(err)
	}
	must(tw.Close())
	path := filepath.Join(t.TempDir(), "deps.tar")
	must(os.WriteFile(path, wire.Bytes(), 0600))
	source := SourceInventory{Files: []SourceFile{{Path: "go.mod", Content: []byte("module fixture\ngo 1.24\nrequire github.com/a-h/templ v0.0.1\ntool github.com/a-h/templ/cmd/templ\n")}, {Path: "go.sum", Content: sum}}}
	return source, loop.DependencyBundle{Path: path, Digest: digest(wire.Bytes())}
}

// Fault injection at the actual admitted population port, not a cache-store
// substitute. It must receive a lockfiles-only tree and the literal compiler;
// failed/unknown/canceled/overflowed builds cannot publish even readable caches.
func TestToolSeedNativePopulationFailureCustody(t *testing.T) {
	kernel, _ := fixture(t)
	source, bundle := toolSeedFaultInputs(t)
	source.Files = append(source.Files, SourceFile{Path: "candidate.go", Content: []byte("must never enter tool population")})
	for _, failure := range []string{"failed", "unjoined", "canceled", "overflow"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := &ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Admit: kernel.Admit, dependencies: &bundle}
			manifest, err := e.verifyToolchain(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// A distinct stable environment binds each fault so accidental publication
			// cannot hide a later population. No actual child is run by this injected port.
			env := []string{"HOME=/unused", "TMPDIR=/unused", "GOCACHE=/unused/cache", "GOMODCACHE=/unused/modules", "GOPROXY=file:///unused/proxy", "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOFLAGS=-p=1 -trimpath -buildvcs=false -mod=readonly -modcacherw", "GOROOT=" + manifest.Root, "GOMEMLIMIT=" + failure}
			calls := 0
			home := ""
			e.Native = func(ctx context.Context, c *exec.Cmd) error {
				calls++
				home = filepath.Dir(c.Dir)
				entries, err := os.ReadDir(c.Dir)
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				if !reflect.DeepEqual(names, []string{"go.mod", "go.sum"}) {
					t.Fatal("candidate entered seed", names)
				}
				if !reflect.DeepEqual(c.Args[1:], []string{"build", "-o", filepath.Join(home, "templ"), templTool}) {
					t.Fatal("nonliteral compiler", c.Args)
				}
				// A plausible cache must still not be read/published on non-success.
				if err = os.Mkdir(filepath.Join(home, "cache"), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(home, "cache", "entry"), []byte("injected incomplete bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				switch failure {
				case "failed", "unjoined":
					return errors.New(failure)
				case "canceled":
					cancel()
				case "overflow":
					_, _ = c.Stdout.Write(make([]byte, maxBytes+1))
				}
				return nil
			}
			destination := filepath.Join(t.TempDir(), "cache")
			if err = e.seedTemplCache(ctx, source, env, destination); err == nil {
				t.Fatal("bad build accepted")
			}
			if calls != 1 {
				t.Fatal("population not called", calls)
			}
			if _, err = os.Lstat(destination); !os.IsNotExist(err) {
				t.Fatal("bad population copied")
			}
			_, statErr := os.Stat(home)
			if failure == "failed" || failure == "unjoined" {
				if statErr != nil {
					t.Fatal("unknown writer home removed", statErr)
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatal("joined rejected home retained", statErr)
			}
			wire, err := dependencyBundleBytes(bundle)
			if err != nil {
				t.Fatal(err)
			}
			key, err := toolSeedKey(e.toolchainDigest, source, wire, env)
			if err != nil {
				t.Fatal(err)
			}
			pinnedToolCache.mu.Lock()
			published := pinnedToolCache.key == key
			pinnedToolCache.mu.Unlock()
			if published {
				t.Fatal("bad population published")
			}
		})
	}
}
