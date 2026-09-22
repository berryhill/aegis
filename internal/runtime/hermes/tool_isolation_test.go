package hermes

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
)

func TestLaunchToolsetsPreservesAuthority(t *testing.T) {
	for _, tc := range []struct {
		tools []string
		want  string
	}{
		{nil, "context_engine"}, {[]string{"no_mcp"}, "context_engine"},
		{[]string{"no_mcp", "file"}, "file"}, {[]string{"web", "terminal"}, "web,terminal"},
	} {
		if got := launchToolsets(tc.tools); got != tc.want {
			t.Fatalf("pin=%q want=%q", got, tc.want)
		}
	}
}

func TestEmptyGatewayFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		result  map[string]any
		allowed bool
	}{
		{"zero", map[string]any{"total": float64(0), "sections": []any{}}, true},
		{"fallback", map[string]any{"total": float64(26), "sections": []any{}}, false},
		{"missing", nil, false},
		{"null sections", map[string]any{"total": float64(0), "sections": nil}, false},
		{"string total", map[string]any{"total": "0", "sections": []any{}}, false},
		{"contradiction", map[string]any{"total": float64(0), "sections": []any{map[string]any{"name": "terminal"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := make(chan gatewayMessage, 1)
			messages <- gatewayMessage{ID: "aegis-tools", Result: tc.result}
			var written bytes.Buffer
			err := verifyEmptyGateway(context.Background(), &written, messages, make(chan error))
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
			if strings.Contains(written.String(), "prompt.submit") {
				t.Fatal("probe submitted prompt")
			}
		})
	}
}

func TestAttemptRejectsUnexpectedToolsBeforePrompt(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready"}}'
read request
case "$request" in
 *tools.show*) printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":26,"sections":[]}}';;
 *) exit 91;;
esac
while read request; do exit 92; done
`)
	request := validAttemptTurnRequest(root)
	request.Bounds.Duration = time.Second
	result, err := adapter.AttemptTurn(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "tool isolation") || result.Attempt.State != execution.StateDenied {
		t.Fatalf("unexpected tools must deny before prompt: state=%s err=%v", result.Attempt.State, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("must reject, not time out")
	}
}
