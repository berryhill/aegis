//go:build linux

package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/core"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalAttemptRealHTTPAndCleanup(t *testing.T) {
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprint(drift), func(t *testing.T) {
			var calls atomic.Int32
			digest := "sha256:" + strings.Repeat("a", 64)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					d := digest
					if drift {
						d = "sha256:" + strings.Repeat("b", 64)
					}
					json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]string{"name": "exact:1", "digest": d}}})
					return
				}
				calls.Add(1)
				var q map[string]any
				json.NewDecoder(r.Body).Decode(&q)
				if q["model"] != "exact:1" || q["response_format"] != nil || q["tools"] != nil {
					t.Error("route/prompt policy changed")
				}
				json.NewEncoder(w).Encode(map[string]any{"id": "answer", "object": "chat.completion", "model": "exact:1", "choices": []any{map[string]any{"index": 0, "message": map[string]string{"role": "assistant", "content": "ordinary answer"}, "finish_reason": "stop"}}})
			}))
			defer upstream.Close()
			adapter, root := attemptTestAdapter(t, `#!/usr/bin/python3
import os,sys,json,urllib.request
assert os.environ['HERMES_TUI_TOOLSETS']=='context_engine'
assert os.environ['OPENROUTER_API_KEY']=='aegis-local-parser-compatibility'
assert 'HTTP_PROXY' not in os.environ
print(json.dumps({'method':'event','params':{'type':'gateway.ready'}}),flush=True)
sys.stdin.readline()
print(json.dumps({'id':'create','result':{'session_id':'local'}}),flush=True)
p=json.loads(sys.stdin.readline())
try:
 req=urllib.request.Request(os.environ['OPENROUTER_BASE_URL']+'/chat/completions',data=json.dumps({'model':os.environ['HERMES_MODEL'],'messages':[{'role':'user','content':p['params']['text']}]}).encode(),headers={'Content-Type':'application/json','Authorization':'Bearer '+os.environ['OPENROUTER_API_KEY']})
 out=json.load(urllib.request.urlopen(req))['choices'][0]['message']['content']
 for typ,payload in [('message.start',{}),('message.complete',{'text':out,'status':'complete'})]:
  print(json.dumps({'method':'event','params':{'type':typ,'session_id':'local','payload':payload}}),flush=True)
except Exception:
 print(json.dumps({'method':'event','params':{'type':'error','session_id':'local'}}),flush=True)
for line in sys.stdin: pass
`)
			r := validAttemptTurnRequest(root)
			h := core.HermesConfig{Model: "exact:1", Provider: "ollama", LocalInference: &core.LocalInference{Kind: "ollama", Endpoint: upstream.URL, ModelDigest: digest}}
			r.Launch.Mandate.Hermes = h
			r.Launch.Mandate.Tools = nil
			r.Launch.AuthorityContext.Authority.Hermes = h
			r.Launch.AuthorityContext.Authority.Tools = nil
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			r.Model = h.Model
			r.Provider = h.Provider
			r.Bounds.Duration = 5 * time.Second
			result, err := adapter.AttemptTurn(context.Background(), r)
			if drift {
				if err == nil || calls.Load() != 0 {
					t.Fatalf("drift forwarded: %v", err)
				}
			} else if err != nil || result.Output != "ordinary answer" || calls.Load() != 1 {
				t.Fatalf("transport failed: %v %+v calls=%d", err, result, calls.Load())
			}
			entries, err := os.ReadDir(filepath.Join(r.StateRoot, "runtime"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("cleanup: %v %v", entries, err)
			}
		})
	}
}
