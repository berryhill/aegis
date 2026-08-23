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
	"github.com/berryhill/aegis/internal/principalauth"
	consoleweb "github.com/berryhill/aegis/web/console"
)

var (
	ErrUnauthenticated            = errors.New("console authentication failed")
	ErrDenied                     = errors.New("console request denied")
	ErrInvalidInput               = errors.New("invalid console input")
	ErrReviewReceiptInvalidFormat = errors.New("review_receipt_invalid_format")
	ErrReviewReceiptUnavailable   = errors.New("review_receipt_unavailable")
	ErrInvalidCredentials         = errors.New("principal authentication failed")
	ErrLoginThrottled             = errors.New("principal authentication retry limit reached")
)

const CookieName = "aegis-console"

const (
	reviewReceiptTTL             = 2 * time.Minute
	maxReviewReceiptPayloadBytes = 512 << 10
)

type Config struct {
	Origin           string
	SessionTTL       time.Duration
	MaxPageSize      int
	PrincipalID      string
	PrincipalAuthTTL time.Duration
	PasswordVerifier *principalauth.Record
	LoginBurst       int
	LoginWindow      time.Duration
}

type session struct {
	subject    core.Subject
	csrf       string
	csrfHash   [32]byte
	expires    time.Time
	generation uint64
}

type reviewReceipt struct {
	session [32]byte
	purpose string
	payload []byte
	expires time.Time
}

type Manager struct {
	mu            sync.Mutex
	rotationMu    sync.Mutex
	origin        *url.URL
	config        Config
	now           func() time.Time
	sessions      map[[32]byte]session
	loginFailures map[string][]time.Time
	loginInFlight map[string]int
	generation    uint64
	receipts      map[[32]byte]reviewReceipt
	pending       map[[32]byte][32]byte
}

