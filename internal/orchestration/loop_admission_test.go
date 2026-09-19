package orchestration

import (
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
	"testing"
)

func TestLoopAdmissionRejectsUnsupportedEvidenceFreeAction(t *testing.T) {
	_, repository, _, _, _, _ := fleetServiceFixture(t)
	value := repository.loop
	value.RequiredEvidence = nil
	value.Digest = ""
	// This shape is valid as a definition but cannot be executed by the narrow worker.
	value, _, err := loop.NewRevision(value)
	if err != nil {
		t.Fatal(err)
	}
	worker := &QueueWorker{}
	if err := worker.ValidateLoopAdmission(value, registry.AgentRevision{}); err == nil {
		t.Fatal("unsupported zero-evidence work admitted")
	}
}
