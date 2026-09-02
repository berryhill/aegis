package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"golang.org/x/sys/unix"
)

const maximumLocalHermesMarkerBytes = 1 << 20

type localHermesProfileEvidence struct {
	Fingerprint string
}

type LocalHermesAgentImportProposal struct {
	AgentRegistrationProposal
	SelectedProfile    string `json:"selected_profile"`
	ProfileFingerprint string `json:"profile_fingerprint"`
	Confirmation       string `json:"confirmation"`
}

func localHermesDefaultHome(principalUser, principalUID string) (string, error) {
	account, err := user.Lookup(principalUser)
	if err != nil {
		return "", fmt.Errorf("resolve configured principal account: %w", err)
	}
	if account.Uid != principalUID || strconv.Itoa(os.Geteuid()) != principalUID || !filepath.IsAbs(account.HomeDir) {
		return "", errors.New("configured principal does not match the process-owned absolute home")
	}
	return filepath.Join(account.HomeDir, ".hermes"), nil
}

func inspectLocalHermesDefaultProfile(root, principalUID string) (localHermesProfileEvidence, error) {
	uid, err := strconv.ParseUint(principalUID, 10, 32)
	if err != nil {
		return localHermesProfileEvidence{}, errors.New("configured principal UID is invalid")
	}
	directoryFD, err := unix.Open(root, localHermesDirectoryOpenFlags(), 0)
	if err != nil {
		return localHermesProfileEvidence{}, fmt.Errorf("open default Hermes profile: %w", err)
	}
	directory := os.NewFile(uintptr(directoryFD), root)
	defer directory.Close()
	if err = verifyLocalHermesFile(directory, uint32(uid), true); err != nil {
		return localHermesProfileEvidence{}, fmt.Errorf("verify default Hermes profile: %w", err)
	}
	markerFD, err := unix.Openat(directoryFD, "config.yaml", localHermesMarkerOpenFlags(), 0)
	if err != nil {
		return localHermesProfileEvidence{}, fmt.Errorf("open default Hermes profile marker: %w", err)
	}
	marker := os.NewFile(uintptr(markerFD), "config.yaml")
	defer marker.Close()
	if err = verifyLocalHermesFile(marker, uint32(uid), false); err != nil {
		return localHermesProfileEvidence{}, fmt.Errorf("verify default Hermes profile marker: %w", err)
	}
	info, err := marker.Stat()
	if err != nil {
		return localHermesProfileEvidence{}, fmt.Errorf("stat default Hermes profile marker: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Size() <= 0 || info.Size() > maximumLocalHermesMarkerBytes {
		return localHermesProfileEvidence{}, errors.New("default Hermes profile marker is empty or unsupported")
	}
	// The profile marker may contain credentials or other private runtime
	// configuration. Import must prove only stable local provenance, so the
	// fingerprint is derived from bounded filesystem metadata and never from
	// marker bytes. Same-owner in-place content mutation is outside the
	// application trust boundary and does not grant identity or authority.
	metadata := fmt.Sprintf("aegis.local-hermes-profile.v2\x00config.yaml\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d", stat.Dev, stat.Ino, stat.Uid, stat.Gid, stat.Mode, info.Size())
	digest := sha256.Sum256([]byte(metadata))
	return localHermesProfileEvidence{Fingerprint: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

func verifyLocalHermesFile(file *os.File, expectedUID uint32, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if directory != info.IsDir() || (!directory && !info.Mode().IsRegular()) {
		return errors.New("unexpected file type")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != expectedUID {
		return errors.New("owner does not match authenticated principal")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return errors.New("group- or world-writable profile evidence")
	}
	return nil
}

func (s *Service) requireLocalHermesImportPrincipal(subject core.Subject) error {
	now := s.Now()
	expectedUID := s.Config.Principal.UID
	if subject.PrincipalID != s.Config.Principal.ID || subject.PrincipalID == "" ||
		subject.Issuer != "linux-so-peercred" || subject.Method != "local-os" ||
		subject.ID != "local-uid:"+expectedUID || subject.Claims["uid"] != expectedUID ||
		subject.AuthenticatedAt.IsZero() || subject.AuthenticatedAt.After(now) ||
		now.Sub(subject.AuthenticatedAt) > s.Config.Principal.AuthTTL || !now.Before(subject.ExpiresAt) {
		return fmt.Errorf("%w: local Hermes import requires fresh configured Unix-peer principal authentication", ErrDenied)
	}
	return nil
}

func requireSingleLocalHermesImportCandidate(candidates []registry.Candidate, discoverErr error) (registry.Candidate, error) {
	if discoverErr != nil {
		return registry.Candidate{}, fmt.Errorf("discover local Hermes import candidate: %w", discoverErr)
	}
	if len(candidates) != 1 {
		return registry.Candidate{}, errors.New("local Hermes import discovery must return exactly one candidate")
	}
	return candidates[0], nil
}

func (s *Service) localHermesImportArtifacts(ctx context.Context, subject core.Subject) (LocalHermesAgentImportProposal, core.CanonicalCharter, []byte, error) {
	if err := s.requireLocalHermesImportPrincipal(subject); err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	home, err := s.LocalHermesHome(s.Config.Principal.User, s.Config.Principal.UID)
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	evidence, err := inspectLocalHermesDefaultProfile(home, s.Config.Principal.UID)
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	principalHash := sha256.Sum256([]byte(subject.PrincipalID))
	agentID := "hermes-default-" + hex.EncodeToString(principalHash[:6])
	charter, err := core.Canonicalize(core.Charter{SchemaVersion: core.SchemaVersion, AgentID: agentID, Name: "Imported local Hermes default profile (" + evidence.Fingerprint + ")", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: core.HermesVersionConstraint, Target: "aegis-owned-ephemeral"}, Stanzas: []core.TrustStanza{{ID: "principal", Name: "Authenticated principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 300}, Session: core.SessionPolicy{MaximumLifetimeSec: 300, IdleTimeoutSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "none", Provider: "none"}}}, CreatedBy: subject.PrincipalID, CreatedAt: time.Unix(1, 0).UTC()})
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	fleetID := "local-principal-" + hex.EncodeToString(principalHash[:6])
	sourceID := "hermes-default-profile"
	fixture := registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: fleetID, Agents: []registry.CurrentFleetAgent{{SourceID: sourceID, AgentID: agentID, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: subject.PrincipalID}, Lifecycle: registry.LifecycleDisabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agentID, Revision: 1, Digest: charter.Digest}}}}
	fixtureData, err := json.Marshal(fixture)
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	source, err := registry.NewCurrentFleetFixtureSource(fixtureData)
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	candidates, discoverErr := source.Discover(ctx)
	candidate, err := requireSingleLocalHermesImportCandidate(candidates, discoverErr)
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	revision, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: agentID, Revision: 1, Source: candidate.Source, Runtime: candidate.Runtime, Ownership: candidate.Ownership, Lifecycle: candidate.Lifecycle, Charter: candidate.Charter})
	if err != nil {
		return LocalHermesAgentImportProposal{}, core.CanonicalCharter{}, nil, err
	}
	base := AgentRegistrationProposal{AgentID: agentID, CharterDigest: charter.Digest, RevisionDigest: revision.Digest, Revision: 1, FleetID: fleetID, SourceID: sourceID, Runtime: "hermes / hermes-agent / aegis-owned-ephemeral", Owner: subject.PrincipalID, Accountability: subject.PrincipalID, Capabilities: "None declared", Policies: "None declared", Lifecycle: string(registry.LifecycleDisabled)}
	proposal := LocalHermesAgentImportProposal{AgentRegistrationProposal: base, SelectedProfile: "profile/default", ProfileFingerprint: evidence.Fingerprint, Confirmation: "/agents import hermes default confirm " + revision.Digest}
	return proposal, charter, fixtureData, nil
}

func (s *Service) PrepareLocalHermesAgentImportAs(ctx context.Context, subject core.Subject) (LocalHermesAgentImportProposal, error) {
	proposal, _, _, err := s.localHermesImportArtifacts(ctx, subject)
	return proposal, err
}

func (s *Service) ConfirmLocalHermesAgentImportAs(ctx context.Context, subject core.Subject, expectedDigest string) (FleetAgent, bool, error) {
	if err := s.requireLocalHermesImportPrincipal(subject); err != nil {
		return FleetAgent{}, false, err
	}
	if err := s.requireFleetPrincipal(subject); err != nil {
		return FleetAgent{}, false, err
	}
	proposal, charter, fixture, err := s.localHermesImportArtifacts(ctx, subject)
	if err != nil {
		return FleetAgent{}, false, err
	}
	if expectedDigest == "" || expectedDigest != proposal.RevisionDigest {
		return FleetAgent{}, false, ErrConflict
	}
	identity := registry.FleetSource{FleetID: proposal.FleetID, Kind: registry.CurrentFleetSourceKind, SourceID: proposal.SourceID}
	if existing, lookupErr := s.FleetRepository.GetAgentRegistrationBySource(ctx, identity); lookupErr == nil {
		if existing.AgentID != proposal.AgentID || existing.InitialRevision.Digest != expectedDigest {
			return FleetAgent{}, false, ErrConflict
		}
	} else if !IsFleetNotFound(lookupErr) {
		return FleetAgent{}, false, lookupErr
	}
	if existing, lookupErr := s.FleetRepository.GetAgentRegistration(ctx, proposal.AgentID); lookupErr == nil {
		if existing.Source != identity || existing.InitialRevision.Digest != expectedDigest {
			return FleetAgent{}, false, ErrConflict
		}
	} else if !IsFleetNotFound(lookupErr) {
		return FleetAgent{}, false, lookupErr
	}
	// Charter and fleet definitions live in separate qualified stores. The only
	// recoverable partial state is this exact immutable charter with no Agent
	// registration: a retry reuses it and attempts the same digest-bound Agent
	// registration. Any conflicting charter or registration was denied above.
	storedCharter, loadErr := s.Store.GetCharter(charter.Charter.AgentID, charter.Charter.Revision)
	if loadErr == nil {
		if storedCharter.Digest != charter.Digest {
			return FleetAgent{}, false, ErrConflict
		}
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return FleetAgent{}, false, loadErr
	} else if saveErr := s.Store.SaveCharter(charter); saveErr != nil {
		// SaveCharter is immutable rather than idempotent. A concurrent exact
		// confirmation may have won the write; accept only exact readback.
		storedCharter, loadErr = s.Store.GetCharter(charter.Charter.AgentID, charter.Charter.Revision)
		if loadErr != nil || storedCharter.Digest != charter.Digest {
			return FleetAgent{}, false, saveErr
		}
	}
	agent, created, err := s.RegisterFleetAgentAs(ctx, subject, NewRegisterFleetAgentInput(fixture, proposal.FleetID, proposal.SourceID))
	if err != nil {
		return FleetAgent{}, false, err
	}
	readback, err := s.GetFleetAgentAs(ctx, subject, proposal.AgentID, proposal.Revision)
	if err != nil || readback.Revision.Digest != expectedDigest || agent.Revision.Digest != expectedDigest {
		return FleetAgent{}, false, errors.New("local Hermes Agent import exact readback mismatch")
	}
	return readback, created, nil
}
