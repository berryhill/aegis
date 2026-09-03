package managergateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/slash"
)

const SessionHeader = "X-Aegis-Manager-Session"

// CommandParseError preserves safe registry-derived usage information across
// the managergateway boundary without requiring transport packages to import
// the slash implementation layer.
type CommandParseError struct {
	Reason  string
	Message string
}

func (e *CommandParseError) Error() string { return e.Message }

type Opened struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires"`
	Mode      string    `json:"mode"`
	Reason    string    `json:"reason,omitempty"`
	NextStep  string    `json:"next_step,omitempty"`
}

type session struct {
	id       string
	token    [32]byte
	subject  core.Subject
	issuedAt time.Time
	expires  time.Time
	mode     string
	reason   string
	nextStep string
	runtime  *conversation
}

type Service struct {
	app            *app.Service
	registry       *slash.Registry
	commands       *slash.Service
	now            func() time.Time
	profileHome    func(string, string) (string, error)
	ctx            context.Context
	conversationMu sync.Mutex
	mu             sync.Mutex
	sessions       map[string]session
	cleanupErr     error
	readOperations func(core.Subject) managerdomain.Operations
}

func New(parent context.Context, application *app.Service) (*Service, error) {
	if application == nil {
		return nil, errors.New("manager gateway requires an application service")
	}
	if parent == nil {
		return nil, errors.New("manager gateway requires a lifecycle context")
	}
	registry, err := slash.NewRegistry()
	if err != nil {
		return nil, err
	}
	return &Service{app: application, registry: registry, commands: slash.NewService(application, registry), now: application.Now, profileHome: localHermesHome, ctx: parent, sessions: make(map[string]session), readOperations: func(subject core.Subject) managerdomain.Operations {
		return gatewayOperations{service: application, subject: subject}
	}}, nil
}

func opaque(size int) (string, error) {
	material := make([]byte, size)
	if _, err := rand.Read(material); err != nil {
		return "", err
	}
	return hex.EncodeToString(material), nil
}

func managerSessionExpiry(_ time.Time, subject core.Subject) time.Time {
	return subject.ExpiresAt
}

func managerSessionContext(parent context.Context, expires time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, expires)
}

func (s *Service) Open(ctx context.Context, subject core.Subject) (Opened, error) {
	if err := s.app.RequirePrincipal(subject); err != nil {
		return Opened{}, err
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	admissionNow := s.now()
	if err := s.closeExpiredConversations(admissionNow); err != nil {
		return Opened{}, fmt.Errorf("retire expired manager conversation: %w", err)
	}
	if s.hasActiveConversation(admissionNow) {
		return Opened{}, fmt.Errorf("%w: manager_conversation_already_active", app.ErrDenied)
	}
	idMaterial, err := opaque(12)
	if err != nil {
		return Opened{}, fmt.Errorf("generate manager session id: %w", err)
	}
	token, err := opaque(32)
	if err != nil {
		return Opened{}, fmt.Errorf("generate manager session token: %w", err)
	}
	now := s.now()
	expires := managerSessionExpiry(now, subject)
	if !now.Before(expires) {
		return Opened{}, app.ErrExpired
	}
	id := "mgr-" + idMaterial
	runtime, runtimeErr := startConversation(ctx, s.ctx, s.app, subject, id, expires)
	mode, reason, nextStep := "conversational", "", ""
	if runtimeErr != nil {
		mode, reason = "degraded", managerReason(runtimeErr)
		nextStep = managerNextStep(reason)
	}
	if !s.now().Before(expires) {
		if runtime != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
			defer cancel()
			closeErr := runtime.Close(cleanup, managerdomain.EndSessionExpired)
			s.recordCleanupFailure(closeErr)
		}
		return Opened{}, app.ErrExpired
	}
	entry := session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: now, expires: expires, mode: mode, reason: reason, nextStep: nextStep, runtime: runtime}
	s.mu.Lock()
	s.prune(now)
	s.sessions[id] = entry
	s.mu.Unlock()
	outcome, auditReason := "ok", "manager_ready"
	if mode == "degraded" {
		outcome, auditReason = "degraded", reason
	}
	if err = s.app.AuditManagerStartup(ctx, subject, outcome, auditReason, map[string]string{"route": "authenticated-unix-gateway", "runtime": "hermes", "model": s.app.Config.Manager.Inference.Model}); err != nil {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		if runtime != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
			defer cancel()
			closeErr := runtime.Close(cleanup, managerdomain.EndStartupFailed)
			s.recordCleanupFailure(closeErr)
		}
		return Opened{}, err
	}
	go s.watchSession(entry)
	return Opened{ID: id, Token: token, ExpiresAt: expires, Mode: mode, Reason: reason, NextStep: nextStep}, nil
}

