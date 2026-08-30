package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

type fleetServiceRepository struct {
	fleet.Repository
	registration    registry.AgentRegistration
	agent           registry.AgentRevision
	loop            loop.LoopRevision
	graph           graph.GraphRevision
	loopLifecycle   loop.Lifecycle
	graphLifecycle  graph.Lifecycle
	loadErr         error
	accepted        *fleet.AcceptedSubmission
	rejected        *queue.Rejection
	registerFact    fleet.AuditFact
	revisionFact    fleet.AuditFact
	loopPublished   bool
	loopPublication loop.PublishRequest
	lifecycleEvent  loop.LifecycleEvent
	lifecycleCalls  int
	graphPublished  bool
	loopExecution   execution.LoopExecution
	claim           queue.Claim
	attempt         execution.Attempt
	completion      fleet.Completion
	projection      queue.Projection
	retryMutation   fleet.RetryMutation
	cancelMutation  fleet.CancellationMutation
	terminalCtxErr  error
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
func (repository *fleetServiceRepository) ListLoopLifecycleEvents(context.Context) ([]loop.LifecycleEvent, error) {
	if repository.loopLifecycle.LoopID != "" {
		event := loop.LifecycleEvent{LoopID: repository.loopLifecycle.LoopID, State: repository.loopLifecycle.State, Digest: "lifecycle-current"}
		if repository.loopLifecycle.State == loop.LifecycleActive {
			event.Revision = loop.NewProvenanceRevision(repository.loopLifecycle.LoopID, repository.loopLifecycle.ActiveRevision, repository.loopLifecycle.ActiveDigest)
		}
		return []loop.LifecycleEvent{event}, repository.loadErr
	}
	return []loop.LifecycleEvent{{LoopID: repository.loop.LoopID, State: loop.LifecycleActive, Revision: loop.NewProvenanceRevision(repository.loop.LoopID, repository.loop.Revision, repository.loop.Digest), Digest: "lifecycle-current"}}, repository.loadErr
}
func (repository *fleetServiceRepository) GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error) {
	return repository.graph, repository.loadErr
}
func (repository *fleetServiceRepository) GetGraphLifecycle(context.Context, string) (graph.Lifecycle, error) {
	if repository.graphLifecycle.GraphID != "" {
		return repository.graphLifecycle, repository.loadErr
	}
	return graph.Lifecycle{GraphID: repository.graph.GraphID, State: graph.LifecycleActive, ActiveRevision: repository.graph.Revision, ActiveDigest: repository.graph.Digest}, repository.loadErr
}
func (repository *fleetServiceRepository) AcceptSubmission(_ context.Context, value fleet.AcceptedSubmission, _ fleet.AuditFact) (bool, error) {
	repository.accepted = &value
	return true, nil
}
func (repository *fleetServiceRepository) RejectSubmission(_ context.Context, value queue.Rejection, _ fleet.AuditFact) (bool, error) {
	repository.rejected = &value
	return true, nil
}
func (repository *fleetServiceRepository) GetQueueItem(context.Context, string) (queue.Item, error) {
	return repository.accepted.QueueItem, nil
}
func (repository *fleetServiceRepository) GetQueueProjection(context.Context, string) (queue.Projection, error) {
	if repository.projection.State != "" {
		return repository.projection, nil
	}
	return queue.Projection{State: queue.StateQueued, AvailableAt: repository.accepted.QueueItem.AvailableAt}, nil
}
func (repository *fleetServiceRepository) GetClaim(context.Context, string) (queue.Claim, error) {
	return repository.claim, nil
}
func (repository *fleetServiceRepository) RetryQueueItem(_ context.Context, mutation fleet.RetryMutation, _ fleet.AuditFact) error {
	repository.retryMutation = mutation
	return nil
}
func (repository *fleetServiceRepository) CancelQueueItem(ctx context.Context, mutation fleet.CancellationMutation, _ fleet.AuditFact) error {
	repository.terminalCtxErr = ctx.Err()
	repository.cancelMutation = mutation
	return nil
}
func (repository *fleetServiceRepository) GetGraphRunSnapshot(context.Context, string) (graph.GraphRunSnapshot, error) {
	return repository.accepted.Snapshot, nil
}
func (repository *fleetServiceRepository) ListLoopExecutions(context.Context) ([]execution.LoopExecution, error) {
	if repository.loopExecution.LoopExecutionID == "" {
		return []execution.LoopExecution{}, nil
	}
	return []execution.LoopExecution{repository.loopExecution}, nil
}
func (repository *fleetServiceRepository) CreateLoopExecution(_ context.Context, value execution.LoopExecution, _ fleet.AuditFact) (bool, error) {
	repository.loopExecution = value
	return true, nil
}
func (repository *fleetServiceRepository) ClaimQueueItem(_ context.Context, claim queue.Claim, attempt execution.Attempt, _ queue.QueueTransition, _ fleet.AuditFact) error {
	repository.claim, repository.attempt = claim, attempt
	return nil
}
func (repository *fleetServiceRepository) CompleteQueueItem(_ context.Context, completion fleet.Completion, _ fleet.AuditFact, _ fleet.EvidenceReader) error {
	repository.completion = completion
	return nil
}
func (repository *fleetServiceRepository) RegisterAgent(_ context.Context, registration registry.AgentRegistration, revision registry.AgentRevision, fact fleet.AuditFact) (bool, error) {
	repository.registerFact = fact
	if repository.registration.AgentID != "" {
		if repository.registration == registration && repository.agent.Digest == revision.Digest {
			return false, nil
		}
		return false, fleet.ErrConflict
	}
	repository.registration, repository.agent = registration, revision
	return true, nil
}
func (repository *fleetServiceRepository) GetAgentRegistration(context.Context, string) (registry.AgentRegistration, error) {
	if repository.registration.AgentID == "" {
		return registry.AgentRegistration{}, fleet.ErrNotFound
	}
	return repository.registration, repository.loadErr
}
func (repository *fleetServiceRepository) LatestAgentRevision(context.Context, string) (registry.AgentRevision, error) {
	return repository.agent, repository.loadErr
}
func (repository *fleetServiceRepository) PublishAgentRevision(_ context.Context, revision registry.AgentRevision, fact fleet.AuditFact) error {
	repository.agent = revision
	repository.revisionFact = fact
	return nil
}
func (repository *fleetServiceRepository) PublishLoop(_ context.Context, request loop.PublishRequest, _ fleet.AuditFact) (loop.PublicationDecision, error) {
	repository.loopPublished = true
	repository.loopPublication = request
	return loop.PublicationDecision{}, nil
}
func (repository *fleetServiceRepository) AppendLoopLifecycle(_ context.Context, request loop.LifecycleRequest, _ fleet.AuditFact) (loop.LifecycleEvent, bool, error) {
	repository.lifecycleEvent = request.Event
	repository.lifecycleCalls++
	return request.Event, false, nil
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

type sequencedAuthorityCommands struct {
	core.AuthorityCommandRepository
	authority core.AuthorityContext
	calls     int
	admit     int
}

func (commands *sequencedAuthorityCommands) AuthorityAdmission(_ context.Context, _, _ string, at time.Time) (core.AuthorityAdmissionView, error) {
	commands.calls++
	admitted := commands.calls <= commands.admit
	reason := "admitted"
	if !admitted {
		reason = "revoked"
	}
	return core.AuthorityAdmissionView{AuthorityContext: commands.authority, EvaluatedAt: at, Admitted: admitted, ReasonCode: reason}, nil
}

func TestFleetReadinessCoversEveryConsequentialAction(t *testing.T) {
	service, _, _, subject, authorityRef, _ := fleetServiceFixture(t)
	actions := []FleetAction{FleetActionRegister, FleetActionAgentRevision, FleetActionLoopValidate, FleetActionLoopPublish, FleetActionLoopLifecycle, FleetActionGraphValidate, FleetActionGraphPublish, FleetActionSubmission, FleetActionQueueAdmission, FleetActionClaim, FleetActionReclaim, FleetActionQueueLifecycle, FleetActionRuntimeEffect, FleetActionEvidenceVerify, FleetActionDisposition}
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

func TestPublishLoopBindsExactEnabledPublisherAndImmutableProvenance(t *testing.T) {
	service, repository, _, subject, authorityRef, _ := fleetServiceFixture(t)
	validation := loop.ValidateRevision(repository.loop)
	publication := loop.PublishRequest{Revision: repository.loop, Validation: validation}
	publisher := revisionRef(repository.agent.AgentID, repository.agent.Revision, repository.agent.Digest)

	wrong := publisher
	wrong.ID = "prompt-selected-agent"
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: wrong, Publication: publication}); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong publisher err=%v", err)
	}
	wrongTarget := publisher
	repository.agent.Runtime.Target = "substituted-target"
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: wrongTarget, Publication: publication}); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong publisher runtime target err=%v", err)
	}
	repository.agent.Runtime.Target = "local"
	repository.agent.Lifecycle = registry.LifecycleDisabled
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: publisher, Publication: publication}); !errors.Is(err, ErrDenied) {
		t.Fatalf("disabled publisher err=%v", err)
	}
	repository.agent.Lifecycle = registry.LifecycleEnabled
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: publisher, Publication: publication}); err != nil {
		t.Fatalf("publish Loop: %v", err)
	}
	provenance := repository.loopPublication.Provenance
	if provenance.PublisherAgent != provenanceRevision(publisher) || provenance.Authority != provenanceDigest(authorityRef) || provenance.Loop.Digest != repository.loop.Digest || provenance.ValidationDigest != validation.Digest || provenance.MandateID != "mandate-1" || provenance.Digest == "" {
		t.Fatalf("publication provenance=%+v", provenance)
	}
}

