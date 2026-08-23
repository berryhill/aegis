package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	consoleauth "github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/orchestration"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
	"github.com/labstack/echo/v5"
)

type telemetryRecorder struct {
	mu           sync.Mutex
	observations []HTTPObservation
}

func (r *telemetryRecorder) ObserveHTTP(_ context.Context, observation HTTPObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, observation)
}

type blockingTelemetry struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (b *blockingTelemetry) ObserveHTTP(context.Context, HTTPObservation) {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
}

type recordingConsoleQueueOperator struct {
	view         app.QueueExecutionView
	getErr       error
	operationErr error
	gotItem      string
	gotSubject   core.Subject
	operation    string
	process      app.ProcessQueueItemInput
	retry        app.RetryQueueItemInput
	terminal     app.TerminalQueueItemInput
}

func (o *recordingConsoleQueueOperator) Get(_ context.Context, subject core.Subject, itemID string) (app.QueueExecutionView, error) {
	o.gotSubject, o.gotItem = subject, itemID
	return o.view, o.getErr
}
func (o *recordingConsoleQueueOperator) Process(_ context.Context, _ core.Subject, input app.ProcessQueueItemInput) error {
	o.operation, o.process = "process", input
	return o.operationErr
}
func (o *recordingConsoleQueueOperator) Reclaim(_ context.Context, _ core.Subject, input app.RetryQueueItemInput) error {
	o.operation, o.retry = "reclaim", input
	return o.operationErr
}
func (o *recordingConsoleQueueOperator) Cancel(_ context.Context, _ core.Subject, input app.TerminalQueueItemInput) error {
	o.operation, o.terminal = "cancel", input
	return o.operationErr
}
func (o *recordingConsoleQueueOperator) Expire(_ context.Context, _ core.Subject, input app.TerminalQueueItemInput) error {
	o.operation, o.terminal = "expire", input
	return o.operationErr
}
func (o *recordingConsoleQueueOperator) Exhaust(_ context.Context, _ core.Subject, input app.TerminalQueueItemInput) error {
	o.operation, o.terminal = "exhaust", input
	return o.operationErr
}
func (o *recordingConsoleQueueOperator) Revoke(_ context.Context, _ core.Subject, input app.TerminalQueueItemInput) error {
	o.operation, o.terminal = "revoke", input
	return o.operationErr
}

func consoleQueueRouteFixture(t *testing.T, svc *app.Service) (*consoleauth.Manager, *http.Cookie, string) {
	t.Helper()
	now := svc.Now()
	manager, err := consoleauth.New(consoleauth.Config{Origin: "http://127.0.0.1", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "local-uid:test", PrincipalID: svc.Config.Principal.ID, AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)}
	bootstrap, err := manager.IssueBootstrap(subject)
	if err != nil {
		t.Fatal(err)
	}
	exchange := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/session", nil)
	exchange.Header.Set("Origin", "http://127.0.0.1")
	session, csrf, _, err := manager.Exchange(exchange, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	manager.SetCookie(recorder, session)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("console session cookies=%d", len(cookies))
	}
	return manager, cookies[0], csrf
}

func serveConsoleQueueOperation(t *testing.T, svc *app.Service, manager *consoleauth.Manager, cookie *http.Cookie, csrf, operation string, operator consoleQueueOperator) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, code, message := classifyError(err)
		_ = c.JSON(status, envelope{Code: code, Message: message, RequestID: "test-request"})
	}
	e.POST("/console/queue/:item/operate", consoleQueueOperationHandler(svc, manager, operator, func(prefix string) (string, error) {
		return prefix + "-server", nil
	}))
	form := url.Values{"csrf": {csrf}, "operation": {operation}}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/queue-authoritative/operate", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://127.0.0.1")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, request)
	return recorder
}

func TestConsoleQueueOperationRouteWiresAllClosedOperationsWithServerBindings(t *testing.T) {
	svc := apiService(t)
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	view := app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}, LoopExecutions: []execution.LoopExecution{{LoopExecutionID: "loop-existing"}}}

	for _, operation := range []string{"process", "reclaim", "cancel", "expire", "exhaust", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			operator := &recordingConsoleQueueOperator{view: view}
			response := serveConsoleQueueOperation(t, svc, manager, cookie, csrf, operation, operator)
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/console/queue?record_key=queue-authoritative#/queue" {
				t.Fatalf("operation=%s status=%d location=%q body=%s", operation, response.Code, response.Header().Get("Location"), response.Body.String())
			}
			if operator.gotItem != "queue-authoritative" || operator.gotSubject.PrincipalID != svc.Config.Principal.ID || operator.operation != operation {
				t.Fatalf("operation=%s did not preserve authenticated authoritative binding: %+v", operation, operator)
			}
			switch operation {
			case "process":
				if operator.process.QueueItemID != "queue-authoritative" || operator.process.LoopExecutionID != "loop-existing" || operator.process.WorkerID != "console-worker-server" || operator.process.ClaimID != "claim-server" || operator.process.AttemptID != "attempt-server" || operator.process.ClaimTransitionID != "transition-claim-server" || operator.process.TerminalTransitionID != "transition-terminal-server" || operator.process.DispositionID != "disposition-server" || operator.process.ArtifactID != "artifact-server" || operator.process.LeaseDuration != 5*time.Minute {
					t.Fatalf("process input was not generated and bound server-side: %+v", operator.process)
				}
			case "reclaim":
				if operator.retry.QueueItemID != "queue-authoritative" || operator.retry.RetryID != "retry-server" || operator.retry.TransitionID != "transition-reclaim-server" || !operator.retry.Reclaimed || operator.retry.ReasonCode != app.QueueReasonLeaseReclaimed {
					t.Fatalf("reclaim input was not generated and bound server-side: %+v", operator.retry)
				}
			default:
				if operator.terminal.QueueItemID != "queue-authoritative" || operator.terminal.CancellationID != operation+"-server" || operator.terminal.TransitionID != "transition-"+operation+"-server" {
					t.Fatalf("terminal input was not generated and bound server-side: %+v", operator.terminal)
				}
			}
		})
	}
	generatedLoop := &recordingConsoleQueueOperator{view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}}
	response := serveConsoleQueueOperation(t, svc, manager, cookie, csrf, "process", generatedLoop)
	if response.Code != http.StatusSeeOther || generatedLoop.process.LoopExecutionID != "loop-execution-server" {
		t.Fatalf("missing Loop execution binding was not generated server-side: status=%d input=%+v", response.Code, generatedLoop.process)
	}
}

func TestConsoleQueueOperationRouteRequiresSessionAndCSRF(t *testing.T) {
	svc := apiService(t)
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	view := app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}

	unauthenticated := serveConsoleQueueOperation(t, svc, manager, nil, csrf, "cancel", &recordingConsoleQueueOperator{view: view})
	if unauthenticated.Code != http.StatusUnauthorized || !strings.Contains(unauthenticated.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("missing session status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}
	wrongCSRF := serveConsoleQueueOperation(t, svc, manager, cookie, "wrong-csrf", "cancel", &recordingConsoleQueueOperator{view: view})
	if wrongCSRF.Code != http.StatusForbidden || !strings.Contains(wrongCSRF.Body.String(), `"code":"denied"`) {
		t.Fatalf("wrong CSRF status=%d body=%s", wrongCSRF.Code, wrongCSRF.Body.String())
	}

	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, code, message := classifyError(err)
		_ = c.JSON(status, envelope{Code: code, Message: message, RequestID: "test-request"})
	}
	e.POST("/console/queue/:item/operate", consoleQueueOperationHandler(svc, manager, &recordingConsoleQueueOperator{view: view}, func(prefix string) (string, error) {
		return prefix + "-server", nil
	}))
	malformedRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/queue-authoritative/operate", strings.NewReader("csrf="+url.QueryEscape(csrf)+"&operation=cancel&authority=forged"))
	malformedRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	malformedRequest.Header.Set("Origin", "http://127.0.0.1")
	malformedRequest.AddCookie(cookie)
	malformed := httptest.NewRecorder()
	e.ServeHTTP(malformed, malformedRequest)
	if malformed.Code != http.StatusBadRequest || !strings.Contains(malformed.Body.String(), `"code":"queue_operation_malformed"`) {
		t.Fatalf("malformed operation form status=%d body=%s", malformed.Code, malformed.Body.String())
	}
}

