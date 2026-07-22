// Package reset implements the authenticated, plan-bound local reset transaction.
package reset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/layout"
	"github.com/berryhill/aegis/internal/safefs"
	bolt "go.etcd.io/bbolt"
)

const (
	ReasonDenied            = "reset_denied"
	ReasonChanged           = "reset_plan_changed"
	ReasonIncomplete        = "reset_incomplete"
	ReasonDeclined          = "reset_declined"
	ReasonRequiresTTY       = "reset_requires_tty"
	ReasonRequiresAuthority = "reset_requires_authority_passphrase"
	Confirmation            = "yes"
	maximumArtifacts        = 10000
	maximumPathBytes        = 4096
	maximumPathDepth        = 64
)

type Identity struct {
	Device  uint64
	Inode   uint64
	Mode    uint32
	UID     uint32
	GID     uint32
	Links   uint64
	Size    int64
	ModTime int64
}

type Artifact struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	identity Identity
}

type Principal struct {
	UID  string `json:"uid"`
	User string `json:"user"`
}

type Plan struct {
	Principal         Principal    `json:"authenticated_principal"`
	ConfigPath        string       `json:"config_path"`
	ConfigState       config.State `json:"config_state"`
	Artifacts         []Artifact   `json:"delete"`
	Preserved         []string     `json:"preserve"`
	CredentialRecords bool         `json:"credential_records_destroyed"`
	LocalKEK          bool         `json:"local_kek_destroyed"`
	Postcondition     string       `json:"postcondition"`
	Warning           string       `json:"warning"`
	LegacyRetained    []string     `json:"retained_empty_legacy_directories,omitempty"`
	Legacy            bool         `json:"legacy_layout"`
	configIdentity    *Identity
	legacyRoots       map[string]Identity
}

type Service struct {
	Current             func() (*user.User, error)
	LookupID            func(string) (*user.User, error)
	HomeDir             func() (string, error)
	BeforeApply         func(Plan)
	RepositoryResetRoot string
}

func New() *Service {
	return &Service{Current: user.Current, LookupID: user.LookupId, HomeDir: os.UserHomeDir}
}

