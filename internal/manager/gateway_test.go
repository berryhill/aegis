package manager

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGatewayFixtureMultiTurn(t *testing.T) {
	gatewayOutputReader, gatewayOutputWriter := io.Pipe()
	gatewayInputReader, gatewayInputWriter := io.Pipe()
	client, err := NewGatewayClient(gatewayOutputReader, gatewayInputWriter, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer gatewayOutputWriter.Close()
		defer gatewayInputReader.Close()
		encoder := json.NewEncoder(gatewayOutputWriter)
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "gateway.ready", "payload": map[string]any{}}})
		scanner := bufio.NewScanner(gatewayInputReader)
		for scanner.Scan() {
			var request map[string]any
			_ = json.Unmarshal(scanner.Bytes(), &request)
			id := request["id"]
			switch request["method"] {
			case "session.create":
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"session_id": "session-1"}})
			case "prompt.submit":
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "session_id": "another-session"}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "another-session", "payload": map[string]any{"text": "wrong session"}}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "session_id": "session-1"}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.delta", "session_id": "session-1", "payload": map[string]any{"text": `{"schema_version":"aegis.manager.response.v1","kind":"message","message":"ok","proposal":null}`}}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "session-1", "payload": map[string]any{"status": "complete"}}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "session.info", "session_id": "session-1", "payload": map[string]any{"running": false}}})
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	session, err := client.CreateSession(ctx, "aegis-manager")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		var response []byte
		var err error
		if index == 0 {
			response, err = client.Turn(ctx, session, "hello", 4096)
		} else {
			var streamed []byte
			response, err = client.TurnStream(ctx, session, "hello", 4096, func(delta []byte) error {
				streamed = append(streamed, delta...)
				return nil
			})
			if !bytes.Equal(streamed, response) {
				t.Fatalf("streamed=%s response=%s", streamed, response)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := DecodeResponse(response, 4096); err != nil {
			t.Fatalf("turn %d response invalid: %s: %v", index, response, err)
		}
	}
}

func TestGatewayTurnWaitsForSessionSettlement(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	inputReader, inputWriter := io.Pipe()
	client, err := NewGatewayClient(outputReader, inputWriter, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer outputWriter.Close()
	defer inputReader.Close()
	defer inputWriter.Close()
	var settled atomic.Bool
	go func() {
		encoder := json.NewEncoder(outputWriter)
		scanner := bufio.NewScanner(inputReader)
		if !scanner.Scan() {
			return
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "session_id": "session-1", "seq": 1}})
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.delta", "session_id": "session-1", "seq": 2, "payload": map[string]any{"text": "first"}}})
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "session-1", "seq": 3, "payload": map[string]any{"status": "complete"}}})
		time.Sleep(30 * time.Millisecond) // Hermes clears running after message.complete.
		settled.Store(true)
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "session.info", "session_id": "session-1", "seq": 4, "payload": map[string]any{"running": false}}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Turn(ctx, "session-1", "first", 4096)
	if err != nil || string(response) != "first" {
		t.Fatalf("turn response=%q error=%v", response, err)
	}
	if !settled.Load() {
		t.Fatal("turn returned at message.complete before the session was reusable")
	}
}

func TestGatewayMissingSettlementPoisonsSession(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	inputReader, inputWriter := io.Pipe()
	client, err := NewGatewayClient(outputReader, inputWriter, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer outputWriter.Close()
	defer inputReader.Close()
	defer inputWriter.Close()
	go func() {
		scanner := bufio.NewScanner(inputReader)
		if !scanner.Scan() {
			return
		}
		encoder := json.NewEncoder(outputWriter)
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "session_id": "session-1"}})
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "session-1", "payload": map[string]any{"status": "complete", "text": "done"}}})
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "session.info", "session_id": "other-session", "payload": map[string]any{"running": false}}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.Turn(ctx, "session-1", "first", 4096); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing settlement error=%v", err)
	}
	if _, err := client.Turn(context.Background(), "session-1", "second", 4096); err == nil || !strings.Contains(err.Error(), "unusable after an interrupted turn") {
		t.Fatalf("session reused after missing settlement: %v", err)
	}
}

