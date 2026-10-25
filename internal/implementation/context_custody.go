package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/loop"
)

// RepositorySourceDigest commits the ordered explicit path/role/size/content
// preimages, not a branch name or the manifest's own SourceDigest. The executor
// verifies every preimage from controller-read bytes before any model effect.
func RepositorySourceDigest(m *loop.RepositoryContext) string {
	if m == nil {
		return ""
	}
	b, _ := json.Marshal(struct {
		Kind  string                       `json:"kind"`
		Files []loop.RepositoryContextFile `json:"files"`
	}{"explicit-go-source.v1", m.Files})
	return digest(b)
}

func cloneImplementation(c loop.VerifiedImplementation) loop.VerifiedImplementation {
	c.WritableFiles = append([]string(nil), c.WritableFiles...)
	c.Policy.Packages = append([]string(nil), c.Policy.Packages...)
	c.Policy.RequiredTests = append([]loop.RequiredGoTest(nil), c.Policy.RequiredTests...)
	if c.RepositoryContext != nil {
		m := *c.RepositoryContext
		m.Files = append([]loop.RepositoryContextFile(nil), m.Files...)
		c.RepositoryContext = &m
	}
	return c
}

func cloneContext(c *CollectedRepositoryContext) *CollectedRepositoryContext {
	if c == nil {
		return nil
	}
	v := *c
	v.Files = append([]RepositoryContextText(nil), c.Files...)
	return &v
}

func verifyContext(ctx context.Context, c loop.VerifiedImplementation, expected *CollectedRepositoryContext) error {
	if expected == nil {
		return nil
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return errors.New("repository context rejected")
	}
	defer root.Close()
	var total int64
	for _, f := range expected.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		size := int64(len(f.Text))
		total += size
		if size > c.RepositoryContext.MaxFileBytes || total > c.RepositoryContext.MaxTotalBytes {
			return errors.New("repository context rejected")
		}
		b, err := readRepositoryContextFile(root, f.Path, size)
		if err != nil || digest(b) != f.Digest || string(b) != f.Text {
			return errors.New("repository context rejected")
		}
	}
	return nil
}

func advanceContext(c loop.VerifiedImplementation, state *CollectedRepositoryContext, edits []Edit) {
	if state == nil {
		return
	}
	for _, edit := range edits {
		for i := range state.Files {
			if state.Files[i].Path == edit.Path {
				state.Files[i].Text = string(edit.Content)
				state.Files[i].Digest = digest(edit.Content)
			}
		}
	}
}

func (e *Executor) contextBlob(v *CollectedRepositoryContext) (string, error) {
	if v == nil {
		return "", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return e.blob(b)
}

func (e *Executor) revalidateContext(c loop.VerifiedImplementation, r Record) error {
	reject := errors.New("repository context evidence mismatch")
	if c.RepositoryContext == nil {
		if r.ContextDigest != "" {
			return reject
		}
		for _, p := range r.Passes {
			if p.ContextIdentity != "" || p.ContextStateDigest != "" {
				return reject
			}
		}
		return nil
	}
	m := c.RepositoryContext
	if RepositorySourceDigest(m) != m.SourceDigest {
		return reject
	}
	id, err := m.Digest()
	if err != nil {
		return reject
	}
	load := func(d string) (*CollectedRepositoryContext, error) {
		b, err := e.loadBlob(d)
		if err != nil {
			return nil, reject
		}
		var v CollectedRepositoryContext
		if json.Unmarshal(b, &v) != nil || v.Identity != id || v.SourceDigest != m.SourceDigest || len(v.Files) != len(m.Files) {
			return nil, reject
		}
		return &v, nil
	}
	initial, err := load(r.ContextDigest)
	if err != nil {
		return err
	}
	for i, f := range initial.Files {
		entry := m.Files[i]
		if f.Path != entry.Path || f.Role != entry.Role || f.Digest != entry.Digest || digest([]byte(f.Text)) != entry.Digest || int64(len(f.Text)) != entry.Bytes {
			return reject
		}
	}
	writable := map[string]bool{}
	for _, p := range c.WritableFiles {
		writable[p] = true
	}
	var last *CollectedRepositoryContext
	for _, p := range r.Passes {
		if p.ContextIdentity != id {
			return reject
		}
		last, err = load(p.ContextStateDigest)
		if err != nil {
			return err
		}
		for i, f := range last.Files {
			original := initial.Files[i]
			if f.Path != original.Path || f.Role != original.Role || digest([]byte(f.Text)) != f.Digest || (!writable[f.Path] && f != original) {
				return reject
			}
		}
	}
	if last == nil {
		return reject
	}
	return verifyContext(context.Background(), c, last)
}
