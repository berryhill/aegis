package hermes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
)

const (
	MaxAttemptInputBytes  = 64 << 10
	MaxAttemptOutputBytes = 1 << 20
	MaxAttemptDuration    = 5 * time.Minute
)

var (
	ErrAttemptDenied      = errors.New("Hermes attempt denied")
	ErrAttemptInputBound  = errors.New("Hermes attempt input exceeds bound")
	ErrAttemptOutputBound = errors.New("Hermes attempt output exceeds bound")
)

// AttemptBounds are required per-turn limits. A caller may select smaller
// limits, but cannot raise the adapter's hard ceilings.
type AttemptBounds struct {
	InputBytes  int
	OutputBytes int
	Duration    time.Duration
}

// AttemptTurnRequest binds one Hermes turn to an already authenticated,
// exactly-one-stanza plumbing aggregate. Aggregate must be a current
// control-plane read; the adapter rechecks its authority immediately before it
// starts Hermes. Input and output bodies are deliberately absent from Attempt.
type AttemptTurnRequest struct {
	Aggregate       plumbing.Aggregate
	AttemptID       string
	ParentAttemptID string
	StateRoot       string
	Input           string
	Model           string
	Provider        string
	Credentials     []Credential
	Bounds          AttemptBounds
}

type AttemptTurnResult struct {
	Attempt plumbing.Attempt
	Output  string
}

