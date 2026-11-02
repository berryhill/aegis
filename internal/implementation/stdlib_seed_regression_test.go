package implementation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testStdlibImage() *stdlibImage {
	b := []byte("immutable stdlib fixture bytes")
	image := &stdlibImage{Files: []stdlibFile{{"ab/entry-d", b, digest(b)}}}
	image.Inventory = image.inventory()
	return image
}
func TestStdlibSeedIntegrityAndIsolation(t *testing.T) {
	ctx := context.Background()
	image := testStdlibImage()
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parent, err := os.MkdirTemp(root, "copy-")
			if err != nil {
				t.Error(err)
				return
			}
			cache := filepath.Join(parent, "cache")
			if err = image.copy(ctx, cache); err != nil {
				t.Error(err)
				return
			}
			if err = os.WriteFile(filepath.Join(cache, image.Files[0].Path), []byte("mutated private copy"), 0600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if string(image.Files[0].Bytes) != "immutable stdlib fixture bytes" {
		t.Fatal("private mutation reached seed")
	}
	image.Files[0].Bytes[0] ^= 1
	denied := filepath.Join(root, "denied")
	if err := image.copy(ctx, denied); err == nil {
		t.Fatal("tamper accepted")
	}
	if _, err := os.Stat(denied); !os.IsNotExist(err) {
		t.Fatal("tamper wrote cache")
	}
	image = testStdlibImage()
	image.Files[0].Path = "../escape"
	if err := image.copy(ctx, denied); err == nil {
		t.Fatal("inventory drift accepted")
	}
}
func TestStdlibSeedPopulationCancellation(t *testing.T) {
	s := stdlibStore{entries: make(map[string]*stdlibEntry)}
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var populations atomic.Int32
	populate := func() (*stdlibImage, error) {
		populations.Add(1)
		close(entered)
		<-release
		return testStdlibImage(), nil
	}
	go func() { _, err := s.get(context.Background(), "key", populate); done <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.get(ctx, "key", func() (*stdlibImage, error) { t.Error("waiter populated"); return nil, nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := s.get(context.Background(), "key", func() (*stdlibImage, error) { t.Error("seed not reused"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if populations.Load() != 1 {
		t.Fatal(populations.Load())
	}
	failure := errors.New("failed warmup")
	if _, err := s.get(context.Background(), "failed", func() (*stdlibImage, error) { return testStdlibImage(), failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := s.get(context.Background(), "failed", func() (*stdlibImage, error) { return testStdlibImage(), nil }); err != nil {
		t.Fatal("failed population published", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if _, err := s.get(ctx, "canceled-populator", func() (*stdlibImage, error) { cancel(); return testStdlibImage(), nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.entries["canceled-populator"] != nil {
		t.Fatal("canceled image published")
	}
}
func stdlibRegressionCommand(t *testing.T) *exec.Cmd {
	t.Helper()
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	cmd := exec.Command(goBinary, "test", "-json", "-count=1", "-p=1", "-timeout=60s", ".")
	cmd.Env = []string{"PATH=" + filepath.Dir(goBinary), "HOME=" + home, "GOCACHE=" + filepath.Join(home, "cache"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOMAXPROCS=2"}
	return cmd
}
func TestStdlibSeedQualifierAndMeasuredReuse(t *testing.T) {
	ctx := context.Background()
	cmd := stdlibRegressionCommand(t)
	key, err := stdlibQualifier(ctx, cmd)
	if err != nil || key == "" {
		t.Fatal("qualifier", key, err)
	}
	other := stdlibRegressionCommand(t)
	same, err := stdlibQualifier(ctx, other)
	if err != nil || same != key {
		t.Fatal("private path drift", same, key, err)
	}
	other.Env = append(other.Env, "GOEXPERIMENT=arenas")
	changed, err := stdlibQualifier(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if changed == key {
		t.Fatal("compiler environment drift reused seed")
	}
	other = stdlibRegressionCommand(t)
	other.Args = append(other.Args, "-race")
	if k, e := stdlibQualifier(ctx, other); e != nil || k != "" {
		t.Fatal("unsupported flags accepted", k, e)
	}
	other = stdlibRegressionCommand(t)
	// A private GOENV file/config must not be ignored merely because HOME differs.
	home := ""
	for _, v := range other.Env {
		if len(v) > 5 && v[:5] == "HOME=" {
			home = v[5:]
		}
	}
	if err = os.MkdirAll(filepath.Join(home, ".config", "go"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(home, ".config", "go", "env"), []byte("GOFLAGS=-race\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if k, e := stdlibQualifier(ctx, other); e != nil || k != "" {
		t.Fatal("implicit GOENV ignored", k, e)
	}
	// Real isolated pre-change warmup cost, then an independent shared population
	// and reuse measurement. Never seed from the host cache.
	before := time.Now()
	if _, err = populateStdlib(ctx, cmd, t); err != nil {
		t.Fatal(err)
	}
	t.Logf("baseline per-fixture warmup: %s", time.Since(before))
	s := stdlibStore{entries: make(map[string]*stdlibEntry)}
	var count int
	populate := func() (*stdlibImage, error) { count++; return populateStdlib(ctx, cmd, t) }
	before = time.Now()
	image, err := s.get(ctx, key, populate)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("shared initial warmup: %s", time.Since(before))
	before = time.Now()
	reused, err := s.get(ctx, key, populate)
	if err != nil {
		t.Fatal(err)
	}
	if err = reused.copy(ctx, filepath.Join(t.TempDir(), "cache")); err != nil {
		t.Fatal(err)
	}
	t.Logf("shared reuse + fresh private copy: %s populations=%d entries=%d", time.Since(before), count, len(image.Files))
	if count != 1 || reused != image {
		t.Fatal("not reused")
	}
}
