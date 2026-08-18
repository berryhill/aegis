package app

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

var ErrFleetUnavailable = errors.New("fleet control service is unavailable")

// Fleet error classifiers keep transport adapters dependent on the application
// boundary rather than concrete orchestration or persistence packages.
func IsFleetDenied(err error) bool {
	return errors.Is(err, orchestration.ErrDenied) || errors.Is(err, orchestration.ErrWorkerDenied)
}

func IsFleetUnavailable(err error) bool {
	return errors.Is(err, ErrFleetUnavailable) || errors.Is(err, fleet.ErrClosed)
}

func IsFleetConflict(err error) bool {
	return errors.Is(err, fleet.ErrConflict) || errors.Is(err, registry.ErrConflict) || errors.Is(err, registry.ErrRetired)
}
func IsFleetNotFound(err error) bool {
	return errors.Is(err, fleet.ErrNotFound) || errors.Is(err, registry.ErrNotFound)
}
func IsFleetCorrupt(err error) bool   { return errors.Is(err, fleet.ErrCorrupt) }
func IsFleetAmbiguous(err error) bool { return errors.Is(err, registry.ErrAmbiguousSource) }

// SubmitGraphInput and ProcessQueueItemInput are application-owned transport
// contracts. Aliases preserve the canonical domain wire form without asking
// CLI or HTTP adapters to import the orchestration layer directly.
type SubmitGraphInput = orchestration.SubmitGraphRequest
type ProcessQueueItemInput = orchestration.WorkRequest

// QueueItemView keeps the immutable admitted item separate from its
// rebuildable lifecycle projection. Consumers must not mistake the initial
// item state for the current queue state.
type QueueItemView struct {
	Item       queue.Item       `json:"item"`
	Projection queue.Projection `json:"projection"`
}

// FleetAgent is the product read model for one registered executable participant.
type FleetAgent struct {
	Registration registry.AgentRegistration `json:"registration"`
	Revision     registry.AgentRevision     `json:"revision"`
}

// RegisterFleetAgentInput is shared by the CLI and HTTP adapters. Fixture is a
// deterministic current-fleet proposal; Identity selects exactly one candidate.
type RegisterFleetAgentInput struct {
	Fixture  json.RawMessage      `json:"fixture"`
	Identity registry.FleetSource `json:"identity"`
}

type SetAgentLifecycleInput struct {
	Expected  reference.RevisionRef `json:"expected"`
	Lifecycle registry.Lifecycle    `json:"lifecycle"`
}

type PublishLoopInput struct {
	Authority              reference.DigestRef   `json:"authority"`
	Publisher              reference.RevisionRef `json:"publisher"`
	Revision               loop.LoopRevision     `json:"revision"`
	ExpectedPreviousDigest string                `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string                `json:"idempotency_key"`
}

type PublishedLoop struct {
	Revision   loop.LoopRevision         `json:"revision"`
	Validation loop.LoopValidationResult `json:"validation"`
	Decision   loop.PublicationDecision  `json:"decision"`
}

type SetLoopLifecycleInput struct {
	Authority              reference.DigestRef   `json:"authority"`
	Publisher              reference.RevisionRef `json:"publisher"`
	Loop                   reference.RevisionRef `json:"loop"`
	State                  loop.LifecycleState   `json:"state"`
	EventID                string                `json:"event_id"`
	ExpectedPreviousDigest string                `json:"expected_previous_digest,omitempty"`
}

type LoopLifecycleResult struct {
	Event      loop.LifecycleEvent `json:"event"`
	Idempotent bool                `json:"idempotent"`
}

type PublishGraphInput struct {
	Authority              reference.DigestRef `json:"authority"`
	Revision               graph.GraphRevision `json:"revision"`
	ExpectedPreviousDigest string              `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string              `json:"idempotency_key"`
}

type PublishedGraph struct {
	Revision   graph.GraphRevision         `json:"revision"`
	Validation graph.GraphValidationResult `json:"validation"`
	Decision   graph.PublicationDecision   `json:"decision"`
}

type LoopView struct {
	Revision    loop.LoopRevision           `json:"revision"`
	Validations []loop.LoopValidationResult `json:"validations"`
	Provenance  loop.PublicationProvenance  `json:"provenance"`
	Lifecycle   loop.Lifecycle              `json:"lifecycle"`
	History     []loop.LifecycleEvent       `json:"lifecycle_history"`
}

