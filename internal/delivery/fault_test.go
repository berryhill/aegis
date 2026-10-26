package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPRCreateAndLostResponseReconciliation(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, PullRequest)
	guard := func(context.Context) error { return nil }
	o := g.Apply(context.Background(), i, p, guard)
	if o.Outcome != Confirmed || o.PR != 7 || f.writes != 1 {
		t.Fatalf("%+v writes=%d", o, f.writes)
	}
	o = g.Reconcile(context.Background(), i, p, guard)
	if o.Outcome != Confirmed || f.writes != 1 {
		t.Fatal(o)
	}
}
func TestFreshAdmissionImmediatelyBeforeMutation(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	calls := 0
	o := g.Apply(context.Background(), i, p, func(context.Context) error {
		calls++
		if calls > 1 {
			return ErrDenied
		}
		return nil
	})
	if o.Outcome != Denied || f.writes != 0 || calls != 2 {
		t.Fatalf("%+v calls=%d writes=%d", o, calls, f.writes)
	}
}
func TestRemoteRefLostResponseNoBlindResend(t *testing.T) {
	p := objectPlan(t)
	i, _ := intentFor(p, Publish)
	var published atomic.Bool
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		for _, o := range p.Objects {
			if r.URL.Path == "/repos/owner/repo/git/"+o.Kind+"s/"+o.OID {
				switch o.Kind {
				case "blob":
					send(map[string]any{"sha": o.OID, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(o.Raw)})
				case "tree":
					entries, _ := treeEntries(o.Raw)
					send(map[string]any{"sha": o.OID, "tree": entries})
				case "commit":
					cp, _ := commitPayload(o)
					send(map[string]any{"sha": o.OID, "message": cp.Message, "tree": map[string]string{"sha": cp.Tree}, "parents": []any{map[string]string{"sha": cp.Parents[0]}}, "author": cp.Author, "committer": cp.Committer})
				}
				return
			}
		}
		switch r.URL.Path {
		case "/user":
			send(map[string]any{"login": p.Actor, "id": 42})
		case "/repos/owner/repo":
			send(map[string]any{"id": 1, "full_name": p.Repository, "default_branch": p.Base, "permissions": map[string]bool{"push": true}})
		case "/repos/owner/repo/git/ref/heads/contribution/run":
			if !published.Load() {
				w.WriteHeader(404)
				return
			}
			send(map[string]any{"ref": "refs/heads/" + p.Branch, "object": map[string]string{"sha": p.Head}})
		case "/repos/owner/repo/git/commits/" + p.Head:
			send(map[string]string{"sha": p.Head})
		case "/repos/owner/repo/git/refs":
			writes.Add(1)
			published.Store(true)
			c, _, _ := w.(http.Hijacker).Hijack()
			_ = c.Close()
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	g, e := newGitHub(srv.URL, fixtureCustody{}, fixturePrivacy{})
	if e != nil {
		t.Fatal(e)
	}
	guard := func(context.Context) error { return nil }
	o := g.Apply(context.Background(), i, p, guard)
	if o.Outcome != Unknown || writes.Load() != 1 {
		t.Fatal(o)
	}
	o = g.Reconcile(context.Background(), i, p, guard)
	if o.Outcome != Confirmed || writes.Load() != 1 {
		t.Fatal(o)
	}
}
func TestAdapterRejectsTamperedIntentAndPRTarget(t *testing.T) {
	for _, kind := range []string{"intent", "ref", "base", "pr", "stale_review"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			i, _ := intentFor(p, Merge)
			switch kind {
			case "intent":
				i.Actor = "other"
			case "ref":
				p.Branch = "other/ref"
			case "base":
				p.Base = "other"
			case "pr":
				p.PR = 8
			case "stale_review":
				p.Head = strings.Repeat("f", 40)
			}
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome != Denied || f.writes != 0 {
				t.Fatal(o)
			}
		})
	}
}
func TestPublicationCanaryDenialIsSanitized(t *testing.T) {
	g, f, p := newGithubFixture(t)
	p.PRBody = "private-canary-value"
	g.privacy = fixturePrivacy{true}
	i, _ := intentFor(p, PullRequest)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	b, _ := json.Marshal(o)
	if o.Outcome != Denied || f.writes != 0 || strings.Contains(string(b), "private-canary-value") {
		t.Fatal("publication leak or missing denial")
	}
}
func TestMissingPostmergeCoverageAndWrongTreeUnresolved(t *testing.T) {
	for _, kind := range []string{"missing", "tree"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			if kind == "missing" {
				p.PostMergeChecks = nil
			} else {
				f.mergeTree = strings.Repeat("f", 40)
			}
			i, _ := intentFor(p, Merge)
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome != Unknown || o.Merge == "" {
				t.Fatal(o)
			}
		})
	}
}