func TestSetLoopLifecycleRequiresExactEnabledPublisherAndRecordsAuthority(t *testing.T) {
	service, repository, _, subject, authorityRef, _ := fleetServiceFixture(t)
	publisher := revisionRef(repository.agent.AgentID, repository.agent.Revision, repository.agent.Digest)
	loopRef := revisionRef(repository.loop.LoopID, repository.loop.Revision, repository.loop.Digest)
	request := SetLoopLifecycleRequest{
		Subject: subject, Authority: authorityRef, Publisher: publisher, Loop: loopRef,
		State: loop.LifecycleActive, EventID: "activate-loop-1",
	}

	wrong := request
	wrong.Publisher.Digest = "sha256:" + strings.Repeat("f", 64)
	if _, _, err := service.SetLoopLifecycle(context.Background(), wrong); !errors.Is(err, ErrDenied) {
		t.Fatalf("substituted publisher err=%v", err)
	}
	if repository.lifecycleCalls != 0 {
		t.Fatal("denied lifecycle request reached persistence")
	}
	event, _, err := service.SetLoopLifecycle(context.Background(), request)
	if err != nil {
		t.Fatalf("activate Loop: %v", err)
	}
	if event.Revision != provenanceRevision(loopRef) || event.PublisherAgent != provenanceRevision(publisher) || event.Authority != provenanceDigest(authorityRef) || event.MandateID != "mandate-1" || event.StanzaID == "" || event.Digest == "" {
		t.Fatalf("lifecycle event=%+v", event)
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

	publisher := revisionRef(repository.agent.AgentID, repository.agent.Revision, repository.agent.Digest)
	validation := loop.ValidateRevision(repository.loop)
	publication := loop.PublishRequest{Revision: repository.loop, Validation: validation}
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: publisher, Publication: publication}); err != nil {
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

func TestGraphCompositionRequiresExactPinnedLoopInterface(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)

	mismatched := repository.graph
	mismatched.Nodes = append([]graph.Node(nil), mismatched.Nodes...)
	mismatched.Nodes[0].Inputs = []graph.Port{{ID: "prompt", Type: graph.TypeString, Required: true}}
	if _, err := service.PublishGraph(context.Background(), PublishGraphRequest{
		Subject: subject, Authority: authorityRef, Publication: graph.PublishRequest{Revision: mismatched},
	}); !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "node_loop_interface_mismatch") {
		t.Fatalf("Graph publication did not deny an interface assertion absent from the exact Loop: %v", err)
	}
	if repository.graphPublished {
		t.Fatal("interface-mismatched Graph reached the persistence boundary")
	}

	// Simulate corruption or substitution behind the repository boundary: the
	// exact digest reference still resolves, but the returned Loop interface no
	// longer matches the immutable Graph node. Submission must become a durable
	// rejection rather than a queue item.
	repository.loop.Inputs = []loop.Port{{ID: "prompt", Type: loop.TypeString, Required: true}}
	request := SubmitGraphRequest{
		Subject: subject, Authority: authorityRef, Graph: graphRef,
		SubmissionID: "submission-interface-mismatch", IdempotencyKey: "submit-interface-mismatch",
		SnapshotID: "snapshot-interface-mismatch", QueueItemID: "queue-interface-mismatch",
		GraphRunID: "run-interface-mismatch", TransitionID: "transition-interface-mismatch",
		RejectionID: "rejection-interface-mismatch", MaxAttempts: 1,
	}
	decision, err := service.PrepareGraphRun(context.Background(), request)
	if err != nil || decision.Accepted != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "loop_interface_mismatch" {
		t.Fatalf("submission interface mismatch was not durably rejected: decision=%+v err=%v", decision, err)
	}
	if repository.accepted != nil || repository.rejected == nil || repository.rejected.RejectionID != request.RejectionID {
		t.Fatalf("submission mismatch crossed acceptance boundary: accepted=%+v rejected=%+v", repository.accepted, repository.rejected)
	}
}

