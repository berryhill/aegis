package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Process-owned immutable bytes, not a writable cache shared with candidates.
// No t.Cleanup can delete another fixture's seed. Only joined stdlib warmup
// enters this store; candidate code, results and authority never enter it.
type stdlibFile struct {
	Path  string
	Bytes []byte
	Hash  string
}
type stdlibImage struct {
	Files     []stdlibFile
	Inventory string
}

func (image *stdlibImage) inventory() string {
	var b strings.Builder
	for _, f := range image.Files {
		fmt.Fprintf(&b, "%s\x00%s\x00%d\n", f.Path, f.Hash, len(f.Bytes))
	}
	return digest([]byte(b.String()))
}

type stdlibEntry struct {
	ready chan struct{}
	image *stdlibImage
	err   error
}
type stdlibStore struct {
	mu      sync.Mutex
	entries map[string]*stdlibEntry
}

var fixtureStdlib = stdlibStore{entries: make(map[string]*stdlibEntry)}

func (s *stdlibStore) get(ctx context.Context, key string, populate func() (*stdlibImage, error)) (*stdlibImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if entry := s.entries[key]; entry != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-entry.ready:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return entry.image, entry.err
	}
	entry := &stdlibEntry{ready: make(chan struct{})}
	s.entries[key] = entry
	s.mu.Unlock()
	image, err := populate()
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	if err != nil {
		delete(s.entries, key)
	} else {
		entry.image = image
	}
	entry.err = err
	close(entry.ready)
	s.mu.Unlock()
	return entry.image, err
}

