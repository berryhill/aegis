package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type v5Transport struct {
	base       http.RoundTripper
	policy     map[string]any
	lateReview bool
	fixture    *githubFixture
	writes     int
}

func (v *v5Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != "GET" {
		v.writes++
	}
	if strings.HasSuffix(r.URL.Path, "/protection") && v.policy != nil {
		b, _ := json.Marshal(v.policy)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}, nil
	}
	if v.lateReview && strings.HasSuffix(r.URL.Path, "/merge") {
		v.fixture.Lock()
		v.fixture.review = "DISMISSED"
		v.fixture.Unlock()
	}
	if r.URL.Path == "/user" && r.Header.Get("Authorization") == "Bearer rotated-synthetic-token" {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"login":"wrong-actor"}`))}, nil
	}
	return v.base.RoundTrip(r)
}
func TestV5StrictReviewPolicy(t *testing.T) {
	for _, kind := range []string{"missing", "count", "insufficient-count", "stale", "last-push"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			pol := fixtureProtection()
			rv := pol["required_pull_request_reviews"].(map[string]any)
			switch kind {
			case "missing":
				delete(pol, "required_pull_request_reviews")
			case "count":
				rv["required_approving_review_count"] = 0
			case "insufficient-count":
				p.RequiredReviewers = []string{"reviewer", "second-independent-reviewer"}
			case "stale":
				rv["dismiss_stale_reviews"] = false
			case "last-push":
				rv["require_last_push_approval"] = false
			}
			p.ProtectionPolicy = digest(pol)
			g.client.Transport = &v5Transport{base: g.client.Transport, policy: pol}
			i, _ := intentFor(p, Merge)
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome != Denied || f.writes != 0 {
				t.Fatal(o, f.writes)
			}
		})
	}
}
func TestV5ReviewDismissedAtFinalGuard(t *testing.T) {
	g, f, p := newGithubFixture(t)
	i, _ := intentFor(p, Merge)
	calls := 0
	o := g.Apply(context.Background(), i, p, func(context.Context) error {
		calls++
		if calls >= 4 {
			f.Lock()
			f.review = "DISMISSED"
			f.Unlock()
		}
		return nil
	})
	if calls < 4 || f.writes != 0 || o.Outcome == Confirmed {
		t.Fatal(o, calls, f.writes)
	}
}
func TestV5ServerReviewLastWindowDenial(t *testing.T) {
	g, f, p := newGithubFixture(t)
	tr := &v5Transport{base: g.client.Transport, lateReview: true, fixture: f}
	g.client.Transport = tr
	i, _ := intentFor(p, Merge)
	o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
	if o.Outcome != Unknown || tr.writes != 1 || f.merged {
		t.Fatal(o, tr.writes, f.merged)
	}
}

type v5RotatingCustody struct{ calls, rotate int }

func (c *v5RotatingCustody) Use(_ context.Context, _ Intent, fn func([]byte) error) error {
	c.calls++
	if c.calls >= c.rotate {
		return fn([]byte("rotated-synthetic-token"))
	}
	return fn([]byte("synthetic-controller-token"))
}
func TestV5ActorUsesWriteCredential(t *testing.T) {
	for _, object := range []bool{false, true} {
		t.Run(map[bool]string{false: "general", true: "object"}[object], func(t *testing.T) {
			var g *GitHub
			var p Plan
			op := PullRequest
			rotate := 5
			if object {
				p = objectPlan(t)
				g = serveObjects(t, p, &objectServer{})
				p.Object = p.Objects[0]
				op = UploadBlob
				rotate = 4
			} else {
				g, _, p = newGithubFixture(t)
			}
			tr := &v5Transport{base: g.client.Transport}
			g.client.Transport = tr
			custody := &v5RotatingCustody{rotate: rotate}
			g.custody = custody
			i, _ := intentFor(p, op)
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if custody.calls != rotate || tr.writes != 0 || o.Outcome == Confirmed {
				t.Fatal(o, tr.writes)
			}
		})
	}
}
func TestV5FencePinsSemanticNotSlot(t *testing.T) {
	p := fixturePlan()
	a, _ := intentFor(p, Publish)
	p.FenceEpoch++
	p.RoleBinding = digest("synthetic-replacement-not-real-authority")
	b, _ := intentFor(p, Publish)
	if a.ID != b.ID || a.Payload == b.Payload || a == b {
		t.Fatal(a, b)
	}
	p.FenceEpoch = 0
	if _, e := intentFor(p, Publish); e == nil {
		t.Fatal("zero epoch accepted")
	}
}
