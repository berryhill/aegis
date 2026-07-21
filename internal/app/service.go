package app

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"

	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/credentials/broker"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

var (
	ErrUnauthenticated = errors.New("authentication failed")
	ErrDenied          = errors.New("authorization denied")
	ErrAmbiguous       = errors.New("authorization is ambiguous")
	ErrExpired         = errors.New("authority expired")
	ErrConflict        = errors.New("state conflict")
)

type Service struct {
	Config    config.Config
	Store     *store.Store
	Audit     AuditAuthority
	Hermes    *hermes.Adapter
	Log       *slog.Logger
	Now       func() time.Time
	Current   func() (*user.User, error)
	LookupEnv func(string) (string, bool)

	CredentialAuthority *credentials.Authority
	capMu               sync.RWMutex
	capabilities        map[[32]byte]broker.Capability
	brokerRequests      map[[32]byte]map[[32]byte]struct{}
}

// AuditAuthority is the narrow append and verification boundary that hardened
// deployments can place behind a separately supervised process or OS account.
// Hermes runtimes never receive this authority.
type AuditAuthority interface {
	AppendAudit(context.Context, core.AuditEvent) error
	AuditEvents() ([]core.AuditEvent, error)
	VerifyAudit() error
}

func New(cfg config.Config, st *store.Store, h *hermes.Adapter, log *slog.Logger) *Service {
	return &Service{Config: cfg, Store: st, Audit: st, Hermes: h, Log: log.With("component", "app"), Now: func() time.Time { return time.Now().UTC() }, Current: user.Current, LookupEnv: os.LookupEnv, capabilities: make(map[[32]byte]broker.Capability), brokerRequests: make(map[[32]byte]map[[32]byte]struct{})}
}

func (s *Service) resolveProviderCredential(provider string, scopes []string) ([]hermes.Credential, error) {
	reference := "provider:" + provider
	if provider == "" || !contains(scopes, reference) {
		return nil, fmt.Errorf("credential scope must include %q", reference)
	}
	binding, ok := s.Config.Credentials.ProviderAuth[provider]
	if !ok || binding.Type != "environment" {
		return nil, fmt.Errorf("selected provider credential %q is not configured", reference)
	}
	value, ok := s.LookupEnv(binding.SourceEnv)
	if !ok || value == "" {
		return nil, fmt.Errorf("selected provider credential %q is unavailable", reference)
	}
	return []hermes.Credential{{Reference: reference, TargetEnv: binding.TargetEnv, Value: value}}, nil
}

func (s *Service) resolveDesignCredential() ([]hermes.Credential, error) {
	provider := s.Config.Credentials.DesignProvider
	if provider == "" {
		return nil, nil
	}
	credentials, err := s.resolveProviderCredential(provider, []string{"provider:" + provider})
	return credentials, err
}

func (s *Service) audit(ctx context.Context, e core.AuditEvent) error {
	return s.Audit.AppendAudit(ctx, e)
}

// AuditCredentialOperation records metadata-only administrative credential
// events. Credential values, references, destinations, and key material are
// intentionally excluded from this boundary.
func (s *Service) AuditCredentialOperation(ctx context.Context, subject core.Subject, eventType, outcome, reason, recordID string) error {
	if subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID {
		return ErrDenied
	}
	allowed := map[string]bool{
		"credential_authority_initialized": true,
		"credential_created":               true,
		"credential_rotated":               true,
		"credential_revoked":               true,
		"credential_bound":                 true,
		"credential_value_read":            true,
		"credential_backup_created":        true,
	}
	if !allowed[eventType] || (outcome != "ok" && outcome != "denied") || strings.TrimSpace(reason) == "" {
		return errors.New("invalid credential audit event")
	}
	metadata := map[string]string{}
	if recordID != "" {
		metadata["record_id"] = recordID
	}
	return s.audit(ctx, core.AuditEvent{Type: eventType, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: outcome, Reason: reason, Metadata: metadata})
}

// AuditManagerSession appends one metadata-only manager lifecycle event.
func (s *Service) AuditManagerSession(ctx context.Context, subject core.Subject, outcome, reason string, metadata map[string]string) error {
	if subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID || (outcome != "ok" && outcome != "denied") || strings.TrimSpace(reason) == "" {
		return ErrDenied
	}
	return s.audit(ctx, core.AuditEvent{Type: "manager_session_closed", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: outcome, Reason: reason, Metadata: metadata})
}

// AuditManagerStartup appends one metadata-only preflight result while
// preserving the exact readiness reason selected outside the model.
func (s *Service) AuditManagerStartup(ctx context.Context, subject core.Subject, outcome, reason string, metadata map[string]string) error {
	if subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID || (outcome != "ok" && outcome != "degraded" && outcome != "denied") || strings.TrimSpace(reason) == "" {
		return ErrDenied
	}
	return s.audit(ctx, core.AuditEvent{Type: "manager_startup", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: outcome, Reason: reason, Metadata: metadata})
}

// AuditManagerCertification records metadata-only certification attempts. It
// never accepts prompts, responses, credential material, or model output.
func (s *Service) AuditManagerCertification(ctx context.Context, subject core.Subject, outcome, reason string, metadata map[string]string) error {
	if subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID || (outcome != "ok" && outcome != "denied") || strings.TrimSpace(reason) == "" {
		return ErrDenied
	}
	return s.audit(ctx, core.AuditEvent{Type: "manager_certification", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: outcome, Reason: reason, Metadata: metadata})
}

// AuditManagerOnboarding records metadata-only acquisition and binding events.
func (s *Service) AuditManagerOnboarding(ctx context.Context, subject core.Subject, action, outcome, reason string, metadata map[string]string) error {
	allowed := action == "model_pull" || action == "model_bound"
	if !allowed || subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID || (outcome != "ok" && outcome != "denied") || strings.TrimSpace(reason) == "" {
		return ErrDenied
	}
	copy := make(map[string]string, len(metadata)+1)
	for key, value := range metadata {
		copy[key] = value
	}
	copy["action"] = action
	return s.audit(ctx, core.AuditEvent{Type: "manager_onboarding", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: outcome, Reason: reason, Metadata: copy})
}

// AuditManagerCommand records only canonical registry metadata. Command text,
// arguments, protected values, evidence, and rendered output are excluded.
func (s *Service) AuditManagerCommand(ctx context.Context, subject core.Subject, operation, outcome, reason, operationID, scopeDigest string) error {
	if subject.PrincipalID == "" || subject.PrincipalID != s.Config.Principal.ID || !strings.HasPrefix(operation, "manager.") || strings.ContainsAny(operation, " 	\r\n") || strings.TrimSpace(reason) == "" {
		return ErrDenied
	}
	allowedOutcome := outcome == "completed" || outcome == "unavailable" || outcome == "denied" || outcome == "failed" || outcome == "accepted" || outcome == "cancel_requested" || outcome == "degraded" || outcome == "partial" || outcome == "completed_no_findings" || outcome == "completed_with_findings" || outcome == "cancelled"
	if !allowedOutcome {
		return ErrDenied
	}
	metadata := map[string]string{"operation": operation}
	if operationID != "" {
		metadata["operation_id"] = operationID
	}
	if scopeDigest != "" {
		metadata["scope_digest"] = scopeDigest
	}
	return s.audit(ctx, core.AuditEvent{Type: "manager_command", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: "aegis", StanzaID: "secrets-manager", MandateID: subject.ID, Runtime: "hermes-agent", Outcome: outcome, Reason: reason, Metadata: metadata})
}

