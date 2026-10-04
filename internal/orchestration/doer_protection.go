package orchestration

import (
	"github.com/berryhill/aegis/internal/loop"
	"path/filepath"
)

// SetDoerProtectedPaths is controller startup wiring, never definition/model
// input. Protection applies even to a previously configured contract allowlist.
func (w *QueueWorker) SetDoerProtectedPaths(paths []string) {
	w.doerProtectedPaths = append([]string(nil), paths...)
}

// Supplemental application scopes never replace the actual controller scopes.
func (w *QueueWorker) effectiveDoerProtectedPaths() []string {
	paths := append([]string(nil), w.doerProtectedPaths...)
	if c := w.implementation; c != nil {
		if c.root != "" {
			paths = append(paths, filepath.Dir(filepath.Dir(c.root)))
		}
		for _, p := range []string{c.config.GoBinary, c.config.LayaPython, c.config.LayaHome} {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

func ValidateDoerProtectedWorkspace(c loop.DoerContract, paths []string) error {
	if validateProtectedDoerWorkspace(c, paths) != nil {
		return ErrDoerUnsafeWorkspace
	}
	return ValidateDoerHostWorkspace(c)
}

func (w *QueueWorker) ValidateDoerProtectedWorkspace(c loop.DoerContract) error {
	return ValidateDoerProtectedWorkspace(c, w.effectiveDoerProtectedPaths())
}
