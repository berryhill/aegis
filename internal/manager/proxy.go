package manager

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ProxyConfig struct {
	Target               string
	Model                string
	RouteDigest          string
	MaximumRequestBytes  int64
	MaximumResponseBytes int64
	Timeout              time.Duration
	Guard                *Guard
	SessionActive        func() bool
	CapabilityExpires    time.Time
	ConsumeCapability    func() bool
}

type Proxy struct {
	config   ProxyConfig
	token    string
	server   *http.Server
	listen   net.Listener
	once     sync.Once
	mu       sync.RWMutex
	closeErr error
}

type openAIChatRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Stop                any             `json:"stop,omitempty"`
	Tools               []any           `json:"tools,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"`
	ResponseFormat      any             `json:"response_format,omitempty"`
	User                string          `json:"user,omitempty"`
}
type openAIMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  []any  `json:"tool_calls,omitempty"`
}
type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
		Logprobs     any           `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func StartProxy(ctx context.Context, config ProxyConfig) (*Proxy, error) {
	if config.Guard == nil || config.Model == "" || config.RouteDigest == "" || config.MaximumRequestBytes < 1024 || config.MaximumRequestBytes > 16<<20 || config.MaximumResponseBytes < 1024 || config.MaximumResponseBytes > 16<<20 || config.Timeout <= 0 || config.Timeout > 5*time.Minute || config.SessionActive == nil {
		return nil, errors.New("invalid inference proxy configuration")
	}
	target, err := url.Parse(config.Target)
	if err != nil || target.Scheme != "http" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Path != "" || !loopbackHost(target.Hostname()) {
		return nil, errors.New("inference target must be an HTTP loopback origin")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		listener.Close()
		return nil, errors.New("generate proxy authentication")
	}
	proxy := &Proxy{config: config, token: base64.RawURLEncoding.EncodeToString(tokenBytes), listen: listener}
	proxy.server = &http.Server{Handler: http.HandlerFunc(proxy.handle), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: config.Timeout, WriteTimeout: config.Timeout, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = proxy.server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = proxy.Close(shutdown)
	}()
	return proxy, nil
}

func (p *Proxy) Endpoint() string { return "http://" + p.listen.Addr().String() }
func (p *Proxy) Token() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.token
}

func (p *Proxy) Close(ctx context.Context) error {
	p.once.Do(func() {
		p.mu.Lock()
		p.token = ""
		p.mu.Unlock()
		p.closeErr = p.server.Shutdown(ctx)
		_ = p.listen.Close()
	})
	return p.closeErr
}

func (p *Proxy) handle(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	capabilityValid := p.config.CapabilityExpires.IsZero() || time.Now().Before(p.config.CapabilityExpires)
	if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" || request.URL.RawQuery != "" || request.Header.Get("Content-Type") != "application/json" || !p.config.SessionActive() || !capabilityValid || !constantToken(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "), p.Token()) {
		http.Error(writer, "route denied", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, p.config.MaximumRequestBytes+1))
	if err != nil || int64(len(body)) > p.config.MaximumRequestBytes {
		http.Error(writer, "request denied", http.StatusRequestEntityTooLarge)
		return
	}
	var envelope openAIChatRequest
	if err = validateJSONObject(body, 32); err != nil {
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if err = strictDecode(body, &envelope); err != nil || envelope.Model != p.config.Model || envelope.Stream || len(envelope.Tools) != 0 || envelope.ToolChoice != nil || envelope.User != "" || !validMessages(envelope.Messages) {
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if p.config.ConsumeCapability != nil && !p.config.ConsumeCapability() {
		http.Error(writer, "route denied", http.StatusForbidden)
		return
	}
	finding := p.config.Guard.Inspect(request.Context(), ContentEnvelope{Source: SourceOperation, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: "application/json", ProvenanceID: "serialized-request", RouteClass: "local", Content: body})
	if finding.Decision != AllowLocal {
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	target := strings.TrimSuffix(p.config.Target, "/") + request.URL.Path
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		http.Error(writer, "local inference unavailable", http.StatusBadGateway)
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: p.config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(upstream)
	if err != nil {
		http.Error(writer, "local inference unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		http.Error(writer, "local inference redirect denied", http.StatusBadGateway)
		return
	}
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, p.config.MaximumResponseBytes+1))
	if err != nil || int64(len(responseBody)) > p.config.MaximumResponseBytes {
		http.Error(writer, "local inference response denied", http.StatusBadGateway)
		return
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") || !validChatResponse(responseBody, p.config.Model) {
		http.Error(writer, "local inference response denied", http.StatusBadGateway)
		return
	}
	finding = p.config.Guard.Inspect(request.Context(), ContentEnvelope{Source: SourceModelOutput, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: response.Header.Get("Content-Type"), ProvenanceID: "serialized-response", RouteClass: "local", Content: responseBody})
	if finding.Decision != AllowLocal {
		http.Error(writer, "local inference response denied", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(responseBody)
}

func constantToken(got, expected string) bool {
	return got != "" && expected != "" && len(got) == len(expected) && subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func validMessages(messages []openAIMessage) bool {
	if len(messages) == 0 || len(messages) > 256 {
		return false
	}
	for _, message := range messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" {
			return false
		}
		if _, ok := message.Content.(string); !ok || message.Name != "" || message.ToolCallID != "" || len(message.ToolCalls) != 0 {
			return false
		}
	}
	return true
}

func validChatResponse(body []byte, model string) bool {
	if validateJSONObject(body, 16) != nil {
		return false
	}
	var response openAIChatResponse
	if strictDecode(body, &response) != nil || response.Model != model || len(response.Choices) != 1 {
		return false
	}
	message := response.Choices[0].Message
	content, ok := message.Content.(string)
	return ok && content != "" && message.Role == "assistant" && message.Name == "" && message.ToolCallID == "" && len(message.ToolCalls) == 0
}
