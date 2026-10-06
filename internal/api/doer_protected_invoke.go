package api

import (
	"context"
	"reflect"
	"strconv"
	"sync"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

type protectedSetupLauncher func(context.Context, app.DoerProtectedSetupInput) string

// The typed skill can REQUEST the protected surface, never supply its password
// or approve its review. The pinned companion repeats enrolled authentication
// and uses the native session-bound review/decision boundary above.
func registerDoerProtectedInvokeRoutes(g *echo.Group, svc *app.Service, launch protectedSetupLauncher) {
	if launch == nil {
		launch = func(ctx context.Context, in app.DoerProtectedSetupInput) string {
			args := []string{"doer-setup-companion", "--socket", svc.Config.API.UnixSocket, "--origin", svc.Config.API.Console.Origin, "--draft-id", in.ID, "--expected-version", strconv.FormatUint(in.ExpectedVersion, 10), "--action", in.Action}
			return launchProtectedDoerCompanion(ctx, args, svc.Config.API.Token, func(int) {}, svc.ConfigFile)
		}
	}
	var mu sync.Mutex
	active := map[string]bool{}
	g.POST("/loops/doer/drafts/:id/setup-protected", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = svc.RequirePrincipal(sub); err != nil {
			return err
		}
		var in app.DoerProtectedSetupInput
		if err = decode(c, &in); err != nil {
			return err
		}
		if in.ID != c.Param("id") || in.ExpectedVersion == 0 || (in.Action != "successor" && in.Action != "host" && in.Action != "provision") {
			return echo.NewHTTPError(400, "invalid protected setup invocation")
		}
		before, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, in.ID)
		if err != nil {
			return err
		}
		if before.Version != in.ExpectedVersion {
			return app.ErrConflict
		}
		var proposal *app.DoerSetupProposal
		if in.Action == "successor" {
			p, e := svc.ProposeDoerSetupSuccessorAs(c.Request().Context(), sub, in.ID, in.ExpectedVersion)
			if e != nil {
				return e
			}
			proposal = &p
		}
		mu.Lock()
		if active[in.ID] || len(active) >= 8 {
			mu.Unlock()
			return echo.NewHTTPError(429, "protected setup interaction already active or capacity exhausted")
		}
		active[in.ID] = true
		mu.Unlock()
		defer func() { mu.Lock(); delete(active, in.ID); mu.Unlock() }()
		status := launch(c.Request().Context(), in)
		after, err := svc.ReadDoerDraftAs(c.Request().Context(), sub, in.ID)
		if err != nil {
			return err
		}
		result := app.DoerProtectedSetupResult{Status: status, Draft: after}
		if status != "approved" {
			return c.JSON(200, result)
		}
		// Companion exit text is not authority. Only exact owning-service readback
		// can assert completion; partial/ambiguous outcomes remain unverified.
		if !reflect.DeepEqual(before.Contract, after.Contract) || before.ID != after.ID {
			return app.ErrConflict
		}
		switch in.Action {
		case "successor":
			if after.Version != before.Version+1 || after.Agent == before.Agent {
				return app.ErrConflict
			}
			old, e := svc.GetFleetAgentAs(c.Request().Context(), sub, before.Agent.ID, before.Agent.Revision)
			current, e2 := svc.GetFleetAgentAs(c.Request().Context(), sub, after.Agent.ID, after.Agent.Revision)
			if e != nil || e2 != nil || proposal == nil || after.Agent.ID != before.Agent.ID || current.Revision.Revision != old.Revision.Revision+1 || current.Revision.CharterSuccessor == nil || current.Revision.CharterSuccessor.Previous != before.Agent || current.Revision.Charter.ID != proposal.Proposed.Charter.AgentID || current.Revision.Charter.Revision != proposal.Proposed.Charter.Revision || current.Revision.Charter.Digest != proposal.Proposed.Digest {
				return app.ErrConflict
			}
			result.Successor = &current.Revision
		case "host":
			if !reflect.DeepEqual(before, after) {
				return app.ErrConflict
			}
			grant, e := svc.ReadDoerHostWriteApprovalAs(c.Request().Context(), sub, after.Agent, after.Contract)
			if e != nil {
				return e
			}
			result.Host = &grant
		case "provision":
			if !reflect.DeepEqual(before, after) {
				return app.ErrConflict
			}
			agent, e := svc.GetFleetAgentAs(c.Request().Context(), sub, after.Agent.ID, after.Agent.Revision)
			if e != nil {
				return e
			}
			receipts, e := svc.ListReceipts()
			if e != nil {
				return e
			}
			for _, receipt := range receipts {
				if receipt.Status != "verified" || receipt.CharterDigest != agent.Revision.Charter.Digest {
					continue
				}
				plan, e := svc.GetPlan(receipt.PlanID)
				if e == nil && plan.Digest == receipt.PlanDigest && plan.CharterDigest == receipt.CharterDigest && plan.AgentID == agent.Revision.Charter.ID && plan.Revision == agent.Revision.Charter.Revision {
					r := receipt
					result.Provisioning = &r
					break
				}
			}
			if result.Provisioning == nil {
				return app.ErrConflict
			}
		}
		return c.JSON(200, result)
	})
}
