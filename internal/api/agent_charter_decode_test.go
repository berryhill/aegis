package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

func TestCharterApprovalHTTPDecodeRejectsDuplicateKeys(t *testing.T) {
	for _, body := range []string{`{"expected":{},"expected":{},"charter":{}}`, `{"expected":{"id":"one","id":"two"},"charter":{}}`, `{"expected":{},"charter":{},"unknown":true}`} {
		req := httptest.NewRequest(http.MethodPut, "/v1/agents/agent/charter", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c := echo.New().NewContext(req, httptest.NewRecorder())
		var input app.ApproveAgentCharterInput
		if err := decode(c, &input); err == nil {
			t.Fatalf("accepted ambiguous HTTP approval: %s", body)
		}
	}
}
