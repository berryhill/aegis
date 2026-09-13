package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/managergateway"
)

func TestManagerIntakeStrictMetadata(t *testing.T) {
	for _, body := range []string{
		`{"operation_id":"op","action":"review","reference":"demo","kind":"opaque"}`,
		`{"operation_id":"op","action":"approve"}`,
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeManagerIntake(r); err != nil {
			t.Fatal("valid metadata denied")
		}
	}
	for _, body := range []string{
		`{"operation_id":"op","action":"approve","action":"cancel"}`,
		`{"Operation_id":"op","action":"approve"}`,
		`{"operation_id":"op","action":null}`,
		`{"operation_id":"op","action":"approve","value":"forbidden"}`,
		`{"operation_id":"op","action":"approve"} {}`,
		`{"operation_id":"op","action":"approve"}` + strings.Repeat(" ", 4096),
		`null`, `[]`, `{}`,
	} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.ContentLength = -1
		r.Header.Set("Content-Type", "application/json")
		if _, err := decodeManagerIntake(r); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestManagerIntakeCapabilityDoesNotAdmitBrowser(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	if supportsManagerProtectedIntake(r) {
		t.Fatal("missing capability accepted")
	}
	r.Header.Set(managergateway.ProtectedIntakeHeader, managergateway.ProtectedIntakeProtocol)
	if !supportsManagerProtectedIntake(r) {
		t.Fatal("terminal capability rejected")
	}
	r.Header.Set("Origin", "http://localhost")
	if supportsManagerProtectedIntake(r) {
		t.Fatal("browser origin accepted")
	}
	r.Header.Del("Origin")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if supportsManagerProtectedIntake(r) {
		t.Fatal("browser fetch accepted")
	}
}
