package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

// Match the canonical Loop identifier grammar. A compound read key appends one
// exact positive uint64 revision; no token is trimmed, repaired or normalized.
var consoleLoopIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)
var consoleLoopDecimal = regexp.MustCompile(`^[0-9]+$`)

func loopBookmarkNeedsResolution(key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	if len(key) > 255+1+20 {
		return false, errors.New("Loop read key exceeds bound")
	}
	if colon := strings.LastIndexByte(key, ':'); colon > 0 {
		suffix := key[colon+1:]
		if consoleLoopDecimal.MatchString(suffix) {
			revision, err := strconv.ParseUint(suffix, 10, 64)
			if err != nil || revision == 0 || strconv.FormatUint(revision, 10) != suffix || !consoleLoopIdentifier.MatchString(key[:colon]) {
				return false, errors.New("invalid exact Loop read key")
			}
			return false, nil
		}
	}
	if !consoleLoopIdentifier.MatchString(key) {
		return false, errors.New("invalid Loop identifier")
	}
	return true, nil
}

func loopBookmarkTarget(key string, inventory []app.LoopView, query url.Values) (string, error) {
	var selected *app.LoopView
	for i := range inventory {
		view := &inventory[i]
		if view.Revision.LoopID == key && (selected == nil || view.Revision.Revision > selected.Revision.Revision) {
			selected = view
		}
	}
	if selected == nil {
		return "", errors.New("Loop record not found")
	}
	target, err := url.Parse(consoleRecordURL(consoleLoops, fmt.Sprintf("%s:%d", selected.Revision.LoopID, selected.Revision.Revision)))
	if err != nil {
		return "", err
	}
	values := target.Query()
	for _, name := range []string{"q", "lifecycle", "availability", "page", "limit"} {
		if len(query[name]) > 1 {
			return "", errors.New("ambiguous Loop read filter")
		}
		if value := query.Get(name); value != "" {
			values.Set(name, value)
		}
	}
	target.RawQuery = values.Encode()
	return target.String(), nil
}

// A logical-ID bookmark is pinned by an authenticated server read, then native
// redirect. Refresh therefore keeps the exact immutable revision, not a mutable
// browser-selected latest value. This is definition navigation, never Run.
func pinConsoleLoopBookmark(c *echo.Context, svc *app.Service, subject core.Subject) (string, error) {
	query := c.QueryParams()
	if len(query["record_key"]) > 1 {
		return "", echo.NewHTTPError(http.StatusBadRequest, "ambiguous Loop bookmark")
	}
	key := query.Get("record_key")
	needed, err := loopBookmarkNeedsResolution(key)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid Loop bookmark")
	}
	if !needed {
		return "", nil
	}
	inventory, err := svc.ListLoopInventoryAs(c.Request().Context(), subject)
	if err != nil {
		return "", err
	}
	target, err := loopBookmarkTarget(key, inventory, query)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusNotFound, "Loop bookmark unavailable")
	}
	return target, nil
}