// Authenticate uses only the kernel-backed process account mapping. No prompt,
// display name, requested stanza, or CLI authority flag is accepted as evidence.
func (s *Service) Authenticate(ctx context.Context) (core.Subject, error) {
	now := s.Now()
	u, err := s.Current()
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "authentication", Outcome: "failure", Reason: "os_identity_unavailable"})
		return core.Subject{}, fmt.Errorf("%w: obtain operating-system identity", ErrUnauthenticated)
	}
	uidOK := subtle.ConstantTimeCompare([]byte(u.Uid), []byte(s.Config.Principal.UID)) == 1
	userOK := subtle.ConstantTimeCompare([]byte(u.Username), []byte(s.Config.Principal.User)) == 1
	if u.Uid == "" || u.Username == "" {
		_ = s.audit(ctx, core.AuditEvent{Type: "authentication", Outcome: "failure", Reason: "incomplete_os_identity"})
		return core.Subject{}, fmt.Errorf("%w: incomplete local operating-system identity", ErrUnauthenticated)
	}
	sub := core.Subject{ID: "local-uid:" + u.Uid, Kind: "human", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(s.Config.Principal.AuthTTL), Claims: map[string]string{"uid": u.Uid, "user": u.Username}}
	reason := "local_os_identity_verified"
	if uidOK && userOK {
		sub.PrincipalID = s.Config.Principal.ID
		reason = "local_os_principal_mapped"
	}
	if err := s.audit(ctx, core.AuditEvent{Type: "authentication", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "success", Reason: reason}); err != nil {
		return core.Subject{}, err
	}
	return sub, nil
}

// AuthenticateUnixPeer maps kernel-supplied Unix-socket peer credentials. The
// UID comes from SO_PEERCRED, never an HTTP field or bearer-token label.
func (s *Service) AuthenticateUnixPeer(ctx context.Context, uid uint32) (core.Subject, error) {
	now := s.Now()
	uidString := strconv.FormatUint(uint64(uid), 10)
	sub := core.Subject{ID: "local-uid:" + uidString, Kind: "human", Issuer: "linux-so-peercred", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(s.Config.Principal.AuthTTL), Claims: map[string]string{"uid": uidString}}
	if subtle.ConstantTimeCompare([]byte(uidString), []byte(s.Config.Principal.UID)) == 1 {
		sub.PrincipalID = s.Config.Principal.ID
		sub.Claims["user"] = s.Config.Principal.User
	}
	if err := s.audit(ctx, core.AuditEvent{Type: "authentication", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "success", Reason: "unix_peer_credentials_verified"}); err != nil {
		return core.Subject{}, err
	}
	return sub, nil
}

func (s *Service) requirePrincipal(sub core.Subject) error {
	if sub.PrincipalID != s.Config.Principal.ID || sub.PrincipalID == "" || !s.Now().Before(sub.ExpiresAt) {
		return fmt.Errorf("%w: operation requires fresh configured principal authentication", ErrDenied)
	}
	return nil
}

func (s *Service) Runtime(ctx context.Context) (core.RuntimeDescriptor, error) {
	return s.Hermes.Discover(ctx)
}

func (s *Service) ImportCharter(ctx context.Context, data []byte) (core.CanonicalCharter, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	return s.ImportCharterAs(ctx, sub, data)
}

func (s *Service) ImportCharterAs(ctx context.Context, sub core.Subject, data []byte) (core.CanonicalCharter, error) {
	var err error
	if err = s.requirePrincipal(sub); err != nil {
		return core.CanonicalCharter{}, err
	}
	c, err := core.DecodeCharter(strings.NewReader(string(data)))
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "charter", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "failure", Reason: "charter_invalid"})
		return core.CanonicalCharter{}, err
	}
	if c.CreatedBy != sub.PrincipalID {
		return core.CanonicalCharter{}, fmt.Errorf("%w: created_by must equal authenticated principal", ErrDenied)
	}
	if _, err = s.Runtime(ctx); err != nil {
		return core.CanonicalCharter{}, err
	}
	cc, err := core.Canonicalize(c)
	if err != nil {
		return cc, err
	}
	if err = s.Store.SaveCharter(cc); err != nil {
		return cc, err
	}
	err = s.audit(ctx, core.AuditEvent{Type: "charter", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: c.AgentID, CharterRevision: c.Revision, CharterDigest: cc.Digest, Runtime: c.Runtime.Runtime, Outcome: "success", Reason: "draft_saved"})
	return cc, err
}

func (s *Service) ValidateCharter(ctx context.Context, data []byte) (core.CanonicalCharter, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	return s.ValidateCharterAs(ctx, subject, data)
}

func (s *Service) ValidateCharterAs(ctx context.Context, subject core.Subject, data []byte) (core.CanonicalCharter, error) {
	c, err := core.DecodeCharter(strings.NewReader(string(data)))
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "charter", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "failure", Reason: "charter_validation_failed"})
		return core.CanonicalCharter{}, err
	}
	canonical, err := core.Canonicalize(c)
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "charter", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: c.AgentID, CharterRevision: c.Revision, Runtime: c.Runtime.Runtime, Outcome: "failure", Reason: "charter_validation_failed"})
		return core.CanonicalCharter{}, err
	}
	err = s.audit(ctx, core.AuditEvent{Type: "charter", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: c.AgentID, CharterRevision: c.Revision, CharterDigest: canonical.Digest, Runtime: c.Runtime.Runtime, Outcome: "success", Reason: "charter_validated"})
	return canonical, err
}