type GraphView struct {
	Revision    graph.GraphRevision           `json:"revision"`
	Validations []graph.GraphValidationResult `json:"validations"`
	Lifecycle   graph.Lifecycle               `json:"lifecycle"`
	Runs        []AcceptedGraphRunView        `json:"accepted_runs"`
}

// AcceptedGraphRunView is the immutable accepted-submission read contract. It
// cannot authorize work; runtime effects still require fresh admission.
type AcceptedGraphRunView struct {
	Snapshot   graph.GraphRunSnapshot `json:"snapshot"`
	Submission queue.Submission       `json:"submission"`
	QueueItem  queue.Item             `json:"queue_item"`
	GraphRun   execution.GraphRun     `json:"graph_run"`
}

type SubmissionHistory struct {
	Accepted []AcceptedGraphRunView `json:"accepted"`
	Rejected []queue.Rejection      `json:"rejected"`
}

type QueueExecutionView struct {
	Item           queue.Item                     `json:"item"`
	Projection     queue.Projection               `json:"projection"`
	GraphRun       execution.GraphRun             `json:"graph_run"`
	LoopExecutions []execution.LoopExecution      `json:"loop_executions"`
	Attempts       []execution.Attempt            `json:"attempts"`
	Runtime        registry.RuntimeBinding        `json:"runtime"`
	Artifact       *evidence.RuntimeArtifact      `json:"artifact,omitempty"`
	Receipts       []evidence.VerificationReceipt `json:"receipts"`
	Disposition    *disposition.Record            `json:"disposition,omitempty"`
}

type SurfaceReadiness struct {
	State         string `json:"state"`
	ReasonCode    string `json:"reason_code"`
	Source        string `json:"source"`
	Count         int    `json:"count"`
	Authoritative bool   `json:"authoritative"`
}

// CredentialView is metadata-only. Workspace reads never resolve secrets.
type CredentialView struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type FleetSurface struct {
	Agents      []FleetAgent                       `json:"agents"`
	Loops       []LoopView                         `json:"loops"`
	Graphs      []GraphView                        `json:"graphs"`
	Submissions SubmissionHistory                  `json:"submissions"`
	Queue       []QueueExecutionView               `json:"queue"`
	Credentials []CredentialView                   `json:"credentials"`
	Readiness   map[string]SurfaceReadiness        `json:"readiness"`
	Actions     map[string]orchestration.Readiness `json:"actions"`
}

// ConfigureFleet installs the single application boundary used by all fleet
// transports. Identity and authority repositories remain controller-owned.
func (s *Service) ConfigureFleet(repository fleet.Repository, service *orchestration.FleetService, worker *orchestration.QueueWorker) error {
	if repository == nil || service == nil || worker == nil {
		return ErrFleetUnavailable
	}
	s.FleetRepository, s.Fleet, s.QueueWorker = repository, service, worker
	return nil
}

func (s *Service) requireFleetPrincipal(subject core.Subject) error {
	if err := s.requirePrincipal(subject); err != nil {
		return err
	}
	if s.FleetRepository == nil || s.Fleet == nil || s.QueueWorker == nil {
		return ErrFleetUnavailable
	}
	return nil
}

// FleetAuthorityForSession returns the exact immutable reference needed by
// fleet mutations. It discovers authority only from a controller-created
// session binding; callers cannot select a stanza or construct authority here.
func (s *Service) FleetAuthorityForSessionAs(ctx context.Context, subject core.Subject, sessionID string) (reference.DigestRef, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return reference.DigestRef{}, err
	}
	authority, err := s.authorityContextForSession(ctx, sessionID)
	if err != nil {
		return reference.DigestRef{}, err
	}
	if authority.SubjectID != subject.ID {
		return reference.DigestRef{}, ErrDenied
	}
	mandate, err := s.Authority.GetMandate(ctx, authority.MandateID)
	if err != nil || core.ValidateAuthorityContext(authority, mandate) != nil {
		return reference.DigestRef{}, ErrDenied
	}
	admission, err := s.AuthorityCommands.AuthorityAdmission(ctx, authority.ID, authority.Digest, s.Now())
	if err != nil {
		return reference.DigestRef{}, err
	}
	if !admission.Admitted || admission.AuthorityContext.ID != authority.ID || admission.AuthorityContext.Digest != authority.Digest {
		return reference.DigestRef{}, ErrDenied
	}
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}, nil
}

