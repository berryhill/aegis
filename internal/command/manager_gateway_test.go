package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/slash"
	"github.com/spf13/cobra"
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
	cfg.Manager.CleanupTimeout = 7 * time.Minute

	client, err := newGatewayManagerClient(cfg)
	if err != nil {
		t.Fatalf("construct manager gateway client: %v", err)
	}
	serverWriteDeadline := cfg.Manager.CleanupTimeout + 5*time.Second
	if client.http.Timeout <= serverWriteDeadline {
		t.Fatalf("client timeout=%s, must exceed server write deadline=%s", client.http.Timeout, serverWriteDeadline)
	}
}

func TestGatewayManagerCommandAcceptsOnlyTypedParseFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		recover bool
	}{
		{name: "typed parse", status: http.StatusBadRequest, body: `{"code":"manager_command_parse_error","message":"usage: /agents readiness","request_id":"opaque"}`, recover: true},
		{name: "unknown code", status: http.StatusBadRequest, body: `{"code":"future_parse_error","message":"usage: forged","request_id":"opaque"}`},
		{name: "malformed", status: http.StatusBadRequest, body: `{"code":`},
		{name: "generic bad request", status: http.StatusBadRequest, body: `{"code":"invalid_request","message":"Bad Request"}`},
		{name: "oversized", status: http.StatusBadRequest, body: `{"code":"manager_command_parse_error","message":"usage: /agents readiness"}` + strings.Repeat(" ", (64<<10)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &gatewayManagerClient{http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}, transport: "transport", sessionID: "mgr-test", token: "capability"}
			_, err := client.execute(context.Background(), "/agents")
			if err == nil || isManagerCommandParseError(err) != test.recover {
				t.Fatalf("err=%v recover=%t want=%t", err, isManagerCommandParseError(err), test.recover)
			}
			if test.recover && (!strings.Contains(err.Error(), "usage: /agents readiness") || strings.Contains(err.Error(), "opaque")) {
				t.Fatalf("unsafe parse rendering: %v", err)
			}
		})
	}
}

func TestGatewayManagerLoopKeepsDeterministicControlsAfterRuntimeFailure(t *testing.T) {
	client := &gatewayManagerClient{
		mode: "conversational", transport: "transport", sessionID: "mgr-test", token: "capability",
		http: &http.Client{Transport: gatewayRoundTripper(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/v1/manager/sessions/mgr-test/turns":
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(`{"code":"manager_runtime_unavailable","message":"manager conversation runtime is unavailable"}`)), Header: make(http.Header)}, nil
			case "/v1/manager/sessions/mgr-test/commands":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"schema":"manager-command-result-v1","operation":"manager.status","state":"completed","reason":"status_snapshot","data":{"runtime":"degraded"}}`)), Header: make(http.Header)}, nil
			default:
				t.Fatalf("unexpected manager path %q", request.URL.Path)
				return nil, errors.New("unexpected manager path")
			}
		})},
	}
	cfg := config.Defaults()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("hello\n/status\n/exit\n"))
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := runGatewayManagerLoop(cmd, cfg, client, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if client.mode != "degraded" || client.reason != "manager_runtime_failed" {
		t.Fatalf("client did not retain a degraded authenticated session: mode=%q reason=%q", client.mode, client.reason)
	}
	got := output.String()
	if !strings.Contains(got, "conversational runtime degraded") || !strings.Contains(got, "manager.status: completed") {
		t.Fatalf("deterministic command was not executed after runtime failure: %q", got)
	}
}

func TestGatewayManagerLoopKeepsSessionAfterIncompleteAgentsCommand(t *testing.T) {
	commands := 0
	client := &gatewayManagerClient{
		mode: "degraded", reason: "manager_model_absent", nextStep: "configure a model",
		transport: "transport", sessionID: "mgr-test", token: "capability",
		http: &http.Client{Transport: gatewayRoundTripper(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/manager/sessions/mgr-test/commands" {
				t.Fatalf("unexpected manager path %q", request.URL.Path)
			}
			commands++
			if commands == 1 {
				return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader(`{"code":"manager_command_parse_error","message":"usage: /agents readiness|list|show|prepare|register","request_id":"opaque"}`)), Header: make(http.Header)}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"schema":"manager-command-result-v1","operation":"manager.status","state":"completed","reason":"status_snapshot","data":{"runtime":"degraded"}}`)), Header: make(http.Header)}, nil
		})},
	}
	cfg := config.Defaults()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(strings.NewReader("/agents register\n/status\n/exit\n"))
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := runGatewayManagerLoop(cmd, cfg, client, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if commands != 2 || !strings.Contains(output.String(), "usage: /agents") || !strings.Contains(output.String(), "manager.status: completed") {
		t.Fatalf("incomplete /agents command did not recover in-session: calls=%d output=%q", commands, output.String())
	}
}

