package implementation

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
)

func lockedSums(sum []byte) (map[string]string, error) {
	result := map[string]string{}
	for _, line := range strings.Split(string(sum), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.HasPrefix(fields[2], "h1:") {
			return nil, errors.New("invalid locked sums")
		}
		key := fields[0] + " " + fields[1]
		if prev, ok := result[key]; ok && prev != fields[2] {
			return nil, errors.New("conflicting locked sums")
		}
		result[key] = fields[2]
	}
	return result, nil
}

func hashMod(b []byte) (string, error) {
	return dirhash.Hash1([]string{"go.mod"}, func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil })
}
func hashZip(b []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return "", err
	}
	names := []string{}
	files := map[string]*zip.File{}
	var expanded uint64
	for _, f := range z.File {
		expanded += f.UncompressedSize64
		if len(z.File) > 65536 || f.UncompressedSize64 > 64<<20 || expanded > 512<<20 {
			return "", errors.New("unbounded module archive")
		}
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		if files[f.Name] != nil {
			return "", errors.New("duplicate zip file")
		}
		names = append(names, f.Name)
		files[f.Name] = f
	}
	return dirhash.Hash1(names, func(p string) (io.ReadCloser, error) { return files[p].Open() })
}

// validateLockedDependencyBundle validates every retained archive byte against the
// independently frozen go.sum, including go.mod hashes (not ambient .ziphash).
// All direct/indirect go.mod requirements must have full offline proxy triplets.
func validateLockedDependencyBundle(wire, mod, sum []byte) error {
	sums, err := lockedSums(sum)
	if err != nil {
		return err
	}
	m, err := modfile.Parse("go.mod", mod, nil)
	if err != nil {
		return err
	}
	reader := tar.NewReader(bytes.NewReader(wire))
	seen := map[string]bool{}
	var total int64
	for {
		h, e := reader.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		name := h.Name
		total += h.Size
		if h.Typeflag != tar.TypeReg || !filepath.IsLocal(name) || filepath.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") || seen[name] || len(seen) >= 8192 || h.Size < 0 || h.Size > maxDependencyBundleBytes || total > maxDependencyBundleBytes {
			return errors.New("unsafe dependency proxy")
		}
		seen[name] = true
		parts := strings.Split(name, "/@v/")
		if len(parts) != 2 {
			return errors.New("invalid proxy path")
		}
		p, e := module.UnescapePath(parts[0])
		if e != nil {
			return e
		}
		ext := filepath.Ext(parts[1])
		if ext != ".mod" && ext != ".zip" && ext != ".info" {
			return errors.New("unexpected proxy entry")
		}
		v, e := module.UnescapeVersion(strings.TrimSuffix(parts[1], ext))
		if e != nil || module.Check(p, v) != nil {
			return errors.New("invalid module version")
		}
		b, e := io.ReadAll(reader)
		if e != nil {
			return e
		}
		key := p + " " + v
		switch ext {
		case ".mod":
			actual, e := hashMod(b)
			if e != nil || sums[key+"/go.mod"] != actual {
				return errors.New("module definition not locked")
			}
		case ".zip":
			actual, e := hashZip(b)
			if e != nil || sums[key] != actual {
				return errors.New("module archive not locked")
			}
		case ".info":
			var info struct {
				Version string
				Time    string
			}
			if json.Unmarshal(b, &info) != nil || info.Version != v {
				return errors.New("invalid module info")
			}
		}
	}
	for _, r := range m.Require {
		p, e := module.EscapePath(r.Mod.Path)
		if e != nil {
			return e
		}
		v, e := module.EscapeVersion(r.Mod.Version)
		if e != nil {
			return e
		}
		for _, ext := range []string{".mod", ".zip", ".info"} {
			if !seen[p+"/@v/"+v+ext] {
				return fmt.Errorf("missing locked module input: %s %s", r.Mod.Path, r.Mod.Version)
			}
		}
	}
	return nil
}

// PrepareEngineeringDependencyBundle is a definition-time, local-only utility.
// Cache bytes are untrusted transport and only admitted when verified against
// explicit repository locks. It executes only the trusted Go driver's closed
// module graph/download operations, never repository generators or tests.
func PrepareEngineeringDependencyBundle(ctx context.Context, repository, cache, goBinary, output string) (loop.DependencyBundle, error) {
	var result loop.DependencyBundle
	deny := errors.New("offline dependency definition preparation rejected")
	for _, p := range []string{repository, cache, goBinary, output} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return result, deny
		}
	}
	return prepareEngineeringDependencyBundle(ctx, repository, cache, goBinary, output)
}

func readDependencyInput(rootPath, name string, limit int64) ([]byte, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("unsafe dependency input")
	}
	return readRepositoryContextFile(root, name, info.Size())
}

