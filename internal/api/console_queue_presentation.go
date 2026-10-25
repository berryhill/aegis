package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func applyConsoleQueuePresentation(ctx context.Context, svc *app.Service, sub core.Subject, model *consoleweb.SurfaceModel) {
	apply := func(record *consoleweb.RecordModel) {
		actions := &consoleweb.QueueActionsModel{RequeueReason: "Queue actions unavailable; canonical ownership and eligibility required"}
		record.QueueActions = actions
		view, err := svc.ConsoleQueuePresentationAs(ctx, sub, record.Key, "")
		if err != nil {
			return
		}
		actions.Digest, actions.Revision = view.Digest, strconv.FormatUint(view.Revision, 10)
		actions.Archived, actions.CanArchive = view.Archived, view.Terminal
		actions.RequeueReason = "Requeue unavailable: only supported failed direct Doer work with remaining cumulative budget; fresh admission required"
		if view.Failed {
			result, err := svc.CheckQueueRequeueAs(ctx, sub, app.ConsoleQueueRequeueInput(record.Key, view.Digest, "").QueueItem)
			actions.CanRequeue = err == nil && result.Reason == "eligible_fresh_admission_required"
			if actions.CanRequeue {
				actions.RequeueReason = "Same pins and inputs; linked successor, remaining cumulative budget and fresh authority admission"
			} else if err == nil && result.RequiredAction != "" {
				actions.CanReviewRequeue = true
				actions.RequeueReason = "Requeue unavailable: independent_setup_required; " + result.Reason + "; required action: " + result.RequiredAction + "; Console authentication does not grant execution authority"
			} else if result.Reason == "doer_reexecution_budget_exhausted" {
				actions.RequeueReason = "Requeue unavailable: immutable implementation-attempt budget exhausted"
			}
		}
	}
	for i := range model.Records {
		apply(&model.Records[i])
	}
	if model.Inspector != nil {
		apply(model.Inspector)
	}
	// Existing bands retain their selection/navigation; attach the same controls.
	for i := range model.ActiveRecords {
		apply(&model.ActiveRecords[i])
	}
	for i := range model.FailedRecords {
		apply(&model.FailedRecords[i])
	}
}

func registerConsoleQueuePresentation(e *echo.Echo, svc *app.Service, manager *console.Manager, portals ...*DoerContinuationPortal) {
	e.POST("/console/queue/preferences", consoleQueuePreferencesHandler(svc, manager))
	e.POST("/console/queue/:item/archive", consoleQueuePresentationHandler(svc, manager, "archive", portals...))
	e.POST("/console/queue/:item/unarchive", consoleQueuePresentationHandler(svc, manager, "unarchive", portals...))
	e.POST("/console/queue/:item/requeue", consoleQueuePresentationHandler(svc, manager, "requeue", portals...))
	e.POST("/console/queue/:item/requeue-review", consoleQueuePresentationHandler(svc, manager, "requeue-review", portals...))
}

func consoleQueuePresentationHandler(svc *app.Service, manager *console.Manager, action string, portals ...*DoerContinuationPortal) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		request := c.Request()
		media, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		bad := func() error { return echo.NewHTTPError(http.StatusBadRequest, "invalid queue action form") }
		if err != nil || media != "application/x-www-form-urlencoded" || request.Body == nil {
			return bad()
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 4097))
		if err != nil || len(body) > 4096 {
			return bad()
		}
		values, err := url.ParseQuery(string(body))
		if err != nil || len(values) != 3 {
			return bad()
		}
		for key, entries := range values {
			if (key != "csrf" && key != "digest" && key != "expected_revision") || len(entries) != 1 || entries[0] == "" {
				return bad()
			}
		}
		revision, err := strconv.ParseUint(values.Get("expected_revision"), 10, 64)
		if err != nil {
			return bad()
		}
		request.Header.Set("X-CSRF-Token", values.Get("csrf"))
		sub, session, err := manager.AuthorizeCommand(request)
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.RequireDashboardPrincipal(sub); err != nil {
			return err
		}
		view, err := svc.ConsoleQueuePresentationAs(request.Context(), sub, c.Param("item"), values.Get("digest"))
		if err != nil {
			return err
		}
		selector := app.ConsoleQueueRequeueInput(c.Param("item"), view.Digest, "")
		ref := selector.QueueItem
		if (action == "requeue" || action == "requeue-review") && view.Revision != revision {
			return app.ErrConflict
		}
		if !view.Terminal {
			return app.ErrDenied
		}
		// Stable server-derived identity binds the canonical source, CAS and action;
		// caller cannot rotate a key or supply authority/task/budget fields.
		intentAction := action
		if action == "requeue-review" {
			intentAction = "requeue"
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("console-queue-v1:%s:%s:%s:%d:%s", sub.PrincipalID, ref.ID, ref.Digest, revision, intentAction)))
		identity := fmt.Sprintf("console-queue-%x", sum)
		selector.IdempotencyKey = identity
		if action == "requeue-review" {
			if len(portals) == 0 {
				return app.ErrDenied
			}
			v, err := portals[0].retainRequeue(request.Context(), sub, session, selector)
			if err != nil {
				return err
			}
			return portals[0].render(c, v, values.Get("csrf"))
		}
		if action == "requeue" {
			var result app.QueueLoopResult
			if len(portals) > 0 {
				run, _, prepErr := svc.PrepareQueueRequeueAs(request.Context(), sub, selector)
				if prepErr != nil {
					return prepErr
				}
				if pending, ok := portals[0].exact(request.Context(), sub, session, run); ok {
					result, err = svc.QueueDoerContinuationAs(request.Context(), sub, session, pending.ApprovalID, pending.Review.Intent)
				} else {
					result, err = svc.RequeueQueueItemAs(request.Context(), sub, selector)
				}
			} else {
				result, err = svc.RequeueQueueItemAs(request.Context(), sub, selector)
			}
			// A worker can return an error after a durable failed disposition.
			// Reconcile that exact admitted item read-only; never retry an effect.
			result, err = svc.ReconcileConsoleQueueRequeueFailureAs(request.Context(), sub, selector, result, err)
			if err != nil {
				return err
			}
			if result.Execution != nil {
				return c.Redirect(http.StatusSeeOther, consoleRecordURL(consoleQueue, result.Execution.Item.ItemID))
			}
			// Render an HTML document rather than Chrome's text/plain viewer,
			// whose injected inline wrapping style violates our strict CSP.
			message := "Requeue admission: independent_setup_required; " + result.Reason + "; required action: " + result.RequiredAction + "; no completed execution asserted"
			return c.HTML(http.StatusOK, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>Requeue admission</title></head><body><main><h1>Independent setup required</h1><p>"+html.EscapeString(message)+"</p><a href=\""+html.EscapeString(consoleRecordURL(consoleQueue, ref.ID))+"\">Return to execution</a></main></body></html>")
		}
		if action != "archive" && action != "unarchive" {
			return bad()
		}
		_, _, err = svc.SetQueueArchiveAs(request.Context(), sub, app.ConsoleQueueArchiveInput(ref.ID, ref.Digest, identity, action == "archive", revision))
		if err != nil {
			return err
		}
		if action == "archive" {
			return c.Redirect(http.StatusSeeOther, "/console/queue")
		}
		return c.Redirect(http.StatusSeeOther, consoleRecordURL(consoleQueue, ref.ID))
	}
}
