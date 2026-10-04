package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"

	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

// A reload after a lost response must retain the same request identity.
// Rotating it is an explicit new-run action, never a page-load side effect.
func consoleLoopRunKey(c *echo.Context, subject core.Subject, record *consoleweb.RecordModel) (string, error) {
	identity := sha256.Sum256([]byte(subject.PrincipalID + "\x00" + record.Label + "\x00" + record.Revision + "\x00" + record.Digest))
	name := "aegis_loop_run_" + hex.EncodeToString(identity[:12])
	if c.QueryParam("new_run") != "1" {
		if cookie, err := c.Request().Cookie(name); err == nil && strings.HasPrefix(cookie.Value, "loop-run-") && len(cookie.Value) == len("loop-run-")+32 {
			if _, err := hex.DecodeString(strings.TrimPrefix(cookie.Value, "loop-run-")); err == nil {
				return cookie.Value, nil
			}
		}
	}
	key, err := randomConsoleID("loop-run")
	if err != nil {
		return "", err
	}
	http.SetCookie(c.Response(), &http.Cookie{Name: name, Value: key, Path: "/console/loops", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: c.Request().TLS != nil || c.Request().Header.Get("X-Forwarded-Proto") == "https"})
	return key, nil
}

// The browser selects only an immutable revision and a reusable request key.
// Authority, publisher and workspace are resolved from authenticated state.
type consoleLoopRunForm struct {
	CSRF, Digest, IdempotencyKey string
	Revision                     uint64
}

func decodeConsoleLoopRunForm(request *http.Request) (consoleLoopRunForm, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > 4096 {
		return consoleLoopRunForm{}, errors.New("invalid Loop run form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 4097))
	if err != nil || len(body) > 4096 {
		return consoleLoopRunForm{}, errors.New("invalid Loop run form")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) != 4 {
		return consoleLoopRunForm{}, errors.New("invalid Loop run form")
	}
	for _, key := range []string{"csrf", "revision", "digest", "idempotency_key"} {
		if len(values[key]) != 1 || values.Get(key) == "" {
			return consoleLoopRunForm{}, errors.New("invalid Loop run form")
		}
	}
	revision, err := strconv.ParseUint(values.Get("revision"), 10, 64)
	key := values.Get("idempotency_key")
	if err != nil || revision == 0 || len(key) > 256 || strings.TrimSpace(key) != key {
		return consoleLoopRunForm{}, errors.New("invalid Loop run form")
	}
	return consoleLoopRunForm{CSRF: values.Get("csrf"), Revision: revision, Digest: values.Get("digest"), IdempotencyKey: key}, nil
}

func consoleLoopRunHandler(svc *app.Service, manager *console.Manager, portals ...*DoerContinuationPortal) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		form, err := decodeConsoleLoopRunForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Loop run form")
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, sessionID, err := manager.AuthorizeCommand(c.Request())
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		ctx := c.Request().Context()
		view, err := svc.GetLoopViewAs(ctx, subject, c.QueryParam("loop_id"), form.Revision)
		if err != nil {
			return err
		}
		if view.Revision.Digest != form.Digest || view.Revision.Doer == nil || view.Revision.DoerReusable != nil {
			return app.ErrDenied
		}
		publisher := view.Provenance.PublisherAgent
		agent, err := svc.FleetRepository.GetAgentRevision(ctx, publisher.ID, publisher.Revision)
		if err != nil || agent.Digest != publisher.Digest {
			return app.ErrDenied
		}
		run := app.QueueLoopInput{
			Agent: app.RevisionReference(publisher.ID, publisher.Revision, publisher.Digest), Loop: app.RevisionReference(view.Revision.LoopID, view.Revision.Revision, view.Revision.Digest),
			IdempotencyKey: form.IdempotencyKey, Activate: true,
		}
		var result app.QueueLoopResult
		if len(portals) > 0 {
			if pending, ok := portals[0].exact(ctx, subject, sessionID, run); ok {
				result, err = svc.QueueDoerContinuationAs(ctx, subject, sessionID, pending.ApprovalID, pending.Review.Intent)
			} else {
				result, err = svc.QueueLoopAs(ctx, subject, run)
			}
		} else {
			result, err = svc.QueueLoopAs(ctx, subject, run)
		}
		if err != nil && (result.RequestID == "" || errors.Is(err, app.ErrDenied) || app.IsFleetDenied(err) || errors.Is(err, app.ErrConflict) || app.IsFleetConflict(err)) {
			return err
		}
		outcome, message, resultURL := "blocked", fmt.Sprintf("Prerequisite reason: %s · request_id: %s · required_action: %s", result.Reason, result.RequestID, result.RequiredAction), ""
		if err != nil {
			outcome, message = "uncertain", "Request interrupted or failed after admission may have begun. Read back or resume this exact request before starting another. Request ID: "+result.RequestID
		}
		if result.Rejection != nil {
			outcome, message = "rejected", fmt.Sprintf("Durable submission rejection %s · request_id: %s · reason: %s", result.Rejection.RejectionID, result.RequestID, result.Rejection.ReasonCode)
		}
		if result.RequiredCharter != nil {
			message += fmt.Sprintf(" · required_charter: %s r%d @ %s", result.RequiredCharter.ID, result.RequiredCharter.Revision, result.RequiredCharter.Digest)
		}
		if result.QueueItemID != "" {
			execution, readErr := svc.GetQueueItemAs(ctx, subject, result.QueueItemID)
			if readErr != nil && err == nil {
				return readErr
			}
			if readErr == nil {
				outcome = string(execution.Projection.State)
				message = fmt.Sprintf("Queue item %s · state %s · numbered attempts %d · verification receipts %d", execution.Item.ItemID, execution.Projection.State, len(execution.Attempts), len(execution.Receipts))
				if execution.Artifact != nil {
					message += " · selected-file artifact " + execution.Artifact.ID
				}
				if execution.Disposition != nil {
					message += fmt.Sprintf(" · terminal disposition %s (%s)", execution.Disposition.State, execution.Disposition.ReasonCode)
				}
				resultURL = consoleRecordURL(consoleQueue, execution.Item.ItemID)
			}
		}
		page := consoleweb.PageModel{Authenticated: true, CSRF: form.CSRF, Surface: consoleweb.SurfaceModel{Domain: "loops", Title: "Loops"}, CommandReceipt: &consoleweb.OperationReceiptModel{Title: "Doer Loop Run", Outcome: outcome, OperationID: result.RequestID, ReasonCode: result.Reason, Message: message, ResultURL: resultURL, ResultLabel: "View authoritative Queue execution", RetryURL: "/console/loops/run?loop_id=" + url.QueryEscape(view.Revision.LoopID), RetryCSRF: form.CSRF, RetryKey: form.IdempotencyKey, RetryDigest: form.Digest, RetryRevision: form.Revision}}
		content, err := renderConsole(ctx, consoleweb.Document(page))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	}
}
