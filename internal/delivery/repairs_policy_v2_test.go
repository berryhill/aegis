package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type policyTransport struct {
	base   http.RoundTripper
	kind   string
	policy map[string]any
}

func (v policyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var out any
	if strings.HasSuffix(r.URL.Path, "/protection") {
		out = v.policy
	}
	if strings.HasSuffix(r.URL.Path, "/permission") && v.kind == "admin" {
		out = map[string]string{"permission": "admin", "role_name": "admin"}
	}
	if strings.Contains(r.URL.Path, "/rules/branches/") && v.kind == "rules" {
		out = []any{map[string]string{"type": "required_status_checks"}}
	}
	if out == nil {
		return v.base.RoundTrip(r)
	}
	b, _ := json.Marshal(out)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b)))}, nil
}
func TestV2ProtectionNegativeEnvelope(t *testing.T) {
	for _, kind := range []string{"loose", "admins-off", "missing-checks", "extra-check", "bypass", "admin", "rules", "drift", "app-pin"} {
		t.Run(kind, func(t *testing.T) {
			g, f, p := newGithubFixture(t)
			pol := fixtureProtection()
			checks := pol["required_status_checks"].(map[string]any)
			switch kind {
			case "loose":
				checks["strict"] = false
			case "admins-off":
				pol["enforce_admins"] = map[string]bool{"enabled": false}
			case "missing-checks":
				delete(pol, "required_status_checks")
			case "extra-check":
				checks["contexts"] = []string{"test", "other"}
			case "bypass":
				pol["required_pull_request_reviews"] = map[string]any{"bypass_pull_request_allowances": map[string]any{"users": []any{map[string]string{"login": p.Actor}}}}
			case "app-pin":
				checks["checks"] = []any{map[string]any{"context": "test", "app_id": 123}}
			}
			if kind != "drift" {
				p.ProtectionPolicy = digest(pol)
			} else {
				pol["allow_deletions"] = map[string]bool{"enabled": true}
			}
			g.client.Transport = policyTransport{g.client.Transport, kind, pol}
			i, _ := intentFor(p, Merge)
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome != Denied || f.writes != 0 {
				t.Fatalf("%+v writes=%d", o, f.writes)
			}
		})
	}
}
func TestV2OwnedPRUpdateAndUnknownNoSecondPR(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.pr = true
	p.PRBody = "Repaired bounded scope"
	m := &memoryOwner{}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, g)
	r, err := e.Execute(context.Background(), p.Reference, UpdatePullRequest)
	if err != nil || r.Outcome != Confirmed || r.PR != 7 || f.writes != 1 || !f.pr || f.body != p.PRBody {
		t.Fatalf("%+v %v writes=%d", r, err, f.writes)
	}
	_, err = e.Execute(context.Background(), p.Reference, UpdatePullRequest)
	if err != nil || f.writes != 1 {
		t.Fatal(err)
	}
	// Changed creation payload cannot open a second PR, even before an update.
	_, err = NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, g).Execute(context.Background(), p.Reference, PullRequest)
	if err != nil || f.writes != 1 {
		t.Fatal(err)
	}
}
func TestV2ConflictForwardedToExistingRepairOwner(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.conflict = true
	m := &memoryOwner{}
	r, e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, g).Execute(context.Background(), p.Reference, Merge)
	if e != nil || r.Outcome != Failed || r.Code != "merge_conflict" || f.writes != 0 {
		t.Fatalf("%+v %v", r, e)
	}
	owner := &fixtureRepair{}
	if ForwardRepair(context.Background(), owner, r) != nil || owner.calls != 1 || owner.failure.Class != "merge_conflict" {
		t.Fatal("repair not forwarded")
	}
}
