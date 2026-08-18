package orchestration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

var ErrDenied = errors.New("fleet operation denied")

// FleetAction is a closed set because readiness is an authorization decision,
// not a caller-defined label. Adding an action requires defining its exact
// prerequisites and denial behavior below.
type FleetAction string

const (
	FleetActionRegister       FleetAction = "register_fleet_agent"
	FleetActionAgentRevision  FleetAction = "agent_revision"
	FleetActionLoopValidate   FleetAction = "loop_validate"
	FleetActionLoopPublish    FleetAction = "loop_publish"
	FleetActionGraphValidate  FleetAction = "graph_validate"
	FleetActionGraphPublish   FleetAction = "graph_publish"
	FleetActionSubmission     FleetAction = "submission"
	FleetActionQueueAdmission FleetAction = "queue_admission"
	FleetActionClaim          FleetAction = "queue_claim"
	FleetActionReclaim        FleetAction = "queue_reclaim"
	FleetActionRuntimeEffect  FleetAction = "runtime_effect"
	FleetActionEvidenceVerify FleetAction = "evidence_verify"
	FleetActionDisposition    FleetAction = "disposition"
)

type ReadinessState string

const (
	ReadinessReady       ReadinessState = "ready"
	ReadinessDenied      ReadinessState = "denied"
	ReadinessUnavailable ReadinessState = "unavailable"
	ReadinessRepair      ReadinessState = "degraded_repair_required"
	ReadinessEmpty       ReadinessState = "empty"
)

type RepairAction string

const (
	RepairAuthenticate      RepairAction = "authenticate_principal"
	RepairSelectAuthority   RepairAction = "select_exact_authority_context"
	RepairRegisterAgent     RepairAction = "register_fleet_agent"
	RepairEnableAgent       RepairAction = "enable_agent_revision"
	RepairPublishLoop       RepairAction = "publish_exact_loop_revision"
	RepairPublishGraph      RepairAction = "publish_exact_graph_revision"
	RepairRecoverFleetStore RepairAction = "recover_fleet_store"
	RepairRestoreRuntime    RepairAction = "restore_runtime_adapter"
)

type Readiness struct {
	Action        FleetAction    `json:"action"`
	State         ReadinessState `json:"state"`
	ReasonCode    string         `json:"reason_code"`
	RepairActions []RepairAction `json:"repair_actions,omitempty"`
}

type ReadinessRequest struct {
	Action    FleetAction
	Subject   core.Subject
	Authority reference.DigestRef
	Agent     reference.RevisionRef
	Loop      reference.RevisionRef
	Graph     reference.RevisionRef
}

// FleetRepository is the bounded cross-domain persistence port used by the
// orchestration service. Its records remain owned by their domain packages.
type FleetRepository interface {
	RegisterAgent(context.Context, registry.AgentRegistration, registry.AgentRevision, fleet.AuditFact) (bool, error)
	PublishAgentRevision(context.Context, registry.AgentRevision, fleet.AuditFact) error
	PublishLoop(context.Context, loop.PublishRequest, fleet.AuditFact) (loop.PublicationDecision, error)
	PublishGraph(context.Context, graph.PublishRequest, fleet.AuditFact) (graph.PublicationDecision, error)
	GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error)
	LatestAgentRevision(context.Context, string) (registry.AgentRevision, error)
	GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error)
	GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error)
	AcceptSubmission(context.Context, fleet.AcceptedSubmission, fleet.AuditFact) (bool, error)
	RejectSubmission(context.Context, queue.Rejection, fleet.AuditFact) (bool, error)
}

type SubjectAuthorizer func(context.Context, FleetAction, core.Subject) error

type DispositionRequest struct {
	Subject    core.Subject
	Authority  reference.DigestRef
	GraphRunID string
	State      execution.State
	ReasonCode string
	OccurredAt time.Time
}

type DispositionSink interface {
	RecordDisposition(context.Context, DispositionRequest, fleet.AuditFact) error
}

type FleetService struct {
	repository        FleetRepository
	authority         core.AuthorityRepository
	authorityCommands core.AuthorityCommandRepository
	authorize         SubjectAuthorizer
	dispositions      DispositionSink
	now               func() time.Time
}