// AttemptTurn executes exactly one prompt in a clean disposable Hermes gateway
// session. Identity, stanza selection, mandates, and revocation facts remain
// Aegis-owned inputs; Hermes only supplies untrusted response bytes.
func (a *Adapter) AttemptTurn(ctx context.Context, request AttemptTurnRequest) (AttemptTurnResult, error) {
	if err := validateAttemptRequest(request); err != nil {
		return AttemptTurnResult{}, err
	}
	authority := *request.Aggregate.Authority
	now := time.Now().UTC()
	if err := liveAuthorityCheck(request.Aggregate, now); err != nil {
		return AttemptTurnResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return AttemptTurnResult{}, err
	}
	// The deadline is never later than the caller's bound or the immutable
	// authority cutoff, and covers discovery and process setup as well as model
	// execution. Caller cancellation is inherited by the child context.
	deadline := now.Add(request.Bounds.Duration)
	if authority.ExpiresAt.Before(deadline) {
		deadline = authority.ExpiresAt
	}
	turnContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	descriptor, err := a.Discover(turnContext)
	if err != nil {
		return AttemptTurnResult{}, err
	}
	if descriptor.Runtime != authority.Runtime || descriptor.Version != authority.RuntimeVersion {
		return AttemptTurnResult{}, fmt.Errorf("%w: runtime binding does not match authority context", ErrAttemptDenied)
	}
	python := gatewayPython(descriptor)
	if python == "" {
		return AttemptTurnResult{}, errors.New("Hermes TUI gateway Python executable not found")
	}
	tools, err := ResolveTools(authority.Tools)
	if err != nil {
		return AttemptTurnResult{}, fmt.Errorf("%w: %v", ErrAttemptDenied, err)
	}
	for _, credential := range request.Credentials {
		if !containsString(authority.CredentialScopes, credential.Reference) {
			return AttemptTurnResult{}, fmt.Errorf("%w: credential %q is outside authority", ErrAttemptDenied, credential.Reference)
		}
	}

	runtimeRoot := filepath.Join(request.StateRoot, "runtime")
	if err = os.MkdirAll(runtimeRoot, 0700); err != nil {
		return AttemptTurnResult{}, err
	}
	home, err := os.MkdirTemp(runtimeRoot, "attempt-")
	if err != nil {
		return AttemptTurnResult{}, err
	}
	defer os.RemoveAll(home) //nolint:errcheck

	toolsets := "no_mcp"
	if len(tools) > 0 {
		toolsets = strings.Join(tools, ",")
	}
	command := exec.CommandContext(turnContext, python, "-m", "tui_gateway.entry")
	command.Dir = home
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(minimalEnv(home, request.Credentials),
		"HERMES_PYTHON_SRC_ROOT="+descriptor.Installation,
		"HERMES_TUI_TOOLSETS="+toolsets,
		"HERMES_TUI_SKILLS=",
		"HERMES_DISABLE_AUTO_SKILLS=1",
	)
	if request.Model != "" {
		command.Env = append(command.Env, "HERMES_TUI_MODEL="+request.Model)
	}
	if request.Provider != "" {
		command.Env = append(command.Env, "HERMES_TUI_PROVIDER="+request.Provider)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return AttemptTurnResult{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return AttemptTurnResult{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return AttemptTurnResult{}, err
	}

	// Recheck as close as possible to the authority-bearing process start. The
	// aggregate is the caller's authoritative live snapshot, not model input.
	startedAt := time.Now().UTC()
	if err = liveAuthorityCheck(request.Aggregate, startedAt); err != nil {
		return AttemptTurnResult{}, err
	}
	attempt := plumbing.Attempt{
		ID:                 request.AttemptID,
		Kind:               plumbing.AttemptSession,
		ParentAttemptID:    request.ParentAttemptID,
		AuthorityContextID: authority.ID,
		State:              plumbing.AttemptRequested,
		RequestedAt:        now,
	}
	if !plumbing.CanTransitionAttempt(attempt.State, plumbing.AttemptStarted) {
		return AttemptTurnResult{}, errors.New("invalid requested-to-started attempt transition")
	}
	attempt.State = plumbing.AttemptStarted
	attempt.StartedAt = &startedAt

	if err = command.Start(); err != nil {
		return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "runtime_start_failed", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer stopAttemptProcess(command, stdin, done)

	messages := make(chan gatewayMessage, 32)
	readErrors := make(chan error, 1)
	go readGateway(stdout, messages, readErrors)
	if _, err = waitGateway(turnContext, messages, readErrors, func(message gatewayMessage) bool {
		return message.Method == "event" && message.Params.Type == "gateway.ready"
	}); err != nil {
		return finishContextError(attempt, request.Aggregate.OwnerID, authority.ExpiresAt, turnContext, err)
	}
	if err = writeGateway(stdin, "create", "session.create", map[string]any{"cols": 100, "source": "aegis-attempt"}); err != nil {
		return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "session_create_write_failed", err)
	}
	created, err := waitGateway(turnContext, messages, readErrors, func(message gatewayMessage) bool {
		return fmt.Sprint(message.ID) == "create"
	})
	if err != nil {
		return finishContextError(attempt, request.Aggregate.OwnerID, authority.ExpiresAt, turnContext, err)
	}
	if created.Error != nil {
		return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "session_create_failed", errors.New("Hermes session.create failed"))
	}
	sessionID := fmt.Sprint(created.Result["session_id"])
	if sessionID == "" || sessionID == "<nil>" {
		sessionID = fmt.Sprint(created.Result["id"])
	}
	if sessionID == "" || sessionID == "<nil>" {
		return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "session_id_missing", errors.New("Hermes session.create returned no session ID"))
	}
	attempt.RuntimeAttemptID = sessionID
	if err = writeGateway(stdin, "prompt", "prompt.submit", map[string]any{"session_id": sessionID, "text": request.Input}); err != nil {
		return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "prompt_write_failed", err)
	}

	var output strings.Builder
	messageStarted := false
	for {
		message, waitErr := waitGateway(turnContext, messages, readErrors, func(message gatewayMessage) bool {
			return (message.Method == "event" && (message.Params.Type == "message.start" || message.Params.Type == "message.delta" || message.Params.Type == "message.complete" || message.Params.Type == "error")) || fmt.Sprint(message.ID) == "prompt"
		})
		if waitErr != nil {
			return finishContextError(attempt, request.Aggregate.OwnerID, authority.ExpiresAt, turnContext, waitErr)
		}
		if message.Error != nil {
			return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "prompt_rejected", errors.New("Hermes prompt failed"))
		}
		switch message.Params.Type {
		case "message.start":
			messageStarted = true
		case "message.delta":
			if messageStarted {
				if err = appendBounded(&output, attemptPayloadText(message.Params.Payload), request.Bounds.OutputBytes); err != nil {
					return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "output_bound_exceeded", err)
				}
			}
		case "error":
			return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "runtime_turn_failed", errors.New("Hermes turn failed"))
		case "message.complete":
			if !messageStarted {
				continue
			}
			if output.Len() == 0 {
				if err = appendBounded(&output, attemptPayloadText(message.Params.Payload), request.Bounds.OutputBytes); err != nil {
					return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptFailed, "output_bound_exceeded", err)
				}
			}
			finishedAt := time.Now().UTC()
			if !finishedAt.Before(authority.ExpiresAt) {
				return finishAttempt(attempt, request.Aggregate.OwnerID, plumbing.AttemptExpired, "authority_expired", ErrAttemptDenied)
			}
			result, finishErr := finishAttemptAt(attempt, request.Aggregate.OwnerID, plumbing.AttemptSucceeded, "turn_completed", finishedAt, nil)
			result.Output = output.String()
			return result, finishErr
		}
	}
}

