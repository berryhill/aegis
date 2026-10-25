package orchestration

import (
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

func (w *QueueWorker) authorizeImplementationRevision(r loop.LoopRevision, a registry.AgentRevision) error {
	if r.SchemaVersion != loop.ContributionRevisionSchemaVersion {
		return w.implementation.authorize(r.Steps, a)
	}
	// A v3 template digest does not authorize the new repository/Git/role/check
	// effects. Explicit operator host approval pins the complete v6 revision.
	if w.implementation == nil || w.contributionRoles == nil || a.Runtime.Adapter != "hermes" || a.Runtime.Runtime != "hermes-agent" || managedContributionProfile(r) != nil {
		return errors.New("managed contribution host approval required")
	}
	for _, allowed := range w.implementation.config.AuthorizedContracts {
		if allowed == r.Digest {
			return nil
		}
	}
	return errors.New("exact managed contribution revision not host approved")
}