func TestFleetSubmissionDurablyRejectsInactiveExactLoop(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	repository.loopLifecycle = loop.Lifecycle{LoopID: repository.loop.LoopID, State: loop.LifecycleRetired}
	request := SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-inactive-loop", IdempotencyKey: "submit-inactive-loop", SnapshotID: "snapshot-inactive-loop", QueueItemID: "queue-inactive-loop", GraphRunID: "run-inactive-loop", TransitionID: "transition-inactive-loop", RejectionID: "rejection-inactive-loop", MaxAttempts: 1}
	decision, err := service.PrepareGraphRun(context.Background(), request)
	if err != nil || decision.Accepted != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "loop_inactive" {
		t.Fatalf("inactive exact Loop was not durably rejected: decision=%+v err=%v", decision, err)
	}
	if repository.accepted != nil || repository.rejected == nil {
		t.Fatalf("inactive Loop crossed Queue admission boundary: accepted=%+v rejected=%+v", repository.accepted, repository.rejected)
	}
}

func TestQueueWorkerRunsInjectedAdapterToDurableEvidenceDisposition(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-worker", IdempotencyKey: "submit-worker", SnapshotID: "snapshot-worker", QueueItemID: "queue-worker", GraphRunID: "run-worker", TransitionID: "queued-worker", RejectionID: "rejected-worker", MaxAttempts: 1})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewQueueWorker(repository, service, blobs, verifier, fixedRuntimeAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue-worker", WorkerID: "worker-1", LoopExecutionID: "loop-execution-worker", ClaimID: "claim-worker", AttemptID: "attempt-worker", ClaimTransitionID: "claimed-worker", TerminalTransitionID: "terminal-worker", DispositionID: "disposition-worker", ArtifactID: "artifact-worker", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition.State != execution.StateSucceeded || result.Artifact == nil || result.Artifact.ContentRef == "" || len(result.Receipts) != 1 || result.Receipts[0].Outcome != evidence.Passed || repository.completion.Disposition.Digest != result.Disposition.Digest {
		t.Fatalf("injected runtime execution did not reach evidence disposition: %+v completion=%+v", result, repository.completion)
	}
	if repository.claim.Authority != authorityRef || repository.attempt.LoopExecutionID != repository.loopExecution.LoopExecutionID {
		t.Fatalf("claim causality or authority was lost: claim=%+v attempt=%+v loop=%+v", repository.claim, repository.attempt, repository.loopExecution)
	}
}

