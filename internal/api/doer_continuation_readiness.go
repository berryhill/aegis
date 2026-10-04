package api

import (
	"context"
	"reflect"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

// ApprovedDraftReadiness evaluates the genuinely admitted original controller
// subject only after exact signed approval resolution. It never authenticates a
// substitute subject or grants authority from pending/draft metadata.
func (p *DoerContinuationPortal) ApprovedDraftReadiness(ctx context.Context, requester core.Subject, session string, draft app.DoerDraft) (app.DoerCandidateReadiness, bool) {
	p.mu.Lock()
	ids := []string{}
	for id, v := range p.pending {
		if v.ApprovalID != "" && v.Review.Intent.SessionID == session && reflect.DeepEqual(v.Review.Intent.Requester, requester) && reflect.DeepEqual(v.Review.Draft, draft) {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	for _, id := range ids {
		v, err := p.reload(ctx, id)
		if err != nil {
			continue
		}
		executor, err := p.svc.ResolveDoerContinuationAs(ctx, requester, session, v.ApprovalID, v.Review.Intent)
		if err != nil {
			continue
		}
		candidate, _, err := app.NewDoerLoopRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
		if err != nil {
			continue
		}
		readiness, err := p.svc.ProbeDoerCandidateReadinessAs(ctx, executor, app.DoerCandidateReadinessInput{Agent: draft.Agent, Candidate: candidate})
		if err == nil {
			return readiness, true
		}
	}
	return app.DoerCandidateReadiness{}, false
}
