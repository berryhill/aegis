// Package onboarding derives bootstrap progress from authoritative artifacts
// and implements exact-plan configuration changes. It never persists an
// optimistic completion flag.
package onboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/manager"
	"go.yaml.in/yaml/v3"
)

type State string

const (
	Uninitialized       State = "uninitialized"
	PrincipalConfigured State = "principal-configured"
	AuthorityConfigured State = "authority-configured"
	RuntimeConfigured   State = "runtime-configured"
	ModelPresent        State = "model-present"
	ModelCertified      State = "model-certified"
	Ready               State = "ready"
	RepairRequired      State = "repair-required"
)

type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Remedy string `json:"remedy,omitempty"`
}

type Snapshot struct {
	State         State   `json:"state"`
	Reason        string  `json:"reason"`
	NextCommand   string  `json:"next_command"`
	ConfigPath    string  `json:"config_path"`
	StatePath     string  `json:"state_path,omitempty"`
	Principal     string  `json:"principal,omitempty"`
	HermesPath    string  `json:"hermes_path,omitempty"`
	HermesVersion string  `json:"hermes_version,omitempty"`
	OllamaRoute   string  `json:"ollama_route,omitempty"`
	Model         string  `json:"model,omitempty"`
	ModelDigest   string  `json:"model_digest,omitempty"`
	Checks        []Check `json:"checks"`
}

type RuntimeDiscovery interface {
	Discover(context.Context) (core.RuntimeDescriptor, error)
}

type Inspector struct {
	Runtime  RuntimeDiscovery
	Current  func() (*user.User, error)
	LookupID func(string) (*user.User, error)
}

func NewInspector(runtime RuntimeDiscovery) *Inspector {
	return &Inspector{Runtime: runtime, Current: user.Current, LookupID: user.LookupId}
}