func (image *stdlibImage) copy(ctx context.Context, cache string) error {
	// Validate the entire byte inventory BEFORE writing any entry. Copies are
	// regular independent files, never links back into the immutable image.
	if image.Inventory != image.inventory() {
		return errors.New("stdlib fixture inventory integrity")
	}
	for _, f := range image.Files {
		if !filepath.IsLocal(f.Path) || digest(f.Bytes) != f.Hash {
			return errors.New("stdlib fixture seed integrity")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Lstat(cache); err == nil {
		return errors.New("fixture cache already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(cache, 0700); err != nil {
		return err
	}
	for _, f := range image.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := filepath.Join(cache, f.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.Bytes, 0600); err != nil {
			return err
		}
	}
	return nil
}

// Qualify only the two existing native command shapes. Unsupported compiler
// flags/configuration use the original unseeded runner, never a guessed key.
func stdlibQualifier(ctx context.Context, cmd *exec.Cmd) (string, error) {
	if len(cmd.Args) < 2 || (cmd.Args[1] != "test" && cmd.Args[1] != "build") {
		return "", nil
	}
	for i := 2; i < len(cmd.Args); i++ {
		a := cmd.Args[i]
		if a == "-o" {
			i++
			if i >= len(cmd.Args) {
				return "", nil
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-json" && a != "-count=1" && a != "-p=1" && a != "-mod=readonly" && !strings.HasPrefix(a, "-timeout=") {
			return "", nil
		}
	}
	env := map[string]string{}
	for _, v := range cmd.Env {
		k, value, ok := strings.Cut(v, "=")
		if !ok {
			return "", nil
		}
		if _, exists := env[k]; exists {
			return "", nil
		}
		env[k] = value
	}
	if env["CGO_ENABLED"] != "0" || env["GOTOOLCHAIN"] != "local" || env["GOWORK"] != "off" {
		return "", nil
	}
	if env["GOFLAGS"] != "" && env["GOFLAGS"] != "-p=1 -trimpath -buildvcs=false" {
		return "", nil
	}
	// An implicit GOENV is accepted only when the private HOME has no config.
	if env["GOENV"] != "off" {
		if env["GOENV"] != "" || !filepath.IsAbs(env["HOME"]) {
			return "", nil
		}
		if _, err := os.Lstat(filepath.Join(env["HOME"], ".config")); !os.IsNotExist(err) {
			return "", nil
		}
	}
	volatile := map[string]bool{"HOME": true, "GOCACHE": true, "GOMODCACHE": true, "TMPDIR": true, "AEGIS_CONTRIBUTION_CANDIDATE": true}
	allowed := map[string]bool{}
	for _, k := range strings.Fields("PATH GOROOT GOENV GOOS GOARCH GOAMD64 GOEXPERIMENT GODEBUG GOPROXY GOSUMDB GOTOOLCHAIN GOWORK CGO_ENABLED GOMAXPROCS GOMEMLIMIT GOFLAGS") {
		allowed[k] = true
	}
	var stable []string
	for k, v := range env {
		if volatile[k] {
			continue
		}
		if !allowed[k] {
			return "", nil
		}
		stable = append(stable, k+"="+v)
	}
	sort.Strings(stable)
	_, distribution, err := freezeToolchain(ctx, cmd.Path)
	if err != nil {
		return "", err
	}
	// Query this exact distribution under the invocation's controlled environment,
	// in an empty private directory, not the candidate module or ambient HOME.
	dir, err := os.MkdirTemp("", "aegis-stdlib-env-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	query := exec.Command(cmd.Path, "env", "-json")
	query.Dir = dir
	query.Env = cmd.Env
	var out bytes.Buffer
	query.Stdout = &out
	query.Stderr = &out
	if err = runCheckerNative(ctx, query); err != nil {
		return "", err
	}
	effective := map[string]string{}
	if err = json.Unmarshal(out.Bytes(), &effective); err != nil {
		return "", err
	}
	if effective["GOROOT"] != filepath.Dir(filepath.Dir(cmd.Path)) || effective["CGO_ENABLED"] != "0" || effective["GOCACHEPROG"] != "" || (effective["GOFLAGS"] != "" && effective["GOFLAGS"] != "-p=1 -trimpath -buildvcs=false") {
		return "", nil
	}
	// These values only locate private writable setup state; all effective compiler
	// settings (including distribution defaults/GOEXPERIMENT/GOFIPS140) remain keyed.
	for _, k := range strings.Fields("GOCACHE GOMODCACHE GOPATH GOENV GOTELEMETRYDIR GOTMPDIR GOGCCFLAGS GOMOD") {
		delete(effective, k)
	}
	encoded, err := json.Marshal(effective)
	if err != nil {
		return "", err
	}
	return digest(append(append(distribution, []byte(strings.Join(stable, "\x00"))...), encoded...)), nil
}

func populateStdlib(ctx context.Context, cmd *exec.Cmd, t *testing.T) (*stdlibImage, error) {
	dir, err := os.MkdirTemp("", "aegis-stdlib-seed-")
	if err != nil {
		return nil, err
	}
	joined := true
	defer func() {
		if joined {
			_ = os.RemoveAll(dir)
		}
	}()
	warm := exec.Command(cmd.Path, "list", "-export", "-deps", "-f", "{{.Standard}}", "fmt", "os/exec", "testing/internal/testdeps")
	warm.Dir = dir
	for _, v := range cmd.Env {
		if !strings.HasPrefix(v, "GOCACHE=") && !strings.HasPrefix(v, "AEGIS_CONTRIBUTION_CANDIDATE=") {
			warm.Env = append(warm.Env, v)
		}
	}
	cache := filepath.Join(dir, "cache")
	warm.Env = append(warm.Env, "GOCACHE="+cache, "GO111MODULE=off")
	var out bytes.Buffer
	warm.Stdout = &out
	warm.Stderr = &out
	started := time.Now()
	err = runCheckerNative(ctx, warm)
	if errors.Is(err, ErrCheckerQuiescenceUnavailable) {
		joined = false
	}
	if err != nil {
		return nil, fmt.Errorf("stdlib fixture compilation: %w: %s", err, out.String())
	}
	packages := strings.Fields(out.String())
	if len(packages) == 0 {
		return nil, errors.New("empty stdlib seed")
	}
	for _, p := range packages {
		if p != "true" {
			return nil, errors.New("non-standard package in fixture seed")
		}
	}
	// The native runner joined ALL writers before this first cache read.
	image := &stdlibImage{}
	err = filepath.WalkDir(cache, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsafe stdlib cache entry")
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(cache, p)
		if e != nil {
			return e
		}
		image.Files = append(image.Files, stdlibFile{rel, b, digest(b)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(image.Files) == 0 {
		return nil, errors.New("empty stdlib cache")
	}
	image.Inventory = image.inventory()
	t.Logf("stdlib-only fixture compilation (shared population): %s entries=%d", time.Since(started), len(image.Files))
	return image, nil
}

func stdlibSeededNative(t *testing.T, goBinary string) func(context.Context, *exec.Cmd) error {
	return func(ctx context.Context, cmd *exec.Cmd) error {
		if cmd.Path != goBinary {
			return runCheckerNative(ctx, cmd)
		}
		key, err := stdlibQualifier(ctx, cmd)
		if err != nil {
			return err
		}
		if key == "" {
			t.Log("unsupported stdlib qualifier: original unseeded native runner")
			return runCheckerNative(ctx, cmd)
		}
		started := time.Now()
		image, err := fixtureStdlib.get(ctx, key, func() (*stdlibImage, error) { return populateStdlib(ctx, cmd, t) })
		if err != nil {
			return err
		}
		cache := ""
		for _, v := range cmd.Env {
			if strings.HasPrefix(v, "GOCACHE=") {
				cache = strings.TrimPrefix(v, "GOCACHE=")
			}
		}
		if cache == "" {
			return errors.New("fixture missing private native cache")
		}
		if err = image.copy(ctx, cache); err != nil {
			return err
		}
		t.Logf("stdlib-only seed obtain/copy: %s key=%s", time.Since(started), key)
		started = time.Now()
		err = runCheckerNative(ctx, cmd)
		t.Logf("fresh native %s with stdlib-only seed: %s", cmd.Args[1], time.Since(started))
		return err
	}
}