func (s *Service) FleetAuthorityForSession(ctx context.Context, sessionID string) (reference.DigestRef, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return reference.DigestRef{}, err
	}
	return s.FleetAuthorityForSessionAs(ctx, subject, sessionID)
}

func (s *Service) RegisterFleetAgent(ctx context.Context, input RegisterFleetAgentInput) (FleetAgent, bool, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return FleetAgent{}, false, err
	}
	return s.RegisterFleetAgentAs(ctx, subject, input)
}

func (s *Service) RegisterFleetAgentAs(ctx context.Context, subject core.Subject, input RegisterFleetAgentInput) (FleetAgent, bool, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return FleetAgent{}, false, err
	}
	source, err := registry.NewCurrentFleetFixtureSource(input.Fixture)
	if err != nil {
		return FleetAgent{}, false, err
	}
	registration, revision, created, err := s.Fleet.RegisterFleetAgent(ctx, orchestration.RegisterFleetAgentRequest{Subject: subject, Source: source, Identity: input.Identity})
	return FleetAgent{Registration: registration, Revision: revision}, created, err
}

func (s *Service) ListFleetAgentsAs(ctx context.Context, subject core.Subject) ([]FleetAgent, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return nil, err
	}
	registrations, err := s.FleetRepository.ListAgentRegistrations(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]FleetAgent, 0, len(registrations))
	for _, registration := range registrations {
		revision, loadErr := s.FleetRepository.LatestAgentRevision(ctx, registration.AgentID)
		if loadErr != nil {
			return nil, loadErr
		}
		result = append(result, FleetAgent{Registration: registration, Revision: revision})
	}
	return result, nil
}

func (s *Service) ListFleetAgents(ctx context.Context) ([]FleetAgent, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListFleetAgentsAs(ctx, subject)
}

func (s *Service) GetFleetAgentAs(ctx context.Context, subject core.Subject, id string, revision uint64) (FleetAgent, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return FleetAgent{}, err
	}
	registration, err := s.FleetRepository.GetAgentRegistration(ctx, id)
	if err != nil {
		return FleetAgent{}, err
	}
	var value registry.AgentRevision
	if revision == 0 {
		value, err = s.FleetRepository.LatestAgentRevision(ctx, id)
	} else {
		value, err = s.FleetRepository.GetAgentRevision(ctx, id, revision)
	}
	return FleetAgent{Registration: registration, Revision: value}, err
}

func (s *Service) GetFleetAgent(ctx context.Context, id string, revision uint64) (FleetAgent, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return FleetAgent{}, err
	}
	return s.GetFleetAgentAs(ctx, subject, id, revision)
}

func (s *Service) ListFleetAgentRevisionsAs(ctx context.Context, subject core.Subject, id string) ([]registry.AgentRevision, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return nil, err
	}
	return s.FleetRepository.ListAgentRevisions(ctx, id)
}

func (s *Service) ListFleetAgentRevisions(ctx context.Context, id string) ([]registry.AgentRevision, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListFleetAgentRevisionsAs(ctx, subject, id)
}

func (s *Service) SetAgentLifecycleAs(ctx context.Context, subject core.Subject, id string, input SetAgentLifecycleInput) (FleetAgent, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return FleetAgent{}, err
	}
	if input.Expected.ID != id {
		return FleetAgent{}, fleet.ErrConflict
	}
	revision, err := s.Fleet.SetAgentLifecycle(ctx, orchestration.SetAgentLifecycleRequest{Subject: subject, Agent: input.Expected, Lifecycle: input.Lifecycle})
	if err != nil {
		return FleetAgent{}, err
	}
	registration, err := s.FleetRepository.GetAgentRegistration(ctx, id)
	return FleetAgent{Registration: registration, Revision: revision}, err
}

func (s *Service) SetAgentLifecycle(ctx context.Context, id string, input SetAgentLifecycleInput) (FleetAgent, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return FleetAgent{}, err
	}
	return s.SetAgentLifecycleAs(ctx, subject, id, input)
}