func (s *Service) Plan(ctx context.Context, configuredPath string) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	principal, err := s.verifiedCurrent()
	if err != nil {
		return Plan{}, deny(err)
	}
	resolvedLayout, err := (layout.Resolver{Home: s.HomeDir, EUID: os.Geteuid}).Resolve()
	if err != nil {
		return Plan{}, deny(fmt.Errorf("resolve operator layout: %w", err))
	}
	home := resolvedLayout.Home
	inspection := config.Inspect(configuredPath)
	legacy := configuredPath == "" && inspection.State == config.StateLegacy
	if legacy {
		inspection = config.Inspect(resolvedLayout.LegacyConfig)
	}
	plan := Plan{
		Principal:     Principal{UID: principal.Uid, User: principal.Username},
		ConfigPath:    inspection.Path,
		ConfigState:   inspection.State,
		Postcondition: "config.Inspect reports uninitialized; the next interactive bare aegis invocation enters first-run initialization",
		Warning:       "encrypted credentials and audit history in reset scope cannot be recovered without separate backups",
		Preserved: []string{
			"Aegis executable and source checkout",
			"Hermes installation and normal Hermes profiles",
			"Ollama installation, operator-managed daemon, and downloaded model stores",
			"external credentials, systemd credentials, and every external system",
		},
	}
	if legacy {
		plan.legacyRoots = map[string]Identity{}
		plan.Legacy = true
	}
	if inspection.Path == "" {
		return Plan{}, deny(errors.New("configuration path is unresolved"))
	}
	if err = validateScopedPath(inspection.Path, home, s.RepositoryResetRoot); err != nil {
		return Plan{}, deny(fmt.Errorf("configuration path: %w", err))
	}

	var cfg *config.Config
	switch inspection.State {
	case config.StateValid:
		if !inspection.FilePresent {
			return Plan{}, deny(errors.New("environment-only configuration cannot be reset by deleting local files; unset AEGIS_PRINCIPAL_UID and AEGIS_PRINCIPAL_USER explicitly"))
		}
		for _, name := range []string{"AEGIS_STATE_DIR", "AEGIS_AUDIT_CHECKPOINT_DIR", "AEGIS_PRINCIPAL_UID", "AEGIS_PRINCIPAL_USER", "AEGIS_MANAGER_INFERENCE_CERTIFICATION"} {
			if _, configured := os.LookupEnv(name); configured {
				return Plan{}, deny(fmt.Errorf("%s must be unset because environment overrides are not deletion authority", name))
			}
		}
		if inspection.Config.Principal.UID != principal.Uid || inspection.Config.Principal.User != principal.Username {
			return Plan{}, deny(errors.New("authenticated OS identity does not exactly match the configured principal"))
		}
		copy := inspection.Config
		cfg = &copy
	case config.StateAbsent, config.StatePartial:
		// Absence and recognized initializer temporaries have no configuration-derived deletion authority.
	case config.StateMalformed:
		// A malformed document authorizes deletion of only its own safely identified file.
	case config.StateInsecure, config.StateAmbiguous:
		return Plan{}, deny(inspection.Failure())
	default:
		return Plan{}, deny(inspection.Failure())
	}

	if inspection.FilePresent || inspection.State == config.StateMalformed {
		artifact, artifactErr := inspectFile(inspection.Path, "configuration")
		if artifactErr != nil {
			return Plan{}, deny(artifactErr)
		}
		plan.configIdentity = &artifact.identity
		plan.Artifacts = append(plan.Artifacts, artifact)
	}
	for _, partial := range inspection.Partials {
		if filepath.Dir(partial) != filepath.Dir(inspection.Path) || !strings.HasPrefix(filepath.Base(partial), config.InitializationTemporaryPrefix) {
			return Plan{}, deny(errors.New("unrecognized initialization temporary"))
		}
		artifact, artifactErr := inspectFile(partial, "initialization-temporary")
		if artifactErr != nil {
			return Plan{}, deny(artifactErr)
		}
		plan.Artifacts = append(plan.Artifacts, artifact)
	}
	entries, readErr := os.ReadDir(filepath.Dir(inspection.Path))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return Plan{}, deny(fmt.Errorf("inspect configuration temporaries: %w", readErr))
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".aegis-model-config-") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		artifact, artifactErr := inspectFile(filepath.Join(filepath.Dir(inspection.Path), name), "manager-configuration temporary")
		if artifactErr != nil {
			return Plan{}, deny(artifactErr)
		}
		plan.Artifacts = append(plan.Artifacts, artifact)
	}

	if cfg != nil {
		if legacy {
			if cfg.StateDir != resolvedLayout.LegacyState || (cfg.Audit.CheckpointDir != resolvedLayout.LegacyCheckpoints && cfg.Audit.CheckpointDir != filepath.Join(resolvedLayout.LegacyState, "audit-checkpoints")) {
				return Plan{}, deny(errors.New("legacy reset accepts only exact recognized legacy state and checkpoint defaults"))
			}
			if err = s.addLegacyScope(ctx, &plan, *cfg, resolvedLayout); err != nil {
				return Plan{}, err
			}
		} else if err = s.addConfiguredScope(ctx, &plan, *cfg, home); err != nil {
			return Plan{}, err
		}
	}
	if !legacy && inspection.Path == resolvedLayout.Config && existsNoFollow(resolvedLayout.Root) && !existsNoFollow(resolvedLayout.ManagedModels) {
		rootEntries, rootErr := os.ReadDir(resolvedLayout.Root)
		if rootErr != nil {
			return Plan{}, deny(rootErr)
		}
		for _, entry := range rootEntries {
			name := entry.Name()
			recognized := name == "aegis.yaml" || name == "state"
			for _, artifact := range plan.Artifacts {
				recognized = recognized || artifact.Path == filepath.Join(resolvedLayout.Root, name)
			}
			if !recognized {
				return Plan{}, deny(fmt.Errorf("unknown canonical-root artifact: %s", filepath.Join(resolvedLayout.Root, name)))
			}
		}
		rootInfo, rootErr := os.Lstat(resolvedLayout.Root)
		if rootErr != nil {
			return Plan{}, deny(rootErr)
		}
		rootIdentity, identityErr := identity(rootInfo, true)
		if identityErr != nil {
			return Plan{}, deny(identityErr)
		}
		plan.Artifacts = append(plan.Artifacts, Artifact{Path: resolvedLayout.Root, Kind: "directory", Status: "delete-if-empty", identity: rootIdentity})
	}

	plan.Artifacts = uniqueArtifacts(plan.Artifacts)
	if len(plan.Artifacts) > maximumArtifacts {
		return Plan{}, deny(errors.New("reset plan exceeds the bounded artifact limit"))
	}
	sort.Slice(plan.Artifacts, func(i, j int) bool {
		if depth(plan.Artifacts[i].Path) != depth(plan.Artifacts[j].Path) {
			return depth(plan.Artifacts[i].Path) > depth(plan.Artifacts[j].Path)
		}
		return plan.Artifacts[i].Path < plan.Artifacts[j].Path
	})
	sort.Strings(plan.Preserved)
	return plan, nil
}

