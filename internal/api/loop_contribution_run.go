package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

type contributionRunForm struct {
	CSRF, Digest, Issue, Action, Delivery string
	Revision                              uint64
}

func decodeContributionRunForm(r *http.Request) (contributionRunForm, error) {
	var out contributionRunForm
	deny := errors.New("invalid contribution form")
	if !isConsoleForm(r) || r.Body == nil || r.ContentLength > 4096 {
		return out, deny
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	if err != nil || len(b) > 4096 {
		return out, deny
	}
	v, err := url.ParseQuery(string(b))
	if err != nil {
		return out, deny
	}
	for k, values := range v {
		if len(values) != 1 {
			return out, deny
		}
		switch k {
		case "csrf", "revision", "digest", "github_issue_url", "action", "delivery":
		default:
			return out, deny
		}
	}
	out = contributionRunForm{CSRF: v.Get("csrf"), Digest: v.Get("digest"), Issue: v.Get("github_issue_url"), Action: v.Get("action"), Delivery: v.Get("delivery")}
	out.Revision, err = strconv.ParseUint(v.Get("revision"), 10, 64)
	if err != nil || out.Revision == 0 || out.CSRF == "" || out.Digest == "" {
		return out, deny
	}
	if _, err = contribution.ParseIssueURL(out.Issue); err != nil {
		return out, deny
	}
	switch out.Action {
	case "run", "resume", "status", "stop", "protected-review":
		if out.Delivery != "" {
			return out, deny
		}
	case "narrow":
		if out.Delivery != "local-only" && out.Delivery != "no-publish" && out.Delivery != "no-merge" {
			return out, deny
		}
	default:
		return out, deny
	}
	return out, nil
}

// Run is not Graph preparation: the live owning Console authenticates the
// principal and derives the session, exact Loop and stable intent server-side.
// All role admission, decisions, host policy, budgets and final verification
// remain in the same shared contribution journey used by /contribute.
func consoleContributionRunHandler(svc *app.Service, manager *console.Manager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		f, err := decodeContributionRunForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid contribution form")
		}
		c.Request().Header.Set("X-CSRF-Token", f.CSRF)
		sub, session, err := manager.AuthorizeCommand(c.Request())
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.RequirePrincipal(sub); err != nil {
			return err
		}
		ctx := c.Request().Context()
		lv, err := svc.GetLoopViewAs(ctx, sub, c.QueryParam("loop_id"), f.Revision)
		if err != nil {
			return err
		}
		if lv.Revision.Digest != f.Digest || lv.Revision.Contribution == nil {
			return app.ErrDenied
		}
		selected := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lv.Revision.LoopID, Revision: f.Revision, Digest: f.Digest}
		key := app.ContributionIntentKey(selected, f.Issue, "")
		outcome, reason, queueID := "observed", "durable_contribution_readback", ""
		switch f.Action {
		case "run", "resume":
			contract, e := svc.AdmitContributionAs(ctx, sub, session, selected, f.Issue, key, "")
			if e != nil {
				// Preparation can retain a genuine independent decision before
				// Queue admission. Preserve navigation, never expose its error
				// or issue/planning transcripts, and never mint authority here.
				if _, held := svc.ReadContributionEngineeringAs(ctx, sub, key); held != nil {
					return e
				}
				outcome, reason = "blocked", "contribution_preparation_held"
				break
			}
			queueID = contract.QueueItemID
			var journey app.ContributionJourneyResult
			if f.Action == "resume" {
				journey, e = svc.ResumeContributionJourneyAs(ctx, sub, key)
			} else {
				journey, e = svc.ExecuteContributionJourneyAs(ctx, sub, key)
			}
			reason = journey.Execution.Reason
			if journey.HeldReason != "" {
				reason = journey.HeldReason
			}
			if e != nil {
				outcome = "uncertain"
				reason = "contribution_journey_interrupted"
			} else {
				outcome = "accepted"
			}
		case "protected-review":
			status, e := svc.ReadContributionEngineeringAs(ctx, sub, key)
			if e != nil || status.Control.Stop || status.Decision == nil || svc.ContributionDecisionDelivery == nil {
				return app.ErrDenied
			}
			if _, e = svc.ContributionDecisionDelivery(ctx, sub, key, status.Decision.Role); e != nil {
				return e
			}
		case "stop", "narrow":
			if err = svc.ControlContributionAs(ctx, sub, session, key, f.Action == "stop", f.Delivery); err != nil {
				return err
			}
		}
		run := &consoleweb.ContributionRunModel{LoopID: lv.Revision.LoopID, Revision: f.Revision, Digest: f.Digest, Issue: f.Issue}
		contract, view, readErr := svc.ReadContributionAs(ctx, sub, key)
		if readErr == nil {
			queueID = contract.QueueItemID
			outcome = string(view.Projection.State)
		} else {
			status, held := svc.ReadContributionEngineeringAs(ctx, sub, key)
			if held != nil {
				return readErr
			}
			outcome, reason = "blocked", "contribution_preparation_held"
			run.Delivery, run.Stopped = status.Control.Delivery, status.Control.Stop
			if status.Decision != nil {
				run.Decision = contributionDecisionProjection(*status.Decision, svc.Now())
			}
		}
		if control, e := svc.ReadContributionControlAs(ctx, sub, key); e == nil {
			run.Delivery, run.Stopped = control.Delivery, control.Stop
			if control.Stop {
				outcome, reason = "stopped", "contribution_stop_retained"
			}
		}
		resultURL := ""
		if queueID != "" {
			resultURL = consoleRecordURL(consoleQueue, queueID)
		}
		page := consoleweb.PageModel{Authenticated: true, CSRF: f.CSRF, Surface: consoleweb.SurfaceModel{Domain: "loops", Title: "Loops"}, CommandReceipt: &consoleweb.OperationReceiptModel{Title: "Contribution " + f.Action, Outcome: outcome, OperationID: key, ReasonCode: reason, Message: "Read authoritative Queue disposition; candidate or request acceptance is not whole-contribution completion.", ResultURL: resultURL, ResultLabel: "View authoritative Queue execution"}}
		page.ContributionRun = run
		if queueID == "" {
			page.CommandReceipt.Message = "Retained preparation only: no Queue admission or contribution completion. Read the exact independent decision and current delivery restrictions below."
		}
		content, err := renderConsole(ctx, consoleweb.Document(page))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	}
}