func NewFleetService(repository FleetRepository, authority core.AuthorityRepository, commands core.AuthorityCommandRepository, authorize SubjectAuthorizer, dispositions DispositionSink, now func() time.Time) (*FleetService, error) {
	if repository == nil || authority == nil || commands == nil || authorize == nil {
		return nil, errors.New("fleet repository, authority repositories, and subject authorizer are required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &FleetService{repository: repository, authority: authority, authorityCommands: commands, authorize: authorize, dispositions: dispositions, now: now}, nil
}

func (service *FleetService) Readiness(ctx context.Context, request ReadinessRequest) Readiness {
	if !knownFleetAction(request.Action) {
		return readiness(request.Action, ReadinessDenied, "unknown_action")
	}
	if err := service.authorize(ctx, request.Action, request.Subject); err != nil {
		return readinessWithRepair(request.Action, ReadinessDenied, "subject_not_authorized", RepairAuthenticate)
	}
	if request.Action == FleetActionRegister {
		return readiness(request.Action, ReadinessReady, "ready")
	}
	if request.Authority.ID == "" || request.Authority.Digest == "" {
		return readinessWithRepair(request.Action, ReadinessDenied, "authority_context_required", RepairSelectAuthority)
	}
	if _, _, result := service.resolveAuthority(ctx, request.Subject, request.Authority); result.State != ReadinessReady {
		result.Action = request.Action
		return result
	}

	if request.Agent.ID != "" {
		agent, err := service.repository.GetAgentRevision(ctx, request.Agent.ID, request.Agent.Revision)
		if result := definitionReadiness(request.Action, err, RepairRegisterAgent); result.State != ReadinessReady {
			return result
		} else if agent.Digest != request.Agent.Digest {
			return readiness(request.Action, ReadinessDenied, "agent_revision_mismatch")
		} else if agent.Lifecycle != registry.LifecycleEnabled {
			return readinessWithRepair(request.Action, ReadinessDenied, "agent_not_enabled", RepairEnableAgent)
		}
	}
	if request.Loop.ID != "" {
		value, err := service.repository.GetLoopRevision(ctx, request.Loop.ID, request.Loop.Revision)
		if result := definitionReadiness(request.Action, err, RepairPublishLoop); result.State != ReadinessReady {
			return result
		} else if value.Digest != request.Loop.Digest {
			return readiness(request.Action, ReadinessDenied, "loop_revision_mismatch")
		}
	}
	if request.Graph.ID != "" {
		value, err := service.repository.GetGraphRevision(ctx, request.Graph.ID, request.Graph.Revision)
		if result := definitionReadiness(request.Action, err, RepairPublishGraph); result.State != ReadinessReady {
			return result
		} else if value.Digest != request.Graph.Digest {
			return readiness(request.Action, ReadinessDenied, "graph_revision_mismatch")
		}
	}
	return readiness(request.Action, ReadinessReady, "ready")
}

func knownFleetAction(action FleetAction) bool {
	switch action {
	case FleetActionRegister, FleetActionAgentRevision, FleetActionLoopValidate, FleetActionLoopPublish,
		FleetActionGraphValidate, FleetActionGraphPublish, FleetActionSubmission, FleetActionQueueAdmission,
		FleetActionClaim, FleetActionReclaim, FleetActionRuntimeEffect, FleetActionEvidenceVerify, FleetActionDisposition:
		return true
	default:
		return false
	}
}

func readiness(action FleetAction, state ReadinessState, reason string) Readiness {
	return Readiness{Action: action, State: state, ReasonCode: reason}
}

func readinessWithRepair(action FleetAction, state ReadinessState, reason string, repair RepairAction) Readiness {
	return Readiness{Action: action, State: state, ReasonCode: reason, RepairActions: []RepairAction{repair}}
}

func definitionReadiness(action FleetAction, err error, repair RepairAction) Readiness {
	switch {
	case err == nil:
		return readiness(action, ReadinessReady, "ready")
	case errors.Is(err, fleet.ErrNotFound):
		return readinessWithRepair(action, ReadinessEmpty, "definition_not_found", repair)
	case errors.Is(err, fleet.ErrCorrupt):
		return readinessWithRepair(action, ReadinessRepair, "fleet_store_corrupt", RepairRecoverFleetStore)
	default:
		return readiness(action, ReadinessUnavailable, "fleet_store_unavailable")
	}
}

func (service *FleetService) resolveAuthority(ctx context.Context, subject core.Subject, ref reference.DigestRef) (core.AuthorityContext, core.Mandate, Readiness) {
	if err := ref.Validate(); err != nil {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessDenied, "authority_reference_invalid")
	}
	authority, err := service.authority.GetAuthorityContext(ctx, ref.ID)
	if err != nil {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessUnavailable, "authority_store_unavailable")
	}
	if authority.Digest != ref.Digest {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessDenied, "authority_context_mismatch")
	}
	mandate, err := service.authority.GetMandate(ctx, authority.MandateID)
	if err != nil || core.ValidateAuthorityContext(authority, mandate) != nil {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessDenied, "authority_binding_invalid")
	}
	if subject.ID == "" || subject.ID != mandate.Subject.ID || subject.PrincipalID != mandate.Subject.PrincipalID || service.now().Before(subject.AuthenticatedAt) || !service.now().Before(subject.ExpiresAt) {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessDenied, "authenticated_subject_mismatch")
	}
	checkedAt := service.now()
	admission, err := service.authorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, checkedAt)
	if err != nil {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessUnavailable, "authority_admission_unavailable")
	}
	if !admission.Admitted || admission.EvaluatedAt != checkedAt || admission.AuthorityContext.ID != authority.ID || admission.AuthorityContext.Digest != authority.Digest || admission.AuthorityContext.Authority.StanzaID != mandate.StanzaID {
		return core.AuthorityContext{}, core.Mandate{}, readiness(FleetActionSubmission, ReadinessDenied, "authority_inactive_or_inconsistent")
	}
	return authority, mandate, readiness(FleetActionSubmission, ReadinessReady, "ready")
}

