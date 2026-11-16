package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Only a joined literal tool build may populate this process-owned image. No
// candidate command ever writes it back. Retain at most one completed image;
// differing toolchains/locks/environments replace it, never alias its key.
type toolCacheFile struct{ Path, Hash, Bytes string }
type toolCacheImage struct {
	Files     []toolCacheFile
	Inventory string
}

func (i *toolCacheImage) inventory() string {
	var b strings.Builder
	for _, f := range i.Files {
		fmt.Fprintf(&b, "%s\x00%s\x00%d\n", f.Path, f.Hash, len(f.Bytes))
	}
	return digest([]byte(b.String()))
}
func (i *toolCacheImage) validate(ctx context.Context) error {
	if i == nil || len(i.Files) == 0 || len(i.Files) > 50000 || i.Inventory != i.inventory() {
		return errors.New("tool cache inventory integrity")
	}
	var total int64
	previous := ""
	for _, f := range i.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		total += int64(len(f.Bytes))
		if !filepath.IsLocal(f.Path) || filepath.Clean(f.Path) != f.Path || strings.ContainsAny(f.Path, "\\\x00\r\n") || f.Path <= previous || len(f.Bytes) > 64<<20 || total > 512<<20 || digest([]byte(f.Bytes)) != f.Hash {
			return errors.New("tool cache byte integrity")
		}
		previous = f.Path
	}
	return ctx.Err()
}
func (i *toolCacheImage) copy(ctx context.Context, cache string) error {
	// Authenticate EVERY entry before creating even the destination directory.
	if err := i.validate(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Mkdir(cache, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(cache)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range i.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(f.Path), 0700); err != nil {
			return err
		}
		out, err := root.OpenFile(f.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = out.WriteString(f.Bytes)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return ctx.Err()
}
func readToolCache(ctx context.Context, cache string) (*toolCacheImage, error) {
	root, err := os.OpenRoot(cache)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	image := &toolCacheImage{}
	var total int64
	err = filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe tool cache entry")
		}
		if d.IsDir() {
			return nil
		}
		total += info.Size()
		if !info.Mode().IsRegular() || !singleLink(info) || info.Size() > 64<<20 || total > 512<<20 || len(image.Files) >= 50000 {
			return errors.New("unsafe or unbounded tool cache")
		}
		rel, err := filepath.Rel(cache, p)
		if err != nil {
			return err
		}
		b, err := readRepositoryContextFile(root, rel, info.Size())
		if err != nil {
			return err
		}
		image.Files = append(image.Files, toolCacheFile{rel, digest(b), string(b)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	image.Inventory = image.inventory()
	if err = image.validate(ctx); err != nil {
		return nil, err
	}
	return image, nil
}

type toolCacheStore struct {
	mu    sync.Mutex
	busy  chan struct{}
	key   string
	image *toolCacheImage
}

var pinnedToolCache toolCacheStore

func (s *toolCacheStore) get(ctx context.Context, key string, populate func() (*toolCacheImage, error)) (*toolCacheImage, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.key == key && s.image != nil {
			image := s.image
			s.mu.Unlock()
			return image, nil
		}
		if s.busy != nil {
			ready := s.busy
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ready:
			}
			continue
		}
		s.busy = make(chan struct{})
		s.mu.Unlock()
		image, err := populate()
		if err == nil {
			err = image.validate(ctx)
		}
		s.mu.Lock()
		if err == nil {
			s.key, s.image = key, image
		}
		close(s.busy)
		s.busy = nil
		s.mu.Unlock()
		return image, err
	}
}

// The environment is constructed by nativeOperation, not inherited. Bind every
// effective setting, normalizing only private locations that are freshly made
// below. GOENV=off/GOTOOLCHAIN=local exclude ambient compiler configuration.
func toolSeedKey(toolchain string, source SourceInventory, wire []byte, env []string) (string, error) {
	if err := pinnedTemplModule(source); err != nil {
		return "", err
	}
	var mod, sum []byte
	for _, f := range source.Files {
		switch f.Path {
		case "go.mod":
			mod = f.Content
		case "go.sum":
			sum = f.Content
		}
	}
	if len(sum) == 0 {
		return "", errTemplConsistency
	}
	settings := map[string]string{}
	for _, s := range env {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return "", errTemplConsistency
		}
		if _, ok = settings[k]; ok {
			return "", errTemplConsistency
		}
		switch k {
		case "HOME", "TMPDIR", "GOCACHE", "GOMODCACHE":
			v = "private:" + k
		case "GOPROXY":
			if !strings.HasPrefix(v, "file://") {
				return "", errTemplConsistency
			}
			v = "locked-offline-proxy"
		case "PATH", "GOROOT", "GOSUMDB", "GOTOOLCHAIN", "GOWORK", "CGO_ENABLED", "GOMAXPROCS", "GOMEMLIMIT", "GOFLAGS", "GOENV", "GOOS", "GOARCH", "GOAMD64", "GOEXPERIMENT", "GODEBUG":
		default:
			return "", errTemplConsistency
		}
		settings[k] = v
	}
	if settings["GOENV"] != "off" || settings["GOTOOLCHAIN"] != "local" || settings["GOWORK"] != "off" || settings["CGO_ENABLED"] != "0" || settings["GOFLAGS"] != "-p=1 -trimpath -buildvcs=false -mod=readonly -modcacherw" {
		return "", errTemplConsistency
	}
	b, _ := json.Marshal([]any{templTool, toolchain, digest(mod), digest(sum), digest(wire), settings})
	return digest(b), nil
}

