package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestV16CanonicalPullURL(t *testing.T) {
	for _, raw := range []string{"https://github.com/owner/repo/pull/7?x=1", "https://user@github.com/owner/repo/pull/7", "https://evil.invalid/owner/repo/pull/7", "https://github.com/owner/repo/pull/07", "https://github.com/owner/repo/pull/7#x"} {
		if canonicalPullURL(raw, "owner/repo", 7) {
			t.Fatal("accepted noncanonical URL")
		}
	}
	g, _, p := newGithubFixture(t)
	i, _ := intentFor(p, CheckDelivery)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	proof, e := ParseReadback(i, o)
	if e != nil || o.Outcome != Confirmed {
		t.Fatal(e, o.Code)
	}
	found := false
	for _, f := range proof.Facts {
		if f.Kind == "pull" {
			found = f.HTMLURL == "https://github.com/owner/repo/pull/7" && f.User.Login == i.Actor
		}
	}
	if !found {
		t.Fatal("missing owned canonical pull")
	}
}

func TestV16ActualHTTPPublicationMetadata(t *testing.T) {
	p := objectPlan(t)
	f := &objectServer{}
	g := serveObjects(t, p, f)
	owner := &memoryOwner{}
	engine := NewEngine(owner, fixtureSource{p}, &fixtureAuthority{}, g)
	r, e := engine.Execute(context.Background(), p.Reference, Publish)
	if e != nil || r.Outcome != Confirmed {
		t.Fatal(e, r.Code)
	}
	p.Object = p.Objects[len(p.Objects)-1]
	i, _ := intentFor(p, UploadCommit)
	o := g.Reconcile(context.Background(), i, p, func(context.Context) error { return nil })
	proof, e := ParseReadback(i, o)
	if e != nil || o.Outcome != Confirmed {
		t.Fatal(e, o.Code)
	}
	expected := publicationFor(p.Object)
	found := false
	for _, fact := range proof.Facts {
		if fact.Kind == "commit" && fact.SHA == i.Head {
			found = fact.Author != nil && fact.Committer != nil && samePerson(*fact.Author, expected.Author) && samePerson(*fact.Committer, expected.Committer) && fact.MessageDigest == expected.MessageDigest && fact.Tree.SHA == p.Commit.Tree && fact.Parents[0].SHA == p.BaseHead
		}
	}
	if !found {
		t.Fatal("remote authored commit metadata missing")
	}
	raw, _ := commitPayload(p.Object)
	if strings.Contains(o.ProofJSON, raw.Message) {
		t.Fatal("raw commit message retained")
	}
	proof.Requirements.Publication.Tree = strings.Repeat("f", 40)
	b, _ := json.Marshal(proof)
	o.ProofJSON = string(b)
	o.ProofDigest = digest(proof)
	if _, e = ParseReadback(i, o); e == nil {
		t.Fatal("publication requirement tamper accepted")
	}
}

func TestV16LatestReviewsActualHTTP(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(map[bool]string{false: "superseded-denial", true: "latest-denial"}[deny], func(t *testing.T) {
			p := fixturePlan()
			p.PR = 7
			latest := "APPROVED"
			if deny {
				latest = "CHANGES_REQUESTED"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/pulls/7/reviews" {
					t.Error("wrong target")
				}
				_ = json.NewEncoder(w).Encode([]any{
					map[string]any{"id": 4, "user": map[string]string{"login": "second"}, "state": "APPROVED", "commit_id": p.Head},
					map[string]any{"id": 1, "user": map[string]string{"login": "reviewer"}, "state": "CHANGES_REQUESTED", "commit_id": p.Head},
					map[string]any{"id": 3, "user": map[string]string{"login": "reviewer"}, "state": latest, "commit_id": p.Head},
				})
			}))
			defer srv.Close()
			g, e := newGitHub(srv.URL, fixtureCustody{}, fixturePrivacy{})
			if e != nil {
				t.Fatal(e)
			}
			e = g.reviews(context.Background(), []byte("synthetic"), Intent{Repository: p.Repository, PR: 7, Head: p.Head, Actor: p.Actor}, []string{"reviewer", "second"})
			if (e != nil) != deny {
				t.Fatal("latest reviewer decision incorrect", e)
			}
		})
	}
}
