package api

import (
	"bytes"
	"errors"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/labstack/echo/v5"
)

func presentationPage(c *echo.Context, status int, title, message, back, forms string) error {
	return c.HTML(status, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><title>"+html.EscapeString(title)+"</title></head><body><main><h1>"+html.EscapeString(title)+"</h1><p>"+html.EscapeString(message)+"</p>"+forms+"<a href=\""+html.EscapeString(back)+"\">Return to records</a></main></body></html>")
}
func presentationFailure(c *echo.Context, manager *console.Manager, err error, back string) error {
	manager.ApplySecurityHeaders(c.Response().Header(), true)
	if c.Request().Method == http.MethodPost && !strings.Contains(c.Request().Header.Get("Accept"), "text/html") {
		return mapConsoleError(err)
	}
	status, code, _ := classifyError(mapConsoleError(err))
	if errors.Is(err, console.ErrReauthenticationRequired) {
		status, code = http.StatusUnauthorized, "principal_reauthentication_required"
	}
	if c.Request().Method == http.MethodPost {
		target := "loops"
		if back == "/console/queue" {
			target = "queue"
		}
		return c.Redirect(http.StatusSeeOther, "/console/presentation-error?status="+strconv.Itoa(status)+"&reason="+url.QueryEscape(code)+"&target="+target)
	}
	message := "No change was applied. The request was denied or the form is stale. Return to the record and review its current state."
	if status >= 500 {
		message = "Completion is not confirmed. Read the exact record to reconcile its current state. Do not resend or retry this action blindly."
	}
	return presentationPage(c, status, "Presentation action not completed", message, back, "")
}