func (s *Service) addLegacyScope(ctx context.Context, plan *Plan, cfg config.Config, resolved layout.Layout) error {
	protected := map[string]bool{}
	configuredFiles := map[string]bool{}
	modelStore := filepath.Join(cfg.StateDir, "manager", "ollama-models")
	if existsNoFollow(modelStore) {
		if err := validateLegacyTraversal(modelStore, resolved.Home); err != nil {
			return deny(fmt.Errorf("unsafe legacy managed model store: %w", err))
		}
		protected[modelStore] = true
		plan.Preserved = append(plan.Preserved, "Aegis managed Ollama model store: "+modelStore)
	}
	if certification := cfg.Manager.Inference.Certification; certification != "" {
		if !within(cfg.StateDir, certification) {
			return deny(errors.New("legacy manager certification is outside the recognized legacy state root"))
		}
		configuredFiles[certification], configuredFiles[certification+".new"] = true, true
	}
	authority := cfg.Credentials.Authority
	for _, authorityPath := range []string{authority.Database, authority.KEKFile} {
		if authorityPath == "" {
			continue
		}
		if !within(cfg.StateDir, authorityPath) {
			protected[authorityPath] = true
			plan.Preserved = append(plan.Preserved, "external credential-authority artifact: "+authorityPath)
			continue
		}
		configuredFiles[authorityPath], configuredFiles[authorityPath+".initialize"] = true, true
	}
	if authority.Custody == "systemd" {
		plan.Preserved = append(plan.Preserved, "systemd KEK credential: "+authority.KEKCredential)
	}
	if authority.Database != "" && within(cfg.StateDir, authority.Database) && existsNoFollow(authority.Database) {
		if err := validateAuthorityDatabase(authority.Database, authority.DeploymentID); err != nil {
			return deny(err)
		}
		plan.CredentialRecords = true
	}
	if authority.KEKFile != "" && within(cfg.StateDir, authority.KEKFile) && existsNoFollow(authority.KEKFile) {
		var err error
		if authority.Custody == "passphrase-file" {
			err = credentials.InspectPassphraseCredential(authority.KEKFile)
		} else {
			err = validateHostKEK(authority.KEKFile)
		}
		if err != nil {
			return deny(err)
		}
		plan.LocalKEK = true
	}
	seen := map[string]bool{}
	for _, rootPath := range []string{cfg.StateDir, cfg.Audit.CheckpointDir} {
		if seen[rootPath] {
			continue
		}
		seen[rootPath] = true
		if !existsNoFollow(rootPath) {
			continue
		}
		if err := validateLegacyTraversal(rootPath, resolved.Home); err != nil {
			return deny(err)
		}
		kind := "state"
		rootProtected := protected
		if rootPath != cfg.StateDir {
			kind, rootProtected = "checkpoint", map[string]bool{}
		}
		artifacts, err := inventory(ctx, inventoryRoot{path: rootPath, kind: kind, state: cfg.StateDir, protected: rootProtected, configuredFiles: configuredFiles})
		if err != nil {
			return deny(err)
		}
		info, err := os.Lstat(rootPath)
		if err != nil {
			return deny(err)
		}
		id, err := identity(info, true)
		if err != nil {
			return deny(err)
		}
		plan.legacyRoots[rootPath] = id
		plan.LegacyRetained = append(plan.LegacyRetained, rootPath)
		for index := range artifacts {
			if artifacts[index].Path == rootPath {
				artifacts[index].Status = "retain-empty"
			}
		}
		plan.Artifacts = append(plan.Artifacts, artifacts...)
	}
	sort.Strings(plan.LegacyRetained)
	return nil
}

func validateLegacyTraversal(path, home string) error {
	if !within(home, path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("legacy path is not cleanly below authenticated home")
	}
	relative, _ := filepath.Rel(home, path)
	current := home
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy traversal contains symlink component %s", current)
		}
	}
	return nil
}

