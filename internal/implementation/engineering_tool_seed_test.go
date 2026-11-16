package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixtureToolImage() *toolCacheImage {
	image := &toolCacheImage{Files: []toolCacheFile{{Path: "ab/entry-d", Bytes: "immutable tool bytes", Hash: digest([]byte("immutable tool bytes"))}}}
	image.Inventory = image.inventory()
	return image
}
func TestToolSeedIntegrityAndPrivateMutation(t *testing.T) {
	ctx := context.Background()
	image := fixtureToolImage()
	parent := t.TempDir()
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			home, err := os.MkdirTemp(parent, "private-")
			if err != nil {
				t.Error(err)
				return
			}
			cache := filepath.Join(home, "cache")
			if err = image.copy(ctx, cache); err != nil {
				t.Error(err)
				return
			}
			p := filepath.Join(cache, image.Files[0].Path)
			info, err := os.Stat(p)
			if err != nil || !singleLink(info) {
				t.Error("not an independent inode", err)
				return
			}
			if err = os.WriteFile(p, []byte("candidate mutation"), 0600); err != nil {
				t.Error(err)
			}
			if err = image.copy(ctx, cache); err == nil {
				t.Error("existing candidate cache accepted")
			}
		}()
	}
	wg.Wait()
	if err := image.validate(ctx); err != nil {
		t.Fatal("private writes reached image", err)
	}
	for _, kind := range []string{"bytes", "last-entry", "hash", "inventory", "traversal", "absolute", "duplicate", "backslash"} {
		t.Run(kind, func(t *testing.T) {
			image := fixtureToolImage()
			switch kind {
			case "bytes":
				image.Files[0].Bytes = "tamper"
			case "last-entry":
				image.Files = append(image.Files, toolCacheFile{Path: "zz/last-d", Bytes: "last", Hash: digest([]byte("last"))})
				image.Inventory = image.inventory()
				image.Files[1].Bytes = "tampered last entry"
			case "hash":
				image.Files[0].Hash = "tamper"
			case "inventory":
				image.Inventory = "tamper"
			case "traversal":
				image.Files[0].Path = "../escape"
				image.Inventory = image.inventory()
			case "absolute":
				image.Files[0].Path = "/escape"
				image.Inventory = image.inventory()
			case "duplicate":
				image.Files = append(image.Files, image.Files[0])
				image.Inventory = image.inventory()
			case "backslash":
				image.Files[0].Path = "ab\\escape"
				image.Inventory = image.inventory()
			}
			dest := filepath.Join(t.TempDir(), "cache")
			if err := image.copy(ctx, dest); err == nil {
				t.Fatal("tamper accepted")
			}
			if _, err := os.Lstat(dest); !os.IsNotExist(err) {
				t.Fatal("wrote before authenticating every entry")
			}
		})
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	dest := filepath.Join(parent, "canceled")
	if err := image.copy(ctx, dest); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("canceled copy wrote")
	}
}

type toolSeedWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *toolSeedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestToolSeedPopulationAndCancellation(t *testing.T) {
	var store toolCacheStore
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := store.get(context.Background(), "one", func() (*toolCacheImage, error) { close(entered); <-release; return fixtureToolImage(), nil })
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waitCtx := &toolSeedWaitContext{Context: ctx, waiting: make(chan struct{})}
	waitResult := make(chan error, 1)
	go func() {
		_, err := store.get(waitCtx, "one", func() (*toolCacheImage, error) { t.Error("canceled waiter populated"); return nil, nil })
		waitResult <- err
	}()
	<-waitCtx.waiting // The waiter has reached the busy-population select.
	cancel()
	if err := <-waitResult; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := store.get(context.Background(), "one", func() (*toolCacheImage, error) { t.Error("not reused"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("failed or unjoined native build")
	if _, err := store.get(context.Background(), "failed", func() (*toolCacheImage, error) { return fixtureToolImage(), sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if store.key != "one" {
		t.Fatal("failed population published")
	}
	ctx, cancel = context.WithCancel(context.Background())
	if _, err := store.get(ctx, "canceled", func() (*toolCacheImage, error) { cancel(); return fixtureToolImage(), nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.key != "one" {
		t.Fatal("canceled population published")
	}
	if _, err := store.get(context.Background(), "failed", func() (*toolCacheImage, error) { return fixtureToolImage(), nil }); err != nil || store.key != "failed" {
		t.Fatal("failed population not retried", err)
	}
}
func TestToolSeedBinding(t *testing.T) {
	source := SourceInventory{Files: []SourceFile{{Path: "go.mod", Content: []byte("module fixture\ngo 1.26\nrequire github.com/a-h/templ v0.3.1001\ntool github.com/a-h/templ/cmd/templ\n")}, {Path: "go.sum", Content: []byte("locked sums")}}}
	env := []string{"HOME=/private/one", "TMPDIR=/private/one", "GOCACHE=/private/one/cache", "GOMODCACHE=/private/one/modules", "GOPROXY=file:///private/one/proxy", "GOENV=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOFLAGS=-p=1 -trimpath -buildvcs=false -mod=readonly -modcacherw", "GOARCH=amd64"}
	key, err := toolSeedKey("toolchain bytes", source, []byte("bundle bytes"), env)
	if err != nil {
		t.Fatal(err)
	}
	other := append([]string(nil), env...)
	for n, s := range other {
		other[n] = strings.ReplaceAll(s, "/private/one", "/private/two")
	}
	same, err := toolSeedKey("toolchain bytes", source, []byte("bundle bytes"), other)
	if err != nil || same != key {
		t.Fatal("private locations changed identity", err)
	}
	for _, which := range []string{"toolchain", "bundle", "mod", "sum", "environment", "ambient"} {
		t.Run(which, func(t *testing.T) {
			tc := "toolchain bytes"
			wire := []byte("bundle bytes")
			s := source
			s.Files = append([]SourceFile(nil), source.Files...)
			env := append([]string(nil), env...)
			switch which {
			case "toolchain":
				tc = "different toolchain bytes"
			case "bundle":
				wire = []byte("different dependency bytes")
			case "mod":
				s.Files[0].Content = append(append([]byte(nil), s.Files[0].Content...), []byte("// drift\n")...)
			case "sum":
				s.Files[1].Content = []byte("different locks")
			case "environment":
				env = append(env, "GOEXPERIMENT=arenas")
			case "ambient":
				env = append(env, "GOCACHEPROG=untrusted")
			}
			got, err := toolSeedKey(tc, s, wire, env)
			if err == nil && got == key {
				t.Fatal("drift reused seed")
			}
		})
	}
}
func TestToolSeedReadRejectsLinks(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			cache := filepath.Join(home, "cache")
			if err := os.Mkdir(cache, 0700); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(home, "outside")
			if err := os.WriteFile(p, []byte("not tool cache"), 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "symlink" {
				err = os.Symlink(p, filepath.Join(cache, "entry"))
			} else {
				err = os.Link(p, filepath.Join(cache, "entry"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = readToolCache(context.Background(), cache); err == nil {
				t.Fatal("linked cache admitted")
			}
		})
	}
}
