package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Domain contract: Registry reads may link historical execution only through
// exact participant revision/digest and Queue snapshot identity. These fixtures
// create durable synthetic history, not a live runtime or current admission.
func storeAgentExecutionHistory(t *testing.T, svc *app.Service, store *fleetbadger.Store, agent registry.AgentRevision) fleet.AcceptedSubmission {
	t.Helper()
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	fact := func(kind, id string) fleet.AuditFact {
		return fleet.AuditFact{Event: core.AuditEvent{Type: kind, SubjectID: id, PrincipalID: svc.Config.Principal.ID, Outcome: "succeeded", Reason: "synthetic historical evidence fixture"}}
	}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	form, err := decodeLoopComposerForm(composerRequest(validLoopComposerValues()))
	check(err)
	lr, lv, err := app.NewLoopRevision(form.Revision)
	check(err)
	provenance, err := loop.NewPublicationProvenance(loop.PublicationProvenance{
		Loop:           loop.NewProvenanceRevision(lr.LoopID, lr.Revision, lr.Digest),
		PublisherAgent: loop.NewProvenanceRevision(agent.AgentID, agent.Revision, agent.Digest),
		Authority:      loop.NewProvenanceDigest("history-authority", "sha256:"+strings.Repeat("d", 64)),
		MandateID:      "history-mandate", StanzaID: "principal", Runtime: loop.ProvenanceRuntime{Runtime: agent.Runtime.Runtime},
		Charter: loop.NewProvenanceRevision(agent.Charter.ID, agent.Charter.Revision, agent.Charter.Digest), ValidationDigest: lv.Digest,
	})
	check(err)
	_, err = store.PublishLoop(ctx, loop.PublishRequest{Revision: lr, Validation: lv, Provenance: provenance, IdempotencyKey: "agent-history-loop"}, fact("loop.published", lr.LoopID))
	check(err)
	input := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	output := graph.Port{ID: "result", Type: graph.TypeString, Required: true}
	gr, gv, err := graph.NewRevision(graph.GraphRevision{
		GraphID: "agent-history-graph", Revision: 1, Inputs: []graph.Port{input}, Outputs: []graph.Port{output},
		Nodes:         []graph.Node{{ID: "work", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: lr.Revision, Digest: lr.Digest}, Inputs: []graph.Port{input}, Outputs: []graph.Port{output}}},
		InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: "work", ToPort: "value"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "result", GraphOutput: "result"}},
	})
	check(err)
	_, err = store.PublishGraph(ctx, graph.PublishRequest{Revision: gr, Validation: gv, IdempotencyKey: "agent-history-graph"}, fact("graph.published", gr.GraphID))
	check(err)
	snapshot, err := graph.NewRunSnapshot("agent-history-snapshot", gr, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: []byte(`"history"`)}})
	check(err)
	now := svc.Now()
	authority := ref("history-authority", "sha256:"+strings.Repeat("d", 64))
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "agent-history-submission", IdempotencyKey: "agent-history-submission", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: authority, MandateID: "history-mandate", Runtime: agent.Runtime.Runtime, SubmittedAt: now})
	check(err)
	item, err := queue.NewItem(queue.Item{ItemID: "agent-history-item", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: authority, GraphRunID: "agent-history-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	check(err)
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: submission.Snapshot, Authority: authority, CreatedAt: now})
	check(err)
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "agent-history-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "synthetic historical submission", OccurredAt: now})
	check(err)
	accepted := fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}
	_, err = store.AcceptSubmission(ctx, accepted, fact("submission.accepted", submission.SubmissionID))
	check(err)
	return accepted
}

// agentHistoryReadProbe injects independent near misses at the repository
// read boundary only. Canonical persisted fixtures remain untouched. A snapshot
// near miss must be excluded or make the production projection unavailable,
// never attach unrelated work to the selected Agent.
type agentHistoryReadProbe struct {
	fleet.Repository
	mutation atomic.Value
}

func (p *agentHistoryReadProbe) field() string { value, _ := p.mutation.Load().(string); return value }
func (p *agentHistoryReadProbe) GetGraphRunSnapshot(ctx context.Context, id string) (graph.GraphRunSnapshot, error) {
	snapshot, err := p.Repository.GetGraphRunSnapshot(ctx, id)
	snapshot.Participants = append([]reference.RevisionRef(nil), snapshot.Participants...)
	if err == nil && id == "agent-history-snapshot" && snapshot.SnapshotID == id && len(snapshot.Participants) > 0 {
		switch p.field() {
		case "participant-id":
			snapshot.Participants[0].ID = "unrelated-agent"
		case "participant-revision":
			snapshot.Participants[0].Revision++
		case "participant-digest":
			snapshot.Participants[0].Digest = "sha256:" + strings.Repeat("e", 64)
		case "no-participant":
			snapshot.Participants = nil
		}
	}
	return snapshot, err
}
func (p *agentHistoryReadProbe) ListQueueItems(ctx context.Context) ([]queue.Item, error) {
	items, err := p.Repository.ListQueueItems(ctx)
	items = append([]queue.Item(nil), items...)
	for index := range items {
		if err != nil || items[index].ItemID != "agent-history-item" {
			continue
		}
		switch p.field() {
		case "item-id":
			items[index].ItemID = "unrelated-item"
		case "snapshot-id":
			items[index].Snapshot.ID = "unrelated-snapshot"
		case "snapshot-digest":
			items[index].Snapshot.Digest = "sha256:" + strings.Repeat("e", 64)
		}
	}
	return items, err
}