func (s *Service) GetCharter(agent string, rev uint64) (core.CanonicalCharter, error) {
	return s.Store.GetCharter(agent, rev)
}
func (s *Service) ListAgents() ([]string, error) { return s.Store.ListAgents() }
func (s *Service) ListCharters(agent string) ([]core.CanonicalCharter, error) {
	return s.Store.ListCharters(agent)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func selectorMatches(sel core.IdentitySelector, sub core.Subject, env core.Environment) bool {
	if len(sel.Kinds) > 0 && !contains(sel.Kinds, sub.Kind) {
		return false
	}
	if len(sel.SubjectIDs) > 0 && !contains(sel.SubjectIDs, sub.ID) {
		return false
	}
	if len(sel.PrincipalIDs) > 0 && !contains(sel.PrincipalIDs, sub.PrincipalID) {
		return false
	}
	if len(sel.Issuers) > 0 && !contains(sel.Issuers, sub.Issuer) {
		return false
	}
	if len(sel.Environments) > 0 && !contains(sel.Environments, env.Name) {
		return false
	}
	for k, v := range sel.Claims {
		if sub.Claims[k] != v {
			return false
		}
	}
	return true
}

func (s *Service) Select(c core.CanonicalCharter, sub core.Subject, requested string, env core.Environment) (core.Decision, error) {
	now := s.Now()
	trusted := map[string]any{"subject_id": sub.ID, "principal_id": sub.PrincipalID, "issuer": sub.Issuer, "method": sub.Method, "environment": env, "requested_stanza": requested, "charter_digest": c.Digest}
	d := core.Decision{TrustedInputs: trusted}
	deny := func(reason string, err error) (core.Decision, error) {
		d.Reason = reason
		return d, err
	}
	if env.Name != "local" || env.Host != "" || env.Tenant != "" {
		return deny("invalid_environment", fmt.Errorf("%w: trusted environment must be the local control-plane environment", ErrDenied))
	}
	if sub.ID == "" || sub.Kind == "" || sub.Issuer == "" || sub.Method == "" || sub.AuthenticatedAt.IsZero() || sub.ExpiresAt.IsZero() || !sub.AuthenticatedAt.Before(sub.ExpiresAt) || now.Before(sub.AuthenticatedAt) {
		return deny("invalid_authenticated_subject", fmt.Errorf("%w: authenticated subject is incomplete or invalid", ErrUnauthenticated))
	}
	if !now.Before(sub.ExpiresAt) {
		return deny("expired_authentication", fmt.Errorf("%w: authenticated subject expired", ErrUnauthenticated))
	}
	// Validate every stanza independently while deliberately excluding overlap
	// analysis. Imported/stored charters reject overlap; this runtime check still
	// permits legacy policy to reach the required multiple-match denial.
	for i := range c.Charter.Stanzas {
		check := c.Charter
		check.Stanzas = append([]core.TrustStanza(nil), c.Charter.Stanzas...)
		for j := range check.Stanzas {
			check.Stanzas[j].Enabled = check.Stanzas[j].Enabled && i == j
		}
		if err := core.ValidateCharter(check); err != nil {
			return deny("invalid_charter", fmt.Errorf("%w: malformed trust stanza policy", ErrDenied))
		}
	}

	authorized := make([]core.TrustStanza, 0, 1)
	staleMatch := false
	for _, st := range c.Charter.Stanzas {
		if !st.Enabled || !contains(st.Authentication.Methods, sub.Method) {
			continue
		}
		selectorMatch := false
		for _, sel := range st.Authentication.Selectors {
			if selectorMatches(sel, sub, env) {
				selectorMatch = true
				break
			}
		}
		if !selectorMatch {
			continue
		}
		if st.Authentication.RequireFresh && now.Sub(sub.AuthenticatedAt) > time.Duration(st.Authentication.MaxAuthAgeSec)*time.Second {
			staleMatch = true
			continue
		}
		authorized = append(authorized, st)
	}

	matches := authorized
	if requested != "" {
		matches = matches[:0]
		for _, st := range authorized {
			if st.ID == requested {
				matches = append(matches, st)
			}
		}
	}
	d.MatchingCount = len(matches)
	if len(matches) == 0 {
		if requested != "" {
			return deny("requested_stanza_unauthorized", fmt.Errorf("%w: requested stanza is not authorized", ErrDenied))
		}
		if staleMatch {
			return deny("stale_authentication", fmt.Errorf("%w: authentication is not fresh enough", ErrDenied))
		}
		return deny("zero_authorized_matches", fmt.Errorf("%w: no authorized stanza", ErrDenied))
	}
	if len(matches) > 1 {
		return deny("multiple_authorized_matches", fmt.Errorf("%w: %d stanzas match", ErrAmbiguous, len(matches)))
	}
	d.Allowed, d.Selected, d.Reason = true, &matches[0], "exactly_one_authorized_match"
	return d, nil
}

func (s *Service) Explain(ctx context.Context, agent string, rev uint64, requested string, env core.Environment) (core.Decision, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Decision{}, err
	}
	return s.ExplainAs(ctx, sub, agent, rev, requested, env)
}

func (s *Service) ExplainAs(ctx context.Context, sub core.Subject, agent string, rev uint64, requested string, env core.Environment) (core.Decision, error) {
	c, err := s.GetCharter(agent, rev)
	if err != nil {
		return core.Decision{}, err
	}
	d, selErr := s.Select(c, sub, requested, env)
	outcome := "deny"
	if d.Allowed {
		outcome = "allow"
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent, StanzaID: requested, CharterRevision: c.Charter.Revision, CharterDigest: c.Digest, Outcome: outcome, Reason: d.Reason})
	return d, selErr
}

func effectiveTools(st core.TrustStanza) ([]string, error) {
	// In the MVP, charter tool IDs are Hermes toolset IDs: the unit Hermes can
	// hard-restrict at process initialization. Requiring the two declarations
	// to be identical prevents a narrow displayed grant with a broader hidden
	// runtime tool surface.
	requested, err := hermes.ResolveTools(st.Grant.Tools)
	if err != nil {
		return nil, err
	}
	runtime, err := hermes.ResolveTools(st.Hermes.Toolsets)
	if err != nil {
		return nil, err
	}
	a, b := append([]string(nil), requested...), append([]string(nil), runtime...)
	sort.Strings(a)
	sort.Strings(b)
	if strings.Join(a, "\x00") != strings.Join(b, "\x00") {
		return nil, fmt.Errorf("Hermes runtime toolsets must exactly match the stanza tool grant")
	}
	return requested, nil
}

func authorityProjection(st core.TrustStanza) core.EffectiveAuthority {
	return core.EffectiveAuthority{
		StanzaID:     st.ID,
		Capabilities: append([]string(nil), st.Grant.Capabilities...),
		Tools:        append([]string(nil), st.Grant.Tools...),
		Memory:       append([]string(nil), st.Scopes.Memory...),
		Credentials:  append([]string(nil), st.Scopes.Credentials...),
		Session:      st.Session,
		Approval:     st.Approval,
		Hermes:       st.Hermes,
	}
}

func (s *Service) EffectiveAuthority(ctx context.Context, agent string, rev uint64, requested string, env core.Environment) (string, core.EffectiveAuthority, core.Decision, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return "", core.EffectiveAuthority{}, core.Decision{}, err
	}
	return s.EffectiveAuthorityAs(subject, agent, rev, requested, env)
}

func (s *Service) EffectiveAuthorityAs(subject core.Subject, agent string, rev uint64, requested string, env core.Environment) (string, core.EffectiveAuthority, core.Decision, error) {
	c, err := s.GetCharter(agent, rev)
	if err != nil {
		return "", core.EffectiveAuthority{}, core.Decision{}, err
	}
	decision, err := s.Select(c, subject, requested, env)
	if err != nil {
		return c.Digest, core.EffectiveAuthority{}, decision, err
	}
	if _, err = effectiveTools(*decision.Selected); err != nil {
		return c.Digest, core.EffectiveAuthority{}, decision, err
	}
	return c.Digest, authorityProjection(*decision.Selected), decision, nil
}