func (s *Service) hasActiveConversation(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cleanupErr != nil {
		return true
	}
	for _, entry := range s.sessions {
		if entry.runtime != nil && now.Before(entry.expires) && now.Before(entry.subject.ExpiresAt) {
			return true
		}
	}
	return false
}

func (s *Service) closeExpiredConversations(now time.Time) error {
	var expired []*conversation
	s.mu.Lock()
	for id, entry := range s.sessions {
		if entry.runtime != nil && (!now.Before(entry.expires) || !now.Before(entry.subject.ExpiresAt)) {
			delete(s.sessions, id)
			expired = append(expired, entry.runtime)
		}
	}
	s.mu.Unlock()
	var result error
	for _, runtime := range expired {
		cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
		result = errors.Join(result, runtime.Close(cleanup, managerdomain.EndSessionExpired))
		cancel()
	}
	s.recordCleanupFailure(result)
	return result
}

func (s *Service) watchSession(entry session) {
	timer := time.NewTimer(max(time.Until(entry.expires), 0))
	defer timer.Stop()
	reason := managerdomain.EndSessionExpired
	runtimeFailed := false
	if entry.runtime == nil {
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			reason = managerdomain.EndTermination
		}
	} else {
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			reason = managerdomain.EndTermination
		case <-entry.runtime.failures:
			select {
			case <-timer.C:
				reason = managerdomain.EndSessionExpired
			default:
				if !time.Now().Before(entry.expires) {
					reason = managerdomain.EndSessionExpired
				} else {
					reason = managerdomain.EndRuntimeFailed
					runtimeFailed = true
				}
			}
		}
	}
	if runtimeFailed {
		s.degradeRuntime(entry)
		return
	}
	if entry.runtime != nil {
		s.conversationMu.Lock()
		defer s.conversationMu.Unlock()
		cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
		defer cancel()
		closeErr := entry.runtime.Close(cleanup, reason)
		s.recordCleanupFailure(closeErr)
	}
	s.mu.Lock()
	current, exists := s.sessions[entry.id]
	if exists && current.token == entry.token {
		delete(s.sessions, entry.id)
	}
	s.mu.Unlock()
}

func (s *Service) recordCleanupFailure(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.cleanupErr = errors.Join(s.cleanupErr, err)
	s.mu.Unlock()
}

func (s *Service) degradeRuntime(entry session) {
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	s.mu.Lock()
	current, exists := s.sessions[entry.id]
	if exists && current.token == entry.token {
		current.mode = "degraded"
		current.reason = managerdomain.ReasonRuntimeFailed
		current.nextStep = managerNextStep(current.reason)
		current.runtime = nil
		s.sessions[entry.id] = current
	}
	s.mu.Unlock()
	if !exists || current.token != entry.token || entry.runtime == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
	defer cancel()
	closeErr := entry.runtime.Close(cleanup, managerdomain.EndRuntimeFailed)
	s.recordCleanupFailure(closeErr)
}

