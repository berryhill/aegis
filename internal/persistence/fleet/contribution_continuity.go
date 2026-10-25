package fleet

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
)

// ContributionContinuityRepository shares the fleet owner and audit transaction.
// It cannot replace RuntimeBinding, retry a Queue, issue authority or release custody.
type ContributionContinuityRepository interface {
	GetContributionContinuity(context.Context, string) (contribution.Continuity, []contribution.ContinuityEvent, error)
	AppendContributionContinuity(context.Context, contribution.ContinuityEvent) (contribution.Continuity, error)
}
