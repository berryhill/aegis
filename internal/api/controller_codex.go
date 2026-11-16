package api

import (
	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
	"net/http"
)

func registerControllerCodexRoutes(g *echo.Group, s *app.Service) {
	g.POST("/agents/:agent/providers", func(c *echo.Context) error {
		sub, err := requestSubject(c)
		if err != nil {
			return err
		}
		var in app.AgentProviderSetupInput
		if err = decode(c, &in); err != nil {
			return err
		}
		out, err := s.AgentProviderSetupAs(c.Request().Context(), sub, c.Param("agent"), in)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	})
	g.POST("/controller/codex/probe", func(c *echo.Context) error {
		sub, e := requestSubject(c)
		if e != nil {
			return e
		}
		var in app.ControllerCodexProbeInput
		if e = decode(c, &in); e != nil {
			return e
		}
		out, e := s.ProbeControllerCodexAs(c.Request().Context(), sub, in)
		if e != nil {
			return e
		}
		return c.JSON(http.StatusOK, out)
	})
	g.POST("/controller/codex/candidates", func(c *echo.Context) error {
		sub, e := requestSubject(c)
		if e != nil {
			return e
		}
		var in app.ControllerCodexCandidatesInput
		if e = decode(c, &in); e != nil {
			return e
		}
		out, e := s.InspectControllerCodexCandidatesAs(c.Request().Context(), sub, in)
		if e != nil {
			return e
		}
		return c.JSON(http.StatusOK, out)
	})
	g.POST("/controller/codex/preview", func(c *echo.Context) error {
		sub, e := requestSubject(c)
		if e != nil {
			return e
		}
		var in app.ControllerCodexPreviewInput
		if e = decode(c, &in); e != nil {
			return e
		}
		out, e := s.PreviewControllerCodexAs(c.Request().Context(), sub, in)
		if e != nil {
			return e
		}
		return c.JSON(http.StatusOK, out)
	})
	g.POST("/controller/codex/connect", func(c *echo.Context) error {
		sub, e := requestSubject(c)
		if e != nil {
			return e
		}
		var in app.ControllerCodexApplyInput
		if e = decode(c, &in); e != nil {
			return e
		}
		out, e := s.ApplyControllerCodexAs(c.Request().Context(), sub, in)
		if e != nil {
			return e
		}
		return c.JSON(http.StatusOK, out)
	})
	g.GET("/controller/codex", func(c *echo.Context) error {
		sub, e := requestSubject(c)
		if e != nil {
			return e
		}
		out, e := s.ReadControllerCodexAs(c.Request().Context(), sub)
		if e != nil {
			return e
		}
		return c.JSON(http.StatusOK, out)
	})
}
