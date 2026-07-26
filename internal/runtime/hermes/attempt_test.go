package hermes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
)

func TestAttemptTurnExecutesOneBoundedGatewayTurn(t *testing.T) {
	adapter, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"runtime-session-1"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"bounded "}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"answer"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","payload":{}}}'
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
	if result.Attempt.State != plumbing.AttemptSucceeded || result.Attempt.RuntimeAttemptID != "runtime-session-1" {
		t.Fatalf("attempt = %#v", result.Attempt)
	}
	if result.Attempt.StartedAt == nil || result.Attempt.FinishedAt == nil || result.Attempt.FinishedAt.Before(*result.Attempt.StartedAt) {
		t.Fatalf("attempt timestamps are incomplete or reversed: %#v", result.Attempt)
	}
	if result.Attempt.Provenance.Producer != plumbing.ProducerRuntimeAdapter || result.Attempt.Provenance.OwnerID != request.Aggregate.OwnerID {
		t.Fatalf("attempt provenance = %#v", result.Attempt.Provenance)
	}
	request.Aggregate.Attempts = append(request.Aggregate.Attempts, result.Attempt)
	request.Aggregate.UpdatedAt = *result.Attempt.FinishedAt
	if err = plumbing.Validate(request.Aggregate); err != nil {
		t.Fatalf("result does not form a valid plumbing aggregate: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("disposable attempt home was retained: %v", entries)
	}
}

func TestAttemptTurnFailsClosedBeforeRuntimeExecution(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		edit func(*AttemptTurnRequest)
	}{
		{"missing authority", func(r *AttemptTurnRequest) { r.Aggregate.Authority = nil }},
		{"ambiguous stanza", func(r *AttemptTurnRequest) { r.Aggregate.Decision.MatchingCount = 2 }},
		{"mismatched stanza", func(r *AttemptTurnRequest) {
			r.Aggregate.Decision.SelectedStanzaID = "teamwide"
		}},
		{"expired authority", func(r *AttemptTurnRequest) {
			r.Aggregate.Authority.ExpiresAt = now.Add(-time.Second)
			r.Aggregate.Authority.Digest = plumbing.AuthorityDigest(*r.Aggregate.Authority)
		}},
		{"revoked authority", func(r *AttemptTurnRequest) {
			revokedAt := now.Add(-time.Second)
			r.Aggregate.Revocations = []plumbing.AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: r.Aggregate.Authority.ID,
				Reason: "operator_revoked", RevokedAt: revokedAt,
				Provenance: attemptProvenance(r.Aggregate.OwnerID, "revocation:1", revokedAt),
			}}
		}},
		{"missing parent dispatch", func(r *AttemptTurnRequest) { r.ParentAttemptID = "unknown-dispatch" }},
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
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"too long"}}}'
while read rest; do :; done
`)
		request := validAttemptTurnRequest(root)
		request.Bounds.OutputBytes = 3
		result, err := adapter.AttemptTurn(context.Background(), request)
		if !errors.Is(err, ErrAttemptOutputBound) {
			t.Fatalf("error = %v, want ErrAttemptOutputBound", err)
		}
		if result.Attempt.State != plumbing.AttemptFailed || result.Attempt.Reason != "output_bound_exceeded" || result.Output != "" {
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
	if result.Attempt.State != plumbing.AttemptCancelled || result.Attempt.Reason != "turn_cancelled_or_timed_out" {
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
	createdAt := now.Add(-2 * time.Minute)
	authenticatedAt := createdAt
	ingressAt := createdAt.Add(time.Second)
	decisionAt := createdAt.Add(2 * time.Second)
	issuedAt := createdAt.Add(3 * time.Second)
	dispatchRequestedAt := createdAt.Add(4 * time.Second)
	dispatchStartedAt := createdAt.Add(5 * time.Second)
	dispatchFinishedAt := createdAt.Add(6 * time.Second)
	owner := "owner-1"
	charterDigest := attemptDigest("charter")
	authority := plumbing.AuthorityContext{
		ID: "authority-1", MandateID: "mandate-1", DecisionID: "decision-1",
		ParticipantID: "participant-1", AgentID: "agent-1", StanzaID: "principal",
		CharterRevision: 1, CharterDigest: charterDigest, Runtime: "hermes-agent", RuntimeVersion: "0.18.2",
		Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, MemoryScopes: []string{}, CredentialScopes: []string{},
		IssuedAt: issuedAt, ExpiresAt: now.Add(5 * time.Minute),
		Provenance: attemptProvenance(owner, "mandate:mandate-1", issuedAt),
	}
	authority.Digest = plumbing.AuthorityDigest(authority)
	aggregate := plumbing.Aggregate{
		SchemaVersion: plumbing.SchemaVersion, ID: "lifecycle-1", Revision: 1, OwnerID: owner,
		Participant: plumbing.Participant{
			ID: "participant-1", Kind: "human", PrincipalID: "principal-1",
			Authentication: plumbing.Authentication{EvidenceID: "auth-evidence-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: authenticatedAt, ExpiresAt: now.Add(10 * time.Minute), ClaimsDigest: attemptDigest("claims")},
			Provenance:     attemptProvenance(owner, "peercred:1", authenticatedAt),
		},
		Ingress:   plumbing.IngressFact{ID: "ingress-1", ParticipantID: "participant-1", ContactID: "contact-1", ChannelID: "channel-1", ChannelKind: "unix-socket", EndpointRef: "listener:control", ObservedAt: ingressAt, Provenance: attemptProvenance(owner, "peercred:1", ingressAt)},
		Decision:  plumbing.StanzaDecision{ID: "decision-1", ParticipantID: "participant-1", IngressFactID: "ingress-1", AgentID: "agent-1", CharterRevision: 1, CharterDigest: charterDigest, Allowed: true, MatchingCount: 1, SelectedStanzaID: "principal", Reason: "exact_authorized_match", DecidedAt: decisionAt, Provenance: attemptProvenance(owner, "selector:decision-1", decisionAt)},
		Authority: &authority,
		Attempts:  []plumbing.Attempt{{ID: "dispatch-1", Kind: plumbing.AttemptDispatch, AuthorityContextID: authority.ID, RuntimeAttemptID: "runtime-dispatch-1", State: plumbing.AttemptSucceeded, RequestedAt: dispatchRequestedAt, StartedAt: &dispatchStartedAt, FinishedAt: &dispatchFinishedAt, Provenance: attemptProvenance(owner, "dispatcher:1", dispatchFinishedAt)}},
		CreatedAt: createdAt, UpdatedAt: now,
	}
	return AttemptTurnRequest{
		Aggregate: aggregate, AttemptID: "session-attempt-2", ParentAttemptID: "dispatch-1",
		StateRoot: filepath.Join(root, "state"), Input: "hello",
		Bounds: AttemptBounds{InputBytes: 1024, OutputBytes: 1024, Duration: time.Second},
	}
}

func attemptProvenance(owner, source string, at time.Time) plumbing.Provenance {
	return plumbing.Provenance{OwnerID: owner, Producer: plumbing.ProducerControlPlane, SourceRef: source, RecordedAt: at}
}

func attemptDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
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
