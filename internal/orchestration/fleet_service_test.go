package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

type fleetServiceRepository struct {
	FleetRepository
	agent          registry.AgentRevision
	loop           loop.LoopRevision
	graph          graph.GraphRevision
	loadErr        error
	accepted       *fleet.AcceptedSubmission
	rejected       *queue.Rejection
	registerFact   fleet.AuditFact
	loopPublished  bool
	graphPublished bool
}

type staticFleetSource []registry.Candidate

func (source staticFleetSource) Discover(context.Context) ([]registry.Candidate, error) {
	return append([]registry.Candidate(nil), source...), nil
}

func (repository *fleetServiceRepository) GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error) {
	return repository.agent, repository.loadErr
}
func (repository *fleetServiceRepository) GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error) {
	return repository.loop, repository.loadErr
}
func (repository *fleetServiceRepository) GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error) {
	return repository.graph, repository.loadErr
}
func (repository *fleetServiceRepository) AcceptSubmission(_ context.Context, value fleet.AcceptedSubmission, _ fleet.AuditFact) (bool, error) {
	repository.accepted = &value
	return true, nil
}
func (repository *fleetServiceRepository) RejectSubmission(_ context.Context, value queue.Rejection, _ fleet.AuditFact) (bool, error) {
	repository.rejected = &value
	return true, nil
}
func (repository *fleetServiceRepository) RegisterAgent(_ context.Context, _ registry.AgentRegistration, _ registry.AgentRevision, fact fleet.AuditFact) (bool, error) {
	repository.registerFact = fact
	return true, nil
}
func (repository *fleetServiceRepository) PublishLoop(context.Context, loop.PublishRequest, fleet.AuditFact) (loop.PublicationDecision, error) {
	repository.loopPublished = true
	return loop.PublicationDecision{}, nil
}
func (repository *fleetServiceRepository) PublishGraph(context.Context, graph.PublishRequest, fleet.AuditFact) (graph.PublicationDecision, error) {
	repository.graphPublished = true
	return graph.PublicationDecision{}, nil
}

type fleetAuthorityRepository struct {
	core.AuthorityRepository
	mandate   core.Mandate
	authority core.AuthorityContext
}

func (repository fleetAuthorityRepository) GetMandate(context.Context, string) (core.Mandate, error) {
	return repository.mandate, nil
}
func (repository fleetAuthorityRepository) GetAuthorityContext(context.Context, string) (core.AuthorityContext, error) {
	return repository.authority, nil
}

type fleetAuthorityCommands struct {
	core.AuthorityCommandRepository
	authority  core.AuthorityContext
	admitted   bool
	err        error
	admissions *int
}

func (commands fleetAuthorityCommands) AuthorityAdmission(_ context.Context, _, _ string, at time.Time) (core.AuthorityAdmissionView, error) {
	if commands.admissions != nil {
		*commands.admissions = *commands.admissions + 1
	}
	return core.AuthorityAdmissionView{AuthorityContext: commands.authority, EvaluatedAt: at, Admitted: commands.admitted, ReasonCode: "admitted"}, commands.err
}

func TestFleetReadinessCoversEveryConsequentialAction(t *testing.T) {
	service, _, _, subject, authorityRef, _ := fleetServiceFixture(t)
	actions := []FleetAction{FleetActionRegister, FleetActionAgentRevision, FleetActionLoopValidate, FleetActionLoopPublish, FleetActionGraphValidate, FleetActionGraphPublish, FleetActionSubmission, FleetActionQueueAdmission, FleetActionClaim, FleetActionReclaim, FleetActionRuntimeEffect, FleetActionEvidenceVerify, FleetActionDisposition}
	for _, action := range actions {
		request := ReadinessRequest{Action: action, Subject: subject, Authority: authorityRef}
		if action == FleetActionRegister {
			request.Authority = reference.DigestRef{}
		}
		got := service.Readiness(context.Background(), request)
		if got.State != ReadinessReady || got.ReasonCode != "ready" {
			t.Fatalf("action %q readiness=%+v", action, got)
		}
	}

	deniedSubject := subject
	deniedSubject.ID = "prompt-selected-display-name"
	got := service.Readiness(context.Background(), ReadinessRequest{Action: FleetActionSubmission, Subject: deniedSubject, Authority: authorityRef})
	if got.State != ReadinessDenied || got.ReasonCode != "authenticated_subject_mismatch" {
		t.Fatalf("model/display identity did not fail closed: %+v", got)
	}
	if strings.Contains(strings.Join(repairStrings(got.RepairActions), " "), "credential") {
		t.Fatalf("optional credentials became a readiness gate: %+v", got)
	}
	service.authorityCommands = fleetAuthorityCommands{authority: service.authority.(fleetAuthorityRepository).authority, admitted: false}
	got = service.Readiness(context.Background(), ReadinessRequest{Action: FleetActionRuntimeEffect, Subject: subject, Authority: authorityRef})
	if got.State != ReadinessDenied || got.ReasonCode != "authority_inactive_or_inconsistent" {
		t.Fatalf("revoked authority did not deny the next effect: %+v", got)
	}
}

