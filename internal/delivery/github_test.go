package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fixtureCustody struct{}

func (fixtureCustody) Use(ctx context.Context, i Intent, fn func([]byte) error) error {
	return fn([]byte("synthetic-controller-token"))
}

type fixturePrivacy struct{ deny bool }

func (p fixturePrivacy) Review(context.Context, Intent, Plan) error {
	if p.deny {
		return ErrDenied
	}
	return nil
}

type githubFixture struct {
	sync.Mutex
	title, body                                       string
	conflict                                          bool
	actor, repo, base, head, check, review, mergeTree string
	writes                                            int
	reads, initialPending, postPending, mergeWrites   int
	pr                                                bool
	merged                                            bool
	postFail                                          bool
	lost                                              bool
}

func newGithubFixture(t *testing.T, plans ...Plan) (*GitHub, *githubFixture, Plan) {
	t.Helper()
	p := fixturePlan()
	if len(plans) > 0 {
		p = plans[0]
	}
	p.PR = 7
	p.RequiredChecks = []string{"test"}
	p.PostMergeChecks = []string{"test"}
	p.RequiredReviewers = []string{"reviewer"}
	p.MergeMethod = "squash"
	if p.Commit.Tree == "" {
		p.Commit.Tree = strings.Repeat("d", 40)
	}
	p.PRTitle = "Scoped contribution"
	p.ProtectionPolicy = digest(fixtureProtection())
	p.PRBody = "Bounded accepted change"
	f := &githubFixture{actor: p.Actor, repo: p.Repository, base: p.BaseHead, head: p.Head, check: "success", review: "APPROVED", mergeTree: p.Commit.Tree, title: p.PRTitle, body: p.PRBody}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.Lock()
		defer f.Unlock()
		w.Header().Set("Content-Type", "application/json")
		send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
		if r.Header.Get("Authorization") != "Bearer synthetic-controller-token" {
			t.Error("credential not applied")
		}
		if r.Method != "GET" {
			f.writes++
		} else {
			f.reads++
		}
		switch r.URL.Path {
		case "/user":
			send(map[string]any{"login": f.actor, "id": 42})
		case "/repos/owner/repo":
			send(map[string]any{"id": 1, "full_name": f.repo, "default_branch": "main", "permissions": map[string]bool{"push": true}, "allow_squash_merge": true})
		case "/repos/owner/repo/rules/branches/main":
			send([]any{})
		case "/repos/owner/repo/branches/main/protection":
			send(fixtureProtection())
		case "/repos/owner/repo/collaborators/actor/permission":
			send(map[string]string{"permission": "write", "role_name": "write"})
		case "/repos/owner/repo/git/ref/heads/main":
			send(map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": f.base}})
		case "/repos/owner/repo/git/ref/heads/contribution/run":
			send(map[string]any{"ref": "refs/heads/contribution/run", "object": map[string]string{"sha": f.head}})
		case "/repos/owner/repo/pulls":
			if r.Method == "POST" {
				f.pr = true
			}
			if r.Method == "GET" {
				if f.pr {
					send([]any{f.pull(p)})
				} else {
					send([]any{})
				}
			} else {
				send(f.pull(p))
			}
		case "/repos/owner/repo/pulls/7":
			if r.Method == "PATCH" {
				var text struct{ Title, Body string }
				if json.NewDecoder(r.Body).Decode(&text) != nil {
					t.Error("bad update")
				}
				f.title = text.Title
				f.body = text.Body
			}
			send(f.pull(p))
		case "/repos/owner/repo/pulls/7/reviews":
			send([]any{map[string]any{"id": 1, "user": map[string]string{"login": "reviewer"}, "state": f.review, "commit_id": f.head, "submitted_at": "2026-01-01T00:00:00Z"}})
		case "/repos/owner/repo/pulls/7/merge":
			f.mergeWrites++
			if f.review != "APPROVED" {
				http.Error(w, "required independent current-head approval", 405)
				return
			}
			if f.base != p.BaseHead {
				http.Error(w, "strict: branch behind", 405)
				return
			}
			f.merged = true
			f.base = strings.Repeat("e", 40)
			if f.lost {
				h := w.(http.Hijacker)
				c, _, _ := h.Hijack()
				_ = c.Close()
				return
			}
			send(map[string]any{"merged": true, "sha": f.base})
		default:
			if strings.HasSuffix(r.URL.Path, "/check-runs") {
				conclusion := f.check
				if !f.merged && f.initialPending > 0 {
					f.initialPending--
					conclusion = ""
				}
				if f.merged && strings.Contains(r.URL.Path, strings.Repeat("e", 40)) && f.postPending > 0 {
					f.postPending--
					conclusion = ""
				}
				if f.merged && f.postFail {
					conclusion = "failure"
				}
				send(map[string]any{"total_count": 1, "check_runs": []any{map[string]any{"id": 123, "app": map[string]any{"id": 55, "slug": "ci"}, "name": "test", "head_sha": strings.Split(r.URL.Path, "/")[5], "status": "completed", "conclusion": conclusion}}})
			} else if strings.HasSuffix(r.URL.Path, "/status") {
				send(map[string]any{"total_count": 0, "statuses": []any{}})
			} else if strings.Contains(r.URL.Path, "/git/commits/") {
				send(map[string]any{"sha": strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/git/commits/"), "tree": map[string]string{"sha": f.mergeTree}, "parents": []any{map[string]string{"sha": p.BaseHead}}})
			} else if strings.Contains(r.URL.Path, "/compare/") {
				send(map[string]any{"status": "identical", "behind_by": 0})
			} else {
				http.Error(w, fmt.Sprint("unsupported fixture path ", r.URL.Path), 404)
			}
		}
	}))
	t.Cleanup(srv.Close)
	g, e := newGitHub(srv.URL, fixtureCustody{}, fixturePrivacy{})
	if e != nil {
		t.Fatal(e)
	}
	return g, f, p
}
func (f *githubFixture) pull(p Plan) map[string]any {
	state := "open"
	merge := ""
	if f.merged {
		state = "closed"
		merge = strings.Repeat("e", 40) // merge identity is independent of later base advancement
	}
	mergeable, stateCode := true, "clean"
	if f.conflict {
		mergeable = false
		stateCode = "dirty"
	}
	return map[string]any{"html_url": "https://github.com/owner/repo/pull/7", "user": map[string]string{"login": f.actor}, "number": 7, "state": state, "draft": false, "merged": f.merged, "merge_commit_sha": merge, "mergeable": mergeable, "mergeable_state": stateCode, "body": f.body, "title": f.title, "head": map[string]any{"sha": f.head, "ref": p.Branch, "repo": map[string]any{"id": 1, "full_name": f.repo}}, "base": map[string]any{"sha": p.BaseHead, "ref": p.Base, "repo": map[string]any{"id": 1, "full_name": f.repo}}}
}
func TestGitHubMergeExactHeadAndReadback(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Confirmed || o.Merge == "" || f.writes != 1 {
		t.Fatalf("%+v writes %d", o, f.writes)
	}
}
func TestGitHubWrongTargetsAndMissingGatesDenyBeforeWrite(t *testing.T) {
	for _, kind := range []string{"actor", "repo", "head", "base", "check", "skipped", "review", "missing", "privacy", "closing", "auth"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			guard := func(context.Context) error { return nil }
			switch kind {
			case "actor":
				f.actor = "other"
			case "repo":
				f.repo = "other/repo"
			case "head":
				f.head = strings.Repeat("f", 40)
			case "base":
				f.base = strings.Repeat("f", 40)
			case "check":
				f.check = "failure"
			case "skipped":
				f.check = "skipped"
			case "review":
				f.review = "CHANGES_REQUESTED"
			case "missing":
				p.RequiredChecks = []string{"absent"}
			case "privacy":
				g.privacy = fixturePrivacy{true}
			case "closing":
				p.PRBody = "Fixes #378"
			case "auth":
				guard = func(context.Context) error { return ErrDenied }
			}
			i, _ := intentFor(p, Merge)
			o := g.Apply(context.Background(), i, p, guard)
			if o.Outcome == Confirmed || f.writes != 0 {
				t.Fatalf("%s %+v writes %d", kind, o, f.writes)
			}
		})
	}
}
func TestGitHubLostMergeResponseReconcilesNoResend(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.lost = true
	i, _ := intentFor(p, Merge)
	guard := func(context.Context) error { return nil }
	o := g.Apply(context.Background(), i, p, guard)
	if o.Outcome != Unknown {
		t.Fatal(o)
	}
	o = g.Reconcile(context.Background(), i, p, guard)
	if o.Outcome != Confirmed || f.writes != 1 {
		t.Fatalf("%+v writes %d", o, f.writes)
	}
}
func TestGitHubPostMergeFailureIsMergedUnresolved(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.postFail = true
	i, _ := intentFor(p, Merge)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Unknown || o.Code != "merged_validation_unresolved" || o.Merge == "" {
		t.Fatal(o)
	}
}
func TestGitHubIssueClosureNeedsTrustedCompletion(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, ReconcileIssue)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Denied || f.writes != 0 {
		t.Fatal(o)
	}
}
func TestGitHubRejectsUnsafeDestination(t *testing.T) {
	for _, s := range []string{"http://github.com", "https://evil.invalid", "https://api.github.com/path", "https://user:pass@api.github.com"} {
		if _, e := NewGitHub(s, fixtureCustody{}, fixturePrivacy{}); e == nil {
			t.Fatal(s)
		}
	}
}
