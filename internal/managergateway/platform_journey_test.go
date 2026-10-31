package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/managerplatform"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

// The only synthetic boundary is the model proposal producer. Gateway session,
// proposal decoder, confirmation, adapters, application services, admission,
// transactional Badger records and readbacks are production implementations.
type platformProposalModel struct {
	response []byte
	calls    int
	inputs   []string
}

func (m *platformProposalModel) Turn(_ context.Context, _ string, text string, _ int) ([]byte, error) {
	m.calls++
	m.inputs = append(m.inputs, text)
	return m.response, nil
}
func (m *platformProposalModel) propose(t *testing.T, name string, input any) {
	t.Helper()
	data, e := json.Marshal(map[string]any{"schema_version": managerdomain.ResponseSchemaVersion, "kind": "proposal", "message": "Untrusted model claim: completed successfully", "proposal": map[string]any{"operation": "platform.call", "arguments": map[string]any{"operation": name, "input": input}}})
	if e != nil {
		t.Fatal(e)
	}
	m.response = data
}
func platformRuntime(t *testing.T, a *app.Service, subject core.Subject, id string, model managerdomain.Gateway) *conversation {
	t.Helper()
	now := a.Now()
	digest := "sha256:" + strings.Repeat("a", 64)
	route := managerdomain.RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, HermesPath: "/synthetic/proposal-only", HermesVersion: "0.21.3", OllamaMode: "external-local", OllamaEndpoint: "http://127.0.0.1:1", OllamaVersion: "fixture", Model: managerdomain.ModelIdentity{Registry: "ollama", Name: "synthetic-proposal-only", Digest: digest, ContextLength: 65536, TemplateIdentity: "fixture", InstructionVersion: managerdomain.InstructionVersion, SchemaVersion: managerdomain.ResponseSchemaVersion, OllamaVersion: "fixture", HermesVersion: "0.21.3", ConformanceVersion: managerdomain.ConformanceVersion, Certified: true, CertifiedAt: now}, ProxyIdentity: "synthetic-proposal-only", IssuedAt: now, ExpiresAt: subject.ExpiresAt}
	guard, e := managerdomain.NewGuard(1<<20, 1<<20, 2, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	session, e := managerdomain.NewSession(context.Background(), managerdomain.SessionConfig{SessionID: id, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Route: route, Gateway: model, GatewaySessionID: id, Guard: guard, Operations: gatewayOperations{service: a, subject: subject}, Platform: managerplatform.New(a, subject), Confirm: func(context.Context, string) (bool, error) {
		t.Fatal("platform mutation used credential confirmation callback")
		return false, nil
	}, Intake: func(context.Context, string) ([]byte, error) { return nil, errors.New("forbidden") }, Receipt: func(context.Context, managerdomain.SessionReceipt) error { return nil }, MaximumResponseBytes: 1 << 20, Now: a.Now})
	if e != nil {
		t.Fatal(e)
	}
	runtime := &conversation{session: session, ctx: context.Background()}
	runtime.active.Store(true)
	return runtime
}
func platformGateway(t *testing.T, a *app.Service, subject core.Subject, model *platformProposalModel) (*Service, string, string) {
	t.Helper()
	gateway, e := New(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	id := "platform-conversation"
	token := "synthetic-platform-token"
	gateway.sessions[id] = session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: a.Now(), expires: subject.ExpiresAt, mode: "conversational", runtime: platformRuntime(t, a, subject, id, model)}
	return gateway, id, token
}

var platformDigest = regexp.MustCompile(`type confirm (sha256:[a-f0-9]{64})`)

func platformTurn(t *testing.T, g *Service, sub core.Subject, id, token, text string) TurnResult {
	t.Helper()
	out, e := g.Turn(context.Background(), sub, id, token, text)
	if e != nil {
		t.Fatal(e)
	}
	if out.Origin != TurnOriginAuthoritative {
		t.Fatalf("operation returned model provenance: %+v", out)
	}
	if strings.Contains(out.Message, "Untrusted model claim") {
		t.Fatal("model success narration substituted for authoritative result")
	}
	return out
}
func platformConfirm(t *testing.T, g *Service, sub core.Subject, id, token, text string) TurnResult {
	t.Helper()
	preview := platformTurn(t, g, sub, id, token, text)
	match := platformDigest.FindStringSubmatch(preview.Message)
	if len(match) != 2 {
		t.Fatalf("no immutable review: %+v", preview)
	}
	result := platformTurn(t, g, sub, id, token, "confirm "+match[1])
	replay := platformTurn(t, g, sub, id, token, "confirm "+match[1])
	if !strings.Contains(replay.Message, "no pending") {
		t.Fatalf("duplicate confirmation not consumed: %s", replay.Message)
	}
	return result
}

