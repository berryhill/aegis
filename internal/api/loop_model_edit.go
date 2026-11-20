package api

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/labstack/echo/v5"
)

// Machine-facing proposal/read/resume only. There is deliberately no approval
// route on the peer-authenticated API and no approval flag in the input.
func registerLoopModelEditRoutes(g *echo.Group, svc *app.Service) {
	g.POST("/loops/edits", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		var in app.PublishLoopInput
		if err = decode(c, &in); err != nil {
			return err
		}
		result, err := svc.SubmitOwnedLoopEditAs(c.Request().Context(), sub, in)
		if err != nil {
			return err
		}
		if result.PendingEdit != nil {
			return c.JSON(http.StatusAccepted, result)
		}
		return c.JSON(createdOrOK(result.Decision.Idempotent), result)
	})
	g.POST("/loops/:loop/:revision/model-edit", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		revision, err := requiredRevision(c.Param("revision"))
		if err != nil {
			return err
		}
		var in app.OwnedLoopModelUpdateInput
		if err = decode(c, &in); err != nil {
			return err
		}
		result, err := svc.UpdateOwnedLoopModelAs(c.Request().Context(), sub, c.Param("loop"), revision, in)
		if err != nil {
			return err
		}
		if result.PendingEdit != nil {
			return c.JSON(http.StatusAccepted, result)
		}
		return c.JSON(createdOrOK(result.Decision.Idempotent), result)
	})
	g.GET("/loops/edits/:id", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		result, err := svc.ReadOwnedLoopEditAs(c.Request().Context(), sub, c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, result)
	})
	g.POST("/loops/edits/:id/publish", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		if c.Request().ContentLength != 0 {
			return c.String(http.StatusBadRequest, "No publication overrides accepted")
		}
		result, err := svc.PublishOwnedLoopEditAs(c.Request().Context(), sub, c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(createdOrOK(result.Decision.Idempotent), result)
	})
}

type loopEditPage struct {
	CSRF, ID, Provider, Model, Candidate, Original, Proposed, Receipt, Message, Publication string
	Reviewable                                                                              bool
}

