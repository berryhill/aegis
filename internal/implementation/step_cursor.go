package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// StepCheckpoint is one append-only controller cursor for a pinned Loop run.
// Its payload is bounded, opaque step state; it is not authority or evidence.
// The owning worker must repeat fresh admission before advancing it.
type StepCheckpoint struct {
	RunID          string          `json:"run_id"`
	RevisionDigest string          `json:"revision_digest"`
	Sequence       uint32          `json:"sequence"`
	PreviousDigest string          `json:"previous_digest,omitempty"`
	Payload        json.RawMessage `json:"payload"`
	Digest         string          `json:"digest"`
}

type StepCheckpointStore struct {
	DB             Store
	RunID          string
	RevisionDigest string
}

const maxStepCheckpoints = 4096
const maxStepCursorBytes = 64 << 10

func (s StepCheckpointStore) key(sequence uint32) []byte {
	return []byte(fmt.Sprintf("implementation/step/%s/%04d", digest([]byte(s.RunID)), sequence))
}

func (s StepCheckpointStore) validate() error {
	if s.DB == nil || s.RunID == "" || len(s.RunID) > 255 || len(s.RevisionDigest) != len("sha256:")+64 || !strings.HasPrefix(s.RevisionDigest, "sha256:") {
		return errors.New("exact step cursor custody required")
	}
	for _, char := range s.RevisionDigest[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return errors.New("exact step cursor revision digest required")
		}
	}
	return nil
}

func sealStepCheckpoint(value StepCheckpoint) (StepCheckpoint, error) {
	if value.Sequence >= maxStepCheckpoints || len(value.Payload) == 0 || len(value.Payload) > maxStepCursorBytes || !json.Valid(value.Payload) {
		return value, errors.New("bounded JSON step cursor required")
	}
	value.Digest = ""
	wire, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	value.Digest = digest(wire)
	return value, nil
}

// Load verifies the entire contiguous hash chain, not only the latest mutable
// projection. A missing first record means no cursor has been started.
func (s StepCheckpointStore) Load(ctx context.Context) (StepCheckpoint, error) {
	if err := s.validate(); err != nil {
		return StepCheckpoint{}, err
	}
	var latest StepCheckpoint
	for sequence := uint32(0); sequence < maxStepCheckpoints; sequence++ {
		if err := ctx.Err(); err != nil {
			return StepCheckpoint{}, err
		}
		wire, err := s.DB.Get(s.key(sequence))
		if errors.Is(err, ErrNotFound) {
			if sequence == 0 {
				return StepCheckpoint{}, ErrNotFound
			}
			return latest, nil
		}
		if err != nil {
			return StepCheckpoint{}, err
		}
		var record StepCheckpoint
		if err := json.Unmarshal(wire, &record); err != nil || record.RunID != s.RunID || record.RevisionDigest != s.RevisionDigest || record.Sequence != sequence || record.PreviousDigest != latest.Digest {
			return StepCheckpoint{}, errors.New("step cursor chain corrupt")
		}
		sealed, err := sealStepCheckpoint(record)
		if err != nil || sealed.Digest != record.Digest {
			return StepCheckpoint{}, errors.New("step cursor digest mismatch")
		}
		canonical, err := json.Marshal(record)
		if err != nil || string(canonical) != string(wire) {
			return StepCheckpoint{}, errors.New("step cursor noncanonical")
		}
		latest = record
	}
	return StepCheckpoint{}, errors.New("step cursor limit reached")
}

// Append uses the Store's create-only transaction as the single-winner CAS for
// one sequence. A retry with stale predecessor cannot overwrite any fact.
func (s StepCheckpointStore) Append(ctx context.Context, previous string, payload json.RawMessage) (StepCheckpoint, error) {
	if err := s.validate(); err != nil {
		return StepCheckpoint{}, err
	}
	if err := ctx.Err(); err != nil {
		return StepCheckpoint{}, err
	}
	latest, err := s.Load(ctx)
	var sequence uint32
	if errors.Is(err, ErrNotFound) {
		if previous != "" {
			return StepCheckpoint{}, errors.New("step cursor predecessor mismatch")
		}
	} else if err != nil {
		return StepCheckpoint{}, err
	} else {
		if latest.Digest != previous || latest.Sequence+1 >= maxStepCheckpoints {
			return StepCheckpoint{}, errors.New("step cursor predecessor mismatch")
		}
		sequence = latest.Sequence + 1
	}
	record, err := sealStepCheckpoint(StepCheckpoint{RunID: s.RunID, RevisionDigest: s.RevisionDigest, Sequence: sequence, PreviousDigest: previous, Payload: append(json.RawMessage(nil), payload...)})
	if err != nil {
		return StepCheckpoint{}, err
	}
	wire, err := json.Marshal(record)
	if err != nil {
		return StepCheckpoint{}, err
	}
	if err := s.DB.Create(s.key(sequence), wire); err != nil {
		return StepCheckpoint{}, err
	}
	stored, err := s.DB.Get(s.key(sequence))
	if err != nil || string(stored) != string(wire) {
		return StepCheckpoint{}, errors.New("step cursor commit uncertain")
	}
	return record, nil
}

// History reconstructs a verified exact-attempt checkpoint sequence. It
// exposes opaque cursor payloads only to the controller, never as authority.
func (s StepCheckpointStore) History(ctx context.Context) ([]StepCheckpoint, error) {
	last, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]StepCheckpoint, 0, last.Sequence+1)
	previous := ""
	for index := uint32(0); index <= last.Sequence; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wire, err := s.DB.Get(s.key(index))
		if err != nil {
			return nil, err
		}
		var fact StepCheckpoint
		if err := json.Unmarshal(wire, &fact); err != nil || fact.PreviousDigest != previous || fact.Sequence != index {
			return nil, errors.New("step history chain corrupt")
		}
		previous = fact.Digest
		result = append(result, fact)
	}
	return result, nil
}
