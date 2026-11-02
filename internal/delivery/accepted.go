package delivery

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/berryhill/aegis/internal/implementation"
)

// ProjectAccepted is pure: it reads frozen Git objects and projects authored
// publication bytes/OID in memory, without object/index/ref writes. The caller must
// hold real accepted-candidate custody. The synthetic freeze commit is never the
// publication identity. Every tree entry (including ignored/read-only inputs)
// must match the full inventory; only changed entries require the reviewed scope.
func (g *Git) ProjectAccepted(ctx context.Context, c implementation.ContributionCandidate, s CommitSpec, allowed []string) (CommitSpec, []GitObject, error) {
	deny := func() (CommitSpec, []GitObject, error) { return CommitSpec{}, nil, ErrDenied }
	if !sha.MatchString(c.CandidateCommit) || !sha.MatchString(c.CandidateTree) || !sha.MatchString(s.Parent) || c.HeadCommit != c.Base || s.Tree != c.CandidateTree || c.Source.Schema != "aegis.contribution.source.v1" {
		return deny()
	}
	// A controller-admitted repair publishes a nonforce child of the original
	// owned head. Pure ancestry checks cannot grant that admission; Source and
	// canonical Reserve independently validate the immutable failed predecessor.
	if s.Parent != c.Base {
		if _, err := g.run(ctx, nil, nil, "merge-base", "--is-ancestor", c.Base, s.Parent); err != nil {
			return deny()
		}
	}
	parents, e := g.run(ctx, nil, nil, "rev-list", "--parents", "-n", "1", c.CandidateCommit)
	if e != nil || strings.TrimSpace(string(parents)) != c.CandidateCommit+" "+c.Base {
		return deny()
	}
	tree, e := g.run(ctx, nil, nil, "rev-parse", c.CandidateCommit+"^{tree}")
	if e != nil || strings.TrimSpace(string(tree)) != c.CandidateTree {
		return deny()
	}
	type entry struct{ mode, oid string }
	read := func(oid string) (map[string]entry, error) {
		if !sha.MatchString(oid) {
			return nil, ErrDenied
		}
		b, e := g.run(ctx, nil, nil, "ls-tree", "-r", "-z", oid)
		if e != nil {
			return nil, ErrDenied
		}
		out := map[string]entry{}
		for _, line := range bytes.Split(b, []byte{0}) {
			if len(line) == 0 {
				continue
			}
			fields := bytes.SplitN(line, []byte{'\t'}, 2)
			if len(fields) != 2 {
				return nil, ErrDenied
			}
			h := strings.Fields(string(fields[0]))
			p := string(fields[1])
			if len(h) != 3 || h[1] != "blob" || !safePath(p) || privatePath(p) || (h[0] != "100644" && h[0] != "100755") || !sha.MatchString(h[2]) {
				return nil, ErrDenied
			}
			if _, ok := out[p]; ok {
				return nil, ErrDenied
			}
			out[p] = entry{h[0], h[2]}
		}
		return out, nil
	}
	accepted, e := read(c.CandidateTree)
	if e != nil || len(accepted) != len(c.Source.Files) {
		return deny()
	}
	base, e := read(c.Base)
	if e != nil {
		return deny()
	}
	// CurrentBase is usable only after the native owner has independently linked
	// it to immutable integration/readback evidence. Git still independently
	// proves ancestry and exact upstream bytes here; this never widens builder
	// scope. A nonwritable entry must equal the current base, including deletion.
	upstream := base
	if c.CurrentBase != "" {
		if !sha.MatchString(c.CurrentBase) || c.CurrentBase == c.Base {
			return deny()
		}
		if _, err := g.run(ctx, nil, nil, "merge-base", "--is-ancestor", c.Base, c.CurrentBase); err != nil {
			return deny()
		}
		upstream, e = read(c.CurrentBase)
		if e != nil {
			return deny()
		}
	}
	scope := map[string]bool{}
	for _, p := range allowed {
		if !safePath(p) || privatePath(p) || scope[p] {
			return deny()
		}
		scope[p] = true
	}
	seen := map[string]bool{}
	s.Files = nil
	for _, f := range c.Source.Files {
		mode := "100644"
		if f.Mode&0111 != 0 {
			mode = "100755"
		}
		a, ok := accepted[f.Path]
		if !ok || seen[f.Path] || a.mode != mode || a.oid != objectOID("blob", f.Content) {
			return deny()
		}
		seen[f.Path] = true
		if !scope[f.Path] {
			if u, ok := upstream[f.Path]; !ok || u != a {
				return deny()
			}
		}
		if b, ok := base[f.Path]; !ok || b != a {
			s.Files = append(s.Files, File{f.Path, mode, append([]byte(nil), f.Content...)})
		}
	}
	for p := range upstream {
		if !seen[p] && !scope[p] {
			return deny()
		}
	}
	for p := range base {
		if !seen[p] {
			if _, exists := upstream[p]; exists && !scope[p] {
				return deny()
			}
			s.Files = append(s.Files, File{Path: p, Mode: "000000"})
		}
	}
	slices.SortFunc(s.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	if g.files(s) != nil {
		return deny()
	}
	if s.IntegrationParent != "" && s.IntegrationParent != c.CurrentBase {
		return deny()
	}
	if c.CurrentBase != "" {
		if s.Parent == c.Base || s.Parent == c.CurrentBase {
			return deny()
		}
		s.IntegrationParent = c.CurrentBase
	}
	authored, e := authoredObject(s)
	if e != nil || authored.OID == c.CandidateCommit {
		return deny()
	}
	// Read only the already frozen #376 graph. Its exact tree/blob objects are
	// retained; only the final synthetic commit is replaced in memory. A repair's
	// publication parent is the preceding owned head, but the frozen native
	// candidate remains anchored to its original admitted base.
	objects, e := g.ExtractObjects(ctx, c.CandidateCommit, s.Tree, c.Base)
	if e != nil || len(objects) == 0 || objects[len(objects)-1].Kind != "commit" || objects[len(objects)-1].OID != c.CandidateCommit {
		return deny()
	}
	objects[len(objects)-1] = authored
	s.Commit = authored.OID
	return s, objects, nil
}
