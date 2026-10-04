package app

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
)

func (s *Service) doerControllerProtectedPaths() []string {
	paths := []string{s.Config.StateDir}

	if authority := s.Config.Credentials.Authority; authority.Custody == "systemd" {
		directory := os.Getenv("CREDENTIALS_DIRECTORY")
		// Invalid/missing delivery metadata is retained as a denied scope.
		if directory == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || authority.KEKCredential == "" || filepath.Base(authority.KEKCredential) != authority.KEKCredential || authority.KEKCredential == "." || authority.KEKCredential == ".." {
			paths = append(paths, "")
		} else {
			paths = append(paths, filepath.Join(directory, authority.KEKCredential))
		}
	}
	hermes := s.Config.HermesExecutable
	// Config permits the ordinary PATH command "hermes". Protect its actual
	// executable, not a relative filename; unresolved commands remain denied.
	if hermes != "" && !filepath.IsAbs(hermes) {
		if resolved, err := exec.LookPath(hermes); err == nil {
			hermes = resolved
		}
	}
	for _, p := range []string{s.ConfigFile, s.Config.Audit.CheckpointDir, hermes, s.Config.Implementation.GoBinary, s.Config.Implementation.LayaPython, s.Config.Implementation.LayaHome, s.Config.Credentials.Authority.Database, s.Config.Credentials.Authority.KEKFile} {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Clean(exe))
	}
	return paths
}

func (s *Service) validateDoerControllerWorkspace(c loop.DoerContract) error {
	if err := orchestration.ValidateDoerProtectedWorkspace(c, s.doerControllerProtectedPaths()); err != nil {
		return err
	}
	if s.QueueWorker != nil {
		return s.QueueWorker.ValidateDoerProtectedWorkspace(c)
	}
	return nil
}
