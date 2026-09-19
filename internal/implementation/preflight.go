package implementation

import (
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"path/filepath"
)

// Preflight is a non-executing check shared by admission and the executor.
// It does not certify future readiness; effect admission and checks still repeat.
func Preflight(c loop.VerifiedImplementation, goBinary string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(goBinary)
	if err != nil || !filepath.IsAbs(goBinary) || resolved != goBinary {
		return errors.New("resolved trusted checker required")
	}
	info, err := os.Stat(goBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("trusted executable checker required")
	}
	resolved, err = filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return errors.New("resolved workspace required")
	}
	if _, err = snapshot(c.Workspace); err != nil {
		return err
	}
	for _, name := range c.WritableFiles {
		parent, err := os.Stat(filepath.Dir(filepath.Join(c.Workspace, name)))
		if err != nil || !parent.IsDir() || parent.Mode().Perm()&0200 == 0 {
			return errors.New("writable source parent required")
		}
	}
	return nil
}
