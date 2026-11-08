package implementation

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
	"golang.org/x/mod/modfile"
)

const maxDependencyBundleBytes = 256 << 20

func dependencyBundleBytes(b loop.DependencyBundle) ([]byte, error) {
	deny := errors.New("offline dependency bundle missing, unsafe or changed")
	if b.Validate() != nil {
		return nil, deny
	}
	root, err := os.OpenRoot("/")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := strings.TrimPrefix(b.Path, "/")
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDependencyBundleBytes || info.Mode().Perm()&0022 != 0 {
		return nil, deny
	}
	wire, err := readRepositoryContextFile(root, name, info.Size())
	if err != nil || digest(wire) != b.Digest {
		return nil, deny
	}
	return wire, nil
}

// Materialize a pinned, finite Go file proxy. No links, devices, duplicate names,
// path traversal, ambient module cache, network download or local replacements.
func materializeDependencyBundle(ctx context.Context, wire []byte, destination string) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	r := tar.NewReader(bytes.NewReader(wire))
	seen := map[string]bool{}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.New("invalid dependency archive")
		}
		name := h.Name
		if !filepath.IsLocal(name) || filepath.Clean(name) != name || strings.ContainsAny(name, "\\\x00\r\n") || seen[name] || len(seen) >= 8192 || h.Size < 0 || h.Size > maxDependencyBundleBytes || total+h.Size > 2*maxDependencyBundleBytes {
			return errors.New("unsafe or unbounded dependency archive")
		}
		seen[name] = true
		if h.Typeflag != tar.TypeReg || !strings.Contains(name, "/@v/") || !(strings.HasSuffix(name, ".mod") || strings.HasSuffix(name, ".info") || strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, "/@v/list")) {
			return errors.New("only module proxy regular files supported")
		}
		total += h.Size
		if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0400)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyN(f, r, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}

func qualifiedModule(s SourceInventory, p loop.ContributionExecutionPolicy) error {
	if p.Kind != loop.EngineeringExecutionKind {
		return stdlibModule(s)
	}
	var mod, sum []byte
	for _, f := range s.Files {
		switch f.Path {
		case "go.mod":
			mod = f.Content
		case "go.sum":
			sum = f.Content
		}
	}
	m, err := modfile.Parse("go.mod", mod, nil)
	if err != nil || m.Module == nil || len(m.Replace) > 0 || len(m.Exclude) > 0 {
		return errors.New("engineering module must preserve qualified dependency topology")
	}
	// Tool directives are package declarations, never new commands. Each must
	// resolve beneath an already pinned required module; native execution remains
	// the closed build/test operations with GOTOOLCHAIN=local and readonly sums.
	for _, tool := range m.Tool {
		pinned := false
		for _, req := range m.Require {
			if tool.Path == req.Mod.Path || strings.HasPrefix(tool.Path, req.Mod.Path+"/") {
				pinned = true
			}
		}
		if !pinned || strings.Contains(tool.Path, "..") || strings.ContainsAny(tool.Path, "\\\\\x00\r\n") {
			return errors.New("tool package requires pinned dependency")
		}
	}
	if len(m.Require) > 0 {
		if p.Dependencies == nil || len(sum) == 0 {
			return errors.New("dependencies require immutable sums and approved offline bundle")
		}
		wire, e := dependencyBundleBytes(*p.Dependencies)
		if e != nil {
			return e
		}
		if err = validateLockedDependencyBundle(wire, mod, sum); err != nil {
			return err
		}
	}
	return nil
}
