package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

// ExecutionDebugger is a bounded content-free exact-run read model. Definition
// context and interpreter observations confer neither authority nor completion.
type ExecutionDebugger struct {
	QueueID        string                `json:"queue_id"`
	GraphRunID     string                `json:"graph_run_id"`
	SnapshotDigest string                `json:"snapshot_digest"`
	Source         reference.RevisionRef `json:"source"`
	Direct         bool                  `json:"direct"`
	State          string                `json:"state"`
	Terminal       bool                  `json:"terminal"`
	DispositionID  string                `json:"disposition_id,omitempty"`
	ArtifactIDs    []string              `json:"artifact_ids,omitempty"`
	ReceiptIDs     []string              `json:"receipt_ids,omitempty"`
	Watermark      string                `json:"watermark"`
	Version        uint64                `json:"version"`
	ObservedAt     time.Time             `json:"observed_at"`
	Notice         string                `json:"notice"`
	Nodes          []DebuggerLoop        `json:"nodes"`
	Dependencies   []DebuggerEdge        `json:"dependencies"`
}
type DebuggerLoop struct {
	NodeID      string                `json:"node_id"`
	Loop        reference.RevisionRef `json:"loop"`
	Participant reference.RevisionRef `json:"participant"`
	ExecutionID string                `json:"execution_id,omitempty"`
	Steps       []DebuggerStep        `json:"steps"`
	Edges       []DebuggerEdge        `json:"edges"`
	Attempts    []DebuggerAttempt     `json:"attempts"`
}
type DebuggerStep struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Terminal string `json:"terminal,omitempty"`
}
type DebuggerEdge struct {
	Condition string `json:"condition,omitempty"`
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Limit     uint16 `json:"limit,omitempty"`
}
type DebuggerAttempt struct {
	ID     string         `json:"id"`
	Number uint32         `json:"number"`
	Facts  []DebuggerFact `json:"facts"`
}
type DebuggerFact struct {
	Sequence uint32 `json:"sequence"`
	Ordinal  uint32 `json:"ordinal"`
	Digest   string `json:"digest"`
	orchestration.DoerObservation
}

