package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/berryhill/aegis/internal/loop"
	"golang.org/x/mod/modfile"
)

// materializeCheckerContext reloads only declared inputs. The controller's
// current state, not a new workspace observation, defines writable preimages.
// A root module must be explicitly committed; implicit module discovery could
// import unlisted checker policy. This is input isolation, not a host sandbox.
func materializeCheckerContext(ctx context.Context, c loop.VerifiedImplementation, expected *CollectedRepositoryContext, workspace string) error {
	reject := errors.New("repository context rejected")
	original := c.RepositoryContext
	if original == nil || expected == nil || original.Validate(c.Task, c.WritableFiles) != nil || RepositorySourceDigest(original) != original.SourceDigest || len(expected.Files) != len(original.Files) {
		return reject
	}
	identity, err := original.Digest()
	if err != nil || expected.Identity != identity || expected.SourceDigest != original.SourceDigest {
		return reject
	}
	candidate := cloneImplementation(c)
	writable := map[string]bool{}
	for _, p := range c.WritableFiles {
		writable[p] = true
	}
	module := false
	for i, f := range expected.Files {
		entry := original.Files[i]
		if f.Path != entry.Path || f.Role != entry.Role || digest([]byte(f.Text)) != f.Digest {
			return reject
		}
		if writable[f.Path] {
			if f.Role != "source" {
				return reject
			}
		} else if f.Digest != entry.Digest || int64(len(f.Text)) != entry.Bytes {
			return reject
		}
		candidate.RepositoryContext.Files[i].Bytes = int64(len(f.Text))
		candidate.RepositoryContext.Files[i].Digest = f.Digest
		module = module || f.Path == "go.mod"
	}
	if !module {
		return reject
	}
	candidate.RepositoryContext.SourceDigest = RepositorySourceDigest(candidate.RepositoryContext)
	current, err := CollectRepositoryContext(ctx, candidate, candidate.RepositoryContext.SourceDigest)
	if err != nil {
		return err
	}
	for _, f := range current.Files {
		if f.Path != "go.mod" {
			continue
		}
		module, err := modfile.Parse("go.mod", []byte(f.Text), nil)
		if err != nil {
			return reject
		}
		// Offline resolution must not follow local replacements back into the
		// original repository (or any other unlisted host source tree).
		for _, replacement := range module.Replace {
			if replacement.New.Version == "" {
				return reject
			}
		}
	}
	if err = os.Mkdir(workspace, 0700); err != nil {
		return err
	}
	for _, f := range current.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		target := filepath.Join(workspace, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.WriteString(f.Text)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return verifyContext(ctx, c, expected)
}
