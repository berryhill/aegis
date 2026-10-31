package managergateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

// This opt-in regression uses installed Hermes, real history retention, the
// production process-bound proxy and canonical Badger readback. Only inference
// responses are synthetic: this is NOT model certification or live acceptance.
func TestPlatformConversationInstalledHermesHistory(t *testing.T) {
	installation := os.Getenv("AEGIS_TEST_HERMES_INSTALLATION")
	root := os.Getenv("AEGIS_TEST_GATEWAY_ROOT")
	port, err := strconv.Atoi(os.Getenv("AEGIS_ACCEPTANCE_PORT"))
	if installation == "" || root == "" {
		t.Skip("installed Hermes and isolated test root required")
	}
	if err != nil || port < 8450 || port > 8469 {
		t.Fatal("shared lifetime candidate-port claim required")
	}
	if !filepath.IsAbs(installation) || !filepath.IsAbs(root) {
		t.Fatal("absolute isolated paths required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	a, _, sub, _, _ := contributionFixture(t)
	producer := &platformProposalModel{}
	producer.propose(t, "agents.list", map[string]any{})
	var requests atomic.Int32
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		chunk := map[string]any{"id": "synthetic-platform", "object": "chat.completion.chunk", "created": 1, "model": "synthetic-proposal-only", "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"role": "assistant", "content": string(producer.response)}, "finish_reason": nil}}}
		encoded, _ := json.Marshal(chunk)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"id\":\"synthetic-platform\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"synthetic-proposal-only\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", encoded)
	}))
	upstream.Listener.Close()
	upstream.Listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	upstream.Start()
	defer upstream.Close()
	ingress := a.Config.Manager.Ingress
	guard, err := managerdomain.NewGuard(int(ingress.MaximumMessageBytes), ingress.MaximumMessageRunes, ingress.BoundedDecodeDepth, ingress.ScanTimeout)
	if err != nil {
		t.Fatal(err)
	}
	armed := &armedGateway{sensitive: &managerdomain.SensitiveTracker{}}
	authorizer := managerdomain.NewProcessAuthorizer()
	proxy, err := managerdomain.StartProxy(ctx, managerdomain.ProxyConfig{Target: upstream.URL, Model: "synthetic-proposal-only", RouteDigest: "synthetic-platform", MaximumRequestBytes: 4 << 20, MaximumResponseBytes: 4 << 20, Timeout: 30 * time.Second, Guard: guard, SessionActive: func() bool { return true }, ProcessAuthorizer: authorizer, CapabilityExpires: sub.ExpiresAt, ConsumeCapability: armed.consume, RequireSystemInstruction: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := proxy.Close(cleanup); err != nil {
			t.Error(err)
		}
	}()
	process, err := managerdomain.StartHermesProcess(ctx, managerdomain.HermesProcessConfig{Python: filepath.Join(installation, "venv", "bin", "python"), Installation: installation, StateRoot: root, ProxyEndpoint: proxy.Endpoint(), Model: "synthetic-proposal-only", MaximumMessageBytes: 1 << 20, StartTimeout: 20 * time.Second, AuthorizeRelease: authorizer.Bind})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := process.Close(cleanup); err != nil {
			t.Error(err)
		}
	}()
	armed.client = process.Client()
	session, err := armed.client.CreateSession(ctx, "aegis-platform-history-synthetic-test")
	if err != nil {
		t.Fatal(err)
	}
	g, id, token := platformGateway(t, a, sub, producer)
	owned := g.sessions[id]
	owned.runtime = platformRuntime(t, a, sub, session, armed)
	g.sessions[id] = owned
	for turn := 1; turn <= 4; turn++ {
		result, err := g.Turn(ctx, sub, id, token, "Which registered participants can I use?")
		if err != nil {
			t.Fatalf("turn %d: %v; proxy=%s; upstream requests=%d", turn, err, proxy.LastSafeDiagnostic(), requests.Load())
		}
		if result.Origin != TurnOriginAuthoritative || !strings.Contains(result.Message, "contribution-controller") {
			t.Fatalf("turn %d lacks canonical readback: %+v", turn, result)
		}
		t.Logf("turn=%d authoritative=true upstream_requests=%d proxy=%s", turn, requests.Load(), proxy.LastSafeDiagnostic())
	}
	if requests.Load() != 4 {
		t.Fatalf("unexpected inference requests: %d", requests.Load())
	}
}
