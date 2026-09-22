package hermes

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"os"
	"path/filepath"
	"testing"
)

func TestAttemptTurnEmptyToolsUsesRecognizedEmptyToolset(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 91
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-1"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"runtime-session-1","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-1","payload":{"delta":"bounded "}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-1","payload":{"delta":"answer"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"runtime-session-1","payload":{"status":"complete","text":"bounded answer"}}}'
while read rest; do :; done
`)
	request := validAttemptTurnRequest(root)
	request.Launch.Mandate.Tools = nil
	request.Launch.Mandate.Hermes.Toolsets = nil
	request.Launch.AuthorityContext.Authority.Tools = nil
	request.Launch.AuthorityContext.Authority.Hermes.Toolsets = nil
	request.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(request.Launch.AuthorityContext)
	result, err := adapter.AttemptTurn(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "bounded answer" {
		t.Fatalf("output = %q", result.Output)
	}
	if result.Attempt.State != execution.StateSucceeded || result.Attempt.RuntimeSessionID != "runtime-session-1" {
		t.Fatalf("attempt = %#v", result.Attempt)
	}
	if result.Attempt.StartedAt == nil || result.Attempt.FinishedAt == nil || result.Attempt.FinishedAt.Before(*result.Attempt.StartedAt) {
		t.Fatalf("attempt timestamps are incomplete or reversed: %#v", result.Attempt)
	}
	if result.Attempt.AuthorityContextID != request.Launch.AuthorityContext.ID || result.Attempt.DispatchID != request.Launch.ParentDispatch.ID {
		t.Fatalf("turn bindings = %#v", result.Attempt)
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("disposable attempt home was retained: %v", entries)
	}
}