func TestConsoleQueueOperationDenialsAreDistinctAndFailClosed(t *testing.T) {
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: time.Now().Add(time.Minute)}
	serverID := func(prefix string) (string, error) { return prefix + "-server", nil }
	tests := []struct {
		name, operation, code string
		view                  app.QueueExecutionView
		operationErr          error
	}{
		{name: "malformed", operation: "forged", code: "queue_operation_malformed"},
		{name: "authoritative item mismatch", operation: "cancel", code: "queue_operation_ambiguous", view: app.QueueExecutionView{Item: queue.Item{ItemID: "different-item"}}},
		{name: "ambiguous binding", operation: "process", code: "queue_operation_ambiguous", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}, LoopExecutions: []execution.LoopExecution{{LoopExecutionID: "one"}, {LoopExecutionID: "two"}}}},
		{name: "live retry", operation: "retry", code: "queue_operation_live_retry_denied", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}},
		{name: "unauthorized", operation: "cancel", code: "queue_operation_denied", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}, operationErr: app.ErrDenied},
		{name: "ambiguous authority", operation: "cancel", code: "queue_operation_ambiguous", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}, operationErr: app.ErrAmbiguous},
		{name: "invalid transition", operation: "cancel", code: "queue_operation_invalid_transition", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}, operationErr: app.ErrConflict},
		{name: "stale state", operation: "cancel", code: "queue_operation_stale_state", view: app.QueueExecutionView{Item: queue.Item{ItemID: "queue-authoritative"}}, operationErr: errors.New("projection changed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operator := &recordingConsoleQueueOperator{view: test.view, operationErr: test.operationErr}
			err := operateConsoleQueueItem(context.Background(), operator, subject, "queue-authoritative", test.operation, serverID)
			status, code, _ := classifyError(err)
			if err == nil || code != test.code || status < 400 || status >= 500 {
				t.Fatalf("denial err=%v status=%d code=%q want=%q", err, status, code, test.code)
			}
		})
	}
}

