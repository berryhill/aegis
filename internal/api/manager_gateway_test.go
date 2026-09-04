package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/managergateway"
)

func TestManagerCommandParseFailuresHaveSafeTypedEnvelope(t *testing.T) {
	status, code, message := classifyError(mapManagerCommandError(&managergateway.CommandParseError{Message: "usage: /agents readiness"}))
	if status != http.StatusBadRequest || code != "manager_command_parse_error" || message != "usage: /agents readiness" {
		t.Fatalf("classification=(%d, %q, %q)", status, code, message)
	}
	status, code, message = classifyError(mapManagerCommandError(errors.New("private parser diagnostic")))
	if status != http.StatusInternalServerError || code != "internal_error" || message != "internal server error" {
		t.Fatalf("generic error was exposed: (%d, %q, %q)", status, code, message)
	}
}

func TestManagerTurnFailuresHaveSafeTypedTaxonomy(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "degraded runtime", err: managergateway.ErrTurnRuntimeUnavailable, status: http.StatusServiceUnavailable, code: "manager_runtime_unavailable", message: "manager conversation runtime is unavailable"},
		{name: "authority unavailable", err: managergateway.ErrTurnAuthorityUnavailable, status: http.StatusServiceUnavailable, code: "manager_authority_unavailable", message: "manager credential authority is unavailable"},
		{name: "authority invalid", err: managergateway.ErrTurnAuthorityInvalid, status: http.StatusServiceUnavailable, code: "manager_authority_invalid", message: "manager credential authority is invalid"},
		{name: "timeout", err: context.DeadlineExceeded, status: http.StatusGatewayTimeout, code: "manager_turn_timeout", message: "manager conversational turn timed out"},
		{name: "protocol", err: managergateway.ErrTurnProtocol, status: http.StatusBadGateway, code: "manager_turn_protocol_error", message: "manager conversation runtime protocol failed"},
		{name: "internal", err: errors.New("prompt and secret must not escape"), status: http.StatusInternalServerError, code: "manager_turn_internal_error", message: "manager conversational turn failed"},
		{name: "incidental runtime token", err: errors.New("diagnostic mentions manager_model_absent but is not typed"), status: http.StatusInternalServerError, code: "manager_turn_internal_error", message: "manager conversational turn failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, code, message := classifyError(mapManagerTurnError(test.err))
			if status != test.status || code != test.code || message != test.message {
				t.Fatalf("classification=(%d, %q, %q), want (%d, %q, %q)", status, code, message, test.status, test.code, test.message)
			}
			if strings.Contains(message, "sensitive") || strings.Contains(message, "prompt") || strings.Contains(message, "secret") {
				t.Fatalf("unsafe manager error detail escaped: %q", message)
			}
		})
	}
}

func TestManagerGatewayWriteTimeoutCoversStartupAndTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.API.WriteTimeout = 30 * time.Second
	cfg.Manager.Inference.StartTimeout = 2 * time.Minute
	cfg.Manager.Inference.RequestTimeout = 45 * time.Second
	cfg.Manager.Hermes.GatewayStartTimeout = 20 * time.Second
	cfg.Manager.Hermes.TurnTimeout = 4 * time.Minute
	cfg.Manager.CleanupTimeout = 7 * time.Minute

	got := managerGatewayWriteTimeout(cfg)
	want := cfg.Manager.CleanupTimeout + 5*time.Second
	if got < want {
		t.Fatalf("write timeout=%s, want at least %s for sequential manager startup operations", got, want)
	}
}

