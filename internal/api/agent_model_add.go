package api

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

// Reuse the native protected login/logout, live password registry, peer/origin
// checks and CSRF policy. No model-facing or bearer/peer-only approval route.
func registerAgentModelAddNativeRoutes(e *echo.Echo, svc *app.Service, manager *console.Manager, authorize func(*echo.Context) (core.Subject, error)) {

	const purpose = "agent-model-add-v1"
	e.POST("/console/loops/doer/setup-native/model-review", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var in app.AgentModelAddInput
		if err = decode(c, &in); err != nil {
			return err
		}
		review, err := svc.PrepareAgentModelAddAs(c.Request().Context(), sub, in)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(review)
		if err != nil {
			return err
		}
		review.Receipt, err = manager.IssueReviewReceipt(c.Request(), purpose, raw)
		if err != nil {
			return mapConsoleError(err)
		}
		return c.JSON(200, review)
	})
	e.POST("/console/loops/doer/setup-native/model-decision", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var in app.DoerProtectedSetupDecision
		if err = decode(c, &in); err != nil {
			return err
		}
		if in.Decision != "approve" && in.Decision != "reject" {
			return echo.NewHTTPError(400, "invalid exact decision")
		}
		raw, err := manager.ConsumeReviewReceipt(c.Request(), purpose, in.Receipt)
		if err != nil {
			return mapConsoleError(err)
		}
		var review app.AgentModelAddReview
		if err = json.Unmarshal(raw, &review); err != nil {
			return err
		}
		if in.Decision == "reject" {
			return c.JSON(200, map[string]string{"status": "rejected"})
		}
		agent, err := svc.ConfirmAgentModelAddAs(c.Request().Context(), sub, review)
		if err != nil {
			return err
		}
		return c.JSON(200, app.AgentModelAddResult{Agent: agent})
	})
}
