package initialize

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/persistence/authority"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

const (
	ReasonPartial  = "configuration_initialization_partial"
	ReasonDeclined = "configuration_initialization_declined"
)

type Plan struct {
	ConfigPath    string
	StatePath     string
	AuthorityPath string
	Principal     config.Principal
	Document      []byte
	Partials      []string
	TokenPath     string
	UnixSocket    string
	token         []byte
}

type OperationalAuthorityPlan struct {
	ConfigPath    string
	AuthorityPath string
	Principal     config.Principal
	Config        config.Config
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
	if _, err = authority.ClassifyLegacyAuthority(statePath); err != nil {
		return Plan{}, fmt.Errorf("authority persistence initialization denied: %w", err)
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
	tokenPath := filepath.Join(statePath, "transport", "api.token")
	unixSocket := filepath.Join(statePath, "transport", "aegis.sock")
	token, err := existingOrRandomToken(tokenPath)
	if err != nil {
		return Plan{}, fmt.Errorf("prepare protected API transport: %w", err)
	}
	candidate.API.TokenFile = tokenPath
	candidate.API.UnixSocket = unixSocket
	if err = candidate.Validate(); err != nil {
		return Plan{}, fmt.Errorf("generated configuration is invalid: %w", err)
	}
	document := []byte(fmt.Sprintf("state_dir: %s\nprincipal:\n  id: %s\n  name: %s\n  uid: %s\n  user: %s\n  auth_ttl: %s\napi:\n  unix_socket: %s\n  token_file: %s\naudit:\n  checkpoint_dir: %s\n",
		strconv.Quote(statePath), strconv.Quote(principal.ID), strconv.Quote(principal.Name), strconv.Quote(principal.UID), strconv.Quote(principal.User), principal.AuthTTL, strconv.Quote(unixSocket), strconv.Quote(tokenPath), strconv.Quote(candidate.Audit.CheckpointDir)))
	return Plan{ConfigPath: inspection.Path, StatePath: statePath, AuthorityPath: filepath.Join(statePath, "persistence", "authority-v1"), Principal: principal, Document: document, Partials: append([]string(nil), inspection.Partials...), TokenPath: tokenPath, UnixSocket: unixSocket, token: token}, nil
}

func existingOrRandomToken(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
			return nil, errors.New("existing transport token is unsafe or ambiguous")
		}
		value, readErr := os.ReadFile(path)
		if readErr != nil || len(strings.TrimSpace(string(value))) < 64 {
			return nil, errors.New("existing transport token is malformed")
		}
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return nil, err
	}
	encoded := make([]byte, hex.EncodedLen(len(random)))
	hex.Encode(encoded, random)
	return append(encoded, '\n'), nil
}

// PlanOperationalAuthority authenticates the configured host principal and
// admits only exact absence of the compatibility generation.
func (s *Service) PlanOperationalAuthority(configPath string) (OperationalAuthorityPlan, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid {
		return OperationalAuthorityPlan{}, inspection.Failure()
	}
	current, err := s.verifiedCurrent()
	if err != nil {
		return OperationalAuthorityPlan{}, err
	}
	if current.Uid != inspection.Config.Principal.UID || current.Username != inspection.Config.Principal.User {
		return OperationalAuthorityPlan{}, errors.New("configured principal does not match freshly authenticated host identity")
	}
	authorityPath := filepath.Join(inspection.Config.StateDir, "persistence", "authority-v1")
	state := authoritybadger.Inspect(context.Background(), authorityPath)
	if state.State == authoritybadger.StateReady {
		return OperationalAuthorityPlan{}, authoritybadger.ErrAlreadyInitialized
	}
	if state.State != authoritybadger.StateAbsent {
		return OperationalAuthorityPlan{}, fmt.Errorf("existing invalid operational authority will not be replaced: %w", state.Err)
	}
	return OperationalAuthorityPlan{ConfigPath: inspection.Path, AuthorityPath: authorityPath, Principal: inspection.Config.Principal, Config: inspection.Config}, nil
}