func (s *Service) PublishLoopAs(ctx context.Context, subject core.Subject, input PublishLoopInput) (PublishedLoop, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return PublishedLoop{}, err
	}
	revision, validation, err := loop.NewRevision(input.Revision)
	if err != nil {
		return PublishedLoop{Revision: revision, Validation: validation}, err
	}
	request := loop.PublishRequest{Revision: revision, Validation: validation, ExpectedPreviousDigest: input.ExpectedPreviousDigest, IdempotencyKey: input.IdempotencyKey}
	decision, err := s.Fleet.PublishLoop(ctx, orchestration.PublishLoopRequest{Subject: subject, Authority: input.Authority, Publisher: input.Publisher, Publication: request})
	return PublishedLoop{Revision: revision, Validation: validation, Decision: decision}, err
}

func (s *Service) PublishLoop(ctx context.Context, input PublishLoopInput) (PublishedLoop, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return PublishedLoop{}, err
	}
	return s.PublishLoopAs(ctx, subject, input)
}

func (s *Service) SetLoopLifecycleAs(ctx context.Context, subject core.Subject, id string, input SetLoopLifecycleInput) (LoopLifecycleResult, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return LoopLifecycleResult{}, err
	}
	if input.Loop.ID != id {
		return LoopLifecycleResult{}, fleet.ErrConflict
	}
	event, idempotent, err := s.Fleet.SetLoopLifecycle(ctx, orchestration.SetLoopLifecycleRequest{
		Subject: subject, Authority: input.Authority, Publisher: input.Publisher, Loop: input.Loop,
		State: input.State, EventID: input.EventID, ExpectedPreviousDigest: input.ExpectedPreviousDigest,
	})
	return LoopLifecycleResult{Event: event, Idempotent: idempotent}, err
}

func (s *Service) SetLoopLifecycle(ctx context.Context, id string, input SetLoopLifecycleInput) (LoopLifecycleResult, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return LoopLifecycleResult{}, err
	}
	return s.SetLoopLifecycleAs(ctx, subject, id, input)
}

func (s *Service) GetLoopAs(ctx context.Context, subject core.Subject, id string, revision uint64) (loop.LoopRevision, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return loop.LoopRevision{}, err
	}
	if revision == 0 {
		return loop.LoopRevision{}, errors.New("exact Loop revision is required")
	}
	return s.FleetRepository.GetLoopRevision(ctx, id, revision)
}

func (s *Service) GetLoop(ctx context.Context, id string, revision uint64) (loop.LoopRevision, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return loop.LoopRevision{}, err
	}
	return s.GetLoopAs(ctx, subject, id, revision)
}

func (s *Service) ListLoopsAs(ctx context.Context, subject core.Subject) ([]LoopView, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return nil, err
	}
	revisions, err := s.FleetRepository.ListLoopRevisions(ctx)
	if err != nil {
		return nil, err
	}
	validations, err := s.FleetRepository.ListLoopValidations(ctx)
	if err != nil {
		return nil, err
	}
	provenance, err := s.FleetRepository.ListLoopPublicationProvenance(ctx)
	if err != nil {
		return nil, err
	}
	history, err := s.FleetRepository.ListLoopLifecycleEvents(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]LoopView, 0, len(revisions))
	for _, revision := range revisions {
		view := LoopView{Revision: revision, Validations: []loop.LoopValidationResult{}, History: []loop.LifecycleEvent{}, Lifecycle: loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleDraft}}
		for _, record := range provenance {
			if record.Loop.ID == revision.LoopID && record.Loop.Revision == revision.Revision {
				if record.Loop.Digest != revision.Digest {
					return nil, fleet.ErrCorrupt
				}
				view.Provenance = record
			}
		}
		if view.Provenance.Digest == "" {
			return nil, fleet.ErrCorrupt
		}
		previous := ""
		for _, event := range history {
			if event.LoopID != revision.LoopID {
				continue
			}
			if event.PreviousDigest != previous {
				return nil, fleet.ErrCorrupt
			}
			view.History = append(view.History, event)
			previous = event.Digest
			if event.State == loop.LifecycleActive {
				view.Lifecycle = loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleActive, ActiveRevision: event.Revision.Revision, ActiveDigest: event.Revision.Digest}
			} else {
				view.Lifecycle = loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleRetired}
			}
		}
		for _, validation := range validations {
			if validation.LoopID == revision.LoopID && validation.Revision == revision.Revision {
				if validation.RevisionDigest != revision.Digest {
					return nil, fleet.ErrCorrupt
				}
				view.Validations = append(view.Validations, validation)
			}
		}
		if len(view.Validations) == 0 {
			return nil, fleet.ErrCorrupt
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Service) ListLoops(ctx context.Context) ([]LoopView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListLoopsAs(ctx, subject)
}

