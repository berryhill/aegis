package contribution

import (
	"errors"

	"github.com/berryhill/aegis/internal/loop"
)

// ErrInputBound is safe to project publicly. It contains no source bytes.
var ErrInputBound = errors.New("complete encoded contribution input exceeds the 65536-byte runtime budget")
var ErrSourceResponseBound = errors.New("contribution source HTTP response exceeds 262144 bytes")
var ErrSourceIssueBound = errors.New("contribution issue exceeds title 1024-byte or body 65536-byte bound")
var ErrSourceDiscussionBound = errors.New("contribution requires complete discussion: at most 20 comments, each at most 32768 bytes")

// ValidateImplementationContext joins controller-resolved bytes to the exact
// sealed contract reference. The reference is not a URL, blob address or grant.
// Short historical bindings intentionally need no additional context member.
func ValidateImplementationContext(v loop.VerifiedImplementation, c *Contract) error {
	deny := errors.New("exact resolved contribution context required")
	if v.Validate() != nil {
		return deny
	}
	if v.Contribution == nil {
		if c != nil {
			return deny
		}
		return nil
	}
	if c == nil {
		return deny
	}
	if _, err := Marshal(*c); err != nil || v.Contribution.ID != c.RunID || v.Contribution.Digest != c.Digest {
		return deny
	}
	original := v
	original.Contribution = nil
	// Oversized bindings keep the original approved Task, not a rewritten summary.
	d, err := original.Digest()
	if err != nil || d != c.TemplateDigest || v.Task != c.Outcome || v.Acceptance != c.Acceptance || v.Workspace != c.Worktree || v.MaxPasses != c.CandidateBudget {
		return deny
	}
	return nil
}
