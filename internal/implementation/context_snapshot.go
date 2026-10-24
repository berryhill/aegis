package implementation

import (
	"encoding/json"
	"errors"
	"os"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/loop"
)

// contractSnapshot never enumerates a context-enabled repository. Custody is
// limited to declared files and the checker's root module input. Legacy nil
// contracts retain their historical whole-workspace artifact.
func contractSnapshot(c loop.VerifiedImplementation) ([]byte, error) {
	if c.RepositoryContext == nil {
		return snapshot(c.Workspace)
	}
	reject := errors.New("repository context rejected")
	m := c.RepositoryContext
	if m.Validate(c.Task, c.WritableFiles) != nil || RepositorySourceDigest(m) != m.SourceDigest {
		return nil, reject
	}
	module := false
	for _, f := range m.Files {
		if f.Path == "go.mod" && f.Role == "checker_input" {
			module = true
		}
	}
	if !module {
		return nil, reject
	}
	for _, p := range c.WritableFiles {
		if p == "go.mod" {
			return nil, reject
		}
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Inspect all declared metadata and current budgets before reading contents.
	infos := make([]os.FileInfo, len(m.Files))
	var size int64
	for i, f := range m.Files {
		info, err := root.Lstat(f.Path)
		if err != nil || !DoerTargetHasSingleLink(info) || info.Size() < 0 || info.Size() > m.MaxFileBytes || info.Size() > m.MaxTotalBytes-size {
			return nil, reject
		}
		infos[i] = info
		size += info.Size()
	}
	type file struct {
		Path    string `json:"path"`
		Content []byte `json:"content"`
	}
	files := []file{}
	var total int64
	for i, f := range m.Files {
		p := f.Path
		if !loop.SafeRepositoryContextPath(p) {
			return nil, errors.New("repository context rejected")
		}
		info := infos[i]
		if info.Size() > m.MaxTotalBytes-total {
			return nil, errors.New("repository context rejected")
		}
		b, err := readRepositoryContextFile(root, p, info.Size())
		if err != nil || !utf8.Valid(b) {
			return nil, errors.New("repository context rejected")
		}
		writable := false
		for _, name := range c.WritableFiles {
			writable = writable || name == p
		}
		if !writable && (int64(len(b)) != f.Bytes || digest(b) != f.Digest) {
			return nil, reject
		}
		total += int64(len(b))
		files = append(files, file{p, b})
	}
	return json.Marshal(files)
}
