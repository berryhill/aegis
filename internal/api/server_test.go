package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
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
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
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
	cfg.API.UnixSocket = filepath.Join(root, "aegis.sock")
	cfg.Credentials.ProviderAuth["test"] = config.CredentialBinding{Type: "environment", SourceEnv: "AEGIS_API_TEST_KEY", TargetEnv: "TEST_PROVIDER_KEY"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := app.New(cfg, state, hermes.New(executable, logger), logger)
	svc.LookupEnv = func(name string) (string, bool) {
		if name == "AEGIS_API_TEST_KEY" {
			return "api-test-secret", true
		}
		return "", false
	}
	return svc
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

	request, _ := http.NewRequest(http.MethodGet, "http://unix/v1/runtime", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing bearer status = %d", response.StatusCode)
	}
	_ = response.Body.Close()
	telemetry.mu.Lock()
	if len(telemetry.observations) == 0 || telemetry.observations[0].Route != "/v1/runtime" || telemetry.observations[0].Status != http.StatusUnauthorized {
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
