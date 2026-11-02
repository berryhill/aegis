package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

type toolchainFile struct {
	Path   string
	Mode   uint32
	Digest string
}
type toolchainManifest struct {
	Root, Driver, OS, Arch string
	Files                  []toolchainFile
}

// The entire resolved distribution is an input, including compiler, linker,
// assembly headers, standard library sources and distribution configuration.
// Only bounded hashes, never toolchain contents or ambient environment, persist.
func freezeToolchain(ctx context.Context, driver string) (toolchainManifest, []byte, error) {
	m := toolchainManifest{Driver: driver, Root: filepath.Dir(filepath.Dir(driver)), OS: runtime.GOOS, Arch: runtime.GOARCH}
	bad := errors.New("unsafe or incomplete native toolchain")
	real, err := filepath.EvalSymlinks(driver)
	if err != nil || real != driver || filepath.Base(driver) != "go" || filepath.Base(filepath.Dir(driver)) != "bin" {
		return m, nil, bad
	}
	for _, p := range []string{"VERSION", "go.env", "src/runtime/runtime.go", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/compile", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/link"} {
		info, e := os.Lstat(filepath.Join(m.Root, p))
		if e != nil || !info.Mode().IsRegular() {
			return m, nil, fmt.Errorf("%w: missing %s", bad, p)
		}
	}
	root, err := os.OpenRoot(m.Root)
	if err != nil {
		return m, nil, err
	}
	defer root.Close()
	var total int64
	// One call-local scratch allocation, never shared with another validation.
	// Each file is fully reread and hashed before the next read overwrites it.
	var scratch []byte
	err = filepath.WalkDir(m.Root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		relPath, _ := filepath.Rel(m.Root, p)
		// GOPATH may share GOROOT. Module caches and docs are not native inputs;
		// every actual distribution input is included, with no dependency downloads.
		if d.IsDir() && (relPath == "pkg/mod" || relPath == "pkg/sumdb" || relPath == "doc" || relPath == "test" || relPath == "api" || relPath == "misc") {
			return filepath.SkipDir
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("%w: unsafe %s", bad, relPath)
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() || !singleLink(info) || info.Size() > 64<<20 || len(m.Files) >= 50000 {
			return bad
		}
		total += info.Size()
		if total > 1<<30 {
			return bad
		}
		b, e := readRepositoryContextFileBuffer(root, relPath, info.Size(), scratch)
		if e != nil {
			return e
		}
		scratch = b
		rel, e := filepath.Rel(m.Root, p)
		if e != nil {
			return e
		}
		m.Files = append(m.Files, toolchainFile{rel, uint32(info.Mode().Perm()), digest(b)})
		return nil
	})
	if err != nil {
		return m, nil, err
	}
	b, err := json.Marshal(m)
	return m, b, err
}
func (e *ManagedContribution) verifyToolchain(ctx context.Context) (toolchainManifest, error) {
	m, b, err := freezeToolchain(ctx, e.GoBinary)
	if err != nil {
		return m, err
	}
	d := digest(b)
	if e.toolchainDigest == "" {
		e.toolchainDigest = d
		if err = e.DB.Create(blobKey(d), b); err != nil {
			existing, x := e.DB.Get(blobKey(d))
			if x != nil || digest(existing) != d {
				return m, err
			}
		}
	}
	if d != e.toolchainDigest {
		return m, errors.New("frozen native toolchain changed")
	}
	return m, nil
}
