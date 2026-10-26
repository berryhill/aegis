//go:build coldqualification

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

// Synthetic in-memory custody only. Actual Authority performs envelope encryption.
type coldMemoryCredentials struct {
	credentials.Repository
	r credentials.ResolvedSecret
}

func (*coldMemoryCredentials) StoreID() string      { return "cold-fixture" }
func (*coldMemoryCredentials) DeploymentID() string { return "deployment" }
func (r *coldMemoryCredentials) Create(_ context.Context, s credentials.SecretRecord, v credentials.EncryptedSecretVersion) error {
	r.r.Record = s
	r.r.Version = v
	return nil
}
func (r *coldMemoryCredentials) Resolve(_ context.Context, k credentials.CredentialBindingKey) (credentials.ResolvedSecret, error) {
	if r.r.Binding.Key != k {
		return credentials.ResolvedSecret{}, credentials.ErrNotFound
	}
	return r.r, nil
}

type coldMemoryKey struct{}

func (coldMemoryKey) ActiveKEK(_ context.Context, fn func(credentials.KEKMetadata, []byte) error) error {
	return fn(credentials.KEKMetadata{ID: "synthetic", Version: 1}, make([]byte, 32))
}
func (coldMemoryKey) KEK(_ context.Context, _ string, _ uint64, fn func([]byte) error) error {
	return fn(make([]byte, 32))
}
func coldEncryptedAuthority(t *testing.T) (*credentials.Authority, *coldMemoryCredentials, *ContributionDeliveryCredentialPin) {
	t.Helper()
	r := &coldMemoryCredentials{}
	a := credentials.NewAuthority(r, coldMemoryKey{})
	s, e := a.Create(context.Background(), "synthetic", "api-token", "fixture", []byte("synthetic-controller-token"))
	if e != nil {
		t.Fatal(e)
	}
	k := credentials.CredentialBindingKey{AgentID: "controller", StanzaID: "principal", DeploymentID: "deployment", Scope: "github"}
	r.r.Binding = credentials.CredentialBinding{Key: k, SecretRecord: s.ID, VersionPolicy: credentials.VersionPinned, PinnedVersion: 1, Mode: "use", Destinations: []string{"github"}, Enabled: true, BindingRevision: 1}
	return a, r, &ContributionDeliveryCredentialPin{Key: k, BindingRevision: 1, SecretRecord: s.ID, Version: 1, Destination: "github", Actor: "actor", RepositoryID: 7, Operations: []delivery.Operation{delivery.Merge}}
}
