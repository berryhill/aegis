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
	"mime"
	"net"
	"net/http"
	"net/url"
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
	"github.com/berryhill/aegis/internal/principalauth"
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
	var operationError *consoleQueueOperationError
	switch {
	case errors.Is(err, console.ErrCommandUnknown):
		return http.StatusNotFound, "invalid_request", "console command is not registered"
	case errors.As(err, &operationError):
		return operationError.status, operationError.code, operationError.message
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
	case app.IsFleetAmbiguous(err):
		return http.StatusConflict, "ambiguous_source", "fleet source identity is ambiguous"
	case app.IsFleetConflict(err):
		return http.StatusConflict, "conflict", "immutable fleet record conflict"
	case app.IsFleetNotFound(err):
		return http.StatusNotFound, "not_found", "fleet resource not found"
	case app.IsFleetCorrupt(err):
		return http.StatusServiceUnavailable, "repair_required", "fleet store repair required"
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound, "not_found", "resource not found"
	}
	if httpStatus := echo.StatusCode(err); httpStatus != 0 {
		status = httpStatus
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

func randomConsoleID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate console operation identifier: %w", err)
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func mapConsoleError(err error) error {
	switch {
	case errors.Is(err, console.ErrUnauthenticated):
		return app.ErrUnauthenticated
	case errors.Is(err, console.ErrDenied):
		return app.ErrDenied
	case errors.Is(err, console.ErrInvalidInput):
		return echo.NewHTTPError(http.StatusBadRequest, "invalid console input")
	case errors.Is(err, console.ErrCommandUnknown):
		return echo.NewHTTPError(http.StatusNotFound, "console command is not registered")
	case errors.Is(err, console.ErrCommandConflict), errors.Is(err, console.ErrCommandExpired):
		return echo.NewHTTPError(http.StatusConflict, "console command intent conflict")
	case errors.Is(err, console.ErrCommandFailed):
		return echo.NewHTTPError(http.StatusServiceUnavailable, "console command outcome is uncertain")
	default:
		return err
	}
}

func consoleQueueOperationHandler(svc *app.Service, manager *console.Manager, operator consoleQueueOperator, newID consoleIDFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		manager.ApplySecurityHeaders(c.Response().Header(), true)
		csrf, operation, err := decodeConsoleOperationForm(c.Request())
		if err != nil {
			return queueOperationError(http.StatusBadRequest, "queue_operation_malformed", "invalid queue operation form", err)
		}
		c.Request().Header.Set("X-CSRF-Token", csrf)
		subject, err := manager.AuthorizeMutation(c.Request())
		if err != nil {
			return mapConsoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		if err = operateConsoleQueueItem(c.Request().Context(), operator, subject, c.Param("item"), operation, newID); err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, "/console/queue?record_key="+url.QueryEscape(c.Param("item"))+"#/queue")
	}
}

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

func managerGatewayWriteTimeout(cfg config.Config) time.Duration {
	writeTimeout := cfg.API.WriteTimeout
	startupTimeout := cfg.Manager.Inference.StartTimeout + cfg.Manager.Inference.RequestTimeout + cfg.Manager.Hermes.GatewayStartTimeout
	for _, candidate := range []time.Duration{startupTimeout, cfg.Manager.Hermes.TurnTimeout} {
		if candidate > writeTimeout {
			writeTimeout = candidate
		}
	}
	return writeTimeout + 5*time.Second
}