func apiService(t *testing.T) *app.Service {
	t.Helper()
	root := t.TempDir()
	executable := filepath.Join(root, "hermes-test")
	script := "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: /isolated/api-test'; exit 0; fi\n[ \"${TEST_PROVIDER_KEY:-}\" = \"api-test-secret\" ] || exit 41\nsleep 60 &\nwait\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.StateDir = state.Root()
	cfg.Audit.CheckpointDir = state.CheckpointRoot()
	cfg.HermesExecutable = executable
	cfg.Principal = config.Principal{ID: "principal-1", Name: "Principal Operator", UID: strconv.Itoa(os.Getuid()), User: current.Username, AuthTTL: time.Minute}
	cfg.API.Token = "transport-secret"
	cfg.API.Listen = "127.0.0.1:0"
	cfg.API.UnixSocket = filepath.Join(root, "aegis.sock")
	cfg.Credentials.ProviderAuth["test"] = config.EnvironmentCredentialBinding{Type: "environment", SourceEnv: "AEGIS_API_TEST_KEY", TargetEnv: "TEST_PROVIDER_KEY"}
	verifier, err := principalauth.Enroll(cfg.Principal.ID, []byte("api-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	authorityPath := filepath.Join(state.Root(), "persistence", "authority-v1")
	if _, err = authoritybadger.Initialize(context.Background(), authorityPath); err != nil {
		t.Fatal(err)
	}
	authority, err := authoritybadger.Open(context.Background(), authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = authority.Close() })
	svc := app.New(cfg, state, authority, authority, hermes.New(executable, logger), logger)
	svc.LookupEnv = func(name string) (string, bool) {
		if name == "AEGIS_API_TEST_KEY" {
			return "api-test-secret", true
		}
		return "", false
	}
	return svc
}

func configureAPIFleet(t *testing.T, svc *app.Service) *fleetbadger.Store {
	t.Helper()
	fleetStore, err := fleetbadger.Open(context.Background(), filepath.Join(svc.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fleetStore.Close(); err != nil {
			t.Error(err)
		}
	})
	fleetService, err := orchestration.NewFleetService(fleetStore, svc.Authority, svc.AuthorityCommands, func(_ context.Context, _ orchestration.FleetAction, subject core.Subject) error {
		return svc.RequirePrincipal(subject)
	}, nil, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(svc.Store)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := orchestration.NewQueueWorker(fleetStore, fleetService, svc.Store, verifier, orchestration.NoKeyAdapter{}, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureFleet(fleetStore, fleetService, worker); err != nil {
		t.Fatal(err)
	}
	return fleetStore
}

func waitFor(t *testing.T, network, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout(network, address, 20*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not listen on %s %s", network, address)
}

func unixClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, Timeout: 5 * time.Second}
}

func TestUnixPeerAuthenticationAndBearerSeparation(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	telemetry := &telemetryRecorder{}
	go func() { done <- ServeWithTelemetry(ctx, svc, telemetry) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	for name, authorization := range map[string]string{
		"missing": "",
		"wrong":   "Bearer wrong-transport-token",
	} {
		t.Run("readyz rejects "+name+" bearer", func(t *testing.T) {
			request, _ := http.NewRequest(http.MethodGet, "http://unix/readyz", nil)
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("readyz %s bearer status = %d", name, response.StatusCode)
			}
		})
	}
	request, _ := http.NewRequest(http.MethodGet, "http://unix/readyz", nil)
	request.Header.Set("Authorization", "Bearer transport-secret")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated audit-current readyz status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
	if status := svc.AuditDeliveryReadiness(); !status.Current || status.Pending != 0 {
		t.Fatalf("readiness probe made audit projection stale: %+v", status)
	}

	request, _ = http.NewRequest(http.MethodGet, "http://unix/v1/runtime", nil)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
	telemetry.mu.Lock()
	var runtimeUnauthorized bool
	for _, observation := range telemetry.observations {
		if observation.Route == "/v1/runtime" && observation.Status == http.StatusUnauthorized {
			runtimeUnauthorized = true
			break
		}
	}
	if !runtimeUnauthorized {
		t.Fatalf("authentication middleware telemetry=%+v", telemetry.observations)
	}
	telemetry.mu.Unlock()

	request, _ = http.NewRequest(http.MethodGet, "http://unix/v1/runtime", nil)
	request.Header.Set("Authorization", "Bearer transport-secret")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Unix peer plus bearer status = %d", response.StatusCode)
	}
	_ = response.Body.Close()

	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(svc.Config.API.UnixSocket); !os.IsNotExist(err) {
		t.Fatalf("Unix socket was not removed: %v", err)
	}
}

func TestBearerAloneCannotCreatePrincipalIdentity(t *testing.T) {
	svc := apiService(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.UnixSocket = ""
	svc.Config.API.Listen = address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "tcp", address)

	request, _ := http.NewRequest(http.MethodGet, "http://"+address+"/v1/runtime", nil)
	request.Header.Set("Authorization", "Bearer transport-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bearer-only TCP status = %d, want 401", response.StatusCode)
	}
	_ = response.Body.Close()
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeSingletonDeniesBeforeActiveSocketMutation(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	before, err := os.Lstat(svc.Config.API.UnixSocket)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	secondErr := Serve(context.Background(), svc)
	if secondErr == nil || !strings.Contains(secondErr.Error(), "another Aegis control-plane daemon owns this transport") {
		cancel()
		t.Fatalf("concurrent serve error=%v", secondErr)
	}
	after, err := os.Lstat(svc.Config.API.UnixSocket)
	if err != nil || !os.SameFile(before, after) {
		cancel()
		t.Fatalf("singleton denial mutated active socket: before=%v after=%v err=%v", before, after, err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://unix/livez", nil)
	response, err := unixClient(svc.Config.API.UnixSocket).Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("first daemon was disturbed: response=%v err=%v", response, err)
	}
	_ = response.Body.Close()
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSessionErrorsPreserveNativeRecoveryAndJSONCodes(t *testing.T) {
	svc := apiService(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if serveErr := <-done; serveErr != nil {
			t.Error(serveErr)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)

	var issued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &issued, http.StatusCreated)

	for name, request := range map[string]*http.Request{
		"cross-origin malformed form": func() *http.Request {
			r, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap=malformed"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://attacker.example")
			return r
		}(),
		"missing-origin malformed JSON": func() *http.Request {
			r, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":`))
			r.Header.Set("Content-Type", "application/json")
			return r
		}(),
		"wrong-host malformed JSON": func() *http.Request {
			r, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":`))
			r.Host = "other.example.test"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", "http://"+address)
			return r
		}(),
	} {
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatalf("%s: %v", name, requestErr)
		}
		var denied envelope
		if decodeErr := json.NewDecoder(response.Body).Decode(&denied); decodeErr != nil {
			t.Fatalf("%s decode: %v", name, decodeErr)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden || denied.Code != "denied" {
			t.Fatalf("%s status=%d code=%q", name, response.StatusCode, denied.Code)
		}
	}

	native, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap=malformed"))
	native.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	native.Header.Set("Origin", "http://"+address)
	nativeResponse, err := http.DefaultClient.Do(native)
	if err != nil {
		t.Fatal(err)
	}
	nativeBody, _ := io.ReadAll(nativeResponse.Body)
	_ = nativeResponse.Body.Close()
	if nativeResponse.StatusCode != http.StatusBadRequest || !strings.HasPrefix(nativeResponse.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("native malformed exchange status=%d type=%q", nativeResponse.StatusCode, nativeResponse.Header.Get("Content-Type"))
	}
	for _, required := range []string{"bootstrap_invalid_format", "aegis console", svc.Config.API.Console.BootstrapTTL.String(), svc.Config.API.Console.SessionTTL.String()} {
		if !bytes.Contains(nativeBody, []byte(required)) {
			t.Fatalf("native recovery omitted %q", required)
		}
	}
	if bytes.Contains(nativeBody, []byte("malformed")) {
		t.Fatal("native recovery reflected submitted bootstrap")
	}

	jsonRequest, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":"malformed"}`))
	jsonRequest.Header.Set("Content-Type", "application/json")
	jsonRequest.Header.Set("Origin", "http://"+address)
	jsonResponse, err := http.DefaultClient.Do(jsonRequest)
	if err != nil {
		t.Fatal(err)
	}
	var failure envelope
	if err = json.NewDecoder(jsonResponse.Body).Decode(&failure); err != nil {
		t.Fatal(err)
	}
	_ = jsonResponse.Body.Close()
	if jsonResponse.StatusCode != http.StatusBadRequest || failure.Code != "bootstrap_invalid_format" {
		t.Fatalf("JSON malformed exchange status=%d code=%q", jsonResponse.StatusCode, failure.Code)
	}

	crossOrigin, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":"`+issued.Bootstrap+`"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "http://attacker.example")
	crossOriginResponse, err := http.DefaultClient.Do(crossOrigin)
	if err != nil {
		t.Fatal(err)
	}
	var denied envelope
	if err = json.NewDecoder(crossOriginResponse.Body).Decode(&denied); err != nil {
		t.Fatal(err)
	}
	_ = crossOriginResponse.Body.Close()
	if crossOriginResponse.StatusCode != http.StatusForbidden || denied.Code != "denied" {
		t.Fatalf("cross-origin exchange status=%d code=%q", crossOriginResponse.StatusCode, denied.Code)
	}

	valid, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":"`+issued.Bootstrap+`"}`))
	valid.Header.Set("Content-Type", "application/json")
	valid.Header.Set("Origin", "http://"+address)
	validResponse, err := http.DefaultClient.Do(valid)
	if err != nil {
		t.Fatal(err)
	}
	_ = validResponse.Body.Close()
	if validResponse.StatusCode != http.StatusCreated {
		t.Fatalf("cross-origin denial consumed bootstrap: status=%d", validResponse.StatusCode)
	}

	replay, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader(`{"bootstrap":"`+issued.Bootstrap+`"}`))
	replay.Header.Set("Content-Type", "application/json")
	replay.Header.Set("Origin", "http://"+address)
	replayResponse, err := http.DefaultClient.Do(replay)
	if err != nil {
		t.Fatal(err)
	}
	var replayFailure envelope
	if err = json.NewDecoder(replayResponse.Body).Decode(&replayFailure); err != nil {
		t.Fatal(err)
	}
	_ = replayResponse.Body.Close()
	if replayResponse.StatusCode != http.StatusUnauthorized || replayFailure.Code != "bootstrap_consumed_or_expired" {
		t.Fatalf("replay exchange status=%d code=%q", replayResponse.StatusCode, replayFailure.Code)
	}
}

func TestBearerAloneCannotIssueConsoleBootstrapOverTCP(t *testing.T) {
	svc := apiService(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.UnixSocket = ""
	svc.Config.API.Listen = address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "tcp", address)
	request, _ := http.NewRequest(http.MethodPost, "http://"+address+"/v1/console/bootstrap", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer transport-secret")
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bearer-only TCP bootstrap status=%d, want 401", response.StatusCode)
	}
	_ = response.Body.Close()
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSharedShellRendersAllFiveWorkspaceRoutesWithWiredActionReadiness(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)

	var issued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &issued, http.StatusCreated)
	if issued.Bootstrap == "" {
		t.Fatal("server issued empty browser bootstrap")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	exchange, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap="+url.QueryEscape(issued.Bootstrap)))
	exchange.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	exchange.Header.Set("Origin", "http://"+address)
	exchangeResponse, err := client.Do(exchange)
	if err != nil {
		t.Fatal(err)
	}
	_ = exchangeResponse.Body.Close()
	if exchangeResponse.StatusCode != http.StatusOK || exchangeResponse.Request.URL.Path != "/console/agents" {
		t.Fatalf("native session exchange status=%d final=%s", exchangeResponse.StatusCode, exchangeResponse.Request.URL)
	}

	routes := []struct {
		domain, title, eyebrow, hash, actionLabel, actionKey string
	}{
		{"agents", "Agent Registry", "Participants", "/agents", "Prepare charter import", "register_fleet_agent"},
		{"graphs", "Graphs", "Definitions", "/graphs", "Publish Graph revision", "graph_publish"},
		{"loops", "Loops", "Definitions", "/loops", "Publish Loop revision", "loop_publish"},
		{"queue", "Execution Queue", "Runtime", "/queue", "Prepare execution request", "submission"},
		{"credentials", "Credentials", "Encrypted credential authority", "/credentials", "", ""},
	}

	for _, route := range routes {
		t.Run(route.domain, func(t *testing.T) {
			response, err := client.Get("http://" + address + "/console/" + route.domain)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("route %s status=%d body=%s", route.domain, response.StatusCode, body)
			}
			if response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("route %s cache-control=%q", route.domain, response.Header.Get("Cache-Control"))
			}
			if !strings.Contains(response.Header.Get("Content-Security-Policy"), "default-src 'none'") {
				t.Fatalf("route %s missing strict CSP: %q", route.domain, response.Header.Get("Content-Security-Policy"))
			}
			if route.title != "" && !bytes.Contains(body, []byte(route.title)) {
				t.Fatalf("route %s missing title %s", route.domain, route.title)
			}
			if route.eyebrow != "" && !bytes.Contains(body, []byte(route.eyebrow)) {
				t.Fatalf("route %s missing eyebrow %s", route.domain, route.eyebrow)
			}
			if !bytes.Contains(body, []byte(`href="/console/agents#/agents"`)) ||
				!bytes.Contains(body, []byte(`href="/console/graphs#/graphs"`)) ||
				!bytes.Contains(body, []byte(`href="/console/loops#/loops"`)) ||
				!bytes.Contains(body, []byte(`href="/console/queue#/queue"`)) ||
				!bytes.Contains(body, []byte(`href="/console/credentials#/credentials"`)) {
				t.Fatalf("route %s missing one of the wired shared-shell routes: %s", route.domain, body)
			}
			if !bytes.Contains(body, []byte(`aria-current="page"`)) {
				t.Fatalf("route %s missing current-page aria marker", route.domain)
			}
			if route.actionKey != "" {
				if !bytes.Contains(body, []byte(route.actionLabel)) {
					t.Fatalf("route %s missing action label %s", route.domain, route.actionLabel)
				}
				if !bytes.Contains(body, []byte(`data-state="ready"`)) && !bytes.Contains(body, []byte(`data-state="denied"`)) && !bytes.Contains(body, []byte(`title="ready: ready"`)) {
					t.Fatalf("route %s missing wired contextual action state: %s", route.domain, body)
				}
				if !bytes.Contains(body, []byte("disabled")) && !bytes.Contains(body, []byte("data-state=\"ready\"")) {
					t.Fatalf("route %s missing disabled primary action when readiness is not ready: %s", route.domain, body)
				}
			}
			if route.domain == "credentials" {
				if !bytes.Contains(body, []byte("Secret values, ciphertext, and key material are never read or shown here")) {
					t.Fatalf("credentials domain must explain metadata-only boundary: %s", body)
				}
				if bytes.Contains(body, []byte("source_env")) || bytes.Contains(body, []byte("target_env")) || bytes.Contains(body, []byte("AEGIS_API_TEST_KEY")) {
					t.Fatalf("credentials domain exposed secret values: %s", body)
				}
				if !bytes.Contains(body, []byte("credentials_authority_not_configured")) {
					t.Fatalf("credentials domain must report unconfigured authority when no bbolt is wired: %s", body)
				}
			}
			if bytes.Contains(body, []byte("<script")) || bytes.Contains(body, []byte("data-on:")) {
				t.Fatalf("route %s contained CSP-incompatible browser behavior: %s", route.domain, body)
			}
		})
	}

	charterImportResponse, err := client.Get("http://" + address + "/console/agents/charter-import")
	if err != nil {
		t.Fatal(err)
	}
	charterImportBody, _ := io.ReadAll(charterImportResponse.Body)
	_ = charterImportResponse.Body.Close()
	if charterImportResponse.StatusCode != http.StatusOK {
		t.Fatalf("charter import route status=%d body=%s", charterImportResponse.StatusCode, charterImportBody)
	}
	for _, required := range [][]byte{[]byte("<title>Agent registration · Aegis Console</title>"), []byte("Charter-backed Agent registration"), []byte("This workflow does not import the charter"), []byte(`href="/console/agents#/agents"`), []byte(`action="/console/agents/registration/review"`), []byte("Validate and review"), []byte("aegis charter validate &lt;charter-file.json&gt;"), []byte("aegis charter import &lt;charter-file.json&gt;")} {
		if !bytes.Contains(charterImportBody, required) {
			t.Fatalf("charter import route missing %q: %s", required, charterImportBody)
		}
	}
	if bytes.Contains(charterImportBody, []byte(`action="/console/agents/charter-import"`)) || bytes.Contains(charterImportBody, []byte(`action="/console/agents/registration/execute"`)) || bytes.Contains(charterImportBody, []byte("data-on:")) {
		t.Fatalf("unreviewed charter import route exposed execute behavior: %s", charterImportBody)
	}

	unauthenticatedResponse, err := (&http.Client{Timeout: 5 * time.Second}).Get("http://" + address + "/console/agents/charter-import")
	if err != nil {
		t.Fatal(err)
	}
	unauthenticatedBody, _ := io.ReadAll(unauthenticatedResponse.Body)
	_ = unauthenticatedResponse.Body.Close()
	if unauthenticatedResponse.StatusCode != http.StatusOK || !bytes.Contains(unauthenticatedBody, []byte(`id="session-form"`)) {
		t.Fatalf("unauthenticated charter import route did not render authentication recovery: status=%d body=%s", unauthenticatedResponse.StatusCode, unauthenticatedBody)
	}
	if bytes.Contains(unauthenticatedBody, []byte("aegis charter validate")) || bytes.Contains(unauthenticatedBody, []byte("aegis charter import")) {
		t.Fatalf("unauthenticated charter import route exposed authenticated guidance: %s", unauthenticatedBody)
	}

	credentialsResponse, err := client.Get("http://" + address + "/console/credentials")
	if err != nil {
		t.Fatal(err)
	}
	credentialsBody, _ := io.ReadAll(credentialsResponse.Body)
	_ = credentialsResponse.Body.Close()
	if credentialsResponse.StatusCode != http.StatusOK || !bytes.Contains(credentialsBody, []byte("credentials_authority_not_configured")) {
		t.Fatalf("credentials route did not surface unconfigured authority: status=%d body=%s", credentialsResponse.StatusCode, credentialsBody)
	}

	hostile := &app.FleetAgent{}
	hostile.Registration.AgentID = "</script><script>globalThis.pwned=1</script>"
	hostileSurface, err := consoleSurfaceModel(app.FleetSurface{
		Agents: []app.FleetAgent{*hostile},
		Readiness: map[string]app.SurfaceReadiness{
			"registry": {State: "ready", ReasonCode: "collection_read_succeeded", Source: "fleet.agent_registrations", Count: 1, Authoritative: true},
		},
	}, consoleAgents)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostileSurface.Records) != 1 || strings.Contains(hostileSurface.Records[0].JSON, "<script>") {
		t.Fatalf("hostile record label was not escaped: %#v", hostileSurface.Records)
	}
}

