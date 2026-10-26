package badger

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
)

var _ core.AuthorityHistoryReader = (*Store)(nil)

// HistoricalAuthorityProjection is an evidence-only read. Current effect
// admission remains replay-verified against the current projection and clock.
func (s *Store) HistoricalAuthorityProjection(ctx context.Context, id, digest string, at time.Time) (core.AuthorityProjection, error) {
	var out core.AuthorityProjection
	err := s.withView(ctx, func(txn *badgerdb.Txn) error {
		authority, err := authorityContextFromTxn(txn, id)
		if err != nil {
			return err
		}
		if digest == "" || authority.Digest != digest || at.IsZero() || at.Before(authority.IssuedAt) || !at.Before(authority.ExpiresAt) {
			return fmt.Errorf("%w: historical context interval mismatch", ErrCorruptRecord)
		}
		// Validate the complete retained history, including later revocation,
		// rather than trusting a caller-selected prefix or stale projection.
		if _, err := verifiedProjectionFromTxn(txn, id); err != nil {
			return err
		}
		commands, facts, err := canonicalAuthorityFromTxn(txn, id)
		if err != nil {
			return err
		}
		end := sort.Search(len(facts), func(i int) bool { return facts[i].OccurredAt.After(at) })
		if end == 0 {
			return fmt.Errorf("%w: no canonical authority at recorded time", ErrCorruptRecord)
		}
		out, err = core.ReplayCanonicalAuthority(commands[:end], facts[:end])
		return err
	})
	return out, err
}
