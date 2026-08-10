package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/core"
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

func IsFleetConflict(err error) bool { return errors.Is(err, fleet.ErrConflict) }
func IsFleetNotFound(err error) bool { return errors.Is(err, fleet.ErrNotFound) }
func IsFleetCorrupt(err error) bool  { return errors.Is(err, fleet.ErrCorrupt) }

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

type PublishLoopInput struct {
	Authority              reference.DigestRef `json:"authority"`
	Revision               loop.LoopRevision   `json:"revision"`
	ExpectedPreviousDigest string              `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string              `json:"idempotency_key"`
}

type PublishedLoop struct {
	Revision   loop.LoopRevision         `json:"revision"`
	Validation loop.LoopValidationResult `json:"validation"`
	Decision   loop.PublicationDecision  `json:"decision"`
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
}

type GraphView struct {
	Revision    graph.GraphRevision           `json:"revision"`
	Validations []graph.GraphValidationResult `json:"validations"`
}

type QueueExecutionView struct {
	Item           queue.Item                `json:"item"`
	Projection     queue.Projection          `json:"projection"`
	GraphRun       execution.GraphRun        `json:"graph_run"`
	LoopExecutions []execution.LoopExecution `json:"loop_executions"`
	Attempts       []execution.Attempt       `json:"attempts"`
}

type SurfaceReadiness struct {
	State         string `json:"state"`
	Count         int    `json:"count"`
	Authoritative bool   `json:"authoritative"`
}

type FleetSurface struct {
	Agents    []FleetAgent                `json:"agents"`
	Loops     []LoopView                  `json:"loops"`
	Graphs    []GraphView                 `json:"graphs"`
	Queue     []QueueExecutionView        `json:"queue"`
	Readiness map[string]SurfaceReadiness `json:"readiness"`
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

func (s *Service) PublishLoopAs(ctx context.Context, subject core.Subject, input PublishLoopInput) (PublishedLoop, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return PublishedLoop{}, err
	}
	revision, validation, err := loop.NewRevision(input.Revision)
	if err != nil {
		return PublishedLoop{Revision: revision, Validation: validation}, err
	}
	request := loop.PublishRequest{Revision: revision, Validation: validation, ExpectedPreviousDigest: input.ExpectedPreviousDigest, IdempotencyKey: input.IdempotencyKey}
	decision, err := s.Fleet.PublishLoop(ctx, orchestration.PublishLoopRequest{Subject: subject, Authority: input.Authority, Publication: request})
	return PublishedLoop{Revision: revision, Validation: validation, Decision: decision}, err
}

func (s *Service) PublishLoop(ctx context.Context, input PublishLoopInput) (PublishedLoop, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return PublishedLoop{}, err
	}
	return s.PublishLoopAs(ctx, subject, input)
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
	result := make([]LoopView, 0, len(revisions))
	for _, revision := range revisions {
		view := LoopView{Revision: revision, Validations: []loop.LoopValidationResult{}}
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
	result := make([]GraphView, 0, len(revisions))
	for _, revision := range revisions {
		view := GraphView{Revision: revision, Validations: []graph.GraphValidationResult{}}
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

func (s *Service) GetQueueItemAs(ctx context.Context, subject core.Subject, id string) (QueueItemView, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return QueueItemView{}, err
	}
	item, err := s.FleetRepository.GetQueueItem(ctx, id)
	if err != nil {
		return QueueItemView{}, err
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, id)
	if err != nil {
		return QueueItemView{}, err
	}
	return QueueItemView{Item: item, Projection: projection}, nil
}

func (s *Service) GetQueueItem(ctx context.Context, id string) (QueueItemView, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return QueueItemView{}, err
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
		view := QueueExecutionView{Item: item, Projection: projection, LoopExecutions: []execution.LoopExecution{}, Attempts: []execution.Attempt{}}
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
	agents, err := s.ListFleetAgentsAs(ctx, subject)
	if err != nil {
		return FleetSurface{}, err
	}
	loops, err := s.ListLoopsAs(ctx, subject)
	if err != nil {
		return FleetSurface{}, err
	}
	graphs, err := s.ListGraphsAs(ctx, subject)
	if err != nil {
		return FleetSurface{}, err
	}
	items, err := s.ListQueueAs(ctx, subject)
	if err != nil {
		return FleetSurface{}, err
	}
	state := func(count int) SurfaceReadiness {
		value := "ready"
		if count == 0 {
			value = "empty"
		}
		return SurfaceReadiness{State: value, Count: count, Authoritative: true}
	}
	return FleetSurface{Agents: agents, Loops: loops, Graphs: graphs, Queue: items, Readiness: map[string]SurfaceReadiness{
		"registry": state(len(agents)), "loops": state(len(loops)), "graphs": state(len(graphs)), "queue": state(len(items)),
	}}, nil
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
