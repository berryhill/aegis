package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
	"html"
	"net/http"
)

func (p *DoerContinuationPortal) retainRequeue(ctx context.Context, sub core.Subject, session string, selector app.RequeueQueueItemInput) (doerPending, error) {
	run, binding, err := p.svc.PrepareQueueRequeueAs(ctx, sub, selector)
	if err != nil {
		return doerPending{}, err
	}
	view, err := p.svc.GetLoopViewAs(ctx, sub, run.Loop.ID, run.Loop.Revision)
	if err != nil {
		return doerPending{}, err
	}
	agent, err := p.svc.FleetRepository.GetAgentRevision(ctx, run.Agent.ID, run.Agent.Revision)
	if err != nil || agent.Digest != run.Agent.Digest {
		return doerPending{}, app.ErrDenied
	}
	expiry := sub.ExpiresAt
	if t := sub.AuthenticatedAt.Add(p.svc.Config.Principal.AuthTTL); t.Before(expiry) {
		expiry = t
	}
	intent := app.DoerContinuationIntent{RequeueBinding: core.Digest(binding), CandidateDigest: run.Loop.Digest, Agent: run.Agent, Loop: run.Loop, Charter: agent.Charter, Run: run, Requester: sub, SessionID: session, DeploymentID: p.svc.Config.Credentials.Authority.DeploymentID, ConfigIdentity: p.svc.DoerContinuationConfigIdentity(), ExpiresAt: expiry}
	review := app.DoerContinuationReview{Requeue: &binding, Intent: intent, Published: &view.Revision, Origin: p.svc.Config.API.Console.Origin}
	review.Digest = review.ContentDigest()
	p.mu.Lock()
	for _, saved := range p.pending {
		if saved.Review.Digest == review.Digest {
			id := saved.ID
			p.mu.Unlock()
			return p.reload(ctx, id)
		}
	}
	if len(p.pending) >= 128 {
		p.mu.Unlock()
		return doerPending{}, app.ErrDenied
	}
	id, err := randomConsoleID("doer-pending")
	if err != nil {
		p.mu.Unlock()
		return doerPending{}, err
	}
	v := doerPending{ID: id, Review: review, Status: "no_decision"}
	p.pending[id] = &v
	p.mu.Unlock()
	return v, nil
}

// Uses the same independent native review/observe route as ordinary Run, but
// the final action is always the predecessor-bound Queue form, never Loop Run.
func (p *DoerContinuationPortal) renderRequeue(c *echo.Context, v doerPending, csrf string) error {
	i := v.Review.Intent
	ref := i.Run.Reexecution.PreviousQueue
	archive, err := p.svc.GetQueueArchiveAs(c.Request().Context(), i.Requester, ref)
	if err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(v.Review, "", "  ")
	esc := html.EscapeString
	body := `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Exact Requeue continuation</title></head><body><main><h1>Review exact predecessor-bound Requeue</h1><p>Browser review is not approval. Independent native confirmation grants only this exact continuation, not provisioning, host writes or other runtime prerequisites.</p><pre>` + esc(string(raw)) + `</pre><p role="status">` + esc(v.Status) + `</p><form method="post" action="/console/loops/doer/continuation"><input type="hidden" name="csrf" value="` + esc(csrf) + `"><input type="hidden" name="intent_id" value="` + esc(v.ID) + `"><button name="action" value="request-local-review">Request independent local review</button><button name="action" value="observe">Observe signed decision (no dialog)</button></form>`
	if v.ApprovalID != "" {
		if _, err := p.svc.ResolveDoerContinuationAs(c.Request().Context(), i.Requester, i.SessionID, v.ApprovalID, i); err != nil {
			return err
		}
		body += fmt.Sprintf(`<form method="post" action="/console/queue/%s/requeue"><input type="hidden" name="csrf" value="%s"><input type="hidden" name="digest" value="%s"><input type="hidden" name="expected_revision" value="%d"><button>Requeue exact approved predecessor</button></form>`, esc(ref.ID), esc(csrf), esc(ref.Digest), archive.Revision)
	}
	return c.HTML(http.StatusOK, body+`</main></body></html>`)
}
