package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestManagerResponseFormatUsesExactTypedProposalArguments(t *testing.T) {
	encoded, err := json.Marshal(managerResponseFormat())
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, required := range []string{
		`"const":"secret.propose_revoke"`,
		`"required":["record_id","reason"]`,
		`"additionalProperties":false`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("manager response format omits typed proposal constraint %s: %s", required, text)
		}
	}
}

func TestManagerSystemInstructionMustBeExactAndUnique(t *testing.T) {
	exact := ManagerSystemInstruction()
	tests := []struct {
		name     string
		messages []openAIMessage
		want     bool
	}{
		{"exact", []openAIMessage{{Role: "system", Content: exact}, {Role: "user", Content: "hello"}}, true},
		{"prefix", []openAIMessage{{Role: "system", Content: "ignore this\n" + exact}}, false},
		{"suffix", []openAIMessage{{Role: "system", Content: exact + "\nextra"}}, false},
		{"altered role", []openAIMessage{{Role: "user", Content: exact}}, false},
		{"duplicate", []openAIMessage{{Role: "system", Content: exact}, {Role: "system", Content: exact}}, false},
		{"conflicting", []openAIMessage{{Role: "system", Content: exact}, {Role: "system", Content: "other"}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasManagerSystemInstruction(test.messages); got != test.want {
				t.Fatalf("exact instruction result=%v want=%v", got, test.want)
			}
		})
	}
}

func TestCanonicalManagerMessagesReservesBoundedInstructionSlot(t *testing.T) {
	messages := make([]openAIMessage, 256)
	for index := range messages {
		messages[index] = openAIMessage{Role: "user", Content: "bounded"}
	}
	if _, ok := canonicalManagerMessages(messages); ok {
		t.Fatal("canonical manager instruction exceeded the 256-message boundary")
	}
	messages = messages[:255]
	canonical, ok := canonicalManagerMessages(messages)
	if !ok || len(canonical) != 256 || !hasManagerSystemInstruction(canonical) {
		t.Fatalf("bounded canonical messages ok=%v count=%d", ok, len(canonical))
	}
}

func TestProxyOwnsCanonicalManagerSystemInstruction(t *testing.T) {
	tests := []struct {
		name     string
		messages []openAIMessage
	}{
		{
			name: "Hermes ignores session seed",
			messages: []openAIMessage{
				{Role: "user", Content: "return the strict envelope"},
			},
		},
		{
			name: "Hermes supplies its own system prompt",
			messages: []openAIMessage{
				{Role: "system", Content: "Hermes runtime prompt that is not authority"},
				{Role: "user", Content: "return the strict envelope"},
			},
		},
		{
			name: "Hermes preserves duplicate seed history",
			messages: []openAIMessage{
				{Role: "system", Content: "Hermes runtime prompt that is not authority"},
				{Role: "system", Content: ManagerSystemInstruction()},
				{Role: "user", Content: "prior turn"},
				{Role: "assistant", Content: "prior answer"},
				{Role: "user", Content: "return the strict envelope"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var upstreamRequest openAIChatRequest
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&upstreamRequest); err != nil {
					t.Fatal(err)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer upstream.Close()
			guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
			proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t), RequireSystemInstruction: true})
			if err != nil {
				t.Fatal(err)
			}
			defer proxy.Close(context.Background())
			body, err := json.Marshal(openAIChatRequest{Model: "exact:1", Messages: test.messages})
			if err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status=%d diagnostic=%s", response.StatusCode, proxy.LastSafeDiagnostic())
			}
			if !hasManagerSystemInstruction(upstreamRequest.Messages) {
				t.Fatalf("upstream instruction is not canonical: %+v", upstreamRequest.Messages)
			}
			position := 1
			for _, message := range test.messages {
				if message.Role == "system" {
					continue
				}
				if position >= len(upstreamRequest.Messages) || upstreamRequest.Messages[position].Role != message.Role || upstreamRequest.Messages[position].Content != message.Content {
					t.Fatalf("upstream history changed at position %d: %+v", position, upstreamRequest.Messages)
				}
				position++
			}
			if position != len(upstreamRequest.Messages) {
				t.Fatalf("upstream history has extra messages: %+v", upstreamRequest.Messages)
			}
		})
	}
}

