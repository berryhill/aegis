package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"

	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

type doerSetupIntent struct {
	Draft                          app.DoerDraft
	Action                         string
	Charter                        app.ExactRevisionReference
	PlanID, PlanDigest, ApprovalID string
	Host                           app.DoerHostApprovalInput
}

// registerDoerSetupRoutes is intentionally wired by the server owner. All
// authority originates in the authenticated browser manager, never local OS auth.
func registerDoerSetupRoutes(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	const base = "/console/loops/doer/setup"
	render := func(c *echo.Context, model consoleweb.DoerSetupModel) error {
		csrf, err := manager.CSRF(c.Request())
		if err != nil {
			return c.String(403, "Authentication required")
		}
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		data, err := renderConsole(c.Request().Context(), consoleweb.Document(consoleweb.PageModel{Authenticated: true, CSRF: csrf, DoerSetup: &model, Surface: consoleweb.SurfaceModel{Domain: consoleweb.DomainLoops, Title: "Doer setup"}}))
		if err != nil {
			return err
		}
		return c.HTMLBlob(200, data)
	}
	modelFor := func(d app.DoerDraft) consoleweb.DoerSetupModel {
		return consoleweb.DoerSetupModel{DraftID: d.ID, DraftURL: "/console/loops/doer?draft_id=" + url.QueryEscape(d.ID), SetupURL: base + "?draft_id=" + url.QueryEscape(d.ID), Contract: setupJSON(d.Contract)}
	}
	e.GET(base, func(c *echo.Context) error {
		sub, err := manager.Authenticate(c.Request())
		if err != nil {
			return c.String(403, "Authentication required")
		}
		d, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, c.QueryParam("draft_id"))
		if err != nil {
			return c.String(403, "Draft unavailable")
		}
		m := modelFor(d)
		exact, err := svc.GetFleetAgentAs(c.Request().Context(), sub, d.Agent.ID, d.Agent.Revision)
		if grant, grantErr := svc.ReadDoerHostWriteApprovalAs(c.Request().Context(), sub, d.Agent, d.Contract); grantErr == nil {
			m.HostStatus = "Exact signed host-write approval verified until " + grant.ExpiresAt.UTC().Format(time.RFC3339)
		} else {
			m.HostStatus = "No current verified host-write approval is asserted. Review the exact contract independently below."
		}
		if err != nil || exact.Revision.Digest != d.Agent.Digest {
			m.Message = "Exact Agent unavailable"
			return render(c, m)
		}
		currentCharter, err := svc.GetCharter(exact.Revision.Charter.ID, exact.Revision.Charter.Revision)
		if err != nil || currentCharter.Digest != exact.Revision.Charter.Digest {
			m.Message = "Exact canonical charter unavailable"
			return render(c, m)
		}
		m.Current = setupJSON(exact.Revision) + "\n" + setupJSON(currentCharter.Charter)
		receipts, err := svc.ListReceipts()
		if err != nil {
			m.Message = "Provisioning receipt readback unavailable"
			return render(c, m)
		}
		for _, r := range receipts {
			if r.CharterDigest == currentCharter.Digest && r.Status == "verified" {
				p, err := svc.GetPlan(r.PlanID)
				if err == nil && p.ID == r.PlanID && p.Digest == r.PlanDigest && p.CharterDigest == r.CharterDigest && p.AgentID == currentCharter.Charter.AgentID && p.Revision == currentCharter.Charter.Revision {
					m.Current += "\nExact verified provisioning receipt:\n" + setupJSON(r)
					m.Provisioned = true
				}
			}
		}
		cs, err := svc.ListCharters(d.Agent.ID)
		if err != nil {
			m.Message = "Imported canonical charters unavailable"
			return render(c, m)
		}
		for _, ch := range cs {
			canonical, err := core.Canonicalize(ch.Charter)
			if err != nil || canonical.Digest != ch.Digest {
				continue
			}
			ref := app.RevisionReference(ch.Charter.AgentID, ch.Charter.Revision, ch.Digest)
			if ref.Revision > exact.Revision.Charter.Revision && ch.Charter.Runtime.Adapter == exact.Revision.Runtime.Adapter && ch.Charter.Runtime.Runtime == exact.Revision.Runtime.Runtime && ch.Charter.Runtime.Target == exact.Revision.Runtime.Target {
				m.Charters = append(m.Charters, consoleweb.DoerSetupCharterModel{Ref: setupJSON(ref), Details: setupJSON(ch.Charter)})
			}
		}
		if len(m.Charters) == 0 {
			m.Message = "No imported compatible successor charter is available. This setup cannot import a charter or create/configure a model."
		}
		return render(c, m)
	})
	e.POST(base, func(c *echo.Context) error {
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
			if len(v) != 1 || (k != "csrf" && k != "draft_id" && k != "action" && k != "charter" && k != "receipt") {
				return c.String(400, "Invalid form")
			}
		}
		req.Header.Set("X-CSRF-Token", values.Get("csrf"))
		sub, err := manager.AuthorizeMutation(req)
		if err != nil {
			return c.String(403, "Fresh authentication, origin and CSRF required")
		}
		d, err := svc.ReadDoerDraftAs(req.Context(), sub, values.Get("draft_id"))
		if err != nil {
			return c.String(403, "Draft unavailable")
		}
		m := modelFor(d)
		fail := func(err error) error {
			m.Message = "Setup did not complete: " + err.Error() + ". Review fresh setup before continuing."
			return render(c, m)
		}
		issue := func(in doerSetupIntent, label string) error {
			token, err := manager.IssueReviewReceipt(req, "doer-setup", []byte(setupJSON(in)))
			if err != nil {
				return fail(err)
			}
			m.Receipt, m.ConfirmLabel = token, label
			return render(c, m)
		}
		action := values.Get("action")
		in := doerSetupIntent{Draft: d, Action: action}
		if action == "host-review" {
			if _, err := svc.ValidateDoerDraftPublicationAs(req.Context(), sub, d.ID, d.Version); err != nil {
				return fail(err)
			}
			candidate, _, err := app.NewDoerLoopRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
			if err != nil {
				return fail(err)
			}
			digest, err := d.Contract.Digest()
			if err != nil {
				return fail(err)
			}
			in.Action = "host-approve"
			in.Host = app.DoerHostApprovalInput{DraftID: d.ID, DraftVersion: d.Version, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"}
			m.Preview = "Exact host-write contract:\n" + setupJSON(d.Contract) + "\nCandidate digest: " + candidate.Digest + "\nContract digest: " + digest + "\nMaximum approval lifetime: 24 hours. No model, credential, provisioning, session, Graph, or native-test authority is granted."
			return issue(in, "Approve this exact host-write contract (maximum 24 hours)")
		}
		if action == "successor-review" {
			if err := json.Unmarshal([]byte(values.Get("charter")), &in.Charter); err != nil || in.Charter.Validate() != nil {
				return fail(app.ErrConflict)
			}
			ch, err := svc.GetCharter(in.Charter.ID, in.Charter.Revision)
			if err != nil || ch.Digest != in.Charter.Digest || in.Charter.ID != d.Agent.ID {
				return fail(app.ErrConflict)
			}
			m.Preview = setupJSON(ch.Charter)
			in.Action = "successor"
			return issue(in, "Approve exact Agent successor and rebind this retained draft")
		}
		if action == "preview" {
			exact, err := svc.ValidateDoerDraftPublicationAs(req.Context(), sub, d.ID, d.Version)
			if err != nil {
				return fail(err)
			}
			agent, err := svc.GetFleetAgentAs(req.Context(), sub, exact.Agent.ID, exact.Agent.Revision)
			if err != nil {
				return fail(err)
			}
			review, err := svc.PreviewPlanAs(req.Context(), sub, agent.Revision.Charter.ID, agent.Revision.Charter.Revision, core.Environment{Name: "local"})
			if err != nil {
				return fail(err)
			}
			if review.CharterDigest != agent.Revision.Charter.Digest {
				return fail(app.ErrConflict)
			}
			charter, err := svc.GetCharter(agent.Revision.Charter.ID, agent.Revision.Charter.Revision)
			if err != nil || charter.Digest != review.CharterDigest {
				return fail(app.ErrConflict)
			}
			m.Preview = setupJSON(review) + "\nComplete per-stanza declarations:\n" + setupJSON(charter.Charter)
			in.Action = "request"
			in.PlanID, in.PlanDigest = review.Plan.ID, review.PlanDigest
			return issue(in, "Request independent decision for this exact provisioning plan")
		}
		payload, err := manager.ConsumeReviewReceipt(req, "doer-setup", values.Get("receipt"))
		if err != nil {
			return fail(err)
		}
		if err = json.Unmarshal(payload, &in); err != nil || in.Draft.ID != d.ID || in.Draft.Version != d.Version || in.Draft.Agent != d.Agent {
			return fail(app.ErrConflict)
		}
		if in.Action != "decision" && action != "confirm" {
			return fail(app.ErrConflict)
		}
		if in.Action == "successor" {
			next, err := svc.ApproveDoerDraftSuccessorAs(req.Context(), sub, d.ID, d.Version, app.ApproveAgentCharterInput{Expected: d.Agent, Charter: in.Charter})
			if err != nil {
				return fail(err)
			}
			m = modelFor(next)
			m.Message = "Exact successor and retained draft binding read back. Prior binding archived; no Run intent changed. Fresh provisioning/publication preview required."
			return render(c, m)
		}
		if in.Action == "host-approve" {
			grant, err := svc.ApproveDoerHostWriteAs(req.Context(), sub, in.Host)
			if err != nil {
				return fail(err)
			}
			m.Message = "Authoritative signed host-write approval read back: " + setupJSON(grant) + ". This did not publish, activate, issue a session, or execute the Loop."
			return render(c, m)
		}
		if _, err := svc.ValidateDoerDraftPublicationAs(req.Context(), sub, d.ID, d.Version); err != nil {
			return fail(err)
		}
		agent, err := svc.GetFleetAgentAs(req.Context(), sub, d.Agent.ID, d.Agent.Revision)
		if err != nil {
			return fail(err)
		}
		plan, err := svc.GetPlan(in.PlanID)
		if err != nil || plan.ID != in.PlanID || plan.Digest != in.PlanDigest || plan.AgentID != agent.Revision.Charter.ID || plan.Revision != agent.Revision.Charter.Revision || plan.CharterDigest != agent.Revision.Charter.Digest {
			return fail(app.ErrConflict)
		}
		m.Preview = setupJSON(plan)
		if in.Action == "request" {
			a, err := svc.RequestApprovalAs(req.Context(), sub, plan.ID, 5*time.Minute)
			if err != nil {
				return fail(err)
			}
			a, err = svc.GetApproval(a.ID)
			if err != nil {
				return fail(err)
			}
			in.ApprovalID = a.ID
			in.Action = "decision"
			m.Approval = setupJSON(a)
			return issue(in, "Approve this exact provisioning plan")
		}
		a, err := svc.GetApproval(in.ApprovalID)
		if err != nil || a.ID != in.ApprovalID || a.PlanID != plan.ID || a.PlanDigest != plan.Digest || a.CharterDigest != plan.CharterDigest || a.RequestedBy != sub.PrincipalID {
			return fail(app.ErrConflict)
		}
		if in.Action == "decision" {
			if action != "confirm" && action != "reject" {
				return fail(app.ErrConflict)
			}
			a, err = svc.DecideApprovalAs(req.Context(), sub, a.ID, action == "confirm")
			if err != nil {
				return fail(err)
			}
			a, err = svc.GetApproval(a.ID)
			if err != nil {
				return fail(err)
			}
			m.Approval = setupJSON(a)
			if a.Status == "rejected" {
				m.Message = "Exact plan rejected; no provisioning effects applied."
				return render(c, m)
			}
			in.Action = "apply"
			return issue(in, "Apply separately approved exact provisioning plan")
		}
		if in.Action == "apply" && action == "confirm" {
			r, err := svc.ApplyAs(req.Context(), sub, plan.ID, a.ID)
			if err != nil {
				return fail(err)
			}
			r, err = svc.GetReceipt(r.ID)
			if err != nil || r.PlanID != plan.ID || r.PlanDigest != plan.Digest || r.ApprovalID != a.ID || r.CharterDigest != plan.CharterDigest {
				return fail(app.ErrConflict)
			}
			m.Message = "Authoritative provisioning receipt: " + setupJSON(r) + ". Return to the same draft and create a fresh preview."
			return render(c, m)
		}
		return fail(errors.New("invalid setup stage"))
	})
}

func setupJSON(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