type RegisterFleetAgentRequest struct {
	Subject  core.Subject         `json:"-"`
	Source   registry.Source      `json:"-"`
	Identity registry.FleetSource `json:"identity"`
}

func (service *FleetService) RegisterFleetAgent(ctx context.Context, request RegisterFleetAgentRequest) (registry.AgentRegistration, registry.AgentRevision, bool, error) {
	if result := service.Readiness(ctx, ReadinessRequest{Action: FleetActionRegister, Subject: request.Subject}); result.State != ReadinessReady {
		return registry.AgentRegistration{}, registry.AgentRevision{}, false, fmt.Errorf("%w: %s", ErrDenied, result.ReasonCode)
	}
	if request.Source == nil || request.Identity.Validate() != nil {
		return registry.AgentRegistration{}, registry.AgentRevision{}, false, errors.New("valid exact fleet source is required")
	}
	candidates, err := request.Source.Discover(ctx)
	if err != nil {
		return registry.AgentRegistration{}, registry.AgentRevision{}, false, err
	}
	var selected *registry.Candidate
	for index := range candidates {
		if candidates[index].Source == request.Identity {
			if selected != nil {
				return registry.AgentRegistration{}, registry.AgentRevision{}, false, registry.ErrAmbiguousSource
			}
			candidate := candidates[index]
			selected = &candidate
		}
	}
	if selected == nil {
		return registry.AgentRegistration{}, registry.AgentRevision{}, false, registry.ErrNotFound
	}
	revision, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: selected.AgentID, Revision: 1, Source: selected.Source, Runtime: selected.Runtime, Ownership: selected.Ownership, Lifecycle: selected.Lifecycle, Charter: selected.Charter, CapabilityDeclarations: append([]string(nil), selected.CapabilityDeclarations...), PolicyRefs: append([]reference.DigestRef(nil), selected.PolicyRefs...)})
	if err != nil {
		return registry.AgentRegistration{}, registry.AgentRevision{}, false, err
	}
	registration := registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: revision.AgentID, Source: revision.Source, InitialRevision: revisionRef(revision.AgentID, revision.Revision, revision.Digest)}
	created, err := service.repository.RegisterAgent(ctx, registration, revision, service.auditFact("fleet.agent.registered", request.Subject, "registered exact fleet source", revision.AgentID, "", ""))
	return registration, revision, created, err
}

type SetAgentLifecycleRequest struct {
	Subject   core.Subject
	Agent     reference.RevisionRef
	Lifecycle registry.Lifecycle
}

