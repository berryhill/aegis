package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"os"
	"reflect"
)

// bindDoerRun is shared by observational readiness and admission. It installs
// only a pure contract binding, never the effect-authorizing exactDoerRunKey.
func bindDoerRun(ctx context.Context, input QueueLoopInput, candidate loop.LoopRevision) (context.Context, error) {
	if candidate.SchemaVersion != loop.DoerReusableSchemaVersion {
		return ctx, nil
	}
	node := graph.Node{ID: "readiness", Participant: input.Agent, Loop: input.Loop}
	for _, p := range candidate.Inputs {
		node.Inputs = append(node.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	direct, err := graph.NewDirectLoopSnapshot("readiness-snapshot", node, input.Inputs)
	if err != nil {
		return ctx, ErrDenied
	}
	snapshot, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		return ctx, ErrDenied
	}
	contract, err := orchestration.BindDirectDoerContract(candidate, node, snapshot)
	if err != nil {
		return ctx, ErrDenied
	}
	return context.WithValue(ctx, boundDoerRunKey{}, boundDoerRun{candidate: input.Loop, contract: contract}), nil
}

// ReadDoerReadinessAs preserves v4 candidate calls and adds exact published v5
// references. Reads never reserve an intent or prepare host authorization.
func (s *Service) ReadDoerReadinessAs(ctx context.Context, subject core.Subject, input DoerReadinessInput) (DoerCandidateReadiness, error) {
	if err := input.validateShape(); err != nil {
		return newDoerReadiness(), err
	}
	if input.Loop == (reference.RevisionRef{}) {
		return s.doerCandidateReadinessAs(ctx, subject, DoerCandidateReadinessInput{Agent: input.Agent, Candidate: input.Candidate}, input.Probe)
	}
	r := newDoerReadiness()
	if ctx == nil || s.requireFleetPrincipal(subject) != nil {
		return r, ErrDenied
	}
	if !reflect.DeepEqual(input.Candidate, loop.LoopRevision{}) || input.Loop.Validate() != nil {
		return r, ErrDenied
	}
	view, err := s.GetLoopViewAs(ctx, subject, input.Loop.ID, input.Loop.Revision)
	if err != nil {
		return r, err
	}
	if view.Revision.Digest != input.Loop.Digest || view.Revision.SchemaVersion != loop.DoerReusableSchemaVersion {
		return r, ErrDenied
	}
	run := QueueLoopInput{Agent: input.Agent, Loop: input.Loop, Inputs: input.Inputs, IdempotencyKey: input.IdempotencyKey, Activate: input.Activate}
	run, identity, err := normalizeLoopRunIdentity(run)
	if err != nil {
		return r, ErrDenied
	}
	requestID := ""
	if input.IdempotencyKey != "" {
		if len(input.IdempotencyKey) > 256 {
			return r, ErrDenied
		}
		requestID = queueLoopRequestID(subject, run)
		var retained string
		loadErr := s.Store.Load("queue-loop-intent", requestID, &retained)
		if loadErr == nil && retained != identity {
			return r, fleet.ErrConflict
		}
		if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
			return r, ErrDenied
		}
	}
	ctx, err = bindDoerRun(ctx, run, view.Revision)
	if err != nil {
		return r, err
	}
	r, err = s.doerCandidateReadinessAs(ctx, subject, DoerCandidateReadinessInput{Agent: input.Agent, Candidate: view.Revision}, input.Probe)
	r.RequestID = requestID
	return r, err
}
