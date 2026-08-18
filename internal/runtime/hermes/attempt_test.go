package hermes

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

type attemptAdmission struct {
	allowed bool
	digest  string
	err     error
}

func (a attemptAdmission) CheckRuntimeAdmission(_ context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	if a.err != nil {
		return execution.AdmissionDecision{}, a.err
	}
	digest := a.digest
	if digest == "" {
		digest = launch.AuthorityContext.Digest
	}
	return execution.AdmissionDecision{
		Allowed: a.allowed, AuthorityContextID: launch.AuthorityContext.ID,
		AuthorityContextDigest: digest, CheckedAt: at, Reason: "test",
	}, nil
}

func TestAttemptTurnExecutesOneBoundedGatewayTurn(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-1"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"runtime-session-1","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-1","payload":{"delta":"bounded "}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-1","payload":{"delta":"answer"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"runtime-session-1","payload":{}}}'
while read rest; do :; done
`)
	request := validAttemptTurnRequest(root)
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

func TestAttemptTurnIgnoresEventsFromUnrelatedGatewaySession(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"selected-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"status":"streaming"}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"unrelated-session","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"unrelated-session","payload":{"text":"hostile output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"unrelated-session","payload":{"text":"hostile output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"text":"uncorrelated output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","payload":{"text":"uncorrelated output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"selected-session","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"selected-session","payload":{"text":"selected output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"selected-session","payload":{}}}'
while read rest; do :; done
`)
	result, err := adapter.AttemptTurn(context.Background(), validAttemptTurnRequest(root))
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "selected output" || result.Attempt.RuntimeSessionID != "selected-session" {
		t.Fatalf("uncorrelated gateway output was accepted: %#v", result)
	}
}

func TestAttemptTurnFailsClosedBeforeRuntimeExecution(t *testing.T) {
	tests := []struct {
		name string
		edit func(*AttemptTurnRequest)
	}{
		{"missing admission source", func(r *AttemptTurnRequest) { r.Admission = nil }},
		{"tampered authority digest", func(r *AttemptTurnRequest) { r.Launch.AuthorityContext.Digest = "sha256:tampered" }},
		{"denied live admission", func(r *AttemptTurnRequest) { r.Admission = attemptAdmission{allowed: false} }},
		{"stale projected admission digest", func(r *AttemptTurnRequest) { r.Admission = attemptAdmission{allowed: true, digest: "sha256:stale"} }},
		{"mismatched parent dispatch", func(r *AttemptTurnRequest) { r.ParentAttemptID = "unknown-dispatch" }},
		{"wrong authority parent", func(r *AttemptTurnRequest) { r.Launch.ParentDispatch.AuthorityContextID = "other-authority" }},
		{"missing authority model", func(r *AttemptTurnRequest) {
			r.Launch.Mandate.Hermes.Model = ""
			r.Launch.AuthorityContext.Authority.Hermes.Model = ""
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			r.Model = ""
		}},
		{"missing authority provider", func(r *AttemptTurnRequest) {
			r.Launch.Mandate.Hermes.Provider = ""
			r.Launch.AuthorityContext.Authority.Hermes.Provider = ""
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			r.Provider = ""
		}},
		{"mismatched selected model", func(r *AttemptTurnRequest) { r.Model = "other-model" }},
		{"mismatched selected provider", func(r *AttemptTurnRequest) { r.Provider = "other-provider" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			request := validAttemptTurnRequest(root)
			test.edit(&request)
			adapter := New(filepath.Join(root, "must-not-run"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, err := adapter.AttemptTurn(context.Background(), request)
			if !errors.Is(err, ErrAttemptDenied) {
				t.Fatalf("error = %v, want ErrAttemptDenied", err)
			}
			if _, statErr := os.Stat(filepath.Join(root, "state", "runtime")); !os.IsNotExist(statErr) {
				t.Fatalf("runtime was prepared before denial: %v", statErr)
			}
		})
	}
}

func TestAttemptTurnEnforcesInputAndOutputBounds(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		root := t.TempDir()
		request := validAttemptTurnRequest(root)
		request.Bounds.InputBytes = 3
		request.Input = "four"
		adapter := New(filepath.Join(root, "must-not-run"), slog.New(slog.NewTextHandler(io.Discard, nil)))
		_, err := adapter.AttemptTurn(context.Background(), request)
		if !errors.Is(err, ErrAttemptInputBound) {
			t.Fatalf("error = %v, want ErrAttemptInputBound", err)
		}
	})

	t.Run("output", func(t *testing.T) {
		adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-2"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"runtime-session-2","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"runtime-session-2","payload":{"delta":"too long"}}}'
while read rest; do :; done
`)
		request := validAttemptTurnRequest(root)
		request.Bounds.OutputBytes = 3
		result, err := adapter.AttemptTurn(context.Background(), request)
		if !errors.Is(err, ErrAttemptOutputBound) {
			t.Fatalf("error = %v, want ErrAttemptOutputBound", err)
		}
		if result.Attempt.State != execution.StateFailed || result.Attempt.Reason != "output_bound_exceeded" || result.Output != "" {
			t.Fatalf("result = %#v", result)
		}
	})
}

func TestAttemptTurnHonorsDurationAndCallerCancellation(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-3"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
sleep 10
`)
	request := validAttemptTurnRequest(root)
	request.Bounds.Duration = 50 * time.Millisecond
	started := time.Now()
	result, err := adapter.AttemptTurn(context.Background(), request)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
	if result.Attempt.State != execution.StateCancelled || result.Attempt.Reason != "turn_cancelled_or_timed_out" {
		t.Fatalf("attempt = %#v", result.Attempt)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("duration bound did not terminate process promptly: %s", elapsed)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = adapter.AttemptTurn(cancelled, validAttemptTurnRequest(root))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled error = %v, want context canceled", err)
	}
}

