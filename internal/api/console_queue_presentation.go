package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func queuePresentationTerminal(state queue.State) bool {
	switch state {
	case queue.StateSucceeded, queue.StateFailed, queue.StateCancelled, queue.StateDenied, queue.StateExpired:
		return true
	}
	return false
}

func applyConsoleQueuePresentation(ctx context.Context, svc *app.Service, sub core.Subject, model *consoleweb.SurfaceModel) {
	apply := func(record *consoleweb.RecordModel) {
		actions := &consoleweb.QueueActionsModel{RequeueReason: "Queue actions unavailable; canonical ownership and eligibility required"}
		record.QueueActions = actions
		view, err := svc.GetQueueItemAs(ctx, sub, record.Key)
		if err != nil || view.Submission.OwnerID != sub.PrincipalID {
			return
		}
		ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: view.Item.ItemID, Digest: view.Item.Digest}
		archive, err := svc.GetQueueArchiveAs(ctx, sub, ref)
		if err != nil {
			return
		}
		actions.Digest, actions.Revision = ref.Digest, strconv.FormatUint(archive.Revision, 10)
		actions.Archived, actions.CanArchive = archive.Archived, queuePresentationTerminal(view.Projection.State)
		actions.RequeueReason = "Requeue unavailable: only supported failed direct Doer work with remaining cumulative budget; fresh admission required"
		if view.Projection.State == queue.StateFailed {
			result, err := svc.CheckQueueRequeueAs(ctx, sub, ref)
			actions.CanRequeue = err == nil
			if err == nil {
				actions.RequeueReason = "Same pins and inputs; linked successor, remaining cumulative budget and fresh authority admission"
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

func registerConsoleQueuePresentation(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	for _, action := range []string{"archive", "unarchive", "requeue"} {
		e.POST("/console/queue/:item/"+action, consoleQueuePresentationHandler(svc, manager, action))
	}
}

func consoleQueuePresentationHandler(svc *app.Service, manager *console.Manager, action string) echo.HandlerFunc {
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
		sub, err := manager.AuthorizeMutation(request)
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.RequirePrincipal(sub); err != nil {
			return err
		}
		view, err := svc.GetQueueItemAs(request.Context(), sub, c.Param("item"))
		if err != nil {
			return err
		}
		if view.Item.Digest != values.Get("digest") || view.Submission.OwnerID != sub.PrincipalID {
			return app.ErrDenied
		}
		ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: view.Item.ItemID, Digest: view.Item.Digest}
		archive, err := svc.GetQueueArchiveAs(request.Context(), sub, ref)
		if err != nil {
			return err
		}
		if archive.Revision != revision {
			return app.ErrConflict
		}
		if !queuePresentationTerminal(view.Projection.State) {
			return app.ErrDenied
		}
		// Stable server-derived identity binds the canonical source, CAS and action;
		// caller cannot rotate a key or supply authority/task/budget fields.
		sum := sha256.Sum256([]byte(fmt.Sprintf("console-queue-v1:%s:%s:%s:%d:%s", sub.PrincipalID, ref.ID, ref.Digest, revision, action)))
		identity := fmt.Sprintf("console-queue-%x", sum)
		if action == "requeue" {
			result, err := svc.RequeueQueueItemAs(request.Context(), sub, app.RequeueQueueItemInput{QueueItem: ref, IdempotencyKey: identity})
			if err != nil {
				return err
			}
			if result.Execution != nil {
				return c.Redirect(http.StatusSeeOther, consoleRecordURL(consoleQueue, result.Execution.Item.ItemID))
			}
			return c.String(http.StatusOK, "Requeue admission: "+result.Reason+"; no completed execution asserted")
		}
		if action != "archive" && action != "unarchive" {
			return bad()
		}
		_, _, err = svc.SetQueueArchiveAs(request.Context(), sub, app.SetQueueArchiveInput{RequestID: identity, QueueItem: ref, Archived: action == "archive", ExpectedRevision: revision})
		if err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, consoleRecordURL(consoleQueue, ref.ID))
	}
}