func TestQueueWorkerBoundsAdapterByExactClaimLease(t *testing.T) {
	for _, test := range []struct {
		name    string
		lease   time.Duration
		allowed bool
	}{
		{"minimum", MinLeaseDuration, true},
		{"maximum", MaxLeaseDuration, true},
		{"below minimum", MinLeaseDuration - time.Nanosecond, false},
		{"above maximum", MaxLeaseDuration + time.Nanosecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
			_, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-lease", IdempotencyKey: "submit-lease", SnapshotID: "snapshot-lease", QueueItemID: "queue-lease", GraphRunID: "run-lease", TransitionID: "queued-lease", RejectionID: "rejected-lease", MaxAttempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			blobs, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			verifier, err := evidence.NewBlobVerifier(blobs)
			if err != nil {
				t.Fatal(err)
			}
			adapter := &deadlineRuntimeAdapter{}
			worker, err := NewQueueWorker(repository, service, blobs, verifier, adapter, service.now)
			if err != nil {
				t.Fatal(err)
			}
			_, err = worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue-lease", WorkerID: "worker-1", LoopExecutionID: "loop-execution-lease", ClaimID: "claim-lease", AttemptID: "attempt-lease", ClaimTransitionID: "claimed-lease", TerminalTransitionID: "terminal-lease", DispositionID: "disposition-lease", ArtifactID: "artifact-lease", LeaseDuration: test.lease})
			if !test.allowed {
				if !errors.Is(err, ErrWorkerDenied) || adapter.called {
					t.Fatalf("outside lease bound reached adapter: called=%v err=%v", adapter.called, err)
				}
				return
			}
			if err != nil || !adapter.called || !adapter.deadline.Equal(service.now().Add(test.lease)) {
				t.Fatalf("adapter deadline=%v want=%v called=%v err=%v", adapter.deadline, service.now().Add(test.lease), adapter.called, err)
			}
		})
	}
}

