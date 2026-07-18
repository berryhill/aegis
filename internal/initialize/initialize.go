package initialize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
)

const (
	ReasonPartial  = "configuration_initialization_partial"
	ReasonDeclined = "configuration_initialization_declined"
)

type Plan struct {
	ConfigPath string
	StatePath  string
	Principal  config.Principal
	Document   []byte
	Partials   []string
}

type Service struct {
	Current  func() (*user.User, error)
	LookupID func(string) (*user.User, error)
}

func New() *Service { return &Service{Current: user.Current, LookupID: user.LookupId} }

// Plan authenticates the operator from host-native account APIs and derives the
// complete deterministic first configuration without writing anything.
func (s *Service) Plan(configPath, statePath string) (Plan, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateAbsent && inspection.State != config.StatePartial {
		if inspection.Err != nil {
			return Plan{}, inspection.Failure()
		}
		return Plan{}, fmt.Errorf("configuration %s is in state %s and will not be overwritten", inspection.Path, inspection.State)
	}
	current, err := s.verifiedCurrent()
	if err != nil {
		return Plan{}, err
	}
	if statePath == "" {
		statePath = config.Defaults().StateDir
	}
	statePath, err = filepath.Abs(statePath)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve state path: %w", err)
	}
	principalName := current.Name
	if principalName == "" {
		principalName = current.Username
	}
	principal := config.Principal{ID: "principal", Name: principalName, UID: current.Uid, User: current.Username, AuthTTL: config.Defaults().Principal.AuthTTL}
	candidate := config.Defaults()
	candidate.StateDir = statePath
	candidate.Audit.CheckpointDir = filepath.Join(statePath, "audit-checkpoints")
	candidate.Principal = principal
	if err = candidate.Validate(); err != nil {
		return Plan{}, fmt.Errorf("generated configuration is invalid: %w", err)
	}
	document := []byte(fmt.Sprintf("state_dir: %s\nprincipal:\n  id: %s\n  name: %s\n  uid: %s\n  user: %s\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %s\n",
		strconv.Quote(statePath), strconv.Quote(principal.ID), strconv.Quote(principal.Name), strconv.Quote(principal.UID), strconv.Quote(principal.User), strconv.Quote(candidate.Audit.CheckpointDir)))
	return Plan{ConfigPath: inspection.Path, StatePath: statePath, Principal: principal, Document: document, Partials: append([]string(nil), inspection.Partials...)}, nil
}

func (s *Service) verifiedCurrent() (*user.User, error) {
	current, err := s.Current()
	if err != nil {
		return nil, fmt.Errorf("authenticate local operator: obtain current OS identity: %w", err)
	}
	if current.Uid == "" || current.Username == "" {
		return nil, errors.New("authenticate local operator: host identity has no UID or username")
	}
	if _, err = strconv.ParseUint(current.Uid, 10, 32); err != nil {
		return nil, fmt.Errorf("authenticate local operator: UID %q is not a host-native numeric UID", current.Uid)
	}
	lookedUp, err := s.LookupID(current.Uid)
	if err != nil {
		return nil, fmt.Errorf("authenticate local operator: verify UID through host account database: %w", err)
	}
	if lookedUp.Uid != current.Uid || lookedUp.Username != current.Username {
		return nil, errors.New("authenticate local operator: current UID and host account database are ambiguous")
	}
	return current, nil
}

// Apply reauthenticates immediately before the consequential write, recovers
// only recognized secure interrupted temporaries, and publishes with no-replace
// hard-link semantics after syncing complete mode-0600 bytes.
func (s *Service) Apply(ctx context.Context, plan Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.verifiedCurrent()
	if err != nil {
		return err
	}
	if current.Uid != plan.Principal.UID || current.Username != plan.Principal.User {
		return errors.New("authenticated local operator changed during initialization")
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateAbsent && inspection.State != config.StatePartial {
		if inspection.Err != nil {
			return inspection.Failure()
		}
		return fmt.Errorf("configuration %s appeared during initialization and was not overwritten", plan.ConfigPath)
	}
	if err = ensureSecureDirectory(filepath.Dir(plan.ConfigPath)); err != nil {
		return err
	}
	for _, partial := range inspection.Partials {
		if err = removeOwnedPartial(partial); err != nil {
			return fmt.Errorf("%s: %w", ReasonPartial, err)
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(plan.ConfigPath), config.InitializationTemporaryPrefix+"*")
	if err != nil {
		return fmt.Errorf("create atomic configuration temporary: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(plan.Document)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write atomic configuration temporary: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Link(temporaryPath, plan.ConfigPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("configuration %s appeared during initialization and was not overwritten", plan.ConfigPath)
		}
		return fmt.Errorf("publish configuration %s atomically: %w", plan.ConfigPath, err)
	}
	_ = os.Remove(temporaryPath)
	if directory, openErr := os.Open(filepath.Dir(plan.ConfigPath)); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	verified := config.Inspect(plan.ConfigPath)
	if verified.State != config.StateValid {
		_ = os.Remove(plan.ConfigPath)
		if verified.Err != nil {
			return fmt.Errorf("verify initialized configuration: %w", verified.Err)
		}
		return fmt.Errorf("verify initialized configuration: state %s", verified.State)
	}
	return nil
}

func ensureSecureDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(path, 0700); err != nil {
			return fmt.Errorf("create configuration directory %s: %w", path, err)
		}
		return os.Chmod(path, 0700)
	}
	if err != nil {
		return fmt.Errorf("inspect configuration directory %s: %w", path, err)
	}
	if !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("configuration directory %s must be an owned directory that is not writable by group or others", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("configuration directory %s is not owned by the current effective UID", path)
	}
	return nil
}

func removeOwnedPartial(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect partial initialization artifact %s: %w", path, err)
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !owned || int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("partial initialization artifact %s is unsafe or ambiguous; inspect it manually", path)
	}
	if err = os.Remove(path); err != nil {
		return fmt.Errorf("recover partial initialization artifact %s: %w", path, err)
	}
	return nil
}
