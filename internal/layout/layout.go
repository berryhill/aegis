// Package layout resolves and validates Aegis's per-operator filesystem layout.
package layout

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const (
	CanonicalDirectory   = ".aegis"
	FormerDirectory      = ".argis"
	DevelopmentDirectory = ".aegis"
)

type HomeResolver func() (string, error)

type Layout struct {
	Home                  string
	Root                  string
	Config                string
	State                 string
	AuditCheckpoints      string
	AuthorityPersistence  string
	CredentialDatabase    string
	HostKEK               string
	ManagerCertifications string
	ManagedModels         string
	Runtime               string
	LegacyConfig          string
	LegacyState           string
	LegacyCheckpoints     string
	XDGConfig             string
	XDGState              string
	XDGCheckpoints        string
}

type Presence string

const (
	None      Presence = "none"
	Canonical Presence = "canonical"
	Legacy    Presence = "legacy"
	Ambiguous Presence = "ambiguous"
)

type Discovery struct {
	Presence           Presence
	CanonicalArtifacts []string
	LegacyArtifacts    []string
	LegacyConfig       string
	LegacyState        string
	LegacyCheckpoints  string
}

type Resolver struct {
	Home HomeResolver
	EUID func() int
}

func New() Resolver { return Resolver{Home: os.UserHomeDir, EUID: os.Geteuid} }

