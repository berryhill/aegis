package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
)

var ErrCredentialUnavailable = errors.New("credential authority unavailable")

// Credential error predicates keep transport adapters on the application
// boundary instead of coupling them to the credential authority package.
func IsCredentialNotFound(err error) bool { return errors.Is(err, credentials.ErrNotFound) }
func IsCredentialRevoked(err error) bool  { return errors.Is(err, credentials.ErrRevoked) }
func IsCredentialAmbiguous(err error) bool {
	return errors.Is(err, credentials.ErrAmbiguous)
}
func IsCredentialConflict(err error) bool { return errors.Is(err, credentials.ErrConflict) }
func IsCredentialLocked(err error) bool   { return credentials.IsPassphraseAuthentication(err) }

// CredentialVersionView is the immutable, metadata-only read of a single
// encrypted credential version. It exposes the verification digest that the
// authoritative verification contract uses, but never the wrapped DEK,
// record nonce, or ciphertext.
type CredentialVersionView struct {
	Version        uint64    `json:"version"`
	FormatVersion  uint16    `json:"format_version"`
	Algorithm      string    `json:"algorithm"`
	KEKVersion     uint64    `json:"kek_version"`
	CreatedAt      time.Time `json:"created_at"`
	CiphertextHash string    `json:"ciphertext_hash"`
}

// CredentialBackupView records whether a backup file was last written and its
// absolute path. The backup bytes are not present; backups are ciphertext-only
// snapshots that require the same KEK to reopen.
type CredentialBackupView struct {
	Available    bool      `json:"available"`
	TargetPath   string    `json:"target_path,omitempty"`
	LastBackupAt time.Time `json:"last_backup_at,omitempty"`
}

// Secret-bearing mutation inputs are shared application contracts. Value is a
// mutable byte slice so transport adapters can wipe each one-use intake.
type CreateCredentialInput struct {
	Reference string `json:"reference"`
	Kind      string `json:"kind"`
	Value     []byte `json:"value"`
}

type RotateCredentialInput struct {
	Value []byte `json:"value"`
}
type RevokeCredentialInput struct {
	Version uint64 `json:"version"`
	Reason  string `json:"reason"`
}
type BindCredentialInput struct {
	AgentID       string   `json:"agent_id"`
	StanzaID      string   `json:"stanza_id"`
	DeploymentID  string   `json:"deployment_id"`
	Scope         string   `json:"scope"`
	Destinations  []string `json:"destinations"`
	Mode          string   `json:"mode"`
	VersionPolicy string   `json:"version_policy"`
	PinnedVersion uint64   `json:"pinned_version,omitempty"`
	Enabled       bool     `json:"enabled"`
}
type CredentialRevocationView struct {
	RecordID string `json:"record_id"`
	Version  uint64 `json:"version"`
	Status   string `json:"status"`
}
type CredentialBindingView struct {
	RecordID      string   `json:"record_id"`
	AgentID       string   `json:"agent_id"`
	StanzaID      string   `json:"stanza_id"`
	DeploymentID  string   `json:"deployment_id"`
	Scope         string   `json:"scope"`
	Destinations  []string `json:"destinations"`
	Mode          string   `json:"mode"`
	VersionPolicy string   `json:"version_policy"`
	PinnedVersion uint64   `json:"pinned_version,omitempty"`
	Enabled       bool     `json:"enabled"`
}
type CredentialBackupResult struct {
	Status      string `json:"status"`
	Destination string `json:"destination"`
}

type CredentialCollectionQuery struct {
	Search, Status, RecordID string
	Page, Limit              int
}

type CredentialCollectionPage struct {
	Records []CredentialView
	Total   int
	Page    int
	Limit   int
}