// SetAgentLifecycle appends a revision after authorizing the authenticated
// principal and matching the exact current revision. It never rewrites history.
func (service *FleetService) SetAgentLifecycle(ctx context.Context, request SetAgentLifecycleRequest) (registry.AgentRevision, error) {
	if err := service.authorize(ctx, FleetActionAgentRevision, request.Subject); err != nil {
		return registry.AgentRevision{}, fmt.Errorf("%w: subject_not_authorized", ErrDenied)
	}
	if err := request.Agent.Validate(); err != nil {
		return registry.AgentRevision{}, errors.New("valid exact agent revision is required")
	}
	current, err := service.repository.LatestAgentRevision(ctx, request.Agent.ID)
	if err != nil {
		return registry.AgentRevision{}, err
	}
	if current.Revision != request.Agent.Revision || current.Digest != request.Agent.Digest {
		return registry.AgentRevision{}, fleet.ErrConflict
	}
	switch request.Lifecycle {
	case registry.LifecycleEnabled, registry.LifecycleDisabled, registry.LifecycleRetired:
	default:
		return registry.AgentRevision{}, errors.New("lifecycle must be enabled, disabled, or retired")
	}
	if current.Lifecycle == request.Lifecycle {
		return current, nil
	}
	if current.Lifecycle == registry.LifecycleRetired {
		return registry.AgentRevision{}, registry.ErrRetired
	}
	next := current
	next.Revision++
	next.Lifecycle = request.Lifecycle
	next, err = registry.SealRevision(next)
	if err != nil {
		return registry.AgentRevision{}, err
	}
	err = service.repository.PublishAgentRevision(ctx, next, service.auditFact("fleet.agent.lifecycle.changed", request.Subject, "appended Agent lifecycle revision", next.AgentID, "", ""))
	return next, err
}

type PublishLoopRequest struct {
	Subject     core.Subject        `json:"-"`
	Authority   reference.DigestRef `json:"authority"`
	Publication loop.PublishRequest `json:"publication"`
}

func (service *FleetService) PublishLoop(ctx context.Context, request PublishLoopRequest) (loop.PublicationDecision, error) {
	if err := service.authorize(ctx, FleetActionLoopPublish, request.Subject); err != nil {
		return loop.PublicationDecision{}, fmt.Errorf("%w: subject_not_authorized", ErrDenied)
	}
	authority, _, result := service.resolveAuthority(ctx, request.Subject, request.Authority)
	if result.State != ReadinessReady {
		return loop.PublicationDecision{}, fmt.Errorf("%w: %s", ErrDenied, result.ReasonCode)
	}
	return service.repository.PublishLoop(ctx, request.Publication, service.auditFact("fleet.loop.published", request.Subject, "published exact validated Loop revision", authority.AgentID, authority.Authority.StanzaID, authority.MandateID))
}

type PublishGraphRequest struct {
	Subject     core.Subject         `json:"-"`
	Authority   reference.DigestRef  `json:"authority"`
	Publication graph.PublishRequest `json:"publication"`
}

func (service *FleetService) PublishGraph(ctx context.Context, request PublishGraphRequest) (graph.PublicationDecision, error) {
	if err := service.authorize(ctx, FleetActionGraphPublish, request.Subject); err != nil {
		return graph.PublicationDecision{}, fmt.Errorf("%w: subject_not_authorized", ErrDenied)
	}
	authority, _, result := service.resolveAuthority(ctx, request.Subject, request.Authority)
	if result.State != ReadinessReady {
		return graph.PublicationDecision{}, fmt.Errorf("%w: %s", ErrDenied, result.ReasonCode)
	}
	ownsNode := false
	for _, node := range request.Publication.Revision.Nodes {
		participant, err := service.repository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
		if err != nil || participant.Digest != node.Participant.Digest || participant.Lifecycle != registry.LifecycleEnabled {
			return graph.PublicationDecision{}, fmt.Errorf("%w: exact enabled participant required", ErrDenied)
		}
		loopRevision, err := service.repository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
		if err != nil || loopRevision.Digest != node.Loop.Digest {
			return graph.PublicationDecision{}, fmt.Errorf("%w: exact Loop revision required", ErrDenied)
		}
		if node.Participant.ID == authority.AgentID {
			if !agentMatchesAuthority(participant, authority) {
				return graph.PublicationDecision{}, fmt.Errorf("%w: participant charter or runtime does not match authority", ErrDenied)
			}
			ownsNode = true
		}
	}
	if !ownsNode {
		return graph.PublicationDecision{}, fmt.Errorf("%w: publishing Agent must be an exact Graph participant", ErrDenied)
	}
	if !policiesDeclaredByParticipants(request.Publication.Revision, service.repository, ctx) {
		return graph.PublicationDecision{}, fmt.Errorf("%w: Graph policy is not declared by an exact participant", ErrDenied)
	}
	return service.repository.PublishGraph(ctx, request.Publication, service.auditFact("fleet.graph.published", request.Subject, "published exact validated Graph revision", authority.AgentID, authority.Authority.StanzaID, authority.MandateID))
}

