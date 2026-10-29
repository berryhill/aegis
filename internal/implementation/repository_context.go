package implementation

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/loop"
)

type RepositoryContextText struct {
	Path   string `json:"path"`
	Role   string `json:"role"`
	Digest string `json:"digest"`
	Text   string `json:"text"`
}

// CollectedRepositoryContext contains untrusted task data. It supplies no tools,
// authority or writable paths. No partial result is returned on any rejection.
type CollectedRepositoryContext struct {
	Identity     string                  `json:"identity"`
	SourceDigest string                  `json:"source_digest"`
	Files        []RepositoryContextText `json:"files"`
}

// CollectRepositoryContext reads only explicitly committed Go source/test paths.
// expectedSourceDigest must come from the controller's trusted source-state
// binding, never model text. File preimages independently detect listed-file drift.
// This primitive does not discover Git state or execute code.
func CollectRepositoryContext(ctx context.Context, c loop.VerifiedImplementation, expectedSourceDigest string) (*CollectedRepositoryContext, error) {
	reject := errors.New("repository context rejected")
	m := c.RepositoryContext
	if ctx == nil || m == nil || m.Validate(c.Task, c.WritableFiles) != nil || m.SourceDigest != expectedSourceDigest || !filepath.IsAbs(c.Workspace) || filepath.Clean(c.Workspace) != c.Workspace || c.Workspace == string(filepath.Separator) {
		return nil, reject
	}
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return nil, reject
	}
	before, err := os.Lstat(c.Workspace)
	if err != nil || !before.IsDir() {
		return nil, reject
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return nil, reject
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(before, actual) {
		return nil, reject
	}
	id, err := m.Digest()
	if err != nil {
		return nil, reject
	}
	result := &CollectedRepositoryContext{Identity: id, SourceDigest: m.SourceDigest}
	var total int64
	for _, entry := range m.Files {
		if ctx.Err() != nil {
			return nil, reject
		}
		b, err := readRepositoryContextFile(root, entry.Path, entry.Bytes)
		if err != nil || int64(len(b)) != entry.Bytes || int64(len(b)) > m.MaxTotalBytes-total || digest(b) != entry.Digest || !utf8.Valid(b) {
			return nil, reject
		}
		for _, r := range string(b) {
			if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
				return nil, reject
			}
		}
		// Reject recognizable private key payloads even under an innocuous Go name.
		if strings.Contains(string(b), "PRIVATE KEY-----") {
			return nil, reject
		}
		total += int64(len(b))
		result.Files = append(result.Files, RepositoryContextText{Path: entry.Path, Role: entry.Role, Digest: entry.Digest, Text: string(b)})
	}
	if ctx.Err() != nil {
		return nil, reject
	}
	return result, nil
}

func readRepositoryContextFile(root *os.Root, name string, expected int64) ([]byte, error) {
	reject := errors.New("repository context rejected")
	parent := root
	var owned []*os.Root
	defer func() {
		for i := len(owned) - 1; i >= 0; i-- {
			owned[i].Close()
		}
	}()
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		info, err := parent.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, reject
		}
		child, err := parent.OpenRoot(part)
		if err != nil {
			return nil, reject
		}
		actual, err := child.Stat(".")
		if err != nil || !os.SameFile(info, actual) {
			child.Close()
			return nil, reject
		}
		owned = append(owned, child)
		parent = child
	}
	base := parts[len(parts)-1]
	info, err := parent.Lstat(base)
	if err != nil || !info.Mode().IsRegular() || !singleLink(info) || info.Size() != expected {
		return nil, reject
	}
	f, err := openRepositoryContextFile(parent, base)
	if err != nil {
		return nil, reject
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !actual.Mode().IsRegular() || !singleLink(actual) || !os.SameFile(info, actual) || actual.Size() != expected {
		return nil, reject
	}
	// The descriptor's bounded size is already known. Read into one exact-sized
	// allocation rather than repeatedly growing/copying ReadAll's buffer (the
	// full native distribution is read at every integrity boundary). Still read
	// every byte, and probe EOF so growth cannot be hidden by the allocation.
	if expected < 0 || uint64(expected) > uint64(^uint(0)>>1) {
		return nil, reject
	}
	b := make([]byte, int(expected))
	if _, err = io.ReadFull(f, b); err != nil {
		return nil, reject
	}
	var extra [1]byte
	if n, err := f.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, reject
	}
	after, err := f.Stat()
	if err != nil || after.Size() != actual.Size() || !after.ModTime().Equal(actual.ModTime()) {
		return nil, reject
	}
	current, err := parent.Lstat(base)
	if err != nil || !os.SameFile(actual, current) {
		return nil, reject
	}
	return b, nil
}
