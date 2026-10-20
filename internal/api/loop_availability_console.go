package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	consoleauth "github.com/berryhill/aegis/internal/console"

	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func filterConsoleLoopAvailability(model *consoleweb.SurfaceModel, state, exact string) error {
	if state == "" {
		state = "published"
	}
	if state != "published" && state != "archived" {
		return errors.New("invalid availability")
	}
	model.Availability = state
	records := make([]consoleweb.RecordModel, 0, len(model.Records))
	for _, record := range model.Records {
		// Explicit exact historical links remain readable regardless of discoverability.
		if record.Availability == state || record.Key == exact {
			records = append(records, record)
		}
	}
	model.Records = records
	model.TotalCount = len(records)
	return nil
}

func consoleLoopAvailabilityHandler(svc *app.Service, manager *consoleauth.Manager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		r := c.Request()
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/x-www-form-urlencoded" {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid availability form")
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || len(data) > 8192 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid availability form")
		}
		values, err := url.ParseQuery(string(data))
		keys := []string{"csrf", "revision", "digest", "sequence", "previous_digest", "expected_state", "state"}
		if err != nil || len(values) != len(keys) {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid availability form")
		}
		for _, key := range keys {
			if len(values[key]) != 1 {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid availability form")
			}
		}
		revision, er := strconv.ParseUint(values.Get("revision"), 10, 64)
		sequence, es := strconv.ParseUint(values.Get("sequence"), 10, 64)
		expected, state := app.LoopAvailability(values.Get("expected_state")), app.LoopAvailability(values.Get("state"))
		if er != nil || es != nil || revision == 0 || sequence == 0 || !expected.Valid() || !state.Valid() || state == expected {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid availability form")
		}
		r.Header.Set("X-CSRF-Token", values.Get("csrf"))
		subject, _, err := manager.AuthorizeCommand(r)
		if err != nil {
			return mapConsoleError(err)
		}
		ctx := r.Context()
		view, err := svc.GetLoopViewAs(ctx, subject, c.QueryParam("loop_id"), revision)
		if err != nil {
			return err
		}
		if view.Revision.Digest != values.Get("digest") {
			return app.ErrConflict
		}
		// Stable original publisher and current workspace authority are server-derived.
		original, err := svc.GetLoopViewAs(ctx, subject, view.Revision.LoopID, 1)
		if err != nil {
			return err
		}
		eventID, err := randomConsoleID("availability")
		if err != nil {
			return err
		}
		_, err = svc.SetLoopAvailabilityAs(ctx, subject, view.Revision.LoopID, app.SetLoopAvailabilityInput{
			AgentID: original.Provenance.PublisherAgent.ID,
			Head:    app.RevisionReference(view.Revision.LoopID, revision, view.Revision.Digest),
			EventID: eventID, Sequence: sequence, ExpectedState: expected, State: state, PreviousDigest: values.Get("previous_digest"),
		})
		if err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, "/console/loops?availability="+url.QueryEscape(string(state))+"&record_key="+url.QueryEscape(view.Revision.LoopID+":"+strconv.FormatUint(revision, 10)))
	}
}
