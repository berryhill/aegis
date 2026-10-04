package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"

	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

// Pending data is controller-owned NONAUTH metadata. It is never restored from
// the unsigned draft store and cannot authorize execution or identify a peer.
type peerPIDKey struct{}

type doerPending struct {
	CompanionPID int
	ID           string
	Review       app.DoerContinuationReview
	ApprovalID   string
	Status       string
	Launching    bool
}
type DoerContinuationPortal struct {
	svc     *app.Service
	manager *console.Manager
	mu      sync.Mutex
	pending map[string]*doerPending
	launch  func(context.Context, string, func(int)) string
}

func NewDoerContinuationPortal(svc *app.Service, manager *console.Manager) *DoerContinuationPortal {
	p := &DoerContinuationPortal{svc: svc, manager: manager, pending: make(map[string]*doerPending)}
	p.launch = func(ctx context.Context, id string, bind func(int)) string {
		return launchDoerCompanion(ctx, svc.Config.API.UnixSocket, svc.Config.API.Console.Origin, id, svc.Config.API.Token, bind, svc.ConfigFile)
	}
	return p
}

func (p *DoerContinuationPortal) retain(ctx context.Context, subject core.Subject, session, draftID string, key string) (doerPending, error) {
	draft, err := p.svc.ReadDoerDraftAs(ctx, subject, draftID)
	if err != nil {
		return doerPending{}, err
	}
	candidate, _, err := app.NewDoerLoopRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil {
		return doerPending{}, err
	}
	agent, err := p.svc.FleetRepository.GetAgentRevision(ctx, draft.Agent.ID, draft.Agent.Revision)
	if err != nil || agent.Digest != draft.Agent.Digest {
		return doerPending{}, app.ErrDenied
	}
	expiry := subject.ExpiresAt
	for _, t := range []time.Time{subject.AuthenticatedAt.Add(p.svc.Config.Principal.AuthTTL), draft.ExpiresAt} {
		if t.Before(expiry) {
			expiry = t
		}
	}
	if !p.svc.Now().Before(expiry) {
		return doerPending{}, app.ErrExpired
	}
	ref := app.RevisionReference(candidate.LoopID, candidate.Revision, candidate.Digest)
	intent := app.DoerContinuationIntent{DraftID: draft.ID, DraftVersion: draft.Version, CandidateDigest: candidate.Digest, Agent: draft.Agent, Loop: ref, Charter: agent.Charter, PublicationKey: draft.PublicationKey, Run: app.QueueLoopInput{Agent: draft.Agent, Loop: ref, IdempotencyKey: key, Activate: true}, Requester: subject, SessionID: session, DeploymentID: p.svc.Config.Credentials.Authority.DeploymentID, ConfigIdentity: p.svc.DoerContinuationConfigIdentity(), ExpiresAt: expiry}
	review := app.DoerContinuationReview{Intent: intent, Draft: draft, Origin: p.svc.Config.API.Console.Origin}
	review.Digest = review.ContentDigest()
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, v := range p.pending {
		if !p.svc.Now().Before(v.Review.Intent.ExpiresAt) {
			delete(p.pending, id)
			continue
		}
		if reflect.DeepEqual(v.Review, review) {
			return *v, nil
		}
	}
	if len(p.pending) >= 128 {
		return doerPending{}, app.ErrDenied
	}
	id, err := randomConsoleID("doer-pending")
	if err != nil {
		return doerPending{}, err
	}
	v := doerPending{ID: id, Review: review, Status: "no_decision"}
	p.pending[id] = &v
	return v, nil
}

