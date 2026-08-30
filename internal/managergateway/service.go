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
	return &Service{app: application, registry: registry, commands: slash.NewService(application, registry), now: application.Now, profileHome: localHermesHome, ctx: parent, sessions: make(map[string]session)}, nil
}

func opaque(size int) (string, error) {
	material := make([]byte, size)
	if _, err := rand.Read(material); err != nil {
		return "", err
	}
	return hex.EncodeToString(material), nil
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
	expires := now.Add(s.app.Config.API.Console.SessionTTL)
	if subject.ExpiresAt.Before(expires) {
		expires = subject.ExpiresAt
	}
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
			_ = runtime.Close(cleanup, managerdomain.EndSessionExpired)
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
			_ = runtime.Close(cleanup, managerdomain.EndStartupFailed)
		}
		return Opened{}, err
	}
	go s.watchSession(entry)
	return Opened{ID: id, Token: token, ExpiresAt: expires, Mode: mode, Reason: reason, NextStep: nextStep}, nil
}

func (s *Service) hasActiveConversation(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return result
}

func (s *Service) watchSession(entry session) {
	timer := time.NewTimer(max(time.Until(entry.expires), 0))
	defer timer.Stop()
	reason := managerdomain.EndSessionExpired
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
			reason = managerdomain.EndRuntimeFailed
		}
	}
	if entry.runtime != nil {
		s.conversationMu.Lock()
		defer s.conversationMu.Unlock()
	}
	s.mu.Lock()
	current, exists := s.sessions[entry.id]
	if exists && current.token == entry.token {
		delete(s.sessions, entry.id)
	}
	s.mu.Unlock()
	if !exists || current.token != entry.token || entry.runtime == nil {
		return
	}
	cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
	defer cancel()
	_ = entry.runtime.Close(cleanup, reason)
}

func (s *Service) Execute(ctx context.Context, subject core.Subject, id, token, input string) (slash.Result, error) {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return slash.Result{}, err
	}
	request, err := s.registry.Parse(input)
	if err != nil {
		return slash.Result{}, err
	}
	manager := slash.Context{
		Subject: entry.subject, StanzaID: managerdomain.SecurityContext,
		MandateID: entry.id, MandateIssued: entry.issuedAt, MandateExpiry: entry.expires,
		MandateState: "active", Lifecycle: lifecycleState(entry.mode), RuntimeState: entry.mode,
		Route: "authenticated-unix-gateway", PolicyVersion: managerdomain.PolicyVersion,
		PolicyDigest: managerdomain.PolicyDigest(),
		Readiness:    map[string]string{"authority": "gateway-owned", "runtime": entry.mode, "reason": entry.reason, "next_step": entry.nextStep},
	}
	return s.commands.Execute(ctx, manager, request)
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
	detection := slash.Detect(input)
	if strings.TrimSpace(input) == "" || detection == slash.Command {
		return TurnResult{}, errors.New("ordinary manager turn required")
	}
	if detection == slash.LiteralSlash {
		input = slash.UnescapeLiteral(input)
	}
	if intent := localProfileIntent(input); intent != "" {
		home, homeErr := s.profileHome(s.app.Config.Principal.User, s.app.Config.Principal.UID)
		if homeErr != nil {
			return TurnResult{}, fmt.Errorf("local Hermes profile discovery denied: %w", homeErr)
		}
		profiles, discoverErr := discoverHermesProfiles(home, s.app.Config.Principal.UID)
		if discoverErr != nil {
			return TurnResult{}, fmt.Errorf("local Hermes profile discovery denied: %w", discoverErr)
		}
		result := TurnResult{Kind: "hermes_profile_inventory", Origin: TurnOriginAuthoritative, Message: renderProfileInventory(profiles), Data: map[string]any{"profiles": profiles, "model_bypassed": true}}
		if intent == "register_default" {
			result.Kind = "hermes_profile_registration_prerequisites"
			result.Message = renderProfileInventory(profiles) + "\n\nSelected runtime source: profile/default. Registration was not performed: a Hermes profile is runtime provenance, not identity or authority. Aegis requires an exact imported charter and fleet/source binding before it can render the immutable registration digest. Use /agents readiness, then the authenticated /agents prepare transaction."
			result.Data["selected_profile"] = "default"
			result.Data["registered"] = false
		}
		return result, nil
	}
	if entry.mode != "conversational" || entry.runtime == nil {
		return TurnResult{}, fmt.Errorf("%s: conversational local inference unavailable; next: %s", entry.reason, entry.nextStep)
	}
	turnCtx, cancel := managerTurnContext(ctx, s.app.Config.Manager.Hermes.TurnTimeout)
	defer cancel()
	message, err := entry.runtime.Turn(turnCtx, input)
	if err != nil {
		return TurnResult{}, err
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
	if entry.runtime != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), s.app.Config.Manager.CleanupTimeout)
		defer cancel()
		if closeErr := entry.runtime.Close(cleanup, managerdomain.EndUserExit); closeErr != nil {
			return closeErr
		}
	}
	return s.app.AuditManagerSession(ctx, entry.subject, "ok", "gateway_client_exit", map[string]string{"route": "authenticated-unix-gateway"})
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
	for _, reason := range []string{managerdomain.ReasonModelAbsent, managerdomain.ReasonDigestMismatch, managerdomain.ReasonNotCertified, managerdomain.ReasonAuthorityUnavailable, managerdomain.ReasonAuthorityInvalid, managerdomain.ReasonRuntimeUnsupported, managerdomain.ReasonContextUnsupported, managerdomain.ReasonOllamaUnavailable, managerdomain.ReasonModelLoadFailed, managerdomain.ReasonRouteMismatch, managerdomain.ReasonGatewayProtocol} {
		if strings.Contains(err.Error(), reason) {
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