func TestQueueWorkerRepeatsAdmissionAndDurablyDeniesRevokedRuntimeEffect(t *testing.T) {
	service, repository, authority, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-revoked", IdempotencyKey: "submit-revoked", SnapshotID: "snapshot-revoked", QueueItemID: "queue-revoked", GraphRunID: "run-revoked", TransitionID: "queued-revoked", RejectionID: "rejected-revoked", MaxAttempts: 1})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	commands := &sequencedAuthorityCommands{authority: authority, admit: 1}
	service.authorityCommands = commands
	worker, err := NewQueueWorker(repository, service, blobs, verifier, NoKeyAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue-revoked", WorkerID: "worker-1", LoopExecutionID: "loop-execution-revoked", ClaimID: "claim-revoked", AttemptID: "attempt-revoked", ClaimTransitionID: "claimed-revoked", TerminalTransitionID: "terminal-revoked", DispositionID: "disposition-revoked", ArtifactID: "artifact-revoked", LeaseDuration: time.Minute})
	if !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("runtime revocation was not denied: result=%+v err=%v", result, err)
	}
	if commands.calls != 3 || result.Disposition.State != execution.StateDenied || result.Disposition.ReasonCode != "disposition_admission_denied" || result.Artifact != nil || len(result.Receipts) != 0 {
		t.Fatalf("runtime denial did not preserve fresh admission and empty evidence: calls=%d result=%+v", commands.calls, result)
	}
	if repository.completion.Disposition.Digest != result.Disposition.Digest || repository.completion.Transition.To != queue.StateDenied {
		t.Fatalf("runtime denial was not durably terminal: %+v", repository.completion)
	}
}

func TestQueueWorkerRejectsAuthorityDriftBeforeClaim(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-drift", IdempotencyKey: "submit-drift", SnapshotID: "snapshot-drift", QueueItemID: "queue-drift", GraphRunID: "run-drift", TransitionID: "queued-drift", RejectionID: "rejected-drift", MaxAttempts: 1})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewQueueWorker(repository, service, blobs, verifier, NoKeyAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	drifted := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authorityRef.ID, Digest: "sha256:" + strings.Repeat("c", 64)}
	_, err = worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: drifted, QueueItemID: "queue-drift", WorkerID: "worker-1", LoopExecutionID: "loop-execution-drift", ClaimID: "claim-drift", AttemptID: "attempt-drift", ClaimTransitionID: "claimed-drift", TerminalTransitionID: "terminal-drift", DispositionID: "disposition-drift", ArtifactID: "artifact-drift", LeaseDuration: time.Minute})
	if !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("authority drift was not denied: %v", err)
	}
	if repository.claim.ClaimID != "" || repository.loopExecution.LoopExecutionID != "" || repository.completion.Disposition.DispositionID != "" {
		t.Fatalf("authority drift produced lifecycle side effects: claim=%+v loop=%+v completion=%+v", repository.claim, repository.loopExecution, repository.completion)
	}
}

func TestQueueLifecycleRetryPreservesCausalityAndFailsClosed(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-retry", IdempotencyKey: "submit-retry", SnapshotID: "snapshot-retry", QueueItemID: "queue-retry", GraphRunID: "run-retry", TransitionID: "queued-retry", RejectionID: "rejected-retry", MaxAttempts: 3})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	item := repository.accepted.QueueItem
	repository.claim, err = queue.NewClaim(queue.Claim{ClaimID: "claim-retry", QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: "attempt-retry", WorkerID: "worker-1", Authority: authorityRef, ClaimedAt: service.now().Add(-time.Minute), ExpiresAt: service.now()})
	if err != nil {
		t.Fatal(err)
	}
	repository.projection = queue.Projection{QueueItemID: item.ItemID, State: queue.StateClaimed, Attempts: 1, ActiveClaimID: repository.claim.ClaimID}
	worker := newLifecycleTestWorker(t, repository, service)
	if _, err = worker.Retry(context.Background(), QueueRetryRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, RetryID: "retry-live", TransitionID: "retried-live"}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("ordinary live retry preempted active runtime: %v", err)
	}
	retry, err := worker.Retry(context.Background(), QueueRetryRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, RetryID: "retry-1", TransitionID: "retried-1", Backoff: time.Minute, Reclaimed: true, ReasonCode: ReasonLeaseReclaimed})
	if err != nil {
		t.Fatal(err)
	}
	if retry.QueueItem.ID != item.ItemID || retry.ClaimID != repository.claim.ClaimID || retry.AttemptNumber != 1 || !retry.AvailableAt.Equal(service.now().Add(time.Minute)) || repository.retryMutation.Transition.To != queue.StateQueued {
		t.Fatalf("retry lost immutable causality or bounded availability: retry=%+v mutation=%+v", retry, repository.retryMutation)
	}
	wrongAuthority := authorityRef
	wrongAuthority.Digest = "sha256:" + strings.Repeat("e", 64)
	if _, err = worker.Retry(context.Background(), QueueRetryRequest{Subject: subject, Authority: wrongAuthority, QueueItemID: item.ItemID, RetryID: "retry-denied", TransitionID: "retried-denied", Reclaimed: true}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("authority substitution was not denied: %v", err)
	}
	if _, err = worker.Retry(context.Background(), QueueRetryRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, RetryID: "retry-unbounded", TransitionID: "retried-unbounded", Backoff: MaxRetryBackoff + time.Nanosecond, Reclaimed: true}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("unbounded backoff was not denied: %v", err)
	}
	repository.projection.Attempts = item.MaxAttempts
	if _, err = worker.Retry(context.Background(), QueueRetryRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, RetryID: "retry-exhausted", TransitionID: "retried-exhausted", Reclaimed: true}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("retry beyond pinned budget was not denied: %v", err)
	}
}

