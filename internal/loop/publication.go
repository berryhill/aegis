package loop

import "errors"

type PublishRequest struct {
	Revision               LoopRevision         `json:"revision"`
	Validation             LoopValidationResult `json:"validation"`
	ExpectedPreviousDigest string               `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string               `json:"idempotency_key"`
}

type PublicationDecision struct {
	Idempotent bool `json:"idempotent"`
}

// ValidatePublication enforces create-only revision publication. previous is
// the exact prior revision, while existing is the already-published record at
// the candidate revision number. Persistence must evaluate these in one atomic
// transaction; this pure function deliberately performs no storage mutation.
func ValidatePublication(request PublishRequest, previous, existing *LoopRevision) (PublicationDecision, error) {
	if !validID(request.IdempotencyKey) {
		return PublicationDecision{}, errors.New("publication idempotency key is malformed")
	}
	if issues := validateRevision(request.Revision, true); len(issues) != 0 {
		return PublicationDecision{}, errors.New("publication revision is invalid")
	}
	if err := validateLoopValidationResult(request.Validation); err != nil {
		return PublicationDecision{}, err
	}
	if request.Validation.LoopID != request.Revision.LoopID ||
		request.Validation.Revision != request.Revision.Revision ||
		request.Validation.RevisionDigest != request.Revision.Digest ||
		request.Validation.Outcome != ValidationValid {
		return PublicationDecision{}, errors.New("publication requires a valid exact-revision validation result")
	}
	if existing != nil {
		if existing.LoopID == request.Revision.LoopID && existing.Revision == request.Revision.Revision && existing.Digest == request.Revision.Digest {
			return PublicationDecision{Idempotent: true}, nil
		}
		return PublicationDecision{}, errors.New("published Loop revision conflict")
	}
	if request.Revision.Revision == 1 {
		if previous != nil || request.ExpectedPreviousDigest != "" || request.Revision.PreviousDigest != "" {
			return PublicationDecision{}, errors.New("first Loop revision cannot name a predecessor")
		}
		return PublicationDecision{}, nil
	}
	if previous == nil || previous.LoopID != request.Revision.LoopID ||
		previous.Revision+1 != request.Revision.Revision || previous.Digest != request.ExpectedPreviousDigest ||
		request.Revision.PreviousDigest != previous.Digest {
		return PublicationDecision{}, errors.New("Loop revision predecessor does not match expected exact digest")
	}
	return PublicationDecision{}, nil
}

func Activate(current Lifecycle, revision LoopRevision) (Lifecycle, error) {
	if issues := validateRevision(revision, true); len(issues) != 0 || current.LoopID != revision.LoopID || current.State == LifecycleRetired {
		return Lifecycle{}, errors.New("cannot activate invalid, foreign, or retired Loop revision")
	}
	if current.State == LifecycleActive && current.ActiveRevision == revision.Revision && current.ActiveDigest == revision.Digest {
		return current, nil
	}
	return Lifecycle{LoopID: revision.LoopID, State: LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}, nil
}

func Retire(current Lifecycle) (Lifecycle, error) {
	if !validID(current.LoopID) || (current.State != LifecycleDraft && current.State != LifecycleActive && current.State != LifecycleRetired) {
		return Lifecycle{}, errors.New("invalid Loop lifecycle")
	}
	if current.State == LifecycleRetired {
		return current, nil
	}
	return Lifecycle{LoopID: current.LoopID, State: LifecycleRetired}, nil
}