func ServeWithTelemetry(ctx context.Context, svc *app.Service, telemetry Telemetry) error {
	if svc.Config.API.Token == "" {
		return errors.New("api.token is required to serve the protected control plane")
	}
	if telemetry == nil {
		telemetry = noopTelemetry{}
	}
	verifierPath := filepath.Join(svc.Config.StateDir, "auth", principalauth.FileName)
	stored, err := principalauth.Load(verifierPath)
	if err != nil {
		return fmt.Errorf("load principal authentication verifier: %w", err)
	}
	verifier := &stored
	consoleManager, err := console.New(console.Config{
		Origin:           svc.Config.API.Console.Origin,
		SessionTTL:       svc.Config.API.Console.SessionTTL,
		MaxPageSize:      svc.Config.API.Console.MaxPageSize,
		PasswordVerifier: verifier,
		PrincipalID:      svc.Config.Principal.ID,
		PrincipalAuthTTL: svc.Config.Principal.AuthTTL,
		LoginBurst:       5,
		LoginWindow:      5 * time.Minute,
	}, svc.Now)
	if err != nil {
		return fmt.Errorf("configure console: %w", err)
	}
	managerGateway, err := managergateway.New(ctx, svc)
	if err != nil {
		return fmt.Errorf("configure manager gateway: %w", err)
	}
	commandService, err := console.NewCommandService(loopCommandDefinitions(svc), loopCommandAuthorityProvider(svc), svc.Now)
	if err != nil {
		return fmt.Errorf("configure console command service: %w", err)
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
	consoleError := mapConsoleError
	consoleHeaders := func(c *echo.Context, authenticated bool) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), authenticated)
		return consoleManager.ValidateOrigin(c.Request(), false)
	}
	authenticationModel := func(reasonCode string) consoleweb.AuthenticationModel {
		model := consoleweb.AuthenticationModel{SessionTTL: svc.Config.API.Console.SessionTTL.String()}
		if reasonCode != "" {
			model.Status = "Authentication failed. Enter the enrolled principal password."
			model.ReasonCode = reasonCode
		}
		return model
	}
	renderAuthentication := func(c *echo.Context, status int, reasonCode string) error {
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(consoleweb.PageModel{
			Authentication: authenticationModel(reasonCode),
			Surface:        consoleweb.SurfaceModel{Domain: string(consoleAgents)},
		}))
		if err != nil {
			return err
		}
		return c.Blob(status, "text/html; charset=utf-8", content)
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
		if domain == consoleAgents && c.QueryParam("revision") != "" {
			revision, revisionErr := requiredRevision(c.QueryParam("revision"))
			if revisionErr != nil || c.QueryParam("record_key") == "" {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Agent Registry revision")
			}
			exact, exactErr := svc.GetFleetAgentAs(c.Request().Context(), subject, c.QueryParam("record_key"), revision)
			if exactErr != nil {
				return consoleweb.PageModel{}, exactErr
			}
			replaced := false
			for index := range surface.Agents {
				if surface.Agents[index].Revision.AgentID == exact.Revision.AgentID {
					surface.Agents[index] = exact
					replaced = true
					break
				}
			}
			if !replaced {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Agent Registry revision")
			}
		}
		var credentialPage *app.CredentialCollectionPage
		if domain == consoleCredentials {
			query := strings.TrimSpace(c.QueryParam("q"))
			status := c.QueryParam("status")
			if status == "" {
				status = "all"
			}
			if len(query) > 128 || strings.ContainsAny(query, "\r\n\x00") || (status != "all" && status != "active" && status != "revoked") {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Credentials filter")
			}
			pageNumber := 1
			if raw := c.QueryParam("page"); raw != "" {
				pageNumber, err = strconv.Atoi(raw)
				if err != nil || pageNumber < 1 || pageNumber > 10000 {
					return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Credentials page")
				}
			}
			page, queryErr := svc.QueryCredentialsAs(c.Request().Context(), subject, app.CredentialCollectionQuery{Search: query, Status: status, RecordID: c.QueryParam("record_key"), Page: pageNumber, Limit: limit})
			if queryErr != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Credentials query")
			}
			credentialPage = &page
			surface.Credentials = page.Records
		}
		model, err := consoleSurfaceModel(surface, domain)
		if err != nil {
			return consoleweb.PageModel{}, err
		}
		if domain == consoleAgents {
			if err = filterConsoleAgents(&model, c.QueryParam("q"), c.QueryParam("lifecycle")); err != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Agent Registry filter")
			}
		}
		if domain == consoleLoops || domain == consoleGraphs {
			if err = filterConsoleDefinitions(&model, c.QueryParam("q"), c.QueryParam("lifecycle")); err != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid definition filter")
			}
		}
		if domain == consoleQueue {
			if err = filterConsoleQueue(&model, c.QueryParam("state")); err != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Execution Queue filter")
			}
		}
		if domain != consoleCredentials {
			if err = paginateConsoleCollection(&model, c.QueryParam("page"), c.QueryParam("record_key"), limit, c.QueryParams()); err != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid console page")
			}
			if domain == consoleQueue {
				syncConsoleQueueBands(&model)
			}
		}
		if domain == consoleCredentials && credentialPage != nil {
			model.Query = strings.TrimSpace(c.QueryParam("q"))
			model.Lifecycle = c.QueryParam("status")
			if model.Lifecycle == "" {
				model.Lifecycle = "all"
			}
			if err = applyCredentialPage(&model, *credentialPage, c.QueryParams()); err != nil {
				return consoleweb.PageModel{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Credentials page")
			}
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleweb.PageModel{}, consoleError(err)
		}
		model.CSRF = csrf
		return consoleweb.PageModel{Authenticated: true, CSRF: csrf, Surface: model}, nil
	}
	consolePage := func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		rawDomain := c.QueryParam("domain")
		if routeDomain := strings.TrimPrefix(c.Request().URL.Path, "/console/"); routeDomain != c.Request().URL.Path {
			rawDomain = routeDomain
		}
		domain, err := parseConsoleDomain(rawDomain)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid console domain")
		}
		model := consoleweb.PageModel{Authentication: authenticationModel(""), Surface: consoleweb.SurfaceModel{Domain: string(domain)}}
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
	}
	charterImportPage := func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		model := consoleweb.PageModel{Authentication: authenticationModel(""), Surface: consoleweb.SurfaceModel{Domain: string(consoleAgents)}}
		if subject, err := consoleManager.Authenticate(c.Request()); err == nil {
			model, err = loadConsole(c, subject, consoleAgents)
			if err != nil {
				return err
			}
			model.CharterImport = true
		}
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(model))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	}
	renderCredentialOperation := func(c *echo.Context, status int, subject core.Subject, operation *consoleweb.CredentialOperationModel) error {
		model, err := loadConsole(c, subject, consoleCredentials)
		if err != nil {
			return err
		}
		model.CredentialOperation = operation
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(model))
		if err != nil {
			return err
		}
		return c.Blob(status, "text/html; charset=utf-8", content)
	}
	renderAgentOperation := func(c *echo.Context, status int, subject core.Subject, operation *consoleweb.AgentOperationModel) error {
		model, err := loadConsole(c, subject, consoleAgents)
		if err != nil {
			return err
		}
		model.CharterImport, model.AgentOperation = true, operation
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(model))
		if err != nil {
			return err
		}
		return c.Blob(status, "text/html; charset=utf-8", content)
	}
	authorizeAgentReview := func(c *echo.Context) (core.Subject, agentOperationForm, error) {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		form, err := decodeAgentOperationForm(c.Request())
		if err != nil {
			return core.Subject{}, agentOperationForm{}, echo.NewHTTPError(http.StatusBadRequest, "invalid Agent operation form")
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return core.Subject{}, agentOperationForm{}, consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return core.Subject{}, agentOperationForm{}, err
		}
		return subject, form, nil
	}
	e.GET("/console", consolePage)
	for _, domain := range []consoleDomain{consoleAgents, consoleGraphs, consoleLoops, consoleQueue, consoleCredentials} {
		e.GET("/console/"+string(domain), consolePage)
	}
	e.GET("/console/agents/charter-import", charterImportPage)
	e.GET("/console/credentials/operation", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			return renderAuthentication(c, http.StatusUnauthorized, "console_authentication_required")
		}
		operation := c.QueryParam("operation")
		switch operation {
		case "create", "rotate", "revoke", "bind", "backup":
		default:
			return echo.NewHTTPError(http.StatusBadRequest, "credential operation is not browser-enabled")
		}
		return renderCredentialOperation(c, http.StatusOK, subject, credentialOperationModel(credentialOperationForm{Operation: operation, RecordID: c.QueryParam("record_id")}, "prepare"))
	})
	e.POST("/console/credentials/operation/review", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		form, err := decodeCredentialOperationForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid credential operation form")
		}
		defer wipeBytes(form.Value)
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		if err = verifyCredentialTarget(subject, svc, c.Request(), form); err != nil {
			operation := credentialOperationModel(form, "prepare")
			operation.Status, operation.ReasonCode = "Credential operation denied during authoritative review.", "credential_target_denied"
			return renderCredentialOperation(c, http.StatusConflict, subject, operation)
		}
		payload, err := encodeCredentialReview(form)
		if err != nil {
			return err
		}
		defer wipeBytes(payload)
		receipt, err := consoleManager.IssueReviewReceipt(c.Request(), credentialReviewPurpose, payload)
		if err != nil {
			return consoleError(err)
		}
		operation := credentialOperationModel(form, "review")
		operation.Receipt, operation.Status = receipt, "Review prepared; no state has changed and no secret is rendered."
		return renderCredentialOperation(c, http.StatusOK, subject, operation)
	})
	e.POST("/console/credentials/operation/cancel", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		execute, err := decodeAgentExecuteForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid credential cancellation")
		}
		c.Request().Header.Set("X-CSRF-Token", execute.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		if err = consoleManager.CancelReviewReceipt(c.Request(), credentialReviewPurpose, execute.Receipt); err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "credential review receipt expired, consumed, or denied")
		}
		return c.Redirect(http.StatusSeeOther, "/console/credentials#/credentials")
	})
	e.POST("/console/credentials/operation/execute", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		execute, err := decodeAgentExecuteForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid credential confirmation")
		}
		c.Request().Header.Set("X-CSRF-Token", execute.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		payload, err := consoleManager.ConsumeReviewReceipt(c.Request(), credentialReviewPurpose, execute.Receipt)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "credential review receipt expired, consumed, or denied")
		}
		defer wipeBytes(payload)
		form, err := decodeCredentialReview(payload)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "credential review receipt denied")
		}
		defer wipeBytes(form.Value)
		operation := credentialOperationModel(form, "result")
		if err = verifyCredentialTarget(subject, svc, c.Request(), form); err != nil {
			operation.Status, operation.ReasonCode = "Credential operation denied after authoritative revalidation.", "credential_target_conflict"
			return renderCredentialOperation(c, http.StatusConflict, subject, operation)
		}
		resultID, message, err := executeCredentialOperation(svc, c.Request(), subject, form)
		if err != nil {
			operation.Status, operation.ReasonCode = "Credential operation denied.", "credential_operation_denied"
			return renderCredentialOperation(c, http.StatusConflict, subject, operation)
		}
		operation.Status, operation.Result = "Credential operation completed with metadata-only authoritative readback.", credentialReceipt(form.Operation, resultID, message)
		return renderCredentialOperation(c, http.StatusOK, subject, operation)
	})
	e.POST("/console/agents/registration/review", func(c *echo.Context) error {
		subject, form, err := authorizeAgentReview(c)
		if err != nil {
			return err
		}
		operation, err := prepareAgentOperation(c.Request().Context(), svc, subject, form)
		if err != nil {
			return renderAgentOperation(c, http.StatusBadRequest, subject, &consoleweb.AgentOperationModel{Stage: "error", Status: "Registration proposal denied.", ReasonCode: agentOperationReason(err), Charter: form.Charter, Fixture: form.Fixture, FleetID: form.FleetID, SourceID: form.SourceID})
		}
		payload, err := encodeAgentReviewPayload(form)
		if err != nil {
			return err
		}
		operation.Receipt, err = consoleManager.IssueReviewReceipt(c.Request(), agentRegistrationReviewPurpose, payload)
		if err != nil {
			return consoleError(err)
		}
		return renderAgentOperation(c, http.StatusOK, subject, operation)
	})
	e.POST("/console/agents/registration/execute", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		execute, err := decodeAgentExecuteForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Agent execute form")
		}
		c.Request().Header.Set("X-CSRF-Token", execute.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		payload, err := consoleManager.ConsumeReviewReceipt(c.Request(), agentRegistrationReviewPurpose, execute.Receipt)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "Agent registration review receipt denied")
		}
		form, err := decodeAgentReviewPayload(payload)
		if err != nil {
			return echo.NewHTTPError(http.StatusForbidden, "Agent registration review receipt denied")
		}
		operation, err := prepareAgentOperation(c.Request().Context(), svc, subject, form)
		if err != nil {
			return renderAgentOperation(c, http.StatusConflict, subject, &consoleweb.AgentOperationModel{Stage: "error", Status: "Registration execute denied after authoritative revalidation.", ReasonCode: agentOperationReason(err), Charter: form.Charter, Fixture: form.Fixture, FleetID: form.FleetID, SourceID: form.SourceID})
		}
		agent, created, err := svc.RegisterFleetAgentAs(c.Request().Context(), subject, app.NewRegisterFleetAgentInput([]byte(form.Fixture), form.FleetID, form.SourceID))
		if err != nil {
			return renderAgentOperation(c, http.StatusConflict, subject, &consoleweb.AgentOperationModel{Stage: "error", Status: "Agent registration denied.", ReasonCode: agentOperationReason(err), Charter: form.Charter, Fixture: form.Fixture, FleetID: form.FleetID, SourceID: form.SourceID})
		}
		operation.Stage, operation.Status = "success", "Registered Agent with authoritative exact revision readback."
		if !created {
			operation.Status = "Exact Agent registration already existed; authoritative readback matched."
		}
		operation.Revision, operation.RevisionDigest = strconv.FormatUint(agent.Revision.Revision, 10), agent.Revision.Digest
		operation.ResultURL = "/console/agents?record_key=" + url.QueryEscape(agent.Revision.AgentID) + "#/agents"
		return renderAgentOperation(c, http.StatusOK, subject, operation)
	})
	e.POST("/console/agents/:agent/lifecycle", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		form, err := decodeAgentLifecycleForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Agent lifecycle form")
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		revision, err := strconv.ParseUint(form.Revision, 10, 64)
		if err != nil || revision == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Agent lifecycle revision")
		}
		agentID := c.Param("agent")
		input, err := app.NewSetAgentLifecycleInput(agentID, revision, form.Digest, form.Lifecycle)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Agent lifecycle")
		}
		_, err = svc.SetAgentLifecycleAs(c.Request().Context(), subject, agentID, input)
		if err != nil {
			return err
		}
		return c.Redirect(http.StatusSeeOther, "/console/agents?record_key="+url.QueryEscape(agentID)+"#/agents")
	})
	e.GET("/console/loops/compose", func(c *echo.Context) error {
		if err := consoleHeaders(c, false); err != nil {
			return consoleError(err)
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			return consoleError(err)
		}
		page, err := loadConsole(c, subject, consoleLoops)
		if err != nil {
			return err
		}
		surface, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		readiness, ok := surface.Actions["loop_publish"]
		if !ok || readiness.State != "ready" {
			return app.ErrDenied
		}
		composer := &consoleweb.LoopComposerModel{Publishers: []consoleweb.LoopPublisherModel{}}
		for _, agent := range surface.Agents {
			if agent.Revision.Lifecycle != "enabled" {
				continue
			}
			composer.Publishers = append(composer.Publishers, consoleweb.LoopPublisherModel{ID: agent.Revision.AgentID, Revision: fmt.Sprintf("r%d", agent.Revision.Revision), Digest: agent.Revision.Digest, Runtime: agent.Revision.Runtime.Runtime})
		}
		page.LoopComposer = composer
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(page))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	})
	e.GET("/console/graphs/compose", func(c *echo.Context) error {
		if err := consoleHeaders(c, true); err != nil {
			return consoleError(err)
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			consoleManager.ClearCookie(c.Response())
			return consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return err
		}
		surface, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleError(err)
		}
		agents, loops := consoleweb.GraphReferenceOptions(surface)
		content, err := renderConsole(c.Request().Context(), consoleweb.GraphComposerDocument(consoleweb.GraphComposerModel{CSRF: csrf, Agents: agents, Loops: loops}))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	})
	e.GET("/console/graphs/run", func(c *echo.Context) error {
		if err := consoleHeaders(c, true); err != nil {
			return consoleError(err)
		}
		subject, err := consoleManager.Authenticate(c.Request())
		if err != nil {
			consoleManager.ClearCookie(c.Response())
			return consoleError(err)
		}
		revisionNumber, err := requiredRevision(c.QueryParam("revision"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "exact Graph revision is required")
		}
		revision, err := svc.GetGraphAs(c.Request().Context(), subject, c.QueryParam("graph"), revisionNumber)
		if err != nil {
			return err
		}
		if revision.Digest != c.QueryParam("digest") {
			return echo.NewHTTPError(http.StatusConflict, "exact Graph digest changed or is unavailable")
		}
		lifecycle, err := svc.GetGraphLifecycleAs(c.Request().Context(), subject, revision.GraphID)
		if err != nil {
			return err
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleError(err)
		}
		inputs := make([]consoleweb.GraphRunInputModel, 0, len(revision.Inputs))
		for _, input := range revision.Inputs {
			inputs = append(inputs, consoleweb.GraphRunInputModel{ID: input.ID, Type: string(input.Type), Required: input.Required})
		}
		content, err := renderConsole(c.Request().Context(), consoleweb.GraphRunDocument(consoleweb.GraphRunFormModel{CSRF: csrf, GraphID: revision.GraphID, Revision: revision.Revision, Digest: revision.Digest, Lifecycle: string(lifecycle.State), Inputs: inputs}))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	})
	e.POST("/console/graphs/publish", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		values, err := consoleweb.DecodeGraphConsoleForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		csrf, err := consoleweb.ExactFormValue(values, "csrf")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		c.Request().Header.Set("X-CSRF-Token", csrf)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		sessionID, err := consoleweb.ExactFormValue(values, "authority_session_id")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		authority, err := svc.FleetAuthorityForSessionAs(c.Request().Context(), subject, sessionID)
		if err != nil {
			return err
		}
		surface, err := svc.FleetSurfaceAs(c.Request().Context(), subject)
		if err != nil {
			return err
		}
		input, err := consoleweb.ParseGraphPublication(values, surface, authority)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		published, err := svc.PublishGraphAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		result := consoleweb.GraphActionResultModel{Title: "Graph publication recorded", Outcome: "published", Reason: "Exact validated revision stored; idempotent=" + strconv.FormatBool(published.Decision.Idempotent), GraphID: published.Revision.GraphID, RecordKey: published.Revision.GraphID + ":" + strconv.FormatUint(published.Revision.Revision, 10)}
		content, err := renderConsole(c.Request().Context(), consoleweb.GraphActionResultDocument(result))
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, "text/html; charset=utf-8", content)
	})
	e.POST("/console/graphs/submit", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		values, err := consoleweb.DecodeGraphConsoleForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		csrf, err := consoleweb.ExactFormValue(values, "csrf")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		c.Request().Header.Set("X-CSRF-Token", csrf)
		subject, err := consoleManager.AuthorizeMutation(c.Request())
		if err != nil {
			return consoleError(err)
		}
		revisionNumber, err := strconv.ParseUint(consoleweb.OptionalFormValue(values, "graph_revision"), 10, 64)
		if err != nil || revisionNumber == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "exact Graph revision is required")
		}
		revision, err := svc.GetGraphAs(c.Request().Context(), subject, consoleweb.OptionalFormValue(values, "graph_id"), revisionNumber)
		if err != nil {
			return err
		}
		sessionID, err := consoleweb.ExactFormValue(values, "authority_session_id")
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		authority, authorityErr := svc.FleetAuthorityForSessionAs(c.Request().Context(), subject, sessionID)
		if authorityErr != nil {
			// Preserve the stable submission envelope and let the application
			// boundary record one durable readiness_denied rejection. An invalid
			// browser-supplied session never becomes an authority reference.
			authority = app.SubmitGraphInput{}.Authority
		}
		input, err := consoleweb.ParseGraphSubmission(values, revision, authority)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		decision, err := svc.SubmitGraphAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		result := consoleweb.GraphActionResultModel{Title: "Graph submission decided", GraphID: revision.GraphID, RecordKey: revision.GraphID + ":" + strconv.FormatUint(revision.Revision, 10)}
		if decision.Accepted != nil {
			result.Outcome, result.Reason, result.QueueItemID = "accepted", "One immutable snapshot and Queue item bind the reviewed exact definitions and normalized inputs.", decision.Accepted.QueueItem.ItemID
		} else if decision.Rejection != nil {
			result.Outcome, result.Reason = "rejected · "+decision.Rejection.ReasonCode, decision.Rejection.Reason
		} else {
			return errors.New("submission decision is missing both acceptance and rejection")
		}
		content, err := renderConsole(c.Request().Context(), consoleweb.GraphActionResultDocument(result))
		if err != nil {
			return err
		}
		status := http.StatusCreated
		if !decision.Created {
			status = http.StatusOK
		}
		return c.Blob(status, "text/html; charset=utf-8", content)
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
	e.POST("/console/login", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		if err := consoleManager.ValidateOrigin(c.Request(), true); err != nil {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), core.Subject{}, "denied", "principal_password_authentication_denied"); auditErr != nil {
				return auditErr
			}
			return consoleError(err)
		}
		password, err := decodeConsoleForm(c.Request(), "password")
		if err != nil || len(password) > 1024 {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), core.Subject{}, "denied", "principal_password_authentication_denied"); auditErr != nil {
				return auditErr
			}
			return renderAuthentication(c, http.StatusUnauthorized, "invalid_credentials")
		}
		passwordBytes := []byte(password)
		defer func() {
			for index := range passwordBytes {
				passwordBytes[index] = 0
			}
		}()
		sessionValue, _, expires, subject, err := consoleManager.Login(c.Request(), sourceKey(c.Request()), passwordBytes)
		if err != nil {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), core.Subject{}, "denied", "principal_password_authentication_denied"); auditErr != nil {
				return auditErr
			}
			return renderAuthentication(c, http.StatusUnauthorized, "invalid_credentials")
		}
		if err = svc.AuditConsoleSession(c.Request().Context(), subject, "success", "principal_password_authenticated"); err != nil {
			consoleManager.RevokeSessionValue(sessionValue)
			return err
		}
		consoleManager.SetCookieUntil(c.Response(), sessionValue, expires)
		return c.Redirect(http.StatusSeeOther, "/console/agents#/agents")
	})
	e.POST("/console/password", func(c *echo.Context) error {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		if err := consoleManager.ValidateOrigin(c.Request(), true); err != nil {
			return consoleError(err)
		}
		form, err := decodePasswordRotationForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid principal password rotation")
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		currentPassword := []byte(form.Current)
		newPassword := []byte(form.New)
		confirmation := []byte(form.Confirmation)
		defer func() {
			for _, secret := range [][]byte{currentPassword, newPassword, confirmation} {
				for index := range secret {
					secret[index] = 0
				}
			}
		}()
		err = consoleManager.RotatePassword(c.Request(), sourceKey(c.Request()), currentPassword, newPassword, confirmation, form.Approved, func(current, replacement principalauth.Record, subject core.Subject) error {
			return replacePrincipalVerifier(verifierPath, current, replacement,
				func() error {
					return svc.AuditConsoleSession(c.Request().Context(), subject, "authorized", "principal_password_rotation_authorized")
				},
				func() error {
					return svc.AuditConsoleSession(c.Request().Context(), subject, "success", "principal_password_rotated")
				},
			)
		})
		if err != nil {
			if auditErr := svc.AuditConsoleSession(c.Request().Context(), core.Subject{}, "denied", "principal_password_rotation_denied"); auditErr != nil {
				return errors.Join(err, auditErr)
			}
			return renderAuthentication(c, http.StatusUnauthorized, "password_rotation_denied")
		}
		consoleManager.ClearCookie(c.Response())
		return c.Redirect(http.StatusSeeOther, "/console")
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
	e.POST("/console/queue/:item/operate", consoleQueueOperationHandler(svc, consoleManager, appConsoleQueueOperator{service: svc}, randomConsoleID))
	decodeCommand := func(c *echo.Context, destination any) error {
		if c.Request().Body == nil {
			return console.ErrInvalidInput
		}
		body, err := io.ReadAll(io.LimitReader(c.Request().Body, console.CommandBodyBytesMax+1))
		if err != nil || len(body) > console.CommandBodyBytesMax {
			return console.ErrInvalidInput
		}
		if isConsoleForm(c.Request()) {
			return console.DecodeCommandForm(body, destination)
		}
		mediaType, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			return console.ErrInvalidInput
		}
		return console.DecodeCommandRequest(body, destination)
	}
	commandAdmission := func(c *echo.Context) (core.Subject, string, error) {
		consoleManager.ApplySecurityHeaders(c.Response().Header(), true)
		subject, sessionID, err := consoleManager.AuthorizeCommand(c.Request())
		if err != nil {
			return core.Subject{}, "", consoleError(err)
		}
		if err = svc.RequirePrincipal(subject); err != nil {
			return core.Subject{}, "", err
		}
		return subject, sessionID, nil
	}
	renderLoopCommandPage := func(c *echo.Context, page consoleweb.PageModel, status int) error {
		content, err := renderConsole(c.Request().Context(), consoleweb.Document(page))
		if err != nil {
			return err
		}
		return c.Blob(status, "text/html; charset=utf-8", content)
	}
	e.POST("/console/loops/preview", func(c *echo.Context) error {
		form, err := decodeLoopComposerForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, sessionID, err := commandAdmission(c)
		if err != nil {
			return err
		}
		head := emptyLoopHeadDigest(form.Revision.LoopID)
		revisions, err := svc.FleetRepository.ListLoopRevisions(c.Request().Context())
		if err != nil {
			return err
		}
		var latest uint64
		for _, revision := range revisions {
			if revision.LoopID == form.Revision.LoopID && revision.Revision > latest {
				latest, head = revision.Revision, revision.Digest
			}
		}
		input, err := json.Marshal(loopPublishCommandInput{PublisherID: form.PublisherID, Revision: form.Revision, ExpectedPreviousDigest: form.Revision.PreviousDigest, PublicationKey: form.PublicationKey})
		if err != nil {
			return err
		}
		preview, err := commandService.Preview(c.Request().Context(), subject, sessionID, console.CommandPreviewRequest{SchemaVersion: console.CommandCatalogVersion, CommandID: loopPublishCommandID, TargetID: form.Revision.LoopID, ExpectedDigest: head, IdempotencyKey: form.PublicationKey, Input: input})
		if err != nil {
			return consoleError(err)
		}
		page := consoleweb.PageModel{Authenticated: true, CSRF: form.CSRF, Surface: consoleweb.SurfaceModel{Domain: string(consoleLoops), Title: "Loops"}, CommandPreview: &consoleweb.CommandPreviewModel{IntentID: preview.IntentID, CommandID: preview.CommandID, TargetID: preview.Target.ID, TargetDigest: preview.Target.Digest, InputDigest: preview.InputDigest, ExpiresAt: preview.ExpiresAt.UTC().Format(time.RFC3339)}}
		return renderLoopCommandPage(c, page, http.StatusOK)
	})
	e.POST("/console/loops/lifecycle-preview", func(c *echo.Context) error {
		form, err := decodeLoopLifecycleForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Loop lifecycle request")
		}
		c.Request().Header.Set("X-CSRF-Token", form.CSRF)
		subject, sessionID, err := commandAdmission(c)
		if err != nil {
			return err
		}
		input, err := json.Marshal(loopLifecycleCommandInput{PublisherID: form.PublisherID, State: form.State, ExpectedPreviousDigest: form.ExpectedPreviousDigest, IdempotencyKey: form.IdempotencyKey})
		if err != nil {
			return err
		}
		preview, err := commandService.Preview(c.Request().Context(), subject, sessionID, console.CommandPreviewRequest{SchemaVersion: console.CommandCatalogVersion, CommandID: loopLifecycleCommandID, TargetID: form.TargetID, ExpectedDigest: form.ExpectedDigest, IdempotencyKey: form.IdempotencyKey, Input: input})
		if err != nil {
			return consoleError(err)
		}
		page := consoleweb.PageModel{Authenticated: true, CSRF: form.CSRF, Surface: consoleweb.SurfaceModel{Domain: string(consoleLoops), Title: "Loops"}, CommandPreview: &consoleweb.CommandPreviewModel{IntentID: preview.IntentID, CommandID: preview.CommandID, TargetID: preview.Target.ID, TargetDigest: preview.Target.Digest, InputDigest: preview.InputDigest, ExpiresAt: preview.ExpiresAt.UTC().Format(time.RFC3339)}}
		return renderLoopCommandPage(c, page, http.StatusOK)
	})
	e.POST("/console/loops/execute", func(c *echo.Context) error {
		csrf, intentID, err := decodeLoopExecuteForm(c.Request())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid Loop confirmation")
		}
		c.Request().Header.Set("X-CSRF-Token", csrf)
		subject, sessionID, err := commandAdmission(c)
		if err != nil {
			return err
		}
		receipt, err := commandService.Execute(c.Request().Context(), subject, sessionID, console.CommandExecuteRequest{SchemaVersion: console.CommandCatalogVersion, IntentID: intentID})
		if err != nil {
			return consoleError(err)
		}
		page := consoleweb.PageModel{Authenticated: true, CSRF: csrf, Surface: consoleweb.SurfaceModel{Domain: string(consoleLoops), Title: "Loops"}, CommandReceipt: &consoleweb.OperationReceiptModel{Title: receipt.CommandID, Outcome: receipt.Outcome, OperationID: receipt.IntentID, RecordedAt: receipt.CommittedAt.UTC().Format(time.RFC3339), ReasonCode: receipt.ReasonCode, Message: "Exact authoritative readback: " + string(receipt.Readback)}}
		return renderLoopCommandPage(c, page, http.StatusOK)
	})
	e.POST("/console/api/commands/preview", func(c *echo.Context) error {
		subject, sessionID, err := commandAdmission(c)
		if err != nil {
			return err
		}
		var request console.CommandPreviewRequest
		if err = decodeCommand(c, &request); err != nil {
			return consoleError(err)
		}
		preview, err := commandService.Preview(c.Request().Context(), subject, sessionID, request)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(http.StatusOK, preview)
	})
	e.POST("/console/api/commands/execute", func(c *echo.Context) error {
		subject, sessionID, err := commandAdmission(c)
		if err != nil {
			return err
		}
		var request console.CommandExecuteRequest
		if err = decodeCommand(c, &request); err != nil {
			return consoleError(err)
		}
		receipt, err := commandService.Execute(c.Request().Context(), subject, sessionID, request)
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(http.StatusOK, receipt)
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
		aggregateState, aggregateStatus := fleetSurfaceAggregateState(surface.Readiness)
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
		if len(surface.Credentials) > limit {
			surface.Credentials = surface.Credentials[:limit]
		}
		csrf, err := consoleManager.CSRF(c.Request())
		if err != nil {
			return consoleError(err)
		}
		return c.JSON(aggregateStatus, map[string]any{"state": aggregateState, "surface": surface, "csrf": csrf, "limit": limit})
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
	g.POST("/manager/sessions/:session/turns", func(c *echo.Context) error {
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
		message, err := managerGateway.Turn(c.Request().Context(), subject, c.Param("session"), c.Request().Header.Get(managergateway.SessionHeader), input.Input)
		if err != nil {
			if errors.Is(err, app.ErrUnauthenticated) || errors.Is(err, app.ErrDenied) || errors.Is(err, app.ErrExpired) {
				return err
			}
			return echo.NewHTTPError(http.StatusServiceUnavailable, err.Error())
		}
		return c.JSON(http.StatusOK, map[string]string{"message": message})
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
	g.GET("/agents/:agent/revisions", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		values, err := svc.ListFleetAgentRevisionsAs(c.Request().Context(), subject, c.Param("agent"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, values)
	})
	g.PUT("/agents/:agent/lifecycle", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SetAgentLifecycleInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, err := svc.SetAgentLifecycleAs(c.Request().Context(), subject, c.Param("agent"), input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, value)
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
		return c.JSON(createdOrOK(value.Decision.Idempotent), value)
	})
	g.PUT("/loops/:loop/lifecycle", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.SetLoopLifecycleInput
		if err = decode(c, &input); err != nil {
			return err
		}
		value, err := svc.SetLoopLifecycleAs(c.Request().Context(), subject, c.Param("loop"), input)
		if err != nil {
			return err
		}
		return c.JSON(createdOrOK(value.Idempotent), value)
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
		value, err := svc.GetLoopViewAs(c.Request().Context(), subject, c.Param("loop"), revision)
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
	g.GET("/graphs/:graph/lifecycle", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.GetGraphLifecycleAs(c.Request().Context(), subject, c.Param("graph"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.GET("/submissions", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		value, err := svc.ListSubmissionHistoryAs(c.Request().Context(), subject)
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
		state, status := fleetSurfaceAggregateState(value.Readiness)
		return c.JSON(status, map[string]any{"state": state, "collections": value.Readiness, "actions": value.Actions})
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
	g.POST("/queue/:item/retry", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.RetryQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.RetryQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.POST("/queue/:item/cancel", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.TerminalQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.CancelQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.POST("/queue/:item/expire", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.TerminalQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.ExpireQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.POST("/queue/:item/exhaust", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.TerminalQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.ExhaustQueueItemAs(c.Request().Context(), subject, input)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, value)
	})
	g.POST("/queue/:item/revoke", func(c *echo.Context) error {
		subject, err := requestSubject(c)
		if err != nil {
			return err
		}
		var input app.TerminalQueueItemInput
		if err = decode(c, &input); err != nil {
			return err
		}
		if input.QueueItemID != c.Param("item") {
			return echo.NewHTTPError(http.StatusBadRequest, "queue item path and body must match")
		}
		value, err := svc.RevokeQueueItemAs(c.Request().Context(), subject, input)
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
	srv := &http.Server{Addr: svc.Config.API.Listen, Handler: e, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: svc.Config.API.ReadTimeout, WriteTimeout: managerGatewayWriteTimeout(svc.Config), IdleTimeout: 60 * time.Second, MaxHeaderBytes: 256 << 10}
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

func createdOrOK(idempotent bool) int {
	if idempotent {
		return http.StatusOK
	}
	return http.StatusCreated
}

func requiredRevision(raw string) (uint64, error) {
	revision, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || revision == 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, "revision must be a positive integer")
	}
	return revision, nil
}

func fleetSurfaceAggregateState(readiness map[string]app.SurfaceReadiness) (string, int) {
	for _, key := range []string{"registry", "loops", "graphs", "queue"} {
		value, ok := readiness[key]
		if !ok || !value.Authoritative || (value.State != "ready" && value.State != "empty") {
			return "unavailable", http.StatusServiceUnavailable
		}
	}
	// Credentials are intentionally non-gating for the credential-independent
	// MVI vertical. An unconfigured, locked, corrupt, or unavailable authority
	// must not fail fleet-level state aggregation.
	if cred, ok := readiness["credentials"]; ok && !cred.Authoritative && cred.State == "error" {
		return "unavailable", http.StatusServiceUnavailable
	}
	return "ready", http.StatusOK
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
