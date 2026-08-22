package console

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

type commandAuthorityStub struct {
	binding CommandAuthorityBinding
	err     error
}

func (s *commandAuthorityStub) ResolveCommandAuthority(context.Context, core.Subject, string, string) (CommandAuthorityBinding, error) {
	return s.binding, s.err
}

type commandFixture struct {
	now       time.Time
	subject   core.Subject
	sessionID string
	target    CommandTargetState
	authority *commandAuthorityStub
	service   *CommandService
	commits   int
}

func newCommandFixture(t *testing.T) *commandFixture {
	t.Helper()
	fixture := &commandFixture{
		now:       time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
		subject:   core.Subject{ID: "subject-1", PrincipalID: "principal-1", ExpiresAt: time.Date(2026, 8, 22, 12, 10, 0, 0, time.UTC)},
		sessionID: "session-1",
		target:    CommandTargetState{ID: "agent-1", Type: "agent", Digest: digestBytes([]byte("target-v1"))},
	}
	fixture.authority = &commandAuthorityStub{binding: CommandAuthorityBinding{
		SubjectID:       fixture.subject.ID,
		SessionID:       fixture.sessionID,
		StanzaID:        "stanza-1",
		MandateID:       "mandate-1",
		AuthorityID:     "authority-1",
		AuthorityDigest: digestBytes([]byte("authority-v1")),
		Runtime:         "hermes",
		ExpiresAt:       fixture.now.Add(5 * time.Minute),
	}}
	definition := CommandDefinition{
		ID:                   "agent.retire",
		Version:              "v1",
		TargetType:           "agent",
		AuthorityRequirement: "agent.admin",
		ConfirmationClass:    "destructive",
		ReplayPolicy:         "exactly-once-readback",
		ResultType:           "agent.receipt",
		StableReasonCodes:    []string{"agent_retired"},
		Timeout:              time.Second,
		MaxBodyBytes:         1024,
		Normalize: func(input json.RawMessage) ([]byte, error) {
			var request struct {
				Reason string `json:"reason"`
			}
			if err := DecodeCommandRequest(input, &request); err != nil || request.Reason == "" {
				return nil, ErrInvalidInput
			}
			return json.Marshal(request)
		},
		ResolveTarget: func(context.Context, string) (CommandTargetState, error) {
			return fixture.target, nil
		},
		Commit: func(_ context.Context, invocation CommandInvocation) (CommandReceipt, error) {
			fixture.commits++
			if invocation.Subject.ID != fixture.subject.ID || invocation.SessionID != fixture.sessionID || invocation.Authority != fixture.authority.binding {
				return CommandReceipt{}, errors.New("commit received untrusted invocation context")
			}
			return CommandReceipt{
				SchemaVersion: CommandCatalogVersion,
				IntentID:      invocation.IntentID,
				CommandID:     invocation.CommandID,
				Target:        invocation.Target,
				Outcome:       "committed",
				ReasonCode:    "agent_retired",
				CommittedAt:   fixture.now,
				Readback:      json.RawMessage(`{"id":"agent-1","state":"retired"}`),
			}, nil
		},
	}
	var err error
	fixture.service, err = NewCommandService([]CommandDefinition{definition}, fixture.authority, func() time.Time { return fixture.now })
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *commandFixture) preview(t *testing.T, key string) CommandPreview {
	t.Helper()
	preview, err := f.service.Preview(context.Background(), f.subject, f.sessionID, CommandPreviewRequest{
		SchemaVersion:  CommandCatalogVersion,
		CommandID:      "agent.retire",
		TargetID:       f.target.ID,
		ExpectedDigest: f.target.Digest,
		IdempotencyKey: key,
		Input:          json.RawMessage(`{"reason":"operator request"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestCommandIntentAcceptsBoundedSlashAndColonTarget(t *testing.T) {
	fixture := newCommandFixture(t)
	fixture.target.ID = "42:tenant:team/review"
	preview := fixture.preview(t, "target-grammar")
	if preview.Target.ID != fixture.target.ID {
		t.Fatalf("target=%q", preview.Target.ID)
	}
}

func TestCommandIntentCommitsOnceAndReturnsAuthoritativeReadback(t *testing.T) {
	fixture := newCommandFixture(t)
	preview := fixture.preview(t, "request-1")
	if preview.AuthorityState != "admitted" || preview.CommandVersion != "v1" || !preview.ExpiresAt.Equal(fixture.now.Add(CommandIntentTTLMax)) {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	request := CommandExecuteRequest{SchemaVersion: CommandCatalogVersion, IntentID: preview.IntentID}
	first, err := fixture.service.Execute(context.Background(), fixture.subject, fixture.sessionID, request)
	if err != nil {
		t.Fatal(err)
	}
	first.Readback[0] = '['
	second, err := fixture.service.Execute(context.Background(), fixture.subject, fixture.sessionID, request)
	if err != nil {
		t.Fatal(err)
	}
	if fixture.commits != 1 || string(second.Readback) != `{"id":"agent-1","state":"retired"}` || second.Outcome != "committed" {
		t.Fatalf("duplicate execution was not exact authoritative readback: commits=%d receipt=%+v", fixture.commits, second)
	}

	replayedPreview := fixture.preview(t, "request-1")
	if replayedPreview.IntentID != preview.IntentID {
		t.Fatalf("same idempotency request created another intent: first=%s second=%s", preview.IntentID, replayedPreview.IntentID)
	}
	_, err = fixture.service.Preview(context.Background(), fixture.subject, fixture.sessionID, CommandPreviewRequest{
		SchemaVersion: CommandCatalogVersion, CommandID: "agent.retire", TargetID: fixture.target.ID,
		ExpectedDigest: fixture.target.Digest, IdempotencyKey: "request-1", Input: json.RawMessage(`{"reason":"changed"}`),
	})
	if !errors.Is(err, ErrCommandConflict) || fixture.commits != 1 {
		t.Fatalf("changed idempotency reuse did not conflict without mutation: err=%v commits=%d", err, fixture.commits)
	}
}

func TestCommandIntentConcurrentConfirmationCommitsExactlyOnce(t *testing.T) {
	fixture := newCommandFixture(t)
	preview := fixture.preview(t, "request-concurrent")
	request := CommandExecuteRequest{SchemaVersion: CommandCatalogVersion, IntentID: preview.IntentID}
	const confirmations = 8
	var wait sync.WaitGroup
	errorsSeen := make(chan error, confirmations)
	for index := 0; index < confirmations; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			receipt, err := fixture.service.Execute(context.Background(), fixture.subject, fixture.sessionID, request)
			if err == nil && receipt.Outcome != "committed" {
				err = errors.New("non-committed replay receipt")
			}
			errorsSeen <- err
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fixture.commits != 1 {
		t.Fatalf("concurrent confirmations committed %d times", fixture.commits)
	}
}

func TestCommandIntentCommitUncertaintyIsTerminalAndNeverMutatesTwice(t *testing.T) {
	tests := map[string]func(*commandFixture) func(context.Context, CommandInvocation) (CommandReceipt, error){
		"repository failure": func(f *commandFixture) func(context.Context, CommandInvocation) (CommandReceipt, error) {
			return func(context.Context, CommandInvocation) (CommandReceipt, error) {
				f.commits++
				return CommandReceipt{}, errors.New("repository unavailable after dispatch")
			}
		},
		"audit failure after mutation": func(f *commandFixture) func(context.Context, CommandInvocation) (CommandReceipt, error) {
			return func(context.Context, CommandInvocation) (CommandReceipt, error) {
				f.commits++
				return CommandReceipt{}, errors.New("audit publication uncertain")
			}
		},
		"cancellation after mutation": func(f *commandFixture) func(context.Context, CommandInvocation) (CommandReceipt, error) {
			return func(context.Context, CommandInvocation) (CommandReceipt, error) {
				f.commits++
				return CommandReceipt{}, context.Canceled
			}
		},
		"malformed receipt after mutation": func(f *commandFixture) func(context.Context, CommandInvocation) (CommandReceipt, error) {
			return func(context.Context, CommandInvocation) (CommandReceipt, error) {
				f.commits++
				return CommandReceipt{SchemaVersion: CommandCatalogVersion, Outcome: "committed"}, nil
			}
		},
	}
	for name, replacement := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newCommandFixture(t)
			definition := fixture.service.definitions["agent.retire"]
			definition.Commit = replacement(fixture)
			fixture.service.definitions[definition.ID] = definition
			preview := fixture.preview(t, "uncertain-outcome")
			request := CommandExecuteRequest{SchemaVersion: CommandCatalogVersion, IntentID: preview.IntentID}
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := fixture.service.Execute(context.Background(), fixture.subject, fixture.sessionID, request); !errors.Is(err, ErrCommandFailed) {
					t.Fatalf("attempt %d returned non-terminal error: %v", attempt+1, err)
				}
			}
			if fixture.commits != 1 {
				t.Fatalf("uncertain commit was invoked %d times", fixture.commits)
			}
		})
	}
}

func TestCommandIntentRevalidatesAuthorityIdentityExpiryAndTargetBeforeMutation(t *testing.T) {
	tests := map[string]struct {
		change func(*commandFixture) (core.Subject, string)
		want   error
	}{
		"subject changed": {change: func(f *commandFixture) (core.Subject, string) {
			subject := f.subject
			subject.ID = "subject-2"
			return subject, f.sessionID
		}, want: ErrDenied},
		"session changed": {change: func(f *commandFixture) (core.Subject, string) { return f.subject, "session-2" }, want: ErrDenied},
		"intent expired": {change: func(f *commandFixture) (core.Subject, string) {
			f.now = f.now.Add(CommandIntentTTLMax)
			return f.subject, f.sessionID
		}, want: ErrCommandExpired},
		"subject expired": {change: func(f *commandFixture) (core.Subject, string) {
			subject := f.subject
			subject.ExpiresAt = f.now
			return subject, f.sessionID
		}, want: ErrCommandExpired},
		"target changed": {change: func(f *commandFixture) (core.Subject, string) {
			f.target.Digest = digestBytes([]byte("target-v2"))
			return f.subject, f.sessionID
		}, want: ErrCommandConflict},
		"authority revoked": {change: func(f *commandFixture) (core.Subject, string) {
			f.authority.err = ErrDenied
			return f.subject, f.sessionID
		}, want: ErrDenied},
		"authority changed": {change: func(f *commandFixture) (core.Subject, string) {
			f.authority.binding.AuthorityDigest = digestBytes([]byte("authority-v2"))
			return f.subject, f.sessionID
		}, want: ErrDenied},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newCommandFixture(t)
			preview := fixture.preview(t, "request-denial")
			subject, sessionID := test.change(fixture)
			_, err := fixture.service.Execute(context.Background(), subject, sessionID, CommandExecuteRequest{SchemaVersion: CommandCatalogVersion, IntentID: preview.IntentID})
			if !errors.Is(err, test.want) || fixture.commits != 0 {
				t.Fatalf("unsafe execution: err=%v want=%v commits=%d", err, test.want, fixture.commits)
			}
		})
	}
}

func TestCommandIntentRejectsBrowserAuthorityClaimsAndAmbiguousDecoding(t *testing.T) {
	for _, input := range []string{
		`{"reason":"x","actor":"forged"}`,
		`{"reason":"x","nested":{"mandate_id":"forged"}}`,
		`{"reason":"x","items":[{"runtime":"forged"}]}`,
		`{"reason":"x","audit-disposition":"success"}`,
	} {
		fixture := newCommandFixture(t)
		_, err := fixture.service.Preview(context.Background(), fixture.subject, fixture.sessionID, CommandPreviewRequest{
			SchemaVersion: CommandCatalogVersion, CommandID: "agent.retire", TargetID: fixture.target.ID,
			ExpectedDigest: fixture.target.Digest, IdempotencyKey: "request-forged", Input: json.RawMessage(input),
		})
		if !errors.Is(err, ErrInvalidInput) || fixture.commits != 0 {
			t.Fatalf("browser authority claim accepted: input=%s err=%v", input, err)
		}
	}

	var preview CommandPreviewRequest
	if err := DecodeCommandRequest([]byte(`{"schema_version":"v","command_id":"a","command_id":"b","target_id":"t","expected_digest":"d","idempotency_key":"i","input":{}}`), &preview); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate JSON command key accepted: %v", err)
	}
	if err := DecodeCommandForm([]byte("schema_version=v&intent_id=i&intent_id=j"), &CommandExecuteRequest{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate form command key accepted: %v", err)
	}
}
