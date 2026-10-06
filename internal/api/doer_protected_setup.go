package api

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

// Native setup uses the same enrolled-password manager as the console, not
// transport UID as approval. No body is logged, persisted, or returned at login.
// The Unix peer restriction selects the owning instance, never the approver.
func registerDoerProtectedSetupRoutes(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	const purpose = "doer-protected-setup-v1"
	peer := func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
		if !ok {
			return app.ErrDenied
		}
		if _, err := svc.AuthenticateUnixPeer(c.Request().Context(), uid); err != nil {
			return app.ErrDenied
		}
		return manager.ValidateOrigin(c.Request(), true)
	}
	e.POST("/console/loops/doer/setup-native/login", func(c *echo.Context) error {
		if err := peer(c); err != nil {
			return mapConsoleError(err)
		}
		if c.Request().Header.Get("Content-Type") != "application/octet-stream" {
			return echo.NewHTTPError(400, "invalid protected login")
		}
		password, err := io.ReadAll(io.LimitReader(c.Request().Body, 1025))
		defer wipeBytes(password)
		if err != nil || len(password) == 0 || len(password) > 1024 {
			return echo.NewHTTPError(400, "invalid protected login")
		}
		// All local native login attempts share a stable throttle bucket. Neither a
		// claimed PID, per-command nonce nor caller-controlled header resets it.
		value, csrf, expires, sub, err := manager.Login(c.Request(), "doer-native-local", password)
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), sub, "success", "principal_password_authenticated"); err != nil {
			manager.RevokeSessionValue(value)
			return err
		}
		manager.SetCookieUntil(c.Response(), value, expires)
		return c.JSON(200, map[string]string{"csrf": csrf, "config_identity": svc.DoerContinuationConfigIdentity()})
	})
	authorize := func(c *echo.Context) (core.Subject, error) {
		if err := peer(c); err != nil {
			return core.Subject{}, err
		}
		return manager.AuthorizeMutation(c.Request())
	}
	e.POST("/console/loops/doer/setup-native/logout", func(c *echo.Context) error {
		if _, err := authorize(c); err != nil {
			return mapConsoleError(err)
		}
		manager.Revoke(c.Request())
		manager.ClearCookie(c.Response())
		return c.JSON(200, map[string]string{"status": "logged_out"})
	})
	e.POST("/console/loops/doer/setup-native/review", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var input app.DoerProtectedSetupInput
		if err = decode(c, &input); err != nil {
			return err
		}
		d, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, input.ID)
		if err != nil {
			return err
		}
		if d.Version != input.ExpectedVersion {
			return app.ErrConflict
		}
		review := app.DoerProtectedSetupReview{Action: input.Action, Draft: d}
		switch input.Action {
		case "successor":
			proposal, err := svc.ProposeDoerSetupSuccessorAs(c.Request().Context(), sub, d.ID, d.Version)
			if err != nil {
				return err
			}
			review.Proposal = &proposal
		case "host":
			if _, err := svc.ValidateDoerDraftPublicationAs(c.Request().Context(), sub, d.ID, d.Version); err != nil {
				return err
			}
			candidate, _, err := app.NewDoerLoopRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
			if err != nil {
				return err
			}
			digest, err := d.Contract.Digest()
			if err != nil {
				return err
			}
			review.Host = &app.DoerHostApprovalInput{DraftID: d.ID, DraftVersion: d.Version, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"}
		case "provision":
			if _, err := svc.ValidateDoerDraftPublicationAs(c.Request().Context(), sub, d.ID, d.Version); err != nil {
				return err
			}
			agent, err := svc.GetFleetAgentAs(c.Request().Context(), sub, d.Agent.ID, d.Agent.Revision)
			if err != nil {
				return err
			}
			preview, err := svc.PreviewPlanAs(c.Request().Context(), sub, agent.Revision.Charter.ID, agent.Revision.Charter.Revision, core.Environment{Name: "local"})
			if err != nil {
				return err
			}
			charter, err := svc.GetCharter(agent.Revision.Charter.ID, agent.Revision.Charter.Revision)
			if err != nil || charter.Digest != preview.CharterDigest {
				return app.ErrConflict
			}
			review.Plan, review.Charter, review.ProvisionReview = &preview.Plan, &charter, &preview
		default:
			return echo.NewHTTPError(400, "unsupported protected setup action")
		}
		payload, err := json.Marshal(review)
		if err != nil {
			return err
		}
		review.Receipt, err = manager.IssueReviewReceipt(c.Request(), purpose, payload)
		if err != nil {
			return mapConsoleError(err)
		}
		return c.JSON(200, review)
	})
	e.POST("/console/loops/doer/setup-native/decision", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var input app.DoerProtectedSetupDecision
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.Decision != "approve" && input.Decision != "reject" {
			return echo.NewHTTPError(400, "invalid exact decision")
		}
		payload, err := manager.ConsumeReviewReceipt(c.Request(), purpose, input.Receipt)
		if err != nil {
			return mapConsoleError(err)
		}
		var review app.DoerProtectedSetupReview
		if err = json.Unmarshal(payload, &review); err != nil {
			return err
		}
		d, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, review.Draft.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(d, review.Draft) {
			return app.ErrConflict
		}
		result := app.DoerProtectedSetupResult{Status: "rejected", Draft: d}
		if input.Decision == "reject" {
			return c.JSON(200, result)
		}
		switch review.Action {
		case "successor":
			if review.Proposal == nil {
				return app.ErrDenied
			}
			result.Draft, err = svc.ConfirmDoerSetupSuccessorAs(c.Request().Context(), sub, *review.Proposal)
			if err == nil {
				view, e := svc.GetFleetAgentAs(c.Request().Context(), sub, result.Draft.Agent.ID, result.Draft.Agent.Revision)
				err = e
				if e == nil {
					result.Successor = &view.Revision
				}
			}
		case "host":
			if review.Host == nil {
				return app.ErrDenied
			}
			grant, e := svc.ApproveDoerHostWriteAs(c.Request().Context(), sub, *review.Host)
			err = e
			if err == nil {
				result.Host = &grant
			}
		case "provision":
			if review.Plan == nil || review.Charter == nil {
				return app.ErrDenied
			}
			if _, err = svc.ValidateDoerDraftPublicationAs(c.Request().Context(), sub, d.ID, d.Version); err != nil {
				return err
			}
			plan, e := svc.GetPlan(review.Plan.ID)
			if e != nil || !reflect.DeepEqual(plan, *review.Plan) {
				return app.ErrConflict
			}
			agent, e := svc.GetFleetAgentAs(c.Request().Context(), sub, d.Agent.ID, d.Agent.Revision)
			if e != nil || plan.CharterDigest != agent.Revision.Charter.Digest || plan.AgentID != agent.Revision.Charter.ID || plan.Revision != agent.Revision.Charter.Revision {
				return app.ErrConflict
			}
			approval, e := svc.RequestApprovalAs(c.Request().Context(), sub, plan.ID, 5*time.Minute)
			if e != nil {
				return e
			}
			if _, e = svc.DecideApprovalAs(c.Request().Context(), sub, approval.ID, true); e != nil {
				return e
			}
			receipt, e := svc.ApplyAs(c.Request().Context(), sub, plan.ID, approval.ID)
			if e != nil {
				return e
			}
			verified, e := svc.GetReceipt(receipt.ID)
			if e != nil || verified.Status != "verified" || verified.PlanID != plan.ID || verified.PlanDigest != plan.Digest || verified.CharterDigest != plan.CharterDigest {
				return app.ErrConflict
			}
			result.Provisioning = &verified
		default:
			return app.ErrDenied
		}
		if err != nil {
			return err
		}
		// Read the exact retained target rather than treating a transport success as
		// completion. No successor action includes provisioning, host consent or Run.
		readback, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, result.Draft.ID)
		if err != nil || !reflect.DeepEqual(readback, result.Draft) {
			return app.ErrConflict
		}
		result.Status = "approved"
		return c.JSON(http.StatusOK, result)
	})
}