func New(config Config, now func() time.Time) (*Manager, error) {
	if now == nil || config.SessionTTL <= 0 || config.SessionTTL > 15*time.Minute || config.MaxPageSize < 1 || config.MaxPageSize > 1000 {
		return nil, fmt.Errorf("%w: console limits must be positive and bounded", ErrInvalidInput)
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || origin.Scheme != "https" && origin.Scheme != "http" {
		return nil, fmt.Errorf("%w: console origin must be an absolute HTTP origin", ErrInvalidInput)
	}
	if origin.Scheme != "https" && !loopbackHost(origin.Hostname()) {
		return nil, fmt.Errorf("%w: plaintext console transport is restricted to loopback", ErrInvalidInput)
	}
	if config.PasswordVerifier != nil {
		if config.PrincipalID == "" || config.PrincipalAuthTTL <= 0 || config.PrincipalAuthTTL > 15*time.Minute || config.PasswordVerifier.PrincipalID != config.PrincipalID || config.LoginBurst < 1 || config.LoginBurst > 20 || config.LoginWindow <= 0 || config.LoginWindow > 15*time.Minute {
			return nil, fmt.Errorf("%w: principal password authentication configuration is invalid", ErrInvalidInput)
		}
	}
	return &Manager{origin: origin, config: config, now: now, sessions: make(map[[32]byte]session), loginFailures: make(map[string][]time.Time), loginInFlight: make(map[string]int), generation: 1, receipts: make(map[[32]byte]reviewReceipt), pending: make(map[[32]byte][32]byte)}, nil
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

func validOpaqueFormat(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

func (m *Manager) Login(request *http.Request, client string, password []byte) (string, string, time.Time, core.Subject, error) {
	if err := m.ValidateOrigin(request, true); err != nil {
		return "", "", time.Time{}, core.Subject{}, err
	}
	now := m.now()
	m.mu.Lock()
	failures := m.pruneLoginFailures(client, now)
	if m.config.PasswordVerifier == nil || client == "" || len(failures)+m.loginInFlight[client] >= m.config.LoginBurst {
		m.mu.Unlock()
		return "", "", time.Time{}, core.Subject{}, ErrLoginThrottled
	}
	m.loginInFlight[client]++
	verifier := *m.config.PasswordVerifier
	generation := m.generation
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.loginInFlight[client]--
		if m.loginInFlight[client] == 0 {
			delete(m.loginInFlight, client)
		}
		m.mu.Unlock()
	}()
	if err := verifier.Verify(password); err != nil {
		m.mu.Lock()
		m.loginFailures[client] = append(m.loginFailures[client], now)
		m.mu.Unlock()
		return "", "", time.Time{}, core.Subject{}, ErrInvalidCredentials
	}
	m.mu.Lock()
	if generation != m.generation || m.config.PasswordVerifier == nil || verifier != *m.config.PasswordVerifier {
		m.mu.Unlock()
		return "", "", time.Time{}, core.Subject{}, ErrInvalidCredentials
	}
	delete(m.loginFailures, client)
	m.mu.Unlock()
	subjectValue, _, err := opaque()
	if err != nil {
		return "", "", time.Time{}, core.Subject{}, fmt.Errorf("generate principal authentication subject: %w", err)
	}
	subject := core.Subject{ID: "password:" + subjectValue, Kind: "principal", PrincipalID: m.config.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: now, ExpiresAt: now.Add(m.config.PrincipalAuthTTL)}
	sessionValue, sessionDigest, err := opaque()
	if err != nil {
		return "", "", time.Time{}, core.Subject{}, fmt.Errorf("generate console session: %w", err)
	}
	csrf, _, err := opaque()
	if err != nil {
		return "", "", time.Time{}, core.Subject{}, fmt.Errorf("generate CSRF value: %w", err)
	}
	expires := now.Add(m.config.SessionTTL)
	if subject.ExpiresAt.Before(expires) {
		expires = subject.ExpiresAt
	}
	m.mu.Lock()
	m.prune(now)
	if generation != m.generation {
		m.mu.Unlock()
		return "", "", time.Time{}, core.Subject{}, ErrInvalidCredentials
	}
	m.sessions[sessionDigest] = session{subject: subject, csrf: csrf, csrfHash: sha256.Sum256([]byte(csrf)), expires: expires, generation: generation}
	m.mu.Unlock()
	return sessionValue, csrf, expires, subject, nil
}

func (m *Manager) IssueReviewReceipt(request *http.Request, purpose string, payload []byte) (string, error) {
	if purpose == "" || len(purpose) > 128 || len(payload) == 0 || len(payload) > maxReviewReceiptPayloadBytes {
		return "", ErrDenied
	}
	if _, err := m.Authenticate(request); err != nil {
		return "", err
	}
	cookie, _ := request.Cookie(CookieName)
	sessionDigest := sha256.Sum256([]byte(cookie.Value))
	value, receiptDigest, err := opaque()
	if err != nil {
		return "", fmt.Errorf("generate review receipt: %w", err)
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	candidate, ok := m.sessions[sessionDigest]
	if !ok || !now.Before(candidate.expires) || !now.Before(candidate.subject.ExpiresAt) {
		return "", ErrUnauthenticated
	}
	expires := now.Add(reviewReceiptTTL)
	if candidate.expires.Before(expires) {
		expires = candidate.expires
	}
	if candidate.subject.ExpiresAt.Before(expires) {
		expires = candidate.subject.ExpiresAt
	}
	if previous, exists := m.pending[sessionDigest]; exists {
		m.deleteReceipt(previous)
	}
	m.receipts[receiptDigest] = reviewReceipt{session: sessionDigest, purpose: purpose, payload: append([]byte(nil), payload...), expires: expires}
	m.pending[sessionDigest] = receiptDigest
	return value, nil
}

func (m *Manager) ConsumeReviewReceipt(request *http.Request, purpose, value string) ([]byte, error) {
	if !validOpaqueFormat(value) {
		return nil, ErrReviewReceiptInvalidFormat
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil, ErrReviewReceiptUnavailable
	}
	now := m.now()
	sessionDigest := sha256.Sum256([]byte(cookie.Value))
	receiptDigest := sha256.Sum256([]byte(value))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	sessionCandidate, sessionOK := m.sessions[sessionDigest]
	pending, pendingOK := m.pending[sessionDigest]
	receiptCandidate, receiptOK := m.receipts[receiptDigest]
	if !sessionOK || !now.Before(sessionCandidate.expires) || !now.Before(sessionCandidate.subject.ExpiresAt) || !pendingOK || pending != receiptDigest || !receiptOK || receiptCandidate.session != sessionDigest || receiptCandidate.purpose != purpose || !now.Before(receiptCandidate.expires) {
		return nil, ErrReviewReceiptUnavailable
	}
	result := append([]byte(nil), receiptCandidate.payload...)
	m.deleteReceipt(receiptDigest)
	delete(m.pending, sessionDigest)
	return result, nil
}

func (m *Manager) CancelReviewReceipt(request *http.Request, purpose, value string) error {
	if !validOpaqueFormat(value) {
		return ErrReviewReceiptInvalidFormat
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return ErrReviewReceiptUnavailable
	}
	now := m.now()
	sessionDigest := sha256.Sum256([]byte(cookie.Value))
	receiptDigest := sha256.Sum256([]byte(value))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(now)
	sessionCandidate, sessionOK := m.sessions[sessionDigest]
	pending, pendingOK := m.pending[sessionDigest]
	receiptCandidate, receiptOK := m.receipts[receiptDigest]
	if !sessionOK || !now.Before(sessionCandidate.expires) || !now.Before(sessionCandidate.subject.ExpiresAt) || !pendingOK || pending != receiptDigest || !receiptOK || receiptCandidate.session != sessionDigest || receiptCandidate.purpose != purpose || !now.Before(receiptCandidate.expires) {
		return ErrReviewReceiptUnavailable
	}
	m.deleteReceipt(receiptDigest)
	delete(m.pending, sessionDigest)
	return nil
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
	if !ok || candidate.generation != m.generation || !now.Before(candidate.expires) || !now.Before(candidate.subject.ExpiresAt) || candidate.subject.PrincipalID == "" {
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

// AuthorizeCommand repeats the complete browser mutation admission and returns
// a server-derived session binding for command intents. The binding is a digest
// of the opaque cookie value; neither the cookie nor its CSRF material leaves
// the console boundary or enters command/audit records.
func (m *Manager) AuthorizeCommand(request *http.Request) (core.Subject, string, error) {
	subject, err := m.AuthorizeMutation(request)
	if err != nil {
		return core.Subject{}, "", err
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return core.Subject{}, "", ErrUnauthenticated
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	return subject, "session-" + hex.EncodeToString(digest[:]), nil
}

// RotatePassword requires an authenticated same-origin session, its CSRF
// proof, fresh verification of the current password, exact confirmation, and
// explicit approval. The callback durably publishes and audits before the
// manager activates the replacement.
func (m *Manager) RotatePassword(request *http.Request, client string, currentPassword, newPassword, confirmation []byte, approved bool, replace func(principalauth.Record, principalauth.Record, core.Subject) error) error {
	subject, err := m.AuthorizeMutation(request)
	if err != nil {
		return err
	}
	if client == "" || !approved || replace == nil || len(newPassword) != len(confirmation) || subtle.ConstantTimeCompare(newPassword, confirmation) != 1 {
		return ErrInvalidInput
	}
	m.rotationMu.Lock()
	defer m.rotationMu.Unlock()
	m.mu.Lock()
	cookie, err := request.Cookie(CookieName)
	if err != nil {
		m.mu.Unlock()
		return ErrUnauthenticated
	}
	candidate, ok := m.sessions[sha256.Sum256([]byte(cookie.Value))]
	now := m.now()
	if !ok || candidate.generation != m.generation || !now.Before(candidate.expires) || !now.Before(candidate.subject.ExpiresAt) || m.config.PasswordVerifier == nil {
		m.mu.Unlock()
		return ErrUnauthenticated
	}
	if len(m.pruneLoginFailures(client, now)) >= m.config.LoginBurst {
		m.mu.Unlock()
		return ErrLoginThrottled
	}
	current := *m.config.PasswordVerifier
	if err = current.Verify(currentPassword); err != nil {
		m.loginFailures[client] = append(m.loginFailures[client], now)
		m.mu.Unlock()
		return ErrInvalidCredentials
	}
	replacement, err := principalauth.Enroll(m.config.PrincipalID, newPassword)
	if err != nil {
		m.mu.Unlock()
		return ErrInvalidInput
	}
	m.mu.Unlock()
	if err = replace(current, replacement, subject); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.PasswordVerifier == nil || *m.config.PasswordVerifier != current || candidate.generation != m.generation {
		return principalauth.ErrVerifierChanged
	}
	m.config.PasswordVerifier = &replacement
	m.generation++
	m.sessions = make(map[[32]byte]session)
	m.loginFailures = make(map[string][]time.Time)
	return nil
}

// pruneLoginFailures requires m.mu to be held.
func (m *Manager) pruneLoginFailures(client string, now time.Time) []time.Time {
	failures := m.loginFailures[client][:0]
	for _, failure := range m.loginFailures[client] {
		if now.Sub(failure) < m.config.LoginWindow {
			failures = append(failures, failure)
		}
	}
	if len(failures) == 0 {
		delete(m.loginFailures, client)
	} else {
		m.loginFailures[client] = failures
	}
	return failures
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
	if receipt, ok := m.pending[digest]; ok {
		m.deleteReceipt(receipt)
		delete(m.pending, digest)
	}
	delete(m.sessions, digest)
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
	m.SetCookieUntil(writer, value, m.now().Add(m.config.SessionTTL))
}

func (m *Manager) SetCookieUntil(writer http.ResponseWriter, value string, expires time.Time) {
	maxAge := int(expires.Sub(m.now()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(writer, &http.Cookie{Name: CookieName, Value: value, Path: "/console", Expires: expires, MaxAge: maxAge, HttpOnly: true, Secure: m.origin.Scheme == "https", SameSite: http.SameSiteStrictMode})
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
	header.Set("Referrer-Policy", "strict-origin")
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

// deleteReceipt requires m.mu to be held and clears any secret-bearing review
// payload before releasing the map reference.
func (m *Manager) deleteReceipt(key [32]byte) {
	if receipt, ok := m.receipts[key]; ok {
		for index := range receipt.payload {
			receipt.payload[index] = 0
		}
		delete(m.receipts, key)
	}
}

func (m *Manager) prune(now time.Time) {
	for key, value := range m.sessions {
		if !now.Before(value.expires) || !now.Before(value.subject.ExpiresAt) {
			if receipt, ok := m.pending[key]; ok {
				m.deleteReceipt(receipt)
				delete(m.pending, key)
			}
			delete(m.sessions, key)
		}
	}
	for key, value := range m.receipts {
		if !now.Before(value.expires) {
			m.deleteReceipt(key)
			if m.pending[value.session] == key {
				delete(m.pending, value.session)
			}
		}
	}
}

func Styles() []byte   { return append([]byte(nil), consoleweb.CSS...) }
func Datastar() []byte { return append([]byte(nil), consoleweb.Datastar...) }
