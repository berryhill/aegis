package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/looprun"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// Synthetic stdio runtime only; not a live Queue, native tool or provider proof.
func TestRoutedRuntimeToolEventCannotProduceOutput(t *testing.T) {
	root := t.TempDir()
	adapter := routedHermesTestAdapter(t, root, `#!/bin/sh
printf '%s\n' '{"method":"event","params":{"type":"gateway.ready"}}'
read tools
printf '%s\n' '{"id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"id":"create","result":{"session_id":"selected"}}'
read prompt
printf '%s\n' '{"method":"event","params":{"type":"message.start","session_id":"selected"}}'
printf '%s\n' '{"method":"event","params":{"type":"tool.start","session_id":"selected","payload":{"name":"SYNTHETIC_PRIVATE"}}}'
printf '%s\n' '{"method":"event","params":{"type":"message.complete","session_id":"selected","payload":{"status":"complete","text":"SYNTHETIC_PRIVATE"}}}'
while read rest; do :; done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := adapter.Execute(ctx, routedRuntimeRequest(root))
	var failure *RuntimeFailure
	if !errors.As(err, &failure) || !errors.Is(err, hermesruntime.ErrAttemptDenied) || failure.State != execution.StateDenied || failure.Reason != "unexpected_tool_event" || len(result.Output) != 0 || result.MediaType != "" {
		t.Fatal("tool event denial became output or lost closed classification")
	}
	if strings.Contains(err.Error(), "SYNTHETIC_PRIVATE") {
		t.Fatal("private runtime bytes escaped routed denial")
	}
	closed := doerTurnFailure(hermesruntime.AttemptTurnResult{Attempt: execution.Turn{State: failure.State, Reason: failure.Reason}}, err)
	var packet *looprun.Failure
	if !errors.As(closed, &packet) || packet.Code != "unexpected_tool_event" || ReviewedDoerFailureCode(packet.Code) != packet.Code {
		t.Fatal("downstream closed failure vocabulary lost denial")
	}
	entries, statErr := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if statErr != nil || len(entries) != 0 {
		t.Fatal("denied routed turn retained disposable home")
	}
}
