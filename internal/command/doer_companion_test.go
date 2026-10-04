package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func companionReviewFixture(t *testing.T) app.DoerContinuationReview {
	t.Helper()
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	contract := loop.DoerContract{Task: "Write a result", Workspace: "/workspace/project", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}
	candidate, _, e := loop.NewDoerRevision("draft-loop", 1, "", contract)
	if e != nil {
		t.Fatal(e)
	}
	loopRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}
	expiry := time.Now().Add(time.Minute)
	draft := app.DoerDraft{ID: "doerdraft_abc", Version: 1, Agent: ref, LoopID: candidate.LoopID, Revision: 1, Contract: contract, PublicationKey: "publication-key", ExpiresAt: expiry}
	intent := app.DoerContinuationIntent{DraftID: draft.ID, DraftVersion: 1, Agent: ref, Loop: loopRef, Charter: ref, CandidateDigest: candidate.Digest, PublicationKey: draft.PublicationKey, Run: app.QueueLoopInput{Agent: ref, Loop: loopRef, IdempotencyKey: "exact-run-key", Activate: true}, ExpiresAt: expiry, ConfigIdentity: "config-identity", SessionID: "original-session", DeploymentID: "deployment"}
	intent.Requester = core.Subject{ID: "password:" + strings.Repeat("b", 64), Kind: "principal", PrincipalID: "principal", Method: "password", Issuer: "aegis-principal-auth", AuthenticatedAt: expiry.Add(-time.Minute), ExpiresAt: expiry}
	r := app.DoerContinuationReview{Intent: intent, Draft: draft, Origin: "http://localhost:8080"}
	r.Digest = r.ContentDigest()
	return r
}

func TestCompanionUnixExactDecision(t *testing.T) {
	for _, scenario := range []string{"approve", "cancelled", "denied", "no_decision", "timeout", "unavailable", "digest-drift", "origin-drift", "config-drift", "draft-drift", "redirect", "unknown", "sensitive", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			review := companionReviewFixture(t)
			switch scenario {
			case "digest-drift":
				review.Digest = "wrong"
			case "origin-drift":
				review.Origin = "http://foreign"
				review.Digest = review.ContentDigest()
			case "config-drift":
				review.Intent.ConfigIdentity = "different"
				review.Digest = review.ContentDigest()
			case "draft-drift":
				review.Draft.Version++
				review.Digest = review.ContentDigest()
			case "sensitive":
				review.Draft.Contract.Task = "password=secret"
				candidate, _, _ := loop.NewDoerRevision(review.Draft.LoopID, 1, "", review.Draft.Contract)
				review.Intent.CandidateDigest = candidate.Digest
				review.Intent.Loop.Digest = candidate.Digest
				review.Intent.Run.Loop = review.Intent.Loop
				review.Digest = review.ContentDigest()
			}
			dir := t.TempDir()
			if e := os.Chmod(dir, 0700); e != nil {
				t.Fatal(e)
			}
			socket := filepath.Join(dir, "owner.sock")
			listener, e := net.Listen("unix", socket)
			if e != nil {
				t.Fatal(e)
			}
			if e = os.Chmod(socket, 0600); e != nil {
				t.Fatal(e)
			}
			if !companionSocketSafe(socket) {
				info, _ := os.Lstat(socket)
				resolved, _ := filepath.EvalSymlinks(socket)
				parent, _ := os.Lstat(filepath.Dir(socket))
				t.Fatalf("unsafe fixture %s resolved=%s mode=%v sys=%v parent=%v uid=%d", socket, resolved, info.Mode(), info.Sys(), parent.Mode(), os.Geteuid())
			}
			var decisions, confirmations atomic.Int32
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "localhost:8080" || r.Header.Get("Origin") != reviewOrigin() || r.Header.Get("Authorization") != "Bearer private-canary" {
					t.Error("request authority mismatch")
				}
				if r.Method == "GET" {
					if scenario == "redirect" {
						w.Header().Set("Location", "http://foreign")
						w.WriteHeader(302)
						return
					}
					if scenario == "oversize" {
						io.WriteString(w, strings.Repeat("x", companionJSONLimit+1))
						return
					}
					_ = json.NewEncoder(w).Encode(review)
					return
				}
				decisions.Add(1)
				if r.Method != "POST" || r.URL.Path != "/v1/doer-continuations/pending/intent_abc/decision" {
					t.Error("unexpected operation")
				}
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if len(body) != 2 || body["decision"] != "approve" || body["expected_digest"] != review.Digest {
					t.Error("decision not exact")
				}
				status := "approved"
				if scenario == "unknown" {
					status = "invented"
				}
				_ = json.NewEncoder(w).Encode(doerCompanionDecision{IntentID: "intent_abc", Digest: review.Digest, Status: status})
			})}
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			o := companionOptions{socket: socket, origin: reviewOrigin(), intentID: "intent_abc", fd: 3}
			got := runDoerCompanion(context.Background(), o, "config-identity", []byte("private-canary"), func(_ context.Context, data string) string {
				confirmations.Add(1)
				for _, value := range []string{"UNTRUSTED DATA", "Write a result", "/workspace/project", "exact-run-key", "result.txt", review.Digest} {
					if !strings.Contains(data, value) {
						t.Error("review incomplete")
					}
				}
				if scenario == "approve" || scenario == "unknown" {
					return "confirmed"
				}
				return scenario
			})
			want := scenario
			switch scenario {
			case "approve":
				want = "approved"
			case "unknown", "redirect":
				want = "unknown_response"
			case "digest-drift", "origin-drift", "config-drift", "draft-drift":
				want = "response_drift"
			case "sensitive":
				want = "sensitive_body_rejected"
			case "oversize":
				want = "malformed_protocol"
			}
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
			if scenario != "approve" && scenario != "unknown" && decisions.Load() != 0 {
				t.Fatal("decision sent without explicit confirmation")
			}
			if strings.HasSuffix(scenario, "drift") && confirmations.Load() != 0 {
				t.Fatal("drift shown to helper")
			}
		})
	}
}
func reviewOrigin() string { return "http://localhost:8080" }
func TestCompanionStrictJSONAndPolicy(t *testing.T) {
	for _, input := range []string{`{"status":"approved","status":"denied"}`, `{"status":"approved","Status":"denied"}`, `{"status":"approved","credential":"private"}`, `null`, `{} {}`, `{"status":null}`} {
		var out doerCompanionDecision
		if strictCompanionJSON([]byte(input), &out) {
			t.Errorf("accepted %s", input)
		}
	}
	if companionSocketSafe("/tmp/nonexistent-companion.sock") {
		t.Fatal("missing socket allowed")
	}
	for _, body := range []string{"Bearer canary", "-----BEGIN PRIVATE KEY-----", "api_token", "github_pat_canary", "password"} {
		if !sensitiveCompanionBody([]byte(body), nil) {
			t.Fatal("sensitive body allowed")
		}
	}
}

