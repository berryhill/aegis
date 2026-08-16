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
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/managergateway"
	consoleweb "github.com/berryhill/aegis/web/console"
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
	case app.IsFleetDenied(err):
		return http.StatusForbidden, "denied", "fleet authority denied"
	case app.IsFleetUnavailable(err):
		return http.StatusServiceUnavailable, "unavailable", "fleet control unavailable"
	case app.IsFleetConflict(err):
		return http.StatusConflict, "conflict", "immutable fleet record conflict"
	case app.IsFleetNotFound(err):
		return http.StatusNotFound, "not_found", "fleet resource not found"
	case app.IsFleetCorrupt(err):
		return http.StatusServiceUnavailable, "repair_required", "fleet store repair required"
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
	consoleManager, err := console.New(console.Config{
		Origin:       svc.Config.API.Console.Origin,
		SessionTTL:   svc.Config.API.Console.SessionTTL,
		BootstrapTTL: svc.Config.API.Console.BootstrapTTL,
		MaxPageSize:  svc.Config.API.Console.MaxPageSize,
	}, svc.Now)
	if err != nil {
		return fmt.Errorf("configure console: %w", err)
	}
	managerGateway, err := managergateway.New(svc)
	if err != nil {
		return fmt.Errorf("configure manager gateway: %w", err)
	}
	e := echo.New()
	var ready atomic.Bool
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
	authenticateTransport := func(c *echo.Context) (uint32, error) {
		h := c.Request().Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(svc.Config.API.Token)) != 1 {
			return 0, app.ErrUnauthenticated
		}
		uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
		if !ok {
			// Bearer authentication is transport-only. Kernel peer evidence is
			// required before constructing an Aegis subject.
			return 0, app.ErrUnauthenticated
		}
		return uid, nil
	}
	protected := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			uid, err := authenticateTransport(c)
			if err != nil {
				return err
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
	e.GET("/livez", func(c *echo.Context) error { return c.JSON(http.StatusOK, map[string]string{"status": "live"}) })
	e.GET("/readyz", func(c *echo.Context) error {
		uid, err := authenticateTransport(c)
		if err != nil {
			return err
		}
		// Readiness authentication is deliberately observational. Transport
		// authentication above uses the bearer and kernel SO_PEERCRED directly;
		// a normal protected-route authentication event would enqueue fresh audit
		// work immediately before the audit-current check and stale its own result.
		subjectID := "local-uid:" + strconv.FormatUint(uint64(uid), 10)
		if !postAuthLimit.allow(subjectID) {
			return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
		}
		if !ready.Load() {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{"status": "draining", "audit": core.AuditDeliveryStatus{State: "unverifiable", Reason: "service_draining", Verifiable: false}})
		}
		auditStatus := svc.AuditDeliveryReadiness()
		if !auditStatus.Current {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "audit": auditStatus})
		}
		return c.JSON(http.StatusOK, map[string]any{"status": "ready", "audit": auditStatus})
	})
	consoleError := func(err error) error {
		switch {
		case errors.Is(err, console.ErrUnauthenticated):
			return app.ErrUnauthenticated
		case errors.Is(err, console.ErrDenied):
			return app.ErrDenied
		case errors.Is(err, console.ErrInvalidInput):
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console input")
		default:
			return err
		}
	}
	consoleHeaders := func(c *echo.Context, authenticated bool) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), authenticated)
		return consoleManager.ValidateOrigin(c.Request(), false)
	}
	loadConsole := func(c *echo.Context, subject core.Subject, domain consoleDomain) (consoleweb.PageModel, error) {
		if err := svc.RequirePrincipal(subject); err != nil {
			return consoleweb.PageModel{}, err
		}
		limit, err := consoleManager.Page(c.QueryParam("limit"))
		if err != nil {
			return consoleweb.PageModel{}, consoleError(err)
		}
		surface, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return consoleweb.PageModel{Authenticated: true, Surface: consoleweb.SurfaceModel{Domain: string(domain), Title: "Fleet control", State: "unavailable", Status: "Fleet control unavailable. No collection was treated as empty."}}, nil
		}
		if len(surface.Agents) > limit {
			surface.Agents = surface.Agents[:limit]
		}
		if len(surface.Loops) > limit {
			surface.Loops = surface.Loops[:limit]
		}
		if len(surface.Graphs) > limit {
			surface.Graphs = surface.Graphs[:limit]
		}
		if len(surface.Queue) > limit {
			surface.Queue = surface.Queue[:limit]
		}
		model, err := consoleSurfaceModel(surface, domain)
		if err != nil {
			return consoleweb.PageModel{}, err
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleweb.PageModel{}, consoleError(err)
		}
		return consoleweb.PageModel{Authenticated: true, CSRF: csrf, Surface: model}, nil
	}
	e.GET("/console", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		domain, err := parseConsoleDomain(c.QueryParam("domain"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console domain")
		}
		model := consoleweb.PageModel{Surface: consoleweb.SurfaceModel{Domain: string(domain)}}
		if subject, err := consoleManager.Authenticate(c.Request()); err == nil {
			model, err = loadConsole(c, subject, domain)
			if err != nil {
				return err
			}
			if recordKey := c.QueryParam("record_key"); recordKey != "" {
				if err = selectConsoleRecord(&model.Surface, recordKey); err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, "invalid console record")
				}
			}
		}
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(model))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	})
	e.GET("/favicon.ico", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		return c.NoContent(http.StatusNoContent)
	})
	e.GET("/console/assets/app.css", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		return c.Blob(http.StatusOK, "text/css; charset=utf-8", console.Styles())
	})
	e.GET("/console/assets/datastar-v1.0.2.js", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		return c.Blob(http.StatusOK, "text/javascript; charset=utf-8", console.Datastar())
	})
	e.POST("/console/session", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		var input consoleSignals
		nativeForm := isConsoleForm(c.Request())
		if nativeForm {
			bootstrap, err := decodeConsoleForm(c.Request(), "bootstrap")
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid console form")
			}
			input.Bootstrap = bootstrap
		} else if err := decode(c, &input); err != nil {
			return err
		}
		sessionValue, csrf, expires, err := consoleManager.Exchange(c.Request(), input.Bootstrap)
		if err != nil {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), core.Subject{}, "denied", "browser_session_exchange_denied"); auditErr != nil {
				return auditErr
			}
			return consoleError(err)
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), core.Subject{PrincipalID: svc.Config.Principal.ID}, "success", "browser_session_issued"); err != nil {
			consoleManager.RevokeSessionValue(sessionValue)
			return err
		}
		consoleManager.SetCookie(c.Response(), sessionValue)
		if nativeForm {
			return c.Redirect(http.StatusSeeOther, "/console")
		}
		if wantsDatastar(c.Request()) {
			c.Request().AddCookie(&http.Cookie{Name: console.CookieName, Value: sessionValue})
			subject, authErr := consoleManager.Authenticate(c.Request())
			if authErr != nil {
				return consoleError(authErr)
			}
			model, loadErr := loadConsole(c, subject, consoleAgents)
			if loadErr != nil {
				return loadErr
			}
			return patchConsole(c.Response(), c.Request(), consoleweb.Document(model))
		}
		return c.JSON(http.StatusCreated, map[string]string{"csrf": csrf, "expires": expires.UTC().Format(time.RFC3339)})
	})
	e.GET("/console/fragments/surface", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		if err := validateConsoleSignals(c.Request()); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console signals")
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			consoleManager.ClearCookie(c.Response())
			return consoleError(err)
		}
		domain, err := parseConsoleDomain(c.QueryParam("domain"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console domain")
		}
		model, err := loadConsole(c, subject, domain)
		if err != nil {
			return err
		}
		return patchConsole(c.Response(), c.Request(), consoleweb.Document(model))
	})
	e.GET("/console/fragments/inspect", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		if err := validateConsoleSignals(c.Request()); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console signals")
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			consoleManager.ClearCookie(c.Response())
			return consoleError(err)
		}
		domain, err := parseConsoleDomain(c.QueryParam("domain"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console domain")
		}
		model, err := loadConsole(c, subject, domain)
		if err != nil {
			return err
		}
		if err = selectConsoleRecord(&model.Surface, c.QueryParam("record_key")); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console record")
		}
		return patchConsole(c.Response(), c.Request(), consoleweb.Document(model))
	})
	e.GET("/console/api/state", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			consoleManager.ClearCookie(c.Response())
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		limit, err := consoleManager.Page(c.QueryParam("limit"))
		if err != nil {
			return consoleError(err)
		}
		surface, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{"state": "unavailable", "readiness": map[string]any{"fleet": map[string]any{"state": "unavailable", "authoritative": false}}})
		}
		if len(surface.Agents) > limit {
			surface.Agents = surface.Agents[:limit]
		}
		if len(surface.Loops) > limit {
			surface.Loops = surface.Loops[:limit]
		}
		if len(surface.Graphs) > limit {
			surface.Graphs = surface.Graphs[:limit]
		}
		if len(surface.Queue) > limit {
			surface.Queue = surface.Queue[:limit]
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(http.StatusOK, map[string]any{"state": "ready", "surface": surface, "csrf": csrf, "limit": limit})
	})
	logout := func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		nativeForm := c.Request().Method == http.MethodPost
		if nativeForm {
			csrf, err := decodeConsoleForm(c.Request(), "csrf")
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid console form")
			}
			c.Request().Header.Set("X-CSRF-Token", csrf)
		}
		if wantsDatastar(c.Request()) {
			if err := validateConsoleSignals(c.Request()); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid console signals")
			}
		}
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), subject, "success", "browser_session_revoked"); err != nil {
			return err
		}
		consoleManager.Revoke(c.Request())
		consoleManager.ClearCookie(c.Response())
		if nativeForm {
			return c.Redirect(http.StatusSeeOther, "/console")
		}
		if wantsDatastar(c.Request()) {
			return patchConsole(c.Response(), c.Request(), consoleweb.Document(consoleweb.PageModel{Surface: consoleweb.SurfaceModel{Domain: string(consoleAgents)}}))
		}
		return c.NoContent(http.StatusNoContent)
	}
	e.DELETE("/console/session", logout)
	e.POST("/console/logout", logout)
	g := e.Group("/v1")
	g.Use(protected)
	g.POST("/manager/sessions", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = decode(c, &struct{}{}); err != nil {
			return err
		}
		opened, err := managerGateway.Open(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, opened)
	})
	g.POST("/manager/sessions/:session/commands", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Input string `json:"input"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		result, err := managerGateway.Execute(c.Request().Context(), subject, c.Param("session"), c.Request().Header.Get(managergateway.SessionHeader), input.Input)
		if err != nil {
			if !errors.Is(err, app.ErrUnauthenticated) && !errors.Is(err, app.ErrDenied) && !errors.Is(err, app.ErrExpired) {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid manager command")
			}
			return err
		}
		return c.JSON(http.StatusOK, result)
	})
	g.DELETE("/manager/sessions/:session", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = managerGateway.Close(c.Request().Context(), subject, c.Param("session"), c.Request().Header.Get(managergateway.SessionHeader)); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	})
	g.POST("/console/bootstrap", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), subject, "denied", "browser_bootstrap_principal_denied"); auditErr != nil {
				return auditErr
			}
			return err
		}
		bootstrap, err := consoleManager.IssueBootstrap(subject)
		if err != nil {
			return consoleError(err)
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), subject, "success", "browser_bootstrap_issued"); err != nil {
			consoleManager.RevokeBootstrap(bootstrap)
			return err
		}
		return c.JSON(http.StatusCreated, map[string]string{"bootstrap": bootstrap, "expires": svc.Now().Add(svc.Config.API.Console.BootstrapTTL).UTC().Format(time.RFC3339)})
	})
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
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		x, err := svc.ListFleetAgentsAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, x)
	})
	g.POST("/agents", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.RegisterFleetAgentInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, created, err := svc.RegisterFleetAgentAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		return c.JSON(status, map[string]any{"agent": value, "created": created})
	})
	g.GET("/agents/:agent", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		revision, err := optionalRevision(c.QueryParam("revision"))
		if err != nil {
			return err
		}
		value, err := svc.GetFleetAgentAs(c.Request().Context(), subject, c.Param("agent"), revision)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.GET("/loops", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		values, err := svc.ListLoopsAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, values)
	})
	g.POST("/loops", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.PublishLoopInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, err := svc.PublishLoopAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, value)
	})
	g.GET("/loops/:loop/:revision", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		revision, err := requiredRevision(c.Param("revision"))
		if err != nil {
			return err
		}
		value, err := svc.GetLoopAs(c.Request().Context(), subject, c.Param("loop"), revision)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.GET("/graphs", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		values, err := svc.ListGraphsAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, values)
	})
	g.POST("/graphs", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.PublishGraphInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, err := svc.PublishGraphAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, value)
	})
	g.GET("/graphs/:graph/:revision", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		revision, err := requiredRevision(c.Param("revision"))
		if err != nil {
			return err
		}
		value, err := svc.GetGraphAs(c.Request().Context(), subject, c.Param("graph"), revision)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.GET("/queue", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		values, err := svc.ListQueueAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, values)
	})
	g.GET("/fleet/readiness", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value.Readiness)
	})
	g.POST("/queue", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SubmitGraphInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, err := svc.SubmitGraphAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		status := http.StatusCreated
		if !value.Created {
			status = http.StatusOK
		}
		return c.JSON(status, value)
	})
	g.GET("/queue/:item", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.GetQueueItemAs(c.Request().Context(), subject, c.Param("item"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.POST("/queue/:item/process", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.ProcessQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.ProcessQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
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
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Agent       string           `json:"agent"`
			Revision    uint64           `json:"revision"`
			Environment core.Environment `json:"environment"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		review, err := svc.PreviewPlanAs(c.Request().Context(), subject, input.Agent, input.Revision, input.Environment)
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
	g.GET("/sessions/:id/authority", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.FleetAuthorityForSessionAs(c.Request().Context(), subject, c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
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
	g.GET("/audit/delivery/status", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		status, err := svc.AuditDeliveryStatusAs(subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, status)
	})
	g.POST("/audit/delivery", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input struct {
			Limit int `json:"limit"`
		}
		if err = decode(c, &input); err != nil {
			return err
		}
		result, err := svc.DeliverAuditAs(c.Request().Context(), subject, input.Limit)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, result)
	})
	g.GET("/audit/delivery/verify", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		if err = svc.VerifyAuditProjectionAs(subject); err != nil {
			return err
		}
		status, err := svc.AuditDeliveryStatusAs(subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]bool{"valid": true, "current": status.Current})
	})
	g.POST("/audit/projection/rebuild", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		status, err := svc.RebuildAuditProjectionAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, status)
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
	srv.ConnContext = func(connectionContext context.Context, connection net.Conn) context.Context {
		if connection.LocalAddr().Network() == "unix" {
			return unixPeerContext(connectionContext, connection)
		}
		return connectionContext
	}
	listeners := make([]net.Listener, 0, 2)
	var singleton *os.File
	closeListeners := func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}
	if svc.Config.API.UnixSocket != "" {
		if err := ensureSocketDirectory(filepath.Dir(svc.Config.API.UnixSocket)); err != nil {
			return err
		}
		singleton, err = acquireSingleton(svc.Config.API.UnixSocket + ".lock")
		if err != nil {
			return err
		}
		defer func() {
			_ = syscall.Flock(int(singleton.Fd()), syscall.LOCK_UN)
			_ = singleton.Close()
		}()
		if info, err := os.Lstat(svc.Config.API.UnixSocket); err == nil {
			stat, owned := info.Sys().(*syscall.Stat_t)
			if info.Mode()&os.ModeSocket == 0 || !owned || int(stat.Uid) != os.Geteuid() {
				return errors.New("api.unix_socket exists and is not one current-owner socket")
			}
			if err = os.Remove(svc.Config.API.UnixSocket); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		unixListener, err := net.Listen("unix", svc.Config.API.UnixSocket)
		if err != nil {
			return err
		}
		listeners = append(listeners, unixListener)
		if err = os.Chmod(svc.Config.API.UnixSocket, 0600); err != nil {
			closeListeners()
			return err
		}
		defer os.Remove(svc.Config.API.UnixSocket) //nolint:errcheck
	}
	tcpListener, err := net.Listen("tcp", svc.Config.API.Listen)
	if err != nil {
		closeListeners()
		return err
	}
	if svc.Config.API.TLSCertFile != "" {
		certificate, loadErr := tls.LoadX509KeyPair(svc.Config.API.TLSCertFile, svc.Config.API.TLSKeyFile)
		if loadErr != nil {
			_ = tcpListener.Close()
			closeListeners()
			return fmt.Errorf("load API TLS identity: %w", loadErr)
		}
		tcpListener = tls.NewListener(tcpListener, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	}
	listeners = append(listeners, tcpListener)
	ready.Store(true)
	defer closeListeners()
	supervisorCtx, stopSupervisor := context.WithCancel(ctx)
	defer stopSupervisor()
	supervisorErr := make(chan error, 1)
	go func() { supervisorErr <- svc.Supervise(supervisorCtx) }()
	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func(listener net.Listener) { errCh <- srv.Serve(listener) }(listener)
	}
	select {
	case err := <-errCh:
		stopSupervisor()
		_ = srv.Close()
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
		for range listeners {
			if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
		}
		if supervisorRunErr := <-supervisorErr; supervisorRunErr != nil {
			return supervisorRunErr
		}
		return nil
	case err := <-supervisorErr:
		ready.Store(false)
		_ = srv.Close()
		return err
	}
}

func ensureSocketDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("api.unix_socket directory must be current-owner directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		if err = os.Chmod(path, 0700); err != nil {
			return errors.New("api.unix_socket directory could not be made owner-only")
		}
	}
	return nil
}

func acquireSingleton(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		_ = file.Close()
		return nil, errors.New("API singleton lock is unsafe or ambiguous")
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.New("another Aegis control-plane daemon owns this transport")
	}
	return file, nil
}

func optionalRevision(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	return requiredRevision(raw)
}

func requiredRevision(raw string) (uint64, error) {
	revision, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || revision == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "revision must be a positive integer")
	}
	return revision, nil
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