func (s *Service) Execute(ctx context.Context, subject core.Subject, id, token, input string) (slash.Result, error) {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return slash.Result{}, err
	}
	ctx, cancel := managerSessionContext(ctx, entry.expires)
	defer cancel()
	request, err := s.registry.Parse(input)
	if err != nil {
		var parseError *slash.ParseError
		if errors.As(err, &parseError) {
			message := "invalid manager slash command; use /help to list available commands"
			if parseError.Reason == "usage" {
				// Grammar usage is sourced from the closed command registry. Other
				// parse messages may contain caller-controlled command names and
				// must not cross the API or logging boundary.
				message = parseError.Message
			}
			return slash.Result{}, &CommandParseError{Reason: parseError.Reason, Message: message}
		}
		return slash.Result{}, err
	}
	manager := slash.Context{
		Subject: entry.subject, StanzaID: managerdomain.SecurityContext,
		MandateID: entry.id, MandateIssued: entry.issuedAt, MandateExpiry: entry.expires,
		MandateState: "active", Lifecycle: lifecycleState(entry.mode), RuntimeState: entry.mode,
		Route: "authenticated-unix-gateway", PolicyVersion: managerdomain.PolicyVersion,
		PolicyDigest: managerdomain.PolicyDigest(),
		Readiness:    managerReadiness(entry, s.app != nil && s.app.CredentialAuthority != nil),
	}
	return s.commands.Execute(ctx, manager, request)
}

func managerReadiness(entry session, authorityOpen bool) map[string]string {
	expertise := managerdomain.PlatformExpertise()
	authority := "unavailable"
	if authorityOpen {
		authority = "ready"
	}
	switch entry.reason {
	case managerdomain.ReasonAuthorityUnavailable:
		authority = "unavailable"
	case managerdomain.ReasonAuthorityInvalid:
		authority = "invalid"
	}
	return map[string]string{
		"authority": authority, "runtime": entry.mode, "reason": entry.reason, "next_step": entry.nextStep,
		"expertise_version": expertise.Version, "expertise_digest": expertise.Digest,
		"agent_registry": "/agents readiness", "credential_creation": "protected intake required",
	}
}

func lifecycleState(mode string) slash.LifecycleState {
	if mode == "conversational" {
		return slash.Active
	}
	return slash.Degraded
}

func managerTurnContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func (s *Service) Turn(ctx context.Context, subject core.Subject, id, token, input string) (TurnResult, error) {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return TurnResult{}, err
	}
	ctx, cancel := managerSessionContext(ctx, entry.expires)
	defer cancel()
	detection := slash.Detect(input)
	if strings.TrimSpace(input) == "" || detection == slash.Command {
		return TurnResult{}, errors.New("ordinary manager turn required")
	}
	if detection == slash.LiteralSlash {
		input = slash.UnescapeLiteral(input)
	}
	if localProfileIntent(input) == "register_default" {
		proposal, prepareErr := s.app.PrepareLocalHermesAgentImportAs(ctx, entry.subject)
		if prepareErr != nil {
			return TurnResult{}, fmt.Errorf("local Hermes Agent import denied: %w", prepareErr)
		}
		message := fmt.Sprintf("Prepared a non-authorizing import review for the owner-verified local Hermes default profile. The Agent remains disabled, registration does not activate execution, and profile provenance is not identity or authority. Review digest %s, then confirm only with the exact command:\n%s", proposal.RevisionDigest, proposal.Confirmation)
		return TurnResult{Kind: "local_hermes_agent_import_prepared", Origin: TurnOriginAuthoritative, Message: message, Data: map[string]any{"proposal": proposal, "confirmation": proposal.Confirmation, "selected_profile": "default", "model_bypassed": true, "registered": false, "activation": false}}, nil
	}
	if result, handled, dispatchErr := DispatchDeterministicAegisTurn(ctx, s.app, entry.subject, input); handled {
		return result, dispatchErr
	}
	route := detectAuthoritativeIntent(input)
	defer route.Wipe()
	switch route.kind {
	case intentCredentialCreate:
		return credentialCreationGuidance(route.credential, route.credentialParsed), nil
	case intentCredentialIntake:
		return credentialIntakeGuidance(), nil
	}
	if managerdomain.IsDeterministicCredentialRead(input) {
		operations := managerdomain.Operations(gatewayOperations{service: s.app, subject: entry.subject})
		if s.readOperations != nil {
			operations = s.readOperations(entry.subject)
		}
		read, handled, readErr := managerdomain.DispatchCredentialRead(ctx, operations, input)
		if !handled {
			return TurnResult{}, errors.New(managerdomain.ReasonProposalInvalid)
		}
		if readErr != nil {
			return TurnResult{}, readErr
		}
		return TurnResult{
			Kind:      read.Kind,
			Origin:    TurnOriginAuthoritative,
			Message:   read.Message,
			Sensitive: read.Sensitive,
			Data: map[string]any{
				"model_bypassed": true,
				"operation":      read.Kind,
			},
		}, nil
	}
	if intent := localProfileIntent(input); intent == "inventory" {
		home, homeErr := s.profileHome(s.app.Config.Principal.User, s.app.Config.Principal.UID)
		if homeErr != nil {
			return TurnResult{}, fmt.Errorf("local Hermes profile discovery denied: %w", homeErr)
		}
		profiles, discoverErr := discoverHermesProfiles(home, s.app.Config.Principal.UID)
		if discoverErr != nil {
			return TurnResult{}, fmt.Errorf("local Hermes profile discovery denied: %w", discoverErr)
		}
		result := TurnResult{Kind: "hermes_profile_inventory", Origin: TurnOriginAuthoritative, Message: renderProfileInventory(profiles), Data: map[string]any{"profiles": profiles, "model_bypassed": true}}
		return result, nil
	}
	if entry.mode != "conversational" || entry.runtime == nil {
		return TurnResult{}, turnFailureForReason(entry.reason)
	}
	turnCtx, cancel := managerTurnContext(ctx, s.app.Config.Manager.Hermes.TurnTimeout)
	defer cancel()
	message, err := entry.runtime.Turn(turnCtx, input)
	if err != nil {
		if !s.now().Before(entry.expires) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return TurnResult{}, app.ErrExpired
		}
		s.degradeRuntime(entry)
		return TurnResult{}, normalizeTurnFailure(err)
	}
	return TurnResult{Kind: "message", Origin: TurnOriginModel, Message: message}, nil
}

