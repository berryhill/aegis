package delivery

import (
	"bytes"
	"context"
	"github.com/berryhill/aegis/internal/implementation"
	"os"
	"path/filepath"
	"testing"
)

// NEGATIVE regression promoted from independent V17 evidence; that immutable
// reproducer remains untouched. Old ProjectAccepted creates an unadmitted object.
func TestProjectAcceptedNoUnadmittedObjects(t *testing.T) {
	ctx := context.Background()
	g, freeze := commitFixture(t)
	c := implementation.ContributionCandidate{CandidateCommit: freeze.Commit, CandidateTree: freeze.Tree, Base: freeze.Parent, HeadCommit: freeze.Parent, Source: implementation.SourceInventory{Schema: "aegis.contribution.source.v1", Files: []implementation.SourceFile{{Path: "source", Mode: 0755, Content: []byte("after")}}}}
	snapshot := func() []byte {
		t.Helper()
		var out []byte
		for _, args := range [][]string{{"cat-file", "--batch-all-objects", "--batch-check=%(objectname)"}, {"show-ref"}, {"ls-files", "--stage"}, {"rev-parse", "HEAD"}} {
			b, e := g.run(ctx, nil, nil, args...)
			if e != nil {
				t.Fatal(e)
			}
			out = append(out, b...)
		}
		b, e := os.ReadFile(filepath.Join(g.Directory, ".git", "index"))
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, b...)
		b, e = os.ReadFile(filepath.Join(g.Directory, "source"))
		if e != nil {
			t.Fatal(e)
		}
		return append(out, b...)
	}
	before := snapshot()
	spec := freeze
	spec.Commit = ""
	spec.Message = "Reviewed publication metadata distinct from freeze\n"
	spec.Author = Identity{"Reviewed Author", "reviewed@example.invalid"}
	var got CommitSpec
	var objects []GitObject
	for range 3 {
		var e error
		got, objects, e = g.ProjectAccepted(ctx, c, spec, []string{"source"})
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(before, snapshot()) {
			t.Fatal("NEGATIVE: projection changed Git/source state before intent")
		}
		if _, e = g.run(ctx, nil, nil, "cat-file", "-e", got.Commit); e == nil {
			t.Fatal("unadmitted publication object exists")
		}
	}
	frozen, e := g.ExtractObjects(ctx, freeze.Commit, freeze.Tree, freeze.Parent)
	if e != nil {
		t.Fatal(e)
	}
	for n := 0; n < len(objects)-1; n++ {
		if frozen[n].OID != objects[n].OID || !bytes.Equal(frozen[n].Raw, objects[n].Raw) {
			t.Fatal("accepted tree/blob changed")
		}
	}
	p := fixturePlan()
	p.Commit = got
	p.Head = got.Commit
	p.BaseHead = got.Parent
	p.Branch = got.Branch
	p.Intake.Worktree = g.Directory
	// Unknown/missing-object readback must not call prepare or create objects,
	// even with permission to read the same intent.
	intent, err := intentFor(p, Commit)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := ClosedAdapter{Git: g}
	if o := readOnly.Reconcile(ctx, intent, p, func(context.Context) error { return nil }); o.Outcome != Unknown || !bytes.Equal(before, snapshot()) {
		t.Fatal("unknown readback wrote unadmitted objects", o)
	}
	if o := readOnly.Apply(ctx, intent, p, func(context.Context) error { return ErrDenied }); o.Outcome != Denied || !bytes.Equal(before, snapshot()) {
		t.Fatal("denied adapter wrote unadmitted objects", o)
	}
	owner := &memoryOwner{}
	authority := &fixtureAuthority{denied: true}
	adapter := &lostLocalAdapter{actual: ClosedAdapter{Git: g}}
	engine := NewEngine(owner, fixtureSource{p}, authority, adapter)
	r, e := engine.Execute(ctx, p.Reference, Commit)
	if e != nil || r.Outcome != Denied || !bytes.Equal(before, snapshot()) {
		t.Fatalf("denied commit changed state: %+v %v", r, e)
	}
	// New independent owner intent after explicit authority, not retry of denial.
	owner = &memoryOwner{}
	authority.denied = false
	adapter.lost = true
	engine = NewEngine(owner, fixtureSource{p}, authority, adapter)
	head, _ := g.run(ctx, nil, nil, "rev-parse", "HEAD")
	index, _ := os.ReadFile(filepath.Join(g.Directory, ".git", "index"))
	r, e = engine.Execute(ctx, p.Reference, Commit)
	if e != nil || r.Outcome != Unknown {
		t.Fatalf("lost response: %+v %v", r, e)
	}
	raw, e := g.run(ctx, nil, nil, "cat-file", "commit", got.Commit)
	if e != nil || !bytes.Equal(raw, objects[len(objects)-1].Raw) {
		t.Fatal("actual authored object differs", e)
	}
	r, e = engine.Execute(ctx, p.Reference, Commit)
	if e != nil || r.Outcome != Confirmed || adapter.writes != 1 {
		t.Fatalf("same intent blind write: %+v %v writes=%d", r, e, adapter.writes)
	}
	afterHead, _ := g.run(ctx, nil, nil, "rev-parse", "HEAD")
	afterIndex, _ := os.ReadFile(filepath.Join(g.Directory, ".git", "index"))
	source, _ := os.ReadFile(filepath.Join(g.Directory, "source"))
	if !bytes.Equal(head, afterHead) || !bytes.Equal(index, afterIndex) || string(source) != "after" {
		t.Fatal("authorized commit touched HEAD/index/source")
	}
}

type lostLocalAdapter struct {
	actual ClosedAdapter
	lost   bool
	writes int
}

func (a *lostLocalAdapter) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	a.writes++
	o := a.actual.Apply(ctx, i, p, g)
	if a.lost && o.Outcome == Confirmed {
		return Observation{Outcome: Unknown, Code: "local_response_unknown"}
	}
	return o
}
func (a *lostLocalAdapter) Reconcile(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	return a.actual.Reconcile(ctx, i, p, g)
}

func TestAuthoredObjectMatchesGit(t *testing.T) {
	for _, date := range []string{"2026-01-01T00:00:00Z", "2026-01-01T05:45:00+05:45", "2026-01-01T00:00:00-07:00", "2026-01-01T00:00:00-00:00"} {
		for _, message := range []string{"message", "message\n", "héllo\n\n"} {
			g, s := commitFixture(t)
			s.Timestamp = date
			s.Message = message
			s.Author.Name = "Réviewed Author"
			o, e := authoredObject(s)
			if e != nil {
				t.Fatal(e)
			}
			_, id, e := g.prepare(context.Background(), s)
			if e != nil || id != o.OID {
				t.Fatalf("canonical mismatch %q %q: %s %s %v", date, message, id, o.OID, e)
			}
		}
	}
	_, s := commitFixture(t)
	for _, kind := range []string{"name", "email", "fraction", "encoding"} {
		bad := s
		switch kind {
		case "name":
			bad.Author.Name = " Author "
		case "email":
			bad.Author.Email = "email. "
		case "fraction":
			bad.Timestamp = "2026-01-01T00:00:00.1Z"
		case "encoding":
			bad.Message = string([]byte{255})
		}
		if _, e := authoredObject(bad); e == nil {
			t.Fatal("unsupported metadata accepted", kind)
		}
	}
}