func TestPlatformConversationProductionJourney(t *testing.T) {
	ctx := context.Background()
	a, repo, sub, _, _ := contributionFixture(t)
	if a.CredentialAuthority != nil {
		t.Fatal("fixture unexpectedly depends on secret setup")
	}
	model := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, model)
	// Different ordinary-language paraphrases reach the exact same general envelope.
	for _, text := range []string{"Which registered participants can I use for this work?", "Could you enumerate our fleet identities for me?"} {
		model.propose(t, "agents.list", map[string]any{})
		out := platformTurn(t, g, sub, id, token, text)
		if !strings.Contains(out.Message, "contribution-controller") {
			t.Fatal(out.Message)
		}
	}
	// Missing input collection causes no write; next model turn receives retained input.
	model.propose(t, "agents.disable", map[string]any{})
	out := platformTurn(t, g, sub, id, token, "Disable one of our agents")
	if !strings.Contains(out.Message, "missing_input") {
		t.Fatal(out.Message)
	}
	model.propose(t, "agents.disable", map[string]any{"expected": map[string]any{"id": "contribution-tester"}})
	platformConfirm(t, g, sub, id, token, "I meant contribution-tester")
	if !strings.Contains(model.inputs[len(model.inputs)-1], "Missing/invalid input:") {
		t.Fatal("clarification lost context")
	}
	current, e := a.GetFleetAgentAs(ctx, sub, "contribution-tester", 0)
	if e != nil || current.Revision.Lifecycle != "disabled" {
		t.Fatalf("Registry mutation absent: %+v %v", current, e)
	}
	// Resolve publication input from canonical readback, not fixture digests.
	revision, validation, e := loop.NewRevision(loop.LoopRevision{LoopID: "conversational-loop", Revision: 1, EntryStepID: "work", Steps: []loop.Step{{ID: "work", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "exact-output", MediaType: "application/json", ExpectedDigest: "sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1}}}, {ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}}, Transitions: []loop.Transition{{ID: "finish", FromStepID: "work", ToStepID: "done"}}, RequiredEvidence: []loop.EvidenceRequirement{{Claim: "exact-output", ProducerStepID: "work"}}})
	if e != nil || validation.Outcome != loop.ValidationValid {
		t.Fatalf("candidate: %v %+v", e, validation)
	}
	model.propose(t, "loops.publish", map[string]any{"agent_id": "contribution-controller", "revision": revision, "idempotency_key": "conversation-publication"})
	platformConfirm(t, g, sub, id, token, "Publish the reviewed reusable definition for our controller")
	stored, e := a.GetLoopAs(ctx, sub, revision.LoopID, 1)
	if e != nil || stored.Digest != revision.Digest {
		t.Fatalf("Loop mutation absent: %v", e)
	}
	loopref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: stored.LoopID, Revision: stored.Revision, Digest: stored.Digest}
	model.propose(t, "loops.activate", map[string]any{"agent_id": "contribution-controller", "loop": loopref})
	platformConfirm(t, g, sub, id, token, "Make that exact definition available for new work")
	agent, e := a.GetFleetAgentAs(ctx, sub, "contribution-controller", 0)
	if e != nil {
		t.Fatal(e)
	}
	agentref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.Revision.AgentID, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}
	inputs := []graph.Port{}
	outputs := []graph.Port{}
	inputMappings := []graph.InputMapping{}
	outputMappings := []graph.OutputMapping{}
	for _, p := range stored.Inputs {
		inputs = append(inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
		inputMappings = append(inputMappings, graph.InputMapping{GraphInput: p.ID, ToNodeID: "work", ToPort: p.ID})
	}
	for _, p := range stored.Outputs {
		outputs = append(outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
		outputMappings = append(outputMappings, graph.OutputMapping{FromNodeID: "work", FromPort: p.ID, GraphOutput: p.ID})
	}
	candidate := graph.GraphRevision{SchemaVersion: graph.RevisionSchemaVersion, GraphID: "conversational-graph", Revision: 1, Inputs: inputs, Outputs: outputs, Nodes: []graph.Node{{ID: "work", Participant: agentref, Loop: loopref, Inputs: inputs, Outputs: outputs}}, InputMappings: inputMappings, Dependencies: []graph.Dependency{}, OutputMappings: outputMappings, AdmissionRules: []graph.AdmissionRule{}, Validator: graph.ValidatorSpec{ID: graph.ValidatorID, Version: graph.ValidatorVersion}}
	model.propose(t, "graphs.publish", map[string]any{"agent_id": agentref.ID, "revision": candidate, "idempotency_key": "conversation-graph"})
	platformConfirm(t, g, sub, id, token, "Compose and publish the reviewed exact graph")
	published, e := a.GetGraphAs(ctx, sub, candidate.GraphID, 1)
	if e != nil {
		t.Fatal(e)
	}
	graphref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.GraphID, Revision: published.Revision, Digest: published.Digest}
	values := []graph.NormalizedInput{}
	for _, p := range inputs {
		values = append(values, graph.NormalizedInput{PortID: p.ID, Type: p.Type, Value: json.RawMessage(`"synthetic request input"`)})
	}
	model.propose(t, "graphs.submit", map[string]any{"agent_id": agentref.ID, "graph": graphref, "inputs": values, "idempotency_key": "conversation-submission", "max_attempts": 1})
	submitted := platformConfirm(t, g, sub, id, token, "Arrange this graph with those exact inputs, without claiming it has run")
	if !strings.Contains(submitted.Message, "preparation-pending") {
		t.Fatalf("admission confused with runtime execution: %s", submitted.Message)
	}
	items, e := repo.ListQueueItems(ctx)
	if e != nil || len(items) != 1 {
		t.Fatalf("Queue submission count %d %v", len(items), e)
	}
	// A second independently prepared proposal with the same stable request key
	// must recover the original durable identities, not enqueue a replacement.
	model.propose(t, "graphs.submit", map[string]any{"agent_id": agentref.ID, "graph": graphref, "inputs": values, "idempotency_key": "conversation-submission", "max_attempts": 1})
	replayed := platformConfirm(t, g, sub, id, token, "Submit this same stable request again")
	if strings.Contains(replayed.Message, "operation error") {
		t.Fatalf("same-key replay failed: %s", replayed.Message)
	}
	again, e := repo.ListQueueItems(ctx)
	if e != nil || len(again) != 1 || again[0].ItemID != items[0].ItemID {
		t.Fatalf("same-key replay replaced Queue identity: %+v %v", again, e)
	}
	item := items[0]
	model.propose(t, "queue.cancel", map[string]any{"agent_id": agentref.ID, "queue_item_id": item.ItemID})
	platformConfirm(t, g, sub, id, token, "Please cancel the admitted work before runtime binding")
	view, e := a.GetQueueItemAs(ctx, sub, item.ItemID)
	if e != nil || view.Projection.State != "cancelled" || len(view.Attempts) != 0 {
		t.Fatalf("Queue mutation/readback: %+v %v", view, e)
	}
	// A store restart and a new conversation re-resolve durable state. No pending
	// confirmation or old model history is used as completion evidence.
	if e = repo.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := fleetbadger.Open(ctx, filepath.Join(a.Config.StateDir, "persistence", "fleet-v1"))
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	configureContributionFleet(t, a, reopened)
	nextModel := &platformProposalModel{}
	next, nextID, nextToken := platformGateway(t, a, sub, nextModel)
	for _, read := range []struct {
		name  string
		input any
		want  string
	}{{"agents.history", map[string]any{"id": "contribution-tester"}, "disabled"}, {"loops.show", map[string]any{"id": loopref.ID, "revision": 1}, loopref.Digest}, {"graphs.show", map[string]any{"id": graphref.ID, "revision": 1}, graphref.Digest}, {"queue.show", map[string]any{"id": item.ItemID}, "cancelled"}} {
		nextModel.propose(t, read.name, read.input)
		out := platformTurn(t, next, sub, nextID, nextToken, "Look up the retained exact platform record again")
		if !strings.Contains(out.Message, read.want) {
			t.Fatalf("durable %s readback: %s", read.name, out.Message)
		}
	}
	if model.calls < 8 {
		t.Fatal("journey bypassed general proposal integration")
	}
}