func (s *Service) Close(ctx context.Context, subject core.Subject, id, token string) error {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return err
	}
	if entry.runtime != nil {
		s.conversationMu.Lock()
		defer s.conversationMu.Unlock()
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
	defer cancel()
	var result error
	if entry.runtime != nil {
		runtimeErr := entry.runtime.Close(cleanup, managerdomain.EndUserExit)
		s.recordCleanupFailure(runtimeErr)
		result = errors.Join(result, runtimeErr)
	}
	result = errors.Join(result, s.app.AuditManagerSession(cleanup, entry.subject, "ok", "gateway_client_exit", map[string]string{"route": "authenticated-unix-gateway"}))
	return result
}

func (s *Service) authenticate(subject core.Subject, id, token string) (session, error) {
	if id == "" || token == "" {
		return session{}, app.ErrUnauthenticated
	}
	now := s.now()
	provided := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	entry, ok := s.sessions[id]
	if !ok || subtle.ConstantTimeCompare(entry.token[:], provided[:]) != 1 || entry.subject.ID != subject.ID || entry.subject.PrincipalID != subject.PrincipalID || !now.Before(entry.expires) || !now.Before(subject.ExpiresAt) {
		return session{}, app.ErrUnauthenticated
	}
	return entry, nil
}

func (s *Service) prune(now time.Time) {
	for id, entry := range s.sessions {
		if (!now.Before(entry.expires) || !now.Before(entry.subject.ExpiresAt)) && entry.runtime == nil {
			delete(s.sessions, id)
		}
	}
}

func managerReason(err error) string {
	if reason, ok := startupFailureReason(err); ok {
		return reason
	}
	for _, reason := range []string{managerdomain.ReasonModelAbsent, managerdomain.ReasonDigestMismatch, managerdomain.ReasonNotCertified, managerdomain.ReasonAuthorityUnavailable, managerdomain.ReasonAuthorityInvalid, managerdomain.ReasonRuntimeUnsupported, managerdomain.ReasonContextUnsupported, managerdomain.ReasonOllamaUnavailable, managerdomain.ReasonModelLoadFailed, managerdomain.ReasonRouteMismatch, managerdomain.ReasonGatewayProtocol} {
		if err != nil && err.Error() == reason {
			return reason
		}
	}
	return managerdomain.ReasonRuntimeUnsupported
}

func managerNextStep(reason string) string {
	switch reason {
	case managerdomain.ReasonModelAbsent:
		return "aegis manager model candidates"
	case managerdomain.ReasonNotCertified:
		return "aegis manager model status"
	default:
		return "aegis manager model status"
	}
}
