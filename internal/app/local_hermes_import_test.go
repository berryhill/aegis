package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/registry"
)

type failOnceAgentRegistrationRepository struct {
	fleet.Repository
	fail bool
}

func (repository *failOnceAgentRegistrationRepository) RegisterAgent(ctx context.Context, registration registry.AgentRegistration, revision registry.AgentRevision, fact fleet.AuditFact) (bool, error) {
	if repository.fail {
		repository.fail = false
		return false, errors.New("injected Agent registration failure")
	}
	return repository.Repository.RegisterAgent(ctx, registration, revision, fact)
}

func localImportPrincipal(service *Service) core.Subject {
	now := service.Now()
	return core.Subject{ID: "local-uid:" + service.Config.Principal.UID, Kind: "human", PrincipalID: service.Config.Principal.ID, Issuer: "linux-so-peercred", Method: "local-os", AuthenticatedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Claims: map[string]string{"uid": service.Config.Principal.UID}}
}

func configureLocalImportFleet(t *testing.T, service *Service, repository fleet.Repository) {
	t.Helper()
	fleetService, err := orchestration.NewFleetService(repository, service.Authority, service.AuthorityCommands, func(_ context.Context, _ orchestration.FleetAction, candidate core.Subject) error {
		return service.RequirePrincipal(candidate)
	}, nil, service.Now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(service.Store)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := orchestration.NewQueueWorker(repository, fleetService, service.Store, verifier, orchestration.NoKeyAdapter{}, service.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ConfigureFleet(repository, fleetService, worker); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLocalHermesAgentImportIsDeterministicAndNonMutating(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(marker, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	subject := localImportPrincipal(service)

	first, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.RevisionDigest == "" || first.ProfileFingerprint == "" {
		t.Fatalf("non-deterministic proposal: first=%+v second=%+v", first, second)
	}
	if first.Runtime != "hermes / hermes-agent / aegis-owned-ephemeral" || first.SelectedProfile != "profile/default" {
		t.Fatalf("unsafe runtime/provenance binding: %+v", first)
	}
	if _, err := service.GetCharter(first.AgentID, 1); err == nil {
		t.Fatal("prepare persisted a charter")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "version: 1\n" {
		t.Fatalf("profile mutated: data=%q err=%v", data, err)
	}
}

func TestPrepareLocalHermesAgentImportFingerprintExcludesMarkerContents(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "config.yaml")
	firstContents := []byte("secret: canary-one\n")
	secondContents := []byte("secret: canary-two\n")
	if len(firstContents) != len(secondContents) {
		t.Fatal("test fixture contents must have equal length")
	}
	if err := os.WriteFile(marker, firstContents, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	subject := localImportPrincipal(service)

	first, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, secondContents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(marker, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	second, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	if first.ProfileFingerprint != second.ProfileFingerprint {
		t.Fatalf("profile fingerprint derived from secret-bearing marker contents: first=%q second=%q", first.ProfileFingerprint, second.ProfileFingerprint)
	}
}

func TestPrepareLocalHermesAgentImportRejectsUnsafeMarker(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }

	if _, err := service.PrepareLocalHermesAgentImportAs(context.Background(), localImportPrincipal(service)); err == nil {
		t.Fatal("symlinked marker was accepted")
	}
}

func TestPrepareLocalHermesAgentImportRejectsUnsafeProfileEvidence(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "empty marker", setup: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "config.yaml"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "oversized marker", setup: func(t *testing.T, root string) {
			marker := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(marker, maximumLocalHermesMarkerBytes+1); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory marker", setup: func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "config.yaml"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "world-writable marker", setup: func(t *testing.T, root string) {
			marker := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(marker, []byte("version: 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(marker, 0o666); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "world-writable root", setup: func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o777); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := testService(t)
			root := filepath.Join(t.TempDir(), ".hermes")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			test.setup(t, root)
			service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
			service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
			if _, err := service.PrepareLocalHermesAgentImportAs(context.Background(), localImportPrincipal(service)); err == nil {
				t.Fatal("unsafe local Hermes profile evidence was accepted")
			}
		})
	}
}

func TestLocalHermesAgentImportRequiresFreshConfiguredUnixPeer(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	valid := localImportPrincipal(service)

	tests := []struct {
		name   string
		mutate func(*core.Subject)
	}{
		{name: "wrong issuer", mutate: func(subject *core.Subject) { subject.Issuer = "local-os" }},
		{name: "wrong method", mutate: func(subject *core.Subject) { subject.Method = "prompt" }},
		{name: "wrong subject ID", mutate: func(subject *core.Subject) { subject.ID = "local-uid:999" }},
		{name: "missing UID claim", mutate: func(subject *core.Subject) { delete(subject.Claims, "uid") }},
		{name: "mismatched UID claim", mutate: func(subject *core.Subject) { subject.Claims["uid"] = "999" }},
		{name: "wrong principal ID", mutate: func(subject *core.Subject) { subject.PrincipalID = "other" }},
		{name: "expired authentication", mutate: func(subject *core.Subject) { subject.ExpiresAt = service.Now() }},
		{name: "future authentication", mutate: func(subject *core.Subject) { subject.AuthenticatedAt = service.Now().Add(time.Second) }},
		{name: "stale authentication", mutate: func(subject *core.Subject) {
			subject.AuthenticatedAt = service.Now().Add(-service.Config.Principal.AuthTTL - time.Second)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			subject := valid
			subject.Claims = map[string]string{"uid": valid.Claims["uid"]}
			test.mutate(&subject)
			if _, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
				t.Fatalf("prepare error=%v", err)
			}
			if _, _, err := service.ConfirmLocalHermesAgentImportAs(context.Background(), subject, "sha256:denied"); !errors.Is(err, ErrDenied) {
				t.Fatalf("confirm error=%v", err)
			}
			agents, err := service.Store.ListAgents()
			if err != nil || len(agents) != 0 {
				t.Fatalf("denial mutated charters: agents=%v err=%v", agents, err)
			}
		})
	}
}

func TestRequireSingleLocalHermesImportCandidateFailsClosed(t *testing.T) {
	candidate := registry.Candidate{AgentID: "agent-local"}
	discoveryFailure := errors.New("injected discovery failure")
	tests := []struct {
		name        string
		candidates  []registry.Candidate
		discoverErr error
		wantErr     error
	}{
		{name: "discovery error", candidates: []registry.Candidate{candidate}, discoverErr: discoveryFailure, wantErr: discoveryFailure},
		{name: "zero candidates", candidates: nil},
		{name: "multiple candidates", candidates: []registry.Candidate{candidate, candidate}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := requireSingleLocalHermesImportCandidate(test.candidates, test.discoverErr)
			if err == nil {
				t.Fatal("unexpected candidate cardinality or discovery error was accepted")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("error=%v, want wrapped %v", err, test.wantErr)
			}
		})
	}
	selected, err := requireSingleLocalHermesImportCandidate([]registry.Candidate{candidate}, nil)
	if err != nil || selected.AgentID != candidate.AgentID {
		t.Fatalf("exact candidate rejected: candidate=%+v err=%v", selected, err)
	}
}

func TestLocalHermesAgentImportRejectsConflictingOrphanCharter(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	subject := localImportPrincipal(service)
	proposal, charter, _, err := service.localHermesImportArtifacts(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}

	conflicting := charter.Charter
	conflicting.Name = "conflicting local Agent identity"
	conflictingCharter, err := core.Canonicalize(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if conflictingCharter.Digest == charter.Digest {
		t.Fatal("conflicting charter did not change the digest")
	}
	if err = service.Store.SaveCharter(conflictingCharter); err != nil {
		t.Fatal(err)
	}
	base, err := fleetbadger.Open(context.Background(), filepath.Join(service.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	configureLocalImportFleet(t, service, base)

	if _, _, err = service.ConfirmLocalHermesAgentImportAs(context.Background(), subject, proposal.RevisionDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("confirm error=%v, want ErrConflict", err)
	}
	if _, err = service.FleetRepository.GetAgentRegistration(context.Background(), proposal.AgentID); !IsFleetNotFound(err) {
		t.Fatalf("conflicting orphan charter mutated Agent store: %v", err)
	}
}

func TestLocalHermesAgentImportRecoversExactOrphanCharterAfterRegistrationFailure(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	subject := localImportPrincipal(service)
	proposal, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}

	base, err := fleetbadger.Open(context.Background(), filepath.Join(service.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	repository := &failOnceAgentRegistrationRepository{Repository: base, fail: true}
	configureLocalImportFleet(t, service, repository)

	if _, _, err = service.ConfirmLocalHermesAgentImportAs(context.Background(), subject, proposal.RevisionDigest); err == nil {
		t.Fatal("injected Agent registration failure was not returned")
	}
	stored, err := service.Store.GetCharter(proposal.AgentID, proposal.Revision)
	if err != nil || stored.Digest != proposal.CharterDigest {
		t.Fatalf("exact orphan charter missing: charter=%+v err=%v", stored, err)
	}
	if _, err = repository.GetAgentRegistration(context.Background(), proposal.AgentID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("failed registration unexpectedly mutated Agent store: %v", err)
	}

	agent, created, err := service.ConfirmLocalHermesAgentImportAs(context.Background(), subject, proposal.RevisionDigest)
	if err != nil || !created || agent.Revision.Digest != proposal.RevisionDigest {
		t.Fatalf("exact orphan recovery: created=%t agent=%+v err=%v", created, agent, err)
	}
	again, created, err := service.ConfirmLocalHermesAgentImportAs(context.Background(), subject, proposal.RevisionDigest)
	if err != nil || created || again.Revision.Digest != proposal.RevisionDigest {
		t.Fatalf("recovery replay: created=%t agent=%+v err=%v", created, again, err)
	}
}