type SubmitGraphRequest struct {
	Subject        core.Subject            `json:"-"`
	Authority      reference.DigestRef     `json:"authority"`
	Graph          reference.RevisionRef   `json:"graph"`
	Inputs         []graph.NormalizedInput `json:"inputs"`
	SubmissionID   string                  `json:"submission_id"`
	IdempotencyKey string                  `json:"idempotency_key"`
	SnapshotID     string                  `json:"snapshot_id"`
	QueueItemID    string                  `json:"queue_item_id"`
	GraphRunID     string                  `json:"graph_run_id"`
	TransitionID   string                  `json:"transition_id"`
	RejectionID    string                  `json:"rejection_id"`
	MaxAttempts    uint32                  `json:"max_attempts"`
}

type SubmissionDecision struct {
	Accepted  *fleet.AcceptedSubmission `json:"accepted,omitempty"`
	Rejection *queue.Rejection          `json:"rejection,omitempty"`
	Created   bool                      `json:"created"`
}

func (service *FleetService) PrepareGraphRun(ctx context.Context, request SubmitGraphRequest) (SubmissionDecision, error) {
	deny := func(code, reason string) (SubmissionDecision, error) {
		rejection, err := queue.NewRejection(queue.Rejection{RejectionID: request.RejectionID, SubmissionID: request.SubmissionID, IdempotencyKey: request.IdempotencyKey, ReasonCode: code, Reason: reason, RejectedAt: service.now()})
		if err != nil {
			return SubmissionDecision{}, err
		}
		created, err := service.repository.RejectSubmission(ctx, rejection, service.auditFact("fleet.submission.rejected", request.Subject, reason, "", "", ""))
		return SubmissionDecision{Rejection: &rejection, Created: created}, err
	}
	result := service.Readiness(ctx, ReadinessRequest{Action: FleetActionSubmission, Subject: request.Subject, Authority: request.Authority, Graph: request.Graph})
	if result.State != ReadinessReady {
		return deny("readiness_denied", "submission readiness denied")
	}
	authority, mandate, result := service.resolveAuthority(ctx, request.Subject, request.Authority)
	if result.State != ReadinessReady {
		return deny("authority_denied", "exact authority context denied")
	}
	revision, err := service.repository.GetGraphRevision(ctx, request.Graph.ID, request.Graph.Revision)
	if err != nil || revision.Digest != request.Graph.Digest {
		return deny("graph_mismatch", "exact Graph revision unavailable")
	}
	if !graphContainsAgent(revision, authority.AgentID) {
		return deny("participant_rebinding", "authority Agent is not bound by the Graph")
	}
	snapshot, err := graph.NewRunSnapshot(request.SnapshotID, revision, request.Inputs)
	if err != nil {
		return deny("invalid_inputs", "typed Graph inputs are invalid")
	}
	for _, participant := range snapshot.Participants {
		value, loadErr := service.repository.GetAgentRevision(ctx, participant.ID, participant.Revision)
		if loadErr != nil || value.Digest != participant.Digest || value.Lifecycle != registry.LifecycleEnabled {
			return deny("participant_unavailable", "exact enabled participant is unavailable")
		}
		if participant.ID == authority.AgentID && !agentMatchesAuthority(value, authority) {
			return deny("authority_participant_mismatch", "authority charter or runtime does not match the exact participant")
		}
	}
	for _, loopRef := range snapshot.Loops {
		value, loadErr := service.repository.GetLoopRevision(ctx, loopRef.ID, loopRef.Revision)
		if loadErr != nil || value.Digest != loopRef.Digest {
			return deny("loop_unavailable", "exact Loop revision is unavailable")
		}
	}
	now := service.now()
	authorityRef := digestRef(authority.ID, authority.Digest)
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: request.SubmissionID, IdempotencyKey: request.IdempotencyKey, Snapshot: digestRef(snapshot.SnapshotID, snapshot.Digest), Authority: authorityRef, MandateID: mandate.ID, Runtime: runtimeID(authority.Runtime), SubmittedAt: now})
	if err != nil {
		return deny("invalid_submission", "submission envelope is invalid")
	}
	item, err := queue.NewItem(queue.Item{ItemID: request.QueueItemID, Submission: digestRef(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: authorityRef, GraphRunID: request.GraphRunID, MaxAttempts: request.MaxAttempts, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		return SubmissionDecision{}, err
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: request.GraphRunID, QueueItem: digestRef(item.ItemID, item.Digest), Snapshot: submission.Snapshot, Authority: authorityRef, CreatedAt: now})
	if err != nil {
		return SubmissionDecision{}, err
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: request.TransitionID, QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "submission accepted", OccurredAt: now})
	if err != nil {
		return SubmissionDecision{}, err
	}
	accepted := fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}
	created, err := service.repository.AcceptSubmission(ctx, accepted, service.auditFact("fleet.submission.admitted", request.Subject, "submission admitted under exact authority", authority.AgentID, authority.Authority.StanzaID, authority.MandateID))
	if err != nil {
		return SubmissionDecision{}, err
	}
	return SubmissionDecision{Accepted: &accepted, Created: created}, nil
}