var loopEditTemplate = template.Must(template.New("loop-model-edit").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Review Loop model permission · Aegis</title></head><body><main>
<h1>Loop model edit</h1><p>{{.Message}}</p>{{if .Publication}}<h2>Already published</h2><pre>{{.Publication}}</pre>{{else}}<p>Requested model: <strong>{{.Provider}} / {{.Model}}</strong>.</p>
<p>The existing default is preserved. This decision adds only this model lane to one exact charter successor. It does not connect a provider, provision, grant tools or credentials, publish, activate, or execute the Loop. Existing runtime checks still apply.</p>{{end}}
<details open><summary>Exact retained Loop edit</summary><pre>{{.Candidate}}</pre></details>
<details><summary>Current approved charter</summary><pre>{{.Original}}</pre></details>
<details open><summary>Exact proposed charter</summary><pre>{{.Proposed}}</pre></details>
{{if .Reviewable}}<form method="post" action="/console/loops/model-edit"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="edit_id" value="{{.ID}}">
{{if .Receipt}}<input type="hidden" name="receipt" value="{{.Receipt}}"><button name="action" value="confirm">Approve this exact model permission</button><button name="action" value="reject">Reject this decision</button>{{else}}<button name="action" value="review">Review exact model permission</button>{{end}}</form>{{end}}
<p><a href="/console/loops">Return to Loops</a></p></main></body></html>`))

func registerLoopModelEditConsole(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	render := func(c *echo.Context, edit app.LoopModelEdit, receipt, message string) error {
		csrf, err := manager.CSRF(c.Request())
		if err != nil {
			return c.String(403, "Authentication required")
		}
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		var body bytes.Buffer
		page := loopEditPage{CSRF: csrf, ID: edit.ID, Provider: edit.MissingModel.Provider, Model: edit.MissingModel.Model, Candidate: setupJSON(edit.Input.Revision), Original: setupJSON(edit.Original), Proposed: setupJSON(edit.Proposed), Receipt: receipt, Message: message}
		page.Reviewable = edit.Status == "pending"
		switch edit.Status {
		case "published":
			page.Publication = setupJSON(edit.Publication)
			page.Message = "Already published. Original immutable publication and provenance; no further model permission decision is needed."
		case "approved":
			page.Message = "Exact model permission approved. The requesting owner can publish the same retained edit with loops edit-publish " + edit.ID + ". No Loop was published or run by this decision."
		case "expired", "stale":
			page.Message = "Historical " + edit.Status + " proposal. This review cannot grant authority or publish."
		}
		if err = loopEditTemplate.Execute(&body, page); err != nil {
			return err
		}
		return c.HTMLBlob(http.StatusOK, body.Bytes())
	}
	e.GET("/console/loops/model-edit", func(c *echo.Context) error {
		sub, err := manager.Authenticate(c.Request())
		if err != nil {
			return c.Redirect(http.StatusSeeOther, "/console/login")
		}
		edit, err := svc.ReadOwnedLoopEditAs(c.Request().Context(), sub, c.QueryParam("edit_id"))
		if err != nil {
			return err
		}
		return render(c, edit, "", "Pending proposal only. Review and confirm independently; the requesting agent cannot approve it.")
	})
	e.POST("/console/loops/model-edit", func(c *echo.Context) error {
		req := c.Request()
		if !isConsoleForm(req) {
			return c.String(400, "Invalid form")
		}
		raw, err := io.ReadAll(io.LimitReader(req.Body, 65537))
		if err != nil || len(raw) > 65536 {
			return c.String(400, "Invalid form")
		}
		values, err := url.ParseQuery(string(raw))
		if err != nil {
			return c.String(400, "Invalid form")
		}
		for k, v := range values {
			if len(v) != 1 || (k != "csrf" && k != "edit_id" && k != "action" && k != "receipt") {
				return c.String(400, "Invalid form")
			}
		}
		req.Header.Set("X-CSRF-Token", values.Get("csrf"))
		sub, err := manager.AuthorizeMutation(req)
		if err != nil {
			return c.String(403, "Fresh authentication, origin and CSRF required")
		}
		// Sensitive approval retains the shorter principal freshness policy, not
		// merely the dashboard navigation/action TTL.
		if err = svc.RequirePrincipal(sub); err != nil {
			return err
		}
		edit, err := svc.ReadOwnedLoopEditAs(req.Context(), sub, values.Get("edit_id"))
		if err != nil {
			return err
		}
		switch values.Get("action") {
		case "review":
			if edit.Status != "pending" {
				return app.ErrConflict
			}
			token, err := manager.IssueReviewReceipt(req, "loop-model-edit", []byte(setupJSON(edit)))
			if err != nil {
				return err
			}
			return render(c, edit, token, "Confirm only the displayed exact authority change. No runtime or publication consent is included.")
		case "confirm", "reject":
			raw, err := manager.ConsumeReviewReceipt(req, "loop-model-edit", values.Get("receipt"))
			if err != nil {
				return c.String(403, "Fresh exact review required")
			}
			var review app.LoopModelEdit
			if json.Unmarshal(raw, &review) != nil || review.ID != edit.ID {
				return c.String(409, "Review changed")
			}
			if edit.Status != "pending" {
				return app.ErrConflict
			}
			if values.Get("action") == "reject" {
				return render(c, edit, "", "Decision rejected. The inert edit and existing authority are unchanged.")
			}
			if _, err = svc.ConfirmOwnedLoopModelAs(req.Context(), sub, review); err != nil {
				return err
			}
			edit, err = svc.ReadOwnedLoopEditAs(req.Context(), sub, edit.ID)
			if err != nil {
				return err
			}
			return render(c, edit, "", "Exact model permission approved. The requesting owner can now publish the same retained edit with loops edit-publish "+edit.ID+". No Loop was published or run by this decision.")
		default:
			return c.String(400, "Invalid action")
		}
	})
}
