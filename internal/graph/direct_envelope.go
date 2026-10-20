package graph

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/reference"
)

// DirectRunSnapshotSchemaVersion discriminates new direct execution data on the
// existing repository/Queue port. Historical v1 bytes and meaning are unchanged.
const DirectRunSnapshotSchemaVersion = "aegis.graph.run-snapshot.v2"

// NewDirectRunSnapshot wraps slice1's sealed direct snapshot, copying its values.
// It does not create a Graph, establish publication, or grant runtime authority.
func NewDirectRunSnapshot(execution ExecutionSnapshot) (GraphRunSnapshot, error) {
	execution, err := canonicalExecutionSnapshot(execution)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	s := GraphRunSnapshot{SchemaVersion: DirectRunSnapshotSchemaVersion, SnapshotID: execution.SnapshotID,
		Inputs: execution.Inputs, Participants: execution.Participants, Loops: execution.Loops, Execution: &execution}
	s, err = canonicalizeSnapshotValues(s)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	s.Digest, err = digestSnapshot(s)
	if err != nil {
		return GraphRunSnapshot{}, err
	}
	if err = validateSnapshot(s); err != nil {
		return GraphRunSnapshot{}, err
	}
	return s, nil
}

func validateDirectRunSnapshot(s GraphRunSnapshot) error {
	if s.Execution == nil || s.Graph != (reference.RevisionRef{}) || s.Execution.SourceKind != SourceDirectLoop {
		return errors.New("direct envelope requires direct execution and zero Graph reference")
	}
	if err := validateExecutionSnapshot(*s.Execution); err != nil {
		return err
	}
	if s.SnapshotID != s.Execution.SnapshotID || !sameSnapshotValues(s, *s.Execution) {
		return errors.New("direct envelope and nested snapshot disagree")
	}
	digest, err := digestSnapshot(s)
	if err != nil || !validDigest(s.Digest) || digest != s.Digest {
		return errors.New("direct envelope digest mismatch")
	}
	return nil
}

func sameSnapshotValues(s GraphRunSnapshot, e ExecutionSnapshot) bool {
	a, _ := json.Marshal(struct {
		Inputs              []NormalizedInput
		Participants, Loops []reference.RevisionRef
	}{s.Inputs, s.Participants, s.Loops})
	b, _ := json.Marshal(struct {
		Inputs              []NormalizedInput
		Participants, Loops []reference.RevisionRef
	}{e.Inputs, e.Participants, e.Loops})
	return bytes.Equal(a, b)
}

// ResolveRunTopology is the shared definition resolver for persisted envelopes.
// For v2 pass an exact registered participant/published Loop interface node; for
// historical v1 pass the exact published Graph, including historical synthetic
// Graphs. Arguments must come from authoritative readback. The direct adapter is
// never a reusable Graph definition. Lifecycle and runtime admission are separate.
func ResolveRunTopology(s GraphRunSnapshot, published *GraphRevision, direct *Node) (ExecutionTopology, error) {
	s, err := canonicalizeSnapshotValues(s)
	if err != nil {
		return ExecutionTopology{}, err
	}
	if err = validateSnapshot(s); err != nil {
		return ExecutionTopology{}, err
	}
	if s.SchemaVersion == DirectRunSnapshotSchemaVersion {
		return ResolveExecutionTopology(*s.Execution, published, direct)
	}
	if direct != nil || published == nil {
		return ExecutionTopology{}, errors.New("historical snapshot requires exact published Graph")
	}
	expected, err := NewRunSnapshot(s.SnapshotID, *published, s.Inputs)
	if err != nil || expected.Digest != s.Digest {
		return ExecutionTopology{}, errors.New("historical snapshot source mismatch")
	}
	return topologyOf(*published), nil
}
