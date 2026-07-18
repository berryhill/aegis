package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

type peerUIDKey struct{}

type HTTPObservation struct {
	Route    string
	Method   string
	Status   int
	Duration time.Duration
}

type Telemetry interface {
	ObserveHTTP(context.Context, HTTPObservation)
}

type noopTelemetry struct{}

func (noopTelemetry) ObserveHTTP(context.Context, HTTPObservation) {}

type envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func classifyError(err error) (int, string, string) {
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, app.ErrUnauthenticated):
		return http.StatusUnauthorized, "unauthenticated", "authentication failed"
	case errors.Is(err, app.ErrDenied):
		return http.StatusForbidden, "denied", "authorization denied"
	case errors.Is(err, app.ErrAmbiguous):
		return http.StatusConflict, "ambiguous", "authorization is ambiguous"
	case errors.Is(err, app.ErrConflict):
		return http.StatusConflict, "conflict", "state conflict"
	case errors.Is(err, app.ErrExpired):
		return http.StatusConflict, "expired", "authority expired"
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, "not_found", "resource not found"
	}
	var httpError *echo.HTTPError
	if errors.As(err, &httpError) {
		status = httpError.Code
		if status < 500 {
			code, message = "invalid_request", http.StatusText(status)
		}
	}
	return status, code, message
}

type limiter struct {
	mu      sync.Mutex
	clients map[string]*bucket
}
type bucket struct {
	at     time.Time
	tokens float64
}

