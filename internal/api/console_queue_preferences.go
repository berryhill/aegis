package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

// Visibility is applied before lifecycle filtering and pagination. Summary bands
// keep their independent lifecycle membership, excluding archived history only.
func filterConsoleQueuePresentation(ctx context.Context, svc *app.Service, sub core.Subject, model *consoleweb.SurfaceModel, query url.Values) error {
	for _, key := range []string{"state", "visibility"} {
		if len(query[key]) > 1 {
			return app.ErrDenied
		}
	}
	prefs, err := svc.GetQueuePreferencesAs(ctx, sub)
	if err != nil {
		return err
	}
	state := query.Get("state")
	if state == "" {
		state = string(prefs.Selection)
	}
	visibility := queue.ArchiveVisibility(query.Get("visibility"))
	if visibility == "" {
		visibility = prefs.Visibility
	}
	if err = visibility.Validate(); err != nil {
		return err
	}
	model.QueueVisibility = string(visibility)
	model.QueuePreferenceRevision = strconv.FormatUint(prefs.Revision, 10)
	archived := map[string]bool{}
	for _, record := range model.Records {
		if record.Queue == nil {
			continue
		}
		a, loadErr := svc.GetQueueArchiveAs(ctx, sub, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: record.Key, Digest: record.Queue.QueueItemDigest})
		if errors.Is(loadErr, fleet.ErrPresentationDenied) {
			continue
		}
		if loadErr != nil {
			return loadErr
		}
		archived[record.Key] = a.Archived
	}
	selectRecords := func(records []consoleweb.RecordModel, band bool) []consoleweb.RecordModel {
		out := make([]consoleweb.RecordModel, 0, len(records))
		for _, record := range records {
			a := archived[record.Key]
			if band {
				if a {
					continue
				}
			} else if visibility == queue.VisibilityArchived && !a || visibility == queue.VisibilityUnarchived && a {
				continue
			}
			out = append(out, record)
		}
		return out
	}
	model.Records = selectRecords(model.Records, false)
	model.ActiveRecords = selectRecords(model.ActiveRecords, true)
	model.FailedRecords = selectRecords(model.FailedRecords, true)
	return filterConsoleQueue(model, state)
}

func consoleQueuePreferencesHandler(svc *app.Service, manager *console.Manager) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		r := c.Request()
		bad := func() error { return echo.NewHTTPError(http.StatusBadRequest, "invalid Queue preferences form") }
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/x-www-form-urlencoded" || r.Body == nil {
			return bad()
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(body) > 4096 {
			return bad()
		}
		v, err := url.ParseQuery(string(body))
		if err != nil || len(v) != 4 {
			return bad()
		}
		for key, entries := range v {
			if (key != "csrf" && key != "state" && key != "visibility" && key != "expected_revision") || len(entries) != 1 || entries[0] == "" {
				return bad()
			}
		}
		revision, err := strconv.ParseUint(v.Get("expected_revision"), 10, 64)
		if err != nil {
			return bad()
		}
		selection, visibility := queue.LifecycleSelection(v.Get("state")), queue.ArchiveVisibility(v.Get("visibility"))
		if selection.Validate() != nil || visibility.Validate() != nil {
			return bad()
		}
		r.Header.Set("X-CSRF-Token", v.Get("csrf"))
		sub, err := manager.AuthorizeMutation(r)
		if err != nil {
			return mapConsoleError(err)
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("console-queue-preferences-v1:%s:%d:%s:%s", sub.PrincipalID, revision, selection, visibility)))
		_, _, err = svc.SetQueuePreferencesAs(r.Context(), sub, app.SetQueuePreferencesInput{RequestID: fmt.Sprintf("console-queue-prefs-%x", sum), Selection: selection, Visibility: visibility, ExpectedRevision: revision})
		if err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, "/console/queue")
	}
}
