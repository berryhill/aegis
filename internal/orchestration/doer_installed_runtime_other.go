//go:build !linux

package orchestration

import hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"

func (w *QueueWorker) PrepareInstalledDoerRuntime(string, *hermesruntime.Adapter) error {
	return ErrLocalLayaUnavailable
}
func trustedLayaExecutable(path string) error { return trustedExecutable(path) }
