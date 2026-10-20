package api

import (
	"net/http"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

func registerLoopAvailabilityRoutes(g *echo.Group, svc *app.Service) {
	g.GET("/loops/inventory", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		values, err := svc.ListLoopInventoryAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, values)
	})
	g.GET("/loops/:loop/availability", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.GetLoopAvailabilityAs(c.Request().Context(), subject, c.Param("loop"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.PUT("/loops/:loop/availability", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SetLoopAvailabilityInput
		if err := decode(c, &input); err != nil {
			return err
		}
		value, err := svc.SetLoopAvailabilityAs(c.Request().Context(), subject, c.Param("loop"), input)
		if err != nil {
			return err
		}
		return c.JSON(createdOrOK(value.Idempotent), value)
	})
}
