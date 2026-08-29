package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
)

func TestManagerGatewayWriteTimeoutCoversStartupAndTurns(t *testing.T) {
	cfg := config.Defaults()
	cfg.API.WriteTimeout = 30 * time.Second
	cfg.Manager.Inference.StartTimeout = 2 * time.Minute
	cfg.Manager.Inference.RequestTimeout = 45 * time.Second
	cfg.Manager.Hermes.GatewayStartTimeout = 20 * time.Second
	cfg.Manager.Hermes.TurnTimeout = 4 * time.Minute

	got := managerGatewayWriteTimeout(cfg)
	if got < 4*time.Minute {
		t.Fatalf("write timeout=%s, want coverage for longest manager operation", got)
	}
}

func TestManagerGatewaySessionExecutesThroughSoleServiceAndRevokes(t *testing.T) {
	svc := apiService(t)
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

	turn := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/"+session.ID+"/turns", map[string]string{"input": "hello"})
	turn.Header.Set("X-Aegis-Manager-Session", session.Token)
	response, err = client.Do(turn)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("degraded turn status=%d, want 503", response.StatusCode)
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

func TestManagerGatewayRejectsMissingSessionBeforeCommandExecution(t *testing.T) {
	svc := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	waitFor(t, "unix", svc.Config.API.UnixSocket)

	request := managerGatewayRequest(t, svc, http.MethodPost, "/v1/manager/sessions/forged/commands", map[string]string{"input": "/status"})
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