func TestManagerGatewaySessionExecutesThroughSoleServiceAndRevokes(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	request := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions", map[string]any{})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create session status=%d", response.StatusCode)
	}
	var session struct {
		ID      string `json:"id"`
		Token   string `json:"token"`
		Expires string `json:"expires"`
		Mode    string `json:"mode"`
		Reason  string `json:"reason"`
		Next    string `json:"next_step"`
	}
	if err = json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if session.ID == "" || session.Token == "" || session.Expires == "" || session.Mode != "degraded" || session.Reason != "manager_model_absent" || session.Next == "" {
		t.Fatalf("incomplete session metadata: %+v", session)
	}

	for _, step := range []struct {
		input string
		kind  string
	}{
		{input: "can we register an agent?", kind: "agent_registration_guidance"},
		{input: "how many agents are resistered?", kind: "agent_registry_count"},
		{input: "can you ensure our aegis gateway and dashboard are up to date?", kind: "manager_lifecycle_guidance"},
	} {
		turn := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/turns", map[string]string{"input": step.input})
		turn.Header.Set(managergateway.SessionHeader, session.Token)
		response, err = client.Do(turn)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("authoritative turn %q status=%d", step.input, response.StatusCode)
		}
		var result managergateway.TurnResult
		if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if result.Kind != step.kind || result.Origin != managergateway.TurnOriginAuthoritative || result.Data["model_bypassed"] != true {
			t.Fatalf("authoritative turn %q returned %+v", step.input, result)
		}
		if step.kind == "agent_registration_guidance" && (result.Data["registered"] != false || result.Data["prepared"] != false || result.Data["activated"] != false) {
			t.Fatalf("registration guidance reported an effect: %+v", result.Data)
		}
		if step.kind == "manager_lifecycle_guidance" && (result.Data["mutated"] != false || result.Data["checked"] != false || result.Data["updated"] != false || result.Data["restarted"] != false) {
			t.Fatalf("lifecycle guidance reported an effect: %+v", result.Data)
		}
	}

	turn := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/turns", map[string]string{"input": "hello"})
	turn.Header.Set("X-Aegis-Manager-Session", session.Token)
	response, err = client.Do(turn)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("degraded turn status=%d, want 503", response.StatusCode)
	}
	var turnFailure envelope
	if err = json.NewDecoder(response.Body).Decode(&turnFailure); err != nil {
		t.Fatal(err)
	}
	if turnFailure.Code != "manager_runtime_unavailable" || turnFailure.Message != "manager conversation runtime is unavailable" {
		t.Fatalf("degraded turn failure=%+v", turnFailure)
	}
	_ = response.Body.Close()

	command := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/commands", map[string]string{"input": "/status"})
	command.Header.Set("X-Aegis-Manager-Session", session.Token)
	response, err = client.Do(command)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("execute status=%d", response.StatusCode)
	}
	var result struct {
		Operation string `json:"operation"`
		State     string `json:"state"`
		StanzaID  string `json:"stanza_id"`
		ContextID string `json:"context_id"`
	}
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if result.Operation != "manager.status" || result.State != "completed" || result.StanzaID != "secrets-manager" || result.ContextID != session.ID {
		t.Fatalf("unexpected gateway result: %+v", result)
	}

	closeRequest := managerGatewayRequest(t, svc, http.MethodDelete, "/v1/manager/sessions/"+session.ID, nil)
	closeRequest.Header.Set("X-Aegis-Manager-Session", session.Token)
	response, err = client.Do(closeRequest)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("close status=%d", response.StatusCode)
	}
	_ = response.Body.Close()

	command = managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/commands", map[string]string{"input": "/status"})
	command.Header.Set("X-Aegis-Manager-Session", session.Token)
	response, err = client.Do(command)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d, want 401", response.StatusCode)
	}
	_ = response.Body.Close()

	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagerGatewayParseFailureDoesNotRevokeSession(t *testing.T) {
	svc := apiService(t)
	var logs bytes.Buffer
	svc.Log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)

	opened := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions", map[string]any{})
	response, err := client.Do(opened)
	if err != nil {
		t.Fatal(err)
	}
	var session struct{ ID, Token string }
	if err = json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	for _, step := range []struct {
		input     string
		status    int
		code      string
		prefix    string
		forbidden string
	}{
		{input: "/agents", status: http.StatusBadRequest, code: "manager_command_parse_error", prefix: "usage: /agents "},
		{input: "/agents register", status: http.StatusBadRequest, code: "manager_command_parse_error", prefix: "usage: /agents "},
		{input: "/operator-secret-marker", status: http.StatusBadRequest, code: "manager_command_parse_error", prefix: "invalid manager slash command", forbidden: "operator-secret-marker"},
		{input: "/status", status: http.StatusOK},
	} {
		request := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/commands", map[string]string{"input": step.input})
		request.Header.Set(managergateway.SessionHeader, session.Token)
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != step.status {
			t.Fatalf("%s status=%d want=%d", step.input, response.StatusCode, step.status)
		}
		if step.code != "" {
			var failure envelope
			if err = json.NewDecoder(response.Body).Decode(&failure); err != nil {
				t.Fatal(err)
			}
			if failure.Code != step.code || !strings.HasPrefix(failure.Message, step.prefix) || (step.forbidden != "" && strings.Contains(failure.Message, step.forbidden)) {
				t.Fatalf("unsafe or untyped parse envelope: %+v", failure)
			}
		}
		_ = response.Body.Close()
	}
	if strings.Contains(logs.String(), "operator-secret-marker") {
		t.Fatalf("caller-controlled command leaked into API logs: %s", logs.String())
	}
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestManagerGatewayRejectsMissingSessionBeforeCommandExecution(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)

	request := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/forged/commands", map[string]string{"input": "/agents"})
	response, err := unixClient(svc.Config.API.UnixSocket).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing session status=%d, want 401", response.StatusCode)
	}
	_ = response.Body.Close()
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func managerGatewayRequest(t *testing.T, _ interface{}, method, path string, body any) *http.Request {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request, err := http.NewRequest(method, "http://unix"+path, &payload)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer transport-secret")
	request.Header.Set("Content-Type", "application/json")
	return request
}