func (s *Service) addConfiguredScope(ctx context.Context, plan *Plan, cfg config.Config, home string) error {
	state, err := filepath.Abs(cfg.StateDir)
	if err != nil {
		return deny(fmt.Errorf("resolve state directory: %w", err))
	}
	checkpoint, err := filepath.Abs(cfg.Audit.CheckpointDir)
	if err != nil {
		return deny(fmt.Errorf("resolve checkpoint directory: %w", err))
	}
	for _, root := range []string{state, checkpoint} {
		if err = validateScopedPath(root, home, s.RepositoryResetRoot); err != nil {
			return deny(err)
		}
	}

	protected := map[string]bool{}
	configuredFiles := map[string]bool{}
	preserveConfiguredPath := func(label, path string) {
		if path == "" {
			return
		}
		absolute, pathErr := filepath.Abs(path)
		if pathErr != nil {
			plan.Preserved = append(plan.Preserved, label+": unresolved")
			return
		}
		plan.Preserved = append(plan.Preserved, label+": "+absolute)
		protected[absolute] = true
	}
	preserveConfiguredPath("configured API Unix socket", cfg.API.UnixSocket)
	preserveConfiguredPath("configured API TLS certificate", cfg.API.TLSCertFile)
	preserveConfiguredPath("configured API TLS private key", cfg.API.TLSKeyFile)
	preserveConfiguredPath("configured credential-broker socket", cfg.Credentials.Authority.Broker.Socket)
	if cfg.Manager.Inference.Certification != "" {
		certification, certificationErr := filepath.Abs(cfg.Manager.Inference.Certification)
		if certificationErr != nil || !within(state, certification) {
			return deny(errors.New("configured manager certification is outside the validated state root"))
		}
		configuredFiles[certification] = true
		configuredFiles[certification+".new"] = true
	}
	modelStore := filepath.Join(state, "manager", "ollama-models")
	if existsNoFollow(modelStore) {
		if validationErr := validateScopedPath(modelStore, home, s.RepositoryResetRoot); validationErr != nil {
			return deny(fmt.Errorf("unsafe managed model store: %w", validationErr))
		}
		protected[modelStore] = true
		plan.Preserved = append(plan.Preserved, "Aegis managed Ollama model store: "+modelStore)
	}
	authority := cfg.Credentials.Authority
	approvedAuthority := func(path string) (string, bool) {
		if path == "" {
			return "", false
		}
		absolute, pathErr := filepath.Abs(path)
		if pathErr != nil || !within(state, absolute) {
			return absolute, false
		}
		return absolute, true
	}
	if authority.Database != "" {
		database, approved := approvedAuthority(authority.Database)
		if !approved {
			plan.Preserved = append(plan.Preserved, "external credential-authority database: "+database)
			protected[database] = true
		} else {
			if existsNoFollow(database) {
				if validationErr := validateAuthorityDatabase(database, authority.DeploymentID); validationErr != nil {
					return deny(validationErr)
				}
			}
			plan.CredentialRecords = existsNoFollow(database)
			configuredFiles[database] = true
			configuredFiles[database+".initialize"] = true
		}
	}
	if authority.Custody == "systemd" {
		plan.Preserved = append(plan.Preserved, "systemd KEK credential: "+authority.KEKCredential)
	} else if authority.KEKFile != "" {
		kek, approved := approvedAuthority(authority.KEKFile)
		if !approved {
			plan.Preserved = append(plan.Preserved, "external KEK credential: "+kek)
			protected[kek] = true
		} else {
			if existsNoFollow(kek) {
				var validationErr error
				if authority.Custody == "passphrase-file" {
					validationErr = credentials.InspectPassphraseCredential(kek)
				} else {
					validationErr = validateHostKEK(kek)
				}
				if validationErr != nil {
					return deny(validationErr)
				}
			}
			plan.LocalKEK = existsNoFollow(kek)
			configuredFiles[kek] = true
		}
	}

	roots := []inventoryRoot{{path: state, kind: "state", state: state, protected: protected, configuredFiles: configuredFiles}}
	if checkpoint != state && !within(state, checkpoint) {
		roots = append(roots, inventoryRoot{path: checkpoint, kind: "checkpoint", state: state, protected: map[string]bool{}, configuredFiles: configuredFiles})
	}
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return err
		}
		artifacts, inventoryErr := inventory(ctx, root)
		if inventoryErr != nil {
			return deny(inventoryErr)
		}
		plan.Artifacts = append(plan.Artifacts, artifacts...)
	}
	return nil
}

type inventoryRoot struct {
	path, kind, state          string
	protected, configuredFiles map[string]bool
}