func TestCompanionFDCustodyFixture(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "--fd-fixture" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	token, e := readCompanionBearer(ctx, 3)
	if e != nil {
		fmt.Println("rejected")
	} else {
		fmt.Println("accepted")
		wipeSecret(token)
	}
	os.Exit(0)
}
func TestCompanionFDCustody(t *testing.T) {
	for _, tc := range []struct{ body, want string }{{"private-canary", "accepted"}, {"", "rejected"}, {"canary\nCONFIRM", "rejected"}, {strings.Repeat("x", 4097), "rejected"}} {
		read, write, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		_, e = write.Write([]byte(tc.body))
		_ = write.Close()
		if e != nil {
			t.Fatal(e)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestCompanionFDCustodyFixture$", "--", "--fd-fixture")
		child.ExtraFiles = []*os.File{read}
		out, e := child.CombinedOutput()
		_ = read.Close()
		if e != nil {
			t.Fatal(e)
		}
		if strings.TrimSpace(string(out)) != tc.want || bytes.Contains(out, []byte("private-canary")) {
			t.Fatalf("invalid custody metadata %q", out)
		}
	}
	if _, e := readCompanionBearer(context.Background(), 4); e == nil {
		t.Fatal("arbitrary descriptor accepted")
	}
}
func TestCompanionRootIsHiddenAndStoreFree(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Dependencies{Out: &out, Err: &out})
	c, _, e := root.Find([]string{"doer-approval-companion"})
	if e != nil || !c.Hidden {
		t.Fatal("not hidden")
	}
	root.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml"), "doer-approval-companion", "--socket", "/missing", "--origin", reviewOrigin(), "--intent-id", "intent_abc"})
	if e = root.Execute(); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "policy_rejected") {
		t.Fatal(out.String())
	}
	_ = core.Digest // fixture digest uses production canonicalization through ContentDigest.
}
