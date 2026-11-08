package console

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PresentationReview is zero-effect, short-lived server custody, NOT authority.
// It retains only an exact bounded discovery/presentation form. No runtime,
// credential, session or foundational operation can use this review mechanism.
type PresentationReview struct {
	Path        string
	Return      string
	Payload     string
	PrincipalID string
	session     [32]byte
	generation  uint64
	expires     time.Time
	ready       bool
}

const presentationReviewTTL = 2 * time.Minute
const maxPresentationReviews = 128

func presentationPath(path string) bool {
	if len(path) > 2048 {
		return false
	}
	u, err := url.ParseRequestURI(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return false
	}
	if u.Path == "/console/loops/availability" {
		return len(u.Query()) == 1 && len(u.Query()["loop_id"]) == 1 && u.Query().Get("loop_id") != ""
	}
	if u.RawQuery != "" {
		return false
	}
	parts := strings.Split(u.Path, "/")
	return len(parts) == 5 && parts[1] == "console" && parts[2] == "queue" && parts[3] != "" && (parts[4] == "archive" || parts[4] == "unarchive")
}

// prunePresentation keeps an expired identity only for matching an already
// issued cookie+CSRF to zero-effect retention. It is NEVER consulted by identity
// or mutation admission. The fixed original expiry cannot be renewed.
// m.mu must be held.
func (m *Manager) prunePresentation(now time.Time) {
	if m.presentation == nil {
		m.presentation = make(map[[32]byte]PresentationReview)
	}
	if m.expiredPresentationSessions == nil {
		m.expiredPresentationSessions = make(map[[32]byte]session)
	}
	for key, v := range m.expiredPresentationSessions {
		if v.generation != m.generation || !now.Before(v.expires.Add(presentationReviewTTL)) {
			delete(m.expiredPresentationSessions, key)
		}
	}
	for key, v := range m.sessions {
		if !now.Before(v.expires) && now.Before(v.expires.Add(presentationReviewTTL)) && v.generation == m.generation && len(m.expiredPresentationSessions) < maxPresentationReviews {
			m.expiredPresentationSessions[key] = v
		}
	}
	for key, v := range m.presentation {
		if !now.Before(v.expires) || v.generation != m.generation {
			delete(m.presentation, key)
		}
	}
}

