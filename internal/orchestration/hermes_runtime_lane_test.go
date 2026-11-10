package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
)

func TestRoutedRuntimeAdapterResolvesStepAndNodeLanes(t *testing.T) {
	root := t.TempDir()
	adapter := routedHermesTestAdapter(t, root, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
IFS= read -r tools || exit 1
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
IFS= read -r create || exit 0
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"queue-lane-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
out="${HERMES_TUI_PROVIDER}:${HERMES_MODEL}"
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"queue-lane-session","payload":{}}}\n'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"queue-lane-session","payload":{"delta":"%s"}}}\n' "$out"
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"queue-lane-session","payload":{"status":"complete","text":"%s"}}}\n' "$out"
while read rest; do :; done
`)

	makeReq := func(id string) RuntimeRequest {
		req := routedRuntimeRequest(root)
		req.LoopExecutionID = id
		lanes := []core.HermesLane{
			{Provider: "none", Model: "proof-no-key"},
			{Provider: "none", Model: "node-lane"},
			{Provider: "none", Model: "step-lane"},
		}
		req.Launch.Mandate.Hermes.AllowedLanes = lanes
		req.Launch.AuthorityContext.Authority.Hermes.AllowedLanes = lanes
		req.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(req.Launch.AuthorityContext)
		req.LoopRevision = loop.LoopRevision{
			EntryStepID: "act",
			Steps: []loop.Step{
				{ID: "act", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}},
				{ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}},
			},
		}
		return req
	}

	t.Run("step lane overrides node lane and charter default", func(t *testing.T) {
		req := makeReq("exec-step")
		req.NodeProvider, req.NodeModel = "none", "node-lane"
		req.LoopRevision.Steps[0].Provider = "none"
		req.LoopRevision.Steps[0].Model = "step-lane"
		res, err := adapter.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if string(res.Output) != "none:step-lane" {
			t.Fatalf("output = %q, want none:step-lane", string(res.Output))
		}
	})

	t.Run("node lane overrides charter default when step lane omitted", func(t *testing.T) {
		req := makeReq("exec-node")
		req.NodeProvider, req.NodeModel = "none", "node-lane"
		res, err := adapter.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}
		if string(res.Output) != "none:node-lane" {
			t.Fatalf("output = %q, want none:node-lane", string(res.Output))
		}
	})

	t.Run("unauthorized step lane fails closed as runtime_admission_denied", func(t *testing.T) {
		req := makeReq("exec-denied")
		req.LoopRevision.Steps[0].Provider = "none"
		req.LoopRevision.Steps[0].Model = "forbidden-model"
		_, err := adapter.Execute(context.Background(), req)
		var failure *RuntimeFailure
		if !errors.As(err, &failure) || failure.State != execution.StateDenied || failure.Reason != "runtime_admission_denied" {
			t.Fatalf("err = %v, want RuntimeFailure StateDenied/runtime_admission_denied", err)
		}
	})
}

func TestDoerAndContributionLaneResolution(t *testing.T) {
	w := &QueueWorker{}
	authority := core.EffectiveAuthority{
		Hermes: core.HermesConfig{
			Provider: "openai-codex",
			Model:    "gpt-5.4",
			ProviderAuthentication: &core.ProviderAuthentication{
				Mode: core.ControllerCodexAccessTokenV1,
			},
			AllowedLanes: []core.HermesLane{
				{Provider: "openai-codex", Model: "gpt-5.4", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}},
				{Provider: "none", Model: "local-fast"},
				{Provider: "none", Model: "local-deep"},
			},
		},
	}
	rev := loop.LoopRevision{
		Steps: []loop.Step{
			{ID: "implement", Kind: loop.StepAction, Executable: &loop.DoerStepBinding{Backend: "hermes", Provider: "none", Model: "local-deep"}},
			{ID: "diagnosis", Kind: loop.StepAction, Executable: &loop.DoerStepBinding{Backend: "hermes", Provider: "none", Model: "local-fast"}},
			{ID: "completion", Kind: loop.StepAction, Executable: &loop.DoerStepBinding{Backend: "hermes", Provider: "none", Model: "local-fast"}},
		},
	}
	if err := w.checkDoerProviderAuthorityForLoop(context.Background(), authority, rev, "", ""); err != nil {
		t.Fatalf("expected explicit ready step lanes to pass without unused codex token: %v", err)
	}

	revBad := rev
	revBad.Steps = append([]loop.Step(nil), rev.Steps...)
	revBad.Steps[1] = loop.Step{ID: "diagnosis", Kind: loop.StepAction, Executable: &loop.DoerStepBinding{Backend: "hermes", Provider: "none", Model: "unauthorized"}}
	if err := w.checkDoerProviderAuthorityForLoop(context.Background(), authority, revBad, "", ""); err == nil {
		t.Fatal("expected unauthorized Doer step lane to be rejected")
	}

	role := ContributionRole{
		Provider: "",
		Model:    "local-deep",
		Launch: execution.LaunchContract{
			AuthorityContext: core.AuthorityContext{
				ID:        "auth-role",
				Authority: authority,
			},
		},
	}
	id := contributionIdentity(role, "rt-1", "turn-1")
	if id.Provider != "none" || id.Model != "local-deep" {
		t.Fatalf("contributionIdentity lane = (%q, %q), want (none, local-deep)", id.Provider, id.Model)
	}
}