func (e *ManagedContribution) seedTemplCache(ctx context.Context, source SourceInventory, env []string, cache string) error {
	wire, err := dependencyBundleBytes(*e.dependencies)
	if err != nil {
		return err
	}
	key, err := toolSeedKey(e.toolchainDigest, source, wire, env)
	if err != nil {
		return err
	}
	image, err := pinnedToolCache.get(ctx, key, func() (*toolCacheImage, error) {
		// No source, output or cache from any builder/tester is admitted here.
		var mod, sum []byte
		for _, f := range source.Files {
			switch f.Path {
			case "go.mod":
				mod = f.Content
			case "go.sum":
				sum = f.Content
			}
		}
		if err := validateLockedDependencyBundle(wire, mod, sum); err != nil {
			return nil, err
		}
		home, err := os.MkdirTemp(e.StateRoot, "tool-seed-")
		if err != nil {
			return nil, err
		}
		joined := true
		defer func() {
			if joined {
				_ = os.RemoveAll(home)
			}
		}()
		workspace := filepath.Join(home, "source")
		if err = os.Mkdir(workspace, 0700); err != nil {
			return nil, err
		}
		for name, b := range map[string][]byte{"go.mod": mod, "go.sum": sum} {
			if err = os.WriteFile(filepath.Join(workspace, name), b, 0600); err != nil {
				return nil, err
			}
		}
		if err = materializeDependencyBundle(ctx, wire, filepath.Join(home, "proxy")); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, e.GoBinary, "build", "-o", filepath.Join(home, "templ"), templTool)
		cmd.Dir = workspace
		for _, s := range env {
			k, _, _ := strings.Cut(s, "=")
			switch k {
			case "HOME", "TMPDIR":
				s = k + "=" + home
			case "GOCACHE":
				s = k + "=" + filepath.Join(home, "cache")
			case "GOMODCACHE":
				s = k + "=" + filepath.Join(home, "modules")
			case "GOPROXY":
				s = k + "=file://" + filepath.Join(home, "proxy")
			}
			cmd.Env = append(cmd.Env, s)
		}
		if err = e.admit(ctx, "native-check"); err != nil {
			return nil, err
		}
		var output boundedOutput
		cmd.Stdout, cmd.Stderr = &output, &output
		cmd.WaitDelay = time.Second
		// The admitted native port owns Start/Wait and complete descendant custody.
		// Any error is potentially unjoined: retain the home and publish nothing.
		if err = e.Native(ctx, cmd); err != nil {
			joined = false
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if output.overflow {
			return nil, errors.New("native check output exceeds bound")
		}
		if _, err = e.verifyToolchain(ctx); err != nil {
			return nil, err
		}
		if _, err = dependencyBundleBytes(*e.dependencies); err != nil {
			return nil, err
		}
		for name, b := range map[string][]byte{"go.mod": mod, "go.sum": sum} {
			actual, err := os.ReadFile(filepath.Join(workspace, name))
			if err != nil || digest(actual) != digest(b) {
				return nil, errTemplConsistency
			}
		}
		return readToolCache(ctx, filepath.Join(home, "cache"))
	})
	if err != nil {
		return err
	}
	return image.copy(ctx, cache)
}