// ExecutionDebuggerAs reads only this item's exact references and transition
// chain. It does not enumerate the fleet or expose cursor/task/model payloads.
// A cursor is bound to the immutable snapshot and Queue identity, never latest.
func (s *Service) ExecutionDebuggerAs(ctx context.Context, subject core.Subject, id, cursor string) (ExecutionDebugger, error) {
	var out ExecutionDebugger
	if err := s.requireFleetPrincipalIdentity(subject); err != nil {
		return out, err
	}
	item, err := s.FleetRepository.GetQueueItem(ctx, id)
	if err != nil {
		return out, err
	}
	snapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
	if err != nil || snapshot.Digest != item.Snapshot.Digest {
		return out, fleet.ErrCorrupt
	}
	topology, err := s.Fleet.ResolveRunTopology(ctx, snapshot)
	if err != nil {
		return out, err
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, id)
	if err != nil {
		return out, err
	}
	transitions, err := s.FleetRepository.ListQueueTransitions(ctx, id)
	if err != nil {
		return out, err
	}
	binding := sha256.Sum256([]byte(id + "\x00" + snapshot.Digest))
	prefix := hex.EncodeToString(binding[:]) + ":"
	if cursor != "" {
		if len(cursor) != len(prefix)+64 || cursor[:len(prefix)] != prefix {
			return out, errors.New("invalid exact-run cursor")
		}
		if _, err := hex.DecodeString(cursor[len(prefix):]); err != nil {
			return out, errors.New("invalid exact-run cursor")
		}
	}
	out = ExecutionDebugger{QueueID: id, GraphRunID: item.GraphRunID, SnapshotDigest: snapshot.Digest, Source: snapshot.Graph, State: string(projection.State), ObservedAt: time.Now().UTC(), Notice: "Interpreter terminal is not evidence-backed Queue disposition. Missing boundaries in legacy history remain unknown."}
	// A direct envelope has no published Graph identity.
	out.Direct = snapshot.SchemaVersion == graph.DirectRunSnapshotSchemaVersion
	if out.Direct {
		out.Source = topology.Nodes[0].Loop
	}
	if len(topology.Nodes) > 1 {
		out.Notice += " Multi-node execution is unsupported by the bounded worker (#317); dependencies are definition context, not verified handoffs."
	}
	out.Nodes = []DebuggerLoop{}
	for _, node := range topology.Nodes {
		rev, err := s.FleetRepository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
		if err != nil || rev.Digest != node.Loop.Digest {
			return out, fleet.ErrCorrupt
		}
		n := DebuggerLoop{NodeID: node.ID, Loop: node.Loop, Participant: node.Participant, Steps: []DebuggerStep{}, Edges: []DebuggerEdge{}, Attempts: []DebuggerAttempt{}}
		for _, step := range rev.Steps {
			d := DebuggerStep{ID: step.ID, Kind: string(step.Kind)}
			if step.Terminal != nil {
				d.Terminal = string(step.Terminal.Outcome)
			}
			n.Steps = append(n.Steps, d)
		}
		for _, edge := range rev.Transitions {
			n.Edges = append(n.Edges, DebuggerEdge{ID: edge.ID, From: edge.FromStepID, To: edge.ToStepID, Limit: edge.MaxTraversals, Condition: edge.Condition})
		}
		out.Nodes = append(out.Nodes, n)
	}
	for _, dep := range topology.Dependencies {
		out.Dependencies = append(out.Dependencies, DebuggerEdge{ID: dep.ID, From: dep.FromNodeID, To: dep.ToNodeID})
	}
	seen := map[string]bool{}
	for _, transition := range transitions {
		if transition.ClaimID == "" || seen[transition.ClaimID] {
			continue
		}
		seen[transition.ClaimID] = true
		claim, err := s.FleetRepository.GetClaim(ctx, transition.ClaimID)
		if err != nil || claim.QueueItem.ID != id || claim.QueueItem.Digest != item.Digest {
			return out, fleet.ErrCorrupt
		}
		attempt, err := s.FleetRepository.GetAttempt(ctx, claim.AttemptID)
		if err != nil || attempt.GraphRunID != item.GraphRunID || attempt.QueueItem != claim.QueueItem || attempt.ClaimID != claim.ClaimID {
			return out, fleet.ErrCorrupt
		}
		child, err := s.FleetRepository.GetLoopExecution(ctx, attempt.LoopExecutionID)
		if err != nil || child.GraphRunID != item.GraphRunID {
			return out, fleet.ErrCorrupt
		}
		matched := false
		for i := range out.Nodes {
			n := &out.Nodes[i]
			if n.NodeID != child.GraphNodeID {
				continue
			}
			if n.Loop != child.Loop || n.Participant != child.Participant {
				return out, fleet.ErrCorrupt
			}
			matched = true
			n.ExecutionID = child.LoopExecutionID
			a := DebuggerAttempt{ID: attempt.AttemptID, Number: attempt.AttemptNumber, Facts: []DebuggerFact{}}
			trace, err := s.QueueWorker.DoerTrace(ctx, attempt.AttemptID, child.Loop.Digest)
			if err != nil {
				return out, err
			}
			stepIDs := map[string]bool{}
			edges := map[string]DebuggerEdge{}
			for _, step := range n.Steps {
				stepIDs[step.ID] = true
			}
			for _, edge := range n.Edges {
				edges[edge.ID] = edge
			}
			for _, checkpoint := range trace {
				for ordinal, o := range checkpoint.Observations {
					if !stepIDs[o.StepID] {
						return out, fleet.ErrCorrupt
					}
					if o.Kind == "transition" {
						edge, ok := edges[o.TransitionID]
						if !ok || edge.From != o.StepID || edge.To != o.ToStepID {
							return out, fleet.ErrCorrupt
						}
					}
					a.Facts = append(a.Facts, DebuggerFact{Sequence: checkpoint.Sequence, Ordinal: uint32(ordinal), Digest: checkpoint.Digest, DoerObservation: o})
				}
			}
			// Count checkpoints, not observations: old history still advances watermark.
			out.Version += uint64(len(trace))
			n.Attempts = append(n.Attempts, a)
		}
		if !matched {
			return out, fleet.ErrCorrupt
		}
	}
	for i := range out.Nodes {
		sort.Slice(out.Nodes[i].Attempts, func(a, b int) bool { return out.Nodes[i].Attempts[a].Number < out.Nodes[i].Attempts[b].Number })
	}
	disposition, err := s.FleetRepository.GetDispositionByGraphRun(ctx, item.GraphRunID)
	if err == nil {
		if disposition.QueueItem.ID != id || disposition.QueueItem.Digest != item.Digest {
			return out, fleet.ErrCorrupt
		}
		out.DispositionID = disposition.DispositionID
		out.State = string(disposition.State)
		out.Terminal = true
		out.ArtifactIDs = append([]string(nil), disposition.ArtifactIDs...)
		out.ReceiptIDs = append([]string(nil), disposition.ReceiptIDs...)
	} else if !errors.Is(err, fleet.ErrNotFound) {
		return out, err
	}
	out.Version += uint64(len(transitions)) * 8192
	// The fingerprint excludes read time; polling an unchanged run is stable.
	out.Watermark = ""
	out.ObservedAt = time.Time{}
	wire, _ := json.Marshal(out)
	sum := sha256.Sum256(wire)
	out.Watermark = prefix + hex.EncodeToString(sum[:])
	out.ObservedAt = time.Now().UTC()
	return out, nil
}