func TestFleetReadinessSeparatesEmptyUnavailableAndRepairRequired(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	request := ReadinessRequest{Action: FleetActionSubmission, Subject: subject, Authority: authorityRef, Graph: graphRef}

	repository.loadErr = fleet.ErrNotFound
	if got := service.Readiness(context.Background(), request); got.State != ReadinessEmpty || got.ReasonCode != "definition_not_found" {
		t.Fatalf("not found readiness=%+v", got)
	}
	repository.loadErr = errors.New("device offline")
	if got := service.Readiness(context.Background(), request); got.State != ReadinessUnavailable || got.ReasonCode != "fleet_store_unavailable" {
		t.Fatalf("unavailable readiness=%+v", got)
	}
	repository.loadErr = fleet.ErrCorrupt
	if got := service.Readiness(context.Background(), request); got.State != ReadinessRepair || got.ReasonCode != "fleet_store_corrupt" || len(got.RepairActions) != 1 {
		t.Fatalf("corrupt readiness=%+v", got)
	}
}

func TestPublishLoopRequiresFreshAuthorityAdmissionAtPublicationBoundary(t *testing.T) {
	service, repository, authority, subject, authorityRef, _ := fleetServiceFixture(t)
	if got := service.Readiness(context.Background(), ReadinessRequest{Action: FleetActionLoopPublish, Subject: subject, Authority: authorityRef}); got.State != ReadinessReady {
		t.Fatalf("initial contextual readiness=%+v", got)
	}
	service.authorityCommands = fleetAuthorityCommands{authority: authority, admitted: false}

	_, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef})
	if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "authority_inactive_or_inconsistent") {
		t.Fatalf("publication after authority denial err=%v", err)
	}
	if repository.loopPublished {
		t.Fatal("Loop was published after authority admission was denied")
	}
}

func TestPublicationResolvesAuthorityExactlyOnceAtEachBoundary(t *testing.T) {
	service, repository, authority, subject, authorityRef, _ := fleetServiceFixture(t)
	admissions := 0
	service.authorityCommands = fleetAuthorityCommands{authority: authority, admitted: true, admissions: &admissions}

	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef}); err != nil {
		t.Fatalf("publish Loop: %v", err)
	}
	if admissions != 1 || !repository.loopPublished {
		t.Fatalf("Loop publication admissions=%d published=%t", admissions, repository.loopPublished)
	}
	if _, err := service.PublishGraph(context.Background(), PublishGraphRequest{Subject: subject, Authority: authorityRef, Publication: graph.PublishRequest{Revision: repository.graph}}); err != nil {
		t.Fatalf("publish Graph: %v", err)
	}
	if admissions != 2 || !repository.graphPublished {
		t.Fatalf("Graph publication cumulative admissions=%d published=%t", admissions, repository.graphPublished)
	}
}

func TestFleetSubmissionBindsExactAuthorityAndHistoricalDefinitions(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	request := SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-1", IdempotencyKey: "submit-key-1", SnapshotID: "snapshot-1", QueueItemID: "queue-item-1", GraphRunID: "graph-run-1", TransitionID: "transition-1", RejectionID: "rejection-1", MaxAttempts: 3}
	decision, err := service.PrepareGraphRun(context.Background(), request)
	if err != nil || !decision.Created || decision.Accepted == nil || decision.Rejection != nil {
		t.Fatalf("admission decision=%+v err=%v", decision, err)
	}
	accepted := repository.accepted
	if accepted == nil || accepted.Submission.Authority != authorityRef || accepted.QueueItem.Authority != authorityRef || accepted.GraphRun.Authority != authorityRef {
		t.Fatalf("authority binding was not preserved: %+v", accepted)
	}
	if accepted.Submission.MandateID != "mandate-1" || accepted.Submission.Runtime != "hermes-agent" || accepted.Snapshot.Graph != graphRef {
		t.Fatalf("historical run binding was incomplete: %+v", accepted)
	}

	wrong := request
	wrong.IdempotencyKey = "submit-key-2"
	wrong.SubmissionID = "submission-2"
	wrong.RejectionID = "rejection-2"
	wrong.Authority.Digest = "sha256:" + strings.Repeat("9", 64)
	decision, err = service.PrepareGraphRun(context.Background(), wrong)
	if err != nil || decision.Rejection == nil || decision.Accepted != nil || repository.rejected == nil {
		t.Fatalf("wrong authority was not durably rejected: decision=%+v err=%v", decision, err)
	}
	if repository.rejected.ReasonCode != "readiness_denied" {
		t.Fatalf("unexpected bounded rejection: %+v", repository.rejected)
	}

	disabled := request
	disabled.IdempotencyKey = "submit-key-3"
	disabled.SubmissionID = "submission-3"
	disabled.RejectionID = "rejection-3"
	repository.agent.Lifecycle = registry.LifecycleDisabled
	decision, err = service.PrepareGraphRun(context.Background(), disabled)
	if err != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "participant_unavailable" {
		t.Fatalf("disabled participant was not durably rejected: decision=%+v err=%v", decision, err)
	}
	if accepted.Snapshot.Graph != graphRef {
		t.Fatalf("current participant drift rewrote historical snapshot: %+v", accepted.Snapshot)
	}
}

