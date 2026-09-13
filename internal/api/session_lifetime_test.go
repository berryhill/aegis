package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
)

func TestBrowserReauthenticationReplacesIdentityAndStaleLogout(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	var elapsed atomic.Int64
	f := newCredentialRouteFixtureWithClock(t, time.Hour, func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	base, _ := url.Parse("http://" + f.address)
	oldJar, _ := cookiejar.New(nil)
	oldJar.SetCookies(base, f.client.Jar.Cookies(base))
	oldClient := &http.Client{Jar: oldJar, Timeout: 5 * time.Second}
	elapsed.Store(int64(16 * time.Minute))
	response, _ := f.post(f.client, "/console/login", url.Values{"password": {"api-principal-password"}})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("step-up status=%d", response.StatusCode)
	}
	if len(response.Cookies()) != 1 || !response.Cookies()[0].Expires.Equal(start.Add(76*time.Minute)) || response.Cookies()[0].MaxAge != 3600 {
		t.Fatal("reauthentication did not create a new fixed one-hour cookie")
	}
	readState := func(client *http.Client, want int) string {
		t.Helper()
		time.Sleep(210 * time.Millisecond)
		response, err := client.Get(base.String() + "/console/api/state")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("state status=%d want=%d", response.StatusCode, want)
		}
		if want != http.StatusOK {
			return ""
		}
		var state struct {
			CSRF string `json:"csrf"`
		}
		if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
			t.Fatal(err)
		}
		if state.CSRF == "" {
			t.Fatal("missing new CSRF")
		}
		return state.CSRF
	}
	readState(oldClient, http.StatusUnauthorized)
	newCSRF := readState(f.client, http.StatusOK)
	response, _ = f.post(f.client, "/console/credentials/operation/review", url.Values{"csrf": {f.csrf}, "operation": {"backup"}})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("old CSRF accepted: %d", response.StatusCode)
	}
	response, _ = f.post(f.client, "/console/credentials/operation/review", url.Values{"csrf": {newCSRF}, "operation": {"backup"}})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh review denied: %d", response.StatusCode)
	}
	elapsed.Store(int64(32 * time.Minute))
	response, _ = f.post(f.client, "/console/logout", url.Values{"csrf": {newCSRF}})
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		t.Fatalf("stale logout denied: %d", response.StatusCode)
	}
	readState(f.client, http.StatusUnauthorized)
}

// Exercise real HTTP routes, not just the displayed TTL or manager admission.
func TestBrowserLifetimeReadsAndSensitiveStepUp(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	var elapsed atomic.Int64
	f := newCredentialRouteFixtureWithClock(t, config.Defaults().API.Console.SessionTTL, func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	get := func(path string, want int) []byte {
		t.Helper()
		time.Sleep(210 * time.Millisecond)
		response, err := f.client.Get("http://" + f.address + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != want {
			t.Fatalf("GET %s status=%d want=%d err=%v", path, response.StatusCode, want, err)
		}
		if want == http.StatusOK && len(response.Cookies()) != 0 {
			t.Fatal("ordinary read renewed session cookie")
		}
		return body
	}
	for _, offset := range []time.Duration{5*time.Minute + time.Second, 16 * time.Minute, time.Hour - time.Nanosecond} {
		elapsed.Store(int64(offset))
		for _, path := range []string{"/console/agents", "/console/loops", "/console/graphs", "/console/queue", "/console/credentials", "/console/api/state"} {
			get(path, http.StatusOK)
		}
	}
	for _, offset := range []time.Duration{15 * time.Minute, 16 * time.Minute, time.Hour - time.Nanosecond} {
		elapsed.Store(int64(offset))
		response, body := f.post(f.client, "/console/credentials/operation/review", url.Values{"csrf": {f.csrf}, "operation": {"backup"}})
		if response.StatusCode != http.StatusUnauthorized || !bytes.Contains(body, []byte("principal_reauthentication_required")) {
			t.Fatalf("stale sensitive operation at %s status=%d missing step-up=%t", offset, response.StatusCode, !bytes.Contains(body, []byte("principal_reauthentication_required")))
		}
	}
	get("/console/reauthenticate", http.StatusOK)
	for _, offset := range []time.Duration{time.Hour, time.Hour + time.Second} {
		elapsed.Store(int64(offset))
		// Use the state endpoint, which denies rather than rendering login HTML.
		get("/console/api/state", http.StatusUnauthorized)
	}
}
