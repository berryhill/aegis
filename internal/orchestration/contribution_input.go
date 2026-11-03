package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// implementationInput is shared by intake preflight and the actual consumer.
// No source field is truncated or summarized. Limits count encoded UTF-8 bytes.
var errImplementationInputBound = errors.New("serialized implementation input exceeds budget")

func validateResolvedContribution(request implementation.Request) error {
	if request.Contribution == nil {
		return contribution.ValidateImplementationContext(request.Contract, nil)
	}
	c, err := contribution.Unmarshal(request.Contribution)
	if err != nil {
		return err
	}
	return contribution.ValidateImplementationContext(request.Contract, &c)
}

func implementationInput(request implementation.Request, instruction string) ([]byte, error) {
	if err := request.Contract.Validate(); err != nil {
		return nil, err
	}
	if err := validateResolvedContribution(request); err != nil {
		return nil, err
	}
	wire, err := json.Marshal(struct {
		Instruction string                 `json:"instruction"`
		Request     implementation.Request `json:"request"`
	}{instruction, request})
	if err != nil {
		return nil, err
	}
	if len(wire) > hermesruntime.MaxAttemptInputBytes {
		if request.Contract.Contribution == nil {
			return nil, errImplementationInputBound
		}
		return nil, contribution.ErrInputBound
	}
	return wire, nil
}

// ValidateContributionIntakeInput rejects unsupported initial input before the
// atomic contribution/Queue reservation. The admitted repository context is read
// with the existing no-symlink/preimage/digest policy, never by the runtime.
// All later turns repeat the exact encoded check before role/runtime dispatch.
func ValidateContributionIntakeInput(ctx context.Context, v loop.VerifiedImplementation, c contribution.Contract, p *loop.ContributionExecutionPolicy) error {
	req := implementation.Request{Contract: v, PassID: fmt.Sprintf("%s-builder-%d", c.RunID, c.CandidateBudget)}
	if v.Contribution != nil {
		req.Contribution, _ = contribution.Marshal(c)
	}
	if v.RepositoryContext != nil {
		source := v
		// The owned worktree is created only after admission. Resolve the pinned
		// preimages from the profile's already approved source repository.
		if p != nil {
			source.Workspace = p.RepositoryPath
		}
		collected, err := implementation.CollectRepositoryContext(ctx, source, v.RepositoryContext.SourceDigest)
		if err != nil {
			return err
		}
		req.RepositoryContext = collected
	}
	// not_observed is the longest closed baseline/ordinary repair packet. This
	// reserves bytes only; it is not an executed test or acceptance observation.
	observations := []implementation.CriterionObservation{}
	if p != nil {
		for _, criterion := range p.Criteria {
			observations = append(observations, implementation.CriterionObservation{Criterion: criterion, Status: implementation.CriterionNotObserved, FailureCode: "required_test_not_observed", Reproduce: criterion.Test, Expected: "required_test_pass"})
		}
	}
	req.PreviousCheck, _ = json.Marshal(observations)
	_, err := implementationInput(req, patchResponseInstruction(false))
	if errors.Is(err, errImplementationInputBound) {
		return contribution.ErrInputBound
	}
	return err
}
