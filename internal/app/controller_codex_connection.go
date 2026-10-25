package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

type ControllerCodexCandidatesInput struct {
	ProfileHome string `json:"profile_home"`
}

// Inspection is non-mutating and grants no review, approval or binding.
func (s *Service) InspectControllerCodexCandidatesAs(ctx context.Context, sub core.Subject, in ControllerCodexCandidatesInput) (hermes.NativeCodexCandidates, error) {
	if s.nativeConnectionAdmission(sub, in.ProfileHome) != nil {
		return hermes.NativeCodexCandidates{}, ErrDenied
	}
	return s.Hermes.InspectNativeCodexCandidates(ctx, in.ProfileHome, s.Now().UTC())
}

type ControllerCodexPreviewInput struct {
	ProfileHome  string `json:"profile_home"`
	CredentialID string `json:"credential_id,omitempty"`
	Replace      bool   `json:"replace,omitempty"`
}
type ControllerCodexApplyInput struct {
	ReviewID string `json:"review_id"`
}
type ControllerCodexConnection struct {
	ProfileHome  string    `json:"profile_home"`
	CredentialID string    `json:"credential_id,omitempty"`
	AccountID    string    `json:"account_id"`
	PrincipalID  string    `json:"principal_id"`
	ReviewID     string    `json:"review_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Status       string    `json:"status"`
}
type nativeConnectionReview struct {
	Subject      core.Subject
	Metadata     ControllerCodexConnection
	Config       []byte
	Previous     *config.ControllerCodexLogin
	ConfigReview config.ControllerCodexConfigReview
}

func connectionIdentity(s core.Subject) core.Subject {
	s.AuthenticatedAt = time.Time{}
	s.ExpiresAt = time.Time{}
	return s
}

func nativeControllerSourceAllowed(home, operatorHome string) bool {
	if !filepath.IsAbs(home) || filepath.Clean(home) != home {
		return false
	}
	// Shared-root use is an explicitly selected source, never fallback.
	return home == filepath.Join(operatorHome, ".hermes") || filepath.Dir(home) == filepath.Join(operatorHome, ".hermes", "profiles")
}

func (s *Service) nativeConnectionAdmission(sub core.Subject, home string) error {
	if s.requirePrincipal(sub) != nil || sub.Method != "local-os" || sub.Issuer != "linux-so-peercred" || sub.Kind != "human" || sub.ID != "local-uid:"+s.Config.Principal.UID || sub.Claims["uid"] != s.Config.Principal.UID {
		return ErrDenied
	}
	if sub.AuthenticatedAt.IsZero() || sub.AuthenticatedAt.After(s.Now()) || !s.Now().Before(sub.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return ErrDenied
	}
	if s.Current == nil || s.Hermes == nil || s.ConfigFile == "" {
		return ErrDenied
	}
	u, e := s.Current()
	if e != nil || u == nil || u.Uid != s.Config.Principal.UID || u.Username != s.Config.Principal.User || !nativeControllerSourceAllowed(home, u.HomeDir) {
		return ErrDenied
	}
	return nil
}

// Preview uses the exact qualified native parser, not raw account/config input.
// Controller operational-source connection grants no Agent authority and requires
// the existing fresh kernel-authenticated principal, not foundational approval.
func (s *Service) PreviewControllerCodexAs(ctx context.Context, sub core.Subject, in ControllerCodexPreviewInput) (ControllerCodexConnection, error) {
	s.nativeConnectionMu.Lock()
	defer s.nativeConnectionMu.Unlock()
	if s.nativeConnectionAdmission(sub, in.ProfileHome) != nil {
		return ControllerCodexConnection{}, ErrDenied
	}
	snapshot, e := config.ConnectionSnapshot(s.ConfigFile)
	if e != nil {
		return ControllerCodexConnection{}, ErrConflict
	}
	cfg, e := config.Load(s.ConfigFile, nil)
	if e != nil {
		return ControllerCodexConnection{}, ErrConflict
	}
	existing, bindingErr := config.ReadControllerCodexBinding(s.ConfigFile)
	var configReview config.ControllerCodexConfigReview
	if bindingErr != nil && in.Replace {
		existing, configReview, bindingErr = config.ReadControllerCodexForReview(s.ConfigFile)
	}
	if bindingErr != nil || (existing != nil) != in.Replace || cfg.Principal != s.Config.Principal {
		return ControllerCodexConnection{}, ErrConflict
	}
	if configReview.Additive && (existing == nil || existing.ProfileHome != in.ProfileHome || existing.CredentialID != in.CredentialID || existing.ApprovedBy != sub.PrincipalID) {
		return ControllerCodexConnection{}, ErrDenied
	}
	// Validate the proposed native-vs-environment exclusivity before source access.
	cfg.Credentials.ControllerCodexLogin = &config.ControllerCodexLogin{ProfileHome: in.ProfileHome, CredentialID: in.CredentialID, AccountID: "pending-native-account", ApprovedBy: sub.PrincipalID}
	if cfg.Validate() != nil {
		return ControllerCodexConnection{}, ErrDenied
	}
	metadata, e := s.Hermes.PreviewNativeCodexLogin(ctx, in.ProfileHome, s.Now().UTC(), in.CredentialID)
	if e != nil {
		return ControllerCodexConnection{}, e
	}
	if configReview.Additive && metadata.AccountID != existing.AccountID {
		return ControllerCodexConnection{}, ErrDenied
	}
	now := s.Now().UTC()
	if s.nativeReviews == nil {
		s.nativeReviews = map[string]nativeConnectionReview{}
	}
	for k, r := range s.nativeReviews {
		if !now.Before(r.Metadata.ExpiresAt) {
			delete(s.nativeReviews, k)
		}
	}
	if len(s.nativeReviews) >= 64 {
		return ControllerCodexConnection{}, ErrConflict
	}
	var entropy [32]byte
	if _, e = rand.Read(entropy[:]); e != nil {
		return ControllerCodexConnection{}, ErrDenied
	}
	out := ControllerCodexConnection{ProfileHome: in.ProfileHome, CredentialID: in.CredentialID, AccountID: metadata.AccountID, PrincipalID: sub.PrincipalID, ReviewID: hex.EncodeToString(entropy[:]), ExpiresAt: now.Add(5 * time.Minute), Status: "review"}
	s.nativeReviews[out.ReviewID] = nativeConnectionReview{Subject: connectionIdentity(sub), Metadata: out, Config: snapshot, Previous: existing, ConfigReview: configReview}
	return out, nil
}

func (s *Service) ApplyControllerCodexAs(ctx context.Context, sub core.Subject, in ControllerCodexApplyInput) (ControllerCodexConnection, error) {
	s.nativeConnectionMu.Lock()
	defer s.nativeConnectionMu.Unlock()
	r, ok := s.nativeReviews[in.ReviewID]
	if !ok || !reflect.DeepEqual(r.Subject, connectionIdentity(sub)) || !s.Now().Before(r.Metadata.ExpiresAt) || s.nativeConnectionAdmission(sub, r.Metadata.ProfileHome) != nil {
		return ControllerCodexConnection{}, ErrDenied
	}
	delete(s.nativeReviews, in.ReviewID)
	current, e := s.Hermes.PreviewNativeCodexLogin(ctx, r.Metadata.ProfileHome, s.Now().UTC(), r.Metadata.CredentialID)
	if e != nil {
		return ControllerCodexConnection{}, e
	}
	if current.AccountID != r.Metadata.AccountID {
		return ControllerCodexConnection{}, ErrConflict
	}
	binding := config.ControllerCodexLogin{ProfileHome: r.Metadata.ProfileHome, CredentialID: r.Metadata.CredentialID, AccountID: current.AccountID, ApprovedBy: sub.PrincipalID}
	if r.ConfigReview.Additive && r.Previous != nil {
		e = config.ReplaceControllerCodexReviewed(s.ConfigFile, r.Config, *r.Previous, binding, r.ConfigReview)
	} else if r.Previous != nil {
		e = config.ReplaceControllerCodex(s.ConfigFile, r.Config, *r.Previous, binding)
	} else {
		e = config.ConnectControllerCodex(s.ConfigFile, r.Config, binding)
	}
	if e != nil {
		return ControllerCodexConnection{}, ErrConflict
	}
	// Config remains immutable in memory. Runtime resolution independently loads
	// only this exact operational binding, avoiding concurrent Config data races.
	out, e := s.ReadControllerCodexAs(ctx, sub)
	if e != nil {
		return ControllerCodexConnection{}, e
	}
	if e = s.audit(ctx, core.AuditEvent{Type: "controller_codex_connection", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "success", Reason: "exact_native_source_connected"}); e != nil {
		return ControllerCodexConnection{}, e
	}
	return out, nil
}
func (s *Service) ReadControllerCodexAs(ctx context.Context, sub core.Subject) (ControllerCodexConnection, error) {
	if s.requirePrincipal(sub) != nil {
		return ControllerCodexConnection{}, ErrDenied
	}
	if _, e := config.ConnectionSnapshot(s.ConfigFile); e != nil {
		return ControllerCodexConnection{}, ErrConflict
	}
	cfg, e := config.Load(s.ConfigFile, nil)
	if e != nil || cfg.Principal != s.Config.Principal {
		return ControllerCodexConnection{}, ErrConflict
	}
	b, e := config.ReadControllerCodexBinding(s.ConfigFile)
	if e != nil {
		return ControllerCodexConnection{}, ErrConflict
	}
	if b == nil {
		return ControllerCodexConnection{Status: "not_connected"}, nil
	}
	return ControllerCodexConnection{ProfileHome: b.ProfileHome, CredentialID: b.CredentialID, AccountID: b.AccountID, PrincipalID: b.ApprovedBy, Status: "connected"}, nil
}

// Load fresh binding only; all unrelated startup configuration stays immutable.
func (s *Service) effectiveControllerCodexConfig() (config.Config, error) {
	if s.ConfigFile == "" {
		return s.Config, nil
	}
	if _, e := config.ConnectionSnapshot(s.ConfigFile); e != nil {
		return config.Config{}, hermes.ErrProviderAuthUnauthorized
	}
	cfg, e := config.Load(s.ConfigFile, nil)
	if e != nil || cfg.Principal != s.Config.Principal {
		return config.Config{}, hermes.ErrProviderAuthUnauthorized
	}
	result := s.Config
	binding, e := config.ReadControllerCodexBinding(s.ConfigFile)
	if e != nil {
		return config.Config{}, hermes.ErrProviderAuthUnauthorized
	}
	result.Credentials.ControllerCodexLogin = binding
	return result, nil
}
