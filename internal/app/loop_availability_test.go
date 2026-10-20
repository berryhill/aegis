package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
)

type availabilityAppRepository struct {
	retainedPublicationRepository
	revisions   []loop.LoopRevision
	validations []loop.LoopValidationResult
	events      []loop.AvailabilityEvent
	writes      int
	err         error
}

func (r *availabilityAppRepository) ListLoopRevisions(context.Context) ([]loop.LoopRevision, error) {
	return r.revisions, nil
}
func (r *availabilityAppRepository) ListLoopValidations(context.Context) ([]loop.LoopValidationResult, error) {
	return r.validations, nil
}
func (r *availabilityAppRepository) ListLoopPublicationProvenance(context.Context) ([]loop.PublicationProvenance, error) {
	out := []loop.PublicationProvenance{}
	for _, v := range r.revisions {
		p := r.provenance
		p.Loop = loop.NewProvenanceRevision(v.LoopID, v.Revision, v.Digest)
		out = append(out, p)
	}
	return out, nil
}
func (r *availabilityAppRepository) ListLoopLifecycleEvents(context.Context) ([]loop.LifecycleEvent, error) {
	return nil, nil
}
func (r *availabilityAppRepository) GetLoopAvailability(context.Context, string) (loop.AvailabilityStatus, error) {
	if r.err != nil {
		return loop.AvailabilityStatus{}, r.err
	}
	s := loop.AvailabilityStatus{LoopID: r.revision.LoopID, State: loop.AvailabilityPublished}
	if len(r.events) > 0 {
		e := r.events[len(r.events)-1]
		s.State, s.Sequence, s.Digest = e.State, e.Sequence, e.Digest
	}
	return s, nil
}
func (r *availabilityAppRepository) ListLoopAvailabilityEvents(context.Context, string) ([]loop.AvailabilityEvent, error) {
	return r.events, r.err
}
func (r *availabilityAppRepository) AppendLoopAvailability(ctx context.Context, e loop.AvailabilityEvent, _ fleet.AuditFact) (loop.AvailabilityEvent, bool, error) {
	for _, old := range r.events {
		if old.EventID == e.EventID {
			if loop.SameAvailabilityIntent(old, e) {
				return old, true, nil
			}
			return loop.AvailabilityEvent{}, false, fleet.ErrConflict
		}
	}
	s, _ := r.GetLoopAvailability(ctx, e.LoopID)
	if e.ExpectedState != s.State || e.PreviousDigest != s.Digest || e.Sequence != s.Sequence+1 {
		return loop.AvailabilityEvent{}, false, fleet.ErrConflict
	}
	r.writes++
	r.events = append(r.events, e)
	return e, false, nil
}