func validateAttemptRequest(request AttemptTurnRequest) error {
	if err := plumbing.Validate(request.Aggregate); err != nil {
		return fmt.Errorf("%w: invalid authority aggregate: %v", ErrAttemptDenied, err)
	}
	if request.Aggregate.Authority == nil || request.Aggregate.Disposition != nil {
		return fmt.Errorf("%w: authority is missing or lifecycle is terminal", ErrAttemptDenied)
	}
	if request.Aggregate.Authority.Runtime != "hermes-agent" {
		return fmt.Errorf("%w: authority does not select Hermes", ErrAttemptDenied)
	}
	if strings.TrimSpace(request.AttemptID) == "" || len(request.AttemptID) > 255 || strings.TrimSpace(request.ParentAttemptID) == "" {
		return errors.New("attempt and parent attempt identifiers are required and bounded")
	}
	parentOK := false
	for _, attempt := range request.Aggregate.Attempts {
		if attempt.ID == request.AttemptID {
			return errors.New("attempt identifier already exists")
		}
		if attempt.ID == request.ParentAttemptID && attempt.Kind == plumbing.AttemptDispatch && attempt.State == plumbing.AttemptSucceeded && attempt.AuthorityContextID == request.Aggregate.Authority.ID {
			parentOK = true
		}
	}
	if !parentOK {
		return fmt.Errorf("%w: successful parent dispatch is missing or mismatched", ErrAttemptDenied)
	}
	if request.Bounds.InputBytes <= 0 || request.Bounds.InputBytes > MaxAttemptInputBytes || request.Bounds.OutputBytes <= 0 || request.Bounds.OutputBytes > MaxAttemptOutputBytes || request.Bounds.Duration <= 0 || request.Bounds.Duration > MaxAttemptDuration {
		return errors.New("attempt bounds are missing or exceed hard ceilings")
	}
	if len(request.Input) > request.Bounds.InputBytes {
		return ErrAttemptInputBound
	}
	if strings.TrimSpace(request.StateRoot) == "" {
		return errors.New("attempt state root is required")
	}
	return nil
}

func liveAuthorityCheck(aggregate plumbing.Aggregate, now time.Time) error {
	if aggregate.Authority == nil || !aggregate.Decision.Allowed || aggregate.Decision.MatchingCount != 1 || aggregate.Decision.SelectedStanzaID == "" {
		return fmt.Errorf("%w: authority is missing or stanza selection is not exact", ErrAttemptDenied)
	}
	authority := aggregate.Authority
	if authority.Digest != plumbing.AuthorityDigest(*authority) || now.Before(authority.IssuedAt) || !now.Before(authority.ExpiresAt) {
		return fmt.Errorf("%w: authority is invalid or expired", ErrAttemptDenied)
	}
	for _, revocation := range aggregate.Revocations {
		if revocation.AuthorityContextID == authority.ID && !now.Before(revocation.RevokedAt) {
			return fmt.Errorf("%w: authority is revoked", ErrAttemptDenied)
		}
	}
	return nil
}

func appendBounded(output *strings.Builder, value string, limit int) error {
	if len(value) > limit-output.Len() {
		return ErrAttemptOutputBound
	}
	_, _ = output.WriteString(value)
	return nil
}

func attemptPayloadText(payload map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "message"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func finishContextError(attempt plumbing.Attempt, owner string, expiresAt time.Time, ctx context.Context, err error) (AttemptTurnResult, error) {
	if ctx.Err() != nil {
		if !time.Now().UTC().Before(expiresAt) {
			return finishAttempt(attempt, owner, plumbing.AttemptExpired, "authority_expired", ctx.Err())
		}
		return finishAttempt(attempt, owner, plumbing.AttemptCancelled, "turn_cancelled_or_timed_out", ctx.Err())
	}
	return finishAttempt(attempt, owner, plumbing.AttemptFailed, "runtime_gateway_failed", err)
}

func finishAttempt(attempt plumbing.Attempt, owner string, state plumbing.AttemptState, reason string, cause error) (AttemptTurnResult, error) {
	return finishAttemptAt(attempt, owner, state, reason, time.Now().UTC(), cause)
}

func finishAttemptAt(attempt plumbing.Attempt, owner string, state plumbing.AttemptState, reason string, finishedAt time.Time, cause error) (AttemptTurnResult, error) {
	if !plumbing.CanTransitionAttempt(attempt.State, state) {
		return AttemptTurnResult{}, errors.New("invalid started-to-terminal attempt transition")
	}
	attempt.State = state
	attempt.FinishedAt = &finishedAt
	attempt.Reason = reason
	attempt.Provenance = plumbing.Provenance{
		OwnerID:    owner,
		Producer:   plumbing.ProducerRuntimeAdapter,
		SourceRef:  "hermes-attempt:" + attempt.RuntimeAttemptID,
		RecordedAt: finishedAt,
	}
	return AttemptTurnResult{Attempt: attempt}, cause
}

func stopAttemptProcess(command *exec.Cmd, stdin io.Closer, done <-chan error) {
	_ = stdin.Close()
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		<-done
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