// presentationHandler wraps ONLY Loop discovery and terminal Queue visibility.
// A retained confirmation consumes custody before invoking the ordinary handler;
// that handler repeats live authentication, ownership, target and durable CAS.
func presentationHandler(svc *app.Service, manager *console.Manager, back string, next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Body == nil || mediaErr != nil || media != "application/x-www-form-urlencoded" {
			return presentationFailure(c, manager, console.ErrInvalidInput, back)
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || len(raw) > 8192 {
			return presentationFailure(c, manager, echo.NewHTTPError(400, "invalid form"), back)
		}
		values, parseErr := url.ParseQuery(string(raw))
		path := r.URL.RequestURI()
		if parseErr == nil && values.Get("retained_action") != "" {
			if len(values) != 2 || len(values["csrf"]) != 1 || len(values["retained_action"]) != 1 {
				return presentationFailure(c, manager, console.ErrDenied, back)
			}
			r.Header.Set("X-CSRF-Token", values.Get("csrf"))
			retained, err := manager.ConsumePresentation(r, values.Get("retained_action"), path)
			if err != nil {
				return presentationFailure(c, manager, err, back)
			}
			if err = validatePresentation(c, svc, manager, retained); err != nil {
				return presentationFailure(c, manager, err, retained.Return)
			}
			values, _ = url.ParseQuery(retained.Payload)
			values.Set("csrf", r.Header.Get("X-CSRF-Token"))
			raw = []byte(values.Encode())
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		err = next(c)
		if err == nil {
			return nil
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") && (errors.Is(err, console.ErrReauthenticationRequired) || errors.Is(err, app.ErrUnauthenticated)) {
			token, retainErr := manager.RetainPresentation(r, path, back, string(raw))
			if retainErr == nil {
				return c.Redirect(http.StatusSeeOther, "/console/presentation/"+token)
			}
		}
		return presentationFailure(c, manager, err, back)
	}
}

func validatePresentation(c *echo.Context, svc *app.Service, manager *console.Manager, v console.PresentationReview) error {
	sub, err := manager.Authenticate(c.Request())
	if err != nil {
		return err
	}
	if sub.PrincipalID != v.PrincipalID {
		return app.ErrDenied
	}
	if err = svc.RequireDashboardPrincipal(sub); err != nil {
		return err
	}
	values, err := url.ParseQuery(v.Payload)
	if err != nil {
		return app.ErrDenied
	}
	u, err := url.ParseRequestURI(v.Path)
	if err != nil {
		return app.ErrDenied
	}
	if u.Path == "/console/loops/availability" {
		revision, err := strconv.ParseUint(values.Get("revision"), 10, 64)
		if err != nil {
			return app.ErrConflict
		}
		id := u.Query().Get("loop_id")
		view, err := svc.GetLoopViewAs(c.Request().Context(), sub, id, revision)
		if err != nil {
			return err
		}
		if view.Revision.Digest != values.Get("digest") {
			return app.ErrConflict
		}
		inventory, err := svc.ListLoopInventoryAs(c.Request().Context(), sub)
		if err != nil {
			return err
		}
		for _, item := range inventory {
			if item.Revision.LoopID == id && item.Revision.Revision > revision {
				return app.ErrConflict
			}
		}
		sequence, err := strconv.ParseUint(values.Get("sequence"), 10, 64)
		if err != nil || sequence != view.Availability.Sequence+1 || values.Get("previous_digest") != view.Availability.Digest || values.Get("expected_state") != string(view.Availability.State) {
			return app.ErrConflict
		}
		original, err := svc.GetLoopViewAs(c.Request().Context(), sub, id, 1)
		if err != nil {
			return err
		}
		_, err = svc.RegisteredAgentWorkspaceAs(c.Request().Context(), sub, original.Provenance.PublisherAgent.ID)
		return err
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 5 {
		return app.ErrDenied
	}
	view, err := svc.ConsoleQueuePresentationAs(c.Request().Context(), sub, parts[3], values.Get("digest"))
	if err != nil {
		return err
	}
	revision, err := strconv.ParseUint(values.Get("expected_revision"), 10, 64)
	if err != nil || revision != view.Revision {
		return app.ErrConflict
	}
	if !view.Terminal {
		return app.ErrDenied
	}
	return nil
}

func registerPresentationRecovery(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	e.GET("/console/presentation-error", func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		status, _ := strconv.Atoi(c.QueryParam("status"))
		if status < 400 || status > 599 {
			status = 400
		}
		reason := c.QueryParam("reason")
		allowed := map[string]bool{"denied": true, "unauthenticated": true, "principal_reauthentication_required": true, "conflict": true, "invalid_request": true, "internal_error": true, "unavailable": true, "bad_request": true}
		if !allowed[reason] {
			reason = "request_not_completed"
		}
		back := "/console/loops"
		if c.QueryParam("target") == "queue" {
			back = "/console/queue"
		}
		title := "Presentation action not completed"
		message := "Nothing was applied. Review the current record and form before making a new request. Reason: " + reason
		switch status {
		case http.StatusUnauthorized:
			title = "Sign-in required"
			message = "Fresh authentication is required. This action was not applied or retained. Sign in again, return to the exact record, and review a new form."
		case http.StatusForbidden:
			title = "Action denied"
			message = "Ownership, authorization, or the browser form's origin/CSRF proof did not pass. Nothing was applied. Return to the record; signing in does not grant ownership."
		case http.StatusConflict:
			title = "The record or form changed"
			message = "The exact publication, availability, or presentation revision no longer matches this form. Nothing was applied. Reload the record and review its current state; the action was not retargeted."
		case http.StatusBadRequest:
			title = "Invalid action request"
			message = "The submitted form was invalid. Nothing was applied. Return to the record to obtain its current form."
		}
		forms := ""
		if status == http.StatusUnauthorized {
			forms = "<a href=\"/console/reauthenticate\">Sign in again</a>"
		}
		if status >= 500 {
			title = "Service unavailable or completion unknown"
			message = "Completion is unknown. Read the record to reconcile; do not blindly resend this action. Reason: " + reason
		}
		return presentationPage(c, status, title, message, back, forms)
	})
	fail := func(c *echo.Context, err error) error { return presentationFailure(c, manager, err, "/console/loops") }
	render := func(c *echo.Context, status int, v console.PresentationReview, token, csrf, message string) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		prefix := "/console/presentation/" + token
		forms := ""
		if manager.PresentationReady(v) {
			forms = "<h2>Review exact retained action</h2><p>" + html.EscapeString(v.Path) + "</p><pre>" + html.EscapeString(v.Payload) + "</pre><form method=\"post\" action=\"" + html.EscapeString(v.Path) + "\"><input type=\"hidden\" name=\"csrf\" value=\"" + html.EscapeString(csrf) + "\"><input type=\"hidden\" name=\"retained_action\" value=\"" + token + "\"><button type=\"submit\">Confirm retained action</button></form>"
		} else {
			forms = "<p>Exact target: " + html.EscapeString(v.Path) + "</p><form id=\"presentation-reauth\" method=\"post\" action=\"" + prefix + "/reauthenticate\"><label for=\"password\">Principal password</label><input id=\"password\" name=\"password\" type=\"password\" autocomplete=\"current-password\" required><button type=\"submit\">Sign in and review</button></form>"
		}
		forms += "<form method=\"post\" action=\"" + prefix + "/cancel\"><input type=\"hidden\" name=\"csrf\" value=\"" + html.EscapeString(csrf) + "\"><button type=\"submit\">Cancel retained action</button></form>"
		return presentationPage(c, status, "Retained presentation action", message, v.Return, forms)
	}
	e.GET("/console/presentation/:token", func(c *echo.Context) error {
		token := c.Param("token")
		v, err := manager.Presentation(c.Request(), token)
		if err != nil {
			return fail(c, err)
		}
		csrf, err := manager.PresentationCSRF(c.Request(), token)
		if err != nil {
			return fail(c, err)
		}
		if manager.PresentationReady(v) {
			if err = validatePresentation(c, svc, manager, v); err != nil {
				return presentationFailure(c, manager, err, v.Return)
			}
		}
		return render(c, http.StatusOK, v, token, csrf, "Nothing has executed. Reauthentication only restores sign-in; explicitly confirm the exact retained action after review. Custody expires after two minutes and never renews authority.")
	})
	e.POST("/console/presentation/:token/reauthenticate", func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		token := c.Param("token")
		v, err := manager.Presentation(c.Request(), token)
		if err != nil || manager.PresentationReady(v) {
			return fail(c, console.ErrDenied)
		}
		password, err := decodeConsoleForm(c.Request(), "password")
		if err != nil || len(password) > 1024 {
			return fail(c, console.ErrInvalidInput)
		}
		secret := []byte(password)
		defer clear(secret)
		session, _, expires, sub, err := manager.Login(c.Request(), sourceKey(c.Request()), secret)
		if err != nil {
			csrf, _ := manager.PresentationCSRF(c.Request(), token)
			return render(c, http.StatusUnauthorized, v, token, csrf, "Password verification failed. Nothing executed and no deadline was renewed.")
		}
		if sub.PrincipalID != v.PrincipalID {
			manager.RevokeSessionValue(session)
			return fail(c, console.ErrDenied)
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), sub, "success", "principal_password_authenticated"); err != nil {
			manager.RevokeSessionValue(session)
			return fail(c, err)
		}
		if err = manager.RebindPresentation(c.Request(), token, session); err != nil {
			manager.RevokeSessionValue(session)
			return fail(c, err)
		}
		manager.Revoke(c.Request())
		manager.SetCookieUntil(c.Response(), session, expires)
		return c.Redirect(http.StatusSeeOther, "/console/presentation/"+token)
	})
	e.POST("/console/presentation/:token/cancel", func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		token := c.Param("token")
		v, err := manager.Presentation(c.Request(), token)
		if err != nil {
			return fail(c, err)
		}
		csrf, err := decodeConsoleForm(c.Request(), "csrf")
		if err != nil {
			return fail(c, err)
		}
		c.Request().Header.Set("X-CSRF-Token", csrf)
		if err = manager.CancelPresentation(c.Request(), token); err != nil {
			return fail(c, err)
		}
		return presentationPage(c, http.StatusOK, "Retained action cancelled", "Nothing was executed. The retained action has been discarded.", v.Return, "")
	})
}
