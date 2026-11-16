package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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
	rootInfo, err := os.Lstat(m.Root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm()&0022 != 0 {
		return m, nil, bad
	}
	root, err := os.OpenRoot(m.Root)
	if err != nil {
		return m, nil, err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(rootInfo, actual) {
		return m, nil, bad
	}
	var total int64
	// Retain each directory descriptor only during this fresh traversal. Every
	// file is fully reread through the same no-follow, size/EOF/identity reader;
	// do not reopen its entire ancestor chain for every sibling. No observation,
	// byte hash or integrity decision survives this call.
	var scratch []byte
	var walk func(*os.Root, string) error
	walk = func(parent *os.Root, prefix string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		dir, err := parent.Open(".")
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(-1)
		closeErr := dir.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			rel := filepath.Join(prefix, name)
			info, err := parent.Lstat(name)
			if err != nil {
				return err
			}
			// Same excluded non-input directories as the original full manifest.
			if info.IsDir() && (rel == "pkg/mod" || rel == "pkg/sumdb" || rel == "doc" || rel == "test" || rel == "api" || rel == "misc") {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
				return fmt.Errorf("%w: unsafe %s", bad, rel)
			}
			if info.IsDir() {
				child, err := parent.OpenRoot(name)
				if err != nil {
					return err
				}
				opened, err := child.Stat(".")
				if err == nil && !os.SameFile(info, opened) {
					err = bad
				}
				if err == nil {
					err = walk(child, rel)
				}
				closeErr := child.Close()
				if err != nil {
					return err
				}
				if closeErr != nil {
					return closeErr
				}
				current, err := parent.Lstat(name)
				if err != nil || !os.SameFile(info, current) || info.Mode() != current.Mode() {
					return bad
				}
				continue
			}
			if !info.Mode().IsRegular() || !singleLink(info) || info.Size() > 64<<20 || len(m.Files) >= 50000 {
				return bad
			}
			total += info.Size()
			if total > 1<<30 {
				return bad
			}
			b, err := readRepositoryContextFileBuffer(parent, name, info.Size(), scratch)
			if err != nil {
				return err
			}
			scratch = b
			m.Files = append(m.Files, toolchainFile{rel, uint32(info.Mode().Perm()), digest(b)})
		}
		return ctx.Err()
	}
	if err = walk(root, ""); err != nil {
		return m, nil, err
	}
	current, err := os.Lstat(m.Root)
	if err != nil || !os.SameFile(rootInfo, current) || rootInfo.Mode() != current.Mode() {
		return m, nil, bad
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
