package manager

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ProxyConfig struct {
	Target                   string
	Model                    string
	RouteDigest              string
	MaximumRequestBytes      int64
	MaximumResponseBytes     int64
	Timeout                  time.Duration
	Guard                    *Guard
	SessionActive            func() bool
	CapabilityExpires        time.Time
	ConsumeCapability        func() bool
	RequireSystemInstruction bool
}

type Proxy struct {
	config                            ProxyConfig
	token                             string
	server                            *http.Server
	listen                            net.Listener
	once                              sync.Once
	mu                                sync.RWMutex
	closeErr                          error
	reached, ollamaReached, forwarded atomic.Bool
	lastSafe                          atomic.Value
}

type openAIChatRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	MaxTokens           int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	Stop                any      `json:"stop,omitempty"`
	Tools               []any    `json:"tools,omitempty"`
	ToolChoice          any      `json:"tool_choice,omitempty"`
	ResponseFormat      any      `json:"response_format,omitempty"`
	ReasoningEffort     string   `json:"reasoning_effort,omitempty"`
	User                string   `json:"user,omitempty"`
	SessionID           string   `json:"session_id,omitempty"`
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

type openAIChatStreamChunk struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	Created           int64  `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint,omitempty"`
	Choices           []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role,omitempty"`
			Content   string `json:"content,omitempty"`
			Reasoning string `json:"reasoning,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
		Logprobs     any     `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
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