func versionTuple(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("version %q is not semantic x.y.z", v)
	}
	for i := range parts {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, fmt.Errorf("version %q is invalid", v)
		}
		out[i] = n
	}
	return out, nil
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func runtimeSatisfies(version, constraint string) error {
	got, err := versionTuple(version)
	if err != nil {
		return err
	}
	for _, raw := range strings.Split(constraint, ",") {
		term := strings.TrimSpace(raw)
		op := "="
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(term, candidate) {
				op = candidate
				term = strings.TrimSpace(strings.TrimPrefix(term, candidate))
				break
			}
		}
		want, err := versionTuple(term)
		if err != nil {
			return fmt.Errorf("invalid runtime constraint %q: %w", constraint, err)
		}
		cmp := compareVersion(got, want)
		ok := (op == "=" && cmp == 0) || (op == ">=" && cmp >= 0) || (op == "<=" && cmp <= 0) || (op == ">" && cmp > 0) || (op == "<" && cmp < 0)
		if !ok {
			return fmt.Errorf("Hermes %s does not satisfy charter constraint %q", version, constraint)
		}
	}
	return nil
}

func fullCharterDiff(previous *core.CanonicalCharter, current core.CanonicalCharter) string {
	var out strings.Builder
	previousLabel := "/dev/null"
	if previous != nil {
		previousLabel = fmt.Sprintf("%s revision %d", previous.Charter.AgentID, previous.Charter.Revision)
	}
	fmt.Fprintf(&out, "--- %s\n+++ %s revision %d\n", previousLabel, current.Charter.AgentID, current.Charter.Revision)
	write := func(prefix byte, data []byte) {
		var pretty bytes.Buffer
		if json.Indent(&pretty, data, "", "  ") != nil {
			pretty.Write(data)
		}
		for _, line := range strings.Split(pretty.String(), "\n") {
			out.WriteByte(prefix)
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	if previous != nil {
		write('-', previous.Canonical)
	}
	write('+', current.Canonical)
	return out.String()
}

func (s *Service) charterDiff(c core.CanonicalCharter) (string, error) {
	revisions, err := s.ListCharters(c.Charter.AgentID)
	if err != nil {
		return "", err
	}
	var previous *core.CanonicalCharter
	for i := range revisions {
		if revisions[i].Charter.Revision < c.Charter.Revision && (previous == nil || revisions[i].Charter.Revision > previous.Charter.Revision) {
			candidate := revisions[i]
			previous = &candidate
		}
	}
	return fullCharterDiff(previous, c), nil
}

func (s *Service) PreviewPlan(ctx context.Context, agent string, rev uint64, env core.Environment) (core.Review, error) {
	c, err := s.GetCharter(agent, rev)
	if err != nil {
		return core.Review{}, err
	}
	rt, err := s.Runtime(ctx)
	if err != nil {
		return core.Review{}, err
	}
	if err = runtimeSatisfies(rt.Version, c.Charter.Runtime.VersionConstraint); err != nil {
		return core.Review{}, err
	}
	target := filepath.Join(s.Store.Root(), "provisioned", c.Charter.AgentID, strconv.FormatUint(c.Charter.Revision, 10), "hermes-config.json")
	payload := map[string]any{"agent_id": c.Charter.AgentID, "revision": c.Charter.Revision, "charter_digest": c.Digest, "runtime": c.Charter.Runtime, "stanzas": c.Charter.Stanzas}
	e := core.Effect{Kind: core.EffectCreateFile, Target: target, Description: "publish deterministic Aegis-owned Hermes mapping", Consequential: true, Digest: core.Digest(payload)}
	for _, st := range c.Charter.Stanzas {
		if st.Hermes.Profile != "" {
			return core.Review{}, fmt.Errorf("persistent named Hermes profiles are not supported by the MVP provisioner")
		}
		if len(st.Hermes.MCPServers) > 0 || len(st.Hermes.Plugins) > 0 {
			return core.Review{}, fmt.Errorf("MCP servers and plugins are not supported by the MVP provisioner")
		}
		if _, err = effectiveTools(st); err != nil {
			return core.Review{}, err
		}
	}
	plan := core.Plan{ID: store.ID("plan"), AgentID: agent, Revision: c.Charter.Revision, CharterDigest: c.Digest, Runtime: rt, Environment: env, Effects: []core.Effect{e}, CreatedAt: s.Now()}
	plan.Digest = core.PlanDigest(plan)
	if err = s.Store.Save("plans", plan.ID, plan); err != nil {
		return core.Review{}, err
	}
	diff, err := s.charterDiff(c)
	if err != nil {
		return core.Review{}, err
	}
	toolsets := make(map[string][]string, len(c.Charter.Stanzas))
	credentials := make(map[string][]string, len(c.Charter.Stanzas))
	memory := make(map[string][]string, len(c.Charter.Stanzas))
	approvalRequirements := make(map[string]core.ApprovalPolicy, len(c.Charter.Stanzas))
	for _, stanza := range c.Charter.Stanzas {
		toolsets[stanza.ID] = append([]string(nil), stanza.Hermes.Toolsets...)
		credentials[stanza.ID] = append([]string(nil), stanza.Scopes.Credentials...)
		memory[stanza.ID] = append([]string(nil), stanza.Scopes.Memory...)
		approvalRequirements[stanza.ID] = stanza.Approval
	}
	review := core.Review{
		Summary:              fmt.Sprintf("Agent %s revision %d on Hermes Agent %s at %s using adapter %s; target environment %s; %d planned effect(s)", agent, c.Charter.Revision, rt.Version, rt.Executable, rt.AdapterVersion, env.Name, len(plan.Effects)),
		Diff:                 diff,
		Warnings:             []string{"Hermes home isolation is not host filesystem sandboxing"},
		CharterDigest:        c.Digest,
		PlanDigest:           plan.Digest,
		RequestedToolsets:    toolsets,
		CredentialScopes:     credentials,
		MemoryScopes:         memory,
		ApprovalRequirements: approvalRequirements,
		Plan:                 plan,
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "provisioning", AgentID: agent, CharterRevision: c.Charter.Revision, CharterDigest: c.Digest, Runtime: rt.Runtime, ProvisioningID: plan.ID, Outcome: "success", Reason: "plan_created"})
	return review, nil
}

func (s *Service) GetPlan(id string) (core.Plan, error) {
	var p core.Plan
	if err := s.Store.Load("plans", id, &p); err != nil {
		return p, err
	}
	if p.Digest == "" || core.PlanDigest(p) != p.Digest {
		return core.Plan{}, fmt.Errorf("%w: stored plan digest mismatch", ErrConflict)
	}
	return p, nil
}
func (s *Service) ListPlans() ([]core.Plan, error) {
	var plans []core.Plan
	err := s.Store.List("plans", func(b json.RawMessage) error {
		var plan core.Plan
		if err := json.Unmarshal(b, &plan); err != nil {
			return err
		}
		if plan.Digest == "" || core.PlanDigest(plan) != plan.Digest {
			return fmt.Errorf("%w: stored plan digest mismatch", ErrConflict)
		}
		plans = append(plans, plan)
		return nil
	})
	return plans, err
}
func (s *Service) GetReceipt(id string) (core.Receipt, error) {
	var receipt core.Receipt
	return receipt, s.Store.Load("receipts", id, &receipt)
}
func (s *Service) ListReceipts() ([]core.Receipt, error) {
	var receipts []core.Receipt
	err := s.Store.List("receipts", func(b json.RawMessage) error {
		var receipt core.Receipt
		if err := json.Unmarshal(b, &receipt); err != nil {
			return err
		}
		receipts = append(receipts, receipt)
		return nil
	})
	return receipts, err
}
func (s *Service) RequestApproval(ctx context.Context, planID string, ttl time.Duration) (core.Approval, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Approval{}, err
	}
	return s.RequestApprovalAs(ctx, sub, planID, ttl)
}
func (s *Service) RequestApprovalAs(ctx context.Context, sub core.Subject, planID string, ttl time.Duration) (core.Approval, error) {
	var err error
	if err = s.requirePrincipal(sub); err != nil {
		return core.Approval{}, err
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		return core.Approval{}, fmt.Errorf("approval lifetime must be positive and at most 15m")
	}
	p, err := s.GetPlan(planID)
	if err != nil {
		return core.Approval{}, err
	}
	a := core.Approval{ID: store.ID("approval"), PlanDigest: p.Digest, CharterDigest: p.CharterDigest, Runtime: p.Runtime.Runtime, RuntimeVersion: p.Runtime.Version, Environment: p.Environment, Status: "pending", RequestedBy: sub.PrincipalID, RequestedAt: s.Now(), ExpiresAt: s.Now().Add(ttl)}
	if err = s.Store.Save("approvals", a.ID, a); err != nil {
		return a, err
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "approval", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: p.AgentID, CharterRevision: p.Revision, CharterDigest: p.CharterDigest, ApprovalID: a.ID, ProvisioningID: p.ID, Runtime: p.Runtime.Runtime, Outcome: "pending", Reason: "approval_requested"})
	return a, nil
}
func (s *Service) GetApproval(id string) (core.Approval, error) {
	var a core.Approval
	return a, s.Store.Load("approvals", id, &a)
}
func (s *Service) ListApprovals() ([]core.Approval, error) {
	var approvals []core.Approval
	err := s.Store.List("approvals", func(b json.RawMessage) error {
		var approval core.Approval
		if err := json.Unmarshal(b, &approval); err != nil {
			return err
		}
		approvals = append(approvals, approval)
		return nil
	})
	return approvals, err
}
func (s *Service) DecideApproval(ctx context.Context, id string, approve bool) (core.Approval, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Approval{}, err
	}
	return s.DecideApprovalAs(ctx, sub, id, approve)
}
func (s *Service) DecideApprovalAs(ctx context.Context, sub core.Subject, id string, approve bool) (core.Approval, error) {
	var err error
	if err = s.requirePrincipal(sub); err != nil {
		return core.Approval{}, err
	}
	var out core.Approval
	err = s.Store.Update("approvals", id, func(b []byte) (any, error) {
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		if out.Status != "pending" {
			return nil, fmt.Errorf("%w: approval is %s", ErrConflict, out.Status)
		}
		if !s.Now().Before(out.ExpiresAt) {
			out.Status = "expired"
			return out, ErrExpired
		}
		now := s.Now()
		out.DecidedAt = &now
		out.ApprovedBy = sub.PrincipalID
		if approve {
			out.Status = "approved"
		} else {
			out.Status = "rejected"
		}
		return out, nil
	})
	outcome := out.Status
	if err != nil {
		outcome = "failure"
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "approval", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, CharterDigest: out.CharterDigest, ApprovalID: id, Outcome: outcome, Reason: "principal_decision"})
	return out, err
}

