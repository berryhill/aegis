package manager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
	proxy, err := StartProxy(ctx, ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }})
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
	if status := request(proxy.Token(), "other:1", ""); status != http.StatusForbidden {
		t.Fatalf("alternate model status %d", status)
	}
	canary := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz"
	if status := request(proxy.Token(), "exact:1", `{"model":"exact:1","messages":[{"role":"user","content":"`+canary+`"}]}`); status != http.StatusForbidden {
		t.Fatalf("canary status %d", status)
	}
	if strings.Contains(upstreamBody, canary) {
		t.Fatal("blocked canary reached upstream")
	}
	if status := request(proxy.Token(), "exact:1", ""); status != http.StatusOK {
		t.Fatalf("valid status %d", status)
	}
}

func TestTrustedLocalProxyAllowsPlaintextRequestRejectsEchoAndWipesTracker(t *testing.T) {
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
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, AllowPlaintextRequests: true, Sensitive: tracker})
	if err != nil {
		t.Fatal(err)
	}
	call := func(content string) int {
		body := `{"model":"exact:1","messages":[{"role":"user","content":"` + content + `"}]}`
		req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+proxy.Token())
		response, requestErr := http.DefaultClient.Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := call(canary); status != http.StatusOK {
		t.Fatalf("trusted plaintext request status=%d", status)
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
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, CapabilityExpires: time.Now().Add(time.Minute), ConsumeCapability: func() bool {
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
		req.Header.Set("Authorization", "Bearer "+proxy.Token())
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
	expired, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }, CapabilityExpires: time.Now().Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	defer expired.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, expired.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+expired.Token())
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
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+proxy.Token())
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
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"stream":true,"stream_options":{"include_usage":true}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+proxy.Token())
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
	proxy, err := StartProxy(context.Background(), ProxyConfig{Target: upstream.URL, Model: "exact:1", RouteDigest: "sha256:route", MaximumRequestBytes: 1 << 20, MaximumResponseBytes: 1 << 20, Timeout: time.Second, Guard: guard, SessionActive: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())
	req, _ := http.NewRequest(http.MethodPost, proxy.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact:1","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+proxy.Token())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("malformed stream status=%d", response.StatusCode)
	}
}
