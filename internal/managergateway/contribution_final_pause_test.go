package managergateway

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/delivery"
)

var errPauseFinalContribution = errors.New("controlled pause before canonical terminal write")

// Fault injection stops BEFORE the real adapter finalizer; it does not claim
// success or supply evidence. Preclosure still calls the real embedded verifier.
type pauseFinalContribution struct {
	*app.ContributionCompletionAdapter
}

func (pauseFinalContribution) FinalizeContributionCompletion(context.Context, delivery.Reference) error {
	return errPauseFinalContribution
}
