package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
)

// doerRoles are controller-selected ports. Model replies can supply only data;
// no role may select its authority, branch table, verifier or writable scope.
type doerRoles struct {
	Admit     func(context.Context, string) error
	Gate      func(context.Context, string) (LayaGate, error)
	Implement func(context.Context, string, string, uint16) (implementation.Proposal, error)
	Apply     func(context.Context, loop.DoerContract, []implementation.Edit) error
	Judge     func(context.Context, string, string) (LayaVerdict, error)
	Verify    func(context.Context) (evidence.SelectedFileResult, error)
	Diagnose  func(context.Context, string, string, []string, uint16) (string, error)
}

type doerStepExecutor struct {
	contract           loop.DoerContract
	roles              doerRoles
	attempts           uint16
	report             string
	feedback           string
	judgment           LayaVerdict
	failure            []string
	observation        evidence.SelectedFileResult
	selectedEditDigest string
}

func (e *doerStepExecutor) Execute(ctx context.Context, request looprun.StepRequest) (looprun.StepResult, error) {
	if e == nil || e.roles.Admit == nil {
		return looprun.StepResult{}, errors.New("Doer executor binding unavailable")
	}
	if request.Step.Kind == loop.StepTerminal && request.Step.Executable == nil {
		return looprun.StepResult{Outputs: looprun.Values{}}, nil
	}
	if request.Step.Executable == nil {
		return looprun.StepResult{}, errors.New("Doer executor binding unavailable")
	}
	operation := request.Step.Executable.Operation
	if err := e.roles.Admit(ctx, string(operation)); err != nil {
		return looprun.StepResult{}, err
	}
	result := looprun.StepResult{Outputs: looprun.Values{}}
	switch operation {
	case loop.DoerEligibility:
		if e.roles.Gate == nil || request.Step.Kind != loop.StepGate {
			return result, errors.New("typed eligibility gate unavailable")
		}
		gate, err := e.roles.Gate(ctx, e.contract.Task)
		if err != nil {
			return result, err
		}
		if gate.Specified && gate.ResultDefined {
			result.Branch = "eligible"
		} else {
			e.failure = []string{"task_or_result_missing"}
			result.Branch = "needs_input"
		}
	case loop.DoerImplement:
		if e.roles.Implement == nil || e.roles.Apply == nil || request.Step.Kind != loop.StepAction || e.attempts >= e.contract.MaxAttempts {
			return result, errors.New("bounded implementation unavailable")
		}
		e.attempts++
		proposal, err := e.roles.Implement(ctx, e.contract.Task, e.feedback, e.attempts)
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(proposal.Report) == "" || len(proposal.Report) > 65536 {
			return result, errors.New("bounded implementer report required")
		}
		selectedDigest := ""
		for _, edit := range proposal.Edits {
			if edit.Path == e.contract.VerifyFile {
				bytesDigest := sha256.Sum256(edit.Content)
				selectedDigest = "sha256:" + hex.EncodeToString(bytesDigest[:])
			}
		}
		if selectedDigest == "" {
			return result, errors.New("selected file must be edited in this Doer pass")
		}
		if err = e.roles.Apply(ctx, e.contract, proposal.Edits); err != nil {
			return result, err
		}
		e.selectedEditDigest = selectedDigest
		e.report, e.feedback = proposal.Report, ""
	case loop.DoerJudgment:
		if e.roles.Judge == nil || request.Step.Kind != loop.StepAction || e.attempts == 0 {
			return result, errors.New("typed report judgment unavailable")
		}
		judgment, err := e.roles.Judge(ctx, e.contract.Task, e.report)
		if err != nil {
			return result, err
		}
		e.judgment = judgment
	case loop.DoerVerify:
		if e.roles.Verify == nil || request.Step.Kind != loop.StepGate || e.attempts == 0 {
			return result, errors.New("independent file verification unavailable")
		}
		observation, err := e.roles.Verify(ctx)
		if err != nil {
			return result, err
		}
		e.observation = observation
		e.failure = e.failure[:0]
		if !e.judgment.Done {
			e.failure = append(e.failure, "done")
		}
		if !e.judgment.StaysInScope {
			e.failure = append(e.failure, "drift")
		}
		if !e.judgment.Fulfills {
			e.failure = append(e.failure, "fulfills")
		}
		if !e.judgment.Works {
			e.failure = append(e.failure, "works")
		}
		if !e.judgment.Practices {
			e.failure = append(e.failure, "practices")
		}
		if observation.Outcome != evidence.Passed {
			e.failure = append(e.failure, "independent_verification:"+observation.FailureCategory)
		} else if e.selectedEditDigest == "" || observation.ContentDigest != e.selectedEditDigest {
			e.failure = append(e.failure, "independent_verification:output_not_from_this_pass")
		}
		if len(e.failure) == 0 {
			result.Branch = "verified"
		} else if e.attempts < e.contract.MaxAttempts {
			result.Branch = "retry"
		} else {
			result.Branch = "exhausted"
		}
	case loop.DoerDiagnosis:
		if e.roles.Diagnose == nil || request.Step.Kind != loop.StepAction {
			return result, errors.New("bounded diagnosis unavailable")
		}
		feedback, err := e.roles.Diagnose(ctx, e.contract.Task, e.report, append([]string(nil), e.failure...), e.attempts)
		if err != nil {
			return result, err
		}
		if len(feedback) > 65536 {
			return result, errors.New("diagnosis exceeded bound")
		}
		e.feedback = feedback
	case loop.DoerCompletion:
		// The best-effort non-authoritative report runs after terminal checkpoint.
		if request.Step.Kind != loop.StepAction {
			return result, errors.New("completion action shape invalid")
		}
	default:
		return result, fmt.Errorf("unsupported Doer operation %q", operation)
	}
	return result, nil
}

var _ looprun.StepExecutor = (*doerStepExecutor)(nil)
