package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// VerifyLoopPublication is read-only reconciliation, never PublishLoop replay.
// Historical request bindings include the original audit subject, not today's
// session. Rejoin that immutable event rather than minting new provenance or
// accepting a revision digest without its original request key.
func (s *Store) VerifyLoopPublication(ctx context.Context, request loop.PublishRequest) error {
	rw, err := loop.MarshalRevision(request.Revision)
	if err != nil {
		return err
	}
	vw, err := loop.MarshalLoopValidationResult(request.Validation)
	if err != nil {
		return err
	}
	pw, err := loop.MarshalPublicationProvenance(request.Provenance)
	if err != nil {
		return err
	}
	return s.view(ctx, func(tx *badgerdb.Txn) error {
		bound, found, err := optional(tx, key(familyLoopRequest, request.IdempotencyKey))
		if err != nil {
			return err
		}
		if !found {
			return fleet.ErrConflict
		}
		if err = verifyLoopPublicationBundle(tx, request, rw, vw, pw); err != nil {
			return err
		}
		matched := false
		// Stream existing audit records; do not create a new history index or load
		// unbounded audit history into a single record/slice.
		err = scan(tx, familyAudit, func(_ []byte, wire []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			var event core.AuditEvent
			if err := json.Unmarshal(wire, &event); err != nil {
				return fleet.ErrCorrupt
			}
			if event.Type != "fleet.loop.published" || event.PrincipalID != request.Provenance.PrincipalID || event.AgentID != request.Provenance.PublisherAgent.ID || event.MandateID != request.Provenance.MandateID {
				return nil
			}
			event.ID, event.OccurredAt, event.PreviousDigest, event.EventDigest = "", time.Time{}, "", ""
			want, err := requestBinding(rw, vw, pw, request.ExpectedPreviousDigest, request.IdempotencyKey, event)
			if err != nil {
				return err
			}
			if bytes.Equal(bound, want) {
				matched = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !matched {
			return fleet.ErrConflict
		}
		return nil
	})
}
