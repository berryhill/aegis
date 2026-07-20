package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/credentials/broker"
)

func TestMCPBridgeListsAndExecutesOnlyTypedBrokerTool(t *testing.T) {
	directory := t.TempDir()
	socket := filepath.Join(directory, "broker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	capability := strings.Repeat("a", 64)
	requests := make(chan broker.Request, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != brokerPath || request.Header.Get("Content-Type") != "application/json" {
			t.Error("unexpected broker request")
			response.WriteHeader(http.StatusForbidden)
			return
		}
		var decoded broker.Request
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&decoded); err != nil {
			t.Error(err)
			response.WriteHeader(http.StatusForbidden)
			return
		}
		requests <- decoded
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(broker.Result{StatusCode: 200, Outcome: "repository_metadata", Repository: broker.Repository{Owner: "approved", Name: "repository", Private: true}})
	})}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	materialPath := filepath.Join(directory, broker.CapabilityFileName)
	material, _ := json.Marshal(map[string]any{"socket": socket, "capability": capability, "expires_at": time.Now().Add(time.Minute).UTC()})
	if err = os.WriteFile(materialPath, material, 0600); err != nil {
		t.Fatal(err)
	}
	bridge, err := New(materialPath, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"github_get_repository","arguments":{"owner":"approved","repository":"repository"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err = bridge.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], `"github_get_repository"`) || !strings.Contains(lines[2], `"repository_metadata"`) || strings.Contains(output.String(), capability) {
		t.Fatalf("unexpected MCP output: %s", output.String())
	}
	select {
	case request := <-requests:
		if request.Capability != capability || request.Owner != "approved" || request.Repository != "repository" || len(request.RequestID) != 32 || request.Deadline.IsZero() {
			t.Fatalf("unexpected broker request: %+v", request)
		}
	default:
		t.Fatal("bridge did not call broker")
	}
}

func TestMCPBridgeFailsClosedAndSanitizesBrokerFailure(t *testing.T) {
	materialPath := filepath.Join(t.TempDir(), broker.CapabilityFileName)
	bridge, err := New(materialPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"github_get_repository","arguments":{"owner":"bad/owner","repository":"repository"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"github_get_repository","arguments":{"owner":"approved","repository":"repository"}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err = bridge.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"code":-32602`) || !strings.Contains(output.String(), `"isError":true`) || strings.Contains(output.String(), materialPath) {
		t.Fatalf("unsafe or incomplete failure output: %s", output.String())
	}
}
