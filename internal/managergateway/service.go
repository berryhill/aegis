package managergateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
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
}

type session struct {
	id       string
	token    [32]byte
	subject  core.Subject
	issuedAt time.Time
	expires  time.Time
}

type Service struct {
	app      *app.Service
	registry *slash.Registry
	commands *slash.Service
	now      func() time.Time
	mu       sync.Mutex
	sessions map[string]session
}

func New(application *app.Service) (*Service, error) {
	if application == nil {
		return nil, errors.New("manager gateway requires an application service")
	}
	registry, err := slash.NewRegistry()
	if err != nil {
		return nil, err
	}
	return &Service{app: application, registry: registry, commands: slash.NewService(application, registry), now: application.Now, sessions: make(map[string]session)}, nil
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
	entry := session{id: id, token: sha256.Sum256([]byte(token)), subject: subject, issuedAt: now, expires: expires}
	s.mu.Lock()
	s.prune(now)
	s.sessions[id] = entry
	s.mu.Unlock()
	if err = s.app.AuditManagerStartup(ctx, subject, "ok", "gateway_manager_session_issued", map[string]string{"route": "authenticated-unix-gateway"}); err != nil {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		return Opened{}, err
	}
	return Opened{ID: id, Token: token, ExpiresAt: expires}, nil
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
		MandateState: "active", Lifecycle: slash.Active, RuntimeState: "gateway-owned",
		Route: "authenticated-unix-gateway", PolicyVersion: managerdomain.PolicyVersion,
		PolicyDigest: managerdomain.PolicyDigest(),
		Readiness:    map[string]string{"authority": "gateway-owned", "runtime": "gateway-owned"},
	}
	return s.commands.Execute(ctx, manager, request)
}

func (s *Service) Close(ctx context.Context, subject core.Subject, id, token string) error {
	entry, err := s.authenticate(subject, id, token)
	if err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
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
		if !now.Before(entry.expires) || !now.Before(entry.subject.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}