func TestRegisterFleetAgentUsesAuthenticatedBoundaryAndMetadataOnlyAudit(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	source := staticFleetSource{{AgentID: "agent-1", Source: registry.FleetSource{Kind: "hermes", FleetID: "fleet-1", SourceID: "profile-1"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}, Ownership: registry.Ownership{OwnerID: "owner-1", AccountabilityID: "accountability-1"}, Lifecycle: registry.LifecycleEnabled, Charter: revisionReference("agent-1", 1, "a"), CapabilityDeclarations: []string{"fleet.execute"}}}
	registration, revision, created, err := service.RegisterFleetAgent(context.Background(), RegisterFleetAgentRequest{Subject: subject, Source: source, Identity: registry.FleetSource{Kind: "hermes", FleetID: "fleet-1", SourceID: "profile-1"}})
	if err != nil || !created || registration.AgentID != "agent-1" || revision.Digest == "" {
		t.Fatalf("registration=%+v revision=%+v created=%v err=%v", registration, revision, created, err)
	}
	if repository.registerFact.Event.SubjectID != subject.ID || repository.registerFact.Event.PrincipalID != subject.PrincipalID || len(repository.registerFact.Event.Metadata) != 0 {
		t.Fatalf("registration audit did not preserve authenticated metadata-only provenance: %+v", repository.registerFact)
	}
}

func fleetServiceFixture(t *testing.T) (*FleetService, *fleetServiceRepository, core.AuthorityContext, core.Subject, reference.DigestRef, reference.RevisionRef) {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	subject := core.Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	runtime := core.RuntimeDescriptor{Name: "hermes", Runtime: "hermes-agent", Version: "0.18.2", Executable: "/usr/bin/hermes", Installation: "system", AdapterVersion: "1"}
	mandate := core.Mandate{ID: "mandate-1", Subject: subject, AgentID: "agent-1", StanzaID: "operator", CharterRevision: 1, CharterDigest: "sha256:" + strings.Repeat("a", 64), Runtime: runtime, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	authority := core.AuthorityContext{ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: subject.ID, AgentID: mandate.AgentID, CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID}, IssuedAt: mandate.IssuedAt, ExpiresAt: mandate.ExpiresAt}
	authority.Digest = core.AuthorityContextDigest(authority)

	loopRevision, loopValidation, err := loop.NewRevision(loop.LoopRevision{LoopID: "loop-1", Revision: 1, EntryStepID: "work", Steps: []loop.Step{{ID: "work", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}}, {ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}}, Transitions: []loop.Transition{{ID: "finish", FromStepID: "work", ToStepID: "done"}}})
	if err != nil {
		t.Fatalf("%v: %+v", err, loopValidation.Issues)
	}
	agent := registry.AgentRevision{AgentID: "agent-1", Revision: 1, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}, Lifecycle: registry.LifecycleEnabled, Charter: revisionRef("agent-1", mandate.CharterRevision, mandate.CharterDigest), Digest: "sha256:" + strings.Repeat("b", 64)}
	agentRef := revisionRef(agent.AgentID, agent.Revision, agent.Digest)
	loopRef := revisionRef(loopRevision.LoopID, loopRevision.Revision, loopRevision.Digest)
	graphRevision, _, err := graph.NewRevision(graph.GraphRevision{GraphID: "graph-1", Revision: 1, Nodes: []graph.Node{{ID: "node-1", Participant: agentRef, Loop: loopRef}}})
	if err != nil {
		t.Fatal(err)
	}
	repository := &fleetServiceRepository{agent: agent, loop: loopRevision, graph: graphRevision}
	authorityRepository := fleetAuthorityRepository{mandate: mandate, authority: authority}
	commands := fleetAuthorityCommands{authority: authority, admitted: true}
	service, err := NewFleetService(repository, authorityRepository, commands, func(_ context.Context, _ FleetAction, candidate core.Subject) error {
		if candidate.PrincipalID != "principal-1" {
			return ErrDenied
		}
		return nil
	}, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return service, repository, authority, subject, digestRef(authority.ID, authority.Digest), revisionRef(graphRevision.GraphID, graphRevision.Revision, graphRevision.Digest)
}

func revisionReference(id string, revision uint64, digestChar string) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: "sha256:" + strings.Repeat(digestChar, 64)}
}

func repairStrings(values []RepairAction) []string {
	out := make([]string, len(values))
	for index := range values {
		out[index] = string(values[index])
	}
	return out
}