// QueryCredentialsAs performs filtering, deterministic pagination, matching
// counts, and exact deep-link resolution inside the authoritative store.
func (s *Service) QueryCredentialsAs(ctx context.Context, subject core.Subject, query CredentialCollectionQuery) (CredentialCollectionPage, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return CredentialCollectionPage{}, err
	}
	if !s.hasCredentials() {
		return CredentialCollectionPage{Records: []CredentialView{}, Page: 1, Limit: query.Limit}, nil
	}
	if query.Status == "" {
		query.Status = "all"
	}
	if query.Page < 1 || query.Page > 10000 || query.Limit < 1 || query.Limit > 100 {
		return CredentialCollectionPage{}, errors.New("credential collection page is invalid")
	}
	result, err := s.CredentialAuthority.Query(ctx, credentials.SecretRecordQuery{Search: query.Search, Status: query.Status, RecordID: query.RecordID, Offset: (query.Page - 1) * query.Limit, Limit: query.Limit})
	if err != nil {
		return CredentialCollectionPage{}, err
	}
	page := CredentialCollectionPage{Records: make([]CredentialView, 0, len(result.Records)), Total: result.Total, Page: result.Offset/query.Limit + 1, Limit: query.Limit}
	for _, record := range result.Records {
		view, viewErr := s.buildCredentialView(ctx, record)
		if viewErr != nil {
			return CredentialCollectionPage{}, viewErr
		}
		page.Records = append(page.Records, view)
	}
	return page, nil
}

func (s *Service) CredentialAs(ctx context.Context, subject core.Subject, recordID string) (CredentialView, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return CredentialView{}, err
	}
	if !s.hasCredentials() {
		return CredentialView{}, ErrCredentialUnavailable
	}
	record, err := s.CredentialAuthority.Metadata(ctx, recordID)
	if err != nil {
		return CredentialView{}, err
	}
	return s.buildCredentialView(ctx, record)
}

// VaultStatusView is the read-only projection of credentials.VaultStatus used
// by the dashboard. It intentionally embeds the domain type and adds
// classification fields (state, reason code) the surface needs.
type VaultStatusView struct {
	credentials.VaultStatus
	State      string `json:"state"`
	ReasonCode string `json:"reason_code"`
}

// Source returns a stable readback string for the dashboard status line. It
// never includes the database path on a non-empty vault because the database
// path is operator-only.
func (v VaultStatusView) Source() string {
	if v.DeploymentID == "" {
		return "credentials.authority.unconfigured"
	}
	return "credentials.authority.bbolt"
}

// ConfigureCredentials installs the bbolt credential authority on the service.
// It is a no-op when no repository is configured, so credential-independent
// MVI still works. A nil custodian is allowed for read-only test surfaces.
func (s *Service) ConfigureCredentials(repository credentials.Repository, custodian credentials.KeyCustodian) {
	if repository == nil {
		s.CredentialAuthority = nil
		return
	}
	s.CredentialAuthority = credentials.NewAuthority(repository, custodian)
}

// ListCredentialsAs returns the metadata-only credential views from the
// configured bbolt authority. When no authority is configured the response is
// a ready/empty record with an explanatory reason. Failures are surfaced as
// readiness errors.
func (s *Service) ListCredentialsAs(ctx context.Context, subject core.Subject) ([]CredentialView, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return nil, err
	}
	if !s.hasCredentials() {
		return []CredentialView{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := s.CredentialAuthority.List(ctx, "", 100)
	if err != nil {
		return nil, err
	}
	views := make([]CredentialView, 0, len(records))
	for _, record := range records {
		view, viewErr := s.buildCredentialView(ctx, record)
		if viewErr != nil {
			return nil, viewErr
		}
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		if views[i].Status == views[j].Status {
			return views[i].Reference < views[j].Reference
		}
		return views[i].Status < views[j].Status
	})
	return views, nil
}

func (s *Service) CreateCredentialAs(ctx context.Context, subject core.Subject, input CreateCredentialInput) (CredentialView, error) {
	if err := s.requireCredentialMutation(subject); err != nil {
		return CredentialView{}, err
	}
	if len(input.Value) == 0 {
		return CredentialView{}, errors.New("credential value is required")
	}
	record, err := s.CredentialAuthority.Create(ctx, input.Reference, input.Kind, subject.PrincipalID, input.Value)
	if err != nil {
		_ = s.AuditCredentialOperation(ctx, subject, "credential_created", "denied", "create_failed", "")
		return CredentialView{}, err
	}
	if err = s.AuditCredentialOperation(ctx, subject, "credential_created", "ok", "operator_request", record.ID); err != nil {
		return CredentialView{}, err
	}
	return s.buildCredentialView(ctx, record)
}

