package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

type Authority struct {
	repository Repository
	custodian  KeyCustodian
	now        func() time.Time
}

func NewAuthority(repository Repository, custodian KeyCustodian) *Authority {
	return &Authority{repository: repository, custodian: custodian, now: func() time.Time { return time.Now().UTC() }}
}

func (a *Authority) Create(ctx context.Context, reference, kind, createdBy string, plaintext []byte) (SecretRecord, error) {
	var record SecretRecord
	if !ValidateIdentifier(reference) || !ValidateIdentifier(kind) || !ValidateIdentifier(createdBy) {
		return record, errors.New("secret reference, kind, and creator must be valid identifiers")
	}
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return record, errors.New("generate secret record identifier")
	}
	record = SecretRecord{ID: "secret-" + hex.EncodeToString(randomID), Reference: reference, Kind: kind, Status: StatusActive, CurrentVersion: 1, CreatedAt: a.now(), CreatedBy: createdBy}
	encrypted, err := Encrypt(ctx, a.custodian, a.repository.StoreID(), record.ID, 1, kind, plaintext)
	if err != nil {
		return SecretRecord{}, err
	}
	encrypted.CreatedAt = record.CreatedAt
	if err = a.repository.Create(ctx, record, encrypted); err != nil {
		return SecretRecord{}, err
	}
	return record, nil
}

func (a *Authority) Rotate(ctx context.Context, recordID string, plaintext []byte) (SecretRecord, error) {
	record, err := a.repository.Metadata(ctx, recordID)
	if err != nil {
		return record, err
	}
	if record.Status != StatusActive {
		return record, ErrRevoked
	}
	version := record.CurrentVersion + 1
	encrypted, err := Encrypt(ctx, a.custodian, a.repository.StoreID(), record.ID, version, record.Kind, plaintext)
	if err != nil {
		return record, err
	}
	encrypted.CreatedAt = a.now()
	if err = a.repository.AddVersion(ctx, encrypted); err != nil {
		return record, err
	}
	return a.repository.Metadata(ctx, recordID)
}

func (a *Authority) Metadata(ctx context.Context, recordID string) (SecretRecord, error) {
	return a.repository.Metadata(ctx, recordID)
}

func (a *Authority) Bind(ctx context.Context, binding CredentialBinding) error {
	if err := ValidateBinding(binding, a.repository.DeploymentID()); err != nil {
		return err
	}
	return a.repository.Bind(ctx, binding)
}

func (a *Authority) Use(ctx context.Context, key CredentialBindingKey, destination string, fn func([]byte) error) error {
	return a.UseResolved(ctx, key, destination, func(_ ResolvedSecret, plaintext []byte) error { return fn(plaintext) })
}

// UseResolved copies and resolves authority state in a short repository read,
// closes that read transaction, and only then decrypts and invokes fn.
func (a *Authority) UseResolved(ctx context.Context, key CredentialBindingKey, destination string, fn func(ResolvedSecret, []byte) error) error {
	if err := key.Validate(); err != nil || key.DeploymentID != a.repository.DeploymentID() || !ValidateIdentifier(destination) {
		return errors.New("credential use context is invalid")
	}
	resolved, err := a.repository.Resolve(ctx, key)
	if err != nil {
		return err
	}
	if !resolved.Binding.Enabled || resolved.Record.Status != StatusActive || !contains(resolved.Binding.Destinations, destination) {
		return ErrRevoked
	}
	return Decrypt(ctx, a.custodian, a.repository.StoreID(), resolved.Record.Kind, resolved.Version, func(plaintext []byte) error { return fn(resolved, plaintext) })
}

func (a *Authority) Revoke(ctx context.Context, recordID string, version uint64, reason string) error {
	if !ValidateIdentifier(recordID) || !ValidateIdentifier(reason) {
		return errors.New("record identifier and revocation reason are required")
	}
	return a.repository.Revoke(ctx, recordID, version, reason, a.now())
}

func (a *Authority) Backup(ctx context.Context, path string) error {
	return a.repository.Backup(ctx, path)
}
func (a *Authority) Close() error { return a.repository.Close() }

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
