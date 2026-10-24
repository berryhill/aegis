package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

// ValidateDoerRuntimeCapability distinguishes missing installed execution
// capability from missing operator authority. It neither grants nor installs.
func (w *QueueWorker) ValidateDoerRuntimeCapability() error {
	if w == nil || w.implementation == nil || w.implementation.adapter == nil || w.implementation.decision == nil {
		return ErrDoerControllerUnavailable
	}
	return nil
}

var ErrLocalLayaUnavailable = errors.New("local Laya unavailable")
var ErrDoerHostApprovalRequired = errors.New("exact Doer host-write approval required")
var ErrDoerUnsafeWorkspace = errors.New("unsafe Doer workspace or file")

// ValidateDoerHostWorkspace performs bounded, non-writing no-follow checks.
func ValidateDoerHostWorkspace(c loop.DoerContract) error {
	if doerWorkspacePolicy(c) != nil {
		return ErrDoerUnsafeWorkspace
	}
	return nil
}

// ValidateDoerAvailability checks that configured local Laya can answer the
// typed helper protocol before a new v4 execution is submitted. Its verdict
// is discarded: only the later preclaim gate judges the actual task. The
// worker repeats structural admission and that gate before creating a claim.
func (w *QueueWorker) ValidateDoerAvailability(ctx context.Context, value loop.LoopRevision, agent registry.AgentRevision) error {
	if ctx == nil {
		return ErrLocalLayaUnavailable
	}
	if err := w.ValidateLoopAdmissionContext(ctx, value, agent); err != nil {
		return err
	}
	if value.SchemaVersion != loop.DoerRevisionSchemaVersion {
		return nil
	}
	if _, err := w.implementation.decision.Gate(ctx, value.Doer.Task); err != nil {
		return ErrLocalLayaUnavailable
	}
	return nil
}

// validateDoerReadiness is controller-owned and non-executing. It is repeated
// before claim; it is not a substitute for fresh runtime/effect admission.
func (w *QueueWorker) validateDoerReadiness(ctx context.Context, value loop.LoopRevision, agent registry.AgentRevision) error {
	if value.Doer == nil {
		return ErrDoerHostApprovalRequired
	}
	return w.ValidateDoerContractReadiness(ctx, *value.Doer, agent, false)
}

// ValidateDoerContractReadiness repeats the existing checks on an exact bound
// contract, without changing its canonical reusable Loop definition.
func (w *QueueWorker) ValidateDoerContractReadiness(ctx context.Context, contract loop.DoerContract, agent registry.AgentRevision, probe bool) error {
	if w == nil || w.implementation == nil {
		return ErrDoerControllerUnavailable
	}
	if err := w.authorizeDoer(ctx, contract, agent); err != nil {
		return err
	}
	c := w.implementation
	// Native authorization is checked first; missing and untrusted checker
	// configuration are distinct and do not imply any grant.
	if contract.Coding != nil {
		if c.config.GoBinary == "" {
			return ErrDoerGoMissing
		}
		// Only a canonical absolute missing path is absence. A relative path,
		// symlink or inaccessible executable remains an integrity failure.
		if filepath.IsAbs(c.config.GoBinary) && filepath.Clean(c.config.GoBinary) == c.config.GoBinary {
			if _, err := os.Lstat(c.config.GoBinary); errors.Is(err, os.ErrNotExist) {
				return ErrDoerGoMissing
			}
		}
		if err := trustedExecutable(c.config.GoBinary); err != nil {
			return ErrDoerGoIntegrity
		}
	}
	if c.adapter == nil || c.decision == nil || c.decision.process == nil || c.root == "" || c.config.LayaPython == "" || c.config.LayaHome == "" {
		return ErrDoerControllerUnavailable
	}
	// Selected-file Doer never executes the native Go checker.
	if contract.Coding != nil || !c.managedLocalDoer || c.config.GoBinary != "" {
		if err := trustedExecutable(c.config.GoBinary); err != nil {
			return ErrDoerGoIntegrity
		}
	}
	if err := trustedLayaExecutable(c.config.LayaPython); err != nil {
		return ErrDoerLayaIntegrity
	}
	if !filepath.IsAbs(c.config.LayaHome) || filepath.Clean(c.config.LayaHome) != c.config.LayaHome || c.config.LayaHome == "/" {
		return ErrDoerHomeIntegrity
	}
	home, err := os.Lstat(c.config.LayaHome)
	if err != nil || !home.IsDir() || home.Mode().Perm()&0077 != 0 {
		return ErrDoerHomeIntegrity
	}
	if err := ValidateDoerHostWorkspace(contract); err != nil {
		return err
	}
	if contract.Coding != nil {
		v, err := contract.CodingImplementation()
		if err != nil {
			return err
		}
		if err = implementation.Preflight(v, c.config.GoBinary); err != nil {
			return err
		}
	}
	if probe {
		if _, err := c.decision.Gate(ctx, contract.Task); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrLocalLayaUnavailable
		}
	}
	return nil
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
		if err == nil && (!implementation.DoerTargetHasSingleLink(info) || info.Size() > 1024*1024) {
			return errors.New("Doer target must be a regular, single-link file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("Doer target unavailable")
		}
	}
	return nil
}