// agentReadOnlyState fingerprints canonical authority and evidence in memory;
// it never emits stored values, credentials, or browser session tokens.
func agentReadOnlyState(t *testing.T, svc *app.Service) [32]byte {
	t.Helper()
	ctx := context.Background()
	values := map[string]any{}
	add := func(name string, value any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		values[name] = value
	}
	mandates, err := svc.Authority.ListMandates(ctx)
	add("mandates", mandates, err)
	authorities, err := svc.Authority.ListAuthorityContexts(ctx)
	add("authorities", authorities, err)
	sessions, err := svc.ListSessions()
	add("sessions", sessions, err)
	receipts, err := svc.ListReceipts()
	add("receipts", receipts, err)
	approvals, err := svc.ListApprovals()
	add("approvals", approvals, err)
	audits, err := svc.FleetRepository.AuditEvents(ctx)
	add("fleet_audit", audits, err)
	items, err := svc.FleetRepository.ListQueueItems(ctx)
	add("queue", items, err)
	claims, err := svc.FleetRepository.ListClaims(ctx)
	add("claims", claims, err)
	attempts, err := svc.FleetRepository.ListAttempts(ctx)
	add("attempts", attempts, err)
	registrations, err := svc.FleetRepository.ListAgentRegistrations(ctx)
	add("registrations", registrations, err)
	for _, registration := range registrations {
		revisions, err := svc.FleetRepository.ListAgentRevisions(ctx, registration.AgentID)
		add("agent-revisions/"+registration.AgentID, revisions, err)
	}
	loops, err := svc.FleetRepository.ListLoopRevisions(ctx)
	add("loops", loops, err)
	loopEvents, err := svc.FleetRepository.ListLoopLifecycleEvents(ctx)
	add("loop-lifecycle", loopEvents, err)
	graphs, err := svc.FleetRepository.ListGraphRevisions(ctx)
	add("graphs", graphs, err)
	graphLifecycles, err := svc.FleetRepository.ListGraphLifecycles(ctx)
	add("graph-lifecycle", graphLifecycles, err)
	submissions, err := svc.FleetRepository.ListSubmissions(ctx)
	add("submissions", submissions, err)
	for _, submission := range submissions {
		snapshot, err := svc.FleetRepository.GetGraphRunSnapshot(ctx, submission.Snapshot.ID)
		add("snapshot/"+submission.Snapshot.ID, snapshot, err)
	}
	runs, err := svc.FleetRepository.ListGraphRuns(ctx)
	add("graph-runs", runs, err)
	rejections, err := svc.FleetRepository.ListRejections(ctx)
	add("rejections", rejections, err)
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func TestAgentDetailExecutionJoinRejectsIndependentNearMisses(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	subject, err := svc.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	selected := app.FleetAgent{Revision: registry.AgentRevision{AgentID: "office", Revision: 7, Digest: "agent-digest"}}
	// Deliberately malformed projected inputs test every comparison independently.
	// The production persisted-route positive fixture is asserted in server_test.go.
	for _, field := range []string{"exact", "participant-id", "participant-revision", "participant-digest", "item-id", "snapshot-id", "snapshot-digest", "no-participant"} {
		t.Run(field, func(t *testing.T) {
			participant := reference.RevisionRef{ID: "office", Revision: 7, Digest: "agent-digest"}
			item := queue.Item{ItemID: "queue-exact", Snapshot: reference.DigestRef{ID: "snapshot-exact", Digest: "snapshot-digest"}}
			accepted := app.AcceptedGraphRunView{Snapshot: graph.GraphRunSnapshot{SnapshotID: "snapshot-exact", Digest: "snapshot-digest"}, QueueItem: item}
			switch field {
			case "participant-id":
				participant.ID = "other"
			case "participant-revision":
				participant.Revision++
			case "participant-digest":
				participant.Digest = "other"
			case "item-id":
				item.ItemID = "other"
			case "snapshot-id":
				item.Snapshot.ID = "other"
			case "snapshot-digest":
				item.Snapshot.Digest = "other"
			}
			if field != "no-participant" {
				accepted.Snapshot.Participants = []reference.RevisionRef{participant}
			}
			surface := app.FleetSurface{Submissions: app.SubmissionHistory{Accepted: []app.AcceptedGraphRunView{accepted}}, Queue: []app.QueueExecutionView{{Item: item, Projection: queue.Projection{State: queue.StateQueued}}}}
			record, err := consoleAgentDetail(context.Background(), svc, subject, selected, selected, surface)
			if err != nil {
				t.Fatal(err)
			}
			if field == "exact" {
				if len(record.Agent.Executions) != 1 || record.Agent.Executions[0].URL != consoleRecordURL(consoleQueue, "queue-exact") {
					t.Fatalf("exact execution link missing: %+v", record.Agent.Executions)
				}
			} else if len(record.Agent.Executions) != 0 {
				t.Fatalf("%s exposed unrelated execution: %+v", field, record.Agent.Executions)
			}
		})
	}
}
