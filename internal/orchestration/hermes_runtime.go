package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func resolveStepLane(rev loop.LoopRevision, stepID, nodeProvider, nodeModel string, hermesCfg core.HermesConfig) (string, string) {
	provider, model := rev.StepLane(stepID)
	if provider == "" && model == "" {
		provider, model = strings.TrimSpace(nodeProvider), strings.TrimSpace(nodeModel)
	}
	if lane, err := hermesCfg.ResolveLane(provider, model); err == nil {
		return lane.Provider, lane.Model
	}
	return provider, model
}

func resolveActionStepLane(rev loop.LoopRevision, nodeProvider, nodeModel string, hermesCfg core.HermesConfig) (string, string) {
	for _, step := range rev.Steps {
		if step.Kind == loop.StepAction {
			return resolveStepLane(rev, step.ID, nodeProvider, nodeModel, hermesCfg)
		}
	}
	return resolveStepLane(rev, "", nodeProvider, nodeModel, hermesCfg)
}

// RuntimeFailure preserves the adapter-observed terminal class without
// treating runtime narration as authoritative completion.
type RuntimeFailure struct {
	State  execution.State
	Reason string
	Cause  error
}

func (failure *RuntimeFailure) Error() string {
	return fmt.Sprintf("%s: %v", failure.Reason, failure.Cause)
}
func (failure *RuntimeFailure) Unwrap() error { return failure.Cause }

// RoutedRuntimeAdapter preserves no-key as an explicit registered adapter while
// routing Hermes-bound participants to the bounded Hermes turn implementation.
type RoutedRuntimeAdapter struct {
	hermes    *hermesruntime.Adapter
	noKey     NoKeyAdapter
	stateRoot string
}

func NewRoutedRuntimeAdapter(adapter *hermesruntime.Adapter, stateRoot string) (*RoutedRuntimeAdapter, error) {
	if adapter == nil || stateRoot == "" || !filepath.IsAbs(stateRoot) {
		return nil, errors.New("Hermes adapter and absolute fleet runtime state root are required")
	}
	return &RoutedRuntimeAdapter{hermes: adapter, stateRoot: stateRoot}, nil
}

func (adapter *RoutedRuntimeAdapter) Execute(ctx context.Context, request RuntimeRequest) (RuntimeResult, error) {
	for _, step := range request.LoopRevision.Steps {
		if step.Implementation != nil {
			return RuntimeResult{}, errors.New("verified implementation requires controller checker and completion custody; ordinary runtime output cannot satisfy it")
		}
	}
	switch request.Participant.Runtime.Adapter {
	case "no-key":
		return adapter.noKey.Execute(ctx, request)
	case "hermes":
		if request.Participant.Runtime.Runtime != "hermes-agent" || request.Launch.AuthorityContext.Runtime.Runtime != "hermes-agent" || request.Admission == nil {
			return RuntimeResult{}, errors.New("exact registered Hermes runtime binding is required")
		}
		input, err := json.Marshal(request.Inputs)
		if err != nil {
			return RuntimeResult{}, err
		}
		provider, model := resolveActionStepLane(request.LoopRevision, request.NodeProvider, request.NodeModel, request.Launch.AuthorityContext.Authority.Hermes)
		result, err := adapter.hermes.AttemptTurn(ctx, hermesruntime.AttemptTurnRequest{
			Launch: request.Launch, Admission: request.Admission,
			AttemptID: request.LoopExecutionID + ":turn", ParentAttemptID: request.Launch.ParentDispatch.ID,
			StateRoot: adapter.stateRoot, Input: string(input),
			Model: model, Provider: provider,
			Bounds: hermesruntime.AttemptBounds{InputBytes: hermesruntime.MaxAttemptInputBytes, OutputBytes: hermesruntime.MaxAttemptOutputBytes, Duration: hermesruntime.MaxAttemptDuration},
		})
		if err != nil {
			state, reason := result.Attempt.State, result.Attempt.Reason
			if state == "" {
				state, reason = execution.StateFailed, "runtime_unavailable_or_binding_invalid"
				if errors.Is(err, hermesruntime.ErrAttemptDenied) {
					state, reason = execution.StateDenied, "runtime_admission_denied"
				} else if errors.Is(err, context.Canceled) {
					state, reason = execution.StateCancelled, "runtime_cancelled"
				} else if errors.Is(err, context.DeadlineExceeded) {
					state, reason = execution.StateFailed, "runtime_timeout"
				}
			}
			return RuntimeResult{}, &RuntimeFailure{State: state, Reason: reason, Cause: err}
		}
		if result.Attempt.Reason != "turn_completed" || result.Output == "" {
			return RuntimeResult{}, errors.New("Hermes turn did not produce bounded successful output")
		}
		return RuntimeResult{Output: []byte(result.Output), MediaType: "text/plain"}, nil
	default:
		return RuntimeResult{}, errors.New("registered runtime adapter is unsupported")
	}
}

var _ RuntimeAdapter = (*RoutedRuntimeAdapter)(nil)