func TestQueueLifecycleCancelledBeforeAdmissionDeniesAndTerminalReplayDenies(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-cancel", IdempotencyKey: "submit-cancel", SnapshotID: "snapshot-cancel", QueueItemID: "queue-cancel", GraphRunID: "run-cancel", TransitionID: "queued-cancel", RejectionID: "rejected-cancel", MaxAttempts: 2})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	item := repository.accepted.QueueItem
	repository.projection = queue.Projection{QueueItemID: item.ItemID, State: queue.StateQueued}
	worker := newLifecycleTestWorker(t, repository, service)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := worker.Cancel(cancelled, QueueTerminalRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, CancellationID: "cancel-before-admission", TransitionID: "cancelled-before-admission", ReasonCode: ReasonOperatorCancelled}); !errors.Is(err, context.Canceled) {
		t.Fatalf("request cancelled before admission gained authority: %v", err)
	}
	if repository.cancelMutation.Cancellation.CancellationID != "" {
		t.Fatal("pre-admission cancellation committed a lifecycle mutation")
	}
	record, err := worker.Cancel(context.Background(), QueueTerminalRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, CancellationID: "cancel-1", TransitionID: "cancelled-1", ReasonCode: ReasonOperatorCancelled})
	if err != nil || record.QueueItem.ID != item.ItemID || repository.cancelMutation.Transition.To != queue.StateCancelled {
		t.Fatalf("admitted cancellation did not commit: record=%+v mutation=%+v err=%v", record, repository.cancelMutation, err)
	}
	repository.projection.State = queue.StateCancelled
	if _, err = worker.Cancel(context.Background(), QueueTerminalRequest{Subject: subject, Authority: authorityRef, QueueItemID: item.ItemID, CancellationID: "cancel-2", TransitionID: "cancelled-2"}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("terminal replay was not denied: %v", err)
	}
}

