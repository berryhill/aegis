package command

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestDoerProtectedCLIRejectsPipedHumanInput(t *testing.T) {
	var out, diag bytes.Buffer
	cmd := NewRoot(Dependencies{Out: &out, Err: &diag})
	cmd.SetIn(strings.NewReader("synthetic-password\nAPPROVE\n"))
	cmd.SetArgs([]string{"--target", "http://127.0.0.1", "loops", "setup-approve", "example", "--expected-version", "1", "--action", "host"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "terminal-backed") || strings.Contains(out.String()+diag.String(), "synthetic-password") {
		t.Fatal("piped approval accepted or exposed", err)
	}
}

// All passwords, keys and decisions below belong to an inert private socket
// fixture. This never authenticates or approves an installed owning service.
func TestDoerProtectedCLIPrivateSessionAndExactReadback(t *testing.T) {
	for _, mode := range []string{"approved", "cancelled", "reject", "drift", "lost-decision", "wrong-instance", "wrong-password"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults()
			cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
			cfg.Credentials.Authority.DeploymentID = "synthetic-deployment"
			cfg.StateDir = filepath.Join(root, "must-not-exist")
			contract := loop.DoerContract{Task: "Create result.txt", Workspace: root, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1}
			candidate, _, err := loop.NewDoerRevision("synthetic-loop", 1, "", contract)
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := contract.Digest()
			agent := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "synthetic-agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
			draft := app.DoerDraft{ID: "doerdraft-" + strings.Repeat("b", 32), Version: 1, PrincipalID: cfg.Principal.ID, Agent: agent, LoopID: candidate.LoopID, Revision: 1, Contract: contract, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
			review := app.DoerProtectedSetupReview{Action: "host", Draft: draft, Receipt: "synthetic-review-secret", Host: &app.DoerHostApprovalInput{DraftID: draft.ID, DraftVersion: 1, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest}}
			_, key, _ := ed25519.GenerateKey(nil)
			grant, err := hostapproval.Sign(hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: cfg.Credentials.Authority.DeploymentID, PrincipalID: draft.PrincipalID, OwnerID: draft.PrincipalID, Agent: agent, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}, Contract: contract, ContractDigest: digest, ApprovedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, key)
			if err != nil {
				t.Fatal(err)
			}
			result := app.DoerProtectedSetupResult{Status: "approved", Draft: draft, Host: &grant}
			if mode == "drift" {
				result.Host.Agent.Revision++
			}
			listener, err := net.Listen("unix", cfg.API.UnixSocket)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(cfg.API.UnixSocket, 0600); err != nil {
				t.Fatal(err)
			}
			if !companionSocketSafe(cfg.API.UnixSocket) {
				info, _ := os.Lstat(cfg.API.UnixSocket)
				parent, _ := os.Lstat(root)
				t.Fatalf("unsafe fixture socket: socket=%v parent=%v", info.Mode(), parent.Mode())
			}
			decisions, logouts := 0, 0
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Origin") != cfg.API.Console.Origin || r.Header.Get("Authorization") != "" {
					t.Error("transport identity leaked/substituted")
				}
				switch filepath.Base(r.URL.Path) {
				case "login":
					password, _ := io.ReadAll(r.Body)
					if string(password) != "synthetic-password" {
						t.Error("wrong password input")
					}
					if mode == "wrong-password" {
						w.WriteHeader(401)
						return
					}
					http.SetCookie(w, &http.Cookie{Name: console.CookieName, Value: "synthetic-session-secret"})
					identity := core.Digest(cfg)
					if mode == "wrong-instance" {
						identity = "different"
					}
					json.NewEncoder(w).Encode(map[string]string{"csrf": "synthetic-csrf-secret", "config_identity": identity})
				case "review":
					json.NewEncoder(w).Encode(review)
				case "decision":
					decisions++
					if r.Header.Get("X-CSRF-Token") != "synthetic-csrf-secret" {
						t.Error("missing private CSRF")
					}
					var body app.DoerProtectedSetupDecision
					json.NewDecoder(r.Body).Decode(&body)
					if body.Receipt != review.Receipt {
						t.Error("receipt drift")
					}
					if mode == "lost-decision" {
						c, _, _ := w.(http.Hijacker).Hijack()
						c.Close()
						return
					}
					if mode == "reject" {
						result.Status = "rejected"
						result.Host = nil
					}
					json.NewEncoder(w).Encode(result)
				case "logout":
					logouts++
					json.NewEncoder(w).Encode(map[string]string{"status": "logged_out"})
				default:
					w.WriteHeader(404)
				}
			})}
			go server.Serve(listener)
			defer server.Close()
			secret := []byte("synthetic-password")
			var out bytes.Buffer
			err = executeDoerProtectedSetup(context.Background(), cfg, app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: 1, Action: "host"}, func(context.Context) ([]byte, error) { return secret, nil }, func(_ context.Context, r app.DoerProtectedSetupReview) string {
				if strings.Contains(doerProtectedReviewDescription(r), r.Receipt) {
					t.Error("private handle displayed")
				}
				if mode == "cancelled" {
					return "cancelled"
				}
				if mode == "reject" {
					return "reject"
				}
				return "approve"
			}, func(r app.DoerProtectedSetupResult) error { return json.NewEncoder(&out).Encode(r) })
			if mode == "approved" && err != nil {
				t.Fatal(err)
			}
			if mode != "approved" && err == nil {
				t.Fatal("invalid outcome accepted")
			}
			if mode == "lost-decision" && !strings.Contains(err.Error(), "outcome_unknown") {
				t.Fatal("lost mutation not distinguished", err)
			}
			if decisions > 1 || (mode == "cancelled" && decisions != 0) {
				t.Fatal("decision retried or cancellation approved")
			}
			if mode != "wrong-password" && logouts != 1 {
				t.Fatal("session not revoked", logouts)
			}
			if !bytes.Equal(secret, make([]byte, len(secret))) {
				t.Fatal("password not wiped")
			}
			for _, s := range []string{"synthetic-password", "synthetic-session-secret", "synthetic-csrf-secret", "synthetic-review-secret"} {
				if strings.Contains(out.String(), s) {
					t.Fatal("private material exposed")
				}
			}
			if mode != "approved" && out.Len() != 0 {
				t.Fatal("unverified outcome emitted")
			}
			if _, e := os.Stat(cfg.StateDir); !os.IsNotExist(e) {
				t.Fatal("alternate state opened")
			}
		})
	}
}

func TestDoerProtectedPasswordRequiresIndependentDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	password, err := acquireDoerSetupPassword(context.Background())
	if len(password) != 0 || !IsPassphraseError(err, PassphraseUnavailable) {
		t.Fatal("desktop absence fell back to terminal")
	}
}

func TestDoerProtectedResultRejectsMissingActionProof(t *testing.T) {
	cfg := config.Defaults()
	d := app.DoerDraft{ID: "example", Version: 1}
	for _, action := range []string{"successor", "host", "provision"} {
		if verifyDoerProtectedResult(cfg, app.DoerProtectedSetupReview{Action: action, Draft: d}, app.DoerProtectedSetupResult{Status: "approved", Draft: d}) {
			t.Fatal("status promoted", action)
		}
	}
	if protectedSetupInputStatus(context.DeadlineExceeded) != "timeout" || protectedSetupInputStatus(context.Canceled) != "cancelled" || protectedSetupInputStatus(errors.New("private detail")) != "unavailable" {
		t.Fatal("unsafe input status")
	}
}