func (s *Service) GetLoopViewAs(ctx context.Context, subject core.Subject, id string, revision uint64) (LoopView, error) {
	if revision == 0 {
		return LoopView{}, errors.New("exact Loop revision is required")
	}
	values, err := s.ListLoopsAs(ctx, subject)
	if err != nil {
		return LoopView{}, err
	}
	for _, value := range values {
		if value.Revision.LoopID == id && value.Revision.Revision == revision {
			return value, nil
		}
	}
	return LoopView{}, fleet.ErrNotFound
}

func (s *Service) GetLoopView(ctx context.Context, id string, revision uint64) (LoopView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return LoopView{}, err
	}
	return s.GetLoopViewAs(ctx, subject, id, revision)
}

func (s *Service) PublishGraphAs(ctx context.Context, subject core.Subject, input PublishGraphInput) (PublishedGraph, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return PublishedGraph{}, err
	}
	revision, validation, err := graph.NewRevision(input.Revision)
	if err != nil {
		return PublishedGraph{Revision: revision, Validation: validation}, err
	}
	request := graph.PublishRequest{Revision: revision, Validation: validation, ExpectedPreviousDigest: input.ExpectedPreviousDigest, IdempotencyKey: input.IdempotencyKey}
	decision, err := s.Fleet.PublishGraph(ctx, orchestration.PublishGraphRequest{Subject: subject, Authority: input.Authority, Publication: request})
	return PublishedGraph{Revision: revision, Validation: validation, Decision: decision}, err
}

func (s *Service) PublishGraph(ctx context.Context, input PublishGraphInput) (PublishedGraph, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return PublishedGraph{}, err
	}
	return s.PublishGraphAs(ctx, subject, input)
}

func (s *Service) GetGraphAs(ctx context.Context, subject core.Subject, id string, revision uint64) (graph.GraphRevision, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return graph.GraphRevision{}, err
	}
	if revision == 0 {
		return graph.GraphRevision{}, errors.New("exact Graph revision is required")
	}
	return s.FleetRepository.GetGraphRevision(ctx, id, revision)
}

func (s *Service) GetGraph(ctx context.Context, id string, revision uint64) (graph.GraphRevision, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return graph.GraphRevision{}, err
	}
	return s.GetGraphAs(ctx, subject, id, revision)
}

func (s *Service) ListGraphsAs(ctx context.Context, subject core.Subject) ([]GraphView, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return nil, err
	}
	revisions, err := s.FleetRepository.ListGraphRevisions(ctx)
	if err != nil {
		return nil, err
	}
	validations, err := s.FleetRepository.ListGraphValidations(ctx)
	if err != nil {
		return nil, err
	}
	lifecycles, err := s.FleetRepository.ListGraphLifecycles(ctx)
	if err != nil {
		return nil, err
	}
	history, err := s.ListSubmissionHistoryAs(ctx, subject)
	if err != nil {
		return nil, err
	}
	result := make([]GraphView, 0, len(revisions))
	for _, revision := range revisions {
		view := GraphView{Revision: revision, Validations: []graph.GraphValidationResult{}, Runs: []AcceptedGraphRunView{}}
		for _, validation := range validations {
			if validation.GraphID == revision.GraphID && validation.Revision == revision.Revision {
				if validation.RevisionDigest != revision.Digest {
					return nil, fleet.ErrCorrupt
				}
				view.Validations = append(view.Validations, validation)
			}
		}
		if len(view.Validations) == 0 {
			return nil, fleet.ErrCorrupt
		}
		for _, lifecycle := range lifecycles {
			if lifecycle.GraphID == revision.GraphID {
				view.Lifecycle = lifecycle
			}
		}
		if view.Lifecycle.GraphID == "" {
			return nil, fleet.ErrCorrupt
		}
		for _, accepted := range history.Accepted {
			if accepted.Snapshot.Graph.ID == revision.GraphID && accepted.Snapshot.Graph.Revision == revision.Revision && accepted.Snapshot.Graph.Digest == revision.Digest {
				view.Runs = append(view.Runs, accepted)
			}
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Service) ListGraphs(ctx context.Context) ([]GraphView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListGraphsAs(ctx, subject)
}

func (s *Service) GetGraphLifecycleAs(ctx context.Context, subject core.Subject, id string) (graph.Lifecycle, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return graph.Lifecycle{}, err
	}
	return s.FleetRepository.GetGraphLifecycle(ctx, id)
}