// Inspect is read-only. It does not open the normal Aegis store, start Ollama,
// load a model, or update credential-authority shutdown metadata.
func (i *Inspector) Inspect(ctx context.Context, configuredPath string) Snapshot {
	ci := config.Inspect(configuredPath)
	s := Snapshot{State: Uninitialized, Reason: ci.ReasonCode, NextCommand: "aegis init", ConfigPath: ci.Path}
	if ci.State == config.StateAbsent {
		s.Checks = append(s.Checks, Check{Name: "principal", Status: "incomplete", Remedy: "aegis init"})
		return s
	}
	if ci.State == config.StatePartial {
		s.Reason = "configuration_initialization_partial"
		s.Checks = append(s.Checks, Check{Name: "configuration", Status: "incomplete", Reason: s.Reason, Remedy: "aegis init"})
		return s
	}
	if ci.State != config.StateValid {
		s.State, s.Reason, s.NextCommand = RepairRequired, ci.ReasonCode, remediation(ci)
		s.Checks = append(s.Checks, Check{Name: "configuration", Status: "repair-required", Reason: ci.ReasonCode, Remedy: s.NextCommand})
		return s
	}
	cfg := ci.Config
	s.StatePath = cfg.StateDir
	s.Principal = cfg.Principal.ID + " (UID " + cfg.Principal.UID + ", user " + cfg.Principal.User + ")"
	current, err := i.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User {
		s.State, s.Reason, s.NextCommand = RepairRequired, "principal_identity_mismatch", "inspect the configured principal and current OS account"
		s.Checks = append(s.Checks, Check{Name: "principal", Status: "repair-required", Reason: s.Reason, Remedy: s.NextCommand})
		return s
	}
	looked, err := i.LookupID(current.Uid)
	if err != nil || looked.Uid != current.Uid || looked.Username != current.Username {
		s.State, s.Reason, s.NextCommand = RepairRequired, "principal_identity_ambiguous", "repair the host account database before retrying"
		return s
	}
	s.State, s.Reason, s.NextCommand = PrincipalConfigured, "credential_authority_incomplete", "aegis init"
	s.Checks = append(s.Checks, Check{Name: "principal", Status: "verified"})
	if err = inspectAuthority(ctx, cfg.Credentials.Authority); err != nil {
		status := "incomplete"
		if systemdAuthorityPrerequisiteIncomplete(cfg.Credentials.Authority) {
			s.Reason = "systemd_authority_prerequisite_incomplete"
			s.NextCommand = "deliver the configured systemd credential and run aegis init"
		} else if authoritySpecified(cfg.Credentials.Authority) {
			status = "repair-required"
			s.State, s.Reason = RepairRequired, "credential_authority_invalid"
		}
		s.Checks = append(s.Checks, Check{Name: "credential-authority", Status: status, Reason: safeReason(err), Remedy: s.NextCommand})
		return s
	}
	s.State, s.Reason = AuthorityConfigured, "runtime_incomplete"
	s.Checks = append(s.Checks, Check{Name: "credential-authority", Status: "verified"})
	if i.Runtime == nil {
		s.State, s.Reason = RepairRequired, "hermes_inspector_unavailable"
		return s
	}
	descriptor, err := i.Runtime.Discover(ctx)
	if err != nil {
		s.Checks = append(s.Checks, Check{Name: "Hermes Agent", Status: "incomplete", Reason: "unsupported_or_absent", Remedy: "install Hermes Agent >=0.18.0,<0.19.0 and run aegis init"})
		return s
	}
	s.HermesPath, s.HermesVersion = descriptor.Executable, descriptor.Version
	s.OllamaRoute = cfg.Manager.Inference.Mode
	if cfg.Manager.Inference.Mode == "external-local" {
		s.OllamaRoute += " " + cfg.Manager.Inference.Endpoint
	}
	if err = inspectOllama(ctx, cfg); err != nil {
		s.Checks = append(s.Checks, Check{Name: "Ollama", Status: "incomplete", Reason: safeReason(err), Remedy: "aegis init"})
		return s
	}
	s.State, s.Reason = RuntimeConfigured, "model_incomplete"
	s.Checks = append(s.Checks, Check{Name: "Hermes Agent", Status: "verified"}, Check{Name: "Ollama", Status: "verified"})
	inf := cfg.Manager.Inference
	if inf.Model == "" {
		s.Checks = append(s.Checks, Check{Name: "exact-model", Status: "incomplete", Reason: manager.ReasonModelAbsent, Remedy: "aegis init"})
		return s
	}
	s.Model, s.ModelDigest = inf.Model, inf.ModelDigest
	if inf.Mode != "external-local" {
		s.Checks = append(s.Checks, Check{Name: "exact-model", Status: "incomplete", Reason: "managed_store_offline", Remedy: "aegis init"})
		return s
	}
	client, err := manager.NewOllamaClient(inf.Endpoint, inf.RequestTimeout)
	if err != nil {
		return withRepair(s, "ollama_route_invalid", err)
	}
	if _, err = client.VerifyModel(ctx, inf.Model, inf.ModelDigest); err != nil {
		s.Checks = append(s.Checks, Check{Name: "exact-model", Status: "repair-required", Reason: safeReason(err), Remedy: "restore the configured exact artifact or run aegis init to select another approved artifact"})
		s.State, s.Reason = RepairRequired, "model_artifact_drift"
		return s
	}
	s.State, s.Reason = ModelPresent, manager.ReasonNotCertified
	s.Checks = append(s.Checks, Check{Name: "exact-model", Status: "verified"})
	version, err := client.Version(ctx)
	if err != nil {
		return withRepair(s, manager.ReasonOllamaUnavailable, err)
	}
	if _, err = manager.LoadCertification(inf.Certification, inf.Model, inf.ModelDigest, descriptor.Version, version, cfg.Manager.Hermes.ContextLength); err != nil {
		s.Checks = append(s.Checks, Check{Name: "certification", Status: "incomplete", Reason: manager.ReasonNotCertified, Remedy: "aegis manager certify " + candidateID(inf.Model)})
		return s
	}
	s.Checks = append(s.Checks, Check{Name: "certification", Status: "verified"})
	s.State, s.Reason, s.NextCommand = Ready, "ready", "aegis"
	return s
}

