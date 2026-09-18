//go:build linux

package hermes

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Synthetic gateway process, real loopback transport and session lifecycle.
// This is not installed-Hermes or live-model acceptance.
func TestLocalSessionLaunchBindingAndTermination(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "positive", true: "runtime-drift"}[mismatch], func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					io.WriteString(w, `{"models":[{"name":"exact:1","digest":"`+strings.Repeat("a", 64)+`"}]}`)
					return
				}
				io.Copy(io.Discard, r.Body)
				close(entered)
				<-r.Context().Done()
				close(stopped)
			}))
			defer s.Close()
			a, root := attemptTestAdapter(t, `#!/usr/bin/python3
import os,sys,json,urllib.request
assert os.environ['HERMES_TUI_TOOLSETS']=='context_engine'
assert os.environ['HERMES_SAFE_MODE']=='1'
assert os.environ['HERMES_IGNORE_USER_CONFIG']=='1'
assert os.environ['HERMES_TUI_MODEL']==os.environ['HERMES_MODEL']=='exact:1'
assert 'HTTP_PROXY' not in os.environ
assert 'ANTHROPIC_API_KEY' not in os.environ
print(json.dumps({'method':'event','params':{'type':'gateway.ready'}}),flush=True)
req=urllib.request.Request(os.environ['OPENROUTER_BASE_URL']+'/chat/completions',data=json.dumps({'model':'exact:1','messages':[{'role':'user','content':'hi'}]}).encode(),headers={'Authorization':'Bearer '+os.environ['OPENROUTER_API_KEY']})
try: urllib.request.urlopen(req).read()
except Exception: pass
for line in sys.stdin: pass
`)
			r := validAttemptTurnRequest(root)
			h := core.HermesConfig{Model: "exact:1", Provider: "ollama", LocalInference: &core.LocalInference{Kind: "ollama", Endpoint: s.URL, ModelDigest: "sha256:" + strings.Repeat("a", 64)}}
			r.Launch.Mandate.Hermes = h
			r.Launch.Mandate.Tools = nil
			r.Launch.AuthorityContext.Authority.Hermes = h
			r.Launch.AuthorityContext.Authority.Tools = nil
			if mismatch {
				r.Launch.Mandate.Runtime.Version = "0.18.1"
				r.Launch.AuthorityContext.Runtime.Version = "0.18.1"
			}
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			id, _, pid, _, e := a.Launch(context.Background(), r.StateRoot, r.Launch.Mandate, r.Launch.AuthorityContext, nil, BrokerBridge{}, func(context.Context) error { return nil })
			if mismatch {
				if e == nil || !strings.Contains(e.Error(), "runtime binding") {
					t.Fatalf("drift accepted: %v", e)
				}
				return
			}
			if e != nil || pid <= 0 {
				t.Fatalf("launch failed: %v pid=%d", e, pid)
			}
			defer a.Terminate(context.Background(), id, true)
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("generation not reached")
			}
			if e = a.Terminate(context.Background(), id, true); e != nil {
				t.Fatal(e)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("upstream survived termination")
			}
		})
	}
}
