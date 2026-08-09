// Package disposition owns terminal decisions over exact execution records.
package disposition

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/reference"
)

const SchemaVersion = "aegis.disposition.v1"

// Record references evidence instead of copying or interpreting it.
type Record struct {
	SchemaVersion   string              `json:"schema_version"`
	DispositionID   string              `json:"disposition_id"`
	GraphRunID      string              `json:"graph_run_id"`
	LoopExecutionID string              `json:"loop_execution_id"`
	AttemptID       string              `json:"attempt_id"`
	QueueItem       reference.DigestRef `json:"queue_item"`
	Authority       reference.DigestRef `json:"authority"`
	State           execution.State     `json:"state"`
	ReasonCode      string              `json:"reason_code"`
	ArtifactIDs     []string            `json:"artifact_ids"`
	ReceiptIDs      []string            `json:"receipt_ids"`
	OccurredAt      time.Time           `json:"occurred_at"`
	Digest          string              `json:"digest"`
}

func New(value Record) (Record, error) {
	value.SchemaVersion = SchemaVersion
	value.Digest = ""
	if err := validate(value, false); err != nil {
		return Record{}, err
	}
	wire, _ := json.Marshal(value)
	sum := sha256.Sum256(wire)
	value.Digest = "sha256:" + hex.EncodeToString(sum[:])
	if err := validate(value, true); err != nil {
		return Record{}, err
	}
	return value, nil
}

func Marshal(value Record) ([]byte, error) {
	if err := validate(value, true); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func Unmarshal(wire []byte) (Record, error) {
	var value Record
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := validate(value, true); err != nil {
		return value, err
	}
	return value, nil
}

func validate(value Record, sealed bool) error {
	terminal := value.State == execution.StateSucceeded || value.State == execution.StateFailed || value.State == execution.StateDenied || value.State == execution.StateCancelled || value.State == execution.StateExpired
	if value.SchemaVersion != SchemaVersion || value.DispositionID == "" || value.GraphRunID == "" || value.LoopExecutionID == "" || value.AttemptID == "" || value.QueueItem.Validate() != nil || value.Authority.Validate() != nil || !terminal || value.ReasonCode == "" || value.OccurredAt.IsZero() {
		return errors.New("invalid disposition")
	}
	if value.State == execution.StateSucceeded && len(value.ArtifactIDs) == 0 {
		return errors.New("successful disposition requires an artifact")
	}
	if sealed {
		copy := value
		copy.Digest = ""
		wire, _ := json.Marshal(copy)
		sum := sha256.Sum256(wire)
		if value.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
			return errors.New("disposition digest mismatch")
		}
	} else if value.Digest != "" {
		return errors.New("unsealed disposition contains digest")
	}
	return nil
}
