package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/slash"
)

type gatewayRoundTripper func(*http.Request) (*http.Response, error)

func (f gatewayRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestGatewayManagerClientTimeoutCoversSequentialStartup(t *testing.T) {
	cfg := config.Defaults()
	cfg.API.UnixSocket = "/unused/aegis.sock"
	cfg.API.Token = "configured-reference"
	cfg.Manager.Inference.StartTimeout = 2 * time.Minute
	cfg.Manager.Inference.RequestTimeout = 45 * time.Second
	cfg.Manager.Hermes.GatewayStartTimeout = 20 * time.Second
	cfg.Manager.Hermes.TurnTimeout = 4 * time.Minute

	client, err := newGatewayManagerClient(cfg)
	if err != nil {
		t.Fatalf("construct manager gateway client: %v", err)
	}
	want := 2*time.Minute + 3*45*time.Second + 20*time.Second
	if client.http.Timeout < want {
		t.Fatalf("client timeout=%s, want at least %s for sequential manager startup operations", client.http.Timeout, want)
	}
}

func TestRenderGatewayHelpIsHumanReadable(t *testing.T) {
	var output bytes.Buffer
	renderGatewayResult(&output, slash.Result{
		Operation: "manager.help",
		State:     "completed",
		Reason:    "registry_help",
		Data: map[string]any{"commands": []any{
			map[string]any{"available": true, "name": "/status", "usage": "/status"},
			map[string]any{"available": false, "name": "/watch", "usage": "/watch start"},
		}},
	})
	got := output.String()
	if !strings.Contains(got, "Available commands:") || !strings.Contains(got, "/status") || strings.Contains(got, `"schema"`) || strings.Contains(got, "/watch") {
		t.Fatalf("unexpected human help rendering: %q", got)
	}
}

func TestRenderGatewayResultDoesNotDumpTransportEnvelope(t *testing.T) {
	var output bytes.Buffer
	renderGatewayResult(&output, slash.Result{Operation: "manager.status", State: "completed", Reason: "status_snapshot", Data: map[string]any{"runtime": "degraded"}})
	got := output.String()
	if got != "manager.status: completed (status_snapshot)\n  runtime: degraded\n" {
		t.Fatalf("unexpected result rendering: %q", got)
	}
}

func TestRenderGatewayResultSanitizesAndOrdersUntrustedContent(t *testing.T) {
	var output bytes.Buffer
	renderGatewayResult(&output, slash.Result{
		Operation: "manager.status",
		State:     "completed",
		Reason:    "status_snapshot",
		Data: map[string]any{
			"zeta":  "safe",
			"alpha": "\x1b]2;forged-title\x07value\u202ereversed",
		},
	})
	got := output.String()
	if strings.ContainsAny(got, "\x1b\x07") || strings.Contains(got, "\u202e") || strings.Contains(got, "forged-title") {
		t.Fatalf("unsafe terminal content escaped sanitizer: %q", got)
	}
	if strings.Index(got, "alpha:") > strings.Index(got, "zeta:") {
		t.Fatalf("result keys are not deterministic: %q", got)
	}
}

func TestRenderGatewayTurnLabelsAndSanitizesModelText(t *testing.T) {
	var output bytes.Buffer
	renderGatewayTurn(&output, managergateway.TurnResult{Kind: "message", Origin: managergateway.TurnOriginModel, Message: "hello\x1b]2;forged-title\x07 world\u202ereversed"})
	got := output.String()
	if !strings.Contains(got, "Hermes model / untrusted") {
		t.Fatalf("model origin label missing: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\x07") || strings.Contains(got, "\u202e") || strings.Contains(got, "forged-title") {
		t.Fatalf("unsafe model text escaped sanitizer: %q", got)
	}
}

func TestRenderGatewayTurnDistinguishesAuthoritativeAegisResult(t *testing.T) {
	var output bytes.Buffer
	renderGatewayTurn(&output, managergateway.TurnResult{Kind: "hermes_profile_inventory", Origin: managergateway.TurnOriginAuthoritative, Message: "default discovered"})
	got := output.String()
	if !strings.Contains(got, "AEGIS / authoritative") || strings.Contains(got, "Hermes model / untrusted") {
		t.Fatalf("authoritative origin not rendered distinctly: %q", got)
	}
}

func TestDegradedGatewaySubmitsOnlyClosedAuthoritativeTurnIntents(t *testing.T) {
	for input, want := range map[string]bool{
		"show Hermes profiles":                                 true,
		"register the default Hermes profile on this computer": true,
		"hello aegis": false,
		"register it": false,
	} {
		if got := shouldSubmitGatewayTurn("degraded", input); got != want {
			t.Fatalf("input=%q got=%t want=%t", input, got, want)
		}
	}
	if !shouldSubmitGatewayTurn("conversational", "hello aegis") {
		t.Fatal("conversational turns must reach the authenticated gateway")
	}
}

func TestGatewayManagerTurnUsesAuthenticatedConversationEndpoint(t *testing.T) {
	token := strings.Repeat("t", 32)
	client := &gatewayManagerClient{
		http: &http.Client{Transport: gatewayRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodPost || request.URL.Path != "/v1/manager/sessions/mgr-test/turns" {
				t.Fatalf("unexpected conversational request: %s %s", request.Method, request.URL.Path)
			}
			if got := request.Header.Get(managergateway.SessionHeader); got != token {
				t.Fatalf("manager session capability mismatch")
			}
			var body struct {
				Input string `json:"input"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Input != "hello aegis" {
				t.Fatalf("ordinary input=%q", body.Input)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"kind":"message","origin":"model_untrusted","message":"authenticated local reply"}`)), Header: make(http.Header)}, nil
		})},
		transport: strings.Repeat("a", 32),
		sessionID: "mgr-test",
		token:     token,
		mode:      "conversational",
	}

	result, err := client.turn(context.Background(), "hello aegis")
	if err != nil {
		t.Fatal(err)
	}
	if result.Message != "authenticated local reply" || result.Origin != managergateway.TurnOriginModel {
		t.Fatalf("result=%+v", result)
	}
}

func TestGatewayManagerTurnFailsClosedWhenConversationUnavailableOrEmpty(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		body   string
	}{
		"runtime unavailable": {status: http.StatusServiceUnavailable, body: `{"error":"unavailable"}`},
		"empty model reply":   {status: http.StatusOK, body: `{"kind":"message","origin":"model_untrusted","message":"   "}`},
		"unknown origin":      {status: http.StatusOK, body: `{"kind":"message","origin":"forged","message":"claimed"}`},
	} {
		t.Run(name, func(t *testing.T) {
			client := &gatewayManagerClient{
				http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
				})},
				transport: strings.Repeat("a", 32),
				sessionID: "mgr-test",
				token:     strings.Repeat("t", 32),
				mode:      "conversational",
			}
			result, err := client.turn(context.Background(), "hello aegis")
			if err == nil || result.Message != "" {
				t.Fatalf("turn must fail closed: result=%+v err=%v", result, err)
			}
		})
	}
}
