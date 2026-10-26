package badger

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// These canonical records contain evidence references, never the process-local
// CompletionProvenance seal (Completion explicitly excludes it from JSON).
type graphNodeRecord struct {
	Schema  string          `json:"schema_version"`
	Digest  string          `json:"digest"`
	Payload json.RawMessage `json:"payload"`
}

func marshalGraphNodeRecord(schema string, value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(append([]byte(schema+"\n"), payload...))
	return json.Marshal(graphNodeRecord{schema, fmt.Sprintf("sha256:%x", sum), payload})
}

func unmarshalGraphNodeRecord(wire []byte, schema string, value any) error {
	var record graphNodeRecord
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || record.Schema != schema {
		return fleet.ErrCorrupt
	}
	decoder = json.NewDecoder(bytes.NewReader(record.Payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return fleet.ErrCorrupt
	}
	canonical, err := marshalGraphNodeRecord(schema, value)
	// Exact canonical comparison also rejects duplicates, trailing JSON, null,
	// omitted required fields, and payload/digest drift at any nesting depth.
	if err != nil || !bytes.Equal(canonical, wire) {
		return fleet.ErrCorrupt
	}
	return nil
}

const graphNodeCheckpointSchema = "aegis.execution.graph-node-checkpoint.v1"

func validateGraphNodeAdmission(a fleet.GraphNodeAdmission) error {
	if a.SchemaVersion != fleet.GraphNodeSchemaVersion || (a.MaxImplementationPasses < 1 || a.MaxImplementationPasses > 2) || a.Attempt.ClaimID != a.Claim.ClaimID || a.Attempt.QueueItem != a.Claim.QueueItem || a.Attempt.LoopExecutionID != a.Execution.LoopExecutionID || a.Attempt.GraphRunID != a.Execution.GraphRunID || a.Attempt.AttemptNumber != 1 || !a.Attempt.CreatedAt.Equal(a.AdmittedAt) || a.AdmittedAt.Before(a.Claim.ClaimedAt) || !a.AdmittedAt.Before(a.Claim.ExpiresAt) {
		return fleet.ErrCorrupt
	}
	if _, err := execution.MarshalLoopExecution(a.Execution); err != nil {
		return fleet.ErrCorrupt
	}
	if _, err := execution.MarshalAttempt(a.Attempt); err != nil {
		return fleet.ErrCorrupt
	}
	return nil
}