func TestGatewayManagerClientCloseKeepsCapabilityUntilDefinitiveResponse(t *testing.T) {
	calls := 0
	client := &gatewayManagerClient{http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}, transport: "transport", sessionID: "mgr-test", token: "capability"}
	if err := client.close(context.Background()); err == nil {
		t.Fatal("close transport failure was discarded")
	}
	if client.sessionID == "" || client.token == "" {
		t.Fatal("capability cleared before definitive close response")
	}
	if err := client.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.sessionID != "" || client.token != "" {
		t.Fatal("capability retained after definitive close response")
	}
}

func TestDeferredGatewayManagerCloseJoinsErrors(t *testing.T) {
	runErr := errors.New("interactive failure")
	client := &gatewayManagerClient{http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}, transport: "transport", sessionID: "mgr-test", token: "capability"}
	err := closeGatewayManager(runErr, client, time.Millisecond)
	if !errors.Is(err, runErr) || !strings.Contains(err.Error(), "session close failed closed") {
		t.Fatalf("deferred close errors not joined: %v", err)
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

func TestManagerBannerDiscoversDeterministicAgentControls(t *testing.T) {
	got := gatewayManagerCommandSummary()
	for _, required := range []string{"/agents readiness", "/agents list", "/agents show", "/agents prepare", "/agents register", "/help agents"} {
		if !strings.Contains(got, required) {
			t.Errorf("manager banner command summary missing %q: %q", required, got)
		}
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
		"show Hermes profiles":                                           true,
		"register the default Hermes profile on this computer":           true,
		"i want to register an agent":                                    true,
		"hey, let's register an agent":                                   true,
		"how do I install the Aegis skills in Hermes?":                   true,
		"does our registered Hermes agent know how to use Aegis?":        true,
		"does our registers hermes agent know how to use aegis/":         true,
		"can you change the name of the default Hermes agent to javi?":   true,
		"how many agents have we registered?":                            true,
		"which agents are registered?":                                   true,
		"show agent agent-alpha revision 2":                              true,
		"what secrets do I have?":                                        true,
		"how many credentials do I have?":                                true,
		"find credentials matching build":                                true,
		"show the value for credential build-token":                      true,
		"Can you register agent alpha?":                                  true,
		"can we register an agent?":                                      true,
		"how many agents are resistered?":                                true,
		"I'd like to register a new agent.":                              true,
		"can you ensure our aegis gateway and dashboard are up to date?": true,
		"Please ensure the Aegis manager is running and up to date.":     true,
		"Please update and restart the Aegis manager.":                   true,
		"what about now?":                                                false,
		"hello aegis":                                                    false,
		"register it":                                                    false,
		"Please update the Aegis manager and delete its state.":          false,
	} {
		if got := shouldSubmitGatewayTurn("degraded", input); got != want {
			t.Fatalf("input=%q got=%t want=%t", input, got, want)
		}
	}
	if !shouldSubmitGatewayTurn("conversational", "hello aegis") {
		t.Fatal("conversational turns must reach the authenticated gateway")
	}
}

func TestKnownManagerDeadlineClassifiesOnlyLateAuthorizationFailureAsExpiry(t *testing.T) {
	expires := time.Date(2026, 9, 2, 17, 5, 0, 0, time.UTC)
	authErr := &managerAuthorizationError{action: "conversational turn", status: http.StatusUnauthorized}

	late := classifyManagerSessionError(authErr, expires, expires)
	var expired *managerSessionExpiredError
	if !errors.As(late, &expired) || !strings.Contains(late.Error(), "expired normally") {
		t.Fatalf("late authorization failure was not typed as expiry: %v", late)
	}
	early := classifyManagerSessionError(authErr, expires, expires.Add(-time.Second))
	if errors.As(early, &expired) || early != authErr {
		t.Fatalf("early authorization failure was mislabeled as expiry: %v", early)
	}
	wrappedDeadline := fmt.Errorf("manager gateway connection interrupted: %w", context.DeadlineExceeded)
	lateDeadline := classifyManagerSessionError(wrappedDeadline, expires, expires)
	if !errors.As(lateDeadline, &expired) {
		t.Fatalf("in-flight deadline was not classified as normal expiry: %v", lateDeadline)
	}
	earlyDeadline := classifyManagerSessionError(wrappedDeadline, expires, expires.Add(-time.Second))
	if errors.As(earlyDeadline, &expired) {
		t.Fatalf("early request timeout was mislabeled as session expiry: %v", earlyDeadline)
	}
}

func TestGatewayManagerLoopWakesIdlePromptAtExpiry(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	client := &gatewayManagerClient{mode: "conversational"}
	cfg := config.Defaults()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(reader)
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	expires := time.Now().Add(30 * time.Millisecond)
	if err := runGatewayManagerLoop(cmd, cfg, client, expires); err != nil {
		t.Fatalf("normal idle expiry returned error: %v", err)
	}
	if !strings.Contains(output.String(), "manager session expired normally") {
		t.Fatalf("normal idle expiry was not rendered: %q", output.String())
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

func TestGatewayManagerTurnAcceptsOnlyTypedAuthoritativeSensitiveValue(t *testing.T) {
	client := &gatewayManagerClient{
		http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"kind":"credential_value","origin":"aegis_authoritative","message":"Credential value: canary","sensitive":true}`)), Header: make(http.Header)}, nil
		})},
		transport: strings.Repeat("a", 32),
		sessionID: "mgr-test",
		token:     strings.Repeat("t", 32),
	}
	result, err := client.turn(context.Background(), "show the value for credential test")
	if err != nil || !result.Sensitive || result.Kind != "credential_value" || result.Origin != managergateway.TurnOriginAuthoritative {
		t.Fatalf("typed sensitive result rejected: result=%+v err=%v", result, err)
	}
}

func TestGatewayManagerTurnFailsClosedWhenConversationUnavailableOrEmpty(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		body   string
	}{
		"runtime unavailable":  {status: http.StatusServiceUnavailable, body: `{"error":"unavailable"}`},
		"empty model reply":    {status: http.StatusOK, body: `{"kind":"message","origin":"model_untrusted","message":"   "}`},
		"unknown origin":       {status: http.StatusOK, body: `{"kind":"message","origin":"forged","message":"claimed"}`},
		"sensitive model":      {status: http.StatusOK, body: `{"kind":"message","origin":"model_untrusted","message":"claimed","sensitive":true}`},
		"sensitive wrong kind": {status: http.StatusOK, body: `{"kind":"credential_list","origin":"aegis_authoritative","message":"claimed","sensitive":true}`},
		"value missing marker": {status: http.StatusOK, body: `{"kind":"credential_value","origin":"aegis_authoritative","message":"claimed"}`},
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

func TestGatewayManagerTurnRendersTypedFailureRemediation(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		code      string
		want      string
		notWanted string
	}{
		{name: "runtime unavailable", status: http.StatusServiceUnavailable, code: "manager_runtime_unavailable", want: "conversation runtime is unavailable; run 'aegis manager model status'; authenticated gateway commands may still be available", notWanted: "manager gateway unavailable"},
		{name: "authority unavailable", status: http.StatusServiceUnavailable, code: "manager_authority_unavailable", want: "credential authority is unavailable; restore or unlock the local authority", notWanted: "model status"},
		{name: "authority invalid", status: http.StatusServiceUnavailable, code: "manager_authority_invalid", want: "credential authority is invalid; repair the local authority configuration", notWanted: "model status"},
		{name: "turn timeout", status: http.StatusGatewayTimeout, code: "manager_turn_timeout", want: "conversational turn timed out; deterministic controls remain available, but further model turns require a fresh 'aegis manager' session", notWanted: "manager gateway unavailable"},
		{name: "protocol failure", status: http.StatusBadGateway, code: "manager_turn_protocol_error", want: "conversation runtime protocol failed; rerun 'aegis manager' to establish a fresh runtime session", notWanted: "manager gateway unavailable"},
		{name: "internal failure", status: http.StatusInternalServerError, code: "manager_turn_internal_error", want: "conversational turn failed internally; inspect local gateway logs, then start a fresh 'aegis manager' session", notWanted: "manager gateway unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &gatewayManagerClient{
				http: &http.Client{Transport: gatewayRoundTripper(func(*http.Request) (*http.Response, error) {
					body := fmt.Sprintf(`{"code":%q,"message":"safe","request_id":"request-only"}`, test.code)
					return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})},
				transport: strings.Repeat("a", 32), sessionID: "mgr-test", token: strings.Repeat("t", 32), mode: "conversational",
			}
			_, err := client.turn(context.Background(), "do not reflect this prompt")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("typed remediation missing: %v", err)
			}
			if strings.Contains(err.Error(), test.notWanted) || strings.Contains(err.Error(), "do not reflect") || strings.Contains(err.Error(), "request-only") {
				t.Fatalf("unsafe or misleading turn error: %v", err)
			}
		})
	}
}

func TestGatewayManagerTurnPreservesAuthorizationStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict} {
		response := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"code":"manager_runtime_unavailable"}`))}
		err := managerGatewayTurnError(response)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
			t.Fatalf("status %d was not preserved: %v", status, err)
		}
	}
}

func TestGatewayManagerTurnRejectsUnknownOrMismatchedTypedFailures(t *testing.T) {
	for name, response := range map[string]struct {
		status int
		code   string
	}{
		"unknown code":    {status: http.StatusServiceUnavailable, code: "manager_future_failure"},
		"mismatched pair": {status: http.StatusInternalServerError, code: "manager_runtime_unavailable"},
		"missing code":    {status: http.StatusServiceUnavailable, code: ""},
	} {
		t.Run(name, func(t *testing.T) {
			httpResponse := &http.Response{StatusCode: response.status, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"code":%q,"message":"private diagnostic","request_id":"private-request"}`, response.code)))}
			err := managerGatewayTurnError(httpResponse)
			if err == nil || !strings.Contains(err.Error(), "failed closed") {
				t.Fatalf("invalid pair did not fail conservatively: %v", err)
			}
			if strings.Contains(err.Error(), "runtime is unavailable") || strings.Contains(err.Error(), "private") {
				t.Fatalf("invalid pair was trusted or leaked: %v", err)
			}
		})
	}
}
