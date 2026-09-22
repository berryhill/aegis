package hermes

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
)

// Installed Hermes tui_gateway/prompt_turn.py emits text/usage/status, with
// statuses complete, error, interrupted (not "completed").
func TestAttemptInstalledCompletionShapes(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		state         execution.State
	}{
		{"complete", `{"text":"hello","usage":{},"status":"complete"}`, execution.StateSucceeded},
		{"error", `{"text":"hello","usage":{},"status":"error"}`, execution.StateFailed},
		{"interrupted", `{"text":"hello","usage":{},"status":"interrupted"}`, execution.StateCancelled},
		{"missing status", `{"text":"hello"}`, execution.StateFailed},
		{"malformed status", `{"text":"hello","status":true}`, execution.StateFailed},
		{"unknown status", `{"text":"hello","status":"completed"}`, execution.StateFailed},
		{"malformed text", `{"text":{},"status":"complete"}`, execution.StateFailed},
		{"missing text", `{"status":"complete"}`, execution.StateFailed},
		{"contradictory error", `{"text":"hello","status":"complete","error":"failed"}`, execution.StateFailed},
	} {
		for _, delta := range []string{"", "hello"} {
			t.Run(fmt.Sprintf("%s/delta=%s", tc.name, delta), func(t *testing.T) {
				adapter, root := attemptTestAdapter(t, fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"method":"event","params":{"type":"gateway.ready"}}'
read tools
printf '%%s\n' '{"id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%%s\n' '{"id":"create","result":{"session_id":"installed"}}'
read prompt
printf '%%s\n' '{"method":"event","params":{"type":"message.start","session_id":"installed","payload":{}}}'
printf '%%s\n' '{"method":"event","params":{"type":"message.delta","session_id":"installed","payload":{"text":"%s"}}}'
printf '%%s\n' '{"method":"event","params":{"type":"message.complete","session_id":"installed","payload":%s}}'
while read rest; do :; done
`, delta, tc.payload))
				result, err := adapter.AttemptTurn(context.Background(), validAttemptTurnRequest(root))
				if result.Attempt.State != tc.state {
					t.Fatalf("state=%s err=%v want=%s", result.Attempt.State, err, tc.state)
				}
				if tc.state == execution.StateSucceeded {
					if err != nil || result.Output != "hello" {
						t.Fatalf("result=%+v err=%v", result, err)
					}
				} else if err == nil || result.Output != "" {
					t.Fatalf("unsuccessful output retained: %+v err=%v", result, err)
				}
			})
		}
	}
}

type completionRevocation struct{ calls int }

func (a *completionRevocation) CheckRuntimeAdmission(ctx context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	a.calls++
	return (attemptAdmission{allowed: a.calls < 3}).CheckRuntimeAdmission(ctx, launch, at)
}

func TestAttemptCompletionRechecksAuthority(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"method":"event","params":{"type":"gateway.ready"}}'
read tools
printf '%s\n' '{"id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"id":"create","result":{"session_id":"installed"}}'
read prompt
printf '%s\n' '{"method":"event","params":{"type":"message.start","session_id":"installed","payload":{}}}'
printf '%s\n' '{"method":"event","params":{"type":"message.complete","session_id":"installed","payload":{"text":"hello","status":"complete"}}}'
while read rest; do :; done
`)
	request := validAttemptTurnRequest(root)
	admission := &completionRevocation{}
	request.Admission = admission
	result, err := adapter.AttemptTurn(context.Background(), request)
	if err == nil || result.Attempt.State != execution.StateDenied || result.Output != "" || admission.calls != 3 {
		t.Fatalf("result=%+v err=%v checks=%d", result, err, admission.calls)
	}
}