func inventory(ctx context.Context, root inventoryRoot) ([]Artifact, error) {
	info, err := os.Lstat(root.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s root is not a real directory: %s", root.kind, root.path)
	}
	if info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("%s root must have mode 0700: %s", root.kind, root.path)
	}
	if _, err = identity(info, true); err != nil {
		return nil, fmt.Errorf("unsafe %s root %s: %w", root.kind, root.path, err)
	}
	var artifacts []Artifact
	err = filepath.WalkDir(root.path, func(path string, entry fs.DirEntry, walkErr error) error {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root.path {
			return nil
		}
		if len(path) > maximumPathBytes || depth(path)-depth(root.path) > maximumPathDepth {
			return fmt.Errorf("reset inventory exceeds path bounds at %s", path)
		}
		for protected := range root.protected {
			if path == protected || within(protected, path) {
				if path == protected && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("reset scope contains a symlink: %s", path)
		}
		relative, relErr := filepath.Rel(root.path, path)
		if relErr != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
			return errors.New("reset inventory escaped its root")
		}
		if !recognized(root, relative, info) {
			return fmt.Errorf("reset scope contains unknown artifact %s", path)
		}
		id, idErr := identity(info, info.IsDir())
		if idErr != nil {
			return fmt.Errorf("unsafe reset artifact %s: %w", path, idErr)
		}
		kind := "file"
		if info.IsDir() {
			kind = "directory"
		}
		artifacts = append(artifacts, Artifact{Path: path, Kind: kind, Status: "delete", identity: id})
		if len(artifacts) > maximumArtifacts {
			return errors.New("reset inventory exceeds the bounded artifact limit")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	filtered := artifacts[:0]
	for _, artifact := range artifacts {
		containsProtected := false
		if artifact.Kind == "directory" {
			for protected := range root.protected {
				if within(artifact.Path, protected) {
					containsProtected = true
					break
				}
			}
		}
		if !containsProtected {
			filtered = append(filtered, artifact)
		}
	}
	artifacts = filtered
	rootContainsProtected := false
	for protected := range root.protected {
		if within(root.path, protected) {
			rootContainsProtected = true
		}
	}
	if !rootContainsProtected {
		id, idErr := identity(info, true)
		if idErr != nil {
			return nil, idErr
		}
		artifacts = append(artifacts, Artifact{Path: root.path, Kind: "directory", Status: "delete-if-empty", identity: id})
	}
	return artifacts, nil
}

func recognized(root inventoryRoot, relative string, info os.FileInfo) bool {
	parts := strings.Split(relative, string(filepath.Separator))
	absolute := filepath.Join(root.path, relative)
	if root.configuredFiles[absolute] && !info.IsDir() && info.Mode().IsRegular() {
		return true
	}
	if root.kind == "checkpoint" {
		if info.IsDir() {
			return false
		}
		return len(parts) == 1 && (parts[0] == "signing-key" || strings.HasSuffix(parts[0], ".json") || strings.HasPrefix(parts[0], ".aegis-"))
	}
	if info.IsDir() {
		for configured := range root.configuredFiles {
			if within(filepath.Join(root.path, relative), configured) {
				return true
			}
		}
		for protected := range root.protected {
			if within(filepath.Join(root.path, relative), protected) {
				return true
			}
		}
		switch parts[0] {
		case "plans", "approvals", "receipts", "mandates", "sessions", "charters", "provisioned", "runtime", "manager", "audit-checkpoints":
			if parts[0] == "runtime" && len(parts) >= 2 && !runtimeName(parts[1]) {
				return false
			}
			if parts[0] == "manager" && len(parts) == 2 && parts[1] != "certifications" && parts[1] != "ollama-models" {
				return false
			}
			return true
		default:
			return false
		}
	}
	if !info.Mode().IsRegular() {
		return false
	}
	if strings.HasPrefix(filepath.Base(relative), ".aegis-") && parts[0] != "manager" && parts[0] != "runtime" {
		return true
	}
	if len(parts) == 1 {
		return parts[0] == ".lock" || parts[0] == "audit.jsonl"
	}
	switch parts[0] {
	case "plans", "approvals", "receipts", "mandates", "sessions":
		return len(parts) == 2 && strings.HasSuffix(parts[1], ".json")
	case "charters":
		return len(parts) == 3 && strings.HasSuffix(parts[2], ".json")
	case "provisioned":
		return strings.HasSuffix(parts[len(parts)-1], ".json")
	case "runtime":
		return len(parts) >= 3 && runtimeName(parts[1])
	case "manager":
		return len(parts) == 3 && parts[1] == "certifications" && (strings.HasSuffix(parts[2], ".json") || strings.HasSuffix(parts[2], ".json.new"))
	case "audit-checkpoints":
		return len(parts) == 2 && (parts[1] == "signing-key" || strings.HasSuffix(parts[1], ".json") || strings.HasPrefix(parts[1], ".aegis-"))
	default:
		return false
	}
}

func runtimeName(name string) bool {
	for _, prefix := range []string{"design-", "stanza-", "manager-", "ollama-"} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return true
		}
	}
	return false
}