func TestGatewayBusyPromptSubmitFailsWithoutWaitingForEvents(t *testing.T) {
	for _, status := range []any{"queued", "busy", nil, 1} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			outputReader, outputWriter := io.Pipe()
			inputReader, inputWriter := io.Pipe()
			client, err := NewGatewayClient(outputReader, inputWriter, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer outputWriter.Close()
			defer inputReader.Close()
			defer inputWriter.Close()
			go func() {
				scanner := bufio.NewScanner(inputReader)
				if !scanner.Scan() {
					return
				}
				var request map[string]any
				_ = json.Unmarshal(scanner.Bytes(), &request)
				result := map[string]any{}
				if status != nil {
					result["status"] = status
				}
				_ = json.NewEncoder(outputWriter).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := client.Turn(ctx, "session-1", "hello", 4096); err == nil || !strings.Contains(err.Error(), "prompt-submit RPC status") {
				t.Fatalf("invalid submit status %v did not fail explicitly: %v", status, err)
			}
		})
	}
}

func TestGatewayMalformedOversizedAndTimeoutFailClosed(t *testing.T) {
	for name, input := range map[string]string{
		"malformed": "not-json\n", "duplicate": "{\"jsonrpc\":\"2.0\",\"jsonrpc\":\"2.0\"}\n",
		"oversized":            strings.Repeat("x", 2048) + "\n",
		"zero sequence":        `{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"s","seq":0}}` + "\n",
		"negative sequence":    `{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"s","seq":-1}}` + "\n",
		"sessionless sequence": `{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","seq":1}}` + "\n",
		"unknown event field":  `{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"s","seq":1,"authority":true}}` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewGatewayClient(strings.NewReader(input), io.Discard, 1024)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := client.WaitReady(ctx); err == nil {
				t.Fatal("bad gateway input accepted")
			}
		})
	}
	client, err := NewGatewayClient(bytes.NewBuffer(nil), io.Discard, 1024)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := client.WaitReady(ctx); err == nil {
		t.Fatal("closed gateway accepted")
	}
}

func TestGatewayInterruptedTurnPoisonsSessionAgainstStaleEvents(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	client, err := NewGatewayClient(outputReader, io.Discard, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer outputWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err = client.Turn(ctx, "session-1", "first", 4096); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first turn error=%v", err)
	}
	go func() {
		encoder := json.NewEncoder(outputWriter)
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "payload": map[string]any{}}})
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "payload": map[string]any{"text": "stale"}}})
	}()
	if _, err = client.Turn(context.Background(), "session-1", "second", 4096); err == nil || !strings.Contains(err.Error(), "unusable after an interrupted turn") {
		t.Fatalf("later turn accepted stale session: %v", err)
	}
}

func TestGatewayTurnBoundsBlockedTransportWrite(t *testing.T) {
	outputReader, outputWriter := io.Pipe()
	inputReader, inputWriter := io.Pipe()
	client, err := NewGatewayClient(outputReader, inputWriter, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer outputWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.Turn(ctx, "session-1", "blocked-write", 4096)
	_ = inputReader.Close()
	_ = inputWriter.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked transport error=%v", err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("blocked transport exceeded context deadline")
	}
}

func TestGatewayRejectsNonSuccessfulHermesCompletionStatus(t *testing.T) {
	for _, status := range []string{"", "error", "interrupted"} {
		t.Run(status, func(t *testing.T) {
			outputReader, outputWriter := io.Pipe()
			inputReader, inputWriter := io.Pipe()
			client, err := NewGatewayClient(outputReader, inputWriter, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer outputWriter.Close()
			defer inputReader.Close()
			go func() {
				defer inputWriter.Close()
				if !bufio.NewScanner(inputReader).Scan() {
					return
				}
				encoder := json.NewEncoder(outputWriter)
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.start", "session_id": "session-1", "payload": map[string]any{}}})
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "session-1", "payload": map[string]any{"text": "not a successful response", "status": status}}})
			}()
			if _, err = client.Turn(context.Background(), "session-1", "hello", 4096); err == nil || !strings.Contains(err.Error(), "completion status was") {
				t.Fatalf("status %q accepted: %v", status, err)
			}
		})
	}
}