func (s *Service) ListSubmissionHistoryAs(ctx context.Context, subject core.Subject) (SubmissionHistory, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return SubmissionHistory{}, err
	}
	submissions, err := s.FleetRepository.ListSubmissions(ctx)
	if err != nil {
		return SubmissionHistory{}, err
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return SubmissionHistory{}, err
	}
	runs, err := s.FleetRepository.ListGraphRuns(ctx)
	if err != nil {
		return SubmissionHistory{}, err
	}
	result := SubmissionHistory{Accepted: []AcceptedGraphRunView{}, Rejected: []queue.Rejection{}}
	for _, submission := range submissions {
		snapshot, loadErr := s.FleetRepository.GetGraphRunSnapshot(ctx, submission.Snapshot.ID)
		if loadErr != nil || snapshot.Digest != submission.Snapshot.Digest {
			return SubmissionHistory{}, fleet.ErrCorrupt
		}
		view := AcceptedGraphRunView{Snapshot: snapshot, Submission: submission}
		foundItem, foundRun := false, false
		for _, item := range items {
			if item.Submission.ID == submission.SubmissionID && item.Submission.Digest == submission.Digest {
				view.QueueItem, foundItem = item, true
				for _, run := range runs {
					if run.GraphRunID == item.GraphRunID && run.QueueItem.ID == item.ItemID && run.QueueItem.Digest == item.Digest {
						view.GraphRun, foundRun = run, true
					}
				}
			}
		}
		if !foundItem || !foundRun {
			return SubmissionHistory{}, fleet.ErrCorrupt
		}
		result.Accepted = append(result.Accepted, view)
	}
	result.Rejected, err = s.FleetRepository.ListRejections(ctx)
	return result, err
}

func (s *Service) SubmitGraphAs(ctx context.Context, subject core.Subject, request orchestration.SubmitGraphRequest) (orchestration.SubmissionDecision, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return orchestration.SubmissionDecision{}, err
	}
	request.Subject = subject
	return s.Fleet.PrepareGraphRun(ctx, request)
}

func (s *Service) SubmitGraph(ctx context.Context, request orchestration.SubmitGraphRequest) (orchestration.SubmissionDecision, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return orchestration.SubmissionDecision{}, err
	}
	return s.SubmitGraphAs(ctx, subject, request)
}

func (s *Service) GetQueueItemAs(ctx context.Context, subject core.Subject, id string) (QueueExecutionView, error) {
	views, err := s.ListQueueAs(ctx, subject)
	if err != nil {
		return QueueExecutionView{}, err
	}
	for _, view := range views {
		if view.Item.ItemID == id {
			return view, nil
		}
	}
	return QueueExecutionView{}, fleet.ErrNotFound
}

func (s *Service) GetQueueItem(ctx context.Context, id string) (QueueExecutionView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return QueueExecutionView{}, err
	}
	return s.GetQueueItemAs(ctx, subject, id)
}