// LastSafeDiagnostic exposes routing metadata without request or response data.
func (p *Proxy) LastSafeDiagnostic() string {
	if value := p.lastSafe.Load(); value != nil {
		return value.(string)
	}
	if p.forwarded.Load() {
		return "response_forwarded"
	}
	if p.ollamaReached.Load() {
		return "ollama_response_rejected"
	}
	if p.reached.Load() {
		return "proxy_request_rejected"
	}
	return "proxy_not_reached"
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
	p.reached.Store(true)
	p.lastSafe.Store("proxy_reached")
	writer.Header().Set("Cache-Control", "no-store")
	capabilityValid := p.config.CapabilityExpires.IsZero() || time.Now().Before(p.config.CapabilityExpires)
	if request.Method != http.MethodPost || request.URL.Path != "/v1/chat/completions" || request.URL.RawQuery != "" || !jsonContentType(request.Header.Get("Content-Type")) || !p.config.SessionActive() || !capabilityValid || !constantToken(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "), p.Token()) {
		p.lastSafe.Store("route_or_auth_rejected")
		http.Error(writer, "route denied", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, p.config.MaximumRequestBytes+1))
	if err != nil || int64(len(body)) > p.config.MaximumRequestBytes {
		p.lastSafe.Store("request_size_rejected")
		http.Error(writer, "request denied", http.StatusRequestEntityTooLarge)
		return
	}
	var envelope openAIChatRequest
	if err = validateJSONObject(body, 32); err != nil {
		p.lastSafe.Store("request_json_rejected")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if err = strictDecode(body, &envelope); err != nil {
		p.lastSafe.Store(safeRequestDecodeDiagnostic(err))
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if envelope.Model != p.config.Model {
		p.lastSafe.Store("request_model_mismatch")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if len(envelope.Tools) != 0 || envelope.ToolChoice != nil {
		p.lastSafe.Store("request_tools_rejected")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if envelope.User != "" || envelope.ResponseFormat != nil || !validMessages(envelope.Messages) {
		p.lastSafe.Store("request_messages_rejected")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if p.config.RequireSystemInstruction && !hasManagerSystemInstruction(envelope.Messages) {
		p.lastSafe.Store("request_system_instruction_missing")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if len(envelope.SessionID) > 256 {
		p.lastSafe.Store("request_session_id_rejected")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	if p.config.ConsumeCapability != nil && !p.config.ConsumeCapability() {
		p.lastSafe.Store("capability_rejected")
		http.Error(writer, "route denied", http.StatusForbidden)
		return
	}
	guardContent := untrustedMessageContent(envelope.Messages)
	finding := p.config.Guard.Inspect(request.Context(), ContentEnvelope{Source: SourceOperation, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: "text/plain", ProvenanceID: "message-content", RouteClass: "local", Content: guardContent})
	if finding.Decision != AllowLocal {
		p.lastSafe.Store("request_guard_rejected")
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	// session_id is a Hermes transport extension, not part of Ollama's OpenAI
	// request schema. Validate it above, then remove it at this boundary. Bound
	// completion length for the small-model certification contract; every valid
	// envelope is far smaller than this and an unbounded reasoning run must fail
	// closed rather than consume the entire authenticated session.
	envelope.SessionID = ""
	if envelope.MaxCompletionTokens > 192 {
		envelope.MaxCompletionTokens = 192
	} else if envelope.MaxCompletionTokens == 0 && (envelope.MaxTokens == 0 || envelope.MaxTokens > 192) {
		envelope.MaxTokens = 192
	}
	envelope.ReasoningEffort = "none"
	zero := 0.0
	envelope.Temperature = &zero
	envelope.ResponseFormat = managerResponseFormat()
	upstreamBody, err := json.Marshal(envelope)
	if err != nil {
		http.Error(writer, "request denied", http.StatusForbidden)
		return
	}
	target := strings.TrimSuffix(p.config.Target, "/") + request.URL.Path
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodPost, target, bytes.NewReader(upstreamBody))
	if err != nil {
		http.Error(writer, "local inference unavailable", http.StatusBadGateway)
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: p.config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	p.ollamaReached.Store(true)
	p.lastSafe.Store("ollama_reached")
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
	contentType := response.Header.Get("Content-Type")
	modelOutput := responseBody
	validResponse := response.StatusCode >= 200 && response.StatusCode < 300
	if envelope.Stream {
		var valid bool
		modelOutput, valid = validChatStreamResponse(responseBody, p.config.Model)
		validResponse = validResponse && strings.HasPrefix(strings.ToLower(contentType), "text/event-stream") && valid
	} else {
		validResponse = validResponse && strings.HasPrefix(strings.ToLower(contentType), "application/json") && validChatResponse(responseBody, p.config.Model)
	}
	if !validResponse {
		http.Error(writer, "local inference response denied", http.StatusBadGateway)
		return
	}
	finding = p.config.Guard.Inspect(request.Context(), ContentEnvelope{Source: SourceModelOutput, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: contentType, ProvenanceID: "serialized-response", RouteClass: "local", Content: modelOutput})
	if finding.Decision != AllowLocal {
		http.Error(writer, "local inference response denied", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Type", contentType)
	p.forwarded.Store(true)
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(responseBody)
}

func safeRequestDecodeDiagnostic(err error) string {
	const prefix = "json: unknown field \""
	text := err.Error()
	if strings.HasPrefix(text, prefix) && strings.HasSuffix(text, "\"") {
		field := strings.TrimSuffix(strings.TrimPrefix(text, prefix), "\"")
		for _, char := range field {
			if (char < 'a' || char > 'z') && char != '_' {
				return "request_envelope_decode_rejected"
			}
		}
		if field != "" && len(field) <= 64 {
			return "request_unknown_field_" + field
		}
	}
	return "request_envelope_decode_rejected"
}

func constantToken(got, expected string) bool {
	return got != "" && expected != "" && len(got) == len(expected) && subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func jsonContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && strings.EqualFold(mediaType, "application/json")
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

func untrustedMessageContent(messages []openAIMessage) []byte {
	var content bytes.Buffer
	for _, message := range messages {
		// Safe mode suppresses user config, rules, skills, plugins, and MCP, so
		// system messages originate from Hermes and Aegis. Scan user and prior
		// assistant text, which remain the untrusted inference payload.
		if message.Role == "system" {
			continue
		}
		if text, ok := message.Content.(string); ok {
			content.WriteString(text)
			content.WriteByte('\n')
		}
	}
	return content.Bytes()
}

func hasManagerSystemInstruction(messages []openAIMessage) bool {
	for _, message := range messages {
		if message.Role != "system" {
			continue
		}
		if text, ok := message.Content.(string); ok && strings.Contains(text, SystemInstruction) {
			return true
		}
	}
	return false
}

func managerResponseFormat() any {
	branch := func(kind string, proposalSchema any) map[string]any {
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"schema_version", "kind", "message", "proposal"},
			"properties": map[string]any{
				"schema_version": map[string]any{"type": "string", "const": ResponseSchemaVersion},
				"kind":           map[string]any{"type": "string", "const": kind},
				"message":        map[string]any{"type": "string", "maxLength": 160},
				"proposal":       proposalSchema,
			},
		}
	}
	return map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "aegis_manager_response",
			"strict": true,
			"schema": map[string]any{"oneOf": []any{
				branch("message", map[string]any{"type": "null"}),
				branch("proposal", map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"operation", "arguments"},
					"properties": map[string]any{
						"operation": map[string]any{"type": "string", "enum": []string{string(StatusShow), string(AuditVerify), string(SessionExit), string(SecretList), string(AuditQuery), string(SecretSearch), string(SecretMetadata), string(SecretHistory), string(SecretProposeCreate), string(SecretProposeRevoke), string(SecretProposeRotate), string(SecretProposeBinding)}},
						"arguments": map[string]any{"type": "object"},
					},
				}),
			}},
		},
	}
}

func managerProposalSchemas() []any {
	stringProperty := func() any { return map[string]any{"type": "string", "maxLength": 256} }
	integerProperty := func() any { return map[string]any{"type": "integer", "minimum": 1} }
	stringArrayProperty := func() any { return map[string]any{"type": "array", "items": map[string]any{"type": "string"}} }
	page := map[string]any{"limit": integerProperty(), "cursor": stringProperty()}
	return []any{
		managerProposalSchema(StatusShow, nil, nil), managerProposalSchema(AuditVerify, nil, nil), managerProposalSchema(SessionExit, nil, nil),
		managerProposalSchema(SecretList, page, nil), managerProposalSchema(AuditQuery, page, nil),
		managerProposalSchema(SecretSearch, map[string]any{"query": stringProperty(), "limit": integerProperty(), "cursor": stringProperty()}, []string{"query"}),
		managerProposalSchema(SecretMetadata, map[string]any{"record_id": stringProperty()}, []string{"record_id"}),
		managerProposalSchema(SecretHistory, map[string]any{"record_id": stringProperty()}, []string{"record_id"}),
		managerProposalSchema(SecretProposeCreate, map[string]any{"reference": stringProperty(), "kind": stringProperty(), "disclosure": stringProperty(), "tags": stringArrayProperty(), "collection": stringProperty()}, []string{"reference", "kind", "disclosure"}),
		managerProposalSchema(SecretProposeRevoke, map[string]any{"record_id": stringProperty(), "reason": stringProperty(), "version": integerProperty()}, []string{"record_id", "reason"}),
		managerProposalSchema(SecretProposeRotate, map[string]any{"record_id": stringProperty()}, []string{"record_id"}),
		managerProposalSchema(SecretProposeBinding, map[string]any{"agent_id": stringProperty(), "stanza_id": stringProperty(), "scope": stringProperty(), "record_id": stringProperty(), "version_policy": stringProperty(), "mode": stringProperty(), "destinations": stringArrayProperty(), "pinned_version": integerProperty()}, []string{"agent_id", "stanza_id", "scope", "record_id", "version_policy", "mode", "destinations"}),
	}
}

func managerProposalSchema(operation Operation, properties map[string]any, required []string) any {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"operation", "arguments"},
		"properties": map[string]any{
			"operation": map[string]any{"type": "string", "const": string(operation)},
			"arguments": map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required},
		},
	}
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

func validChatStreamResponse(body []byte, model string) ([]byte, bool) {
	var output strings.Builder
	sawAssistant, sawStop, sawDone := false, false, false
	for _, rawLine := range bytes.Split(body, []byte("\n")) {
		line := strings.TrimSuffix(string(rawLine), "\r")
		if line == "" {
			continue
		}
		if sawDone || !strings.HasPrefix(line, "data: ") {
			return nil, false
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			sawDone = true
			continue
		}
		if validateJSONObject([]byte(data), 16) != nil {
			return nil, false
		}
		var chunk openAIChatStreamChunk
		if strictDecode([]byte(data), &chunk) != nil || chunk.Model != model || chunk.Object != "chat.completion.chunk" || len(chunk.Choices) > 1 {
			return nil, false
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 || choice.Delta.Role != "" && choice.Delta.Role != "assistant" {
				return nil, false
			}
			if choice.Delta.Role == "assistant" {
				sawAssistant = true
			}
			output.WriteString(choice.Delta.Reasoning)
			output.WriteString(choice.Delta.Content)
			if choice.FinishReason != nil {
				if *choice.FinishReason != "stop" || sawStop {
					return nil, false
				}
				sawStop = true
			}
		}
	}
	if !sawAssistant || !sawStop || !sawDone || output.Len() == 0 {
		return nil, false
	}
	return []byte(output.String()), true
}