func (p *DoerContinuationPortal) reload(ctx context.Context, id string) (doerPending, error) {
	p.mu.Lock()
	v, ok := p.pending[id]
	var value doerPending
	if ok {
		value = *v
	}
	p.mu.Unlock()
	if !ok {
		return value, app.ErrDenied
	}
	i := value.Review.Intent
	if !p.svc.Now().Before(i.ExpiresAt) {
		return value, app.ErrExpired
	}
	if i.ConfigIdentity != p.svc.DoerContinuationConfigIdentity() || i.DeploymentID != p.svc.Config.Credentials.Authority.DeploymentID {
		return value, app.ErrDenied
	}
	draft, err := p.svc.ReadDoerDraftAs(ctx, i.Requester, i.DraftID)
	if err != nil {
		return value, err
	}
	if !reflect.DeepEqual(draft, value.Review.Draft) || value.Review.Digest != value.Review.ContentDigest() {
		return value, app.ErrConflict
	}
	candidate, _, err := app.NewDoerLoopRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil || candidate.Digest != i.Loop.Digest {
		return value, app.ErrConflict
	}
	agent, err := p.svc.FleetRepository.GetAgentRevision(ctx, i.Agent.ID, i.Agent.Revision)
	if err != nil || agent.Digest != i.Agent.Digest || agent.Charter != i.Charter {
		return value, app.ErrDenied
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	saved := p.pending[id]
	if saved == nil || !reflect.DeepEqual(saved.Review, value.Review) {
		return value, app.ErrConflict
	}
	if err := p.recoverApprovalLocked(ctx, saved); err != nil {
		return value, err
	}
	return *saved, nil
}

// Caller holds mu: signed readback, not mutable portal status, supplies linkage.
func (p *DoerContinuationPortal) recoverApprovalLocked(ctx context.Context, saved *doerPending) error {
	i := saved.Review.Intent
	id, _, err := p.svc.RecoverDoerContinuationAs(ctx, i.Requester, i.SessionID, i)
	if err != nil {
		return err
	}
	if saved.ApprovalID != "" && saved.ApprovalID != id {
		return app.ErrDenied
	}
	if id != "" {
		saved.ApprovalID = id
		saved.Status = "approved"
	}
	return nil
}

func (p *DoerContinuationPortal) exact(ctx context.Context, subject core.Subject, session string, run app.QueueLoopInput) (doerPending, bool) {
	p.mu.Lock()
	ids := []string{}
	for id, v := range p.pending {
		if v.Review.Intent.SessionID == session && reflect.DeepEqual(v.Review.Intent.Requester, subject) && reflect.DeepEqual(v.Review.Intent.Run, run) {
			ids = append(ids, id)
		}
	}
	p.mu.Unlock()
	for _, id := range ids {
		v, err := p.reload(ctx, id)
		if err == nil && v.ApprovalID != "" {
			if _, err = p.svc.ResolveDoerContinuationAs(ctx, subject, session, v.ApprovalID, v.Review.Intent); err == nil {
				return v, true
			}
		}
	}
	return doerPending{}, false
}

func (p *DoerContinuationPortal) nativeSubject(c *echo.Context) (core.Subject, error) {
	if c.Request().Header.Get("Origin") != p.svc.Config.API.Console.Origin || len(c.Request().Header.Values("Origin")) != 1 {
		return core.Subject{}, app.ErrDenied
	}
	subject, err := requestSubject(c)
	if err != nil {
		return subject, err
	}
	if subject.Kind != "human" || subject.PrincipalID != p.svc.Config.Principal.ID || subject.Method != "local-os" || subject.Issuer != "linux-so-peercred" {
		return subject, app.ErrDenied
	}
	return subject, nil
}

func (p *DoerContinuationPortal) requireCompanionPeer(c *echo.Context) error {
	pid, ok := c.Request().Context().Value(peerPIDKey{}).(int)
	p.mu.Lock()
	defer p.mu.Unlock()
	pending := p.pending[c.Param("id")]
	if !ok || pid <= 0 || pending == nil || !pending.Launching || pending.CompanionPID != pid {
		return app.ErrDenied
	}
	return nil
}

// RegisterNative must be attached only to the existing protected /v1 group.
func (p *DoerContinuationPortal) RegisterNative(g *echo.Group) {
	g.GET("/doer-continuations/pending/:id", func(c *echo.Context) error {
		if _, err := p.nativeSubject(c); err != nil {
			return err
		}
		if err := p.requireCompanionPeer(c); err != nil {
			return err
		}
		v, err := p.reload(c.Request().Context(), c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, v.Review)
	})
	g.POST("/doer-continuations/pending/:id/decision", func(c *echo.Context) error {
		subject, err := p.nativeSubject(c)
		if err != nil {
			return err
		}
		var body struct {
			Decision       string `json:"decision"`
			ExpectedDigest string `json:"expected_digest"`
		}
		if err = decode(c, &body); err != nil {
			return err
		}
		if body.Decision != "approve" {
			return app.ErrDenied
		}
		if err := p.requireCompanionPeer(c); err != nil {
			return err
		}
		v, err := p.reload(c.Request().Context(), c.Param("id"))
		if err != nil {
			return err
		}
		if body.ExpectedDigest != v.Review.Digest {
			return app.ErrConflict
		}
		id := v.ApprovalID
		if id == "" {
			id, err = p.svc.ApproveDoerContinuationAs(c.Request().Context(), subject, v.Review.Intent)
			if err != nil {
				return err
			}
		}
		if _, err = p.svc.ResolveDoerContinuationAs(c.Request().Context(), v.Review.Intent.Requester, v.Review.Intent.SessionID, id, v.Review.Intent); err != nil {
			return err
		}
		p.mu.Lock()
		if saved := p.pending[v.ID]; saved != nil {
			saved.ApprovalID = id
			saved.Status = "approved"
		}
		p.mu.Unlock()
		return c.JSON(http.StatusOK, map[string]string{"intent_id": v.ID, "digest": v.Review.Digest, "status": "approved"})
	})
}

func (p *DoerContinuationPortal) render(c *echo.Context, v doerPending, csrf string) error {
	if v.ApprovalID != "" {
		if _, err := p.svc.ResolveDoerContinuationAs(c.Request().Context(), v.Review.Intent.Requester, v.Review.Intent.SessionID, v.ApprovalID, v.Review.Intent); err != nil {
			v.Status = "approval_unavailable"
		} else {
			v.Status = "approved"
		}
	}
	raw, _ := json.MarshalIndent(v.Review, "", "  ")
	m := consoleweb.DoerContinuationModel{ID: v.ID, DraftID: v.Review.Intent.DraftID, Details: string(raw), Status: v.Status, Run: consoleweb.PublishedDoerRunModel{URL: "/console/loops/run?loop_id=" + url.QueryEscape(v.Review.Intent.Loop.ID), CSRF: csrf, Key: v.Review.Intent.Run.IdempotencyKey, Digest: v.Review.Intent.Loop.Digest, Revision: v.Review.Intent.Loop.Revision}}
	m.RunBlocker = "Independent local approval and exact publication are required before Run."
	if v.Status == "approved" {
		view, readErr := p.svc.GetLoopViewAs(c.Request().Context(), v.Review.Intent.Requester, v.Review.Intent.Loop.ID, v.Review.Intent.Loop.Revision)
		if readErr == nil && view.Revision.Digest == v.Review.Intent.Loop.Digest {
			executor, resolveErr := p.svc.ResolveDoerContinuationAs(c.Request().Context(), v.Review.Intent.Requester, v.Review.Intent.SessionID, v.ApprovalID, v.Review.Intent)
			if resolveErr == nil {
				readiness, readinessErr := p.svc.ReadDoerCandidateReadinessAs(c.Request().Context(), executor, app.DoerCandidateReadinessInput{Agent: v.Review.Intent.Agent, Candidate: view.Revision})
				m.CanRun = readinessErr == nil && (readiness.Reason == "helper_probe_required" || readiness.CanExecute)
				m.RunBlocker = readiness.Reason
			}
		}
	}
	page := consoleweb.PageModel{Authenticated: true, CSRF: csrf, Surface: consoleweb.SurfaceModel{Domain: "loops", Title: "Loops"}, DoerContinuation: &m}
	data, err := renderConsole(c.Request().Context(), consoleweb.Document(page))
	if err != nil {
		return err
	}
	return c.Blob(http.StatusOK, "text/html; charset=utf-8", data)
}

func (p *DoerContinuationPortal) RegisterBrowser(e *echo.Echo) {
	e.POST("/console/loops/doer/continuation", func(c *echo.Context) error {
		p.manager.ApplySecurityHeaders(c.Response().Header(), true)
		form, err := decodeDoerContinuationForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid continuation form")
		}
		c.Request().Header.Set("X-CSRF-Token", form.Get("csrf"))
		subject, session, err := p.manager.AuthorizeCommand(c.Request())
		if err != nil {
			return mapConsoleError(err)
		}
		if err = p.svc.RequirePrincipal(subject); err != nil {
			return err
		}
		var v doerPending
		switch form.Get("action") {
		case "review":
			d, readErr := p.svc.ReadDoerDraftAs(c.Request().Context(), subject, form.Get("draft_id"))
			if readErr != nil {
				return readErr
			}
			candidate, _, readErr := app.NewDoerLoopRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
			if readErr != nil {
				return readErr
			}
			p.mu.Lock()
			var retained *doerPending
			for _, saved := range p.pending {
				if saved.Review.Intent.SessionID == session && reflect.DeepEqual(saved.Review.Intent.Requester, subject) && reflect.DeepEqual(saved.Review.Draft, d) {
					copy := *saved
					retained = &copy
					break
				}
			}
			p.mu.Unlock()
			if retained != nil {
				v, err = p.reload(c.Request().Context(), retained.ID)
				break
			}
			// Contextual review is never an explicit new Run action.
			query := c.Request().URL.Query()
			query.Del("new_run")
			c.Request().URL.RawQuery = query.Encode()
			key, keyErr := consoleLoopRunKey(c, subject, &consoleweb.RecordModel{Label: d.LoopID, Revision: "r" + strconv.FormatUint(d.Revision, 10), Digest: candidate.Digest})
			if keyErr != nil {
				return keyErr
			}
			v, err = p.retain(c.Request().Context(), subject, session, d.ID, key)
		case "observe", "request-local-review":
			v, err = p.reload(c.Request().Context(), form.Get("intent_id"))
			if err == nil && (v.Review.Intent.SessionID != session || !reflect.DeepEqual(v.Review.Intent.Requester, subject)) {
				err = app.ErrDenied
			}
			if err == nil && form.Get("action") == "request-local-review" && v.ApprovalID == "" {
				p.mu.Lock()
				saved := p.pending[v.ID]
				if saved == nil {
					p.mu.Unlock()
					return app.ErrDenied
				}
				// Recheck immediately at launch admission, including bookkeeping
				// recovered by a concurrent observe or a lost native response.
				if err = p.recoverApprovalLocked(c.Request().Context(), saved); err != nil {
					p.mu.Unlock()
					return err
				}
				launching := saved.Launching || saved.ApprovalID != ""
				v = *saved
				if !launching {
					saved.Launching = true
				}
				p.mu.Unlock()
				if !launching {
					status := p.launch(c.Request().Context(), v.ID, func(pid int) { p.mu.Lock(); saved.CompanionPID = pid; p.mu.Unlock() })
					p.mu.Lock()
					saved.Launching = false
					saved.CompanionPID = 0
					if saved.ApprovalID == "" {
						saved.Status = status
					}
					// The companion may have committed approval before losing its
					// response. Resolve that signed result, never its exit status.
					err = p.recoverApprovalLocked(c.Request().Context(), saved)
					v = *saved
					p.mu.Unlock()
				}
			}
		default:
			return app.ErrDenied
		}
		if err != nil {
			return err
		}
		return p.render(c, v, form.Get("csrf"))
	})
}

func decodeDoerContinuationForm(r *http.Request) (url.Values, error) {
	if !isConsoleForm(r) || r.ContentLength > 4096 || r.Body == nil {
		return nil, errors.New("invalid form")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	v := r.PostForm
	for k, values := range v {
		if (k != "csrf" && k != "draft_id" && k != "action" && k != "intent_id") || len(values) != 1 || values[0] == "" {
			return nil, errors.New("invalid field")
		}
	}
	if v.Get("csrf") == "" || v.Get("action") == "" || len(v) != 3 {
		return nil, errors.New("invalid form")
	}
	if v.Get("action") == "review" && v.Get("draft_id") == "" || v.Get("action") != "review" && v.Get("intent_id") == "" {
		return nil, errors.New("invalid target")
	}
	return v, nil
}