func validateAuthorityDatabase(path, deploymentID string) error {
	if _, err := inspectFile(path, "credential-authority database"); err != nil {
		return err
	}
	database, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 250 * time.Millisecond})
	if err != nil {
		return fmt.Errorf("credential-authority ownership evidence is unavailable: %w", err)
	}
	defer database.Close()
	required := []string{"meta", "agents", "deployments", "secret_records", "secret_versions", "credential_bindings", "roles", "role_bindings", "projection_generations", "revocations", "receipts"}
	return database.View(func(transaction *bolt.Tx) error {
		for _, name := range required {
			if transaction.Bucket([]byte(name)) == nil {
				return errors.New("credential-authority database is missing Aegis schema evidence")
			}
		}
		metadata := transaction.Bucket([]byte("meta"))
		if !bytes.Equal(metadata.Get([]byte("schema_version")), []byte("1")) || !bytes.Equal(metadata.Get([]byte("deployment_id")), []byte(deploymentID)) {
			return errors.New("credential-authority database deployment or schema evidence does not match configuration")
		}
		return nil
	})
}

func validateHostKEK(path string) error {
	if _, err := inspectFile(path, "host-file KEK"); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	var evidence struct {
		FormatVersion uint16 `json:"format_version"`
		ID            string `json:"id"`
		Version       uint64 `json:"version"`
		Key           string `json:"key"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&evidence); err != nil || decoder.Decode(&struct{}{}) != io.EOF || evidence.FormatVersion != 1 || evidence.Version == 0 || len(evidence.ID) < 1 || len(evidence.ID) > 128 {
		return errors.New("host-file KEK lacks unambiguous Aegis ownership evidence")
	}
	key, decodeErr := base64.StdEncoding.Strict().DecodeString(evidence.Key)
	if decodeErr != nil || len(key) != 32 {
		return errors.New("host-file KEK lacks unambiguous Aegis ownership evidence")
	}
	for index := range key {
		key[index] = 0
	}
	for _, character := range evidence.ID {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
			return errors.New("host-file KEK lacks unambiguous Aegis ownership evidence")
		}
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, plan Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.verifiedCurrent()
	if err != nil {
		return deny(err)
	}
	if current.Uid != plan.Principal.UID || current.Username != plan.Principal.User {
		return changed(errors.New("authenticated principal changed after preview"))
	}
	if s.BeforeApply != nil {
		s.BeforeApply(plan)
	}
	revalidatePath := plan.ConfigPath
	if plan.Legacy {
		revalidatePath = ""
	}
	revalidated, err := s.Plan(ctx, revalidatePath)
	if err != nil {
		return changed(fmt.Errorf("revalidate reset plan immediately before mutation: %w", err))
	}
	if err = validateCurrentPlan(plan, revalidated); err != nil {
		return changed(fmt.Errorf("%w; no deletion was attempted", err))
	}
	for _, artifact := range plan.Artifacts {
		if err = ctx.Err(); err != nil {
			return err
		}
		info, statErr := os.Lstat(artifact.Path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return changed(statErr)
		}
		got, identityErr := identity(info, info.IsDir())
		if identityErr != nil || got != artifact.identity {
			return changed(fmt.Errorf("artifact identity changed: %s", artifact.Path))
		}
	}
	if plan.Legacy {
		for _, artifact := range plan.Artifacts {
			if artifact.Path == plan.ConfigPath || artifact.Status == "retain-empty" {
				continue
			}
			for root := range plan.legacyRoots {
				if !within(root, artifact.Path) {
					continue
				}
				relative, relErr := filepath.Rel(root, artifact.Path)
				if relErr != nil {
					return incomplete(relErr)
				}
				rootIdentity := plan.legacyRoots[root]
				if err = safefs.RemoveRelativeIdentity(root, relative, artifact.Kind == "directory", rootIdentity.Device, rootIdentity.Inode); err != nil {
					return incomplete(err)
				}
				break
			}
		}
		if plan.configIdentity != nil {
			if err = safefs.RemoveFile(filepath.Dir(plan.ConfigPath), filepath.Base(plan.ConfigPath)); err != nil {
				return incomplete(err)
			}
		}
		if inspection := config.Inspect(""); inspection.State != config.StateAbsent {
			return incomplete(fmt.Errorf("post-reset default configuration state is %s", inspection.State))
		}
		return nil
	}
	// Revalidate the complete immutable plan before the first deletion. Files are
	// removed before directories; os.Remove never follows links or removes a
	// non-empty directory. The configuration is removed last so failed state
	// cleanup cannot masquerade as a completed reset.
	for _, artifact := range plan.Artifacts {
		if artifact.Kind != "file" || artifact.Path == plan.ConfigPath {
			continue
		}
		if err = removeExact(ctx, artifact); err != nil {
			return incomplete(err)
		}
	}
	for _, artifact := range plan.Artifacts {
		if artifact.Kind != "directory" {
			continue
		}
		if artifact.Path == filepath.Dir(plan.ConfigPath) {
			continue
		}
		if err = removeExact(ctx, artifact); err != nil {
			return incomplete(err)
		}
	}
	for _, artifact := range plan.Artifacts {
		if artifact.Kind == "file" && artifact.Path == plan.ConfigPath {
			if err = removeExact(ctx, artifact); err != nil {
				return incomplete(err)
			}
		}
	}
	for _, artifact := range plan.Artifacts {
		if artifact.Kind == "directory" && artifact.Path == filepath.Dir(plan.ConfigPath) {
			if err = removeExact(ctx, artifact); err != nil {
				return incomplete(err)
			}
		}
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateAbsent {
		return incomplete(fmt.Errorf("post-reset configuration state is %s", inspection.State))
	}
	entries, readErr := os.ReadDir(filepath.Dir(plan.ConfigPath))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return incomplete(fmt.Errorf("verify interrupted configuration state: %w", readErr))
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".aegis-model-config-") && strings.HasSuffix(name, ".yaml") {
			return incomplete(fmt.Errorf("recognized interrupted manager configuration remains: %s", filepath.Join(filepath.Dir(plan.ConfigPath), name)))
		}
	}
	return nil
}

func removeExact(ctx context.Context, artifact Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(artifact.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	got, err := identity(info, info.IsDir())
	if err != nil || got != artifact.identity {
		return fmt.Errorf("%s: artifact changed immediately before deletion: %s", ReasonChanged, artifact.Path)
	}
	if err = os.Remove(artifact.Path); err != nil {
		if artifact.Kind == "directory" && errors.Is(err, syscall.ENOTEMPTY) {
			return fmt.Errorf("unknown or changed files preserved in non-empty directory %s", artifact.Path)
		}
		return fmt.Errorf("delete %s: %w", artifact.Path, err)
	}
	return syncDirectory(filepath.Dir(artifact.Path))
}

func inspectFile(path, kind string) (Artifact, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Artifact{}, err
	}
	id, err := identity(info, false)
	if err != nil {
		return Artifact{}, fmt.Errorf("unsafe %s %s: %w", kind, path, err)
	}
	return Artifact{Path: path, Kind: "file", Status: "delete", identity: id}, nil
}

func identity(info os.FileInfo, directory bool) (Identity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, errors.New("filesystem identity is unavailable")
	}
	if directory {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return Identity{}, errors.New("expected a non-symlink directory")
		}
	} else if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return Identity{}, errors.New("expected a non-symlink regular file")
	}
	if int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() {
		return Identity{}, errors.New("ownership does not match the effective operator")
	}
	if info.Mode().Perm()&0022 != 0 {
		return Identity{}, errors.New("artifact is writable by group or others")
	}
	if !directory && stat.Nlink != 1 {
		return Identity{}, errors.New("regular file has ambiguous hard links")
	}
	identified := Identity{Device: uint64(stat.Dev), Inode: stat.Ino, Mode: uint32(info.Mode()), UID: stat.Uid, GID: stat.Gid}
	if !directory {
		identified.Links = uint64(stat.Nlink)
		identified.Size = info.Size()
		identified.ModTime = info.ModTime().UnixNano()
	}
	return identified, nil
}

func validateScopedPath(path, home, repositoryResetRoot string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("path must be clean and absolute")
	}
	volume := filepath.VolumeName(path) + string(filepath.Separator)
	if path == volume || path == home {
		return errors.New("filesystem root and operator home are never reset targets")
	}
	if !within(home, path) {
		return errors.New("path is outside the authenticated operator home")
	}
	repositoryTargetAllowed := repositoryResetRoot != "" && (path == repositoryResetRoot || within(repositoryResetRoot, path))
	current := home
	relative, err := filepath.Rel(home, path)
	if err != nil {
		return err
	}
	components := strings.Split(relative, string(filepath.Separator))
	for index, component := range components {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path contains symlink component %s", current)
		}
		if index < len(components)-1 {
			if !info.IsDir() {
				return fmt.Errorf("path contains non-directory parent %s", current)
			}
			strictArtifactParent := !repositoryTargetAllowed || current == repositoryResetRoot || within(repositoryResetRoot, current)
			if strictArtifactParent {
				if _, identityErr := identity(info, true); identityErr != nil {
					return fmt.Errorf("unsafe parent %s: %w", current, identityErr)
				}
			}
		}
	}
	for current := filepath.Dir(path); within(home, current) || current == home; current = filepath.Dir(current) {
		if existsNoFollow(filepath.Join(current, ".git")) {
			if !repositoryTargetAllowed {
				return fmt.Errorf("repository paths are never reset targets: %s", current)
			}
			break
		}
		if current == home {
			break
		}
	}
	return nil
}

func (s *Service) verifiedCurrent() (*user.User, error) {
	current, err := s.Current()
	if err != nil {
		return nil, fmt.Errorf("authenticate local operator: %w", err)
	}
	if current.Uid == "" || current.Username == "" {
		return nil, errors.New("authenticate local operator: incomplete host identity")
	}
	if _, err = strconv.ParseUint(current.Uid, 10, 32); err != nil {
		return nil, errors.New("authenticate local operator: UID is not numeric")
	}
	lookedUp, err := s.LookupID(current.Uid)
	if err != nil || lookedUp.Uid != current.Uid || lookedUp.Username != current.Username {
		return nil, errors.New("authenticate local operator: host identity is ambiguous")
	}
	return current, nil
}

func uniqueArtifacts(in []Artifact) []Artifact {
	seen := map[string]Artifact{}
	for _, artifact := range in {
		if previous, ok := seen[artifact.Path]; ok && previous.identity != artifact.identity {
			continue
		}
		seen[artifact.Path] = artifact
	}
	out := make([]Artifact, 0, len(seen))
	for _, artifact := range seen {
		out = append(out, artifact)
	}
	return out
}

func validateCurrentPlan(original, current Plan) error {
	if original.Principal != current.Principal || original.ConfigPath != current.ConfigPath || original.ConfigState != current.ConfigState || original.CredentialRecords != current.CredentialRecords || original.LocalKEK != current.LocalKEK || original.Legacy != current.Legacy || strings.Join(original.LegacyRetained, "\x00") != strings.Join(current.LegacyRetained, "\x00") || strings.Join(original.Preserved, "\x00") != strings.Join(current.Preserved, "\x00") {
		return errors.New("reset scope changed after preview")
	}
	expected := make(map[string]Artifact, len(original.Artifacts))
	for _, artifact := range original.Artifacts {
		expected[artifact.Path] = artifact
	}
	for _, artifact := range current.Artifacts {
		previewed, ok := expected[artifact.Path]
		if !ok || previewed.Kind != artifact.Kind || previewed.Status != artifact.Status || previewed.identity != artifact.identity {
			return fmt.Errorf("artifact changed or appeared after preview: %s", artifact.Path)
		}
	}
	if original.configIdentity != nil && (current.configIdentity == nil || *current.configIdentity != *original.configIdentity) {
		return errors.New("configuration identity changed after preview")
	}
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func existsNoFollow(path string) bool { _, err := os.Lstat(path); return err == nil }
func depth(path string) int           { return strings.Count(filepath.Clean(path), string(filepath.Separator)) }
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func deny(err error) error       { return fmt.Errorf("%s: %w", ReasonDenied, err) }
func changed(err error) error    { return fmt.Errorf("%s: %w", ReasonChanged, err) }
func incomplete(err error) error { return fmt.Errorf("%s: %w", ReasonIncomplete, err) }

func PlanDigest(plan Plan) string {
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%t", plan.Principal.UID, plan.Principal.User, plan.ConfigPath, plan.ConfigState, plan.CredentialRecords, plan.LocalKEK, plan.Legacy)
	for _, artifact := range plan.Artifacts {
		_, _ = fmt.Fprintf(hash, "\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d", artifact.Path, artifact.Kind, artifact.Status, artifact.identity.Device, artifact.identity.Inode, artifact.identity.Mode, artifact.identity.UID, artifact.identity.GID, artifact.identity.Links, artifact.identity.Size, artifact.identity.ModTime)
	}
	for _, preserved := range plan.Preserved {
		_, _ = fmt.Fprintf(hash, "\x00preserve\x00%s", preserved)
	}
	for _, retained := range plan.LegacyRetained {
		_, _ = fmt.Fprintf(hash, "\x00retain-empty\x00%s", retained)
	}
	_, _ = fmt.Fprintf(hash, "\x00%s\x00%s", plan.Postcondition, plan.Warning)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