func newLimiter() *limiter { return &limiter{clients: map[string]*bucket{}} }
func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.clients[key]
	if b == nil {
		b = &bucket{at: now, tokens: 20}
		l.clients[key] = b
	}
	b.tokens += now.Sub(b.at).Seconds() * 5
	if b.tokens > 20 {
		b.tokens = 20
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
func requestID() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func sourceKey(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}

func Serve(ctx context.Context, svc *app.Service) error {
	return ServeWithTelemetry(ctx, svc, noopTelemetry{})
}

func ServeWithTelemetry(ctx context.Context, svc *app.Service, telemetry Telemetry) error {
	if svc.Config.API.Token == "" {
		return errors.New("api.token is required to serve the protected control plane")
	}
	if telemetry == nil {
		telemetry = noopTelemetry{}
	}
	e := echo.New()
	var ready atomic.Bool
	ready.Store(true)
	preAuthLimit := newLimiter()
	postAuthLimit := newLimiter()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, code, msg := classifyError(err)
		rid, _ := c.Get("request_id").(string)
		svc.Log.ErrorContext(c.Request().Context(), "API request failed", "request_id", rid, "route", c.Path(), "error", err)
		_ = c.JSON(status, envelope{code, msg, rid})
	}
	// Outer-to-inner: request ID, structured logging, recovery, body limit,
	// coarse source rate limit. Protected routes add authentication.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			rid := c.Request().Header.Get("X-Request-ID")
			if len(rid) < 8 || len(rid) > 128 {
				rid = requestID()
			}
			c.Set("request_id", rid)
			c.Response().Header().Set("X-Request-ID", rid)
			return next(c)
		}
	})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)
			duration := time.Since(start)
			status := http.StatusOK
			if response, ok := c.Response().(*echo.Response); ok {
				status = response.Status
			}
			if err != nil {
				status, _, _ = classifyError(err)
			}
			route := c.Path()
			telemetry.ObserveHTTP(c.Request().Context(), HTTPObservation{Route: route, Method: c.Request().Method, Status: status, Duration: duration})
			svc.Log.InfoContext(c.Request().Context(), "API request", "request_id", c.Get("request_id"), "method", c.Request().Method, "route", route, "duration", duration)
			return err
		}
	})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) (err error) {
			defer func() {
				if recover() != nil {
					err = echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
				}
			}()
			return next(c)
		}
	})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, svc.Config.API.MaxBodyBytes)
			return next(c)
		}
	})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !preAuthLimit.allow(sourceKey(c.Request())) {
				return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
			}
			return next(c)
		}
	})
	e.GET("/livez", func(c *echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "live"}) })
	e.GET("/readyz", func(c *echo.Context) error {
		if !ready.Load() {
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "draining"})
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
	})
	protected := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h := c.Request().Header.Get("Authorization")
			token, ok := strings.CutPrefix(h, "Bearer ")
			if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(svc.Config.API.Token)) != 1 {
				return app.ErrUnauthenticated
			}
			uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
			if !ok {
				// Bearer authentication is transport-only. Kernel peer evidence is
				// required before constructing an Aegis subject.
				return app.ErrUnauthenticated
			}
			subject, err := svc.AuthenticateUnixPeer(c.Request().Context(), uid)
			if err != nil {
				return err
			}
			if !postAuthLimit.allow(subject.ID) {
				return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
			}
			c.Set("subject", subject)
			return next(c)
		}
	}
	g := e.Group("/v1")
	g.Use(protected)
	g.GET("/runtime", func(c *echo.Context) error {
		x, err := svc.Runtime(c.Request().Context())
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{"resolved_runtime": x, "selection_source": "configured_default", "visible": true})
	})
	g.GET("/config", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, config.Redacted(svc.Config))
	})
	g.GET("/agents", func(c *echo.Context) error {
		x, err := svc.ListAgents()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, x)
	})
	g.GET("/agents/:agent/charters", func(c *echo.Context) error {
		x, err := svc.ListCharters(c.Param("agent"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, x)
	})
	g.GET("/charters/:agent/:revision", func(c *echo.Context) error {
		revision, err := strconv.ParseUint(c.Param("revision"), 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid revision")
		}
		charter, err := svc.GetCharter(c.Param("agent"), revision)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, charter)
	})
	g.POST("/charters/validate", func(c *echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		charter, err := svc.ValidateCharterAs(c.Request().Context(), subject, body)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, charter)
	})
	g.POST("/charters/import", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		}
		charter, err := svc.ImportCharterAs(c.Request().Context(), subject, body)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, charter)
	})
	g.GET("/charters/:agent/:revision/stanzas/:stanza", func(c *echo.Context) error {
		revision, err := strconv.ParseUint(c.Param("revision"), 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid revision")
		}
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		environmentName := c.QueryParam("environment")
		if environmentName == "" {
			environmentName = "local"
		}
		digest, authority, decision, err := svc.EffectiveAuthorityAs(subject, c.Param("agent"), revision, c.Param("stanza"), core.Environment{Name: environmentName})
		if err != nil {
			status, _, _ := classifyError(err)
			return c.JSON(status, map[string]any{"charter_digest": digest, "authority": authority, "decision": decision, "authority_not_unioned": true})
		}
		return c.JSON(http.StatusOK, map[string]any{"charter_digest": digest, "authority": authority, "decision": decision, "authority_not_unioned": true})
	})
	g.POST("/design", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Requirements string `json:"requirements"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		charter, err := svc.DesignSmokeAs(c.Request().Context(), subject, []byte(input.Requirements))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, charter)
	})
	g.POST("/plans/preview", func(c *echo.Context) error {
		var input struct {
			Agent       string           `json:"agent"`
			Revision    uint64           `json:"revision"`
			Environment core.Environment `json:"environment"`
		}
		if err := decode(c, &input); err != nil {
			return err
		}
		review, err := svc.PreviewPlan(c.Request().Context(), input.Agent, input.Revision, input.Environment)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, review)
	})
	g.GET("/plans", func(c *echo.Context) error {
		plans, err := svc.ListPlans()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, plans)
	})
	g.GET("/plans/:id", func(c *echo.Context) error {
		plan, err := svc.GetPlan(c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, plan)
	})
	g.GET("/approvals", func(c *echo.Context) error {
		approvals, err := svc.ListApprovals()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, approvals)
	})
	g.GET("/approvals/:id", func(c *echo.Context) error {
		approval, err := svc.GetApproval(c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, approval)
	})
	g.POST("/approvals", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			PlanID string `json:"plan_id"`
			TTL    string `json:"ttl"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		ttl, err := time.ParseDuration(input.TTL)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid approval ttl")
		}
		approval, err := svc.RequestApprovalAs(c.Request().Context(), subject, input.PlanID, ttl)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, approval)
	})
	g.POST("/approvals/:id/decision", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Approve bool `json:"approve"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		approval, err := svc.DecideApprovalAs(c.Request().Context(), subject, c.Param("id"), input.Approve)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, approval)
	})
	g.POST("/provision", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			PlanID     string `json:"plan_id"`
			ApprovalID string `json:"approval_id"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		receipt, err := svc.ApplyAs(c.Request().Context(), subject, input.PlanID, input.ApprovalID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, receipt)
	})
	g.GET("/receipts", func(c *echo.Context) error {
		receipts, err := svc.ListReceipts()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, receipts)
	})
	g.GET("/receipts/:id", func(c *echo.Context) error {
		receipt, err := svc.GetReceipt(c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, receipt)
	})
	g.POST("/sessions/preview", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Agent       string           `json:"agent"`
			Revision    uint64           `json:"revision"`
			Stanza      string           `json:"stanza"`
			Environment core.Environment `json:"environment"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		mandate, decision, err := svc.PreviewSessionAs(c.Request().Context(), subject, input.Agent, input.Revision, input.Stanza, input.Environment)
		if err != nil {
			status, _, _ := classifyError(err)
			return c.JSON(status, map[string]any{"mandate": mandate, "decision": decision})
		}
		return c.JSON(http.StatusCreated, map[string]any{"mandate": mandate, "decision": decision})
	})
	g.POST("/sessions/start", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			MandateID string `json:"mandate_id"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		session, err := svc.StartSessionAs(c.Request().Context(), subject, input.MandateID)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, session)
	})
	g.GET("/sessions", func(c *echo.Context) error {
		x, err := svc.ListSessions()
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, x)
	})
	g.GET("/sessions/:id", func(c *echo.Context) error {
		x, alive, err := svc.InspectSession(c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{"session": x, "runtime_process_alive": alive})
	})
	g.POST("/sessions/:id/revoke", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Reason string `json:"reason"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.Reason == "" {
			input.Reason = "api_operator_request"
		}
		if err = svc.RevokeSessionAs(c.Request().Context(), subject, c.Param("id"), input.Reason); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "revoked"})
	})
	g.POST("/sessions/:id/terminate", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Reason string `json:"reason"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.Reason == "" {
			input.Reason = "api_operator_request"
		}
		if err = svc.TerminateSessionAs(c.Request().Context(), subject, c.Param("id"), input.Reason); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "terminated"})
	})
	g.GET("/audit", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		events, err := svc.AuditEventsAs(subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, events)
	})
	g.GET("/audit/verify", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = svc.VerifyAuditAs(subject); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]bool{"valid": true})
	})
	g.POST("/authorization/explain", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var in struct {
			Agent       string           `json:"agent"`
			Revision    uint64           `json:"revision"`
			Stanza      string           `json:"stanza"`
			Environment core.Environment `json:"environment"`
		}
		if err = decode(c, &in); err != nil {
			return err
		}
		d, err := svc.ExplainAs(c.Request().Context(), subject, in.Agent, in.Revision, in.Stanza, in.Environment)
		if err != nil {
			status, _, _ := classifyError(err)
			return c.JSON(status, d)
		}
		return c.JSON(http.StatusOK, d)
	})
	srv := &http.Server{Addr: svc.Config.API.Listen, Handler: e, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: svc.Config.API.ReadTimeout, WriteTimeout: svc.Config.API.WriteTimeout, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 256 << 10}
	var listener net.Listener
	if svc.Config.API.UnixSocket != "" {
		if err := os.MkdirAll(filepath.Dir(svc.Config.API.UnixSocket), 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(svc.Config.API.UnixSocket); err == nil {
			if info.Mode()&os.ModeSocket == 0 {
				return errors.New("api.unix_socket exists and is not a socket")
			}
			if err = os.Remove(svc.Config.API.UnixSocket); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var err error
		listener, err = net.Listen("unix", svc.Config.API.UnixSocket)
		if err != nil {
			return err
		}
		if err = os.Chmod(svc.Config.API.UnixSocket, 0600); err != nil {
			_ = listener.Close()
			return err
		}
		defer os.Remove(svc.Config.API.UnixSocket) //nolint:errcheck
		srv.ConnContext = unixPeerContext
	} else {
		var err error
		listener, err = net.Listen("tcp", svc.Config.API.Listen)
		if err != nil {
			return err
		}
		if svc.Config.API.TLSCertFile != "" {
			certificate, loadErr := tls.LoadX509KeyPair(svc.Config.API.TLSCertFile, svc.Config.API.TLSKeyFile)
			if loadErr != nil {
				_ = listener.Close()
				return fmt.Errorf("load API TLS identity: %w", loadErr)
			}
			listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
		}
	}
	defer listener.Close()
	supervisorCtx, stopSupervisor := context.WithCancel(ctx)
	defer stopSupervisor()
	supervisorErr := make(chan error, 1)
	go func() { supervisorErr <- svc.Supervise(supervisorCtx) }()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()
	select {
	case err := <-errCh:
		stopSupervisor()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		ready.Store(false)
		shutdown, cancel := context.WithTimeout(context.Background(), svc.Config.API.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
			return fmt.Errorf("API shutdown: %w", err)
		}
		err := <-errCh
		if supervisorRunErr := <-supervisorErr; supervisorRunErr != nil {
			return supervisorRunErr
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-supervisorErr:
		ready.Store(false)
		_ = srv.Close()
		return err
	}
}

func requestSubject(c *echo.Context) (core.Subject, error) {
	subject, ok := c.Get("subject").(core.Subject)
	if !ok {
		return core.Subject{}, app.ErrUnauthenticated
	}
	return subject, nil
}

func decode(c *echo.Context, v any) error {
	if !strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/json") {
		return echo.NewHTTPError(http.StatusUnsupportedMediaType, "application/json required")
	}
	d := json.NewDecoder(c.Request().Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if errors.Is(d.Decode(&struct{}{}), io.EOF) {
		return nil
	}
	return echo.NewHTTPError(http.StatusBadRequest, "trailing JSON")
}
