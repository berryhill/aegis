package console

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
)

var (
	ErrUnauthenticated = errors.New("console authentication failed")
	ErrDenied          = errors.New("console request denied")
	ErrInvalidInput    = errors.New("invalid console input")
)

const CookieName = "aegis-console"

type Config struct {
	Origin       string
	SessionTTL   time.Duration
	BootstrapTTL time.Duration
	MaxPageSize  int
}

type bootstrap struct {
	subject core.Subject
	expires time.Time
}

type session struct {
	subject  core.Subject
	csrf     string
	csrfHash [32]byte
	expires  time.Time
}

type Manager struct {
	mu         sync.Mutex
	origin     *url.URL
	config     Config
	now        func() time.Time
	bootstraps map[[32]byte]bootstrap
	sessions   map[[32]byte]session
}

func New(config Config, now func() time.Time) (*Manager, error) {
	if now == nil || config.SessionTTL <= 0 || config.SessionTTL > 15*time.Minute || config.BootstrapTTL <= 0 || config.BootstrapTTL > time.Minute || config.MaxPageSize < 1 || config.MaxPageSize > 1000 {
		return nil, fmt.Errorf("%w: console limits must be positive and bounded", ErrInvalidInput)
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || origin.Scheme != "https" && origin.Scheme != "http" {
		return nil, fmt.Errorf("%w: console origin must be an absolute HTTP origin", ErrInvalidInput)
	}
	if origin.Scheme != "https" && !loopbackHost(origin.Hostname()) {
		return nil, fmt.Errorf("%w: plaintext console transport is restricted to loopback", ErrInvalidInput)
	}
	return &Manager{origin: origin, config: config, now: now, bootstraps: make(map[[32]byte]bootstrap), sessions: make(map[[32]byte]session)}, nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func opaque() (string, [32]byte, error) {
	var material [32]byte
	if _, err := rand.Read(material[:]); err != nil {
		return "", [32]byte{}, err
	}
	value := hex.EncodeToString(material[:])
	return value, sha256.Sum256([]byte(value)), nil
}

func (m *Manager) IssueBootstrap(subject core.Subject) (string, error) {
	now := m.now()
	if subject.ID == "" || subject.PrincipalID == "" || !now.Before(subject.ExpiresAt) {
		return "", ErrDenied
	}
	value, digest, err := opaque()
	if err != nil {
		return "", fmt.Errorf("generate console bootstrap: %w", err)
	}
	expires := now.Add(m.config.BootstrapTTL)
	if subject.ExpiresAt.Before(expires) {
		expires = subject.ExpiresAt
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	m.bootstraps[digest] = bootstrap{subject: subject, expires: expires}
	return value, nil
}

func (m *Manager) Exchange(request *http.Request, value string) (string, string, error) {
	if err := m.ValidateOrigin(request, true); err != nil {
		return "", "", err
	}
	now := m.now()
	digest := sha256.Sum256([]byte(value))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	candidate, ok := m.bootstraps[digest]
	delete(m.bootstraps, digest)
	if !ok || value == "" || !now.Before(candidate.expires) || candidate.subject.PrincipalID == "" || !now.Before(candidate.subject.ExpiresAt) {
		return "", "", ErrUnauthenticated
	}
	sessionValue, sessionDigest, err := opaque()
	if err != nil {
		return "", "", fmt.Errorf("generate console session: %w", err)
	}
	csrf, _, err := opaque()
	if err != nil {
		return "", "", fmt.Errorf("generate CSRF value: %w", err)
	}
	expires := now.Add(m.config.SessionTTL)
	if candidate.subject.ExpiresAt.Before(expires) {
		expires = candidate.subject.ExpiresAt
	}
	m.sessions[sessionDigest] = session{subject: candidate.subject, csrf: csrf, csrfHash: sha256.Sum256([]byte(csrf)), expires: expires}
	return sessionValue, csrf, nil
}

func (m *Manager) Authenticate(request *http.Request) (core.Subject, error) {
	if err := m.ValidateOrigin(request, false); err != nil {
		return core.Subject{}, err
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return core.Subject{}, ErrUnauthenticated
	}
	now := m.now()
	digest := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	candidate, ok := m.sessions[digest]
	if !ok || !now.Before(candidate.expires) || !now.Before(candidate.subject.ExpiresAt) || candidate.subject.PrincipalID == "" {
		delete(m.sessions, digest)
		return core.Subject{}, ErrUnauthenticated
	}
	return candidate.subject, nil
}

func (m *Manager) AuthorizeMutation(request *http.Request) (core.Subject, error) {
	subject, err := m.Authenticate(request)
	if err != nil {
		return core.Subject{}, err
	}
	if err = m.ValidateOrigin(request, true); err != nil {
		return core.Subject{}, err
	}
	cookie, _ := request.Cookie(CookieName)
	digest := sha256.Sum256([]byte(cookie.Value))
	provided := sha256.Sum256([]byte(request.Header.Get("X-CSRF-Token")))
	m.mu.Lock()
	candidate, ok := m.sessions[digest]
	m.mu.Unlock()
	if !ok || request.Header.Get("X-CSRF-Token") == "" || subtle.ConstantTimeCompare(provided[:], candidate.csrfHash[:]) != 1 {
		return core.Subject{}, ErrDenied
	}
	return subject, nil
}

func (m *Manager) CSRF(request *http.Request) (string, error) {
	if _, err := m.Authenticate(request); err != nil {
		return "", err
	}
	cookie, _ := request.Cookie(CookieName)
	digest := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	defer m.mu.Unlock()
	candidate, ok := m.sessions[digest]
	if !ok || candidate.csrf == "" {
		return "", ErrUnauthenticated
	}
	return candidate.csrf, nil
}

func (m *Manager) Revoke(request *http.Request) {
	cookie, err := request.Cookie(CookieName)
	if err != nil {
		return
	}
	m.RevokeSessionValue(cookie.Value)
}

func (m *Manager) RevokeSessionValue(value string) {
	digest := sha256.Sum256([]byte(value))
	m.mu.Lock()
	delete(m.sessions, digest)
	m.mu.Unlock()
}

func (m *Manager) RevokeBootstrap(value string) {
	digest := sha256.Sum256([]byte(value))
	m.mu.Lock()
	delete(m.bootstraps, digest)
	m.mu.Unlock()
}

func (m *Manager) ValidateOrigin(request *http.Request, requireOrigin bool) error {
	if request.Host != m.origin.Host {
		return ErrDenied
	}
	origin := request.Header.Get("Origin")
	if requireOrigin && origin == "" {
		return ErrDenied
	}
	if origin != "" && origin != m.origin.String() {
		return ErrDenied
	}
	return nil
}

func (m *Manager) SetCookie(writer http.ResponseWriter, value string) {
	http.SetCookie(writer, &http.Cookie{Name: CookieName, Value: value, Path: "/console", MaxAge: int(m.config.SessionTTL.Seconds()), HttpOnly: true, Secure: m.origin.Scheme == "https", SameSite: http.SameSiteStrictMode})
}

func (m *Manager) ClearCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{Name: CookieName, Path: "/console", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: m.origin.Scheme == "https", SameSite: http.SameSiteStrictMode})
}

func (m *Manager) Page(raw string) (int, error) {
	if raw == "" {
		return m.config.MaxPageSize, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > m.config.MaxPageSize {
		return 0, ErrInvalidInput
	}
	return value, nil
}

func (m *Manager) ApplySecurityHeaders(header http.Header, authenticated bool) {
	header.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	if authenticated {
		header.Set("Cache-Control", "no-store")
		header.Set("Pragma", "no-cache")
	} else {
		header.Set("Cache-Control", "no-store")
	}
	if m.origin.Scheme == "https" {
		header.Set("Strict-Transport-Security", "max-age=31536000")
	}
}

func (m *Manager) prune(now time.Time) {
	for key, value := range m.bootstraps {
		if !now.Before(value.expires) {
			delete(m.bootstraps, key)
		}
	}
	for key, value := range m.sessions {
		if !now.Before(value.expires) || !now.Before(value.subject.ExpiresAt) {
			delete(m.sessions, key)
		}
	}
}

func Shell() []byte      { return append([]byte(nil), consoleweb.Index...) }
func Styles() []byte     { return append([]byte(nil), consoleweb.CSS...) }
func JavaScript() []byte { return append([]byte(nil), consoleweb.JavaScript...) }
