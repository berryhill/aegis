//go:build linux

package localinference

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFreshRevocationBetweenVerificationAndForward(t *testing.T) {
	var upstreamCalls, checks atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Write([]byte(`{"models":[{"name":"exact","digest":"` + strings.Repeat("a", 64) + `"}]}`))
	}))
	defer s.Close()
	p, e := New(core.LocalInference{Kind: "ollama", Endpoint: s.URL, ModelDigest: "sha256:" + strings.Repeat("a", 64)}, "exact", func(context.Context) error {
		if checks.Add(1) > 1 {
			return errors.New("revoked")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if e = p.Bind(os.Getpid()); e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("POST", p.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact","messages":[{"role":"user","content":"plain"}]}`))
	req.Header.Set("Authorization", "Bearer "+CompatibilityToken)
	r, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 403 || upstreamCalls.Load() != 1 || checks.Load() != 2 {
		t.Fatalf("revocation failed status=%d upstream=%d checks=%d", r.StatusCode, upstreamCalls.Load(), checks.Load())
	}
	endpoint := p.Endpoint()
	p.Close()
	if r, e = http.Get(endpoint); e == nil {
		r.Body.Close()
		t.Fatal("proxy survived cleanup")
	}
}
