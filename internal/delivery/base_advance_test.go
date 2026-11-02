package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This is controlled HTTP evidence for the held premerge boundary, not a
// productive current-base integration or a protected/live remote campaign.
func TestGitHubCurrentBaseAdvanceHeldWithoutRebinding(t *testing.T) {
	for _, kind := range []string{"advance", "diverged", "behind", "missing", "mismatched-ref", "wrong-head", "wrong-repo", "merged"} {
		t.Run(kind, func(t *testing.T) {
			p := fixturePlan()
			p.PR = 7
			i, err := intentFor(p, CheckDelivery)
			if err != nil {
				t.Fatal(err)
			}
			original := i
			current := strings.Repeat("f", 40)
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					http.Error(w, "mutation forbidden", 405)
					return
				}
				send := func(v any) { _ = json.NewEncoder(w).Encode(v) }
				switch r.URL.Path {
				case "/user":
					send(map[string]any{"login": p.Actor, "id": 42})
				case "/repos/owner/repo":
					send(map[string]any{"id": p.RepositoryID, "full_name": p.Repository, "default_branch": p.Base, "permissions": map[string]bool{"push": true}})
				case "/repos/owner/repo/pulls/7":
					f := &githubFixture{actor: p.Actor, repo: p.Repository, head: p.Head}
					if kind == "wrong-head" {
						f.head = strings.Repeat("9", 40)
					}
					if kind == "wrong-repo" {
						f.repo = "other/repo"
					}
					if kind == "merged" {
						f.merged = true
					}
					live := p
					live.BaseHead = current
					send(f.pull(live))
				case "/repos/owner/repo/git/ref/heads/main":
					ref := current
					if kind == "mismatched-ref" {
						ref = p.BaseHead
					}
					send(map[string]any{"ref": "refs/heads/main", "object": map[string]string{"sha": ref}})
				case "/repos/owner/repo/git/ref/heads/contribution/run":
					send(map[string]any{"ref": "refs/heads/" + p.Branch, "object": map[string]string{"sha": p.Head}})
				case "/repos/owner/repo/compare/" + p.BaseHead + "..." + current:
					if kind == "missing" {
						http.Error(w, "missing", 404)
						return
					}
					status, behind := "ahead", 0
					if kind == "diverged" {
						status = "diverged"
					}
					if kind == "behind" {
						behind = 1
					}
					send(map[string]any{"status": status, "behind_by": behind})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.Error(w, "unexpected", 404)
				}
			}))
			defer srv.Close()
			g, err := newGitHub(srv.URL, fixtureCustody{}, fixturePrivacy{})
			if err != nil {
				t.Fatal(err)
			}
			o := g.Apply(context.Background(), i, p, func(context.Context) error { return nil })
			if o.Outcome != Unknown || writes != 0 || i != original || p.BaseHead != original.BaseHead {
				t.Fatalf("held boundary lost: %+v writes=%d", o, writes)
			}
			if (o.Code == "base_advanced") != (kind == "advance") {
				t.Fatalf("unexpected classification: %+v", o)
			}
			if _, err := ParseReadback(i, o); err != nil {
				t.Fatalf("readback: %v", err)
			}
			r := Record{Intent: i, Revision: 2, Outcome: o.Outcome, Observation: o}
			if repairableFailure(r) != (kind == "advance") {
				t.Fatal("only a positively verified read-only base prerequisite may be forwarded")
			}
			r.Code = o.Code
			got, advanceErr := VerifiedBaseAdvance(r)
			if (advanceErr == nil) != (kind == "advance") || (advanceErr == nil && got != current) {
				t.Fatalf("positive prerequisite mismatch: base=%s error=%v", got, advanceErr)
			}
			for _, op := range []Operation{Publish, Merge, ReconcileIssue} {
				mutation := r
				mutation.Intent.Operation = op
				if _, err := VerifiedBaseAdvance(mutation); err == nil {
					t.Fatal("unknown mutation acknowledged as base advance", op)
				}
			}
		})
	}
}

func TestPublicationParentPins(t *testing.T) {
	base, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct{ name, previous, repair, want string }{
		{"legacy", "", "", base},
		{"legacy-expected-old", old, "", base},
		{"repair", old, "sha256:" + strings.Repeat("c", 64), old},
		{"malformed-failure", old, "sha256:no", ""},
		{"missing-old", "", "sha256:" + strings.Repeat("c", 64), ""},
		{"malformed-old", "not-a-sha", "sha256:" + strings.Repeat("c", 64), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := publicationParent(base, tc.previous, tc.repair); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
