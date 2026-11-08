package app

import (
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/loop"
	"slices"
)

// contributionDeliveryFiles repeats the executor's exact sealed binding. The
// published engineering template is not the issue-specific writable scope, and
// planner-selected inputs alone omit controller-derived generated output pairs.
// BindLoop independently validates both against immutable definition policy.
func contributionDeliveryFiles(revision loop.LoopRevision, c contribution.Contract) ([]string, error) {
	bound, err := contribution.BindLoop(revision, c)
	if err != nil || len(bound.WritableFiles) == 0 {
		return nil, delivery.ErrDenied
	}
	return slices.Clone(bound.WritableFiles), nil
}
