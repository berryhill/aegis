// Package localinference provides ordinary, process-owned local inference.
package localinference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/core"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CompatibilityToken = "aegis-local-parser-compatibility"

type Proxy struct {
	route       core.LocalInference
	model       string
	admit       func(context.Context) error
	server      *http.Server
	listener    net.Listener
	client      *http.Client
	mu          sync.RWMutex
	custody     *ProcessCustody
	lifetime    context.Context
	cancel      context.CancelFunc
	watcherDone chan struct{}
}

func New(route core.LocalInference, model string, admit func(context.Context) error) (*Proxy, error) {
	return NewWithContext(context.Background(), route, model, admit)
}

// NewWithContext binds all upstream work to the runtime lifetime.
func NewWithContext(ctx context.Context, route core.LocalInference, model string, admit func(context.Context) error) (*Proxy, error) {
	if err := route.Validate(); err != nil {
		return nil, err
	}
	if model == "" || admit == nil {
		return nil, errors.New("local inference requires model and fresh admission")
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &Proxy{route: route, model: model, admit: admit, listener: l, client: &http.Client{Timeout: 5 * time.Minute, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	p.lifetime, p.cancel = context.WithCancel(ctx)
	p.watcherDone = make(chan struct{})
	go p.watchAdmission()
	p.server = &http.Server{Handler: http.HandlerFunc(p.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 5 * time.Minute, MaxHeaderBytes: 8192}
	go p.server.Serve(l)
	return p, nil
}
func (p *Proxy) Endpoint() string { return "http://" + p.listener.Addr().String() }
func (p *Proxy) Bind(pid int) error {
	c, e := AcquireProcessCustody(pid)
	if e != nil {
		return e
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.custody != nil {
		c.Close()
		return errors.New("already bound")
	}
	p.custody = c
	return nil
}
func (p *Proxy) watchAdmission() {
	defer close(p.watcherDone)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.lifetime.Done():
			return
		case <-ticker.C:
			check, cancel := context.WithTimeout(p.lifetime, time.Second)
			err := p.admit(check)
			expired := check.Err() != nil
			cancel()
			p.mu.RLock()
			dead := p.custody != nil && !p.custody.Alive()
			p.mu.RUnlock()
			if err != nil || expired || dead {
				p.cancel()
				return
			}
		}
	}
}
func (p *Proxy) Close() error {
	p.cancel()
	<-p.watcherDone
	err := p.server.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.custody != nil {
		p.custody.Close()
	}
	p.client.CloseIdleConnections()
	return err
}
func (p *Proxy) owned(r *http.Request) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	remote, e := net.ResolveTCPAddr("tcp", r.RemoteAddr)
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	return e == nil && p.custody != nil && p.custody.OwnsTCPConnection(remote, local)
}
func decode(b []byte, v any) error {
	if err := uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func (p *Proxy) request(ctx context.Context, path string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(p.lifetime, cancel)
	defer stop()
	if p.lifetime.Err() != nil {
		return nil, p.lifetime.Err()
	}
	method := "GET"
	if body != nil {
		method = "POST"
	}
	r, e := http.NewRequestWithContext(ctx, method, p.route.Endpoint+path, bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	s, e := p.client.Do(r)
	if e != nil {
		return nil, e
	}
	defer s.Body.Close()
	if s.StatusCode != 200 {
		return nil, errors.New("local inference upstream rejected")
	}
	b, e := io.ReadAll(io.LimitReader(s.Body, (4<<20)+1))
	if e != nil || len(b) > 4<<20 {
		return nil, errors.New("response exceeds bound")
	}
	return b, nil
}

// VerifyModel re-resolves the exact name before every inference; duplicate names deny.
// This inventory check is not atomic artifact pinning across the inference call.
func (p *Proxy) VerifyModel(ctx context.Context) error {
	b, e := p.request(ctx, "/api/tags", nil)
	if e != nil {
		return e
	}
	var inventory struct {
		Models []struct {
			Name   string `json:"name"`
			Model  string `json:"model"`
			Digest string `json:"digest"`
		} `json:"models"`
	}
	if uniqueJSON(json.NewDecoder(bytes.NewReader(b)), 0) != nil || json.Unmarshal(b, &inventory) != nil || len(inventory.Models) > 10000 {
		return errors.New("invalid inventory")
	}
	matches := 0
	for _, m := range inventory.Models {
		if m.Name != "" && m.Model != "" && m.Name != m.Model {
			return errors.New("conflicting model aliases")
		}
		if m.Name == p.model || m.Model == p.model {
			matches++
			if "sha256:"+strings.TrimPrefix(m.Digest, "sha256:") != p.route.ModelDigest {
				return errors.New("model digest drift")
			}
		}
	}
	if matches != 1 {
		return errors.New("absent or ambiguous model")
	}
	return nil
}
func (p *Proxy) serve(w http.ResponseWriter, r *http.Request) {
	deny := func() { http.Error(w, "local inference denied", 403) }
	if len(r.Header.Values("Authorization")) != 1 || r.Header.Get("Authorization") != "Bearer "+CompatibilityToken || p.lifetime.Err() != nil || r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.URL.RawQuery != "" || !p.owned(r) {
		deny()
		return
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		deny()
		return
	}
	var q openAIChatRequest
	if decode(b, &q) != nil || q.Model != p.model || len(q.Messages) == 0 || len(q.Tools) > 0 || q.ToolChoice != nil {
		deny()
		return
	}
	for _, m := range q.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			deny()
			return
		}
		if _, ok := m.Content.(string); !ok || m.Name != "" || m.ToolCallID != "" || len(m.ToolCalls) > 0 {
			deny()
			return
		}
	}
	ctx := r.Context()
	if p.admit(ctx) != nil || p.VerifyModel(ctx) != nil || p.admit(ctx) != nil || !p.owned(r) {
		deny()
		return
	}
	stream := q.Stream
	q.Stream = false
	q.StreamOptions = nil
	b, _ = json.Marshal(q)
	b, e = p.request(ctx, "/v1/chat/completions", b)
	var answer openAIChatResponse
	if e != nil || decode(b, &answer) != nil || answer.Model != p.model || len(answer.Choices) != 1 {
		deny()
		return
	}
	c := answer.Choices[0]
	text, ok := c.Message.Content.(string)
	if !ok || c.Message.Role != "assistant" || len(c.Message.ToolCalls) > 0 || c.Message.ToolCallID != "" || c.Message.Name != "" || c.FinishReason != "stop" || c.Index != 0 || p.admit(ctx) != nil {
		deny()
		return
	}
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := map[string]any{"id": answer.ID, "object": "chat.completion.chunk", "created": answer.Created, "model": answer.Model, "choices": []any{map[string]any{"index": 0, "delta": map[string]string{"role": "assistant", "content": text}, "finish_reason": nil}}}
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	chunk["choices"] = []any{map[string]any{"index": 0, "delta": map[string]string{}, "finish_reason": "stop"}}
	data, _ = json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
}
