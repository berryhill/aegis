package delivery

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixtureProtection() map[string]any {
	return map[string]any{"enforce_admins": map[string]bool{"enabled": true}, "required_status_checks": map[string]any{"strict": true, "contexts": []string{"test"}, "checks": []any{}}, "required_pull_request_reviews": map[string]any{"required_approving_review_count": 1, "dismiss_stale_reviews": true, "require_last_push_approval": true, "bypass_pull_request_allowances": map[string]any{"users": []any{}, "teams": []any{}, "apps": []any{}}}}
}
func TestV2StableProjectionAndPayloadConflict(t *testing.T) {
	for _, op := range []Operation{Publish, PullRequest} {
		p := fixturePlan()
		m := &memoryOwner{}
		a := &fixtureAdapter{lost: true}
		_, e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a).Execute(context.Background(), p.Reference, op)
		if e != nil {
			t.Fatal(e)
		}
		p.PostMergeChecks = []string{"later"}
		_, e = NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a).Execute(context.Background(), p.Reference, op)
		if e != nil || a.writes != 1 || len(m.records) != 1 {
			t.Fatalf("projection rotated: %v %d", e, a.writes)
		}
		if op == PullRequest {
			p.PRBody = "changed"
		} else {
			p.ExpectedOld = strings.Repeat("c", 40)
		}
		_, e = NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a).Execute(context.Background(), p.Reference, op)
		if !errors.Is(e, ErrConflict) || a.writes != 1 {
			t.Fatalf("payload resent %v", e)
		}
	}
}

// Synchronize launches rather than waiting for two GETs: the corrected engine
// legitimately emits only one writer, so a two-GET barrier would deadlock.
func TestV2ConcurrentProjectionOnePR(t *testing.T) {
	g, f, p := newGithubFixture(t)
	m := &memoryOwner{}
	start := make(chan struct{})
	var wg sync.WaitGroup
	p2 := p
	p2.PostMergeChecks = []string{"later"}
	for _, plan := range []Plan{p, p2} {
		wg.Go(func() {
			<-start
			_, _ = NewEngine(m, fixtureSource{plan}, &fixtureAuthority{}, g).Execute(context.Background(), plan.Reference, PullRequest)
		})
	}
	close(start)
	wg.Wait()
	if f.writes != 1 || len(m.records) != 1 {
		t.Fatalf("writes=%d records=%d", f.writes, len(m.records))
	}
}
func TestV2CheckedOutTargetAnyWorktree(t *testing.T) {
	for _, linked := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "linked"}[linked], func(t *testing.T) {
			g, s := commitFixture(t)
			args := []string{"switch", "-c", s.Branch}
			if linked {
				args = []string{"worktree", "add", "-b", s.Branch, filepath.Join(t.TempDir(), "linked"), s.Parent}
			}
			if _, e := g.run(context.Background(), nil, nil, args...); e != nil {
				t.Fatal(e)
			}
			before, _ := g.run(context.Background(), nil, nil, "rev-parse", "HEAD")
			o := g.Commit(context.Background(), s, func(context.Context) error { return nil })
			after, _ := g.run(context.Background(), nil, nil, "rev-parse", "HEAD")
			if o.Outcome != Denied || string(before) != string(after) {
				t.Fatal(o)
			}
		})
	}
}
func TestV2DocumentedClosingSyntax(t *testing.T) {
	// GitHub linking-a-pull-request-to-an-issue: all nine verbs, optional colon,
	// same-repo, cross-repo and full URL references. Conservative whitespace scan.
	for _, verb := range []string{"close", "closes", "closed", "fix", "fixes", "fixed", "resolve", "resolves", "resolved"} {
		for _, ref := range []string{"#378", "owner/repo#378", "https://github.com/owner/repo/issues/378"} {
			for _, sep := range []string{" ", ": ", ":\n", "\n"} {
				g, f, p := newGithubFixture(t)
				p.PRBody = strings.ToUpper(verb) + sep + ref
				i, _ := intentFor(p, PullRequest)
				if o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil }); o.Outcome != Denied || f.writes != 0 {
					t.Fatal(p.PRBody, o)
				}
			}
		}
	}
	g, f, p := newGithubFixture(t)
	p.Commit.Files = []File{{Bytes: []byte("Fixes: #378")}}
	i, _ := intentFor(p, Publish)
	if o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil }); o.Outcome != Denied || f.writes != 0 {
		t.Fatal(o)
	}
}

type v2Transport struct {
	base   http.RoundTripper
	before func(*http.Request)
}

func (v v2Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	v.before(r)
	return v.base.RoundTrip(r)
}
func TestV2StrictServerRejectsLastWindow(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	g.client.Transport = v2Transport{g.client.Transport, func(r *http.Request) {
		if r.Method == "PUT" {
			f.Lock()
			f.base = strings.Repeat("f", 40)
			f.Unlock()
		}
	}}
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Unknown || f.merged || f.writes != 1 {
		t.Fatalf("server did not reject actual last window: %+v merged=%v writes=%d", o, f.merged, f.writes)
	}
}
func TestV2FinalGuardAdvanceNoRequest(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	calls := 0
	o := g.Apply(context.Background(), i, p, func(context.Context) error {
		calls++
		if calls == 2 {
			f.Lock()
			f.base = strings.Repeat("f", 40)
			f.Unlock()
		}
		return nil
	})
	if f.writes != 0 || o.Outcome == Confirmed {
		t.Fatal(o)
	}
}
func TestV2MissingProtectionDenies(t *testing.T) {
	g, f, p := newGithubFixture(t)
	p.ProtectionPolicy = ""
	i, _ := intentFor(p, Merge)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Denied || f.writes != 0 {
		t.Fatal(o)
	}
}