func (m *Manager) RetainPresentation(request *http.Request, path, back, payload string) (string, error) {
	if m.ValidateOrigin(request, true) != nil || !presentationPath(path) || len(payload) == 0 || len(payload) > 8192 || (back != "/console/loops" && back != "/console/queue") {
		return "", ErrDenied
	}
	values, err := url.ParseQuery(payload)
	if err != nil || values.Get("csrf") == "" {
		return "", ErrDenied
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil || !validOpaqueFormat(cookie.Value) {
		return "", ErrDenied
	}
	binding := sha256.Sum256([]byte(cookie.Value))
	provided := sha256.Sum256([]byte(values.Get("csrf")))
	token, key, err := opaque()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.prune(now)
	candidate, ok := m.sessions[binding]
	if !ok {
		candidate, ok = m.expiredPresentationSessions[binding]
	}
	if !ok || candidate.generation != m.generation || candidate.subject.PrincipalID != m.config.PrincipalID || subtle.ConstantTimeCompare(provided[:], candidate.csrfHash[:]) != 1 || !now.Before(candidate.expires.Add(presentationReviewTTL)) {
		return "", ErrDenied
	}
	// At most one pending presentation per original browser identity.
	for k, v := range m.presentation {
		if v.session == binding {
			delete(m.presentation, k)
		}
	}
	if len(m.presentation) >= maxPresentationReviews {
		return "", ErrDenied
	}
	expires := now.Add(presentationReviewTTL)
	if candidate.expires.Add(presentationReviewTTL).Before(expires) {
		expires = candidate.expires.Add(presentationReviewTTL)
	}
	values.Del("csrf")
	m.presentation[key] = PresentationReview{Path: path, Return: back, Payload: values.Encode(), PrincipalID: candidate.subject.PrincipalID, session: binding, generation: m.generation, expires: expires}
	return token, nil
}

// Presentation requires the exact bound cookie, even before password login.
// Possessing a URL/token alone cannot inspect, transfer, cancel or execute it.
func (m *Manager) Presentation(request *http.Request, token string) (PresentationReview, error) {
	if m.ValidateOrigin(request, false) != nil || !validOpaqueFormat(token) {
		return PresentationReview{}, ErrDenied
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil {
		return PresentationReview{}, ErrDenied
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	v, ok := m.presentation[sha256.Sum256([]byte(token))]
	if !ok || v.session != sha256.Sum256([]byte(cookie.Value)) || v.generation != m.generation {
		return PresentationReview{}, ErrDenied
	}
	return v, nil
}

// RebindPresentation is called only after real password Login. It transfers
// custody, not authority, and never extends the original review deadline.
func (m *Manager) RebindPresentation(request *http.Request, token, newSession string) error {
	if m.ValidateOrigin(request, true) != nil || !validOpaqueFormat(token) {
		return ErrDenied
	}
	cookie, err := request.Cookie(CookieName)
	if err != nil {
		return ErrDenied
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	key := sha256.Sum256([]byte(token))
	v, ok := m.presentation[key]
	candidate, fresh := m.sessions[sha256.Sum256([]byte(newSession))]
	if !ok || v.ready || v.session != sha256.Sum256([]byte(cookie.Value)) || !fresh || candidate.generation != v.generation || candidate.subject.PrincipalID != v.PrincipalID || !m.now().Before(candidate.actionUntil) {
		return ErrDenied
	}
	v.session = sha256.Sum256([]byte(newSession))
	v.ready = true
	m.presentation[key] = v
	return nil
}

func (m *Manager) ConsumePresentation(request *http.Request, token, path string) (PresentationReview, error) {
	if _, err := m.AuthorizeMutation(request); err != nil {
		return PresentationReview{}, err
	}
	cookie, _ := request.Cookie(CookieName)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	key := sha256.Sum256([]byte(token))
	v, ok := m.presentation[key]
	if !validOpaqueFormat(token) || !ok || !v.ready || v.Path != path || v.session != sha256.Sum256([]byte(cookie.Value)) {
		return PresentationReview{}, ErrDenied
	}
	delete(m.presentation, key) // before the effect: unknown completion cannot replay
	return v, nil
}

func (m *Manager) CancelPresentation(request *http.Request, token string) error {
	if m.ValidateOrigin(request, true) != nil {
		return ErrDenied
	}
	v, err := m.Presentation(request, token)
	if err != nil {
		return err
	}
	// Cancellation needs the original/new session CSRF, never fresh action rights.
	cookie, _ := request.Cookie(CookieName)
	binding := sha256.Sum256([]byte(cookie.Value))
	provided := sha256.Sum256([]byte(request.Header.Get("X-CSRF-Token")))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	candidate, ok := m.sessions[binding]
	if !ok {
		candidate, ok = m.expiredPresentationSessions[binding]
	}
	if !ok || subtle.ConstantTimeCompare(provided[:], candidate.csrfHash[:]) != 1 || v.session != binding {
		return ErrDenied
	}
	delete(m.presentation, sha256.Sum256([]byte(token)))
	return nil
}

func (m *Manager) PresentationCSRF(request *http.Request, token string) (string, error) {
	if _, err := m.Presentation(request, token); err != nil {
		return "", err
	}
	cookie, _ := request.Cookie(CookieName)
	binding := sha256.Sum256([]byte(cookie.Value))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prune(m.now())
	candidate, ok := m.sessions[binding]
	if !ok {
		candidate, ok = m.expiredPresentationSessions[binding]
	}
	if !ok || candidate.generation != m.generation {
		return "", ErrDenied
	}
	return candidate.csrf, nil
}

func (m *Manager) PresentationReady(v PresentationReview) bool { return v.ready }
