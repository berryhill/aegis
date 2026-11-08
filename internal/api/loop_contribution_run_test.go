package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func TestConsoleContributionRunClosedForm(t *testing.T) {
	good := url.Values{"csrf": {"csrf"}, "revision": {"1"}, "digest": {"sha256:" + strings.Repeat("a", 64)}, "github_issue_url": {"https://github.com/owner/repo/issues/1"}, "action": {"run"}}
	decode := func(v url.Values) error {
		r := httptest.NewRequest("POST", "/", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, e := decodeContributionRunForm(r)
		return e
	}
	if err := decode(good); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"session", "authority", "scope", "inputs", "workspace", "agent_id", "idempotency_key"} {
		v := url.Values{}
		for k, vs := range good {
			v[k] = append([]string(nil), vs...)
		}
		v.Set(k, "forged")
		if decode(v) == nil {
			t.Fatal("accepted authority/data injection", k)
		}
	}
	for k := range good {
		v := url.Values{}
		for k, vs := range good {
			v[k] = append([]string(nil), vs...)
		}
		v.Add(k, "duplicate")
		if decode(v) == nil {
			t.Fatal("duplicate", k)
		}
	}
	good.Set("github_issue_url", strings.Repeat("x", 4097))
	if decode(good) == nil {
		t.Fatal("unbounded form")
	}
}

func TestConsoleContributionNativeRunRecoveryAndBoundaries(t *testing.T) {
	s, repo, _, ref, reader := decisionHTTPFixture(t)
	s.ContributionReader = reader
	manager, cookie, csrf := consoleQueueRouteFixture(t, s)
	s.DashboardActionAuthentication = manager.ValidateActionSubject
	e := echo.New()
	e.HTTPErrorHandler = apiHTTPErrorHandler(s, manager)
	e.POST("/console/loops/contribution", consoleContributionRunHandler(s, manager))
	values := url.Values{"csrf": {csrf}, "revision": {strconv.FormatUint(ref.Revision, 10)}, "digest": {ref.Digest}, "github_issue_url": {reader.source.URL.URL}, "action": {"run"}}
	call := func(auth bool, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/contribution?loop_id="+ref.ID, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		if auth {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		return w
	}
	if w := call(false, "http://127.0.0.1"); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code, w.Body.String())
	}
	if w := call(true, "https://foreign.invalid"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	values.Set("csrf", "wrong")
	if w := call(true, "http://127.0.0.1"); w.Code != 403 {
		t.Fatal("csrf", w.Code)
	}
	values.Set("csrf", csrf)
	values.Set("digest", "sha256:"+strings.Repeat("f", 64))
	if w := call(true, "http://127.0.0.1"); w.Code != 403 {
		t.Fatal("digest", w.Code)
	}
	values.Set("digest", ref.Digest)
	w := call(true, "http://127.0.0.1")
	if w.Code != 200 {
		t.Fatal("real intake", w.Code, w.Body.String())
	}
	key := app.ContributionIntentKey(ref, reader.source.URL.URL, "")
	c, err := repo.GetContribution(context.Background(), s.Config.Principal.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionID == "" || !strings.HasPrefix(c.SessionID, "session-") {
		t.Fatal("not actual Console session", c.SessionID)
	}
	if !strings.Contains(w.Body.String(), "Same-intent recovery") || !strings.Contains(w.Body.String(), "Resume same intent") {
		t.Fatal("no retained controls")
	}
	// A response retry must read the original exact intent, not allocate work.
	if w = call(true, "http://127.0.0.1"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	again, err := repo.GetContribution(context.Background(), c.OwnerID, key)
	if err != nil || again.Digest != c.Digest || reader.calls != 1 {
		t.Fatal("duplicate intake", err, reader.calls)
	}
	values.Set("action", "narrow")
	values.Set("delivery", "local-only")
	if w = call(true, "http://127.0.0.1"); w.Code != 200 {
		t.Fatal("narrow", w.Code, w.Body.String())
	}
	values.Del("delivery")
	values.Set("action", "stop")
	if w = call(true, "http://127.0.0.1"); w.Code != 200 {
		t.Fatal("stop", w.Code, w.Body.String())
	}
	values.Set("action", "status")
	if w = call(true, "http://127.0.0.1"); w.Code != 200 {
		t.Fatal("status", w.Code, w.Body.String())
	}
	control, err := repo.GetContributionControl(context.Background(), c.QueueItemID)
	if err != nil || !control.Stop {
		t.Fatal("stop not retained", err)
	}
	// Render the actual exact Loop detail, not a disconnected demo form.
	lv, err := s.GetLoopViewAs(context.Background(), mustConsoleSubject(t, manager, cookie), ref.ID, ref.Revision)
	if err != nil {
		t.Fatal(err)
	}
	record := consoleLoopRecord(lv)
	body, err := renderConsole(context.Background(), consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops", CSRF: csrf}, &record, consoleweb.LoopTopology{}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `name="github_issue_url"`) || strings.Contains(text, "Find a Graph") || strings.Contains(text, "Implementation workspace") || strings.Contains(text, "Implement the approved value behavior") {
		t.Fatal("wrong/private contribution detail", text)
	}
}

func mustConsoleSubject(t *testing.T, m interface {
	Authenticate(*http.Request) (core.Subject, error)
}, cookie *http.Cookie) core.Subject {
	t.Helper()
	r := httptest.NewRequest("GET", "http://127.0.0.1/console/loops", nil)
	r.AddCookie(cookie)
	s, e := m.Authenticate(r)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
