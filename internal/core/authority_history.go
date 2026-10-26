package core

import (
	"context"
	"time"
)

// AuthorityHistoryReader reconstructs immutable evidence at a recorded time.
// Its projection is not current execution admission, cannot issue a grant and
// must never replace AuthorityAdmission/AuthorityReadiness at an effect boundary.
// Implementations verify the entire canonical history before replaying a prefix.
type AuthorityHistoryReader interface {
	HistoricalAuthorityProjection(context.Context, string, string, time.Time) (AuthorityProjection, error)
}
