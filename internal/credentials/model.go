package credentials

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"
)

const (
	FormatVersion  uint16 = 1
	Algorithm             = "xchacha20-poly1305"
	StatusActive          = "active"
	StatusRevoked         = "revoked"
	VersionCurrent        = "current"
	VersionPinned         = "pinned"
)

var identifierPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,254}$`)

var (
	ErrNotFound  = errors.New("credential record not found")
	ErrConflict  = errors.New("credential state conflict")
	ErrRevoked   = errors.New("credential is revoked")
	ErrAmbiguous = errors.New("credential binding is ambiguous")
)

type SecretRecord struct {
	ID             string    `json:"id"`
	Reference      string    `json:"reference"`
	Kind           string    `json:"kind"`
	Status         string    `json:"status"`
	CurrentVersion uint64    `json:"current_version"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      string    `json:"created_by"`
	RevokedAt      time.Time `json:"revoked_at,omitempty"`
	Revocation     string    `json:"revocation_reason,omitempty"`
}

type EncryptedSecretVersion struct {
	RecordID       string    `json:"record_id"`
	Version        uint64    `json:"version"`
	FormatVersion  uint16    `json:"format_version"`
	Algorithm      string    `json:"algorithm"`
	KEKID          string    `json:"kek_id"`
	KEKVersion     uint64    `json:"kek_version"`
	RecordNonce    []byte    `json:"record_nonce"`
	Ciphertext     []byte    `json:"ciphertext"`
	WrapNonce      []byte    `json:"wrap_nonce"`
	WrappedDEK     []byte    `json:"wrapped_dek"`
	CiphertextHash string    `json:"ciphertext_hash"`
	CreatedAt      time.Time `json:"created_at"`
}

type CredentialBindingKey struct {
	AgentID      string `json:"agent_id"`
	StanzaID     string `json:"stanza_id"`
	DeploymentID string `json:"deployment_id"`
	Scope        string `json:"scope"`
}

type CredentialBinding struct {
	Key             CredentialBindingKey `json:"key"`
	SecretRecord    string               `json:"secret_record"`
	VersionPolicy   string               `json:"version_policy"`
	PinnedVersion   uint64               `json:"pinned_version,omitempty"`
	Mode            string               `json:"mode"`
	Destinations    []string             `json:"destinations"`
	Enabled         bool                 `json:"enabled"`
	BindingRevision uint64               `json:"binding_revision"`
}

type ResolvedSecret struct {
	Record  SecretRecord
	Version EncryptedSecretVersion
	Binding CredentialBinding
}

type SecretVersionMetadata struct {
	RecordID       string    `json:"record_id"`
	Version        uint64    `json:"version"`
	FormatVersion  uint16    `json:"format_version"`
	Algorithm      string    `json:"algorithm"`
	KEKVersion     uint64    `json:"kek_version"`
	CreatedAt      time.Time `json:"created_at"`
	CiphertextHash string    `json:"ciphertext_hash"`
}

type SecretCounts struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Revoked int `json:"revoked"`
}

type Repository interface {
	StoreID() string
	DeploymentID() string
	Create(context.Context, SecretRecord, EncryptedSecretVersion) error
	AddVersion(context.Context, EncryptedSecretVersion) error
	Metadata(context.Context, string) (SecretRecord, error)
	CurrentByReference(context.Context, string) (SecretRecord, EncryptedSecretVersion, error)
	List(context.Context, string, int) ([]SecretRecord, error)
	Counts(context.Context) (SecretCounts, error)
	Version(context.Context, string, uint64) (EncryptedSecretVersion, error)
	History(context.Context, string, int) ([]SecretVersionMetadata, error)
	Bind(context.Context, CredentialBinding) error
	Resolve(context.Context, CredentialBindingKey) (ResolvedSecret, error)
	Revoke(context.Context, string, uint64, string, time.Time) error
	Backup(context.Context, string) error
	Close() error
}

func ValidateIdentifier(value string) bool { return identifierPattern.MatchString(value) }

func ValidateRecord(record SecretRecord) error {
	if !ValidateIdentifier(record.ID) || !ValidateIdentifier(record.Reference) || !ValidateIdentifier(record.Kind) || !ValidateIdentifier(record.CreatedBy) || record.CurrentVersion == 0 || record.CreatedAt.IsZero() || (record.Status != StatusActive && record.Status != StatusRevoked) {
		return errors.New("credential record metadata is invalid")
	}
	if record.Status == StatusRevoked && (record.RevokedAt.IsZero() || !ValidateIdentifier(record.Revocation)) {
		return errors.New("credential record revocation metadata is invalid")
	}
	return nil
}

func (key CredentialBindingKey) Validate() error {
	if !ValidateIdentifier(key.AgentID) || !ValidateIdentifier(key.StanzaID) || !ValidateIdentifier(key.DeploymentID) || !ValidateIdentifier(key.Scope) {
		return errors.New("credential binding requires valid exact agent, stanza, deployment, and scope identifiers")
	}
	return nil
}

func ValidateBinding(binding CredentialBinding, deploymentID string) error {
	if err := binding.Key.Validate(); err != nil {
		return err
	}
	if binding.Key.DeploymentID != deploymentID || !ValidateIdentifier(binding.SecretRecord) || (binding.VersionPolicy != VersionCurrent && binding.VersionPolicy != VersionPinned) || (binding.VersionPolicy == VersionPinned && binding.PinnedVersion == 0) || (binding.VersionPolicy == VersionCurrent && binding.PinnedVersion != 0) || !ValidateIdentifier(binding.Mode) || len(binding.Destinations) == 0 {
		return errors.New("credential binding is invalid or targets another deployment")
	}
	destinations := append([]string(nil), binding.Destinations...)
	sort.Strings(destinations)
	for index, destination := range destinations {
		if !ValidateIdentifier(destination) || (index > 0 && destination == destinations[index-1]) {
			return errors.New("credential binding destination is invalid or duplicated")
		}
	}
	return nil
}
