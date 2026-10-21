package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Mock only independent human CONFIRM, not kernel authentication/signing or
// execution. Production launch/decision remains pinned-companion-only.
func approveRequeueNativeFixture(t *testing.T, p *DoerContinuationPortal, id string) {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, _, _ := classifyError(err)
		_ = c.String(status, err.Error())
	}
	g := e.Group("/v1")
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
			if !ok {
				return app.ErrDenied
			}
			sub, err := p.svc.AuthenticateUnixPeer(c.Request().Context(), uid)
			if err != nil {
				return err
			}
			c.Set("subject", sub)
			return next(c)
		}
	})
	p.RegisterNative(g)
	path := socketTestDir(t) + "/rq.sock"
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: e, ConnContext: unixPeerContext}
	go func() { _ = server.Serve(l) }()
	defer server.Close()
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}, DisableKeepAlives: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	p.mu.Lock()
	p.pending[id].Launching = true
	p.pending[id].CompanionPID = os.Getpid()
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.pending[id].Launching = false; p.pending[id].CompanionPID = 0; p.mu.Unlock() }()
	request := func(method, suffix, body string) *http.Response {
		req, _ := http.NewRequest(method, "http://127.0.0.1/v1/doer-continuations/pending/"+id+suffix, strings.NewReader(body))
		req.Header.Set("Origin", p.svc.Config.API.Console.Origin)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 200 {
			t.Fatalf("native requeue %s: %d", method, res.StatusCode)
		}
		return res
	}
	res := request("GET", "", "")
	var review app.DoerContinuationReview
	err = json.NewDecoder(res.Body).Decode(&review)
	res.Body.Close()
	if err != nil || review.ContentDigest() != review.Digest {
		t.Fatal("native review drift", err)
	}
	raw, _ := json.Marshal(map[string]string{"decision": "approve", "expected_digest": review.Digest})
	res = request("POST", "/decision", string(raw))
	res.Body.Close()
	p.mu.Lock()
	approvalID := p.pending[id].ApprovalID
	p.mu.Unlock()
	approval, err := p.svc.Store.ReadDoerContinuation(approvalID)
	if err != nil || approval.Executor.Issuer != "linux-so-peercred" || approval.Intent.Run.Reexecution == nil || approval.Intent.RequeueBinding == "" {
		t.Fatal("missing signed genuine predecessor executor", err)
	}
}