func withRepair(s Snapshot, reason string, err error) Snapshot {
	s.State, s.Reason, s.NextCommand = RepairRequired, reason, "aegis init"
	s.Checks = append(s.Checks, Check{Name: "runtime-route", Status: "repair-required", Reason: safeReason(err), Remedy: s.NextCommand})
	return s
}

func authoritySpecified(a config.CredentialAuthority) bool {
	return a.Database != "" || a.DeploymentID != "" || a.Custody != "" || a.KEKFile != "" || a.KEKCredential != ""
}

// systemdAuthorityPrerequisiteIncomplete distinguishes an intentionally
// recorded external-custody prerequisite from malformed or drifted authority
// state. Missing delivery, or a database not yet initialized after delivery,
// is resumable onboarding rather than repair-required state.
func systemdAuthorityPrerequisiteIncomplete(a config.CredentialAuthority) bool {
	if a.Custody != "systemd" || a.Database == "" || a.DeploymentID == "" || a.KEKCredential == "" {
		return false
	}
	directory := os.Getenv("CREDENTIALS_DIRECTORY")
	if directory == "" {
		return true
	}
	credential := filepath.Join(directory, a.KEKCredential)
	if _, err := os.Lstat(credential); errors.Is(err, os.ErrNotExist) {
		return true
	}
	custodian, err := credentials.LoadFileCustodian(credential)
	if err != nil {
		return false
	}
	custodian.Close()
	if _, err = os.Lstat(a.Database); errors.Is(err, os.ErrNotExist) {
		return true
	}
	return false
}

func inspectAuthority(ctx context.Context, a config.CredentialAuthority) error {
	if !authoritySpecified(a) {
		return errors.New("authority is absent")
	}
	var path string
	switch a.Custody {
	case "host-file":
		path = a.KEKFile
	case "systemd":
		dir := os.Getenv("CREDENTIALS_DIRECTORY")
		if dir == "" {
			return errors.New("systemd credential directory is unavailable")
		}
		path = filepath.Join(dir, a.KEKCredential)
	default:
		return errors.New("custody is absent or unsupported")
	}
	custodian, err := credentials.LoadFileCustodian(path)
	if err != nil {
		return err
	}
	defer custodian.Close()
	return credentialbolt.Inspect(ctx, a.Database, a.DeploymentID, custodian)
}

func inspectOllama(ctx context.Context, cfg config.Config) error {
	inf := cfg.Manager.Inference
	if inf.Mode == "external-local" {
		client, err := manager.NewOllamaClient(inf.Endpoint, inf.RequestTimeout)
		if err != nil {
			return err
		}
		_, err = client.Version(ctx)
		return err
	}
	path, err := exec.LookPath(inf.Executable)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, path, "--version")
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "OLLAMA_NO_CLOUD=1"}
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		return runErr
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return errors.New("Ollama version output is empty")
	}
	return nil
}

func remediation(ci config.Inspection) string {
	switch ci.State {
	case config.StateLegacy:
		return "run aegis migrate-layout in a real terminal, or run aegis reset to remove the exact legacy installation"
	case config.StateAmbiguous:
		return "inspect canonical and legacy artifacts; preserve one installation explicitly before rerunning aegis"
	case config.StateInsecure:
		return "chmod 600 " + ci.Path + " and verify ownership, then run aegis init"
	case config.StateMalformed:
		return "repair the strict YAML configuration at " + ci.Path + ", then run aegis init"
	default:
		return "inspect " + ci.Path + " and remove ambiguity before running aegis init"
	}
}

func safeReason(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	for _, pair := range []struct{ needle, reason string }{
		{manager.ReasonModelAbsent, manager.ReasonModelAbsent},
		{manager.ReasonDigestMismatch, manager.ReasonDigestMismatch},
		{"permission", "insecure_permissions"}, {"custody", "custody_invalid"},
		{"deployment", "deployment_mismatch"}, {"schema", "schema_invalid"},
		{"sentinel", "sentinel_invalid"},
	} {
		if strings.Contains(text, strings.ToLower(pair.needle)) {
			return pair.reason
		}
	}
	return "validation_failed"
}

