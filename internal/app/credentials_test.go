package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
)

// fakeAuthority is a minimal Repository that backs List and Status without
// touching bbolt. It is intentionally metadata-only.
type fakeAuthority struct {
	records   []credentials.SecretRecord
	status    credentials.VaultStatus
	statusErr error
}

func (f *fakeAuthority) StoreID() string      { return f.status.StoreID }
func (f *fakeAuthority) DeploymentID() string { return f.status.DeploymentID }
func (f *fakeAuthority) Status(context.Context) (credentials.VaultStatus, error) {
	return f.status, f.statusErr
}
func (f *fakeAuthority) Create(context.Context, credentials.SecretRecord, credentials.EncryptedSecretVersion) error {
	return errors.New("not used")
}
func (f *fakeAuthority) AddVersion(context.Context, credentials.EncryptedSecretVersion) error {
	return errors.New("not used")
}
func (f *fakeAuthority) Metadata(context.Context, string) (credentials.SecretRecord, error) {
	return credentials.SecretRecord{}, credentials.ErrNotFound
}
func (f *fakeAuthority) CurrentByReference(context.Context, string) (credentials.SecretRecord, credentials.EncryptedSecretVersion, error) {
	return credentials.SecretRecord{}, credentials.EncryptedSecretVersion{}, credentials.ErrNotFound
}
func (f *fakeAuthority) List(_ context.Context, query string, _ int) ([]credentials.SecretRecord, error) {
	if query == "" {
		return f.records, nil
	}
	out := make([]credentials.SecretRecord, 0, len(f.records))
	for _, r := range f.records {
		if r.Reference == query {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeAuthority) Counts(context.Context) (credentials.SecretCounts, error) {
	return credentials.SecretCounts{Total: len(f.records), Active: len(f.records)}, nil
}
func (f *fakeAuthority) Version(context.Context, string, uint64) (credentials.EncryptedSecretVersion, error) {
	return credentials.EncryptedSecretVersion{}, credentials.ErrNotFound
}
func (f *fakeAuthority) History(_ context.Context, recordID string, _ int) ([]credentials.SecretVersionMetadata, error) {
	for _, r := range f.records {
		if r.ID == recordID {
			return []credentials.SecretVersionMetadata{{RecordID: r.ID, Version: r.CurrentVersion, FormatVersion: 1, Algorithm: "xchacha20-poly1305", KEKVersion: 1, CreatedAt: r.CreatedAt, CiphertextHash: "sha256:" + r.ID}}, nil
		}
	}
	return nil, credentials.ErrNotFound
}
func (f *fakeAuthority) Bind(context.Context, credentials.CredentialBinding) error {
	return nil
}
func (f *fakeAuthority) Resolve(context.Context, credentials.CredentialBindingKey) (credentials.ResolvedSecret, error) {
	return credentials.ResolvedSecret{}, credentials.ErrNotFound
}
func (f *fakeAuthority) BindingCount(_ context.Context, recordID string) (int, error) {
	if recordID == "" {
		return 0, errors.New("invalid record id")
	}
	return 2, nil
}
func (f *fakeAuthority) Revoke(context.Context, string, uint64, string, time.Time) error {
	return nil
}
func (f *fakeAuthority) Backup(context.Context, string) error { return nil }
func (f *fakeAuthority) Close() error                         { return nil }

func newServiceWithFakeAuthority(t *testing.T, repo credentials.Repository) *Service {
	t.Helper()
	cfg := config.Defaults()
	cfg.Credentials.Authority = config.CredentialAuthority{
		Database:     "/tmp/test.db",
		DeploymentID: "deployment-test",
		Custody:      "host-file",
		KEKFile:      "/tmp/kek",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(cfg, nil, nil, nil, nil, logger)
	svc.ConfigureCredentials(repo, nil)
	return svc
}

func newServiceWithoutAuthority(t *testing.T) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(config.Defaults(), nil, nil, nil, nil, logger)
	return svc
}

func TestListCredentialsAsReturnsEmptyWhenAuthorityAbsent(t *testing.T) {
	svc := newServiceWithoutAuthority(t)
	subject := core.Subject{PrincipalID: "principal", ID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	views, err := svc.ListCredentialsAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 0 {
		t.Fatalf("expected empty list without authority, got %d", len(views))
	}
}

func TestListCredentialsAsProjectsMetadataAndExcludesCiphertext(t *testing.T) {
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	repo := &fakeAuthority{records: []credentials.SecretRecord{
		{ID: "secret-a", Reference: "github/api", Kind: "api-token", Status: "active", CurrentVersion: 2, CreatedAt: at, CreatedBy: "principal"},
		{ID: "secret-b", Reference: "github/legacy", Kind: "api-token", Status: "revoked", CurrentVersion: 1, CreatedAt: at, CreatedBy: "principal", RevokedAt: at.Add(time.Hour), Revocation: "rotation"},
	}, status: credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"}}
	svc := newServiceWithFakeAuthority(t, repo)
	subject := core.Subject{PrincipalID: "principal", ID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	views, err := svc.ListCredentialsAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}
	for _, v := range views {
		if v.Reference == "" || v.ID == "" || v.CurrentVersion == 0 {
			t.Fatalf("metadata-only projection is incomplete: %+v", v)
		}
		if v.BindingCount != 2 {
			t.Fatalf("binding count was not projected: %+v", v)
		}
		if len(v.VersionHistory) == 0 {
			t.Fatalf("version history was not projected: %+v", v)
		}
	}
}

func TestListCredentialsAsDeniesUnauthenticatedSubject(t *testing.T) {
	repo := &fakeAuthority{}
	svc := newServiceWithFakeAuthority(t, repo)
	subject := core.Subject{PrincipalID: "different", ID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	if _, err := svc.ListCredentialsAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected ErrDenied for mismatched principal, got %v", err)
	}
}

func TestVaultStatusAsReturnsUnconfiguredWhenAuthorityAbsent(t *testing.T) {
	svc := newServiceWithoutAuthority(t)
	view, err := svc.VaultStatusAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "unconfigured" || view.ReasonCode != "credentials_authority_not_configured" {
		t.Fatalf("expected unconfigured state, got %+v", view)
	}
}

// TestVaultStatusAsReturnsInitializedWhenRepositoryIsHealthy ensures the
// healthy code path renders an initialized vault with all KEK metadata.
func TestVaultStatusAsReturnsInitializedWhenRepositoryIsHealthy(t *testing.T) {
	repo := &fakeAuthority{status: credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"}}
	svc := newServiceWithFakeAuthority(t, repo)
	view, err := svc.VaultStatusAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "initialized" || view.KEKID != "kek-1" || view.KEKVersion != 1 {
		t.Fatalf("expected initialized vault, got %+v", view)
	}
}

// TestVaultStatusAsClassifiesRepositoryError verifies that a repository
// failure surfaces as an unavailable vault without leaking the error to the
// caller.
func TestVaultStatusAsClassifiesRepositoryError(t *testing.T) {
	repo := &fakeAuthority{statusErr: errors.New("disk fault")}
	svc := newServiceWithFakeAuthority(t, repo)
	view, err := svc.VaultStatusAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "unavailable" {
		t.Fatalf("expected unavailable state, got %+v", view)
	}
}

// TestVaultStatusAsReportsLockedForPassphraseAuthentication pins the locked
// vault code path. The dashboard must render "locked" when the passphrase-
// protected KEK cannot be unlocked, and must not leak the underlying error
// to the caller.
func TestVaultStatusAsReportsLockedForPassphraseAuthentication(t *testing.T) {
	repo := &fakeAuthority{statusErr: credentials.ErrPassphraseAuthentication}
	svc := newServiceWithFakeAuthority(t, repo)
	view, err := svc.VaultStatusAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "locked" {
		t.Fatalf("expected locked state, got %+v", view)
	}
}

// TestVaultStatusAsReportsEmptyForNotFound pins the difference between an
// empty vault and a missing or unavailable one. A NotFound returned by
// Status() means the repository is open and the schema is healthy, but no
// metadata has been written yet.
func TestVaultStatusAsReportsEmptyForNotFound(t *testing.T) {
	repo := &fakeAuthority{statusErr: credentials.ErrNotFound}
	svc := newServiceWithFakeAuthority(t, repo)
	view, err := svc.VaultStatusAs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.State != "empty" {
		t.Fatalf("expected empty state, got %+v", view)
	}
}

// TestCredentialReadinessClassifiesAllVaultStates pins the readiness
// classifier that drives the dashboard. Each vault state must surface a
// distinct reason code; the dashboard relies on these reason codes to
// render the correct copy and never assert a count on a non-authoritative
// state.
func TestCredentialReadinessClassifiesAllVaultStates(t *testing.T) {
	cases := []struct {
		name       string
		views      []CredentialView
		readErr    error
		vault      VaultStatusView
		configured bool
		wantState  string
		wantReason string
	}{
		{
			name: "unconfigured", configured: false, vault: VaultStatusView{State: "unconfigured"},
			wantState: "unconfigured", wantReason: "credentials_authority_not_configured",
		},
		{
			name: "error on read", configured: true, readErr: errors.New("disk fault"),
			vault: VaultStatusView{State: "unavailable"}, wantState: "error", wantReason: "credentials_authority_read_failed",
		},
		{
			name: "locked vault", configured: true, vault: VaultStatusView{State: "locked"},
			wantState: "locked", wantReason: "credentials_authority_locked",
		},
		{
			name: "corrupt vault", configured: true, vault: VaultStatusView{State: "corrupt"},
			wantState: "degraded_repair_required", wantReason: "credentials_authority_corrupt",
		},
		{
			name: "unavailable vault", configured: true, vault: VaultStatusView{State: "unavailable"},
			wantState: "unavailable", wantReason: "credentials_authority_unavailable",
		},
		{
			name: "empty vault", configured: true, vault: VaultStatusView{State: "initialized"}, views: []CredentialView{},
			wantState: "empty", wantReason: "credentials_authority_empty",
		},
		{
			name: "ready vault", configured: true, vault: VaultStatusView{State: "initialized"},
			views:     []CredentialView{{ID: "secret-1", Status: "active"}, {ID: "secret-2", Status: "revoked"}},
			wantState: "ready", wantReason: "credentials_authority_read_succeeded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := credentialReadiness(tc.views, tc.readErr, tc.vault, tc.configured)
			if got.State != tc.wantState || got.ReasonCode != tc.wantReason {
				t.Fatalf("got=%+v want state=%q reason=%q", got, tc.wantState, tc.wantReason)
			}
			// Authoritative must be false for every failure state and true for empty/ready.
			wantAuth := tc.wantState == "ready" || tc.wantState == "empty"
			if got.Authoritative != wantAuth {
				t.Fatalf("got.Authoritative=%v want %v (state=%q)", got.Authoritative, wantAuth, got.State)
			}
		})
	}
}

// TestCredentialReadinessEmptyIsAuthoritativeButAssertsZeroCount pins the
// "credential-independent MVI" guarantee at the readiness level. An empty
// vault is still authoritative; the count is zero by design, not by failure.
// The dashboard must be able to distinguish "vault is healthy and empty" from
// "vault could not be read".
func TestCredentialReadinessEmptyIsAuthoritativeButAssertsZeroCount(t *testing.T) {
	got := credentialReadiness(nil, nil, VaultStatusView{State: "initialized"}, true)
	if !got.Authoritative || got.Count != 0 || got.State != "empty" {
		t.Fatalf("expected authoritative empty, got %+v", got)
	}
}

// TestListCredentialsAsCountsActiveAndRevokedSeparately pins the dashboard
// contract: a credential view must always carry the original Status field
// so the surface can render an active/revoked split without re-querying
// the bbolt authority.
func TestListCredentialsAsCountsActiveAndRevokedSeparately(t *testing.T) {
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	repo := &fakeAuthority{records: []credentials.SecretRecord{
		{ID: "secret-a", Reference: "github/a", Kind: "api-token", Status: "active", CurrentVersion: 1, CreatedAt: at, CreatedBy: "principal"},
		{ID: "secret-b", Reference: "github/b", Kind: "api-token", Status: "active", CurrentVersion: 1, CreatedAt: at, CreatedBy: "principal"},
		{ID: "secret-c", Reference: "github/c", Kind: "api-token", Status: "revoked", CurrentVersion: 1, CreatedAt: at, CreatedBy: "principal", RevokedAt: at.Add(time.Hour), Revocation: "rotation"},
	}, status: credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"}}
	svc := newServiceWithFakeAuthority(t, repo)
	subject := core.Subject{PrincipalID: "principal", ID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	views, err := svc.ListCredentialsAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	var active, revoked int
	for _, v := range views {
		switch v.Status {
		case "active":
			active++
		case "revoked":
			revoked++
		}
	}
	if active != 2 || revoked != 1 {
		t.Fatalf("expected 2 active and 1 revoked, got active=%d revoked=%d (views=%+v)", active, revoked, views)
	}
}

// TestListCredentialsAsDoesNotReadFromProviderAuthConfig is the regression
// test for the original bug: the fleet Credentials surface used to be
// populated from Config.Credentials.ProviderAuth, which is the wrong
// source of truth. The bbolt authority is the only authoritative source;
// ProviderAuth is an environment-binding map and must not leak into the
// dashboard list.
func TestListCredentialsAsDoesNotReadFromProviderAuthConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Credentials.Authority = config.CredentialAuthority{
		Database: "/tmp/test.db", DeploymentID: "deployment-test", Custody: "host-file", KEKFile: "/tmp/kek",
	}
	// ProviderAuth must be populated; if the dashboard reads from it, the
	// surface would show this binding instead of the empty bbolt authority.
	cfg.Credentials.ProviderAuth["GITHUB_TOKEN"] = config.EnvironmentCredentialBinding{
		Type: "bearer", SourceEnv: "GITHUB_TOKEN", TargetEnv: "GH_TOKEN",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(cfg, nil, nil, nil, nil, logger)
	// Configure with an empty authority (no records). ProviderAuth must not
	// leak into the surface.
	svc.ConfigureCredentials(&fakeAuthority{status: credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"}}, nil)
	subject := core.Subject{PrincipalID: "principal", ID: "sub-1", ExpiresAt: time.Now().Add(time.Hour)}
	views, err := svc.ListCredentialsAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 0 {
		t.Fatalf("ProviderAuth leaked into the credential surface: %+v", views)
	}
	for _, v := range views {
		if v.Reference == "GITHUB_TOKEN" || v.Reference == "GH_TOKEN" {
			t.Fatalf("ProviderAuth environment binding surfaced as credential: %+v", v)
		}
	}
}

// fleetSurfaceSubject returns the configured principal as an authenticated
// subject so the FleetSurfaceAs readiness checks succeed.
func fleetSurfaceSubject(now time.Time) core.Subject {
	return core.Subject{PrincipalID: "principal", ID: "sub-fleet", Kind: "human", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour)}
}

// TestFleetSurfaceAsPopulatesCredentialsAndVaultStatusFromAuthority pins the
// full integration: a configured bbolt authority must populate
// surface.Credentials, surface.CredentialRecords (same slice), surface.VaultStatus,
// and surface.Readiness["credentials"] with the ready/authoritative code
// path. ProviderAuth must remain absent from the surface.
func TestFleetSurfaceAsPopulatesCredentialsAndVaultStatusFromAuthority(t *testing.T) {
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	repo := &fakeAuthority{records: []credentials.SecretRecord{
		{ID: "secret-a", Reference: "github/api", Kind: "api-token", Status: "active", CurrentVersion: 2, CreatedAt: at, CreatedBy: "principal"},
		{ID: "secret-b", Reference: "github/legacy", Kind: "api-token", Status: "revoked", CurrentVersion: 1, CreatedAt: at, CreatedBy: "principal", RevokedAt: at.Add(time.Hour), Revocation: "rotation"},
	}, status: credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"}}
	svc := newServiceWithFakeAuthority(t, repo)
	// ProviderAuth must not leak even when fleet surface reads everything.
	svc.Config.Credentials.ProviderAuth["GH"] = config.EnvironmentCredentialBinding{Type: "bearer", SourceEnv: "GH_SRC", TargetEnv: "GH_DST"}
	surface, err := svc.FleetSurfaceAs(context.Background(), fleetSurfaceSubject(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if len(surface.Credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d: %+v", len(surface.Credentials), surface.Credentials)
	}
	if &surface.Credentials[0] != &surface.CredentialRecords[0] || len(surface.Credentials) != len(surface.CredentialRecords) {
		t.Fatalf("Credentials and CredentialRecords must be the same authoritative slice: creds=%+v records=%+v", surface.Credentials, surface.CredentialRecords)
	}
	if surface.VaultStatus.State != "initialized" || surface.VaultStatus.KEKID != "kek-1" || surface.VaultStatus.KEKVersion != 1 {
		t.Fatalf("vault status was not projected into the surface: %+v", surface.VaultStatus)
	}
	readiness, ok := surface.Readiness["credentials"]
	if !ok {
		t.Fatalf("credentials readiness missing from surface: %+v", surface.Readiness)
	}
	if !readiness.Authoritative || readiness.State != "ready" || readiness.Count != 2 || readiness.Source != "credentials.authority.bbolt" {
		t.Fatalf("credentials readiness was not authoritative/ready: %+v", readiness)
	}
	for _, view := range surface.Credentials {
		if view.Reference == "GH" || view.Reference == "GH_DST" {
			t.Fatalf("ProviderAuth leaked into FleetSurface credentials: %+v", view)
		}
		if len(view.VersionHistory) == 0 || view.BindingCount == 0 {
			t.Fatalf("metadata-only projection missing fields on %s: %+v", view.ID, view)
		}
	}
}

// TestFleetSurfaceAsCredentialReadinessReportsLockedForPassphraseFailure
// pins the locked vault readiness at the FleetSurface level. The dashboard
// must render "locked" when the passphrase-protected KEK cannot be
// unlocked, and must never assert a count when the authority is locked.
// (In production, a passphrase failure during bbolt Open() prevents the
// store from ever being installed; this test simulates the runtime
// scenario where the store was opened with a stale or absent passphrase
// custodian, so List() returns metadata-only records while Status() returns
// ErrPassphraseAuthentication.)
func TestFleetSurfaceAsCredentialReadinessReportsLockedForPassphraseFailure(t *testing.T) {
	repo := &fakeAuthority{
		records:   []credentials.SecretRecord{{ID: "secret-locked", Reference: "locked/api", Kind: "api-token", Status: "active", CurrentVersion: 1, CreatedAt: time.Now(), CreatedBy: "principal"}},
		status:    credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1"},
		statusErr: fmt.Errorf("wrapped: %w", credentials.ErrPassphraseAuthentication),
	}
	svc := newServiceWithFakeAuthority(t, repo)
	surface, err := svc.FleetSurfaceAs(context.Background(), fleetSurfaceSubject(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	readiness, ok := surface.Readiness["credentials"]
	if !ok {
		t.Fatalf("credentials readiness missing: %+v", surface.Readiness)
	}
	if readiness.Authoritative || readiness.State != "locked" || readiness.Count != 0 || readiness.Source != "credentials.authority.bbolt" {
		t.Fatalf("locked vault was not classified: %+v", readiness)
	}
	// The vault status view itself must classify as locked; this is what
	// the dashboard reads, not the underlying error string.
	if surface.VaultStatus.State != "locked" {
		t.Fatalf("vault status view did not classify as locked: %+v", surface.VaultStatus)
	}
}

// TestFleetSurfaceAsCredentialReadinessReportsUnavailableForRepositoryError
// pins the "unavailable" classification when Status() returns an
// unexpected repository error. The dashboard must not assert a count and
// must not surface any ciphertext or record metadata.
func TestFleetSurfaceAsCredentialReadinessReportsUnavailableForRepositoryError(t *testing.T) {
	repo := &fakeAuthority{
		statusErr: errors.New("disk fault"),
	}
	svc := newServiceWithFakeAuthority(t, repo)
	surface, err := svc.FleetSurfaceAs(context.Background(), fleetSurfaceSubject(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	readiness, ok := surface.Readiness["credentials"]
	if !ok {
		t.Fatalf("credentials readiness missing: %+v", surface.Readiness)
	}
	if readiness.Authoritative || readiness.State != "unavailable" || readiness.Count != 0 {
		t.Fatalf("unavailable vault was misclassified: %+v", readiness)
	}
}

// TestFleetSurfaceAsCredentialReadinessEmptyIsAuthoritativeButAssertsZeroCount
// is the FleetSurface-level companion to TestCredentialReadinessEmptyIsAuthoritativeButAssertsZeroCount.
// An empty authority must report ready/empty with Authoritative=true and
// Count=0; the dashboard must be able to distinguish "vault is healthy and
// empty" from "vault could not be read".
func TestFleetSurfaceAsCredentialReadinessEmptyIsAuthoritativeButAssertsZeroCount(t *testing.T) {
	repo := &fakeAuthority{
		records: nil,
		status:  credentials.VaultStatus{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file"},
	}
	svc := newServiceWithFakeAuthority(t, repo)
	surface, err := svc.FleetSurfaceAs(context.Background(), fleetSurfaceSubject(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	readiness, ok := surface.Readiness["credentials"]
	if !ok {
		t.Fatalf("credentials readiness missing: %+v", surface.Readiness)
	}
	if !readiness.Authoritative || readiness.State != "empty" || readiness.Count != 0 {
		t.Fatalf("empty vault was misclassified: %+v", readiness)
	}
}

// TestFleetSurfaceAsUnconfiguredAuthorityDoesNotAssertACount pins the
// "credential-independent MVI" precondition: when no authority is
// configured, FleetSurfaceAs must report State=unconfigured, Count=0,
// Authoritative=false, and must not leak ProviderAuth into Credentials.
func TestFleetSurfaceAsUnconfiguredAuthorityDoesNotAssertACount(t *testing.T) {
	cfg := config.Defaults()
	cfg.Credentials.ProviderAuth["GH"] = config.EnvironmentCredentialBinding{Type: "bearer", SourceEnv: "GH_SRC", TargetEnv: "GH_DST"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := New(cfg, nil, nil, nil, nil, logger)
	surface, err := svc.FleetSurfaceAs(context.Background(), fleetSurfaceSubject(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	readiness, ok := surface.Readiness["credentials"]
	if !ok {
		t.Fatalf("credentials readiness missing: %+v", surface.Readiness)
	}
	if readiness.Authoritative || readiness.State != "unconfigured" || readiness.Count != 0 || readiness.Source != "credentials.authority.unconfigured" {
		t.Fatalf("unconfigured authority was misclassified: %+v", readiness)
	}
	if len(surface.Credentials) != 0 || len(surface.CredentialRecords) != 0 {
		t.Fatalf("unconfigured authority must not surface records: creds=%+v records=%+v", surface.Credentials, surface.CredentialRecords)
	}
	if surface.VaultStatus.State != "unconfigured" {
		t.Fatalf("unconfigured vault status was not surfaced: %+v", surface.VaultStatus)
	}
}
