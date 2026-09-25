package orchestration

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/looprun"
)

// doerCursorStore uses the existing fleet-v1 implementation custody. It writes
// create-only, digest-linked facts rather than trusting a mutable cursor. A
// nonterminal interrupted attempt is not replayed automatically: effects may
// have happened between the last checkpoint and process interruption. After
// lease expiry, the existing authenticated Queue Expire operation terminalizes
// the exact active claim; no second execution or pass-budget reset is allowed.
type doerCursorStore struct {
	facts    implementation.StepCheckpointStore
	previous string
}

func (s *doerCursorStore) Load(ctx context.Context) (looprun.Cursor, error) {
	checkpoint, err := s.facts.Load(ctx)
	if errors.Is(err, implementation.ErrNotFound) {
		return looprun.Cursor{}, nil
	}
	if err != nil {
		return looprun.Cursor{}, err
	}
	var cursor looprun.Cursor
	if err := json.Unmarshal(checkpoint.Payload, &cursor); err != nil {
		return looprun.Cursor{}, errors.New("invalid persisted Doer cursor")
	}
	if cursor.Done == nil {
		return looprun.Cursor{}, errors.New("interrupted Doer step requires controller reconciliation")
	}
	s.previous = checkpoint.Digest
	return cursor, nil
}

func (s *doerCursorStore) Save(ctx context.Context, cursor looprun.Cursor) error {
	wire, err := json.Marshal(cursor)
	if err != nil {
		return err
	}
	checkpoint, err := s.facts.Append(ctx, s.previous, wire)
	if err != nil {
		return err
	}
	s.previous = checkpoint.Digest
	return nil
}

var _ looprun.CursorStore = (*doerCursorStore)(nil)
