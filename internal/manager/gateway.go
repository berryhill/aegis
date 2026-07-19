package manager

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

type GatewayMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	} `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type GatewayClient struct {
	writer   io.Writer
	messages chan GatewayMessage
	failures chan error
	maximum  int
	writeMu  sync.Mutex
	nextID   atomic.Uint64
	poisoned atomic.Bool
}

func NewGatewayClient(reader io.Reader, writer io.Writer, maximumMessageBytes int) (*GatewayClient, error) {
	if reader == nil || writer == nil || maximumMessageBytes < 1024 || maximumMessageBytes > 16<<20 {
		return nil, errors.New("invalid Hermes gateway connection bounds")
	}
	client := &GatewayClient{writer: writer, messages: make(chan GatewayMessage, 64), failures: make(chan error, 1), maximum: maximumMessageBytes}
	go client.read(reader)
	return client, nil
}

func (c *GatewayClient) read(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), c.maximum)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if err := validateJSONObject(line, 32); err != nil {
			c.fail(fmt.Errorf("Hermes gateway protocol: %w", err))
			return
		}
		var message GatewayMessage
		if err := strictDecode(line, &message); err != nil {
			c.fail(fmt.Errorf("Hermes gateway protocol: %w", err))
			return
		}
		if message.JSONRPC != "2.0" {
			c.fail(errors.New("Hermes gateway protocol version mismatch"))
			return
		}
		c.messages <- message
	}
	if err := scanner.Err(); err != nil {
		c.fail(err)
	} else {
		c.fail(io.EOF)
	}
}

func (c *GatewayClient) fail(err error) {
	select {
	case c.failures <- err:
	default:
	}
}

func (c *GatewayClient) WaitReady(ctx context.Context) error {
	_, err := c.wait(ctx, func(message GatewayMessage) bool {
		return message.Method == "event" && message.Params.Type == "gateway.ready"
	})
	return err
}

func (c *GatewayClient) CreateSession(ctx context.Context, source string) (string, error) {
	if source == "" || len(source) > 128 {
		return "", errors.New("Hermes gateway source is invalid")
	}
	id := c.id()
	if err := c.write(id, "session.create", map[string]any{"cols": 100, "source": source}); err != nil {
		return "", err
	}
	message, err := c.wait(ctx, func(message GatewayMessage) bool { return messageID(message) == id })
	if err != nil {
		return "", err
	}
	if message.Error != nil {
		return "", errors.New("Hermes session creation failed")
	}
	sessionID := fmt.Sprint(message.Result["session_id"])
	if sessionID == "" || sessionID == "<nil>" || len(sessionID) > 256 {
		return "", errors.New("Hermes session creation returned an invalid session ID")
	}
	return sessionID, nil
}

func (c *GatewayClient) Turn(ctx context.Context, sessionID, text string, maximumResponseBytes int) ([]byte, error) {
	if sessionID == "" || len(sessionID) > 256 || text == "" || len(text) > c.maximum || maximumResponseBytes < 1 || maximumResponseBytes > c.maximum {
		return nil, errors.New("Hermes turn bounds are invalid")
	}
	if c.poisoned.Load() {
		return nil, errors.New("Hermes gateway session is unusable after an interrupted turn")
	}
	id := c.id()
	if err := c.writeContext(ctx, id, "prompt.submit", map[string]any{"session_id": sessionID, "text": text}); err != nil {
		c.poisoned.Store(true)
		return nil, err
	}
	response := make([]byte, 0, 4096)
	started := false
	for {
		message, err := c.wait(ctx, func(message GatewayMessage) bool {
			return messageID(message) == id || (message.Method == "event" && (message.Params.Type == "message.start" || message.Params.Type == "message.delta" || message.Params.Type == "message.complete" || message.Params.Type == "error"))
		})
		if err != nil {
			// Turn events carry no prompt ID. After interruption, late events cannot
			// safely be distinguished from the events of a later turn.
			c.poisoned.Store(true)
			return nil, err
		}
		if message.Error != nil || message.Params.Type == "error" {
			return nil, errors.New("Hermes manager turn failed")
		}
		switch message.Params.Type {
		case "message.start":
			started = true
		case "message.delta":
			if !started {
				return nil, errors.New("Hermes gateway delta before message start")
			}
			response = append(response, payloadTextValue(message.Params.Payload)...)
			if len(response) > maximumResponseBytes {
				return nil, errors.New("Hermes gateway response exceeds limit")
			}
		case "message.complete":
			if !started {
				return nil, errors.New("Hermes gateway completion before message start")
			}
			if len(response) == 0 {
				response = append(response, payloadTextValue(message.Params.Payload)...)
			}
			if len(response) == 0 || len(response) > maximumResponseBytes {
				return nil, errors.New("Hermes gateway completion is empty or oversized")
			}
			return response, nil
		}
	}
}

func (c *GatewayClient) id() string { return "aegis-" + strconv.FormatUint(c.nextID.Add(1), 10) }
func (c *GatewayClient) write(id, method string, params map[string]any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return json.NewEncoder(c.writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}
func (c *GatewayClient) writeContext(ctx context.Context, id, method string, params map[string]any) error {
	done := make(chan error, 1)
	go func() { done <- c.write(id, method, params) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}
func (c *GatewayClient) wait(ctx context.Context, match func(GatewayMessage) bool) (GatewayMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return GatewayMessage{}, ctx.Err()
		case err := <-c.failures:
			return GatewayMessage{}, err
		case message := <-c.messages:
			if match(message) {
				return message, nil
			}
		}
	}
}
func messageID(message GatewayMessage) string {
	var value string
	_ = json.Unmarshal(message.ID, &value)
	return value
}
func payloadTextValue(payload map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "message"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}
