package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/labstack/echo/v5"
)

func TestDoerProtectedSetupEnrolledLoginAndExactRetainedJourney(t *testing.T) {
	s, sub, _, _, draft := nativeSetupFixture(t)
	s.Config.Credentials.Authority.DeploymentID = "protected-setup-test"
	manager, _, _ := consoleQueueRouteFixture(t, s)
	e := echo.New()
	registerDoerProtectedSetupRoutes(e, s, manager)
	const base = "http://127.0.0.1/console/loops/doer/setup-native"
	var cookie *http.Cookie
	csrf := ""
	call := func(path string, body any, peer bool, origin, token string) *httptest.ResponseRecorder {
		raw, content := "", "application/json"
		if path == "/login" {
			raw, content = body.(string), "application/octet-stream"
		} else {
			data, _ := json.Marshal(body)
			raw = string(data)
		}
		req := httptest.NewRequest("POST", base+path, strings.NewReader(raw))
		if peer {
			req = req.WithContext(context.WithValue(req.Context(), peerUIDKey{}, uint32(os.Getuid())))
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", content)
		req.Header.Set("X-CSRF-Token", token)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	unchanged := func() {
		t.Helper()
		got, err := s.ReadDoerDraftAs(context.Background(), sub, draft.ID)
		if err != nil || !reflect.DeepEqual(got, draft) {
			t.Fatal("retained draft changed without exact consent", err)
		}
	}
	// Transport UID and the entire exact review request alone are not approval.
	input := app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: draft.Version, Action: "successor"}
	if r := call("/review", input, true, "http://127.0.0.1", ""); r.Code < 400 {
		t.Fatal("UID authenticated independently")
	}
	unchanged()
	for _, peer := range []bool{false, true} {
		password := "test-principal-password"
		if peer {
			password = "wrong-password"
		}
		if r := call("/login", password, peer, "http://127.0.0.1", ""); r.Code < 400 || strings.Contains(r.Body.String(), password) {
			t.Fatal("invalid login accepted or leaked password", r.Code)
		}
	}
	unchanged()
	login := call("/login", "test-principal-password", true, "http://127.0.0.1", "")
	var auth struct {
		CSRF string `json:"csrf"`
	}
	if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &auth) != nil || len(login.Result().Cookies()) != 1 {
		t.Fatal("login failed", login.Code, login.Body.String())
	}
	cookie = login.Result().Cookies()[0]
	csrf = auth.CSRF
	review := func(action string) app.DoerProtectedSetupReview {
		t.Helper()
		input.Action = action
		r := call("/review", input, true, "http://127.0.0.1", csrf)
		var result app.DoerProtectedSetupReview
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Receipt == "" {
			t.Fatal("review failed", r.Code, r.Body.String())
		}
		return result
	}
	approve := func(receipt string) *httptest.ResponseRecorder {
		return call("/decision", app.DoerProtectedSetupDecision{Receipt: receipt, Decision: "approve"}, true, "http://127.0.0.1", csrf)
	}
	for _, test := range []struct {
		peer         bool
		origin, csrf string
	}{{false, "http://127.0.0.1", csrf}, {true, "https://foreign.example", csrf}, {true, "http://127.0.0.1", "bad"}} {
		if r := call("/review", input, test.peer, test.origin, test.csrf); r.Code < 400 {
			t.Fatal("invalid admission accepted")
		}
		unchanged()
	}
	first := review("successor")
	unchanged()
	// Candidate bytes and action cannot be supplied at execute time.
	if r := call("/decision", map[string]any{"receipt": first.Receipt, "decision": "approve", "proposal": first.Proposal}, true, "http://127.0.0.1", csrf); r.Code != 400 {
		t.Fatal("substitution accepted", r.Code)
	}
	unchanged()
	// Rejection consumes the exact review without granting anything.
	r := call("/decision", app.DoerProtectedSetupDecision{Receipt: first.Receipt, Decision: "reject"}, true, "http://127.0.0.1", csrf)
	if r.Code != 200 {
		t.Fatal("reject failed", r.Code)
	}
	unchanged()
	if approve(first.Receipt).Code < 400 {
		t.Fatal("reject receipt replayed")
	}
	second := review("successor")
	r = approve(second.Receipt)
	var result app.DoerProtectedSetupResult
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
		t.Fatal("successor failed", r.Code, r.Body.String())
	}
	if result.Status != "approved" || result.Draft.ID != draft.ID || result.Draft.Agent == draft.Agent || !reflect.DeepEqual(result.Draft.Contract, draft.Contract) || result.Draft.PublicationKey == draft.PublicationKey {
		t.Fatal("successor lost retained task")
	}
	draft = result.Draft
	input.ExpectedVersion = draft.Version
	if approve(second.Receipt).Code < 400 {
		t.Fatal("single-use review replayed")
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	items, _ := s.FleetRepository.ListQueueItems(context.Background())
	if len(approvals) != 0 || len(receipts) != 0 || len(items) != 0 {
		t.Fatal("successor auto-granted downstream effects")
	}
	host := review("host")
	unchanged()
	if approve(host.Receipt).Code != 200 {
		t.Fatal("exact host approval failed")
	}
	receipts, _ = s.ListReceipts()
	if len(receipts) != 0 {
		t.Fatal("host consent provisioned")
	}
	provision := review("provision")
	unchanged()
	r = approve(provision.Receipt)
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil || result.Provisioning == nil || result.Provisioning.Status != "verified" {
		t.Fatal("exact provisioning failed", r.Code, r.Body.String())
	}
	unchanged()
	receipts, _ = s.ListReceipts()
	if len(receipts) != 1 {
		t.Fatal("provisioning not read back exactly")
	}
	if approve(provision.Receipt).Code < 400 {
		t.Fatal("provisioning replayed")
	}
	items, _ = s.FleetRepository.ListQueueItems(context.Background())
	if len(items) != 0 {
		t.Fatal("setup auto-published or ran")
	}
	// Fresh password login creates a different session; a copied receipt is useless.
	fresh := review("host")
	login = call("/login", "test-principal-password", true, "http://127.0.0.1", "")
	cookie = login.Result().Cookies()[0]
	json.Unmarshal(login.Body.Bytes(), &auth)
	csrf = auth.CSRF
	if approve(fresh.Receipt).Code < 400 {
		t.Fatal("cross-session receipt accepted")
	}
	expired := review("host")
	now := s.Now()
	s.Now = func() time.Time { return now.Add(2 * time.Minute) }
	if approve(expired.Receipt).Code < 400 {
		t.Fatal("expired review accepted")
	}
	// Manager must still reject an old session after verifier generation change;
	// generation/rotation race regressions live in internal/console's suite.
}

func TestDoerProtectedSetupNativeLoginThrottlesStableClient(t *testing.T) {
	s, _, _, _, _ := nativeSetupFixture(t)
	manager, _, _ := consoleQueueRouteFixture(t, s)
	e := echo.New()
	registerDoerProtectedSetupRoutes(e, s, manager)
	for i := 0; i < 4; i++ {
		password := "wrong-password"
		if i == 3 {
			password = "test-principal-password"
		}
		req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/doer/setup-native/login", strings.NewReader(password))
		req = req.WithContext(context.WithValue(req.Context(), peerUIDKey{}, uint32(os.Getuid())))
		req.Header.Set("Origin", "http://127.0.0.1")
		req.Header.Set("Content-Type", "application/octet-stream")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		if rr.Code < 400 || len(rr.Result().Cookies()) != 0 || strings.Contains(rr.Body.String(), password) {
			t.Fatal("login throttle/secret boundary failed", i, rr.Code)
		}
	}
	_ = console.CookieName
}
