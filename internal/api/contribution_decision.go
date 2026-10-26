package api

import (
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
	"io"
)

func RegisterContributionDecisionRoutes(e *echo.Echo, svc *app.Service, manager *console.Manager) {
	const purpose = "contribution-role-provision-v1"
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
	e.POST("/console/contribution/decision-native/login", func(c *echo.Context) error {
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
		value, csrf, expires, sub, err := manager.Login(c.Request(), "contribution-native-local", password)
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
	e.POST("/console/contribution/decision-native/logout", func(c *echo.Context) error {
		if _, err := authorize(c); err != nil {
			return mapConsoleError(err)
		}
		manager.Revoke(c.Request())
		manager.ClearCookie(c.Response())
		return c.JSON(200, map[string]string{"status": "logged_out"})
	})
	e.POST("/console/contribution/decision-native/review", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var input app.ContributionDecisionInput
		if err = decode(c, &input); err != nil {
			return err
		}
		review, err := svc.PrepareContributionDecisionAs(c.Request().Context(), sub, input)
		if err != nil {
			return err
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
	e.POST("/console/contribution/decision-native/decision", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var input struct {
			Receipt  string `json:"receipt"`
			Decision string `json:"decision"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		payload, err := manager.ConsumeReviewReceipt(c.Request(), purpose, input.Receipt)
		if err != nil {
			if errors.Is(err, console.ErrReviewReceiptUnavailable) {
				return echo.NewHTTPError(409, "protected review receipt unavailable")
			}
			return mapConsoleError(err)
		}
		var review app.ContributionDecisionReview
		if err = json.Unmarshal(payload, &review); err != nil {
			return err
		}
		result, err := svc.CompleteContributionDecisionAs(c.Request().Context(), sub, review, input.Decision)
		if err != nil {
			return err
		}
		return c.JSON(200, result)
	})
	e.POST("/console/contribution/decision-native/read", func(c *echo.Context) error {
		sub, err := authorize(c)
		if err != nil {
			return mapConsoleError(err)
		}
		var input app.ContributionDecisionInput
		if err = decode(c, &input); err != nil {
			return err
		}
		result, err := svc.ReadContributionDecisionAs(c.Request().Context(), sub, input.Key, input.ID)
		if err != nil {
			return err
		}
		return c.JSON(200, result)
	})
}