func TestConsoleAgentRegistrationAndLifecycleUseReviewedAuthoritativeState(t *testing.T) {
	svc := apiService(t)
	fleetStore := configureAPIFleet(t, svc)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)

	var issued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &issued, http.StatusCreated)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	exchange, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap="+url.QueryEscape(issued.Bootstrap)))
	exchange.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	exchange.Header.Set("Origin", "http://"+address)
	exchangeResponse, err := client.Do(exchange)
	if err != nil {
		t.Fatal(err)
	}
	_ = exchangeResponse.Body.Close()
	if exchangeResponse.StatusCode != http.StatusOK {
		t.Fatalf("session exchange status=%d", exchangeResponse.StatusCode)
	}
	stateResponse, err := client.Get("http://" + address + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		CSRF string `json:"csrf"`
	}
	if err = json.NewDecoder(stateResponse.Body).Decode(&state); err != nil {
		t.Fatal(err)
	}
	_ = stateResponse.Body.Close()
	if state.CSRF == "" {
		t.Fatal("authenticated console returned no CSRF capability")
	}

	charter := core.Charter{
		SchemaVersion: core.SchemaVersion, AgentID: "agent-alpha", Name: "Agent Alpha", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "profile/alpha"},
		Stanzas: []core.TrustStanza{{
			ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{"principal-1"}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Scopes: core.Scopes{Memory: []string{"agent-alpha"}, Credentials: []string{"provider:test"}}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"},
		}},
		CreatedBy: "principal-1", CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	canonical, err := core.Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	charterData, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	fixtureData, err := json.Marshal(registry.CurrentFleetFixture{
		SchemaVersion: registry.CurrentFleetFixtureSchemaVersion,
		FleetID:       "fleet-primary",
		Agents: []registry.CurrentFleetAgent{{
			SourceID: "fleet-agent-1", AgentID: charter.AgentID,
			Runtime:   registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/alpha"},
			Ownership: registry.Ownership{OwnerID: "principal-1", AccountabilityID: "team-platform"}, Lifecycle: registry.LifecycleEnabled,
			Charter:                reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: charter.Revision, Digest: canonical.Digest},
			CapabilityDeclarations: []string{"chat"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation := url.Values{"csrf": {state.CSRF}, "charter": {string(charterData)}, "fixture": {string(fixtureData)}, "fleet_id": {"fleet-primary"}, "source_id": {"fleet-agent-1"}}
	post := func(path string, values url.Values, origin string) (*http.Response, []byte) {
		t.Helper()
		request, requestErr := http.NewRequest(http.MethodPost, "http://"+address+path, strings.NewReader(values.Encode()))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return response, body
	}
	response, body := post("/console/agents/registration/review", operation, "http://"+address)
	if response.StatusCode != http.StatusBadRequest || !bytes.Contains(body, []byte("Registration proposal denied")) {
		t.Fatalf("missing stored charter review status=%d body=%s", response.StatusCode, body)
	}
	registrations, err := fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 0 {
		t.Fatalf("missing stored charter review changed registry: registrations=%+v err=%v", registrations, err)
	}
	if _, err = svc.GetCharter(charter.AgentID, charter.Revision); err == nil {
		t.Fatal("registration review imported a missing charter")
	}
	if err = svc.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}

	forged := url.Values{}
	for key, items := range operation {
		forged[key] = append([]string(nil), items...)
	}
	forged.Set("source_id", "substituted-source")
	response, body = post("/console/agents/registration/review", forged, "http://"+address)
	if response.StatusCode != http.StatusBadRequest || !bytes.Contains(body, []byte("Registration proposal denied")) {
		t.Fatalf("substituted source review status=%d body=%s", response.StatusCode, body)
	}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 0 {
		t.Fatalf("denied review changed registry: registrations=%+v err=%v", registrations, err)
	}
	response, _ = post("/console/agents/registration/review", operation, "http://attacker.example")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin review status=%d", response.StatusCode)
	}

	response, body = post("/console/agents/registration/review", operation, "http://"+address)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`action="/console/agents/registration/execute"`)) || !bytes.Contains(body, []byte(canonical.Digest)) || !bytes.Contains(body, []byte("team-platform")) {
		t.Fatalf("registration review status=%d body=%s", response.StatusCode, body)
	}
	receiptMarker := []byte(`name="receipt" value="`)
	receiptStart := bytes.Index(body, receiptMarker)
	if receiptStart < 0 {
		t.Fatalf("registration review omitted receipt: %s", body)
	}
	receiptStart += len(receiptMarker)
	receiptEnd := bytes.IndexByte(body[receiptStart:], '"')
	if receiptEnd != 64 {
		t.Fatalf("registration review emitted malformed receipt field")
	}
	receipt := string(body[receiptStart : receiptStart+receiptEnd])
	execute := url.Values{"csrf": {state.CSRF}, "receipt": {receipt}}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 0 {
		t.Fatalf("review mutated registry: registrations=%+v err=%v", registrations, err)
	}

	substitution := url.Values{"csrf": {state.CSRF}, "receipt": {receipt}, "charter": {string(charterData)}, "fixture": {string(fixtureData)}, "fleet_id": {"fleet-primary"}, "source_id": {"fleet-agent-1"}}
	response, _ = post("/console/agents/registration/execute", substitution, "http://"+address)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("raw candidate substitution status=%d", response.StatusCode)
	}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 0 {
		t.Fatalf("raw candidate substitution mutated registry: registrations=%+v err=%v", registrations, err)
	}
	if stored, getErr := svc.GetCharter(charter.AgentID, charter.Revision); getErr != nil || stored.Digest != canonical.Digest {
		t.Fatalf("denied registration changed the pre-existing charter: stored=%+v err=%v", stored, getErr)
	}

	var secondIssued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &secondIssued, http.StatusCreated)
	secondJar, _ := cookiejar.New(nil)
	secondClient := &http.Client{Jar: secondJar, Timeout: 5 * time.Second}
	secondExchange, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap="+url.QueryEscape(secondIssued.Bootstrap)))
	secondExchange.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondExchange.Header.Set("Origin", "http://"+address)
	secondResponse, err := secondClient.Do(secondExchange)
	if err != nil {
		t.Fatal(err)
	}
	_ = secondResponse.Body.Close()
	secondStateResponse, err := secondClient.Get("http://" + address + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var secondState struct {
		CSRF string `json:"csrf"`
	}
	if err = json.NewDecoder(secondStateResponse.Body).Decode(&secondState); err != nil {
		t.Fatal(err)
	}
	_ = secondStateResponse.Body.Close()
	crossSessionRequest, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/agents/registration/execute", strings.NewReader(url.Values{"csrf": {secondState.CSRF}, "receipt": {receipt}}.Encode()))
	crossSessionRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	crossSessionRequest.Header.Set("Origin", "http://"+address)
	crossSessionResponse, err := secondClient.Do(crossSessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = crossSessionResponse.Body.Close()
	if crossSessionResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-session receipt status=%d", crossSessionResponse.StatusCode)
	}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 0 {
		t.Fatalf("cross-session receipt mutated registry: registrations=%+v err=%v", registrations, err)
	}

	response, body = post("/console/agents/registration/execute", execute, "http://"+address)
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("Registered Agent with authoritative exact revision readback")) || !bytes.Contains(body, []byte("Open exact registered Agent")) {
		t.Fatalf("registration execute status=%d body=%s", response.StatusCode, body)
	}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 1 || registrations[0].AgentID != charter.AgentID {
		t.Fatalf("registration readback=%+v err=%v", registrations, err)
	}
	if stored, getErr := svc.GetCharter(charter.AgentID, charter.Revision); getErr != nil || stored.Digest != canonical.Digest {
		t.Fatalf("Agent registration changed the pre-existing charter: stored=%+v err=%v", stored, getErr)
	}
	response, _ = post("/console/agents/registration/execute", execute, "http://"+address)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("replayed registration receipt status=%d", response.StatusCode)
	}
	registrations, err = fleetStore.ListAgentRegistrations(ctx)
	if err != nil || len(registrations) != 1 {
		t.Fatalf("replayed registration receipt mutated registry: registrations=%+v err=%v", registrations, err)
	}
	// The security-denial probes above intentionally add requests to this
	// end-to-end lifecycle test; allow the production source bucket to refill
	// before exercising the unchanged lifecycle sequence below.
	time.Sleep(time.Second)
	initial, err := fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || initial.Revision != 1 || initial.Digest == "" || initial.Charter.Digest != canonical.Digest {
		t.Fatalf("initial authoritative revision=%+v err=%v", initial, err)
	}

	lifecycle := url.Values{"csrf": {state.CSRF}, "revision": {"1"}, "digest": {initial.Digest}, "lifecycle": {"disabled"}}
	response, body = post("/console/agents/"+charter.AgentID+"/lifecycle", lifecycle, "http://"+address)
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/console/agents" {
		t.Fatalf("disable lifecycle status=%d final=%s body=%s", response.StatusCode, response.Request.URL, body)
	}
	disabled, err := fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || disabled.Revision != 2 || disabled.Lifecycle != registry.LifecycleDisabled || disabled.Digest == initial.Digest {
		t.Fatalf("disabled authoritative revision=%+v err=%v", disabled, err)
	}
	response, _ = post("/console/agents/"+charter.AgentID+"/lifecycle", lifecycle, "http://"+address)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale lifecycle revision status=%d", response.StatusCode)
	}
	latest, err := fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || latest.Digest != disabled.Digest {
		t.Fatalf("stale lifecycle request mutated state: latest=%+v err=%v", latest, err)
	}
	enable := url.Values{"csrf": {state.CSRF}, "revision": {"2"}, "digest": {disabled.Digest}, "lifecycle": {"enabled"}}
	response, body = post("/console/agents/"+charter.AgentID+"/lifecycle", enable, "http://"+address)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("enable lifecycle status=%d body=%s", response.StatusCode, body)
	}
	enabled, err := fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || enabled.Revision != 3 || enabled.Lifecycle != registry.LifecycleEnabled {
		t.Fatalf("enabled authoritative revision=%+v err=%v", enabled, err)
	}
	retire := url.Values{"csrf": {state.CSRF}, "revision": {"3"}, "digest": {enabled.Digest}, "lifecycle": {"retired"}, "confirm_retirement": {"retire"}}
	response, body = post("/console/agents/"+charter.AgentID+"/lifecycle", retire, "http://"+address)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("retire lifecycle status=%d body=%s", response.StatusCode, body)
	}
	retired, err := fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || retired.Revision != 4 || retired.Lifecycle != registry.LifecycleRetired {
		t.Fatalf("retired authoritative revision=%+v err=%v", retired, err)
	}
	reenable := url.Values{"csrf": {state.CSRF}, "revision": {"4"}, "digest": {retired.Digest}, "lifecycle": {"enabled"}}
	response, _ = post("/console/agents/"+charter.AgentID+"/lifecycle", reenable, "http://"+address)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("retired Agent re-enable status=%d", response.StatusCode)
	}
	latest, err = fleetStore.LatestAgentRevision(ctx, charter.AgentID)
	if err != nil || latest.Digest != retired.Digest {
		t.Fatalf("retired Agent re-enable mutated state: latest=%+v err=%v", latest, err)
	}
}

