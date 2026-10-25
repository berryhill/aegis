package evidence

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/store"
)

func mergeObjectFixture(t *testing.T) (*BlobVerifier, ContributionMergeObjects, func(string, []string, string) ContributionGitObject) {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	v, _ := NewBlobVerifier(s)
	makeObject := func(tree string, parents []string, message string) ContributionGitObject {
		t.Helper()
		body := "tree " + tree + "\n"
		for _, p := range parents {
			body += "parent " + p + "\n"
		}
		body += "author Fixture <fixture@example.invalid> 1 +0000\ncommitter Fixture <fixture@example.invalid> 1 +0000\n\n" + message + "\n"
		h := sha1.Sum([]byte(fmt.Sprintf("commit %d\x00%s", len(body), body)))
		ref, err := s.PutBlob([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		return ContributionGitObject{hex.EncodeToString(h[:]), ref}
	}
	base := strings.Repeat("a", 40)
	tree := strings.Repeat("b", 40)
	candidate := makeObject(tree, []string{base}, "synthetic freeze")
	published := makeObject(tree, []string{base}, "reviewed publication")
	merged := makeObject(tree, []string{base}, "squash merge")
	later := makeObject(strings.Repeat("c", 40), []string{merged.OID}, "later independent contribution")
	p := ContributionMergeObjects{Candidate: candidate, Published: published, Merged: merged, TestedBase: base, CheckedBase: base, TestedTree: tree, FinalHead: published.OID, PRHead: published.OID, ProtectedBaseHead: later.OID, Method: "squash", Containment: []ContributionGitObject{later, merged}}
	return v, p, makeObject
}
func TestContributionSquashMergeProvesTreeNotSHAEquality(t *testing.T) {
	v, p, _ := mergeObjectFixture(t)
	if p.Candidate.OID == p.Published.OID || p.Published.OID == p.Merged.OID {
		t.Fatal("fixture requires distinct commit identities")
	}
	if err := v.VerifyContributionMergeObjects(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}
func TestContributionMergeObjectRelationshipNegatives(t *testing.T) {
	v, p, makeObject := mergeObjectFixture(t)
	for _, name := range []string{"candidate-digest", "published-digest", "wrong-tested-tree", "wrong-merge-tree", "wrong-squash-parent", "stale-checked-base", "stale-pr-head", "wrong-final-head", "missing-containment", "wrong-current-base", "unrelated-base", "duplicate-containment", "wrong-policy", "corrupt-blob", "wrong-freeze-parent"} {
		t.Run(name, func(t *testing.T) {
			q := p
			q.Containment = append([]ContributionGitObject{}, p.Containment...)
			switch name {
			case "candidate-digest":
				q.Candidate.OID = strings.Repeat("d", 40)
			case "published-digest":
				q.Published.OID = strings.Repeat("d", 40)
			case "wrong-tested-tree":
				q.TestedTree = strings.Repeat("d", 40)
			case "wrong-merge-tree":
				q.Merged = makeObject(strings.Repeat("d", 40), []string{q.CheckedBase}, "wrong tree")
				q.Containment = []ContributionGitObject{q.Merged}
				q.ProtectedBaseHead = q.Merged.OID
			case "wrong-squash-parent":
				q.Merged = makeObject(q.TestedTree, []string{strings.Repeat("d", 40)}, "wrong parent")
				q.Containment = []ContributionGitObject{q.Merged}
				q.ProtectedBaseHead = q.Merged.OID
			case "stale-checked-base":
				q.CheckedBase = strings.Repeat("d", 40)
			case "stale-pr-head":
				q.PRHead = q.Candidate.OID
			case "wrong-final-head":
				q.FinalHead = q.Candidate.OID
			case "missing-containment":
				q.Containment = nil
			case "wrong-current-base":
				q.ProtectedBaseHead = q.Merged.OID
			case "unrelated-base":
				other := makeObject(q.TestedTree, []string{q.CheckedBase}, "unrelated")
				q.Containment = []ContributionGitObject{other, q.Merged}
				q.ProtectedBaseHead = other.OID
			case "duplicate-containment":
				q.Containment = append(q.Containment, q.Merged)
			case "wrong-policy":
				q.Method = "force"
			case "corrupt-blob":
				q.Candidate.Content = sha256Reference([]byte("absent"))
			case "wrong-freeze-parent":
				q.Candidate = makeObject(q.TestedTree, []string{strings.Repeat("d", 40)}, "wrong frozen base")
			}
			if err := v.VerifyContributionMergeObjects(context.Background(), q); err == nil {
				t.Fatal("invalid merge relationship accepted")
			}
		})
	}
}
func TestContributionMergeAndFastForwardMethods(t *testing.T) {
	v, p, makeObject := mergeObjectFixture(t)
	p.Method = "merge"
	p.Merged = makeObject(p.TestedTree, []string{p.CheckedBase, p.FinalHead}, "real merge relation")
	p.Containment = []ContributionGitObject{p.Merged}
	p.ProtectedBaseHead = p.Merged.OID
	if err := v.VerifyContributionMergeObjects(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	p.Method = "fast-forward"
	p.Merged = p.Published
	p.Containment = []ContributionGitObject{p.Merged}
	p.ProtectedBaseHead = p.Merged.OID
	if err := v.VerifyContributionMergeObjects(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := v.VerifyContributionMergeObjects(ctx, p); err == nil {
		t.Fatal("cancelled verification accepted")
	}
}
