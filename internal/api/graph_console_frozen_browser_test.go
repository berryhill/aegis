package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Fixture-tier durable definitions only: no provider, execution, grant or
// production identity. Child closes stores before the frozen CLI starts.
func TestGraphConsoleFrozenSeed(t *testing.T) {
	root := os.Getenv("AEGIS_GRAPH_ROOT")
	if root == "" {
		t.Skip("private frozen Graph seed")
	}
	if os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("lifetime port claim required")
	}
	svc := apiServiceAt(t, root)
	repo := configureAPIFleet(t, svc)
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	check(err)
	cc := agentObservationCharter(t, svc, subject, "graph-fixture-agent")
	raw, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "graph-console-fixture", Agents: []registry.CurrentFleetAgent{{SourceID: "graph-fixture-agent", AgentID: "graph-fixture-agent", Runtime: registry.RuntimeBinding{Adapter: cc.Charter.Runtime.Adapter, Runtime: cc.Charter.Runtime.Runtime, Target: cc.Charter.Runtime.Target}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "synthetic-read-only-fixture"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: cc.Charter.AgentID, Revision: cc.Charter.Revision, Digest: cc.Digest}}}})
	check(err)
	agent, _, err := svc.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(raw, "graph-console-fixture", "graph-fixture-agent"))
	check(err)
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	check(err)
	lr, lv, err := app.NewLoopRevision(form.Revision)
	check(err)
	provenance, err := loop.NewPublicationProvenance(loop.PublicationProvenance{Loop: loop.NewProvenanceRevision(lr.LoopID, lr.Revision, lr.Digest), PublisherAgent: loop.NewProvenanceRevision(agent.Revision.AgentID, agent.Revision.Revision, agent.Revision.Digest), Authority: loop.NewProvenanceDigest("fixture-authority", "sha256:"+strings.Repeat("d", 64)), MandateID: "fixture-not-live", StanzaID: "principal", Runtime: loop.ProvenanceRuntime{Runtime: agent.Revision.Runtime.Runtime}, Charter: loop.NewProvenanceRevision(cc.Charter.AgentID, cc.Charter.Revision, cc.Digest), ValidationDigest: lv.Digest})
	check(err)
	fact := func(id string) fleet.AuditFact {
		return fleet.AuditFact{Event: core.AuditEvent{Type: "definition.fixture", SubjectID: id, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "synthetic read-only browser fixture"}}
	}
	_, err = repo.PublishLoop(ctx, loop.PublishRequest{Revision: lr, Validation: lv, Provenance: provenance, IdempotencyKey: "graph-fixture-loop"}, fact(lr.LoopID))
	check(err)
	ar := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.Revision.AgentID, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}
	lp := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: lr.Revision, Digest: lr.Digest}
	manifest := map[string]any{"tier": "synthetic durable read-only definitions; no live execution or authority", "agent": ar, "loop": lp}
	records := []graph.GraphRevision{}
	for _, shape := range []struct {
		id           string
		count        int
		branch       bool
		disconnected bool
	}{{"graph-proof", 6, true, false}, {"graph-chain-12", 12, false, false}, {"graph-chain-128", 128, false, false}, {"graph-disconnected-128", 128, false, true}, {"graph-missing-pins", 2, false, false}} {
		g := graph.GraphRevision{GraphID: shape.id, Revision: 1, Inputs: []graph.Port{{ID: "seed", Type: graph.TypeString}}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeString}}}
		for i := 0; i < shape.count; i++ {
			g.Nodes = append(g.Nodes, graph.Node{ID: fmt.Sprintf("n%03d", i), Participant: ar, Loop: lp, Inputs: []graph.Port{{ID: "seed", Type: graph.TypeString}}, Outputs: []graph.Port{{ID: "value", Type: graph.TypeString}}})
		}
		if shape.id == "graph-missing-pins" {
			for i := range g.Nodes {
				g.Nodes[i].Participant.ID = "missing-agent-fixture"
				g.Nodes[i].Loop.ID = "missing-loop-fixture"
			}
		}
		pairs := [][2]int{}
		if shape.branch {
			pairs = [][2]int{{0, 1}, {1, 2}, {1, 3}, {2, 4}, {3, 4}, {4, 5}}
		} else if !shape.disconnected {
			for i := 1; i < shape.count; i++ {
				pairs = append(pairs, [2]int{i - 1, i})
			}
		}
		for _, pair := range pairs {
			from, to := pair[0], pair[1]
			port := fmt.Sprintf("from%03d", from)
			g.Nodes[to].Inputs = append(g.Nodes[to].Inputs, graph.Port{ID: port, Type: graph.TypeString})
			g.Dependencies = append(g.Dependencies, graph.Dependency{ID: fmt.Sprintf("edge%03d-%03d", from, to), FromNodeID: g.Nodes[from].ID, ToNodeID: g.Nodes[to].ID, Mappings: []graph.PortMapping{{FromPort: "value", ToPort: port}}})
		}
		g.InputMappings = []graph.InputMapping{{GraphInput: "seed", ToNodeID: g.Nodes[0].ID, ToPort: "seed"}}
		g.OutputMappings = []graph.OutputMapping{{FromNodeID: g.Nodes[len(g.Nodes)-1].ID, FromPort: "value", GraphOutput: "result"}}
		revisions := 1
		if shape.branch {
			revisions = 2
		}
		previous := ""
		for rev := 1; rev <= revisions; rev++ {
			g.Revision = uint64(rev)
			g.PreviousDigest = previous
			g.Digest = ""
			exact, validation, err := graph.NewRevision(g)
			check(err)
			_, err = repo.PublishGraph(ctx, graph.PublishRequest{Revision: exact, Validation: validation, ExpectedPreviousDigest: previous, IdempotencyKey: fmt.Sprintf("%s-%d", shape.id, rev)}, fact(shape.id))
			check(err)
			records = append(records, exact)
			previous = exact.Digest
		}
	}
	manifest["graphs"] = records
	// Canonical publication must refuse invalid component fixtures; never inject
	// malformed records into the production store merely to draw a browser case.
	for _, kind := range []string{"duplicate", "malformed", "over-bound"} {
		bad := records[0]
		bad.GraphID = "graph-invalid-" + kind
		bad.Nodes = append([]graph.Node(nil), bad.Nodes...)
		switch kind {
		case "duplicate":
			bad.Nodes[1].ID = bad.Nodes[0].ID
		case "malformed":
			bad.Nodes[0].ID = "not a node ID"
		case "over-bound":
			for len(bad.Nodes) <= graph.MaxNodes {
				node := bad.Nodes[0]
				node.ID = fmt.Sprintf("overflow%03d", len(bad.Nodes))
				bad.Nodes = append(bad.Nodes, node)
			}
		}
		invalid, validation, invalidErr := graph.NewRevision(bad)
		if invalidErr == nil || validation.Outcome != graph.ValidationInvalid {
			t.Fatalf("invalid %s accepted by canonical validator", kind)
		}
		if _, err := repo.PublishGraph(ctx, graph.PublishRequest{Revision: invalid, Validation: validation, IdempotencyKey: bad.GraphID}, fact(bad.GraphID)); err == nil {
			t.Fatalf("invalid %s published", kind)
		}
	}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(root, "fixture-manifest.json"), raw, 0600))
	svc.Config.API.Listen = "127.0.0.1:" + os.Getenv("AEGIS_ACCEPTANCE_PORT")
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	repository, err := filepath.Abs("../..")
	check(err)
	svc.Config.API.UnixSocket = filepath.Join(repository, ".scratch", ".graph-proof-"+os.Getenv("AEGIS_ACCEPTANCE_PORT")+".sock")
	svc.Config.Credentials.ProviderAuth = nil
	raw, err = json.Marshal(svc.Config)
	check(err)
	check(os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600))
}

func TestGraphConsoleFrozenBrowser(t *testing.T) {
	if os.Getenv("AEGIS_GRAPH_CANDIDATE") == "" {
		t.Skip("built candidate and lifetime claimed port required")
	}
	root := os.Getenv("AEGIS_GRAPH_ROOT")
	if root == "" || os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("private root and lifetime claim required")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fixture root must be new")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	seed := exec.Command(os.Args[0], "-test.run=^TestGraphConsoleFrozenSeed$", "-test.v")
	seed.Env = os.Environ()
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	browser := exec.Command("python3", "../../scripts/graph_console_frozen_browser_test.py")
	browser.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	if out, err := browser.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v %s", err, out)
	} else {
		t.Log(string(out))
	}
}
