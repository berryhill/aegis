package delivery

import (
	"bytes"
	"context"
	"github.com/berryhill/aegis/internal/implementation"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestProjectAcceptedDeterministicDeletedFiles(t *testing.T) {
	ctx := context.Background()
	g, parent := fixtureGit(t)
	env := []string{"GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid"}
	for _, p := range []string{"z-delete", "a-delete", "m-delete"} {
		if err := os.WriteFile(filepath.Join(g.Directory, p), []byte(p), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := g.run(ctx, nil, nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.run(ctx, env, nil, "commit", "-m", "deletion baseline"); err != nil {
		t.Fatal(err)
	}
	b, _ := g.run(ctx, nil, nil, "rev-parse", "HEAD")
	parent = strings.TrimSpace(string(b))
	files := []File{{Path: "source", Mode: "100644", Bytes: []byte("semantic after")}}
	for _, p := range []string{"z-delete", "a-delete", "m-delete"} {
		files = append(files, File{Path: p, Mode: "000000"})
		if err := os.Remove(filepath.Join(g.Directory, p)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.Directory, "source"), files[0].Bytes, 0644); err != nil {
		t.Fatal(err)
	}
	spec := CommitSpec{Parent: parent, Branch: "contribution/run", Author: Identity{"Fixture", "fixture@example.invalid"}, Committer: Identity{"Fixture", "fixture@example.invalid"}, Timestamp: "2026-01-01T00:00:00Z", Message: "freeze", Files: files}
	tree, freeze, err := g.prepare(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	c := implementation.ContributionCandidate{CandidateCommit: freeze, CandidateTree: tree, Base: parent, HeadCommit: parent, Source: implementation.SourceInventory{Schema: "aegis.contribution.source.v1", Files: []implementation.SourceFile{{Path: "source", Mode: 0644, Content: files[0].Bytes}}}}
	spec.Tree = tree
	spec.Message = "reviewed publication"
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
		return append(out, b...)
	}
	before := snapshot()
	var first CommitSpec
	var intent Intent
	for n := 0; n < 20; n++ {
		got, _, e := g.ProjectAccepted(ctx, c, spec, []string{"source", "z-delete", "a-delete", "m-delete"})
		if e != nil {
			t.Fatal(e)
		}
		if !slices.IsSortedFunc(got.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) }) {
			t.Fatal("unsorted delta")
		}
		p := fixturePlan()
		p.Commit = got
		p.Head = got.Commit
		i, e := intentFor(p, Commit)
		if e != nil {
			t.Fatal(e)
		}
		if n == 0 {
			first = got
			intent = i
		} else if !reflect.DeepEqual(first, got) || intent != i {
			t.Fatal("same semantic projection changed payload/OID")
		}
		if !bytes.Equal(before, snapshot()) {
			t.Fatal("pure projection changed Git state")
		}
	}
	p := fixturePlan()
	p.Commit = first
	p.Head = first.Commit
	p.Commit.Message += " changed"
	different, e := intentFor(p, Commit)
	if e != nil || different.ID != intent.ID || different.Payload == intent.Payload {
		t.Fatal("semantic bytes did not conflict")
	}
	o := g.Commit(ctx, first, func(context.Context) error { return nil })
	if o.Outcome != Confirmed || o.Head != first.Commit {
		t.Fatal("authorized Git differs from pure OID", o)
	}
}