func candidateID(model string) string {
	for _, c := range manager.Candidates() {
		if c.OllamaName == model {
			return c.ID
		}
	}
	return "CANDIDATE_ID"
}

type AuthorityPlan struct {
	ConfigPath     string `json:"config_path"`
	StatePath      string `json:"state_path"`
	Custody        string `json:"custody"`
	Database       string `json:"database"`
	DeploymentID   string `json:"deployment_id"`
	KEKFile        string `json:"kek_file,omitempty"`
	KEKCredential  string `json:"kek_credential,omitempty"`
	OriginalDigest string `json:"original_config_digest"`
	ResultDigest   string `json:"result_config_digest"`
	Confirmation   string `json:"confirmation"`
	document       []byte
}

func PreviewAuthority(configPath, custody string) (AuthorityPlan, error) {
	ci := config.Inspect(configPath)
	if ci.State != config.StateValid || !ci.FilePresent {
		return AuthorityPlan{}, errors.New("authority configuration requires one secure file-backed valid configuration")
	}
	if authoritySpecified(ci.Config.Credentials.Authority) {
		return AuthorityPlan{}, errors.New("credential authority is already configured; inspect or repair it rather than rotating it during onboarding")
	}
	if custody != "host-file" && custody != "systemd" {
		return AuthorityPlan{}, errors.New("custody must be host-file or systemd")
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return AuthorityPlan{}, err
	}
	deployment := "deployment-" + hex.EncodeToString(random)
	database := filepath.Join(ci.Config.StateDir, "credentials", "authority.db")
	plan := AuthorityPlan{ConfigPath: ci.Path, StatePath: ci.Config.StateDir, Custody: custody, Database: database, DeploymentID: deployment}
	if custody == "host-file" {
		plan.KEKFile = filepath.Join(ci.Config.StateDir, "credentials", "authority.kek")
	} else {
		plan.KEKCredential = "aegis-authority-kek"
	}
	original, err := os.ReadFile(ci.Path)
	if err != nil {
		return AuthorityPlan{}, err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(original, &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return AuthorityPlan{}, errors.New("configuration YAML cannot be safely updated")
	}
	credentialsNode := mapChild(doc.Content[0], "credentials")
	authority := mapChild(credentialsNode, "authority")
	setScalar(authority, "database", database)
	setScalar(authority, "deployment_id", deployment)
	setScalar(authority, "custody", custody)
	if custody == "host-file" {
		setScalar(authority, "kek_file", plan.KEKFile)
	} else {
		setScalar(authority, "kek_credential", plan.KEKCredential)
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err = encoder.Encode(&doc); err != nil {
		return AuthorityPlan{}, err
	}
	_ = encoder.Close()
	if err = validateGenerated(ci.Path, encoded.Bytes()); err != nil {
		return AuthorityPlan{}, err
	}
	a, b := sha256.Sum256(original), sha256.Sum256(encoded.Bytes())
	plan.OriginalDigest = "sha256:" + hex.EncodeToString(a[:])
	plan.ResultDigest = "sha256:" + hex.EncodeToString(b[:])
	plan.Confirmation = "configure " + custody + " authority " + deployment
	plan.document = append([]byte(nil), encoded.Bytes()...)
	return plan, nil
}

func ApplyAuthority(plan AuthorityPlan) error {
	if len(plan.document) == 0 || plan.ConfigPath == "" {
		return errors.New("authority plan is incomplete")
	}
	current, err := os.ReadFile(plan.ConfigPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(current)
	if "sha256:"+hex.EncodeToString(sum[:]) != plan.OriginalDigest {
		return errors.New("configuration changed after authority preview; no write performed")
	}
	return atomicReplace(plan.ConfigPath, plan.document)
}

func InitializeHostAuthority(ctx context.Context, plan AuthorityPlan) error {
	if plan.Custody != "host-file" || plan.KEKFile == "" {
		return errors.New("host-file authority plan required")
	}
	if _, err := os.Lstat(plan.KEKFile); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("KEK appeared after preview; refusing to reuse it")
	}
	if _, err := os.Lstat(plan.Database); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("authority database appeared after preview; refusing to reuse it")
	}
	succeeded := false
	defer func() {
		if !succeeded {
			CleanupHostAuthority(plan)
		}
	}()
	if err := credentials.CreateHostKey(plan.KEKFile, "host-kek"); err != nil {
		return err
	}
	custodian, err := credentials.LoadFileCustodian(plan.KEKFile)
	if err != nil {
		return err
	}
	defer custodian.Close()
	repository, err := credentialbolt.Open(ctx, plan.Database, plan.DeploymentID, custodian)
	if err != nil {
		return err
	}
	if err = repository.Close(); err != nil {
		return err
	}
	if err = credentialbolt.Inspect(ctx, plan.Database, plan.DeploymentID, custodian); err != nil {
		return err
	}
	succeeded = true
	return nil
}

// InitializeConfiguredSystemdAuthority creates and verifies only the database
// for a previously confirmed systemd-custody configuration. The KEK remains an
// externally delivered systemd credential and is never copied by Aegis.
func InitializeConfiguredSystemdAuthority(ctx context.Context, configPath string) error {
	ci := config.Inspect(configPath)
	if ci.State != config.StateValid || !ci.FilePresent {
		return errors.New("systemd authority initialization requires one secure file-backed valid configuration")
	}
	a := ci.Config.Credentials.Authority
	if a.Custody != "systemd" || a.Database == "" || a.DeploymentID == "" || a.KEKCredential == "" {
		return errors.New("complete systemd authority configuration is required")
	}
	directory := os.Getenv("CREDENTIALS_DIRECTORY")
	if directory == "" {
		return errors.New("systemd credential directory is unavailable")
	}
	custodian, err := credentials.LoadFileCustodian(filepath.Join(directory, a.KEKCredential))
	if err != nil {
		return err
	}
	defer custodian.Close()
	if _, err = os.Lstat(a.Database); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("authority database already exists; inspect it rather than recreating it")
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.Remove(a.Database)
		}
	}()
	repository, err := credentialbolt.Open(ctx, a.Database, a.DeploymentID, custodian)
	if err != nil {
		return err
	}
	if err = repository.Close(); err != nil {
		return err
	}
	if err = credentialbolt.Inspect(ctx, a.Database, a.DeploymentID, custodian); err != nil {
		return err
	}
	succeeded = true
	return nil
}

