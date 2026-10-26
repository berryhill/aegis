package orchestration

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// Fixture-only interception of actual durable reads in the managed runner. The
// producer still executes through the synthetic Hermes protocol and checkpoints
// real verifier evidence. No production state or provider is used.
type managedBoundaryRepository struct {
	fleet.Repository
	fleet.GraphNodeRepository
	scenario        string
	produced, fired bool
	reads           int
	nodeReads       int
	corruptRef      string
	afterProducer   func()
}

func (r *managedBoundaryRepository) ImplementationStore() implementation.Store {
	return r.Repository.(interface{ ImplementationStore() implementation.Store }).ImplementationStore()
}
func (r *managedBoundaryRepository) CheckpointGraphNode(ctx context.Context, c fleet.GraphNodeCheckpoint, f fleet.AuditFact, b fleet.EvidenceReader) error {
	if err := r.GraphNodeRepository.CheckpointGraphNode(ctx, c, f, b); err != nil {
		return err
	}
	if c.Admission.Execution.GraphNodeID == "producer" && c.Completion.Artifact != nil {
		r.produced = true
		if r.afterProducer != nil {
			r.afterProducer()
		}
	}
	return nil
}
func (r *managedBoundaryRepository) GetLoopExecution(ctx context.Context, id string) (execution.LoopExecution, error) {
	child, err := r.Repository.GetLoopExecution(ctx, id)
	if err == nil && r.produced && id == "child" {
		r.nodeReads++
		if r.scenario == "wrong-node" && r.nodeReads == 2 {
			child.GraphNodeID = "same-bytes-wrong-node"
			r.fired = true
		}
	}
	return child, err
}
func (r *managedBoundaryRepository) GetRuntimeArtifact(ctx context.Context, id string) (evidence.RuntimeArtifact, error) {
	a, err := r.Repository.GetRuntimeArtifact(ctx, id)
	if err != nil || !r.produced || id != "artifact" {
		return a, err
	}
	r.reads++
	target := 2 // the first reload AFTER the initial verified handoff
	if r.scenario == "corrupt-later-admission" {
		target = 4
	}
	if r.scenario == "corrupt-pre-effect" {
		target = 6
	}
	if r.scenario == "corrupt-post-write" {
		target = 7
	}
	if r.scenario == "corrupt-evidence" {
		target = 13
	}
	if r.reads != target {
		return a, err
	}
	switch r.scenario {
	case "wrong-run":
		a.RunID = "same-bytes-wrong-run"
		r.fired = true
	case "wrong-attempt":
		a.AttemptID = "same-bytes-wrong-attempt"
		r.fired = true
	case "wrong-node":
		a.OwnerID = "same-bytes-wrong-owner"
		r.fired = true
	default:
		if strings.HasPrefix(r.scenario, "corrupt-") {
			r.corruptRef = a.ContentRef
			r.fired = true
		}
	}
	return a, err
}

type managedBoundaryBlobs struct {
	BlobStore
	boundary *managedBoundaryRepository
}

func (b *managedBoundaryBlobs) GetBlob(ref string) ([]byte, error) {
	if b.boundary.corruptRef != "" && ref == b.boundary.corruptRef {
		return []byte("substituted artifact bytes"), nil
	}
	return b.BlobStore.GetBlob(ref)
}
func TestManagedGraphNegativeBoundariesSynthetic(t *testing.T) {
	for _, scenario := range []string{"wrong-run", "wrong-attempt", "wrong-node", "corrupt-after-initial", "corrupt-later-admission", "corrupt-pre-effect", "corrupt-post-write", "corrupt-evidence", "cancelled", "deadline", "authority-expired", "authority-revoked", "host-denied", "pass-cap-denied", "lease-expired", "duplicate-concurrent"} {
		t.Run(scenario, func(t *testing.T) { testQueueWorkerChainScenario(t, true, true, scenario) })
	}
}
