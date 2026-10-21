package api

import (
	"net/http"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

// registerQueuePresentationHTTP keeps identity and all admission in the app service.
func registerQueuePresentationHTTP(g *echo.Group, svc *app.Service) {
	g.POST("/queue/:item/archive", queueArchiveHTTPHandler(svc, true))
	g.POST("/queue/:item/unarchive", queueArchiveHTTPHandler(svc, false))
	g.POST("/queue/:item/requeue", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.RequeueQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItem.ID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.RequeueQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.GET("/queue/preferences", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.GetQueuePreferencesAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.PUT("/queue/preferences", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SetQueuePreferencesInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, created, err := svc.SetQueuePreferencesAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(createdOrOK(!created), value)
	})
}

func queueArchiveHTTPHandler(svc *app.Service, archived bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SetQueueArchiveInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItem.ID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		if input.Archived != archived {
			return echo.NewHTTPError(http.StatusBadRequest, "archived value conflicts with operation")
		}
		value, created, err := svc.SetQueueArchiveAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(createdOrOK(!created), value)
	}
}
