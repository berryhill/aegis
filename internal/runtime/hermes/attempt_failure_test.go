package hermes

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAttemptTurnClosedFailureSurface(t *testing.T) {
	for _, tc := range []struct{ name, kind, payload, want string }{
		{"classified completion", "message.complete", `{"status":"error","text":"private runtime text","error":"private error","error_surface":{"layer":"auth","code":"auth_permanent","provider":"private provider","model":"private model","retryable":true}}`, "runtime_auth_auth_permanent"},
		{"classified event", "error", `{"message":"private exception","error_surface":{"layer":"provider","code":"format_error"}}`, "runtime_provider_format_error"},
		{"unknown code", "error", `{"error_surface":{"layer":"provider","code":"private-code"}}`, "runtime_turn_failed"},
		{"wrong pair", "error", `{"error_surface":{"layer":"auth","code":"format_error"}}`, "runtime_turn_failed"},
		{"malformed", "error", `{"error_surface":{"layer":[],"code":false}}`, "runtime_turn_failed"},
		{"missing", "error", `{"message":"private text"}`, "runtime_turn_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"failure-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"failure-session","payload":{}}}'
` + fmt.Sprintf("printf '%%s\\n' '%s'\n", fmt.Sprintf(`{"jsonrpc":"2.0","method":"event","params":{"type":%q,"session_id":"failure-session","payload":%s}}`, tc.kind, tc.payload)) + "while read rest; do :; done\n"
			a, root := attemptTestAdapter(t, script)
			result, err := a.AttemptTurn(context.Background(), validAttemptTurnRequest(root))
			if err == nil || result.Attempt.Reason != tc.want || result.Output != "" {
				t.Fatalf("failure not retained safely: reason=%s err=%v", result.Attempt.Reason, err)
			}
			if strings.Contains(fmt.Sprint(result, err), "private") {
				t.Fatal("raw failure escaped")
			}
		})
	}
}