func (s *Service) ListQueueAs(ctx context.Context, subject core.Subject) ([]QueueExecutionView, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return nil, err
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil {
		return nil, err
	}
	runs, err := s.FleetRepository.ListGraphRuns(ctx)
	if err != nil {
		return nil, err
	}
	loops, err := s.FleetRepository.ListLoopExecutions(ctx)
	if err != nil {
		return nil, err
	}
	attempts, err := s.FleetRepository.ListAttempts(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]QueueExecutionView, 0, len(items))
	for _, item := range items {
		projection, loadErr := s.FleetRepository.GetQueueProjection(ctx, item.ItemID)
		if loadErr != nil {
			return nil, loadErr
		}
		if projection.QueueItemID != item.ItemID {
			return nil, fleet.ErrCorrupt
		}
		view := QueueExecutionView{Item: item, Projection: projection, LoopExecutions: []execution.LoopExecution{}, Attempts: []execution.Attempt{}, Receipts: []evidence.VerificationReceipt{}}
		snapshot, loadErr := s.FleetRepository.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
		if loadErr != nil || snapshot.Digest != item.Snapshot.Digest {
			return nil, fleet.ErrCorrupt
		}
		graphRevision, loadErr := s.FleetRepository.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if loadErr != nil || graphRevision.Digest != snapshot.Graph.Digest || len(graphRevision.Nodes) != 1 {
			return nil, fleet.ErrCorrupt
		}
		participantRef := graphRevision.Nodes[0].Participant
		participant, loadErr := s.FleetRepository.GetAgentRevision(ctx, participantRef.ID, participantRef.Revision)
		if loadErr != nil || participant.Digest != participantRef.Digest {
			return nil, fleet.ErrCorrupt
		}
		view.Runtime = participant.Runtime
		foundRun := false
		for _, run := range runs {
			if run.GraphRunID == item.GraphRunID {
				if run.QueueItem.ID != item.ItemID || run.QueueItem.Digest != item.Digest {
					return nil, fleet.ErrCorrupt
				}
				view.GraphRun, foundRun = run, true
			}
		}
		if !foundRun {
			return nil, fleet.ErrCorrupt
		}
		for _, child := range loops {
			if child.GraphRunID == item.GraphRunID {
				view.LoopExecutions = append(view.LoopExecutions, child)
			}
		}
		for _, attempt := range attempts {
			if attempt.GraphRunID == item.GraphRunID {
				if attempt.QueueItem.ID != item.ItemID || attempt.QueueItem.Digest != item.Digest {
					return nil, fleet.ErrCorrupt
				}
				view.Attempts = append(view.Attempts, attempt)
			}
		}
		if projection.State != queue.StateQueued && projection.State != queue.StateClaimed {
			dispositionRecord, loadErr := s.FleetRepository.GetDispositionByGraphRun(ctx, item.GraphRunID)
			if loadErr != nil || dispositionRecord.QueueItem.ID != item.ItemID || dispositionRecord.QueueItem.Digest != item.Digest {
				return nil, fleet.ErrCorrupt
			}
			view.Disposition = &dispositionRecord
			if len(dispositionRecord.ArtifactIDs) == 1 {
				artifact, artifactErr := s.FleetRepository.GetRuntimeArtifact(ctx, dispositionRecord.ArtifactIDs[0])
				if artifactErr != nil {
					return nil, fleet.ErrCorrupt
				}
				view.Artifact = &artifact
			} else if len(dispositionRecord.ArtifactIDs) != 0 {
				return nil, fleet.ErrCorrupt
			}
			for _, receiptID := range dispositionRecord.ReceiptIDs {
				receipt, receiptErr := s.FleetRepository.GetVerificationReceipt(ctx, receiptID)
				if receiptErr != nil || view.Artifact == nil || receipt.ArtifactID != view.Artifact.ID {
					return nil, fleet.ErrCorrupt
				}
				view.Receipts = append(view.Receipts, receipt)
			}
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Service) ListQueue(ctx context.Context) ([]QueueExecutionView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.ListQueueAs(ctx, subject)
}

func (s *Service) FleetSurfaceAs(ctx context.Context, subject core.Subject) (FleetSurface, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return FleetSurface{}, err
	}
	surface := FleetSurface{
		Agents: []FleetAgent{}, Loops: []LoopView{}, Graphs: []GraphView{}, Queue: []QueueExecutionView{}, Credentials: []CredentialView{},
		Submissions: SubmissionHistory{Accepted: []AcceptedGraphRunView{}, Rejected: []queue.Rejection{}},
		Readiness:   map[string]SurfaceReadiness{}, Actions: map[string]orchestration.Readiness{},
	}
	if s.FleetRepository == nil || s.Fleet == nil || s.QueueWorker == nil {
		for key, source := range map[string]string{"registry": "fleet.agent_registrations", "loops": "fleet.loop_revisions", "graphs": "fleet.graph_revisions", "queue": "fleet.queue_items"} {
			surface.Readiness[key] = SurfaceReadiness{State: "unavailable", ReasonCode: "fleet_service_unavailable", Source: source}
		}
	} else {
		var err error
		surface.Agents, err = s.ListFleetAgentsAs(ctx, subject)
		if ctx.Err() != nil {
			return FleetSurface{}, ctx.Err()
		}
		surface.Readiness["registry"] = collectionReadiness(len(surface.Agents), "fleet.agent_registrations", err)
		surface.Loops, err = s.ListLoopsAs(ctx, subject)
		if ctx.Err() != nil {
			return FleetSurface{}, ctx.Err()
		}
		surface.Readiness["loops"] = collectionReadiness(len(surface.Loops), "fleet.loop_revisions", err)
		surface.Graphs, err = s.ListGraphsAs(ctx, subject)
		if ctx.Err() != nil {
			return FleetSurface{}, ctx.Err()
		}
		surface.Readiness["graphs"] = collectionReadiness(len(surface.Graphs), "fleet.graph_revisions", err)
		if err == nil {
			surface.Submissions, err = s.ListSubmissionHistoryAs(ctx, subject)
			if ctx.Err() != nil {
				return FleetSurface{}, ctx.Err()
			}
		}
		surface.Queue, err = s.ListQueueAs(ctx, subject)
		if ctx.Err() != nil {
			return FleetSurface{}, ctx.Err()
		}
		surface.Readiness["queue"] = collectionReadiness(len(surface.Queue), "fleet.queue_items", err)
		for _, action := range []orchestration.FleetAction{
			orchestration.FleetActionRegister, orchestration.FleetActionLoopPublish, orchestration.FleetActionGraphPublish,
			orchestration.FleetActionSubmission, orchestration.FleetActionClaim, orchestration.FleetActionRuntimeEffect,
			orchestration.FleetActionEvidenceVerify, orchestration.FleetActionDisposition,
		} {
			surface.Actions[string(action)] = s.Fleet.Readiness(ctx, orchestration.ReadinessRequest{Action: action, Subject: subject})
		}
	}
	credentialIDs := make([]string, 0, len(s.Config.Credentials.ProviderAuth))
	for id := range s.Config.Credentials.ProviderAuth {
		credentialIDs = append(credentialIDs, id)
	}
	sort.Strings(credentialIDs)
	for _, id := range credentialIDs {
		binding := s.Config.Credentials.ProviderAuth[id]
		surface.Credentials = append(surface.Credentials, CredentialView{ID: id, Type: binding.Type})
	}
	surface.Readiness["credentials"] = collectionReadiness(len(surface.Credentials), "config.credentials.provider_auth", nil)
	return surface, nil
}

