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

	"github.com/berryhill/aegis/internal/execution"
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

// AttemptTurnRequest binds one Hermes turn to a narrow immutable launch
// contract. Admission is consulted immediately before process start; neither
// the contract nor runtime output can select or widen authority.
type AttemptTurnRequest struct {
	Launch          execution.LaunchContract
	Admission       execution.AdmissionChecker
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
	Attempt execution.Turn
	Output  string
}

// AttemptTurn executes exactly one prompt in a clean disposable Hermes gateway
// session. Identity, stanza selection, mandates, and revocation facts remain
// Aegis-owned inputs; Hermes only supplies untrusted response bytes.
func (a *Adapter) AttemptTurn(ctx context.Context, request AttemptTurnRequest) (AttemptTurnResult, error) {
	if err := validateAttemptRequest(request); err != nil {
		return AttemptTurnResult{}, err
	}
	authority := request.Launch.AuthorityContext
	now := time.Now().UTC()
	if err := checkAdmission(ctx, request, now); err != nil {
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
	if descriptor.Runtime != authority.Runtime.Runtime || descriptor.Version != authority.Runtime.Version {
		return AttemptTurnResult{}, fmt.Errorf("%w: runtime binding does not match authority context", ErrAttemptDenied)
	}
	python := gatewayPython(descriptor)
	if python == "" {
		return AttemptTurnResult{}, errors.New("Hermes TUI gateway Python executable not found")
	}
	tools, err := ResolveTools(authority.Authority.Tools)
	if err != nil {
		return AttemptTurnResult{}, fmt.Errorf("%w: %v", ErrAttemptDenied, err)
	}
	for _, credential := range request.Credentials {
		if !containsString(authority.Authority.Credentials, credential.Reference) {
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

	// Recheck through the authoritative source as close as possible to process
	// start. Historical projections and model input cannot satisfy this gate.
	startedAt := time.Now().UTC()
	if err = checkAdmission(ctx, request, startedAt); err != nil {
		return AttemptTurnResult{}, err
	}
	attempt := execution.Turn{
		ID:                 request.AttemptID,
		DispatchID:         request.ParentAttemptID,
		AuthorityContextID: authority.ID,
		State:              execution.StateRequested,
		RequestedAt:        now,
	}
	if !execution.CanTransition(attempt.State, execution.StateStarted) {
		return AttemptTurnResult{}, errors.New("invalid requested-to-started attempt transition")
	}
	attempt.State = execution.StateStarted
	attempt.StartedAt = &startedAt

	if err = command.Start(); err != nil {
		return finishAttempt(attempt, execution.StateFailed, "runtime_start_failed", err)
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
		return finishContextError(attempt, authority.ExpiresAt, turnContext, err)
	}
	if err = writeGateway(stdin, "create", "session.create", map[string]any{"cols": 100, "source": "aegis-attempt"}); err != nil {
		return finishAttempt(attempt, execution.StateFailed, "session_create_write_failed", err)
	}
	created, err := waitGateway(turnContext, messages, readErrors, func(message gatewayMessage) bool {
		return fmt.Sprint(message.ID) == "create"
	})
	if err != nil {
		return finishContextError(attempt, authority.ExpiresAt, turnContext, err)
	}
	if created.Error != nil {
		return finishAttempt(attempt, execution.StateFailed, "session_create_failed", errors.New("Hermes session.create failed"))
	}
	sessionID := fmt.Sprint(created.Result["session_id"])
	if sessionID == "" || sessionID == "<nil>" {
		sessionID = fmt.Sprint(created.Result["id"])
	}
	if sessionID == "" || sessionID == "<nil>" {
		return finishAttempt(attempt, execution.StateFailed, "session_id_missing", errors.New("Hermes session.create returned no session ID"))
	}
	attempt.RuntimeSessionID = sessionID
	if err = writeGateway(stdin, "prompt", "prompt.submit", map[string]any{"session_id": sessionID, "text": request.Input}); err != nil {
		return finishAttempt(attempt, execution.StateFailed, "prompt_write_failed", err)
	}

	var output strings.Builder
	messageStarted := false
	for {
		message, waitErr := waitGateway(turnContext, messages, readErrors, func(message gatewayMessage) bool {
			return (message.Method == "event" && (message.Params.Type == "message.start" || message.Params.Type == "message.delta" || message.Params.Type == "message.complete" || message.Params.Type == "error")) || fmt.Sprint(message.ID) == "prompt"
		})
		if waitErr != nil {
			return finishContextError(attempt, authority.ExpiresAt, turnContext, waitErr)
		}
		if message.Error != nil {
			return finishAttempt(attempt, execution.StateFailed, "prompt_rejected", errors.New("Hermes prompt failed"))
		}
		switch message.Params.Type {
		case "message.start":
			messageStarted = true
		case "message.delta":
			if messageStarted {
				if err = appendBounded(&output, attemptPayloadText(message.Params.Payload), request.Bounds.OutputBytes); err != nil {
					return finishAttempt(attempt, execution.StateFailed, "output_bound_exceeded", err)
				}
			}
		case "error":
			return finishAttempt(attempt, execution.StateFailed, "runtime_turn_failed", errors.New("Hermes turn failed"))
		case "message.complete":
			if !messageStarted {
				continue
			}
			if output.Len() == 0 {
				if err = appendBounded(&output, attemptPayloadText(message.Params.Payload), request.Bounds.OutputBytes); err != nil {
					return finishAttempt(attempt, execution.StateFailed, "output_bound_exceeded", err)
				}
			}
			finishedAt := time.Now().UTC()
			if !finishedAt.Before(authority.ExpiresAt) {
				return finishAttempt(attempt, execution.StateExpired, "authority_expired", ErrAttemptDenied)
			}
			if err = checkAdmission(turnContext, request, finishedAt); err != nil {
				return finishAttemptAt(attempt, execution.StateDenied, "authority_no_longer_effective", finishedAt, err)
			}
			result, finishErr := finishAttemptAt(attempt, execution.StateSucceeded, "turn_completed", finishedAt, nil)
			result.Output = output.String()
			return result, finishErr
		}
	}
}

func validateAttemptRequest(request AttemptTurnRequest) error {
	if request.Admission == nil {
		return fmt.Errorf("%w: authoritative admission checker is required", ErrAttemptDenied)
	}
	if request.Launch.OwnerID == "" || request.Launch.AuthorityContext.Runtime.Runtime != "hermes-agent" {
		return fmt.Errorf("%w: immutable owner and Hermes authority are required", ErrAttemptDenied)
	}
	if err := execution.ValidateDispatch(request.Launch.ParentDispatch, request.Launch.AuthorityContext); err != nil {
		return fmt.Errorf("%w: %v", ErrAttemptDenied, err)
	}
	if request.ParentAttemptID != request.Launch.ParentDispatch.ID {
		return fmt.Errorf("%w: parent dispatch binding is mismatched", ErrAttemptDenied)
	}
	if strings.TrimSpace(request.AttemptID) == "" || len(request.AttemptID) > 255 || strings.TrimSpace(request.ParentAttemptID) == "" {
		return errors.New("attempt and parent attempt identifiers are required and bounded")
	}
	if request.AttemptID == request.ParentAttemptID {
		return errors.New("turn identifier must differ from parent dispatch")
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

func checkAdmission(ctx context.Context, request AttemptTurnRequest, now time.Time) error {
	decision, err := request.Admission.CheckRuntimeAdmission(ctx, request.Launch, now)
	if err != nil {
		return fmt.Errorf("%w: live authority check failed: %v", ErrAttemptDenied, err)
	}
	if err = execution.ValidateAdmission(request.Launch, decision, now); err != nil {
		return fmt.Errorf("%w: %v", ErrAttemptDenied, err)
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

func finishContextError(attempt execution.Turn, expiresAt time.Time, ctx context.Context, err error) (AttemptTurnResult, error) {
	if ctx.Err() != nil {
		if !time.Now().UTC().Before(expiresAt) {
			return finishAttempt(attempt, execution.StateExpired, "authority_expired", ctx.Err())
		}
		return finishAttempt(attempt, execution.StateCancelled, "turn_cancelled_or_timed_out", ctx.Err())
	}
	return finishAttempt(attempt, execution.StateFailed, "runtime_gateway_failed", err)
}

func finishAttempt(attempt execution.Turn, state execution.State, reason string, cause error) (AttemptTurnResult, error) {
	return finishAttemptAt(attempt, state, reason, time.Now().UTC(), cause)
}

func finishAttemptAt(attempt execution.Turn, state execution.State, reason string, finishedAt time.Time, cause error) (AttemptTurnResult, error) {
	if !execution.CanTransition(attempt.State, state) {
		return AttemptTurnResult{}, errors.New("invalid started-to-terminal attempt transition")
	}
	attempt.State = state
	attempt.FinishedAt = &finishedAt
	attempt.Reason = reason
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
