package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

// validateDoerReadiness is controller-owned and non-executing. It is repeated
// before claim; it is not a substitute for fresh runtime/effect admission.
func (w *QueueWorker) validateDoerReadiness(value loop.LoopRevision, agent registry.AgentRevision) error {
	if value.Doer == nil || w.implementation == nil || w.implementation.authorizeDoer(*value.Doer, agent) != nil {
		return errors.New("exact operator Doer authorization required")
	}
	c := w.implementation
	if c.adapter == nil || c.decision == nil || c.decision.process == nil || c.root == "" || c.config.LayaPython == "" || c.config.LayaHome == "" {
		return errors.New("local Doer decision and runtime prerequisites required")
	}
	if err := trustedExecutable(c.config.GoBinary); err != nil {
		return err
	}
	if err := trustedExecutable(c.config.LayaPython); err != nil {
		return errors.New("local Laya executable unavailable")
	}
	if !filepath.IsAbs(c.config.LayaHome) || filepath.Clean(c.config.LayaHome) != c.config.LayaHome || c.config.LayaHome == "/" {
		return errors.New("private Laya home required")
	}
	home, err := os.Lstat(c.config.LayaHome)
	if err != nil || !home.IsDir() || home.Mode().Perm()&0077 != 0 {
		return errors.New("private Laya home unavailable")
	}
	return doerWorkspacePolicy(*value.Doer)
}

func trustedExecutable(name string) error {
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil || !filepath.IsAbs(name) || resolved != name {
		return errors.New("resolved trusted executable required")
	}
	info, err := os.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 {
		return errors.New("trusted executable unavailable")
	}
	return nil
}

func doerWorkspacePolicy(c loop.DoerContract) error {
	if _, err := c.Digest(); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return errors.New("resolved Doer workspace required")
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return errors.New("Doer workspace unavailable")
	}
	defer root.Close()
	for _, name := range c.WritableFiles {
		parts := strings.Split(name, "/")
		parent := "."
		for _, part := range parts[:len(parts)-1] {
			parent = filepath.Join(parent, part)
			info, err := root.Lstat(parent)
			if err != nil || !info.IsDir() || info.Mode().Perm()&0200 == 0 {
				return errors.New("real writable Doer parent required")
			}
		}
		info, err := root.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0200 == 0 {
			return errors.New("writable Doer parent required")
		}
		info, err = root.Lstat(name)
		if err == nil && !implementation.DoerTargetHasSingleLink(info) {
			return errors.New("Doer target must be a regular, single-link file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("Doer target unavailable")
		}
	}
	return nil
}