func (r Resolver) Resolve() (Layout, error) {
	if r.Home == nil {
		r.Home = os.UserHomeDir
	}
	if r.EUID == nil {
		r.EUID = os.Geteuid
	}
	home, err := r.Home()
	if err != nil {
		return Layout{}, fmt.Errorf("resolve authenticated operator home: %w", err)
	}
	if home == "" || !filepath.IsAbs(home) || filepath.Clean(home) != home || home == filepath.VolumeName(home)+string(filepath.Separator) {
		return Layout{}, errors.New("authenticated operator home must be a clean absolute non-root path")
	}
	info, err := os.Lstat(home)
	if err != nil {
		return Layout{}, fmt.Errorf("inspect authenticated operator home: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || int(stat.Uid) != r.EUID() {
		return Layout{}, errors.New("authenticated operator home must be a real directory owned by the effective operator")
	}
	root := filepath.Join(home, CanonicalDirectory)
	if root == home || !within(home, root) {
		return Layout{}, errors.New("canonical layout escaped authenticated operator home")
	}
	if rootInfo, statErr := os.Lstat(root); statErr == nil {
		rootStat, statOK := rootInfo.Sys().(*syscall.Stat_t)
		if !statOK || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || int(rootStat.Uid) != r.EUID() || rootInfo.Mode().Perm()&0077 != 0 {
			return Layout{}, errors.New("canonical root must be a real operator-owned directory with mode 0700")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Layout{}, fmt.Errorf("inspect canonical root: %w", statErr)
	}
	return ForRoot(home, root), nil
}

// ForRoot derives the complete operational path set for one execution profile.
// Callers must establish custody of scope and root before using the result.
// Keeping this derivation here prevents pre-configuration and ordinary command
// paths from silently using different locations.
func ForRoot(scope, root string) Layout {
	state := filepath.Join(root, "state")
	return Layout{
		Home: scope, Root: root, Config: filepath.Join(root, "aegis.yaml"), State: state,
		AuditCheckpoints:      filepath.Join(state, "audit-checkpoints"),
		AuthorityPersistence:  filepath.Join(state, "persistence", "authority-v1"),
		CredentialDatabase:    filepath.Join(state, "credentials", "authority.db"),
		HostKEK:               filepath.Join(state, "credentials", "authority.kek"),
		ManagerCertifications: filepath.Join(state, "manager", "certifications"),
		ManagedModels:         filepath.Join(state, "manager", "ollama-models"),
		Runtime:               filepath.Join(state, "runtime"),
		LegacyConfig:          filepath.Join(scope, FormerDirectory, "aegis.yaml"),
		LegacyState:           filepath.Join(scope, FormerDirectory, "state"),
		LegacyCheckpoints:     filepath.Join(scope, FormerDirectory, "state", "audit-checkpoints"),
		XDGConfig:             filepath.Join(scope, ".config", "aegis", "aegis.yaml"),
		XDGState:              filepath.Join(scope, ".local", "state", "aegis"),
		XDGCheckpoints:        filepath.Join(scope, ".local", "state", "aegis-checkpoints"),
	}
}

// ForState rederives every layout value whose default is rooted in state.
func (l Layout) ForState(state string) Layout {
	l.State = state
	l.AuditCheckpoints = filepath.Join(state, "audit-checkpoints")
	l.AuthorityPersistence = filepath.Join(state, "persistence", "authority-v1")
	l.CredentialDatabase = filepath.Join(state, "credentials", "authority.db")
	l.HostKEK = filepath.Join(state, "credentials", "authority.kek")
	l.ManagerCertifications = filepath.Join(state, "manager", "certifications")
	l.ManagedModels = filepath.Join(state, "manager", "ollama-models")
	l.Runtime = filepath.Join(state, "runtime")
	return l
}

// WithLegacy selects the one artifact-derived legacy layout returned by Discover.
// It does not grant migration or deletion authority; callers must still validate
// the selected artifacts and authenticated principal before mutation.
func (l Layout) WithLegacy(d Discovery) Layout {
	if d.LegacyConfig != "" {
		l.LegacyConfig = d.LegacyConfig
		l.LegacyState = d.LegacyState
		l.LegacyCheckpoints = d.LegacyCheckpoints
	}
	return l
}

func (l Layout) Discover() (Discovery, error) {
	var d Discovery
	if meaningful, artifacts, err := meaningfulCanonical(l); err != nil {
		return d, err
	} else if meaningful {
		d.CanonicalArtifacts = artifacts
	}
	legacyLayouts := [][3]string{
		{l.LegacyConfig, l.LegacyState, l.LegacyCheckpoints},
		{l.XDGConfig, l.XDGState, l.XDGCheckpoints},
	}
	for _, candidate := range legacyLayouts {
		var artifacts []string
		for _, path := range candidate {
			if exists, err := meaningfulLegacyArtifact(path); err != nil {
				return d, err
			} else if exists {
				artifacts = append(artifacts, path)
			}
		}
		if len(artifacts) == 0 {
			continue
		}
		if d.LegacyConfig != "" {
			return d, errors.New("multiple legacy Aegis layouts exist; migration source is ambiguous")
		}
		d.LegacyConfig, d.LegacyState, d.LegacyCheckpoints = candidate[0], candidate[1], candidate[2]
		d.LegacyArtifacts = append(d.LegacyArtifacts, artifacts...)
	}
	switch {
	case len(d.CanonicalArtifacts) > 0 && len(d.LegacyArtifacts) > 0:
		d.Presence = Ambiguous
	case len(d.CanonicalArtifacts) > 0:
		d.Presence = Canonical
	case len(d.LegacyArtifacts) > 0:
		d.Presence = Legacy
	default:
		d.Presence = None
	}
	return d, nil
}

func meaningfulCanonical(l Layout) (bool, []string, error) {
	rootInfo, err := os.Lstat(l.Root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		if err != nil {
			return false, nil, err
		}
		return true, []string{l.Root}, nil
	}
	artifacts := []string{}
	if present, inspectErr := existsNoFollow(l.Config); inspectErr != nil {
		return false, nil, inspectErr
	} else if present {
		artifacts = append(artifacts, l.Config)
	}
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return false, nil, err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "aegis.yaml":
		case "state":
			meaningful, stateErr := meaningfulCanonicalState(l.State)
			if stateErr != nil {
				return false, nil, stateErr
			}
			if meaningful {
				artifacts = append(artifacts, l.State)
			}
		default:
			artifacts = append(artifacts, filepath.Join(l.Root, entry.Name()))
		}
	}
	return len(artifacts) != 0, artifacts, nil
}

func meaningfulCanonicalState(state string) (bool, error) {
	info, err := os.Lstat(state)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		if err != nil {
			return false, err
		}
		return true, nil
	}
	entries, err := os.ReadDir(state)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() != "manager" {
			return true, nil
		}
		managerPath := filepath.Join(state, "manager")
		managerInfo, managerErr := os.Lstat(managerPath)
		if managerErr != nil || !managerInfo.IsDir() || managerInfo.Mode()&os.ModeSymlink != 0 {
			return true, managerErr
		}
		managerEntries, managerErr := os.ReadDir(managerPath)
		if managerErr != nil {
			return false, managerErr
		}
		for _, managerEntry := range managerEntries {
			if managerEntry.Name() != "ollama-models" {
				return true, nil
			}
			modelInfo, modelErr := os.Lstat(filepath.Join(managerPath, managerEntry.Name()))
			if modelErr != nil || !modelInfo.IsDir() || modelInfo.Mode()&os.ModeSymlink != 0 {
				return true, modelErr
			}
		}
	}
	return false, nil
}

func meaningfulLegacyArtifact(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return true, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) != 0, nil
}

func existsNoFollow(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect layout artifact %s: %w", path, err)
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) && len(rel) > 0 && !(len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator))
}
