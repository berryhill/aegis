package orchestration

import (
	"context"
	"fmt"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// SetLoopAvailabilityRequest keeps discovery metadata separate from execution lifecycle.
type SetLoopAvailabilityRequest struct {
	Subject   core.Subject
	Authority reference.DigestRef
	Publisher reference.RevisionRef
	Workspace *WorkspaceAuthority
	Event     loop.AvailabilityEvent
}

func (service *FleetService) SetLoopAvailability(ctx context.Context, request SetLoopAvailabilityRequest) (loop.AvailabilityEvent, bool, error) {
	if err := service.authorize(ctx, FleetActionLoopLifecycle, request.Subject); err != nil {
		return loop.AvailabilityEvent{}, false, ErrDenied
	}
	repository, ok := service.repository.(fleet.LoopAvailabilityRepository)
	if !ok {
		return loop.AvailabilityEvent{}, false, fmt.Errorf("Loop availability repository unavailable")
	}
	// Admission precedes all replay lookup and persistence.
	var publisher registry.AgentRevision
	var err error
	if request.Workspace != nil {
		workspace, agent, e := service.resolveWorkspace(ctx, request.Subject, request.Authority, request.Workspace, WorkspaceManageLoops)
		if e != nil || request.Publisher != workspace.Agent {
			return loop.AvailabilityEvent{}, false, ErrDenied
		}
		publisher = agent
	} else {
		authority, mandate, readiness := service.resolveAuthority(ctx, request.Subject, request.Authority)
		if readiness.State != ReadinessReady || request.Publisher.Validate() != nil || request.Publisher.ID != authority.AgentID {
			return loop.AvailabilityEvent{}, false, ErrDenied
		}
		publisher, err = service.repository.GetAgentRevision(ctx, request.Publisher.ID, request.Publisher.Revision)
		if err != nil || publisher.Digest != request.Publisher.Digest || publisher.Lifecycle != registry.LifecycleEnabled || !agentMatchesAuthority(publisher, authority, mandate) {
			return loop.AvailabilityEvent{}, false, ErrDenied
		}
	}
	owner, err := service.repository.GetLoopPublicationProvenance(ctx, request.Event.LoopID, 1)
	if err != nil {
		return loop.AvailabilityEvent{}, false, err
	}
	original, err := service.repository.GetAgentRevision(ctx, owner.PublisherAgent.ID, owner.PublisherAgent.Revision)
	if err != nil {
		return loop.AvailabilityEvent{}, false, err
	}
	if original.Digest != owner.PublisherAgent.Digest || (owner.OwnerID != "" && owner.OwnerID != original.Ownership.OwnerID) {
		return loop.AvailabilityEvent{}, false, fleet.ErrCorrupt
	}
	if publisher.AgentID != owner.PublisherAgent.ID || publisher.Ownership.OwnerID != original.Ownership.OwnerID {
		return loop.AvailabilityEvent{}, false, ErrDenied
	}
	event := request.Event
	event.PublisherAgent = provenanceRevision(request.Publisher)
	event.OwnerID, event.PrincipalID = original.Ownership.OwnerID, request.Subject.PrincipalID
	event.Authority, event.OccurredAt = provenanceDigest(request.Authority), service.now()
	event, err = loop.NewAvailabilityEvent(event)
	if err != nil {
		return loop.AvailabilityEvent{}, false, err
	}
	eventType := "loop.archived"
	if event.State == loop.AvailabilityPublished {
		eventType = "loop.unarchived"
	}
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: eventType, SubjectID: event.LoopID, PrincipalID: event.PrincipalID, AgentID: publisher.AgentID, Outcome: "succeeded", Reason: "appended Loop availability event"}}
	return repository.AppendLoopAvailability(ctx, event, fact)
}
