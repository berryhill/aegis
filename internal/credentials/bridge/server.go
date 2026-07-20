package bridge

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/credentials/broker"
)

const (
	protocolVersion = "2024-11-05"
	toolName        = "github_get_repository"
	brokerPath      = "/v1/broker/actions/github-get-repository"
)

type Server struct {
	materialPath string
	timeout      time.Duration
	now          func() time.Time
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type material struct {
	Socket     string    `json:"socket"`
	Capability string    `json:"capability"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type repositoryArguments struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
}

func New(materialPath string, timeout time.Duration) (*Server, error) {
	if materialPath == "" || timeout <= 0 || timeout > 30*time.Second {
		return nil, errors.New("credential bridge requires a material path and bounded timeout")
	}
	return &Server{materialPath: materialPath, timeout: timeout, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if in == nil || out == nil {
		return errors.New("credential bridge requires stdio")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var request rpcRequest
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.JSONRPC != "2.0" || request.Method == "" {
			if err = encoder.Encode(rpcResponse{JSONRPC: "2.0", ID: nil, Error: &rpcError{Code: -32700, Message: "invalid request"}}); err != nil {
				return err
			}
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		var id any
		if err := json.Unmarshal(request.ID, &id); err != nil {
			id = nil
		}
		response := rpcResponse{JSONRPC: "2.0", ID: id}
		response.Result, response.Error = s.handle(ctx, request)
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, request rpcRequest) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "aegis-credential-bridge", "version": "1"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name":        toolName,
			"description": "Read one exact allowlisted GitHub repository through Aegis. Aegis authorizes the session and applies the bound credential; no credential value is returned.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"owner", "repository"},
				"properties": map[string]any{
					"owner":      map[string]string{"type": "string", "description": "Exact GitHub repository owner"},
					"repository": map[string]string{"type": "string", "description": "Exact GitHub repository name"},
				},
			},
		}}}, nil
	case "tools/call":
		return s.call(ctx, request.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (s *Server) call(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var params callParams
	if decodeStrict(raw, &params) != nil || params.Name != toolName {
		return nil, &rpcError{Code: -32602, Message: "invalid tool call"}
	}
	var arguments repositoryArguments
	if decodeStrict(params.Arguments, &arguments) != nil || !validSegment(arguments.Owner) || !validSegment(arguments.Repository) {
		return nil, &rpcError{Code: -32602, Message: "owner and repository must be exact identifiers"}
	}
	result, err := s.execute(ctx, arguments)
	if err != nil {
		return map[string]any{"content": []any{map[string]string{"type": "text", "text": "Aegis denied or could not complete the credential-broker operation."}}, "isError": true}, nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, &rpcError{Code: -32603, Message: "result encoding failed"}
	}
	return map[string]any{
		"content":           []any{map[string]string{"type": "text", "text": string(encoded)}},
		"structuredContent": result,
		"isError":           false,
	}, nil
}

func (s *Server) execute(ctx context.Context, arguments repositoryArguments) (broker.Result, error) {
	data, err := os.ReadFile(s.materialPath)
	if err != nil {
		return broker.Result{}, errors.New("broker capability unavailable")
	}
	defer clear(data)
	var authority material
	if decodeStrict(data, &authority) != nil || authority.Socket == "" || len(authority.Capability) != 64 || !s.now().Before(authority.ExpiresAt) {
		return broker.Result{}, errors.New("broker capability invalid")
	}
	requestID := make([]byte, 16)
	if _, err = rand.Read(requestID); err != nil {
		return broker.Result{}, errors.New("request identity unavailable")
	}
	request := broker.Request{SchemaVersion: 1, RequestID: hex.EncodeToString(requestID), Deadline: s.now().Add(s.timeout), Capability: authority.Capability, Owner: arguments.Owner, Repository: arguments.Repository}
	clear(requestID)
	body, err := json.Marshal(request)
	if err != nil {
		return broker.Result{}, errors.New("request encoding failed")
	}
	defer clear(body)
	requestContext, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestContext, http.MethodPost, "http://unix"+brokerPath, bytes.NewReader(body))
	if err != nil {
		return broker.Result{}, errors.New("request construction failed")
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", authority.Socket)
	}}
	client := &http.Client{Transport: transport, Timeout: s.timeout}
	defer transport.CloseIdleConnections()
	response, err := client.Do(httpRequest)
	if err != nil {
		return broker.Result{}, errors.New("broker unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		return broker.Result{}, errors.New("broker denied request")
	}
	limited := io.LimitReader(response.Body, 64<<10)
	var result broker.Result
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return broker.Result{}, errors.New("broker returned an invalid result")
	}
	return result, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func validSegment(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\\r\n\t") && value != "." && value != ".." && len(value) <= 100
}

func clear(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func (s *Server) String() string {
	return fmt.Sprintf("aegis credential bridge (%s)", toolName)
}
