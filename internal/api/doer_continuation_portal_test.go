package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/labstack/echo/v5"
)

func portalFixture(t *testing.T) (*DoerContinuationPortal, *echo.Echo, *http.Cookie, string, app.DoerDraft, core.Subject, string) {
	t.Helper()
	svc, _, i := continuationFixture(t)
	svc.Config.API.Console.Origin = "http://127.0.0.1"
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/doer/continuation", nil)
	req.Header.Set("Origin", "http://127.0.0.1")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	subject, session, err := manager.AuthorizeCommand(req)
	if err != nil {
		t.Fatal(err)
	}
	old, err := svc.ReadDoerDraftAs(context.Background(), i.Requester, i.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	p := NewDoerContinuationPortal(svc, manager)
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, code, message := classifyError(err)
		_ = c.JSON(status, envelope{Code: code, Message: message})
	}
	p.RegisterBrowser(e)
	return p, e, cookie, csrf, old, subject, session
}
func portalPost(e *echo.Echo, cookie *http.Cookie, csrf string, values url.Values) *httptest.ResponseRecorder {
	values.Set("csrf", csrf)
	req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/doer/continuation", strings.NewReader(values.Encode()))
	req.Header.Set("Origin", "http://127.0.0.1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	out := httptest.NewRecorder()
	e.ServeHTTP(out, req)
	return out
}
func TestDoerPortalReviewNoEffectsStableKeyAndIndependentDecision(t *testing.T) {
	p, e, cookie, csrf, d, subject, session := portalFixture(t)
	before := continuationState(t, p.svc)
	calls := 0
	p.launch = func(ctx context.Context, id string, bind func(int)) string { calls++; bind(1234); return "cancelled" }
	review := func() *httptest.ResponseRecorder {
		return portalPost(e, cookie, csrf, url.Values{"action": {"review"}, "draft_id": {d.ID}})
	}
	out := review()
	if out.Code != 200 {
		t.Fatalf("review: %d %s", out.Code, out.Body.String())
	}
	if calls != 0 || !reflect.DeepEqual(before, continuationState(t, p.svc)) {
		t.Fatal("review granted authority or launched")
	}
	var v doerPending
	for _, saved := range p.pending {
		v = *saved
	}
	if v.ID == "" || v.Review.Intent.SessionID != session || !reflect.DeepEqual(subject, v.Review.Intent.Requester) || v.Review.Intent.ExpiresAt.After(subject.ExpiresAt) {
		t.Fatal("incorrect original binding")
	}
	out = portalPost(e, cookie, csrf, url.Values{"action": {"request-local-review"}, "intent_id": {v.ID}})
	if out.Code != 200 || calls != 1 || !strings.Contains(out.Body.String(), "cancelled") {
		t.Fatal("cancel outcome missing")
	}
	// Deliberately omit the Run cookie: lost responses must not remint the key.
	out = review()
	if out.Code != 200 || len(p.pending) != 1 {
		t.Fatal("review reminted intent")
	}
	if !strings.Contains(out.Body.String(), v.Review.Intent.Run.IdempotencyKey) || calls != 1 {
		t.Fatal("original key lost or review launched")
	}
	if !reflect.DeepEqual(before, continuationState(t, p.svc)) {
		t.Fatal("cancel granted authority")
	}
	_, otherCookie, otherCSRF := consoleQueueRouteFixture(t, p.svc)
	if portalPost(e, otherCookie, otherCSRF, url.Values{"action": {"observe"}, "intent_id": {v.ID}}).Code == 200 {
		t.Fatal("cross session accepted")
	}
	if portalPost(e, cookie, "wrong", url.Values{"action": {"review"}, "draft_id": {d.ID}}).Code == 200 {
		t.Fatal("bad csrf accepted")
	}
}

func TestDoerPortalNativeSOPEERPIDOriginDigestAndSignedReadback(t *testing.T) {
	p, browser, browserCookie, browserCSRF, d, subject, session := portalFixture(t)
	v, err := p.retain(context.Background(), subject, session, d.ID, "stable-run-key")
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) { status, _, _ := classifyError(err); _ = c.NoContent(status) }
	g := e.Group("/v1")
	// Real kernel UID/PID ingress and bearer guard; no synthesized peer context.
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get("Authorization") != "Bearer synthetic-portal-token" {
				return app.ErrDenied
			}
			uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
			if !ok {
				return app.ErrDenied
			}
			s, err := p.svc.AuthenticateUnixPeer(c.Request().Context(), uid)
			if err != nil {
				return err
			}
			c.Set("subject", s)
			return next(c)
		}
	})
	p.RegisterNative(g)
	path := t.TempDir() + "/portal.sock"
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: e, ConnContext: unixPeerContext}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	request := func(method, origin, digest string) (int, []byte) {
		t.Helper()
		body := ""
		suffix := ""
		if method == "POST" {
			suffix = "/decision"
			b, _ := json.Marshal(map[string]string{"decision": "approve", "expected_digest": digest})
			body = string(b)
		}
		req, _ := http.NewRequest(method, "http://127.0.0.1/v1/doer-continuations/pending/"+v.ID+suffix, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer synthetic-portal-token")
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	if code, _ := request("POST", "http://127.0.0.1", v.Review.Digest); code != 403 {
		t.Fatal("decision before launched native callback accepted")
	}
	p.pending[v.ID].Launching = true
	p.pending[v.ID].CompanionPID = os.Getpid() + 1
	if code, _ := request("POST", "http://127.0.0.1", v.Review.Digest); code != 403 {
		t.Fatal("wrong kernel PID accepted")
	}
	p.pending[v.ID].CompanionPID = os.Getpid()
	if code, _ := request("GET", "http://wrong", ""); code != 403 {
		t.Fatal("wrong origin accepted")
	}
	if code, _ := request("POST", "http://127.0.0.1", "sha256:wrong"); code == 200 {
		t.Fatal("wrong digest accepted")
	}
	code, body := request("GET", "http://127.0.0.1", "")
	if code != 200 {
		t.Fatalf("native review %d", code)
	}
	var review app.DoerContinuationReview
	if json.Unmarshal(body, &review) != nil || review.Digest != review.ContentDigest() {
		t.Fatal("invalid review")
	}
	// Explicit fake native CONFIRM boundary; only now send the approval POST.
	code, body = request("POST", "http://127.0.0.1", review.Digest)
	if code != 200 {
		t.Fatalf("native approve %d: %s", code, body)
	}
	saved := p.pending[v.ID]
	approved, err := p.svc.Store.ReadDoerContinuation(saved.ApprovalID)
	if err != nil || approved.Executor.ID != "local-uid:"+p.svc.Config.Principal.UID {
		t.Fatal("missing genuine signed Unix executor", err)
	}
	candidate, _, err := loop.NewDoerRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.svc.PublishLoopAs(context.Background(), approved.Executor, app.PublishLoopInput{AgentID: d.Agent.ID, Revision: candidate, IdempotencyKey: d.PublicationKey}); err != nil {
		t.Fatal(err)
	}
	browser.POST("/console/loops/run", consoleLoopRunHandler(p.svc, p.manager, p))
	form := url.Values{"csrf": {browserCSRF}, "revision": {"1"}, "digest": {candidate.Digest}, "idempotency_key": {review.Intent.Run.IdempotencyKey}}
	req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/run?loop_id="+url.QueryEscape(d.LoopID), strings.NewReader(form.Encode()))
	req.Header.Set("Origin", "http://127.0.0.1")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(browserCookie)
	result := httptest.NewRecorder()
	browser.ServeHTTP(result, req)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "doer_model_required") || !strings.Contains(result.Body.String(), "stable-run-key") {
		t.Fatalf("original browser Run: %d %s", result.Code, result.Body.String())
	}
	events, err := p.svc.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	originalExecutor := false
	for _, event := range events {
		if event.Type == "doer_continuation_executor" && event.SubjectID == approved.Executor.ID {
			originalExecutor = true
		}
	}
	if !originalExecutor {
		t.Fatal("Run did not use signed original executor")
	}
	resolved, ok := p.exact(context.Background(), subject, session, review.Intent.Run)
	if !ok || resolved.ApprovalID != saved.ApprovalID {
		t.Fatal("exact browser binding missing")
	}
	wrong := subject
	wrong.ID = "wrong"
	if _, ok = p.exact(context.Background(), wrong, session, review.Intent.Run); ok {
		t.Fatal("wrong requester accepted")
	}
	if _, ok = p.exact(context.Background(), subject, "other", review.Intent.Run); ok {
		t.Fatal("wrong session accepted")
	}
	run := review.Intent.Run
	run.IdempotencyKey = "different"
	if _, ok = p.exact(context.Background(), subject, session, run); ok {
		t.Fatal("new key accepted")
	}
	// Mutating retained unsigned draft data invalidates the portal before consent.
	_, err = p.svc.SaveDoerDraftAs(context.Background(), subject, app.DoerDraftInput{ID: d.ID, ExpectedVersion: d.Version, Agent: d.Agent, LoopID: d.LoopID, Revision: d.Revision, Contract: loop.DoerContract{Task: "changed", Workspace: d.Contract.Workspace, WritableFiles: d.Contract.WritableFiles, VerifyFile: d.Contract.VerifyFile, MaxAttempts: d.Contract.MaxAttempts}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.reload(context.Background(), v.ID); err == nil {
		t.Fatal("stale draft accepted")
	}
}
