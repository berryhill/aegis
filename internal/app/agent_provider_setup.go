package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// AgentProviderSetupInput is a closed, file-free composition of existing services.
// Neither source selection nor a registration digest approves a charter successor.
type AgentProviderSetupInput struct {
	Action        string `json:"action"`
	ProfileHome   string `json:"profile_home,omitempty"`
	CredentialID  string `json:"credential_id,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	ReviewID      string `json:"review_id,omitempty"`
	CharterDigest string `json:"charter_digest,omitempty"`
}

type AgentProviderSetupResult struct {
	Agent         FleetAgent                    `json:"agent"`
	Models        core.HermesConfig             `json:"models"`
	Connection    *ControllerCodexConnection    `json:"connection,omitempty"`
	Candidates    *hermes.NativeCodexCandidates `json:"candidates,omitempty"`
	Review        *AgentModelReview             `json:"review,omitempty"`
	Qualification string                        `json:"qualification"`
	Executable    bool                          `json:"executable"`
	Next          string                        `json:"next"`
	NextCLI       string                        `json:"next_cli,omitempty"`
	Message       string                        `json:"message"`
	Reason        string                        `json:"reason,omitempty"`
	Choices       []AgentProviderChoice         `json:"choices,omitempty"`
}

type AgentModelReview struct {
	ID                 string                     `json:"id"`
	Expected           reference.RevisionRef      `json:"expected"`
	Charter            core.CanonicalCharter      `json:"charter"`
	ExpiresAt          time.Time                  `json:"expires_at"`
	Confirmation       string                     `json:"confirmation"`
	Connection         *ControllerCodexConnection `json:"connection,omitempty"`
	ProfileFingerprint string                     `json:"profile_fingerprint,omitempty"`
}

type agentModelReview struct {
	Subject         core.Subject
	Public          AgentModelReview
	Fingerprint     string
	Connection      *ControllerCodexConnection
	Config          []byte
	Applied         *FleetAgent
	CharterAdmitted bool // only this review may recover its admitted charter-first write
}

type agentProviderConnectionReview struct {
	Expected      reference.RevisionRef
	ExpiresAt     time.Time
	GuidedDefault bool // prepare, never approve, the already-imported default lane
}

func (in *AgentProviderSetupInput) UnmarshalJSON(data []byte) error {
	if err := reference.RejectDuplicateObjectKeys(data); err != nil {
		return ErrConflict
	}
	type wire AgentProviderSetupInput
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return ErrConflict
	}
	*in = AgentProviderSetupInput(value)
	return nil
}

func (in AgentProviderSetupInput) valid() bool {
	expected := AgentProviderSetupInput{Action: in.Action}
	switch in.Action {
	case "status", "models":
	case "setup":
		if in.ProfileHome != "default" || (in.CredentialID != "" && !hermes.ValidNativeCredentialID(in.CredentialID)) {
			return false
		}
		expected.ProfileHome, expected.CredentialID = in.ProfileHome, in.CredentialID
	case "candidates", "review":
		if in.ProfileHome == "" {
			return false
		}
		expected.ProfileHome = in.ProfileHome
		if in.Action == "review" {
			expected.CredentialID = in.CredentialID
		}
	case "connect":
		if in.ReviewID == "" {
			return false
		}
		expected.ReviewID = in.ReviewID
	case "promote":
		if (in.Provider != "codex" && in.Provider != "openai-codex") || !validLocalHermesModel(in.Provider, in.Model) {
			return false
		}
		expected.Provider, expected.Model = in.Provider, in.Model
	case "approve":
		if in.ReviewID == "" || in.CharterDigest == "" {
			return false
		}
		expected.ReviewID, expected.CharterDigest = in.ReviewID, in.CharterDigest
	default:
		return false
	}
	return in == expected
}

func (s *Service) importedProviderAgent(ctx context.Context, sub core.Subject, id string) (FleetAgent, core.CanonicalCharter, error) {
	if sub.Kind != "human" || id != LocalHermesDefaultAgentID(sub.PrincipalID) {
		return FleetAgent{}, core.CanonicalCharter{}, ErrDenied
	}
	agent, err := s.verifyLocalHermesAgentImportAs(ctx, sub, localHermesImportUnixPeer)
	if err != nil {
		return FleetAgent{}, core.CanonicalCharter{}, err
	}
	if agent.Revision.Lifecycle == registry.LifecycleRetired {
		return FleetAgent{}, core.CanonicalCharter{}, ErrDenied
	}
	c, err := s.Store.GetCharter(id, agent.Revision.Charter.Revision)
	if err != nil || c.Digest != agent.Revision.Charter.Digest || len(c.Charter.Stanzas) != 1 {
		return FleetAgent{}, core.CanonicalCharter{}, ErrConflict
	}
	if len(c.Charter.Stanzas[0].Scopes.Credentials) != 0 {
		return FleetAgent{}, core.CanonicalCharter{}, ErrDenied
	}
	return agent, c, nil
}

// AgentProviderSetupAs never derives local-os authority from a manager identity.
// The original authenticated subject must satisfy each owning service's admission.
// It performs no inference, runtime qualification override, profile mutation or
// credential administration. Promotion changes exactly one explicit Codex lane.
func (s *Service) AgentProviderSetupAs(ctx context.Context, sub core.Subject, id string, in AgentProviderSetupInput) (result AgentProviderSetupResult, resultErr error) {
	defer func() {
		if resultErr == nil {
			renderAgentProviderSetup(&result, s.ConfigFile, s.Config.API.Console.Origin)
		}
	}()
	s.agentModelMu.Lock()
	defer s.agentModelMu.Unlock()
	if !in.valid() {
		return AgentProviderSetupResult{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return AgentProviderSetupResult{}, err
	}
	agent, canonical, err := s.importedProviderAgent(ctx, sub, id)
	if err != nil {
		return AgentProviderSetupResult{}, err
	}
	out := AgentProviderSetupResult{Agent: agent, Models: canonical.Charter.Stanzas[0].Hermes, Qualification: "not_checked"}
	setAgentProviderNext(&out)
	guided := in.Action == "setup"
	if guided {
		// Only the immutable, explicitly approved default is selected. Unsupported
		// defaults never fall back to a different provider or billing account.
		if out.Models.Provider == "none" || out.Models.Provider == "antigravity-oauth" {
			return out, nil
		}
		if out.Models.ProviderAuthentication == nil || out.Models.ProviderAuthentication.Mode != core.ConfiguredProviderV1 {
			return out, nil
		}
	}
	// "default" is an explicit source selection, never an implicit fallback.
	if in.ProfileHome == "default" {
		home, err := s.LocalHermesHome(s.Config.Principal.User, s.Config.Principal.UID)
		if err != nil {
			return out, ErrDenied
		}
		in.ProfileHome = home
	}
	if guided {
		connection, err := s.ReadControllerCodexAs(ctx, sub)
		if err != nil {
			return out, err
		}
		if connection.Status == "connected" {
			// Existing consent cannot be retargeted by this shortcut.
			if connection.ProfileHome != in.ProfileHome || (in.CredentialID != "" && connection.CredentialID != in.CredentialID) {
				return out, ErrConflict
			}
			out.Connection = &connection
			in.Action, in.Provider, in.Model = "promote", out.Models.Provider, out.Models.Model
		} else {
			if in.CredentialID == "" {
				candidates, err := s.InspectControllerCodexCandidatesAs(ctx, sub, ControllerCodexCandidatesInput{ProfileHome: in.ProfileHome})
				if err != nil {
					return out, err
				}
				out.Candidates = &candidates
				if candidates.DistinctEligibleGrants != 1 {
					out.Next = ""
					out.Reason = "explicit_account_selection_required"
					if candidates.DistinctEligibleGrants == 0 {
						out.Reason = "no_eligible_native_grant"
					}
					for _, candidate := range candidates.Candidates {
						if candidate.Eligible && hermes.ValidNativeCredentialID(candidate.CredentialID) {
							command := "/agents providers " + id + " setup default " + candidate.CredentialID
							out.Choices = append(out.Choices, AgentProviderChoice{CredentialID: candidate.CredentialID, AccountFingerprint: candidate.AccountFingerprint, Command: command, CLI: providerCLI(command)})
						}
					}
					return out, nil
				}
			}
			in.Action = "review"
		}
	}
	switch in.Action {
	case "status":
		connection, err := s.ReadControllerCodexAs(ctx, sub)
		if err != nil {
			return out, err
		}
		out.Connection = &connection
		if out.Models.Provider == "none" {
			out.Next = "/agents providers " + id + " models"
		}
		// Observation never probes a login or claims connected == executable.
		return out, nil
	case "candidates":
		candidates, err := s.InspectControllerCodexCandidatesAs(ctx, sub, ControllerCodexCandidatesInput{ProfileHome: in.ProfileHome})
		if err != nil {
			return out, err
		}
		out.Candidates = &candidates
		out.Next = "/agents providers " + id + " review PROFILE_HOME [CREDENTIAL_ID]"
		return out, nil
	case "review":
		for key, previous := range s.agentProviderReviews {
			if !s.Now().Before(previous.ExpiresAt) {
				delete(s.agentProviderReviews, key)
			}
		}
		if len(s.agentProviderReviews) >= 64 {
			return out, ErrConflict
		}
		connection, err := s.PreviewControllerCodexAs(ctx, sub, ControllerCodexPreviewInput{ProfileHome: in.ProfileHome, CredentialID: in.CredentialID})
		if err != nil {
			return out, err
		}
		out.Connection = &connection
		out.Next = "/agents providers " + id + " connect " + connection.ReviewID
		if s.agentProviderReviews == nil {
			s.agentProviderReviews = map[string]agentProviderConnectionReview{}
		}
		for key, previous := range s.agentProviderReviews {
			if !s.Now().Before(previous.ExpiresAt) {
				delete(s.agentProviderReviews, key)
			}
		}
		s.agentProviderReviews[connection.ReviewID] = agentProviderConnectionReview{Expected: agentRevisionRef(agent.Revision), ExpiresAt: connection.ExpiresAt, GuidedDefault: guided}
		s.nativeConnectionMu.Lock()
		native := s.nativeReviews[connection.ReviewID]
		expected := agentRevisionRef(agent.Revision)
		native.ExpectedAgent = &expected
		s.nativeReviews[connection.ReviewID] = native
		s.nativeConnectionMu.Unlock()
		return out, nil
	case "connect":
		review, ok := s.agentProviderReviews[in.ReviewID]
		if !ok || review.Expected != agentRevisionRef(agent.Revision) || !s.Now().Before(review.ExpiresAt) {
			return out, ErrConflict
		}
		connection, err := s.ApplyControllerCodexAs(ctx, sub, ControllerCodexApplyInput{ReviewID: in.ReviewID})
		if err != nil {
			return out, err
		}
		out.Connection = &connection
		delete(s.agentProviderReviews, in.ReviewID)
		if !review.GuidedDefault && (out.Models.Provider == "codex" || out.Models.Provider == "openai-codex") {
			out.Next = "/agents providers " + id + " promote " + out.Models.Provider + " " + out.Models.Model
		}
		if review.GuidedDefault {
			modelReview, err := s.prepareAgentModelReview(ctx, sub, agent, canonical, AgentProviderSetupInput{Action: "promote", Provider: out.Models.Provider, Model: out.Models.Model})
			if err != nil {
				// Connection has its own verified effect. A failed non-mutating
				// continuation must not hide it or suggest replaying consumed consent.
				out.Reason = "connected_charter_review_unavailable"
				return out, nil
			}
			out.Review, out.Next = &modelReview, modelReview.Confirmation
		}
		return out, nil
	case "models", "promote":
		review, err := s.prepareAgentModelReview(ctx, sub, agent, canonical, in)
		if err != nil {
			return out, err
		}
		out.Review, out.Next = &review, review.Confirmation
		return out, nil
	case "approve":
		applied, err := s.approveAgentModelReview(ctx, sub, id, in)
		if err != nil {
			return out, err
		}
		c, err := s.Store.GetCharter(id, applied.Revision.Charter.Revision)
		if err != nil || c.Digest != applied.Revision.Charter.Digest {
			return out, ErrConflict
		}
		out.Agent, out.Models = applied, c.Charter.Stanzas[0].Hermes
		setAgentProviderNext(&out)
		return out, nil
	}
	return out, ErrDenied
}

func (s *Service) prepareAgentModelReview(ctx context.Context, sub core.Subject, agent FleetAgent, c core.CanonicalCharter, in AgentProviderSetupInput) (AgentModelReview, error) {
	s.nativeConnectionMu.Lock()
	defer s.nativeConnectionMu.Unlock()
	// Decode canonical bytes to detach the proposal from all caller-owned slices.
	var next core.Charter
	if json.Unmarshal(c.Canonical, &next) != nil {
		return AgentModelReview{}, ErrConflict
	}
	next.Revision++
	r := agentModelReview{Subject: cloneConnectionSubject(sub)}
	if in.Action == "models" {
		// Explicit migration of the historical metadata-only import, not a second
		// registration and not a rewrite of an arbitrary later charter.
		if c.Charter.Revision != 1 || next.Stanzas[0].Hermes.Provider != "none" || next.Stanzas[0].Hermes.Model != "none" {
			return AgentModelReview{}, ErrConflict
		}
		home, err := s.LocalHermesHome(s.Config.Principal.User, s.Config.Principal.UID)
		if err != nil {
			return AgentModelReview{}, ErrDenied
		}
		evidence, h, err := inspectLocalHermesModels(home, s.Config.Principal.UID)
		if err != nil {
			return AgentModelReview{}, err
		}
		next.Stanzas[0].Hermes = h
		r.Fingerprint = evidence.Fingerprint
	} else {
		h := &next.Stanzas[0].Hermes
		lane, err := h.ResolveAuthorizedLane(in.Provider, in.Model)
		if err != nil || lane.ProviderAuthentication == nil || lane.ProviderAuthentication.Mode != core.ConfiguredProviderV1 {
			return AgentModelReview{}, ErrDenied
		}
		connection, snapshot, err := s.agentModelConnection(ctx, sub)
		if err != nil {
			return AgentModelReview{}, err
		}
		r.Connection, r.Config = &connection, snapshot
		for i := range h.AllowedLanes {
			if h.AllowedLanes[i].Provider == in.Provider && h.AllowedLanes[i].Model == in.Model {
				h.AllowedLanes[i].ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
			}
		}
		if h.Provider == in.Provider && h.Model == in.Model {
			h.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
		}
	}
	canonical, err := core.Canonicalize(next)
	if err != nil {
		return AgentModelReview{}, ErrConflict
	}
	now := s.Now().UTC()
	if s.agentModelReviews == nil {
		s.agentModelReviews = map[string]agentModelReview{}
	}
	for id, old := range s.agentModelReviews {
		if !now.Before(old.Public.ExpiresAt) {
			delete(s.agentModelReviews, id)
		}
	}
	if len(s.agentModelReviews) >= 64 {
		return AgentModelReview{}, ErrConflict
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return AgentModelReview{}, ErrDenied
	}
	id := hex.EncodeToString(entropy[:])
	r.Public = AgentModelReview{ID: id, Expected: agentRevisionRef(agent.Revision), Charter: canonical, ExpiresAt: now.Add(5 * time.Minute), Confirmation: "/agents providers " + agent.Revision.AgentID + " approve " + id + " " + canonical.Digest}
	r.Public.Connection, r.Public.ProfileFingerprint = r.Connection, r.Fingerprint
	s.agentModelReviews[id] = r
	var public AgentModelReview
	encoded, err := json.Marshal(r.Public)
	if err != nil || json.Unmarshal(encoded, &public) != nil {
		return AgentModelReview{}, ErrConflict
	}
	return public, nil
}

func cloneConnectionSubject(sub core.Subject) core.Subject {
	out := connectionIdentity(sub)
	out.Claims = make(map[string]string, len(sub.Claims))
	for k, v := range sub.Claims {
		out.Claims[k] = v
	}
	return out
}

func (s *Service) agentModelConnection(ctx context.Context, sub core.Subject) (ControllerCodexConnection, []byte, error) {
	connection, err := s.ReadControllerCodexAs(ctx, sub)
	if err != nil {
		return connection, nil, err
	}
	if connection.Status != "connected" || connection.PrincipalID != sub.PrincipalID || s.nativeConnectionAdmission(sub, connection.ProfileHome) != nil {
		return connection, nil, ErrDenied
	}
	snapshot, err := config.ConnectionSnapshot(s.ConfigFile)
	if err != nil {
		return connection, nil, ErrConflict
	}
	metadata, err := s.controllerCodexSource().PreviewNativeCodexLogin(ctx, connection.ProfileHome, s.Now().UTC(), connection.CredentialID)
	if err != nil {
		return connection, nil, err
	}
	if metadata.AccountID != connection.AccountID {
		return connection, nil, ErrConflict
	}
	return connection, snapshot, nil
}

func (s *Service) approveAgentModelReview(ctx context.Context, sub core.Subject, id string, in AgentProviderSetupInput) (FleetAgent, error) {
	s.nativeConnectionMu.Lock()
	defer s.nativeConnectionMu.Unlock()
	r, ok := s.agentModelReviews[in.ReviewID]
	if !ok || r.Public.Expected.ID != id || r.Public.Charter.Digest != in.CharterDigest || !reflect.DeepEqual(r.Subject, connectionIdentity(sub)) || !s.Now().Before(r.Public.ExpiresAt) {
		return FleetAgent{}, ErrDenied
	}
	// Never trust response pointers as authority: independently canonicalize the
	// held proposal and compare the exact reviewed digest before any write.
	canonical, err := core.Canonicalize(r.Public.Charter.Charter)
	if err != nil || canonical.Digest != in.CharterDigest {
		return FleetAgent{}, ErrConflict
	}
	if r.Applied != nil {
		return s.GetFleetAgentAs(ctx, sub, id, r.Applied.Revision.Revision)
	}
	current, err := s.GetFleetAgentAs(ctx, sub, id, 0)
	if err != nil {
		return FleetAgent{}, err
	}
	expectedCharter := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: canonical.Charter.Revision, Digest: canonical.Digest}
	if agentRevisionRef(current.Revision) != r.Public.Expected {
		// Reconcile a successful append whose response/audit delivery was interrupted.
		if !r.CharterAdmitted || current.Revision.CharterSuccessor == nil || current.Revision.CharterSuccessor.Previous != r.Public.Expected || current.Revision.Charter != expectedCharter || current.Revision.CharterSuccessor.ApprovedBy != sub.PrincipalID {
			return FleetAgent{}, ErrConflict
		}
		r.Applied = &current
		s.agentModelReviews[in.ReviewID] = r
		return current, nil
	}
	if r.Fingerprint != "" {
		home, err := s.LocalHermesHome(s.Config.Principal.User, s.Config.Principal.UID)
		if err != nil {
			return FleetAgent{}, ErrDenied
		}
		evidence, _, err := inspectLocalHermesModels(home, s.Config.Principal.UID)
		if err != nil {
			return FleetAgent{}, err
		}
		if evidence.Fingerprint != r.Fingerprint {
			return FleetAgent{}, ErrConflict
		}
	}
	if r.Connection != nil {
		connection, snapshot, err := s.agentModelConnection(ctx, sub)
		if err != nil {
			return FleetAgent{}, err
		}
		if connection != *r.Connection || !bytes.Equal(snapshot, r.Config) {
			return FleetAgent{}, ErrConflict
		}
	}
	// Source validation can run arbitrary I/O; only fence Agent mutations after
	// it returns. Never acquire this lock from a source-validation callback.
	s.agentRevisionMu.Lock()
	defer s.agentRevisionMu.Unlock()
	current, err = s.GetFleetAgentAs(ctx, sub, id, 0)
	if err != nil {
		return FleetAgent{}, err
	}
	if agentRevisionRef(current.Revision) != r.Public.Expected {
		return FleetAgent{}, ErrConflict
	}
	// No store reads in this callback: it also runs under the fleet transaction.
	// The Agent effect fence above pins the independently checked exact revision.
	admit := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.requireLocalHermesImportPrincipal(sub, localHermesImportUnixPeer) != nil || s.requireFleetPrincipal(sub) != nil || !s.Now().Before(r.Public.ExpiresAt) {
			return ErrDenied
		}
		return ctx.Err()
	}
	if err = admit(); err != nil {
		return FleetAgent{}, err
	}
	existing, err := s.Store.GetCharter(id, canonical.Charter.Revision)
	if err == nil {
		if !r.CharterAdmitted || existing.Digest != canonical.Digest {
			return FleetAgent{}, ErrConflict
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return FleetAgent{}, err
	} else if err := s.Store.SaveCharterAdmitted(canonical, func() error {
		if err := admit(); err != nil {
			return err
		}
		r.CharterAdmitted = true
		s.agentModelReviews[in.ReviewID] = r
		return nil
	}); err != nil {
		existing, loadErr := s.Store.GetCharter(id, canonical.Charter.Revision)
		if !r.CharterAdmitted || loadErr != nil || existing.Digest != canonical.Digest {
			return FleetAgent{}, ErrConflict
		}
	}
	applied, err := s.approveAgentCharterLocked(ctx, sub, id, ApproveAgentCharterInput{Expected: r.Public.Expected, Charter: expectedCharter}, admit)
	if err != nil {
		return FleetAgent{}, err
	}
	if applied.Revision.Charter != expectedCharter {
		return FleetAgent{}, ErrConflict
	}
	r.Applied = &applied
	s.agentModelReviews[in.ReviewID] = r
	return applied, nil
}