func (service *FleetService) RecordDisposition(ctx context.Context, request DispositionRequest) error {
	if service.dispositions == nil {
		return errors.New("disposition sink is unavailable")
	}
	if request.State == execution.StateSucceeded {
		if result := service.Readiness(ctx, ReadinessRequest{Action: FleetActionDisposition, Subject: request.Subject, Authority: request.Authority}); result.State != ReadinessReady {
			return fmt.Errorf("%w: %s", ErrDenied, result.ReasonCode)
		}
	}
	if request.State != execution.StateSucceeded && request.State != execution.StateFailed && request.State != execution.StateDenied && request.State != execution.StateCancelled && request.State != execution.StateExpired {
		return errors.New("terminal disposition state is required")
	}
	return service.dispositions.RecordDisposition(ctx, request, service.auditFact("fleet.disposition.recorded", request.Subject, request.ReasonCode, "", "", ""))
}

func (service *FleetService) auditFact(eventType string, subject core.Subject, reason, agentID, stanzaID, mandateID string) fleet.AuditFact {
	outcome := "ok"
	if eventType == "fleet.submission.rejected" {
		outcome = "denied"
	}
	return fleet.AuditFact{Event: core.AuditEvent{Type: eventType, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: agentID, StanzaID: stanzaID, MandateID: mandateID, Outcome: outcome, Reason: reason}}
}

func agentMatchesAuthority(agent registry.AgentRevision, authority core.AuthorityContext) bool {
	return agent.AgentID == authority.AgentID && agent.Charter.Revision == authority.CharterRevision &&
		agent.Charter.Digest == authority.CharterDigest && agent.Runtime.Runtime == runtimeID(authority.Runtime)
}

func policiesDeclaredByParticipants(revision graph.GraphRevision, repository FleetRepository, ctx context.Context) bool {
	declared := make(map[reference.DigestRef]struct{})
	for _, node := range revision.Nodes {
		agent, err := repository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
		if err != nil || agent.Digest != node.Participant.Digest {
			return false
		}
		for _, policy := range agent.PolicyRefs {
			declared[policy] = struct{}{}
		}
	}
	for _, rule := range revision.AdmissionRules {
		if _, ok := declared[rule.PolicyRef]; !ok {
			return false
		}
	}
	return true
}

func graphContainsAgent(revision graph.GraphRevision, agentID string) bool {
	for _, node := range revision.Nodes {
		if node.Participant.ID == agentID {
			return true
		}
	}
	return false
}

func runtimeID(value core.RuntimeDescriptor) string {
	if value.Runtime != "" {
		return value.Runtime
	}
	return value.Name
}

func revisionRef(id string, revision uint64, digest string) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: digest}
}

func digestRef(id, digest string) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
}
