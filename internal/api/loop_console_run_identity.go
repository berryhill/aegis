package api

import (
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
	"net/http"
	"net/url"
)

// consoleLoopRunPageIdentity is called only after authenticated exact selection.
func consoleLoopRunPageIdentity(c *echo.Context, subject core.Subject, record *consoleweb.RecordModel) (string, bool, error) {
	key, err := consoleLoopRunKey(c, subject, record)
	if err != nil {
		return "", false, err
	}
	if c.QueryParam("new_run") == "1" {
		// Consume the action before rendering. Reload/history now reference only
		// the exact selected revision and retain the freshly issued cookie.
		target := "/console/loops?" + url.Values{"record_key": {record.Key}}.Encode()
		return key, true, c.Redirect(http.StatusSeeOther, target)
	}
	return key, false, err
}