func newLifecycleTestWorker(t *testing.T, repository fleet.Repository, service *FleetService) *QueueWorker {
	t.Helper()
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewQueueWorker(repository, service, blobs, verifier, NoKeyAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	return worker
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

func TestRegisterFleetAgentCannotClaimBuiltInAegisAgentID(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	before := repository.registration
	source := staticFleetSource{{AgentID: registry.BuiltInAegisAgentID, Source: registry.FleetSource{Kind: registry.CurrentFleetSourceKind, FleetID: "fleet-1", SourceID: "generic-aegis"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}, Ownership: registry.Ownership{OwnerID: "owner-1", AccountabilityID: "accountability-1"}, Lifecycle: registry.LifecycleEnabled, Charter: revisionReference(registry.BuiltInAegisAgentID, 1, "a")}}
	if _, _, _, err := service.RegisterFleetAgent(context.Background(), RegisterFleetAgentRequest{Subject: subject, Source: source, Identity: source[0].Source}); !errors.Is(err, registry.ErrBuiltInImmutable) {
		t.Fatalf("generic orchestration registration occupied reserved Agent ID: %v", err)
	}
	if repository.registration != before {
		t.Fatalf("denied generic registration mutated repository: before=%+v after=%+v", before, repository.registration)
	}
}

func TestSetAgentLifecycleAppendsExactAuthorizedRevisionAndFailsClosed(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	sealed, err := registry.SealRevision(registry.AgentRevision{
		SchemaVersion: registry.AgentRevisionSchemaVersion,
		AgentID:       "agent-1", Revision: 1,
		Source:                 registry.FleetSource{Kind: registry.CurrentFleetSourceKind, FleetID: "fleet-1", SourceID: "profile-1"},
		Runtime:                registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"},
		Ownership:              registry.Ownership{OwnerID: "owner-1", AccountabilityID: "accountability-1"},
		Lifecycle:              registry.LifecycleEnabled,
		Charter:                revisionReference("agent-1", 1, "a"),
		CapabilityDeclarations: []string{"fleet.execute"},
	})
	if err != nil {
		t.Fatal(err)
	}
	repository.agent = sealed
	expected := revisionRef(sealed.AgentID, sealed.Revision, sealed.Digest)

	next, err := service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: expected, Lifecycle: registry.LifecycleDisabled})
	if err != nil || next.Revision != 2 || next.Lifecycle != registry.LifecycleDisabled || next.Digest == sealed.Digest {
		t.Fatalf("lifecycle append: next=%+v err=%v", next, err)
	}
	if repository.revisionFact.Event.Type != "fleet.agent.lifecycle.changed" || repository.revisionFact.Event.SubjectID != subject.ID {
		t.Fatalf("lifecycle audit was not authoritative and subject-bound: %+v", repository.revisionFact)
	}

	if _, err = service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: expected, Lifecycle: registry.LifecycleEnabled}); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("stale expected revision was not denied: %v", err)
	}
	denied := subject
	denied.PrincipalID = "prompt-selected"
	current := revisionRef(next.AgentID, next.Revision, next.Digest)
	if _, err = service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: denied, Agent: current, Lifecycle: registry.LifecycleEnabled}); !errors.Is(err, ErrDenied) {
		t.Fatalf("unauthorized lifecycle mutation was not denied: %v", err)
	}
	if repository.agent.Digest != next.Digest {
		t.Fatalf("denied lifecycle request mutated current revision: %+v", repository.agent)
	}
	idempotent, err := service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: current, Lifecycle: registry.LifecycleDisabled})
	if err != nil || idempotent.Digest != next.Digest || repository.agent.Digest != next.Digest {
		t.Fatalf("exact lifecycle replay was not idempotent: revision=%+v audit=%+v err=%v", idempotent, repository.revisionFact, err)
	}
	retired, err := service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: current, Lifecycle: registry.LifecycleRetired})
	if err != nil || retired.Revision != 3 || retired.Lifecycle != registry.LifecycleRetired {
		t.Fatalf("retirement append: revision=%+v err=%v", retired, err)
	}
	retiredRef := revisionRef(retired.AgentID, retired.Revision, retired.Digest)
	if _, err = service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: retiredRef, Lifecycle: registry.LifecycleEnabled}); !errors.Is(err, registry.ErrRetired) {
		t.Fatalf("retired Agent was reactivated: %v", err)
	}
	if repository.agent.Digest != retired.Digest {
		t.Fatalf("failed reactivation mutated retired revision: %+v", repository.agent)
	}
}

func TestRegisterBuiltInAegisAgentIsAuthorizedIdempotentAndExactReadBack(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	repository.registration = registry.AgentRegistration{}
	repository.agent = registry.AgentRevision{}

	registration, revision, created, err := service.RegisterBuiltInAegisAgent(context.Background(), subject)
	if err != nil || !created {
		t.Fatalf("first registration: created=%t registration=%+v revision=%+v err=%v", created, registration, revision, err)
	}
	if registration.AgentID != "aegis" || revision.AgentID != "aegis" || revision.Ownership.AccountabilityID != subject.PrincipalID || repository.registerFact.Event.PrincipalID != subject.PrincipalID {
		t.Fatalf("canonical registration or authenticated audit fact missing: registration=%+v revision=%+v fact=%+v", registration, revision, repository.registerFact)
	}
	replayedRegistration, replayedRevision, replayCreated, err := service.RegisterBuiltInAegisAgent(context.Background(), subject)
	if err != nil || replayCreated || replayedRegistration != registration || replayedRevision.Digest != revision.Digest {
		t.Fatalf("idempotent replay: created=%t registration=%+v revision=%+v err=%v", replayCreated, replayedRegistration, replayedRevision, err)
	}
	corrupt := revision
	corrupt.Ownership.OwnerID = "tampered-owner"
	repository.agent = corrupt
	if _, _, _, err = service.RegisterBuiltInAegisAgent(context.Background(), subject); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("same-digest non-canonical revision readback was accepted: %v", err)
	}
	repository.agent = revision
	conflict := registration
	conflict.Source.SourceID = "conflict"
	repository.registration = conflict
	if _, _, _, err = service.RegisterBuiltInAegisAgent(context.Background(), subject); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("conflicting canonical identity was not denied: %v", err)
	}
	unauthorized := subject
	unauthorized.PrincipalID = "other"
	if _, _, _, err = service.RegisterBuiltInAegisAgent(context.Background(), unauthorized); !errors.Is(err, ErrDenied) {
		t.Fatalf("unauthorized registration was not denied: %v", err)
	}
}

