//go:build linux

package localinference

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

// Hermes' OpenAI route adds session_id and streaming usage options; Ollama
// returns nullable logprobs. Exercise the owned HTTP boundary, not just decoding.
func TestInstalledHermesProxyShape(t *testing.T) {
	for _, logprobs := range []string{`,"logprobs":null`, ""} {
		t.Run("logprobs="+logprobs, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					io.WriteString(w, `{"models":[{"name":"exact","digest":"`+strings.Repeat("a", 64)+`"}]}`)
					return
				}
				var q map[string]any
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
				}
				if q["session_id"] != "installed-session" || q["stream"] == true || q["stream_options"] != nil {
					t.Errorf("unexpected forwarded shape: %+v", q)
				}
				io.WriteString(w, `{"id":"answer","object":"chat.completion","created":1,"model":"exact","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"`+logprobs+`}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer upstream.Close()
			p, err := New(core.LocalInference{Kind: "ollama", Endpoint: upstream.URL, ModelDigest: "sha256:" + strings.Repeat("a", 64)}, "exact", func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if err = p.Bind(os.Getpid()); err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest("POST", p.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact","messages":[{"role":"user","content":"hello"}],"session_id":"installed-session","stream":true,"stream_options":{"include_usage":true}}`))
			req.Header.Set("Authorization", "Bearer "+CompatibilityToken)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != 200 || !strings.Contains(string(body), `"content":"hello"`) || !strings.Contains(string(body), "data: [DONE]") {
				t.Fatalf("status=%d body=%s err=%v", response.StatusCode, body, err)
			}
		})
	}
}
