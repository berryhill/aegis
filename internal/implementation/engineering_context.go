package implementation

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"strings"
)

// ResolveEngineeringContext reloads only issue-selected paths inside the
// independently approved repository. Existing trusted inputs are never replaced.
// New regression files carry explicit negative preimages, not assumed empty bytes.
func ResolveEngineeringContext(ctx context.Context, template loop.VerifiedImplementation, task, acceptance, workspace, repository string, writable, contextFiles []string) (loop.VerifiedImplementation, error) {
	v := cloneImplementation(template)
	v.SchemaVersion = loop.RepositoryEngineeringSchema
	v.Task = task
	v.Acceptance = acceptance
	v.Workspace = workspace
	v.WritableFiles = append([]string{}, writable...)
	if v.RepositoryContext == nil {
		return v, errors.New("immutable engineering checker context required")
	}
	m := v.RepositoryContext
	m.TaskDigest = digest([]byte(task))
	root, err := os.OpenRoot(repository)
	if err != nil {
		return v, err
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, f := range m.Files {
		seen[f.Path] = true
	}
	for i, p := range append(append([]string{}, writable...), contextFiles...) {
		if ctx.Err() != nil {
			return v, ctx.Err()
		}
		if !loop.SafeRepositoryContextPath(p) || seen[p] {
			return v, errors.New("engineering context overlap or unsafe input")
		}
		seen[p] = true
		info, err := root.Lstat(p)
		if errors.Is(err, os.ErrNotExist) && i < len(writable) && strings.HasSuffix(p, "_test.go") {
			if verifyAbsentRegression(root, p) != nil {
				return v, errors.New("new regression parent rejected")
			}
			m.Files = append(m.Files, loop.RepositoryContextFile{Path: p, Role: "regression_test", Absent: true, Digest: digest(nil)})
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > m.MaxFileBytes {
			return v, errors.New("bounded existing engineering context required")
		}
		b, err := readRepositoryContextFile(root, p, info.Size())
		if err != nil {
			return v, err
		}
		role := "source"
		if strings.HasSuffix(p, "_test.go") {
			if i >= len(writable) {
				return v, errors.New("test acceptance must be fixed independently")
			}
			role = "regression_test"
		}
		m.Files = append(m.Files, loop.RepositoryContextFile{Path: p, Role: role, Bytes: int64(len(b)), Digest: digest(b)})
	}
	m.SourceDigest = RepositorySourceDigest(m)
	if err = v.Validate(); err != nil {
		return v, err
	}
	observed := v
	observed.Workspace = repository
	_, err = CollectRepositoryContext(ctx, observed, m.SourceDigest)
	return v, err
}