func TestLoopAvailabilityApplication(t *testing.T) {
	s, subject, in := draftFixture(t)
	ctx := context.Background()
	revision, validation, err := NewDoerLoopRevision(in.LoopID, 1, "", in.Contract)
	if err != nil {
		t.Fatal(err)
	}
	agent := registry.AgentRevision{AgentID: in.Agent.ID, Revision: 1, Digest: in.Agent.Digest, Lifecycle: registry.LifecycleEnabled, Ownership: registry.Ownership{OwnerID: subject.PrincipalID}}
	r := &availabilityAppRepository{retainedPublicationRepository: retainedPublicationRepository{draftAgentRepository: draftAgentRepository{latest: agent, exact: agent}, revision: revision, validation: validation, provenance: loop.PublicationProvenance{Loop: loop.NewProvenanceRevision(revision.LoopID, 1, revision.Digest), PublisherAgent: loop.NewProvenanceRevision(agent.AgentID, 1, agent.Digest), OwnerID: subject.PrincipalID, Digest: in.Agent.Digest}}, revisions: []loop.LoopRevision{revision}, validations: []loop.LoopValidationResult{validation}}
	s.FleetRepository = r
	s.Fleet, err = orchestration.NewFleetService(r, s.Authority, s.AuthorityCommands, func(context.Context, orchestration.FleetAction, core.Subject) error { return nil }, nil, s.Now)
	if err != nil {
		t.Fatal(err)
	}
	s.QueueWorker = &orchestration.QueueWorker{}
	status, err := s.GetLoopAvailabilityAs(ctx, subject, revision.LoopID)
	if err != nil || status.State != loop.AvailabilityPublished {
		t.Fatalf("default: %+v %v", status, err)
	}
	input := SetLoopAvailabilityInput{AgentID: agent.AgentID, Head: agentRevisionRef(agent), EventID: "archive", Sequence: 1, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived}
	input.Head.ID, input.Head.Digest = revision.LoopID, revision.Digest
	archived, err := s.SetLoopAvailabilityAs(ctx, subject, revision.LoopID, input)
	if err != nil {
		t.Fatal(err)
	}
	if values, err := s.ListLoopsAs(ctx, subject); err != nil || len(values) != 0 {
		t.Fatalf("discovery: %v %v", values, err)
	}
	if view, err := s.GetLoopViewAs(ctx, subject, revision.LoopID, 1); err != nil || view.Revision.Digest != revision.Digest || view.Lifecycle.State != loop.LifecycleDraft || len(view.Validations) != 1 {
		t.Fatalf("exact: %+v %v", view, err)
	}
	if replay, err := s.SetLoopAvailabilityAs(ctx, subject, revision.LoopID, input); err != nil || !replay.Idempotent || r.writes != 1 {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	foreign := input
	foreign.AgentID = "foreign"
	r.latest.AgentID = "foreign"
	r.latest.Ownership.OwnerID = "foreign-owner"
	if _, err := s.SetLoopAvailabilityAs(ctx, subject, revision.LoopID, foreign); err == nil || r.writes != 1 {
		t.Fatalf("foreign replay: %v", err)
	}
	r.latest = agent
	missing := input
	missing.AgentID = ""
	if _, err := s.SetLoopAvailabilityAs(ctx, subject, revision.LoopID, missing); err == nil || r.writes != 1 {
		t.Fatalf("missing authority: %v", err)
	}
	expired := subject
	expired.ExpiresAt = s.Now().Add(-time.Second)
	if _, err := s.SetLoopAvailabilityAs(ctx, expired, revision.LoopID, input); err == nil || r.writes != 1 {
		t.Fatalf("expired replay: %v", err)
	}
	next := revision
	next.Revision = 2
	next.PreviousDigest = revision.Digest
	next, v, err := loop.NewRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	r.revisions = append(r.revisions, next)
	r.validations = append(r.validations, v)
	if values, err := s.ListLoopsAs(ctx, subject); err != nil || len(values) != 0 {
		t.Fatalf("later revision archive: %v %v", values, err)
	}
	if values, err := s.ListLoopInventoryAs(ctx, subject); err != nil || len(values) != 2 {
		t.Fatalf("inventory: %v %v", values, err)
	}
	restore := input
	restore.Head.Revision, restore.Head.Digest = 2, next.Digest
	restore.EventID = "restore"
	restore.Sequence = 2
	restore.ExpectedState = loop.AvailabilityArchived
	restore.State = loop.AvailabilityPublished
	restore.PreviousDigest = archived.Event.Digest
	if _, err := s.SetLoopAvailabilityAs(ctx, subject, revision.LoopID, restore); err != nil {
		t.Fatal(err)
	}
	if values, err := s.ListLoopsAs(ctx, subject); err != nil || len(values) != 2 {
		t.Fatalf("restore: %v %v", values, err)
	}
	r.err = fleet.ErrCorrupt
	if _, err := s.ListLoopsAs(ctx, subject); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("corruption: %v", err)
	}
	if _, err := s.GetLoopAvailabilityAs(ctx, core.Subject{}, revision.LoopID); err == nil {
		t.Fatal("unauthenticated read")
	}
	s.FleetRepository = r.retainedPublicationRepository
	if _, err := s.GetLoopAvailabilityAs(ctx, subject, revision.LoopID); !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("missing capability: %v", err)
	}
}
