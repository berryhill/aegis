package console

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/principalauth"
)

func TestPresentationRetentionDoesNotGrantOrRenewAuthority(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("isolated-presentation-password"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(Config{Origin: "https://console.example.test", SessionTTL: 4 * time.Minute, ActionAuthTTL: time.Minute, MaxPageSize: 100, PrincipalID: "principal", PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := func(cookie, csrf, path string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://console.example.test"+path, nil)
		r.Header.Set("Origin", "https://console.example.test")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: CookieName, Value: cookie})
		}
		return r
	}
	login := func() (string, string, time.Time) {
		s, c, e, _, err := m.Login(request("", "", "/console/login"), "isolated-client", []byte("isolated-presentation-password"))
		if err != nil {
			t.Fatal(err)
		}
		return s, c, e
	}
	old, csrf, originalExpiry := login()
	path := "/console/queue/item/archive"
	r := request(old, csrf, path)
	payload := url.Values{"csrf": {csrf}, "digest": {"immutable-item"}, "expected_revision": {"0"}}.Encode()
	now = now.Add(61 * time.Second)
	if _, err = m.Authenticate(r); err != nil {
		t.Fatal("identity unexpectedly expired", err)
	}
	if _, err = m.AuthorizeMutation(r); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("expired action admitted", err)
	}
	for _, kind := range []string{"origin", "csrf", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			bad := r.Clone(r.Context())
			p := payload
			target := path
			switch kind {
			case "origin":
				bad.Header.Set("Origin", "https://foreign.example.test")
			case "csrf":
				p = strings.Replace(payload, url.QueryEscape(csrf), "wrong", 1)
			case "runtime":
				target = "/console/queue/item/requeue"
			}
			if _, err := m.RetainPresentation(bad, target, "/console/queue", p); err == nil {
				t.Fatal("invalid retention admitted")
			}
		})
	}
	token, err := m.RetainPresentation(r, path, "/console/queue", payload)
	if err != nil {
		t.Fatal(err)
	}
	review, err := m.Presentation(r, token)
	if err != nil || strings.Contains(review.Payload, csrf) || m.PresentationReady(review) {
		t.Fatal("retention leaked proof or became authority", err)
	}
	if _, err = m.Presentation(request("foreign", csrf, path), token); err == nil {
		t.Fatal("foreign cookie accessed retained action")
	}
	if _, err = m.AuthorizeMutation(r); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("retention renewed action authority", err)
	}
	fresh, freshCSRF, freshExpiry := login()
	if !freshExpiry.Equal(now.Add(4*time.Minute)) || !originalExpiry.Before(freshExpiry) {
		t.Fatal("new login did not create separate fixed lifetime")
	}
	if err = m.RebindPresentation(r, token, fresh); err != nil {
		t.Fatal(err)
	}
	m.Revoke(r)
	current := request(fresh, freshCSRF, path)
	if _, err = m.Presentation(current, token); err != nil {
		t.Fatal("old-session revocation removed rebound review", err)
	}
	if _, err = m.ConsumePresentation(current, token, "/console/queue/different/archive"); err == nil {
		t.Fatal("retained target substituted")
	}
	if _, err = m.ConsumePresentation(current, token, path); err != nil {
		t.Fatal(err)
	}
	if _, err = m.ConsumePresentation(current, token, path); err == nil {
		t.Fatal("consumed review replayed")
	}
	if _, err = m.Authenticate(r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("old session revived", err)
	}

	// Expired identity may only prove original cookie/CSRF for a brief zero-effect
	// recovery. It never passes ordinary authentication or mutation admission.
	now = freshExpiry.Add(time.Second)
	if _, err = m.Authenticate(current); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired login admitted", err)
	}
	payload = url.Values{"csrf": {freshCSRF}, "digest": {"immutable-item"}, "expected_revision": {"0"}}.Encode()
	token, err = m.RetainPresentation(current, path, "/console/queue", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.AuthorizeMutation(current); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired identity retention restored authentication", err)
	}
	if err = m.CancelPresentation(current, token); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Presentation(current, token); err == nil {
		t.Fatal("cancelled action retained")
	}
	token, err = m.RetainPresentation(current, path, "/console/queue", payload)
	if err != nil {
		t.Fatal(err)
	}
	now = freshExpiry.Add(presentationReviewTTL)
	if _, err = m.Presentation(current, token); err == nil {
		t.Fatal("review outlived original fixed custody bound")
	}
	if _, err = m.RetainPresentation(current, path, "/console/queue", payload); err == nil {
		t.Fatal("expired retention window renewed")
	}
}