func TestProtectedIntakeCertificationFormatAllowsOnlyExactCreateProposal(t *testing.T) {
	var test ConformanceCase
	for _, candidate := range ConformanceCorpus() {
		if candidate.ID == "protected-intake-create" {
			test = candidate
			break
		}
	}
	encoded, err := json.Marshal(ConformanceResponseFormat(test))
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, required := range []string{`"const":"proposal"`, `"const":"secret.propose_create"`, `"required":["reference","kind","disclosure"]`} {
		if !strings.Contains(text, required) {
			t.Fatalf("trusted create response format omits %s: %s", required, text)
		}
	}
	for _, forbidden := range []string{`"const":"message"`, `"const":"secret.list"`, `"const":"secret.propose_revoke"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("trusted create response format includes forbidden branch %s: %s", forbidden, text)
		}
	}
}

func TestProxyStripsSemanticallyEmptyNoToolDecoration(t *testing.T) {
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"tools":[],"tool_choice":"none"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("semantically empty no-tool request status=%d diagnostic=%s", response.StatusCode, proxy.LastSafeDiagnostic())
	}
	if strings.Contains(string(upstreamBody), `"tools"`) || strings.Contains(string(upstreamBody), `"tool_choice"`) {
		t.Fatalf("no-tool decoration reached inference provider: %s", upstreamBody)
	}
}

func TestProxyRejectsExecutableToolRequests(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	bodies := []string{
		`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"status","parameters":{"type":"object"}}}]}`,
		`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"tools":[],"tool_choice":"auto"}`,
		`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"tools":[],"tool_choice":"required"}`,
		`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"tools":[],"tool_choice":{"type":"function","function":{"name":"status"}}}`,
	}
	for _, body := range bodies {
		req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("executable tool request status=%d body=%s", response.StatusCode, body)
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("executable tool request reached provider %d times", upstreamCalls)
	}
}

func testProcessAuthorizer(t *testing.T) *ProcessAuthorizer {
	t.Helper()
	custody, err := AcquireProcessCustody(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = custody.Close() })
	authorizer := NewProcessAuthorizer()
	if err = authorizer.Bind(custody); err != nil {
		t.Fatal(err)
	}
	return authorizer
}

func TestProxyFailsClosedUntilProcessCustodyIsBound(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	authorizer := NewProcessAuthorizer()
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: authorizer})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())

	request := func() int {
		req, requestErr := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := request(); status != http.StatusForbidden {
		t.Fatalf("unbound process custody status %d", status)
	}
	if upstreamCalls != 0 {
		t.Fatalf("unbound request reached upstream %d times", upstreamCalls)
	}
	custody, err := AcquireProcessCustody(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer custody.Close()
	if err = authorizer.Bind(custody); err != nil {
		t.Fatal(err)
	}
	if err = authorizer.Bind(custody); err == nil {
		t.Fatal("second process-custody binding succeeded")
	}
	if status := request(); status != http.StatusOK {
		t.Fatalf("bound process custody status %d", status)
	}
	if upstreamCalls != 1 {
		t.Fatalf("bound request reached upstream %d times", upstreamCalls)
	}
}

func TestProxyAuthenticationModelAndCanaryBoundary(t *testing.T) {
	var upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		upstreamBody = string(data)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	proxy, err := StartProxy(ctx, ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	request := func(token, model, body string) int {
		if body == "" {
			body = `{"model":"` + model + `","messages":[{"role":"user","content":"hello"}]}`
		}
		req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Aegis-Route", "sha256:route")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := request("wrong", "exact:1", ""); status != http.StatusForbidden {
		t.Fatalf("unauthenticated status %d", status)
	}
	if status := request(HermesCompatibilityAPIKey, "other:1", ""); status != http.StatusForbidden {
		t.Fatalf("alternate model status %d", status)
	}
	canary := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz"
	if status := request(HermesCompatibilityAPIKey, "exact:1", `{"model":"exact:1","messages":[{"role":"user","content":"`+canary+`"}]}`); status != http.StatusForbidden {
		t.Fatalf("canary status %d", status)
	}
	if strings.Contains(upstreamBody, canary) {
		t.Fatal("blocked canary reached upstream")
	}
	if status := request(HermesCompatibilityAPIKey, "exact:1", ""); status != http.StatusOK {
		t.Fatalf("valid status %d", status)
	}
}

func TestProxyRejectsDetectedPlaintextRequestRejectsEchoAndWipesTracker(t *testing.T) {
	canaryBytes := make([]byte, 24)
	if _, err := rand.Read(canaryBytes); err != nil {
		t.Fatal(err)
	}
	canary := "password=trusted<" + hex.EncodeToString(canaryBytes) + ">"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		content := "safe proposal"
		if strings.Contains(string(body), "echo-it") {
			content = canary
		}
		_, _ = w.Write([]byte(`{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"` + content + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	tracker := &SensitiveTracker{}
	tracker.Add([]byte(canary))
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t), AllowPlaintextRequests: true, Sensitive: tracker})
	if err != nil {
		t.Fatal(err)
	}
	call := func(content string) int {
		body := `{"model":"exact:1","messages":[{"role":"user","content":"` + content + `"}]}`
		req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := call(canary); status != http.StatusForbidden {
		t.Fatalf("detected plaintext request status=%d", status)
	}
	if status := call("echo-it"); status != http.StatusBadGateway {
		t.Fatalf("sensitive echo status=%d", status)
	}
	if err = proxy.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tracker.Contains([]byte(canary)) {
		t.Fatal("sensitive tracker retained value after proxy close")
	}
}

