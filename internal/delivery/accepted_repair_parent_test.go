package delivery

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/implementation"
)

func TestProjectAcceptedRepairExtractsOriginalNativeBase(t *testing.T) {
	ctx := context.Background()
	g, freeze := commitFixture(t)
	c := implementation.ContributionCandidate{CandidateCommit: freeze.Commit, CandidateTree: freeze.Tree, Base: freeze.Parent, HeadCommit: freeze.Parent, Source: implementation.SourceInventory{Schema: "aegis.contribution.source.v1", Files: []implementation.SourceFile{{Path: "source", Mode: 0755, Content: []byte("after")}}}}
	// Fixture-only previous publication object, distinct from the native freeze.
	predecessor := freeze
	predecessor.Commit = ""
	predecessor.Message = "Previous owned publication\n"
	_, previous, err := g.prepare(ctx, predecessor)
	if err != nil {
		t.Fatal(err)
	}
	if previous == freeze.Commit || previous == freeze.Parent {
		t.Fatal("fixture did not separate parents")
	}
	successor := freeze
	successor.Commit = ""
	successor.Parent = previous
	successor.Message = "Repaired publication\n"
	got, objects, err := g.ProjectAccepted(ctx, c, successor, []string{"source"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != previous || c.Base != freeze.Parent || len(objects) == 0 {
		t.Fatal("original/native/publication parent binding lost")
	}
	publication, err := commitPayload(objects[len(objects)-1])
	if err != nil || len(publication.Parents) != 1 || publication.Parents[0] != previous {
		t.Fatalf("publication parent: %+v %v", publication, err)
	}
	// The native commit is not a child of the publication predecessor. Extracting
	// against that parent (the old regression) must deny rather than rebind it.
	if _, err := g.ExtractObjects(ctx, freeze.Commit, freeze.Tree, previous); err == nil {
		t.Fatal("native freeze accepted with substituted original base")
	}
}
