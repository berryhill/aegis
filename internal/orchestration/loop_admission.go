package orchestration

import (
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

// ValidateLoopAdmission applies controller-owned implementation gates before
// promoting preparation to executable work. Runtime repeats these checks.
func (w *QueueWorker) ValidateLoopAdmission(value loop.LoopRevision, agent registry.AgentRevision) error {
	if w == nil {
		return errors.New("worker unavailable")
	}
	if _, _, err := loop.NewRevision(value); err != nil {
		return err
	}
	if _, err := executableAction(value); err != nil {
		return err
	}
	for _, step := range value.Steps {
		if step.Implementation != nil {
			if err := w.implementation.authorize([]loop.Step{step}, agent); err != nil {
				return err
			}
			if err := implementation.Preflight(*step.Implementation, w.implementation.config.GoBinary); err != nil {
				return err
			}
		}
	}
	return nil
}