func (s *Service) Apply(ctx context.Context, planID, approvalID string) (core.Receipt, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Receipt{}, err
	}
	return s.ApplyAs(ctx, sub, planID, approvalID)
}

func supportedProvisioningEffect(kind string) (bool, error) {
	switch kind {
	case core.EffectCreateFile:
		return true, nil
	case core.EffectModifyFile, core.EffectCreateHermesProfile, core.EffectConfigureMCP, core.EffectConfigurePlugin, core.EffectStartGateway, core.EffectInstallService, core.EffectCreateCron, core.EffectExternalNetwork:
		return false, nil
	default:
		return false, fmt.Errorf("unknown provisioning effect class %q", kind)
	}
}

func (s *Service) ApplyAs(ctx context.Context, sub core.Subject, planID, approvalID string) (core.Receipt, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return core.Receipt{}, err
	}
	p, err := s.GetPlan(planID)
	if err != nil {
		return core.Receipt{}, err
	}
	c, err := s.GetCharter(p.AgentID, p.Revision)
	if err != nil {
		return core.Receipt{}, err
	}
	if c.Digest != p.CharterDigest {
		return core.Receipt{}, fmt.Errorf("%w: charter changed", ErrConflict)
	}
	var a core.Approval
	r := core.Receipt{ID: store.ID("receipt"), PlanID: p.ID, ApprovalID: approvalID, CharterDigest: c.Digest, Status: "provisioning", StartedAt: s.Now()}
	err = s.Store.UpdateWithIntent("approvals", approvalID, "receipts", r.ID, r, func(b []byte) (any, error) {
		if err := json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		if a.Status != "approved" {
			return nil, fmt.Errorf("%w: approval is not usable", ErrDenied)
		}
		if !s.Now().Before(a.ExpiresAt) {
			a.Status = "expired"
			return a, ErrExpired
		}
		if a.PlanDigest != p.Digest || a.CharterDigest != p.CharterDigest || a.Runtime != p.Runtime.Runtime || a.RuntimeVersion != p.Runtime.Version || a.Environment != p.Environment {
			return nil, fmt.Errorf("%w: exact approval binding mismatch", ErrConflict)
		}
		now := s.Now()
		a.Status = "consumed"
		a.ConsumedAt = &now
		return a, nil
	})
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "approval", ApprovalID: approvalID, ProvisioningID: planID, Outcome: "failure", Reason: "approval_consumption_failed"})
		return core.Receipt{}, err
	}
	r.ApprovalID = a.ID
	r.Status = "applied"
	var published []string
	for _, e := range p.Effects {
		supported, classifyErr := supportedProvisioningEffect(e.Kind)
		if classifyErr != nil {
			r.Status = "failed"
			r.Failure = "unknown effect class"
			err = classifyErr
			break
		}
		if !supported {
			r.Status = "failed"
			r.Failure = "effect class is explicitly unsupported by the MVP provisioner"
			break
		}
		if !strings.HasPrefix(filepath.Clean(e.Target), filepath.Join(s.Store.Root(), "provisioned")+string(os.PathSeparator)) {
			r.Status = "failed"
			r.Failure = "effect outside Aegis-owned provisioned directory"
			break
		}
		payload := map[string]any{"agent_id": c.Charter.AgentID, "revision": c.Charter.Revision, "charter_digest": c.Digest, "runtime": c.Charter.Runtime, "stanzas": c.Charter.Stanzas}
		if core.Digest(payload) != e.Digest {
			r.Status = "failed"
			r.Failure = "effect digest mismatch"
			break
		}
		err = s.Store.PublishProvisioned(e.Target, payload)
		if err != nil {
			r.Status = "failed"
			r.Failure = "atomic publication of deterministic artifact failed"
			break
		}
		published = append(published, e.Target)
		var applied struct {
			AgentID       string                 `json:"agent_id"`
			CharterDigest string                 `json:"charter_digest"`
			Revision      uint64                 `json:"revision"`
			Runtime       core.RuntimeConstraint `json:"runtime"`
			Stanzas       []core.TrustStanza     `json:"stanzas"`
		}
		artifactBytes, readErr := os.ReadFile(e.Target)
		if readErr != nil || json.Unmarshal(artifactBytes, &applied) != nil || core.Digest(applied) != e.Digest {
			r.Status = "failed"
			r.Failure = "published artifact verification failed"
			break
		}
		r.Artifacts = append(r.Artifacts, core.Artifact{Path: e.Target, Action: e.Kind, Digest: core.Digest(applied), Verified: true})
	}
	if r.Status == "applied" {
		r.Status = "verified"
	}
	r.FinishedAt = s.Now()
	if err2 := s.Store.Save("receipts", r.ID, r); err == nil {
		err = err2
	}
	if err != nil || r.Status == "failed" {
		for _, path := range published {
			_ = s.Store.RemoveProvisioned(path)
		}
		for i := range r.Artifacts {
			r.Artifacts[i].Verified = false
		}
		if r.Failure == "" {
			r.Failure = "provisioning receipt publication failed; Aegis-owned artifacts rolled back"
			r.Status = "failed"
		}
		_ = s.Store.Save("receipts", r.ID, r)
	}
	outcome := r.Status
	reason := "provisioning_verified"
	if r.Status == "failed" {
		reason = "provisioning_failed"
		if err == nil {
			err = errors.New(r.Failure)
		}
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "provisioning", AgentID: p.AgentID, CharterRevision: p.Revision, CharterDigest: p.CharterDigest, ApprovalID: a.ID, ProvisioningID: r.ID, Runtime: p.Runtime.Runtime, Outcome: outcome, Reason: reason})
	return r, err
}