func TestBuiltInAegisAgentRejectsGenericLifecycleRevision(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	repository.registration = registry.AgentRegistration{}
	repository.agent = registry.AgentRevision{}
	_, builtIn, _, err := service.RegisterBuiltInAegisAgent(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	builtInRef := revisionRef(builtIn.AgentID, builtIn.Revision, builtIn.Digest)
	if _, err = service.SetAgentLifecycle(context.Background(), SetAgentLifecycleRequest{Subject: subject, Agent: builtInRef, Lifecycle: registry.LifecycleDisabled}); !errors.Is(err, registry.ErrBuiltInImmutable) {
		t.Fatalf("built-in Agent lifecycle mutation was not denied: %v", err)
	}
	if repository.agent.Digest != builtIn.Digest {
		t.Fatalf("denied built-in lifecycle mutation changed repository: %+v", repository.agent)
	}
}

func fleetServiceFixture(t *testing.T) (*FleetService, *fleetServiceRepository, core.AuthorityContext, core.Subject, reference.DigestRef, reference.RevisionRef) {
	t.Helper()
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	subject := core.Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	runtime := core.RuntimeDescriptor{Name: "hermes", Runtime: "hermes-agent", Version: "0.18.2", Executable: "/usr/bin/hermes", Installation: "system", AdapterVersion: "1"}
	mandate := core.Mandate{ID: "mandate-1", Subject: subject, AgentID: "agent-1", StanzaID: "operator", CharterRevision: 1, CharterDigest: "sha256:" + strings.Repeat("a", 64), Runtime: runtime, Target: "local", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	authority := core.AuthorityContext{ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: subject.ID, AgentID: mandate.AgentID, CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID}, IssuedAt: mandate.IssuedAt, ExpiresAt: mandate.ExpiresAt}
	authority.Digest = core.AuthorityContextDigest(authority)

	loopRevision, loopValidation, err := loop.NewRevision(loop.LoopRevision{LoopID: "loop-1", Revision: 1, EntryStepID: "work", Steps: []loop.Step{{ID: "work", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "exact-output", MediaType: "application/json", ExpectedDigest: "sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945", VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1}}}, {ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}}, Transitions: []loop.Transition{{ID: "finish", FromStepID: "work", ToStepID: "done"}}, RequiredEvidence: []loop.EvidenceRequirement{{Claim: "exact-output", ProducerStepID: "work"}}})
	if err != nil {
		t.Fatalf("%v: %+v", err, loopValidation.Issues)
	}
	agent := registry.AgentRevision{AgentID: "agent-1", Revision: 1, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}, Ownership: registry.Ownership{OwnerID: "owner-1", AccountabilityID: "accountability-1"}, Lifecycle: registry.LifecycleEnabled, Charter: revisionRef("agent-1", mandate.CharterRevision, mandate.CharterDigest), Digest: "sha256:" + strings.Repeat("b", 64)}
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

type fixedRuntimeAdapter struct{}

func (fixedRuntimeAdapter) Execute(context.Context, RuntimeRequest) (RuntimeResult, error) {
	return RuntimeResult{Output: []byte("[]"), MediaType: "application/json"}, nil
}

type deadlineRuntimeAdapter struct {
	called   bool
	deadline time.Time
}

func (adapter *deadlineRuntimeAdapter) Execute(ctx context.Context, _ RuntimeRequest) (RuntimeResult, error) {
	adapter.called = true
	adapter.deadline, _ = ctx.Deadline()
	return RuntimeResult{Output: []byte("[]"), MediaType: "application/json"}, nil
}

func repairStrings(values []RepairAction) []string {
	out := make([]string, len(values))
	for index := range values {
		out[index] = string(values[index])
	}
	return out
}