func (s *Service) RotateCredentialAs(ctx context.Context, subject core.Subject, recordID string, input RotateCredentialInput) (CredentialView, error) {
	if err := s.requireCredentialMutation(subject); err != nil {
		return CredentialView{}, err
	}
	if len(input.Value) == 0 {
		return CredentialView{}, errors.New("credential value is required")
	}
	record, err := s.CredentialAuthority.Rotate(ctx, recordID, input.Value)
	if err != nil {
		_ = s.AuditCredentialOperation(ctx, subject, "credential_rotated", "denied", "rotation_failed", recordID)
		return CredentialView{}, err
	}
	if err = s.AuditCredentialOperation(ctx, subject, "credential_rotated", "ok", "operator_request", recordID); err != nil {
		return CredentialView{}, err
	}
	return s.buildCredentialView(ctx, record)
}

func (s *Service) RevokeCredentialAs(ctx context.Context, subject core.Subject, recordID string, input RevokeCredentialInput) (CredentialRevocationView, error) {
	if err := s.requireCredentialMutation(subject); err != nil {
		return CredentialRevocationView{}, err
	}
	if err := s.CredentialAuthority.Revoke(ctx, recordID, input.Version, input.Reason); err != nil {
		_ = s.AuditCredentialOperation(ctx, subject, "credential_revoked", "denied", "revocation_failed", recordID)
		return CredentialRevocationView{}, err
	}
	if err := s.AuditCredentialOperation(ctx, subject, "credential_revoked", "ok", input.Reason, recordID); err != nil {
		return CredentialRevocationView{}, err
	}
	return CredentialRevocationView{RecordID: recordID, Version: input.Version, Status: credentials.StatusRevoked}, nil
}

func (s *Service) BindCredentialAs(ctx context.Context, subject core.Subject, recordID string, input BindCredentialInput) (CredentialBindingView, error) {
	if err := s.requireCredentialMutation(subject); err != nil {
		return CredentialBindingView{}, err
	}
	binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: input.AgentID, StanzaID: input.StanzaID, DeploymentID: input.DeploymentID, Scope: input.Scope}, SecretRecord: recordID, VersionPolicy: input.VersionPolicy, PinnedVersion: input.PinnedVersion, Mode: input.Mode, Destinations: append([]string(nil), input.Destinations...), Enabled: input.Enabled}
	if err := s.CredentialAuthority.Bind(ctx, binding); err != nil {
		_ = s.AuditCredentialOperation(ctx, subject, "credential_bound", "denied", "binding_failed", recordID)
		return CredentialBindingView{}, err
	}
	if err := s.AuditCredentialOperation(ctx, subject, "credential_bound", "ok", "operator_request", recordID); err != nil {
		return CredentialBindingView{}, err
	}
	return CredentialBindingView{RecordID: recordID, AgentID: input.AgentID, StanzaID: input.StanzaID, DeploymentID: input.DeploymentID, Scope: input.Scope, Destinations: append([]string(nil), input.Destinations...), Mode: input.Mode, VersionPolicy: input.VersionPolicy, PinnedVersion: input.PinnedVersion, Enabled: input.Enabled}, nil
}