func (s *Service) hasVerifiedReceipt(digest string) bool {
	found := false
	_ = s.Store.List("receipts", func(b json.RawMessage) error {
		var r core.Receipt
		if json.Unmarshal(b, &r) == nil && r.CharterDigest == digest && r.Status == "verified" {
			found = true
		}
		return nil
	})
	return found
}

// RecoverProvisioning converts durable in-progress intents left by an
// interrupted process into failed receipts and removes only matching
// Aegis-owned artifacts from their approved plan.
func (s *Service) RecoverProvisioning(ctx context.Context) error {
	receipts, err := s.ListReceipts()
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		if receipt.Status != "provisioning" {
			continue
		}
		receipt.Status = "failed"
		receipt.Failure = "interrupted provisioning recovered"
		plan, planErr := s.GetPlan(receipt.PlanID)
		if planErr == nil {
			for _, effect := range plan.Effects {
				if effect.Kind != core.EffectCreateFile {
					continue
				}
				var applied struct {
					AgentID       string                 `json:"agent_id"`
					CharterDigest string                 `json:"charter_digest"`
					Revision      uint64                 `json:"revision"`
					Runtime       core.RuntimeConstraint `json:"runtime"`
					Stanzas       []core.TrustStanza     `json:"stanzas"`
				}
				artifact, readErr := os.ReadFile(effect.Target)
				if errors.Is(readErr, os.ErrNotExist) {
					continue
				}
				if readErr != nil || json.Unmarshal(artifact, &applied) != nil || core.Digest(applied) != effect.Digest {
					receipt.Failure = "interrupted provisioning found a non-matching artifact; manual intervention required"
					continue
				}
				if removeErr := s.Store.RemoveProvisioned(effect.Target); removeErr != nil {
					return removeErr
				}
			}
		} else {
			receipt.Failure = "interrupted provisioning plan is unavailable or corrupt; manual intervention required"
		}
		receipt.FinishedAt = s.Now()
		if err = s.Store.Save("receipts", receipt.ID, receipt); err != nil {
			return err
		}
		if err = s.audit(ctx, core.AuditEvent{Type: "provisioning", ProvisioningID: receipt.ID, ApprovalID: receipt.ApprovalID, CharterDigest: receipt.CharterDigest, Outcome: "failed", Reason: "interrupted_provisioning_recovered"}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) PreviewSession(ctx context.Context, agent string, rev uint64, requested string, env core.Environment) (core.Mandate, core.Decision, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Mandate{}, core.Decision{}, err
	}
	return s.PreviewSessionAs(ctx, sub, agent, rev, requested, env)
}
func (s *Service) PreviewSessionAs(ctx context.Context, sub core.Subject, agent string, rev uint64, requested string, env core.Environment) (core.Mandate, core.Decision, error) {
	c, err := s.GetCharter(agent, rev)
	if err != nil {
		return core.Mandate{}, core.Decision{}, err
	}
	if !s.hasVerifiedReceipt(c.Digest) {
		return core.Mandate{}, core.Decision{}, fmt.Errorf("%w: charter has no verified provisioning receipt", ErrDenied)
	}
	d, err := s.Select(c, sub, requested, env)
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent, StanzaID: requested, CharterRevision: c.Charter.Revision, CharterDigest: c.Digest, Outcome: "deny", Reason: d.Reason})
		return core.Mandate{}, d, err
	}
	rt, err := s.Runtime(ctx)
	if err != nil {
		return core.Mandate{}, d, err
	}
	if err = runtimeSatisfies(rt.Version, c.Charter.Runtime.VersionConstraint); err != nil {
		return core.Mandate{}, d, err
	}
	st := *d.Selected
	toolsets, err := effectiveTools(st)
	if err != nil {
		return core.Mandate{}, d, err
	}
	now := s.Now()
	lifetime := time.Duration(st.Session.MaximumLifetimeSec) * time.Second
	expires := now.Add(lifetime)
	if sub.ExpiresAt.Before(expires) {
		expires = sub.ExpiresAt
	}
	m := core.Mandate{ID: store.ID("mandate"), Subject: sub, AgentID: agent, StanzaID: st.ID, CharterRevision: c.Charter.Revision, CharterDigest: c.Digest, Runtime: rt, Target: c.Charter.Runtime.Target, DeploymentID: s.Config.Credentials.Authority.DeploymentID, Environment: env, Capabilities: append([]string(nil), st.Grant.Capabilities...), Tools: append([]string(nil), st.Grant.Tools...), Scopes: st.Scopes, Hermes: st.Hermes, IssuedAt: now, ExpiresAt: expires}
	m.Hermes.Toolsets = toolsets
	if err = s.Store.Save("mandates", m.ID, m); err != nil {
		return m, d, err
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: agent, StanzaID: st.ID, MandateID: m.ID, CharterRevision: c.Charter.Revision, CharterDigest: c.Digest, Runtime: rt.Runtime, Outcome: "success", Reason: "mandate_issued"})
	return m, d, nil
}
func (s *Service) GetMandate(id string) (core.Mandate, error) {
	var m core.Mandate
	return m, s.Store.Load("mandates", id, &m)
}
func (s *Service) validateMandate(m core.Mandate) error {
	now := s.Now()
	if m.RevokedAt != nil {
		return fmt.Errorf("%w: mandate revoked", ErrDenied)
	}
	if !now.Before(m.ExpiresAt) || !now.Before(m.Subject.ExpiresAt) {
		return ErrExpired
	}
	c, err := s.GetCharter(m.AgentID, m.CharterRevision)
	if err != nil || c.Digest != m.CharterDigest {
		return fmt.Errorf("%w: charter binding is no longer valid", ErrConflict)
	}
	for _, st := range c.Charter.Stanzas {
		if st.ID != m.StanzaID || !st.Enabled {
			continue
		}
		authorizedSubject := contains(st.Authentication.Methods, m.Subject.Method) && !m.IssuedAt.Before(m.Subject.AuthenticatedAt) && m.IssuedAt.Before(m.Subject.ExpiresAt)
		if authorizedSubject {
			authorizedSubject = false
			for _, selector := range st.Authentication.Selectors {
				if selectorMatches(selector, m.Subject, m.Environment) {
					authorizedSubject = true
					break
				}
			}
		}
		if st.Authentication.RequireFresh && m.IssuedAt.Sub(m.Subject.AuthenticatedAt) > time.Duration(st.Authentication.MaxAuthAgeSec)*time.Second {
			authorizedSubject = false
		}
		toolsets, toolErr := effectiveTools(st)
		if toolErr != nil {
			return fmt.Errorf("%w: selected stanza tools are invalid", ErrConflict)
		}
		expectedHermes := st.Hermes
		expectedHermes.Toolsets = toolsets
		maximumExpiry := m.IssuedAt.Add(time.Duration(st.Session.MaximumLifetimeSec) * time.Second)
		if m.Subject.ExpiresAt.Before(maximumExpiry) {
			maximumExpiry = m.Subject.ExpiresAt
		}
		if !authorizedSubject || m.ExpiresAt.After(maximumExpiry) || m.Target != c.Charter.Runtime.Target || m.Environment != (core.Environment{Name: "local"}) || m.DeploymentID != s.Config.Credentials.Authority.DeploymentID ||
			core.Digest(m.Capabilities) != core.Digest(st.Grant.Capabilities) || core.Digest(m.Tools) != core.Digest(st.Grant.Tools) || core.Digest(m.Scopes) != core.Digest(st.Scopes) || core.Digest(m.Hermes) != core.Digest(expectedHermes) {
			return fmt.Errorf("%w: mandate authority does not exactly match selected stanza", ErrConflict)
		}
		return nil
	}
	return fmt.Errorf("%w: stanza disabled or absent", ErrDenied)
}
func (s *Service) StartSession(ctx context.Context, mandateID string) (core.Session, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.Session{}, err
	}
	return s.StartSessionAs(ctx, sub, mandateID)
}
func (s *Service) StartSessionAs(ctx context.Context, sub core.Subject, mandateID string) (core.Session, error) {
	m, err := s.GetMandate(mandateID)
	if err != nil {
		return core.Session{}, err
	}
	if sub.ID != m.Subject.ID {
		return core.Session{}, fmt.Errorf("%w: mandate belongs to another subject", ErrDenied)
	}
	if err = s.validateMandate(m); err != nil {
		return core.Session{}, err
	}
	credentials, err := s.resolveProviderCredential(m.Hermes.Provider, m.Scopes.Credentials)
	if err != nil {
		return core.Session{}, err
	}
	wantsBrokerTool := contains(m.Hermes.Toolsets, "aegis")
	hasBrokerAuthority := contains(m.Capabilities, broker.ActionGitHubGetRepository) && contains(m.Scopes.Credentials, broker.GitHubScope)
	brokerAvailable := s.CredentialAuthority != nil && s.Config.Credentials.Authority.Broker.Socket != ""
	if wantsBrokerTool != hasBrokerAuthority || wantsBrokerTool != brokerAvailable {
		return core.Session{}, fmt.Errorf("%w: Aegis broker tool, capability, credential scope, and configured authority must match exactly", ErrDenied)
	}
	bridge := hermes.BrokerBridge{}
	if wantsBrokerTool {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return core.Session{}, errors.New("resolve Aegis credential bridge executable")
		}
		bridge = hermes.BrokerBridge{Enabled: true, Executable: executable, Timeout: s.Config.Credentials.Authority.Broker.Timeout}
	}
	id, home, pid, launchedToolsets, err := s.Hermes.Launch(ctx, s.Store.Root(), m, credentials, bridge)
	if err != nil {
		return core.Session{}, err
	}
	wantToolsets, gotToolsets := append([]string(nil), m.Hermes.Toolsets...), append([]string(nil), launchedToolsets...)
	sort.Strings(wantToolsets)
	sort.Strings(gotToolsets)
	if strings.Join(wantToolsets, "\x00") != strings.Join(gotToolsets, "\x00") {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Hermes.Terminate(stop, id, true)
		return core.Session{}, errors.New("launched Hermes toolset arguments do not match the approved mandate")
	}
	verification := "launch_arguments"
	if bridge.Enabled {
		verification = "exact_registered_aegis_bridge_tool"
	}
	sess := core.Session{ID: store.ID("session"), Mandate: m, RuntimeSessionID: id, RuntimePID: pid, ProcessStart: processStartToken(pid), RuntimeHome: home, VerifiedToolsets: launchedToolsets, ToolsetVerification: verification, Status: "running", StartedAt: s.Now()}
	if sess.ProcessStart == "" {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Hermes.Terminate(stop, id, !s.Config.Retention.SessionHomes)
		return sess, errors.New("cannot establish Hermes process identity")
	}
	if err = s.Store.Save("sessions", sess.ID, sess); err != nil {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Hermes.Terminate(stop, id, !s.Config.Retention.SessionHomes)
		return sess, err
	}
	if s.CredentialAuthority != nil && s.Config.Credentials.Authority.Broker.Socket != "" {
		if err = s.issueBrokerCapability(&sess); err != nil {
			_ = s.endSession(context.Background(), sess.ID, "failed", "broker_capability_materialization_failed", true)
			return sess, err
		}
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "session", SubjectID: m.Subject.ID, PrincipalID: m.Subject.PrincipalID, AgentID: m.AgentID, StanzaID: m.StanzaID, SessionID: sess.ID, MandateID: m.ID, Runtime: m.Runtime.Runtime, CharterRevision: m.CharterRevision, CharterDigest: m.CharterDigest, Outcome: "running", Reason: "clean_runtime_started", Metadata: map[string]string{"pid": strconv.Itoa(pid)}})
	return sess, nil
}
func (s *Service) GetSession(id string) (core.Session, error) {
	var x core.Session
	return x, s.Store.Load("sessions", id, &x)
}
func (s *Service) ListSessions() ([]core.Session, error) {
	var out []core.Session
	err := s.Store.List("sessions", func(b json.RawMessage) error {
		var x core.Session
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		out = append(out, x)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, err
}

func (s *Service) AuditEvents(ctx context.Context) ([]core.AuditEvent, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return s.AuditEventsAs(subject)
}

func (s *Service) AuditEventsAs(subject core.Subject) ([]core.AuditEvent, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return nil, err
	}
	return s.Audit.AuditEvents()
}

func (s *Service) VerifyAudit(ctx context.Context) error {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return err
	}
	return s.VerifyAuditAs(subject)
}

