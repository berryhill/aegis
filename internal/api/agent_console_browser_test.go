package api

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Synthetic isolated preparation. The child exits and closes both stores before
// the actual frozen candidate serves; no retained configuration or grants copied.
func TestAgentConsoleFrozenSeed(t *testing.T) {
	root := os.Getenv("AEGIS_AGENT_ROOT")
	if root == "" {
		t.Skip("private frozen-browser seed")
	}
	if os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("shared lifetime port custody required")
	}
	svc := apiServiceAt(t, root)
	fleetStore := configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"office", "missing", "disabled", "retired", "history"} {
		cc := agentObservationCharter(t, svc, subject, id)
		fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "agent-console-fixture", Agents: []registry.CurrentFleetAgent{{SourceID: id, AgentID: id, Runtime: registry.RuntimeBinding{Adapter: cc.Charter.Runtime.Adapter, Runtime: cc.Charter.Runtime.Runtime, Target: cc.Charter.Runtime.Target}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "synthetic-fixture"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: cc.Charter.Revision, Digest: cc.Digest}}}})
		if err != nil {
			t.Fatal(err)
		}
		agent, _, err := svc.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(fixture, "agent-console-fixture", id))
		if err != nil {
			t.Fatal(err)
		}
		if id == "office" {
			provisionAgentObservation(t, svc, subject, cc)
			storeAgentExecutionHistory(t, svc, fleetStore, agent.Revision)
			session := core.Session{ID: "session-office-recorded", Status: "ended", Mandate: core.Mandate{ID: "mandate-office-history", AgentID: id, CharterRevision: cc.Charter.Revision, CharterDigest: cc.Digest, Target: cc.Charter.Runtime.Target, Runtime: core.RuntimeDescriptor{Runtime: cc.Charter.Runtime.Runtime}, StanzaID: "principal"}}
			if err = svc.Store.Save("sessions", session.ID, session); err != nil {
				t.Fatal(err)
			}
		}
		if id == "disabled" || id == "retired" || id == "history" {
			state := registry.LifecycleDisabled
			if id == "retired" {
				state = registry.LifecycleRetired
			}
			agent, err = svc.SetAgentLifecycleAs(ctx, subject, id, app.SetAgentLifecycleInput{Expected: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}, Lifecycle: state})
			if err != nil {
				t.Fatal(err)
			}
			if id == "history" {
				_, err = svc.SetAgentLifecycleAs(ctx, subject, id, app.SetAgentLifecycleInput{Expected: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}, Lifecycle: registry.LifecycleEnabled})
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	svc.Config.API.Listen = "127.0.0.1:" + os.Getenv("AEGIS_ACCEPTANCE_PORT")
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repository, ".scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.UnixSocket = filepath.Join(repository, ".scratch", ".agent-proof-"+os.Getenv("AEGIS_ACCEPTANCE_PORT")+".sock")
	// The fixture's provider environment is intentionally absent; no runtime runs.
	svc.Config.Credentials.ProviderAuth = nil
	raw, err := json.Marshal(svc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentConsoleFrozenBrowser(t *testing.T) {
	if os.Getenv("AEGIS_AGENT_CANDIDATE") == "" {
		t.Skip("requires built candidate and claimed port")
	}
	root := os.Getenv("AEGIS_AGENT_ROOT")
	if root == "" || os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("isolated root and lifetime port claim required")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fixture root must be new, never reuse canonical state")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	seed := exec.Command(os.Args[0], "-test.run=^TestAgentConsoleFrozenSeed$", "-test.v")
	seed.Env = os.Environ()
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("isolated preparation: %v %s", err, out)
	}
	browser := exec.Command("python3", "../../scripts/agent_console_browser_test.py")
	browser.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	if out, err := browser.CombinedOutput(); err != nil {
		t.Fatalf("browser acceptance: %v %s", err, out)
	} else {
		t.Log(string(out))
	}
}