func TestConsoleAuthenticatedSessionCSRFHeadersAndPagination(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)

	var issued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &issued, http.StatusCreated)
	if issued.Bootstrap == "" {
		t.Fatal("server issued empty browser bootstrap")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	shell, err := client.Get("http://" + address + "/console")
	if err != nil {
		t.Fatal(err)
	}
	shellBody, _ := io.ReadAll(shell.Body)
	_ = shell.Body.Close()
	if shell.StatusCode != http.StatusOK || !strings.Contains(string(shellBody), "Authentication required") || strings.Contains(string(shellBody), "Authenticated control plane") || strings.Contains(string(shellBody), "<script") || strings.Contains(string(shellBody), "data-on:") || !strings.Contains(shell.Header.Get("Content-Security-Policy"), "default-src 'none'") || shell.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe console shell status=%d headers=%v", shell.StatusCode, shell.Header)
	}
	asset, err := client.Get("http://" + address + "/console/assets/datastar-v1.0.2.js")
	if err != nil {
		t.Fatal(err)
	}
	assetBody, _ := io.ReadAll(asset.Body)
	_ = asset.Body.Close()
	if asset.StatusCode != http.StatusOK || !strings.HasPrefix(asset.Header.Get("Content-Type"), "text/javascript") || len(assetBody) < 1000 {
		t.Fatalf("self-hosted Datastar asset status=%d type=%q bytes=%d", asset.StatusCode, asset.Header.Get("Content-Type"), len(assetBody))
	}
	exchangeBody, _ := json.Marshal(map[string]string{"bootstrap": issued.Bootstrap})
	forgedBody, _ := json.Marshal(map[string]string{"bootstrap": issued.Bootstrap, "authority": "forged-admin"})
	forged, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", bytes.NewReader(forgedBody))
	forged.Header.Set("Content-Type", "application/json")
	forged.Header.Set("Origin", "http://"+address)
	forgedResponse, err := client.Do(forged)
	if err != nil {
		t.Fatal(err)
	}
	if forgedResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged authority field status=%d", forgedResponse.StatusCode)
	}
	_ = forgedResponse.Body.Close()
	exchange, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", bytes.NewReader(exchangeBody))
	exchange.Header.Set("Content-Type", "application/json")
	exchange.Header.Set("Origin", "http://"+address)
	response, err := client.Do(exchange)
	if err != nil {
		t.Fatal(err)
	}
	var established struct {
		CSRF    string `json:"csrf"`
		Expires string `json:"expires"`
	}
	if err = json.NewDecoder(response.Body).Decode(&established); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated || established.CSRF == "" || established.Expires == "" || strings.Contains(string(shellBody), issued.Bootstrap) {
		t.Fatalf("session exchange status=%d csrf=%t", response.StatusCode, established.CSRF != "")
	}
	state, err := client.Get("http://" + address + "/console/api/state?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	if state.StatusCode != http.StatusOK || state.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated state status=%d headers=%v", state.StatusCode, state.Header)
	}
	var consoleState struct {
		State   string           `json:"state"`
		Surface app.FleetSurface `json:"surface"`
	}
	if err = json.NewDecoder(state.Body).Decode(&consoleState); err != nil {
		t.Fatal(err)
	}
	if consoleState.State != "ready" || len(consoleState.Surface.Readiness) != 5 {
		t.Fatalf("console did not return authoritative fleet readiness: %+v", consoleState)
	}
	for _, domain := range []string{"registry", "loops", "graphs", "queue"} {
		readiness := consoleState.Surface.Readiness[domain]
		if readiness.State != "empty" || readiness.ReasonCode != "collection_read_succeeded_empty" || readiness.Source == "" || !readiness.Authoritative || readiness.Count != 0 {
			t.Fatalf("empty %s readiness was not authoritative: %+v", domain, readiness)
		}
	}
	credentials := consoleState.Surface.Readiness["credentials"]
	if credentials.State != "unconfigured" || credentials.ReasonCode != "credentials_authority_not_configured" || credentials.Count != 0 || credentials.Authoritative {
		t.Fatalf("credential readiness must report unconfigured when no bbolt authority is wired: readiness=%+v", credentials)
	}
	if len(consoleState.Surface.Credentials) != 0 {
		t.Fatalf("credential surface should be empty when authority is unconfigured: %+v", consoleState.Surface.Credentials)
	}
	_ = state.Body.Close()
	fragment, _ := http.NewRequest(http.MethodGet, "http://"+address+"/console/fragments/surface?domain=graphs&limit=10", nil)
	fragment.Header.Set("Accept", "text/event-stream")
	fragmentResponse, err := client.Do(fragment)
	if err != nil {
		t.Fatal(err)
	}
	fragmentBody, _ := io.ReadAll(fragmentResponse.Body)
	_ = fragmentResponse.Body.Close()
	if fragmentResponse.StatusCode != http.StatusOK || fragmentResponse.Header.Get("Content-Type") != "text/event-stream" || !bytes.Contains(fragmentBody, []byte("event: datastar-patch-elements")) || !bytes.Contains(fragmentBody, []byte("Graphs")) || !bytes.Contains(fragmentBody, []byte("authoritative collection is empty")) {
		t.Fatalf("Datastar surface patch status=%d type=%q body=%s", fragmentResponse.StatusCode, fragmentResponse.Header.Get("Content-Type"), fragmentBody)
	}
	excess, _ := client.Get("http://" + address + "/console/api/state?limit=100000")
	if excess.StatusCode != http.StatusBadRequest {
		t.Fatalf("excessive page size status=%d", excess.StatusCode)
	}
	_ = excess.Body.Close()
	commandBody := `{"schema_version":"aegis.console-command-catalog.v1","command_id":"unregistered.command","target_id":"agent-1","expected_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","idempotency_key":"browser-test","input":{}}`
	commandRequest, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/api/commands/preview", strings.NewReader(commandBody))
	commandRequest.Header.Set("Origin", "http://"+address)
	commandRequest.Header.Set("X-CSRF-Token", established.CSRF)
	commandRequest.Header.Set("Content-Type", "application/json")
	commandResponse, err := client.Do(commandRequest)
	if err != nil {
		t.Fatal(err)
	}
	commandResponseBody, _ := io.ReadAll(commandResponse.Body)
	_ = commandResponse.Body.Close()
	if commandResponse.StatusCode != http.StatusNotFound || !bytes.Contains(commandResponseBody, []byte(`"code":"invalid_request"`)) {
		t.Fatalf("authenticated browser command path status=%d body=%s", commandResponse.StatusCode, commandResponseBody)
	}
	forgedCommand, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/api/commands/preview", strings.NewReader(commandBody))
	forgedCommand.Header.Set("Origin", "http://"+address)
	forgedCommand.Header.Set("X-CSRF-Token", "forged")
	forgedCommand.Header.Set("Content-Type", "application/json")
	forgedCommandResponse, err := client.Do(forgedCommand)
	if err != nil {
		t.Fatal(err)
	}
	_ = forgedCommandResponse.Body.Close()
	if forgedCommandResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("forged browser command CSRF status=%d", forgedCommandResponse.StatusCode)
	}
	logout, _ := http.NewRequest(http.MethodDelete, "http://"+address+"/console/session", nil)
	logout.Header.Set("Origin", "http://attacker.example")
	logout.Header.Set("X-CSRF-Token", established.CSRF)
	denied, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin logout status=%d", denied.StatusCode)
	}
	_ = denied.Body.Close()
	logout.Header.Set("Origin", "http://"+address)
	logout.Header.Set("Accept", "text/event-stream")
	loggedOut, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	loggedOutBody, _ := io.ReadAll(loggedOut.Body)
	_ = loggedOut.Body.Close()
	if loggedOut.StatusCode != http.StatusOK || !bytes.Contains(loggedOutBody, []byte("Sign the Aegis principal into this browser")) {
		t.Fatalf("Datastar logout patch status=%d body=%s", loggedOut.StatusCode, loggedOutBody)
	}

	var nativeIssued struct {
		Bootstrap string `json:"bootstrap"`
	}
	apiRequest(t, unixClient(svc.Config.API.UnixSocket), http.MethodPost, "/v1/console/bootstrap", map[string]any{}, &nativeIssued, http.StatusCreated)
	nativeExchange, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/session", strings.NewReader("bootstrap="+url.QueryEscape(nativeIssued.Bootstrap)))
	nativeExchange.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	nativeExchange.Header.Set("Origin", "http://"+address)
	nativeAuthenticated, err := client.Do(nativeExchange)
	if err != nil {
		t.Fatal(err)
	}
	nativeAuthenticatedBody, _ := io.ReadAll(nativeAuthenticated.Body)
	_ = nativeAuthenticated.Body.Close()
	if nativeAuthenticated.StatusCode != http.StatusOK || nativeAuthenticated.Request.URL.Path != "/console/agents" || nativeAuthenticated.Request.URL.Fragment != "/agents" || !bytes.Contains(nativeAuthenticatedBody, []byte("Agent Registry")) || bytes.Contains(nativeAuthenticatedBody, []byte("Sign the Aegis principal into this browser")) {
		t.Fatalf("native bootstrap flow status=%d final=%s body=%s", nativeAuthenticated.StatusCode, nativeAuthenticated.Request.URL, nativeAuthenticatedBody)
	}
	nativeGraphs, err := client.Get("http://" + address + "/console?domain=graphs")
	if err != nil {
		t.Fatal(err)
	}
	nativeGraphsBody, _ := io.ReadAll(nativeGraphs.Body)
	_ = nativeGraphs.Body.Close()
	if nativeGraphs.StatusCode != http.StatusOK || !bytes.Contains(nativeGraphsBody, []byte("Graphs")) || !bytes.Contains(nativeGraphsBody, []byte("authoritative collection is empty")) {
		t.Fatalf("native graph navigation status=%d body=%s", nativeGraphs.StatusCode, nativeGraphsBody)
	}
	nativeState, err := client.Get("http://" + address + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var nativeSession struct {
		CSRF string `json:"csrf"`
	}
	if err = json.NewDecoder(nativeState.Body).Decode(&nativeSession); err != nil {
		t.Fatal(err)
	}
	_ = nativeState.Body.Close()
	nativeLogout, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/logout", strings.NewReader("csrf="+url.QueryEscape(nativeSession.CSRF)))
	nativeLogout.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	nativeLogout.Header.Set("Origin", "http://"+address)
	nativeLoggedOut, err := client.Do(nativeLogout)
	if err != nil {
		t.Fatal(err)
	}
	nativeLoggedOutBody, _ := io.ReadAll(nativeLoggedOut.Body)
	_ = nativeLoggedOut.Body.Close()
	if nativeLoggedOut.StatusCode != http.StatusOK || nativeLoggedOut.Request.URL.Path != "/console" || !bytes.Contains(nativeLoggedOutBody, []byte("Sign the Aegis principal into this browser")) {
		t.Fatalf("native logout status=%d final=%s body=%s", nativeLoggedOut.StatusCode, nativeLoggedOut.Request.URL, nativeLoggedOutBody)
	}

	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestServeRejectsInvalidTLSIdentity(t *testing.T) {
	svc := apiService(t)
	svc.Config.API.UnixSocket = ""
	svc.Config.API.Listen = "127.0.0.1:0"
	svc.Config.API.TLSCertFile = filepath.Join(t.TempDir(), "server.crt")
	svc.Config.API.TLSKeyFile = filepath.Join(t.TempDir(), "server.key")
	if err := os.WriteFile(svc.Config.API.TLSCertFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.Config.API.TLSKeyFile, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Serve(context.Background(), svc)
	if err == nil || !strings.Contains(err.Error(), "load API TLS identity") {
		t.Fatalf("invalid TLS identity did not fail before serving: %v", err)
	}
}

func TestShutdownWaitsForInflightRequest(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	telemetry := &blockingTelemetry{entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- ServeWithTelemetry(ctx, svc, telemetry) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)
	requestDone := make(chan error, 1)
	go func() {
		response, err := client.Get("http://unix/livez")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			err = response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-telemetry.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter telemetry middleware")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("server returned before in-flight request drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(telemetry.release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func apiRequest(t *testing.T, client *http.Client, method, path string, input, output any, wantStatus int) {
	t.Helper()
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, "http://unix"+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer transport-secret")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, wantStatus, data)
	}
	if output != nil {
		if err = json.NewDecoder(response.Body).Decode(output); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFleetAgentAPIUsesAuthenticatedSharedApplicationBoundary(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	digest := "sha256:" + strings.Repeat("a", 64)
	fixture, err := json.Marshal(registry.CurrentFleetFixture{
		SchemaVersion: registry.CurrentFleetFixtureSchemaVersion,
		FleetID:       "fleet-primary",
		Agents: []registry.CurrentFleetAgent{{
			SourceID:  "fleet-agent-1",
			AgentID:   "agent-alpha",
			Runtime:   registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/alpha"},
			Ownership: registry.Ownership{OwnerID: "operator-primary", AccountabilityID: "team-platform"},
			Lifecycle: registry.LifecycleEnabled,
			Charter: reference.RevisionRef{
				SchemaVersion: reference.RevisionRefSchemaVersion,
				ID:            "agent-alpha",
				Revision:      7,
				Digest:        digest,
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := app.RegisterFleetAgentInput{
		Fixture:  fixture,
		Identity: registry.FleetSource{FleetID: "fleet-primary", Kind: registry.CurrentFleetSourceKind, SourceID: "fleet-agent-1"},
	}
	var created struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", input, &created, http.StatusCreated)
	if !created.Created || created.Agent.Registration.AgentID != "agent-alpha" || created.Agent.Revision.Charter.Digest != digest {
		t.Fatalf("registered fleet participant lost immutable provenance: %+v", created)
	}

	var replay struct {
		Created bool `json:"created"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", input, &replay, http.StatusOK)
	if replay.Created {
		t.Fatal("exact registration replay created a second fleet participant")
	}
	var listed []app.FleetAgent
	apiRequest(t, client, http.MethodGet, "/v1/agents", nil, &listed, http.StatusOK)
	if len(listed) != 1 || listed[0].Revision.Digest != created.Agent.Revision.Digest {
		t.Fatalf("fleet list did not read back the registered immutable revision: %+v", listed)
	}
	var shown app.FleetAgent
	apiRequest(t, client, http.MethodGet, "/v1/agents/agent-alpha?revision=1", nil, &shown, http.StatusOK)
	if shown.Revision.Digest != created.Agent.Revision.Digest {
		t.Fatalf("exact revision readback mismatch: got=%q want=%q", shown.Revision.Digest, created.Agent.Revision.Digest)
	}
	lifecycleInput := app.SetAgentLifecycleInput{
		Expected:  reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent-alpha", Revision: 1, Digest: created.Agent.Revision.Digest},
		Lifecycle: registry.LifecycleDisabled,
	}
	var disabled app.FleetAgent
	apiRequest(t, client, http.MethodPut, "/v1/agents/agent-alpha/lifecycle", lifecycleInput, &disabled, http.StatusCreated)
	if disabled.Revision.Revision != 2 || disabled.Revision.Lifecycle != registry.LifecycleDisabled || disabled.Revision.Digest == created.Agent.Revision.Digest {
		t.Fatalf("lifecycle route did not append an immutable revision: %+v", disabled)
	}
	var history []registry.AgentRevision
	apiRequest(t, client, http.MethodGet, "/v1/agents/agent-alpha/revisions", nil, &history, http.StatusOK)
	if len(history) != 2 || history[0].Digest != created.Agent.Revision.Digest || history[1].Digest != disabled.Revision.Digest {
		t.Fatalf("revision history route lost ordered immutable provenance: %+v", history)
	}
	apiRequest(t, client, http.MethodPut, "/v1/agents/agent-alpha/lifecycle", lifecycleInput, nil, http.StatusConflict)
	wrongAgent := lifecycleInput
	wrongAgent.Expected.ID = "prompt-selected-agent"
	apiRequest(t, client, http.MethodPut, "/v1/agents/agent-alpha/lifecycle", wrongAgent, nil, http.StatusConflict)
	var loops []app.LoopView
	apiRequest(t, client, http.MethodGet, "/v1/loops", nil, &loops, http.StatusOK)
	var graphs []app.GraphView
	apiRequest(t, client, http.MethodGet, "/v1/graphs", nil, &graphs, http.StatusOK)
	var queueItems []app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &queueItems, http.StatusOK)
	var readiness struct {
		State       string                             `json:"state"`
		Collections map[string]app.SurfaceReadiness    `json:"collections"`
		Actions     map[string]orchestration.Readiness `json:"actions"`
	}
	apiRequest(t, client, http.MethodGet, "/v1/fleet/readiness", nil, &readiness, http.StatusOK)
	if len(loops) != 0 || len(graphs) != 0 || len(queueItems) != 0 || readiness.State != "ready" || readiness.Collections["registry"].Count != 1 || readiness.Collections["registry"].State != "ready" || len(readiness.Actions) == 0 {
		t.Fatalf("live fleet collection routes returned inconsistent state: loops=%d graphs=%d queue=%d readiness=%+v", len(loops), len(graphs), len(queueItems), readiness)
	}

	apiRequest(t, client, http.MethodGet, "/v1/agents/agent-alpha?revision=prompt-selected", nil, nil, http.StatusBadRequest)
	apiRequest(t, client, http.MethodPost, "/v1/agents", map[string]any{"fixture": json.RawMessage(fixture), "identity": input.Identity, "subject": "model-selected"}, nil, http.StatusBadRequest)
}

func TestGraphLifecycleAndSubmissionHistoryRoutesAreAuthenticatedExactReads(t *testing.T) {
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	value := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	revision, validation, err := graph.NewRevision(graph.GraphRevision{
		GraphID: "graph-route", Revision: 1, Inputs: []graph.Port{value}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeString, Required: true}},
		Nodes:         []graph.Node{{ID: "work", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent-route", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "loop-route", Revision: 1, Digest: "sha256:" + strings.Repeat("b", 64)}, Inputs: []graph.Port{value}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeString, Required: true}}}},
		InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: "work", ToPort: "value"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "work", FromPort: "result", GraphOutput: "result"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PublishGraph(context.Background(), graph.PublishRequest{Revision: revision, Validation: validation, IdempotencyKey: "publish-route"}, fleet.AuditFact{Event: core.AuditEvent{Type: "graph.published", SubjectID: revision.GraphID, PrincipalID: "principal-1", Outcome: "succeeded", Reason: "authorized test mutation"}}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	var lifecycle graph.Lifecycle
	apiRequest(t, client, http.MethodGet, "/v1/graphs/graph-route/lifecycle", nil, &lifecycle, http.StatusOK)
	if lifecycle.GraphID != revision.GraphID || lifecycle.State != graph.LifecycleActive || lifecycle.ActiveRevision != revision.Revision || lifecycle.ActiveDigest != revision.Digest {
		t.Fatalf("lifecycle=%+v", lifecycle)
	}
	var history app.SubmissionHistory
	apiRequest(t, client, http.MethodGet, "/v1/submissions", nil, &history, http.StatusOK)
	if history.Accepted == nil || history.Rejected == nil || len(history.Accepted) != 0 || len(history.Rejected) != 0 {
		t.Fatalf("empty authoritative history=%+v", history)
	}
	apiRequest(t, client, http.MethodGet, "/v1/graphs/unknown/lifecycle", nil, nil, http.StatusNotFound)

	for _, path := range []string{"/v1/graphs/graph-route/lifecycle", "/v1/submissions"} {
		request, requestErr := http.NewRequest(http.MethodGet, "http://unix"+path, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		response, requestErr := client.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s status=%d", path, response.StatusCode)
		}
	}
}

func TestQueueLifecycleMutationRoutesRejectPathBodySubstitution(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	for _, action := range []string{"retry", "cancel", "expire", "exhaust", "revoke"} {
		t.Run(action, func(t *testing.T) {
			apiRequest(t, client, http.MethodPost, "/v1/queue/path-item/"+action, map[string]any{
				"queue_item_id": "body-item",
			}, nil, http.StatusBadRequest)
		})
	}
}

func TestFleetAPIUnavailableFailsClosed(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)
	for _, path := range []string{"/v1/agents", "/v1/loops", "/v1/graphs", "/v1/graphs/graph-1/lifecycle", "/v1/submissions", "/v1/queue", "/v1/fleet/readiness"} {
		apiRequest(t, client, http.MethodGet, path, nil, nil, http.StatusServiceUnavailable)
	}
}

func TestIdempotentWriteStatus(t *testing.T) {
	if got := createdOrOK(false); got != http.StatusCreated {
		t.Fatalf("new write status=%d", got)
	}
	if got := createdOrOK(true); got != http.StatusOK {
		t.Fatalf("idempotent write status=%d", got)
	}
}

func TestUnixAPICompleteOperationalWorkflow(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	telemetry := &telemetryRecorder{}
	go func() { done <- ServeWithTelemetry(ctx, svc, telemetry) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", svc.Config.API.UnixSocket)
	}}, Timeout: 5 * time.Second}

	now := time.Now().UTC().Truncate(time.Second)
	uid := strconv.Itoa(os.Getuid())
	charter := core.Charter{
		SchemaVersion: core.SchemaVersion,
		AgentID:       "api-agent",
		Name:          "API Agent",
		Revision:      1,
		Runtime:       core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{
			ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + uid}, PrincipalIDs: []string{"principal-1"}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Scopes: core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}},
			Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"},
		}},
		CreatedBy: "principal-1", CreatedAt: now,
	}

	var imported core.CanonicalCharter
	apiRequest(t, client, http.MethodPost, "/v1/charters/import", charter, &imported, http.StatusCreated)
	if imported.Digest == "" {
		t.Fatal("API import returned no charter digest")
	}
	var redacted config.Config
	apiRequest(t, client, http.MethodGet, "/v1/config", nil, &redacted, http.StatusOK)
	if redacted.API.Token != "[REDACTED]" {
		t.Fatal("API configuration exposed its transport token")
	}
	var decision core.Decision
	apiRequest(t, client, http.MethodPost, "/v1/authorization/explain", map[string]any{"agent": "api-agent", "revision": 1, "stanza": "principal", "environment": core.Environment{Name: "local"}}, &decision, http.StatusOK)
	if !decision.Allowed || decision.Selected == nil || decision.Selected.ID != "principal" {
		t.Fatalf("peer-authenticated authorization decision=%+v", decision)
	}
	var denied core.Decision
	apiRequest(t, client, http.MethodPost, "/v1/authorization/explain", map[string]any{"agent": "api-agent", "revision": 1, "stanza": "model-requested-admin", "environment": core.Environment{Name: "local"}}, &denied, http.StatusForbidden)
	if denied.Allowed || denied.Selected != nil || denied.Reason != "requested_stanza_unauthorized" {
		t.Fatalf("API denial did not return shared safe decision: %+v", denied)
	}
	apiRequest(t, client, http.MethodPost, "/v1/authorization/explain", map[string]any{"agent": "api-agent", "revision": 1, "stanza": "principal", "environment": core.Environment{Name: "production"}}, &denied, http.StatusForbidden)
	if denied.Allowed || denied.Reason != "invalid_environment" {
		t.Fatalf("API request environment broadened authority: %+v", denied)
	}
	var effective struct {
		AuthorityNotUnioned bool                    `json:"authority_not_unioned"`
		Authority           core.EffectiveAuthority `json:"authority"`
		Decision            core.Decision           `json:"decision"`
	}
	apiRequest(t, client, http.MethodGet, "/v1/charters/api-agent/1/stanzas/principal", nil, &effective, http.StatusOK)
	if !effective.AuthorityNotUnioned || effective.Authority.StanzaID != "principal" || len(effective.Authority.Tools) != 1 || effective.Authority.Tools[0] != "no_mcp" || !effective.Decision.Allowed {
		t.Fatal("effective stanza response did not preserve no-union invariant")
	}
	var review core.Review
	apiRequest(t, client, http.MethodPost, "/v1/plans/preview", map[string]any{"agent": "api-agent", "revision": 1, "environment": core.Environment{Name: "local"}}, &review, http.StatusCreated)
	var approval core.Approval
	apiRequest(t, client, http.MethodPost, "/v1/approvals", map[string]any{"plan_id": review.Plan.ID, "ttl": "1m"}, &approval, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/approvals/"+approval.ID+"/decision", map[string]bool{"approve": true}, &approval, http.StatusOK)
	var receipt core.Receipt
	apiRequest(t, client, http.MethodPost, "/v1/provision", map[string]string{"plan_id": review.Plan.ID, "approval_id": approval.ID}, &receipt, http.StatusCreated)
	if receipt.Status != "verified" {
		t.Fatalf("provisioning receipt status=%q", receipt.Status)
	}
	var preview struct {
		Mandate core.Mandate `json:"mandate"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/sessions/preview", map[string]any{"agent": "api-agent", "revision": 1, "stanza": "principal", "environment": core.Environment{Name: "local"}}, &preview, http.StatusCreated)
	var session core.Session
	apiRequest(t, client, http.MethodPost, "/v1/sessions/start", map[string]string{"mandate_id": preview.Mandate.ID}, &session, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/sessions/"+session.ID+"/terminate", map[string]string{"reason": "api_e2e_complete"}, &map[string]string{}, http.StatusOK)
	apiRequest(t, client, http.MethodGet, "/v1/audit/verify", nil, &map[string]bool{}, http.StatusOK)
	var deliveryStatus core.AuditDeliveryStatus
	apiRequest(t, client, http.MethodGet, "/v1/audit/delivery/status", nil, &deliveryStatus, http.StatusOK)
	if deliveryStatus.Pending == 0 || deliveryStatus.Current {
		t.Fatalf("audit delivery did not expose pending work: %+v", deliveryStatus)
	}
	var deliveryResult core.AuditDeliveryResult
	apiRequest(t, client, http.MethodPost, "/v1/audit/delivery", map[string]int{"limit": 1000}, &deliveryResult, http.StatusOK)
	if deliveryResult.Delivered == 0 || !deliveryResult.Status.Current {
		t.Fatalf("audit delivery result = %+v", deliveryResult)
	}
	var verification map[string]bool
	apiRequest(t, client, http.MethodGet, "/v1/audit/delivery/verify", nil, &verification, http.StatusOK)
	if !verification["valid"] || verification["current"] {
		t.Fatalf("verification must distinguish valid lag from current delivery: %+v", verification)
	}
	apiRequest(t, client, http.MethodPost, "/v1/audit/projection/rebuild", map[string]string{}, &deliveryStatus, http.StatusOK)
	if !deliveryStatus.Current || !deliveryStatus.Verifiable {
		t.Fatalf("rebuilt audit delivery status = %+v", deliveryStatus)
	}
	telemetry.mu.Lock()
	defer telemetry.mu.Unlock()
	foundTemplate := false
	for _, observation := range telemetry.observations {
		if strings.Contains(observation.Route, session.ID) {
			t.Fatalf("telemetry used a high-cardinality resource ID as route: %+v", observation)
		}
		if observation.Route == "/v1/sessions/:id/terminate" {
			foundTemplate = true
		}
	}
	if !foundTemplate {
		t.Fatalf("stable route-template telemetry missing: %+v", telemetry.observations)
	}
}