// BackupCredentialsAs selects the destination entirely from server policy.
// Browser and API callers cannot provide an arbitrary host path.
func (s *Service) BackupCredentialsAs(ctx context.Context, subject core.Subject) (CredentialBackupResult, error) {
	if err := s.requireCredentialMutation(subject); err != nil {
		return CredentialBackupResult{}, err
	}
	path := filepath.Clean(s.Config.Credentials.Authority.Database) + ".backup"
	if err := s.CredentialAuthority.Backup(ctx, path); err != nil {
		_ = s.AuditCredentialOperation(ctx, subject, "credential_backup_created", "denied", "backup_failed", "")
		return CredentialBackupResult{}, err
	}
	if err := s.AuditCredentialOperation(ctx, subject, "credential_backup_created", "ok", "operator_request", ""); err != nil {
		return CredentialBackupResult{}, err
	}
	return CredentialBackupResult{Status: "created", Destination: "configured_ciphertext_backup"}, nil
}

func (s *Service) requireCredentialMutation(subject core.Subject) error {
	if err := s.requirePrincipal(subject); err != nil {
		return err
	}
	if !s.hasCredentials() {
		return ErrCredentialUnavailable
	}
	return nil
}

// VaultStatusAs reports the vault classification that should drive the
// credentials dashboard. It is safe to call when the authority is nil.
func (s *Service) VaultStatusAs(ctx context.Context) (VaultStatusView, error) {
	if !s.hasCredentials() {
		return VaultStatusView{State: "unconfigured", ReasonCode: "credentials_authority_not_configured"}, nil
	}
	status, err := s.CredentialAuthority.Status(ctx)
	if err != nil {
		view := VaultStatusView{State: credentials.VaultClassifier(err), ReasonCode: "credentials_vault_read_failed"}
		return view, nil
	}
	status.Custody = s.Config.Credentials.Authority.Custody
	view := VaultStatusView{VaultStatus: status, State: credentials.VaultStateInitialized, ReasonCode: "credentials_vault_ready"}
	return view, nil
}

// buildCredentialView produces the metadata-only surface projection of a
// single encrypted credential record. It never returns plaintext or key
// material; it surfaces only the verification digest, the KEK version, and
// the binding count.
func (s *Service) buildCredentialView(ctx context.Context, record credentials.SecretRecord) (CredentialView, error) {
	view := CredentialView{
		ID:             record.ID,
		Reference:      record.Reference,
		Kind:           record.Kind,
		Status:         record.Status,
		CurrentVersion: record.CurrentVersion,
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339),
		CreatedBy:      record.CreatedBy,
		Type:           record.Kind,
		VersionHistory: []CredentialVersionView{},
		Backup:         CredentialBackupView{Available: false},
	}
	if record.Status == credentials.StatusRevoked {
		view.RevokedAt = record.RevokedAt.UTC().Format(time.RFC3339)
		view.Revocation = record.Revocation
	}
	history, err := s.CredentialAuthority.History(ctx, record.ID, 100)
	if err == nil {
		for _, version := range history {
			view.VersionHistory = append(view.VersionHistory, CredentialVersionView{
				Version:        version.Version,
				FormatVersion:  version.FormatVersion,
				Algorithm:      version.Algorithm,
				KEKVersion:     version.KEKVersion,
				CreatedAt:      version.CreatedAt,
				CiphertextHash: version.CiphertextHash,
			})
		}
	}
	if bindings, countErr := s.CredentialAuthority.BindingCount(ctx, record.ID); countErr == nil {
		view.BindingCount = bindings
	}
	return view, nil
}

// hasCredentials reports whether the service is configured to expose an
// authoritative credential store. It does not check that the store is open.
func (s *Service) hasCredentials() bool {
	return s.CredentialAuthority != nil
}

// CredentialCounts reports active and revoked record counts for the surface.
// It is a no-op when the authority is absent.
func (s *Service) CredentialCounts(ctx context.Context) (credentials.SecretCounts, error) {
	if !s.hasCredentials() {
		return credentials.SecretCounts{}, nil
	}
	return s.CredentialAuthority.Counts(ctx)
}

// IsCredentialError classifies domain credential errors so the readiness
// classifier can deny cleanly.
func IsCredentialError(err error) bool {
	return err != nil && (errors.Is(err, credentials.ErrConflict) || errors.Is(err, credentials.ErrRevoked) || errors.Is(err, credentials.ErrNotFound) || errors.Is(err, credentials.ErrAmbiguous))
}
