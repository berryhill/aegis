package command

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/berryhill/aegis/internal/registry"
)

// Synthetic transport peer only. Real enrolled-password handler/storage proof
// lives in api.TestAgentModelAddNativeNoDraftExactPasswordJourney.
func TestManagerModelAddProtectedClientExactJourney(t *testing.T) {
	for _, mode := range []string{"approve", "reject", "cancelled", "wrong-lane", "extra-edit", "lost-decision", "wrong-readback"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.API.UnixSocket = filepath.Join(socketTestDir(t), "owner.sock")
			in := app.AgentModelAddInput{AgentID: "silas", Provider: "openai-codex", Model: "gpt-6.2-sol"}
			c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "silas", Name: "Silas fixture", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: "operator", CreatedAt: time.Now().UTC(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{"operator"}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: 60}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Provider: "codex", Model: "default-model", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}}}}
			original, err := core.Canonicalize(c)
			if err != nil {
				t.Fatal(err)
			}
			// Detach before changing the successor: Canonicalize can share typed slices.
			c = core.Charter{}
			if err = json.Unmarshal(original.Canonical, &c); err != nil {
				t.Fatal(err)
			}
			c.Revision = 2
			c.Stanzas[0].Hermes.AllowedLanes = append(c.Stanzas[0].Hermes.EffectiveLanes(), core.HermesLane{Provider: in.Provider, Model: in.Model, ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}})
			proposed, err := core.Canonicalize(c)
			if err != nil {
				t.Fatal(err)
			}
			review := app.AgentModelAddReview{Input: in, Expected: app.RevisionReference("silas", 1, "sha256:"+strings.Repeat("a", 64)), Original: original, Proposed: proposed, ExpiresAt: time.Now().Add(time.Minute), Receipt: "private-review"}
			wire, _ := json.Marshal(review)
			var decoded app.AgentModelAddReview
			if !strictCompanionJSON(wire, &decoded) {
				t.Fatalf("review wire rejected: %s", wire)
			}
			result := app.FleetAgent{Revision: registry.AgentRevision{AgentID: "silas", Revision: 2, Charter: app.RevisionReference("silas", 2, proposed.Digest), CharterSuccessor: &registry.CharterSuccessor{Previous: review.Expected}}}
			listener, err := net.Listen("unix", cfg.API.UnixSocket)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(cfg.API.UnixSocket, 0600); err != nil {
				t.Fatal(err)
			}
			decisions, logouts := 0, 0
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != cfg.API.Console.Origin {
					t.Error("wrong authentication transport")
				}
				switch filepath.Base(r.URL.Path) {
				case "login":
					http.SetCookie(w, &http.Cookie{Name: console.CookieName, Value: "private-session"})
					json.NewEncoder(w).Encode(map[string]string{"csrf": "private-csrf", "config_identity": core.Digest(cfg)})
				case "model-review":
					if mode == "extra-edit" {
						c.Name = "Unrequested rename"
						review.Proposed, err = core.Canonicalize(c)
						if err != nil {
							t.Error(err)
						}
					}
					if mode == "wrong-lane" {
						review.Input.Model = "different"
					}
					json.NewEncoder(w).Encode(review)
				case "model-decision":
					decisions++
					var d app.DoerProtectedSetupDecision
					if json.NewDecoder(r.Body).Decode(&d) != nil || d.Receipt != "private-review" || r.Header.Get("X-CSRF-Token") != "private-csrf" {
						t.Error("exact review custody lost")
					}
					if mode == "lost-decision" {
						c, _, _ := w.(http.Hijacker).Hijack()
						c.Close()
						return
					}
					if mode == "reject" {
						json.NewEncoder(w).Encode(map[string]string{"status": "rejected"})
						return
					}
					if mode == "wrong-readback" {
						result.Revision.Charter.Digest = original.Digest
					}
					json.NewEncoder(w).Encode(app.AgentModelAddResult{Agent: result})
				case "logout":
					logouts++
					json.NewEncoder(w).Encode(map[string]string{"status": "logged_out"})
				default:
					w.WriteHeader(404)
				}
			})}
			go server.Serve(listener)
			defer server.Close()
			secret := []byte("fixture-only-password")
			emitted := false
			confirmed := false
			err = executeManagerModelAdd(t.Context(), cfg, in, func(context.Context) ([]byte, error) { return secret, nil }, func(context.Context, app.AgentModelAddReview) string {
				confirmed = true
				if mode == "cancelled" || mode == "reject" {
					return mode
				}
				return "approve"
			}, func(app.FleetAgent) error { emitted = true; return nil })
			if mode == "approve" {
				if err != nil || !emitted || !confirmed {
					t.Fatal("approved journey", err)
				}
			} else if err == nil || emitted {
				t.Fatal("invalid result emitted", err)
			}
			if (mode == "wrong-lane" || mode == "extra-edit") && confirmed {
				t.Fatal("wrong lane presented for approval")
			}
			if mode == "lost-decision" && !strings.Contains(err.Error(), "outcome_unknown") {
				t.Fatal("uncertain decision misclassified", err)
			}
			if decisions > 1 || ((mode == "cancelled" || mode == "wrong-lane") && decisions != 0) || logouts != 1 {
				t.Fatal("decision replay or session leak", decisions, logouts)
			}
			if !bytes.Equal(secret, make([]byte, len(secret))) {
				t.Fatal("password retained")
			}
		})
	}
}