// ApplyOperationalAuthority reauthenticates and revalidates exact absence
// immediately before publishing one secure empty generation.
func (s *Service) ApplyOperationalAuthority(ctx context.Context, plan OperationalAuthorityPlan) (authoritybadger.Generation, error) {
	if err := ctx.Err(); err != nil {
		return authoritybadger.Generation{}, err
	}
	current, err := s.verifiedCurrent()
	if err != nil {
		return authoritybadger.Generation{}, err
	}
	if current.Uid != plan.Principal.UID || current.Username != plan.Principal.User {
		return authoritybadger.Generation{}, errors.New("authenticated host identity changed after operational authority preview")
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateValid || inspection.Config.StateDir != plan.Config.StateDir || inspection.Config.Principal != plan.Principal {
		return authoritybadger.Generation{}, errors.New("configuration changed after operational authority preview")
	}
	return authoritybadger.InitializeEmpty(ctx, plan.AuthorityPath)
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
	if _, err = authority.ClassifyLegacyAuthority(plan.StatePath); err != nil {
		return fmt.Errorf("authority persistence changed during initialization: %w", err)
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
	if err = ensureSecureDirectory(plan.StatePath); err != nil {
		return err
	}
	for _, partial := range inspection.Partials {
		if err = removeOwnedPartial(partial); err != nil {
			return fmt.Errorf("%s: %w", ReasonPartial, err)
		}
	}
	if err = ensureAuthorityPersistence(ctx, plan.AuthorityPath); err != nil {
		return fmt.Errorf("initialize authority persistence: %w", err)
	}
	if err = publishToken(plan.TokenPath, plan.token); err != nil {
		return fmt.Errorf("publish protected API transport: %w", err)
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
	revalidated, err := s.verifiedCurrent()
	if err != nil {
		return err
	}
	if revalidated.Uid != plan.Principal.UID || revalidated.Username != plan.Principal.User {
		return errors.New("authenticated local operator changed immediately before configuration publication")
	}
	if err = os.Link(temporaryPath, plan.ConfigPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("configuration %s appeared during initialization and was not overwritten", plan.ConfigPath)
		}
		return fmt.Errorf("publish configuration %s atomically: %w", plan.ConfigPath, err)
	}
	_ = os.Remove(temporaryPath)
	directory, openErr := os.Open(filepath.Dir(plan.ConfigPath))
	if openErr != nil {
		return fmt.Errorf("open configuration directory for sync: %w", openErr)
	}
	if syncErr := directory.Sync(); syncErr != nil {
		_ = directory.Close()
		return fmt.Errorf("sync published configuration directory: %w", syncErr)
	}
	if closeErr := directory.Close(); closeErr != nil {
		return fmt.Errorf("close published configuration directory: %w", closeErr)
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

func publishToken(path string, value []byte) error {
	if len(value) < 65 {
		return errors.New("transport material is absent or too short")
	}
	if err := ensureSecureDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
			return errors.New("existing transport token is unsafe or ambiguous")
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || string(existing) != string(value) {
			return errors.New("transport token changed after preview")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".api.token.init-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(value)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(temporaryPath, path); err != nil {
		return err
	}
	_ = os.Remove(temporaryPath)
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}

// ensureAuthorityPersistence makes configuration publication resumable. The
// authority generation is published first; a cancellation before the final
// configuration link leaves a verified generation that the next run reopens
// rather than replacing. Existing invalid or unopenable state denies.
func ensureAuthorityPersistence(ctx context.Context, path string) error {
	if _, err := authoritybadger.Initialize(ctx, path); err == nil {
		return nil
	} else if !errors.Is(err, authoritybadger.ErrAlreadyInitialized) {
		return err
	}
	store, err := authoritybadger.Open(ctx, path)
	if err != nil {
		return fmt.Errorf("verify resumable authority persistence: %w", err)
	}
	mandates, mandateErr := store.ListMandates(ctx)
	contexts, contextErr := store.ListAuthorityContexts(ctx)
	if mandateErr != nil || contextErr != nil || len(mandates) != 0 || len(contexts) != 0 {
		closeErr := store.Close()
		return errors.Join(errors.New("resumable authority persistence is not an empty initialization generation"), mandateErr, contextErr, closeErr)
	}
	if err = store.Close(); err != nil {
		return fmt.Errorf("close verified resumable authority persistence: %w", err)
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