func prepareEngineeringDependencyBundle(ctx context.Context, repository, cache, goBinary, output string) (loop.DependencyBundle, error) {
	var result loop.DependencyBundle
	mod, err := readDependencyInput(repository, "go.mod", 1<<20)
	if err != nil {
		return result, err
	}
	sum, err := readDependencyInput(repository, "go.sum", 8<<20)
	if err != nil {
		return result, err
	}
	sums, err := lockedSums(sum)
	if err != nil {
		return result, err
	}
	m, err := modfile.Parse("go.mod", mod, nil)
	if err != nil || m.Module == nil || len(m.Replace) > 0 || len(m.Exclude) > 0 {
		return result, errors.New("unsupported dependency topology")
	}
	before, toolWire, err := freezeToolchain(ctx, goBinary)
	if err != nil {
		return result, err
	}
	entries := map[string][]byte{}
	keys := []string{}
	var total int
	for key := range sums {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fields := strings.Split(key, " ")
		p, err := module.EscapePath(fields[0])
		if err != nil {
			return result, err
		}
		version := strings.TrimSuffix(fields[1], "/go.mod")
		v, err := module.EscapeVersion(version)
		if err != nil {
			return result, err
		}
		ext := ".zip"
		if strings.HasSuffix(fields[1], "/go.mod") {
			ext = ".mod"
		}
		name := p + "/@v/" + v + ext
		b, e := readDependencyInput(cache, filepath.Join("cache", "download", filepath.FromSlash(name)), maxDependencyBundleBytes)
		if e != nil {
			return result, e
		}
		total += len(b)
		if total > maxDependencyBundleBytes {
			return result, errors.New("dependency bundle exceeds bound")
		}
		entries[name] = b
		info, _ := json.Marshal(struct {
			Version string
			Time    string
		}{version, "1970-01-01T00:00:00Z"})
		entries[p+"/@v/"+v+".info"] = info
	}
	names := []string{}
	for p := range entries {
		names = append(names, p)
	}
	sort.Strings(names)
	var wire bytes.Buffer
	tw := tar.NewWriter(&wire)
	for _, name := range names {
		b := entries[name]
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0400, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return result, err
		}
		if _, err = tw.Write(b); err != nil {
			return result, err
		}
	}
	if err = tw.Close(); err != nil {
		return result, err
	}
	if wire.Len() > maxDependencyBundleBytes {
		return result, errors.New("dependency archive exceeds bound")
	}
	if err = validateLockedDependencyBundle(wire.Bytes(), mod, sum); err != nil {
		return result, err
	}
	home, err := os.MkdirTemp(filepath.Dir(output), ".aegis-dependency-check-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(home)
	proxy := filepath.Join(home, "proxy")
	if err = materializeDependencyBundle(ctx, wire.Bytes(), proxy); err != nil {
		return result, err
	}
	source := filepath.Join(home, "source")
	if err = os.Mkdir(source, 0700); err != nil {
		return result, err
	}
	for p, b := range map[string][]byte{"go.mod": mod, "go.sum": sum} {
		if err = os.WriteFile(filepath.Join(source, p), b, 0400); err != nil {
			return result, err
		}
	}
	env := []string{"PATH=" + filepath.Dir(goBinary), "GOROOT=" + before.Root, "HOME=" + home, "TMPDIR=" + home, "GOCACHE=" + filepath.Join(home, "build"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=file://" + proxy, "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "GOMAXPROCS=2", "CGO_ENABLED=0", "GOFLAGS=-mod=readonly -modcacherw -p=1"}
	// The lock is the admitted closure, not every optional/test-only module
	// mentioned in dependencies' go.mod graphs. Fetch each exact locked archive;
	// never expand the lock or download an un-summed module via `all`.
	download := []string{"mod", "download"}
	for _, key := range keys {
		if !strings.HasSuffix(key, "/go.mod") {
			fields := strings.Fields(key)
			download = append(download, fields[0]+"@"+fields[1])
		}
	}
	if len(download) == 2 {
		return result, errors.New("empty dependency closure")
	}
	for _, args := range [][]string{download} {
		cmd := exec.CommandContext(ctx, goBinary, args...)
		cmd.Dir = source
		cmd.Env = env
		var out boundedOutput
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err = cmd.Run(); err != nil {
			return result, fmt.Errorf("offline locked module closure unavailable: %w: %s", err, out.bytes)
		}
		if out.overflow {
			return result, errors.New("module closure output exceeds bound")
		}
	}
	for p, b := range map[string][]byte{"go.mod": mod, "go.sum": sum} {
		observed, e := os.ReadFile(filepath.Join(source, p))
		original, e2 := os.ReadFile(filepath.Join(repository, p))
		if e != nil || e2 != nil || !bytes.Equal(observed, b) || !bytes.Equal(original, b) {
			return result, errors.New("dependency lock changed during preparation")
		}
	}
	_, after, err := freezeToolchain(ctx, goBinary)
	if err != nil || !bytes.Equal(toolWire, after) {
		return result, errors.New("native toolchain changed during preparation")
	}
	f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
	if err != nil {
		return result, err
	}
	_, err = f.Write(wire.Bytes())
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return result, err
	}
	if closeErr != nil {
		return result, closeErr
	}
	result = loop.DependencyBundle{Path: output, Digest: digest(wire.Bytes())}
	return result, nil
}
