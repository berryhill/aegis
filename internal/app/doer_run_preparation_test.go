package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestDoerEphemeralPreparationPolicy(t *testing.T) {
	testDoerEphemeralPreparationPolicy(t, false)
}

func TestReusableDoerExactRunPreparationPolicy(t *testing.T) {
	testDoerEphemeralPreparationPolicy(t, true)
}

func testDoerEphemeralPreparationPolicy(t *testing.T, reusable bool) {
	for _, which := range []string{"allowed", "capability", "memory", "tools", "credentials", "foreign", "workspace-actor", "expired", "stale-auth", "disabled", "persistent", "profile", "persistent-home", "mcp", "plugins", "substituted-agent", "substituted-loop"} {
		t.Run(which, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			subject, err := s.Authenticate(ctx)
			if err != nil {
				t.Fatal(err)
			}
			s.Config.Credentials.Authority.DeploymentID = "policy-fixture"
			c := testCharter(s.Now())
			c.Stanzas = c.Stanzas[:1]
			st := &c.Stanzas[0]
			st.Grant = core.Grant{}
			st.Scopes = core.Scopes{}
			st.Hermes = core.HermesConfig{Model: "proof-no-key", Provider: "none"}
			st.Approval.RequiredOperations = []string{"provision"}
			switch which {
			case "profile":
				st.Hermes.Profile = "retained"
			case "persistent-home":
				st.Hermes.PersistentHome = true
			case "mcp":
				st.Hermes.MCPServers = []string{"ambient"}
			case "plugins":
				st.Hermes.Plugins = []string{"ambient"}
			case "capability":
				st.Grant.Capabilities = []string{"chat"}
			case "memory":
				st.Scopes.Memory = []string{"retained-memory"}
			case "tools":
				st.Grant.Tools = []string{"no_mcp"}
				st.Hermes.Toolsets = []string{"no_mcp"}
			case "credentials":
				st.Scopes.Credentials = []string{"provider:test"}
				st.Hermes.Provider = "test"
			}
			raw, _ := json.Marshal(c)
			canonical, err := s.ImportCharterAs(ctx, subject, raw)
			if err != nil {
				if which == "profile" || which == "persistent-home" || which == "mcp" || which == "plugins" {
					return
				}
				t.Fatal(err)
			}
			agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: c.AgentID, Revision: 1, Source: registry.FleetSource{FleetID: "fixture", Kind: "test", SourceID: "source"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: subject.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 1, Digest: canonical.Digest}})
			if err != nil {
				t.Fatal(err)
			}
			switch which {
			case "foreign":
				agent.Ownership.OwnerID = "foreign"
			case "disabled":
				agent.Lifecycle = registry.LifecycleDisabled
			case "persistent":
				agent.Runtime.Target = "persistent-profile"
			}
			agent, _ = registry.SealRevision(agent)
			repo := &hostApprovalRepository{initial: agent, latest: agent, registration: registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: agentRevisionRef(agent)}}
			s.FleetRepository = repo
			text := "hello"
			contract := loop.DoerContract{Task: "Write result", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}
			candidate, _, err := loop.NewDoerRevision("policy-doer", 1, "", contract)
			if err != nil {
				t.Fatal(err)
			}
			if reusable {
				contract.ExactBytes = false
				candidate, _, err = loop.NewDoerReusableRevision("policy-doer", 1, "", loop.DoerReusableContract{MaxAttempts: contract.MaxAttempts})
				if err != nil {
					t.Fatal(err)
				}
				input := QueueLoopInput{Agent: agentRevisionRef(agent), Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}}
				for _, port := range []struct {
					id    string
					kind  graph.ValueType
					value any
				}{{"task", graph.TypeString, contract.Task}, {"workspace", graph.TypeString, contract.Workspace}, {"writable_files", graph.TypeArray, contract.WritableFiles}, {"verify_file", graph.TypeString, contract.VerifyFile}, {"expected_text", graph.TypeString, text}} {
					raw, err := json.Marshal(port.value)
					if err != nil {
						t.Fatal(err)
					}
					input.Inputs = append(input.Inputs, graph.NormalizedInput{PortID: port.id, Type: port.kind, Value: raw})
				}
				ctx, err = bindDoerRun(ctx, input, candidate)
				if err != nil {
					t.Fatal(err)
				}
			}
			switch which {
			case "workspace-actor":
				subject.Kind = "agent"
				subject.Issuer = "workspace"
			case "expired":
				subject.ExpiresAt = s.Now().Add(-time.Second)
			case "stale-auth":
				subject.AuthenticatedAt = s.Now().Add(-2 * s.Config.Principal.AuthTTL)
			case "substituted-agent":
				agent.Digest = "sha256:bad"
			case "substituted-loop":
				if reusable {
					candidate.DoerReusable.MaxAttempts++
				} else {
					candidate.Doer.Task = "changed without digest"
				}
			}
			allowed := s.doerEphemeralPreparationAllowed(ctx, subject, candidate, agent)
			if allowed != (which == "allowed") {
				t.Fatalf("policy allowed=%v", allowed)
			}
			if which != "allowed" {
				runCtx := context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: "denied-run", candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}})
				_ = s.prepareDoerRunHostScope(runCtx, subject, candidate, agent)
				if _, err := s.Store.ReadDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, agent.Ownership.OwnerID, agentRevisionRef(agent), contract, s.Now()); err == nil {
					t.Fatal("ineligible exact Run minted approval")
				}
			}
			// Even an eligible exact Run never opens the generic session entry point.
			if which == "allowed" {
				_, decision, err := s.PreviewSessionAs(ctx, subject, c.AgentID, 1, "", core.Environment{Name: "local"})
				if err == nil || decision.Reason != "provisioning_receipt_missing" {
					t.Fatalf("generic session bypass: %+v %v", decision, err)
				}
				if err = s.prepareDoerRunHostScope(ctx, subject, candidate, agent); err != nil {
					t.Fatal(err)
				}
				if s.resolveDoerHostApproval(ctx, contract, agent) == nil {
					t.Fatal("readiness without exact Run marker granted writes")
				}
				runCtx := context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: "policy-exact-run", candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}})
				wrongCandidate := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "other-loop", Revision: candidate.Revision, Digest: candidate.Digest}
				if err := s.prepareDoerRunHostScope(context.WithValue(ctx, exactDoerRunKey{}, exactDoerRun{requestID: "policy-exact-run", candidate: wrongCandidate}), subject, candidate, agent); err == nil {
					t.Fatal("caller candidate substitution accepted")
				}
				if err = s.prepareDoerRunHostScope(runCtx, subject, candidate, agent); err != nil {
					t.Fatal(err)
				}
				if err := s.resolveDoerHostApproval(runCtx, contract, agent); err != nil {
					t.Fatalf("exact Run scope denied: %v", err)
				}
				for _, replay := range []exactDoerRun{{requestID: "different-run", candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}}, {requestID: "policy-exact-run", candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "other-loop", Revision: candidate.Revision, Digest: candidate.Digest}}} {
					if s.resolveDoerHostApproval(context.WithValue(ctx, exactDoerRunKey{}, replay), contract, agent) == nil {
						t.Fatal("same-contract Run/Loop replay granted")
					}
				}
				grant, err := s.ReadDoerHostWriteApprovalAs(ctx, subject, agentRevisionRef(agent), contract)
				if err != nil || grant.RunRequestID != "policy-exact-run" || grant.ExpiresAt.After(subject.ExpiresAt) {
					t.Fatalf("grant provenance: %+v %v", grant, err)
				}
				if grant.Candidate != (reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}) || !reflect.DeepEqual(grant.Contract, contract) || !reflect.DeepEqual(grant.DoerReusable, candidate.DoerReusable) || grant.ExpiresAt.After(subject.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
					t.Fatalf("incorrect signed exact source: %+v", grant)
				}
				for _, changed := range []loop.DoerContract{func() loop.DoerContract { c := contract; c.Task = "changed task"; return c }(), func() loop.DoerContract { c := contract; other := "other assertion"; c.ExpectedText = &other; return c }()} {
					if s.resolveDoerHostApproval(runCtx, changed, agent) == nil {
						t.Fatal("changed task/assertion covered")
					}
				}
				now := s.Now
				s.Now = func() time.Time { return grant.ExpiresAt }
				if s.resolveDoerHostApproval(runCtx, contract, agent) == nil {
					t.Fatal("expired signed approval covered")
				}
				s.Now = now
				changed := contract
				changed.WritableFiles = []string{"other.txt"}
				changed.VerifyFile = "other.txt"
				if s.resolveDoerHostApproval(runCtx, changed, agent) == nil {
					t.Fatal("changed write contract granted")
				}
				if _, err = os.Stat(filepath.Join(contract.Workspace, "result.txt")); !os.IsNotExist(err) {
					t.Fatal("preparation wrote result")
				}
				for _, bucket := range []string{"plans", "approvals", "receipts", "provisioned"} {
					if err = s.Store.List(bucket, func(json.RawMessage) error { t.Errorf("fabricated %s", bucket); return nil }); err != nil && !os.IsNotExist(err) {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