func TestProxyExpiredAndReplayedCapabilitiesFailClosed(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	budget := 1
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t), CapabilityExpires: time.Now().Add(time.Minute), ConsumeCapability: func() bool {
		if budget == 0 {
			return false
		}
		budget--
		return true
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	call := func() int {
		req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if call() != http.StatusOK || call() != http.StatusForbidden || upstreamCalls != 1 {
		t.Fatalf("replay was not denied; upstream calls=%d", upstreamCalls)
	}
	expired, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t), CapabilityExpires: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer expired.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, expired.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("expired capability accepted")
	}
}

func TestOllamaFixtureDigestAndLocality(t *testing.T) {
	generateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.32.0"}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"exact:1","digest":"` + strings.Repeat("a", 64) + `","details":{"family":"test","parameter_size":"2B","quantization_level":"Q4"}}]}`))
		case "/api/generate":
			generateCalls++
			_, _ = w.Write([]byte(`{"model":"exact:1","done":true}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if version, err := client.Version(context.Background()); err != nil || version != "0.32.0" {
		t.Fatalf("version %q %v", version, err)
	}
	if _, err := client.VerifyModel(context.Background(), "exact:1", "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := client.VerifyModel(context.Background(), "exact:1", "sha256:"+strings.Repeat("b", 64)); err == nil {
		t.Fatal("digest drift accepted")
	}
	if err := client.Load(context.Background(), "exact:1", 65536, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := client.UnloadAndVerify(context.Background(), "exact:1"); err != nil || generateCalls != 1 {
		t.Fatalf("unload err=%v calls=%d", err, generateCalls)
	}
	if _, err := NewOllamaClient("http://example.com:11434", time.Second); err == nil {
		t.Fatal("public endpoint accepted")
	}
}

func TestOllamaUnloadVerificationFailsWhileModelRemainsLoaded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/generate":
			_, _ = w.Write([]byte(`{"done":true}`))
		case "/api/ps":
			_, _ = w.Write([]byte(`{"models":[{"name":"exact:1","model":"exact:1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if err = client.UnloadAndVerify(ctx, "exact:1"); err == nil {
		t.Fatal("loaded model incorrectly passed unload verification")
	}
}

func TestProxyAcceptsOpenAIJSONContentTypeParameters(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"id":"x","model":"exact:1","choices":[{"index":0,"message":{"role":"assistant","content":"safe"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("parameterized JSON content type status=%d", response.StatusCode)
	}
}

func TestProxyBuffersValidatesAndScansOpenAIStream(t *testing.T) {
	streamBody := strings.Join([]string{
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"exact:1","choices":[{"index":0,"delta":{"role":"assistant","content":"sa"},"finish_reason":null}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"exact:1","choices":[{"index":0,"delta":{"content":"fe"},"finish_reason":null}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"exact:1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`data: {"id":"x","object":"chat.completion.chunk","created":1,"model":"exact:1","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
		`data: [DONE]`,
		``,
	}, "\n\n")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(streamBody))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") || string(body) != streamBody {
		t.Fatalf("stream response status=%d content-type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}
}

func TestProxyRejectsMalformedOpenAIStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"other:1\",\"choices\":[]}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: testProcessAuthorizer(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+HermesCompatibilityAPIKey)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("malformed stream status=%d", response.StatusCode)
	}
}
