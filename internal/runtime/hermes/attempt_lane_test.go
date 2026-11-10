package hermes

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func TestAttemptTurnResolvesCharterLaneEnvelope(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
[ -n "$HERMES_MODEL" ] && [ "$HERMES_MODEL" = "$HERMES_TUI_MODEL" ] || exit 91
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-lane"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"runtime-session-lane","payload":{}}}'
out="${HERMES_TUI_PROVIDER}:${HERMES_MODEL}"
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-lane","payload":{"delta":"%s"}}}\n' "$out"
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"runtime-session-lane","payload":{"status":"complete","text":"%s"}}}\n' "$out"
while read rest; do :; done
`)

	withEnvelope := func() AttemptTurnRequest {
		req := validAttemptTurnRequest(root)
		lanes := []core.HermesLane{
			{ID: "default", Provider: "none", Model: "proof-no-key"},
			{ID: "fast", Provider: "none", Model: "proof-fast-lane"},
		}
		req.Launch.Mandate.Hermes.AllowedLanes = lanes
		req.Launch.AuthorityContext.Authority.Hermes.AllowedLanes = lanes
		req.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(req.Launch.AuthorityContext)
		return req
	}

	t.Run("defaults empty request lane to charter default", func(t *testing.T) {
		req := withEnvelope()
		req.AttemptID = "turn-default"
		req.Provider = ""
		req.Model = ""
		res, err := adapter.AttemptTurn(context.Background(), req)
		if err != nil {
			t.Fatalf("AttemptTurn failed: %v", err)
		}
		if res.Output != "none:proof-no-key" {
			t.Fatalf("output = %q, want none:proof-no-key", res.Output)
		}
	})

	t.Run("selects authorized secondary lane explicitly", func(t *testing.T) {
		req := withEnvelope()
		req.AttemptID = "turn-secondary"
		req.Provider = "none"
		req.Model = "proof-fast-lane"
		res, err := adapter.AttemptTurn(context.Background(), req)
		if err != nil {
			t.Fatalf("AttemptTurn failed: %v", err)
		}
		if res.Output != "none:proof-fast-lane" || res.Attempt.Provider != "none" || res.Attempt.Model != "proof-fast-lane" {
			t.Fatalf("output=%q attempt=%+v, want none:proof-fast-lane", res.Output, res.Attempt)
		}
	})

	t.Run("selects authorized secondary lane by unambiguous model", func(t *testing.T) {
		req := withEnvelope()
		req.AttemptID = "turn-model-only"
		req.Provider = ""
		req.Model = "proof-fast-lane"
		res, err := adapter.AttemptTurn(context.Background(), req)
		if err != nil {
			t.Fatalf("AttemptTurn failed: %v", err)
		}
		if res.Output != "none:proof-fast-lane" || res.Attempt.Provider != "none" || res.Attempt.Model != "proof-fast-lane" {
			t.Fatalf("output=%q attempt=%+v, want none:proof-fast-lane", res.Output, res.Attempt)
		}
	})

	t.Run("denies lane outside charter envelope", func(t *testing.T) {
		req := withEnvelope()
		req.AttemptID = "turn-denied"
		req.Provider = "none"
		req.Model = "unapproved-lane"
		_, err := adapter.AttemptTurn(context.Background(), req)
		if !errors.Is(err, ErrAttemptDenied) {
			t.Fatalf("err = %v, want ErrAttemptDenied", err)
		}
	})
}