// CleanupHostAuthority removes only the two artifacts created by a confirmed
// host-file onboarding plan. It is used to roll back a failed pre-publication
// transaction; callers must never use it for a configured authority.
func CleanupHostAuthority(plan AuthorityPlan) {
	if plan.Custody != "host-file" {
		return
	}
	_ = os.Remove(plan.Database)
	_ = os.Remove(plan.KEKFile)
	_ = os.Remove(filepath.Dir(plan.Database))
}

func atomicReplace(path string, document []byte) error {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".aegis-onboarding-*.yaml")
	if err != nil {
		return err
	}
	name, committed := temp.Name(), false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(document)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	committed = true
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func validateGenerated(existing string, document []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(existing), ".aegis-onboarding-validate-*.yaml")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err = temp.Chmod(0600); err == nil {
		_, err = temp.Write(document)
	}
	_ = temp.Close()
	if err != nil {
		return err
	}
	_, err = config.Load(name, nil)
	return err
}

func mapChild(parent *yaml.Node, key string) *yaml.Node {
	for n := 0; n+1 < len(parent.Content); n += 2 {
		if parent.Content[n].Value == key {
			if parent.Content[n+1].Kind != yaml.MappingNode {
				parent.Content[n+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			return parent.Content[n+1]
		}
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, k, v)
	return v
}

func setScalar(parent *yaml.Node, key, value string) {
	for n := 0; n+1 < len(parent.Content); n += 2 {
		if parent.Content[n].Value == key {
			parent.Content[n+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// StatOwned0600 is exposed for truthful previews without leaking platform
// ownership structures into terminal rendering.
func StatOwned0600(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

func (p AuthorityPlan) String() string {
	return fmt.Sprintf("%s authority deployment %s", p.Custody, p.DeploymentID)
}
