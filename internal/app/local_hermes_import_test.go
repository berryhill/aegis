package app

import (
	"context"
	"encoding/json"
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

func localBootstrapImportPrincipal(service *Service) core.Subject {
	now := service.Now()
	return core.Subject{ID: "local-uid:" + service.Config.Principal.UID, Kind: "human", PrincipalID: service.Config.Principal.ID, Issuer: "local-os", Method: "local-os", AuthenticatedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Claims: map[string]string{"uid": service.Config.Principal.UID, "user": service.Config.Principal.User}}
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

func TestLocalHermesAgentImportBootstrapAcceptsFreshConfiguredLocalProcessWithoutWeakeningPeerRoute(t *testing.T) {
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
	now := service.Now()
	subject := core.Subject{
		ID: "local-uid:" + service.Config.Principal.UID, Kind: "human", PrincipalID: service.Config.Principal.ID,
		Issuer: "local-os", Method: "local-os", AuthenticatedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute),
		Claims: map[string]string{"uid": service.Config.Principal.UID, "user": service.Config.Principal.User},
	}
	if _, err := service.PrepareLocalHermesAgentImportAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("peer-only route accepted direct local process: %v", err)
	}
	proposal, err := service.PrepareLocalHermesAgentImportForBootstrapAs(context.Background(), subject)
	if err != nil || proposal.SelectedProfile != "profile/default" || proposal.RevisionDigest == "" {
		t.Fatalf("bootstrap local-process route failed: proposal=%+v err=%v", proposal, err)
	}
	for _, mutate := range []func(*core.Subject){
		func(candidate *core.Subject) { candidate.Issuer = "prompt" },
		func(candidate *core.Subject) { delete(candidate.Claims, "user") },
		func(candidate *core.Subject) { candidate.Claims["user"] = "other" },
		func(candidate *core.Subject) { candidate.ExpiresAt = now },
	} {
		candidate := subject
		candidate.Claims = map[string]string{"uid": subject.Claims["uid"], "user": subject.Claims["user"]}
		mutate(&candidate)
		if _, err := service.PrepareLocalHermesAgentImportForBootstrapAs(context.Background(), candidate); !errors.Is(err, ErrDenied) {
			t.Fatalf("invalid bootstrap subject accepted: %+v err=%v", candidate, err)
		}
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

func TestBootstrapLocalHermesImportRegistersSecondDisabledAgentWithExactReplayAndNoProfileMutation(t *testing.T) {
	service := testService(t)
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "config.yaml")
	markerContents := []byte("version: 1\nsecret: never-read\n")
	if err := os.WriteFile(marker, markerContents, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	service.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	service.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	repository, err := fleetbadger.Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	configureLocalImportFleet(t, service, repository)
	subject := localBootstrapImportPrincipal(service)

	if _, created, err := service.RegisterBuiltInAegisAgentAs(context.Background(), subject); err != nil || !created {
		t.Fatalf("built-in registration: created=%t err=%v", created, err)
	}
	proposal, err := service.PrepareLocalHermesAgentImportForBootstrapAs(context.Background(), subject)
	if err != nil {
		t.Fatal(err)
	}
	imported, created, err := service.ConfirmLocalHermesAgentImportForBootstrapAs(context.Background(), subject, proposal.RevisionDigest)
	if err != nil || !created || imported.Revision.Lifecycle != registry.LifecycleDisabled || len(imported.Revision.CapabilityDeclarations) != 0 || len(imported.Revision.PolicyRefs) != 0 {
		t.Fatalf("bootstrap import: created=%t imported=%+v err=%v", created, imported, err)
	}
	verified, err := service.VerifyLocalHermesAgentImportForBootstrapAs(context.Background(), subject)
	if err != nil || verified.Revision.Digest != imported.Revision.Digest {
		t.Fatalf("durable bootstrap import verification: agent=%+v err=%v", verified, err)
	}
	agents, err := service.ListFleetAgentsAs(context.Background(), subject)
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents after import: count=%d err=%v agents=%+v", len(agents), err, agents)
	}
	again, created, err := service.ConfirmLocalHermesAgentImportForBootstrapAs(context.Background(), subject, proposal.RevisionDigest)
	if err != nil || created || again.Revision.Digest != imported.Revision.Digest {
		t.Fatalf("bootstrap import replay: created=%t agent=%+v err=%v", created, again, err)
	}
	after, err := os.Stat(marker)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(marker)
	if err != nil || string(contents) != string(markerContents) || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() {
		t.Fatalf("profile marker mutated: contents_match=%t same_file=%t before=%+v after=%+v err=%v", string(contents) == string(markerContents), os.SameFile(before, after), before, after, err)
	}
	lifecycleInput, err := NewSetAgentLifecycleInput(imported.Revision.AgentID, imported.Revision.Revision, imported.Revision.Digest, string(registry.LifecycleEnabled))
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := service.SetAgentLifecycleAs(context.Background(), subject, imported.Revision.AgentID, lifecycleInput)
	if err != nil || enabled.Revision.Revision != 2 || enabled.Revision.Lifecycle != registry.LifecycleEnabled {
		t.Fatalf("enable imported Agent: agent=%+v err=%v", enabled, err)
	}
	verified, err = service.VerifyLocalHermesAgentImportForBootstrapAs(context.Background(), subject)
	if err != nil || verified.Revision.Revision != 2 || verified.Revision.Lifecycle != registry.LifecycleEnabled || verified.Revision.Digest != enabled.Revision.Digest {
		t.Fatalf("durable import verification did not report current lifecycle: agent=%+v err=%v", verified, err)
	}
}

func TestBootstrapLocalHermesImportRejectsLookalikeRegistrationWithoutCanonicalCharter(t *testing.T) {
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
	repository, err := fleetbadger.Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	configureLocalImportFleet(t, service, repository)
	subject := localBootstrapImportPrincipal(service)

	proposal, charter, fixtureData, err := service.localHermesImportArtifacts(context.Background(), subject, localHermesImportBootstrapCLI)
	if err != nil {
		t.Fatal(err)
	}
	lookalike := charter.Charter
	lookalike.Name = "structurally similar but not a canonical local profile import"
	lookalikeCharter, err := core.Canonicalize(lookalike)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Store.SaveCharter(lookalikeCharter); err != nil {
		t.Fatal(err)
	}
	var fixture registry.CurrentFleetFixture
	if err := json.Unmarshal(fixtureData, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Agents[0].Charter.Digest = lookalikeCharter.Digest
	lookalikeFixture, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	registered, created, err := service.RegisterFleetAgentAs(context.Background(), subject, NewRegisterFleetAgentInput(lookalikeFixture, proposal.FleetID, proposal.SourceID))
	if err != nil || !created || registered.Revision.Charter.Digest != lookalikeCharter.Digest {
		t.Fatalf("register lookalike: created=%t agent=%+v err=%v", created, registered, err)
	}
	if _, err := service.VerifyLocalHermesAgentImportForBootstrapAs(context.Background(), subject); !errors.Is(err, ErrConflict) {
		t.Fatalf("lookalike verification error=%v, want ErrConflict", err)
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
	proposal, charter, _, err := service.localHermesImportArtifacts(context.Background(), subject, localHermesImportUnixPeer)
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