func collectionReadiness(count int, source string, err error) SurfaceReadiness {
	result := SurfaceReadiness{State: "ready", ReasonCode: "collection_read_succeeded", Source: source, Count: count, Authoritative: true}
	if err == nil && count == 0 {
		result.State, result.ReasonCode = "empty", "collection_read_succeeded_empty"
		return result
	}
	if err == nil {
		return result
	}
	result.Count, result.Authoritative = 0, false
	switch {
	case IsFleetDenied(err), errors.Is(err, ErrDenied), errors.Is(err, ErrUnauthenticated):
		result.State, result.ReasonCode = "denied", "collection_read_denied"
	case IsFleetCorrupt(err):
		result.State, result.ReasonCode = "degraded_repair_required", "fleet_store_corrupt"
	case IsFleetUnavailable(err):
		result.State, result.ReasonCode = "unavailable", "collection_read_unavailable"
	default:
		result.State, result.ReasonCode = "error", "collection_read_failed"
	}
	return result
}

func (s *Service) ProcessQueueItemAs(ctx context.Context, subject core.Subject, request orchestration.WorkRequest) (orchestration.WorkResult, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return orchestration.WorkResult{}, err
	}
	request.Subject = subject
	if request.LeaseDuration <= 0 {
		request.LeaseDuration = time.Minute
	}
	return s.QueueWorker.Process(ctx, request)
}

func (s *Service) ProcessQueueItem(ctx context.Context, request orchestration.WorkRequest) (orchestration.WorkResult, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return orchestration.WorkResult{}, err
	}
	return s.ProcessQueueItemAs(ctx, subject, request)
}
