package graph

import (
	"errors"
	"fmt"
)

type PublishRequest struct {
	Revision               GraphRevision         `json:"revision"`
	Validation             GraphValidationResult `json:"validation"`
	ExpectedPreviousDigest string                `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string                `json:"idempotency_key"`
}

type PublicationDecision struct {
	Idempotent bool `json:"idempotent"`
}

// ValidatePublication checks a create-only publication transaction. current is
// the latest persisted revision and existing is the record already stored at
// the candidate's identity, if either exists.
func ValidatePublication(request PublishRequest, current, existing *GraphRevision) (PublicationDecision, error) {
	if !validID(request.IdempotencyKey) {
		return PublicationDecision{}, errors.New("publication idempotency key is malformed")
	}
	if issues := validateRevision(request.Revision, true); len(issues) != 0 {
		return PublicationDecision{}, errors.New("cannot publish invalid Graph revision")
	}
	if err := validateValidationResult(request.Validation); err != nil {
		return PublicationDecision{}, fmt.Errorf("validate Graph validation result: %w", err)
	}
	if request.Validation.Outcome != ValidationValid || request.Validation.GraphID != request.Revision.GraphID || request.Validation.Revision != request.Revision.Revision || request.Validation.RevisionDigest != request.Revision.Digest || request.Validation.Validator != request.Revision.Validator {
		return PublicationDecision{}, errors.New("validation result does not bind the exact valid Graph revision")
	}
	if existing != nil {
		if existing.GraphID == request.Revision.GraphID && existing.Revision == request.Revision.Revision && existing.Digest == request.Revision.Digest {
			return PublicationDecision{Idempotent: true}, nil
		}
		return PublicationDecision{}, errors.New("create-only Graph revision conflicts with existing record")
	}
	if request.Revision.Revision == 1 {
		if current != nil || request.ExpectedPreviousDigest != "" {
			return PublicationDecision{}, errors.New("first Graph revision cannot have a predecessor")
		}
		return PublicationDecision{}, nil
	}
	if current == nil || current.GraphID != request.Revision.GraphID || current.Revision+1 != request.Revision.Revision {
		return PublicationDecision{}, errors.New("Graph revision predecessor is missing or non-contiguous")
	}
	if request.ExpectedPreviousDigest != current.Digest || request.Revision.PreviousDigest != current.Digest {
		return PublicationDecision{}, errors.New("Graph revision predecessor digest mismatch")
	}
	return PublicationDecision{}, nil
}

func Activate(current Lifecycle, revision GraphRevision) (Lifecycle, error) {
	if current.GraphID != revision.GraphID || !validID(current.GraphID) {
		return Lifecycle{}, errors.New("Graph lifecycle and revision identities do not match")
	}
	if current.State == LifecycleRetired {
		return Lifecycle{}, errors.New("retired Graph cannot be activated")
	}
	if current.State != LifecycleDraft && current.State != LifecycleActive {
		return Lifecycle{}, errors.New("invalid Graph lifecycle state")
	}
	if issues := validateRevision(revision, true); len(issues) != 0 {
		return Lifecycle{}, errors.New("invalid Graph revision cannot be activated")
	}
	return Lifecycle{GraphID: current.GraphID, State: LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}, nil
}

func Retire(current Lifecycle) (Lifecycle, error) {
	if !validID(current.GraphID) || (current.State != LifecycleDraft && current.State != LifecycleActive) {
		return Lifecycle{}, errors.New("invalid Graph lifecycle cannot be retired")
	}
	return Lifecycle{GraphID: current.GraphID, State: LifecycleRetired}, nil
}