func (s *Service) VerifyAuditAs(subject core.Subject) error {
	if err := s.requirePrincipal(subject); err != nil {
		return err
	}
	return s.Audit.VerifyAudit()
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}

func processStartToken(pid int) string {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return ""
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return ""
	}
	fields := strings.Fields(string(b[end+1:]))
	// /proc/<pid>/stat field 22 is starttime; fields[0] here is field 3.
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

func processMatches(pid int, token string) bool {
	return token != "" && processAlive(pid) && processStartToken(pid) == token
}
func (s *Service) InspectSession(id string) (core.Session, bool, error) {
	x, err := s.GetSession(id)
	if err != nil {
		return x, false, err
	}
	return x, processMatches(x.RuntimePID, x.ProcessStart), nil
}
func (s *Service) endSession(ctx context.Context, id, status, reason string, revoke bool) error {
	var sess core.Session
	if err := s.Store.Update("sessions", id, func(b []byte) (any, error) {
		if err := json.Unmarshal(b, &sess); err != nil {
			return nil, err
		}
		if sess.Status != "running" {
			return nil, fmt.Errorf("%w: session is %s", ErrConflict, sess.Status)
		}
		now := s.Now()
		sess.Status = status
		sess.EndedAt = &now
		sess.EndReason = reason
		return sess, nil
	}); err != nil {
		return err
	}
	if revoke {
		var m core.Mandate
		_ = s.Store.Update("mandates", sess.Mandate.ID, func(b []byte) (any, error) {
			if err := json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			now := s.Now()
			m.RevokedAt = &now
			m.RevocationReason = reason
			return m, nil
		})
	}
	s.revokeBrokerCapabilities(sess.ID)
	stop, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = s.Hermes.Terminate(stop, sess.RuntimeSessionID, !s.Config.Retention.SessionHomes)
	if processMatches(sess.RuntimePID, sess.ProcessStart) {
		_ = syscall.Kill(-sess.RuntimePID, syscall.SIGTERM)
		select {
		case <-time.After(250 * time.Millisecond):
			if processMatches(sess.RuntimePID, sess.ProcessStart) {
				_ = syscall.Kill(-sess.RuntimePID, syscall.SIGKILL)
			}
		case <-ctx.Done():
		}
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "session", SubjectID: sess.Mandate.Subject.ID, PrincipalID: sess.Mandate.Subject.PrincipalID, AgentID: sess.Mandate.AgentID, StanzaID: sess.Mandate.StanzaID, SessionID: sess.ID, MandateID: sess.Mandate.ID, Runtime: sess.Mandate.Runtime.Runtime, CharterRevision: sess.Mandate.CharterRevision, CharterDigest: sess.Mandate.CharterDigest, Outcome: status, Reason: reason})
	return nil
}
func (s *Service) RevokeSession(ctx context.Context, id, reason string) error {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return err
	}
	return s.RevokeSessionAs(ctx, sub, id, reason)
}
func (s *Service) RevokeSessionAs(ctx context.Context, sub core.Subject, id, reason string) error {
	if err := s.requirePrincipal(sub); err != nil {
		return err
	}
	return s.endSession(ctx, id, "revoked", reason, true)
}
func (s *Service) TerminateSession(ctx context.Context, id, reason string) error {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return err
	}
	return s.TerminateSessionAs(ctx, sub, id, reason)
}
func (s *Service) TerminateSessionAs(ctx context.Context, sub core.Subject, id, reason string) error {
	sess, err := s.GetSession(id)
	if err != nil {
		return err
	}
	if sub.ID != sess.Mandate.Subject.ID && sub.PrincipalID != s.Config.Principal.ID {
		return fmt.Errorf("%w: only the session subject or principal may terminate", ErrDenied)
	}
	return s.endSession(ctx, id, "terminated", reason, false)
}