func attemptTestAdapter(t *testing.T, gatewayScript string) (*Adapter, string) {
	t.Helper()
	root := t.TempDir()
	installation := filepath.Join(root, "install")
	if err := os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	hermesExecutable := filepath.Join(root, "hermes")
	hermesScript := "#!/bin/sh\necho 'Hermes Agent v0.18.2'\necho 'Install directory: " + installation + "'\n"
	if err := os.WriteFile(hermesExecutable, []byte(hermesScript), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(gatewayScript), 0700); err != nil {
		t.Fatal(err)
	}
	return New(hermesExecutable, slog.New(slog.NewTextHandler(io.Discard, nil))), root
}

func validAttemptTurnRequest(root string) AttemptTurnRequest {
	now := time.Now().UTC()
	issuedAt := now.Add(-time.Minute)
	expiresAt := now.Add(5 * time.Minute)
	runtime := core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.18.2"}
	mandate := core.Mandate{
		ID: "mandate-1", Subject: core.Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: issuedAt, ExpiresAt: expiresAt},
		AgentID: "agent-1", StanzaID: "principal", CharterRevision: 1, CharterDigest: "sha256:charter",
		Runtime: runtime, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"},
		Scopes: core.Scopes{}, Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "proof-no-key", Provider: "none"}, IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}
	authority := core.AuthorityContext{
		ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: mandate.Subject.ID,
		AgentID: mandate.AgentID, CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest,
		Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Hermes: mandate.Hermes},
		IssuedAt: issuedAt, ExpiresAt: expiresAt,
	}
	authority.Digest = core.AuthorityContextDigest(authority)
	dispatchStarted := now.Add(-30 * time.Second)
	dispatchFinished := now.Add(-29 * time.Second)
	dispatch := execution.Dispatch{
		ID: "dispatch-1", AuthorityContextID: authority.ID, State: execution.StateSucceeded,
		RequestedAt: now.Add(-31 * time.Second), StartedAt: &dispatchStarted, FinishedAt: &dispatchFinished,
	}
	return AttemptTurnRequest{
		Launch:    execution.LaunchContract{OwnerID: "owner-1", Mandate: mandate, AuthorityContext: authority, ParentDispatch: dispatch},
		Admission: attemptAdmission{allowed: true}, AttemptID: "turn-1", ParentAttemptID: dispatch.ID,
		StateRoot: filepath.Join(root, "state"), Input: "hello", Model: mandate.Hermes.Model, Provider: mandate.Hermes.Provider,
		Bounds: AttemptBounds{InputBytes: 1024, OutputBytes: 1024, Duration: time.Second},
	}
}

func TestAppendBoundedCountsBytesAcrossDeltas(t *testing.T) {
	var output strings.Builder
	if err := appendBounded(&output, "é", 3); err != nil {
		t.Fatal(err)
	}
	if err := appendBounded(&output, "é", 3); !errors.Is(err, ErrAttemptOutputBound) {
		t.Fatalf("error = %v, want ErrAttemptOutputBound", err)
	}
	if output.String() != "é" {
		t.Fatalf("partial over-bound output was appended: %q", output.String())
	}
}
