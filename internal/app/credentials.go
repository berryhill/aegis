package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
)

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