// Supervise reconciles durable running sessions, schedules expiry, and owns
// shutdown cleanup while the foreground control plane is alive.
func (s *Service) Supervise(ctx context.Context) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.reconcileSessions(ctx, false); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return s.reconcileSessions(shutdown, true)
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileSessions(ctx context.Context, shutdown bool) error {
	sessions, err := s.ListSessions()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.Status != "running" {
			continue
		}
		status, reason := "", ""
		switch {
		case shutdown:
			status, reason = "terminated", "control_plane_shutdown"
		case !s.Now().Before(session.Mandate.ExpiresAt) || !s.Now().Before(session.Mandate.Subject.ExpiresAt):
			status, reason = "expired", "mandate_expired"
		case s.validateMandate(session.Mandate) != nil:
			status, reason = "failed", "mandate_binding_invalid"
		case !processMatches(session.RuntimePID, session.ProcessStart):
			status, reason = "failed", "runtime_process_missing_or_reused"
		}
		if status != "" {
			if err = s.endSession(ctx, session.ID, status, reason, status == "expired"); err != nil && !errors.Is(err, ErrConflict) {
				return err
			}
		}
	}
	return nil
}

// DesignSmoke drives Hermes's documented TUI-gateway stdio protocol in a
// disposable home. Hermes returns proposal bytes; only Aegis can validate,
// canonicalize, digest, and persist them.
func (s *Service) DesignSmoke(ctx context.Context, requirements []byte) (core.CanonicalCharter, error) {
	sub, err := s.Authenticate(ctx)
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	return s.DesignSmokeAs(ctx, sub, requirements)
}
func (s *Service) DesignSmokeAs(ctx context.Context, sub core.Subject, requirements []byte) (core.CanonicalCharter, error) {
	var err error
	if err = s.requirePrincipal(sub); err != nil {
		return core.CanonicalCharter{}, err
	}
	rt, err := s.Runtime(ctx)
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	if len(strings.TrimSpace(string(requirements))) == 0 {
		return core.CanonicalCharter{}, errors.New("design requirements are required")
	}
	credentials, err := s.resolveDesignCredential()
	if err != nil {
		return core.CanonicalCharter{}, err
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "design_session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Runtime: rt.Runtime, Outcome: "created", Reason: "design_session_created"}); err != nil {
		return core.CanonicalCharter{}, err
	}
	proposal, _, err := s.Hermes.DesignProposal(ctx, s.Store.Root(), string(requirements), s.Config.Retention.DesignHomes, credentials)
	if err != nil {
		_ = s.audit(ctx, core.AuditEvent{Type: "design_session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Runtime: rt.Runtime, Outcome: "failure", Reason: "design_protocol_failed"})
		return core.CanonicalCharter{}, err
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "design_session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Runtime: rt.Runtime, Outcome: "proposal", Reason: "structured_proposal_received"})
	charter, err := s.ImportCharterAs(ctx, sub, []byte(proposal))
	outcome, reason := "closed", "proposal_validated_and_design_closed"
	if err != nil {
		outcome, reason = "failure", "proposal_validation_failed"
	}
	_ = s.audit(ctx, core.AuditEvent{Type: "design_session", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Runtime: rt.Runtime, Outcome: outcome, Reason: reason})
	return charter, err
}
