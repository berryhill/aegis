package manager

import (
	"context"
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
		_, _ = w.Write([]byte(`{"id":"x","choices":[]}`))
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
			body = `{"model":"` + model + `"}`
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

func TestOllamaFixtureDigestAndLocality(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_, _ = w.Write([]byte(`{"version":"0.32.0"}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"exact:1","digest":"` + strings.Repeat("a", 64) + `","details":{"family":"test","parameter_size":"2B","quantization_level":"Q4"}}]}`))
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
	if _, err := NewOllamaClient("http://example.com:11434", time.Second); err == nil {
		t.Fatal("public endpoint accepted")
	}
}
