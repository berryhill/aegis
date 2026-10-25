package api

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestDashboardSixHourExplicitReauthenticationAndLogout(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	var elapsed atomic.Int64
	f := newCredentialRouteFixtureWithClock(t, 6*time.Hour, func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	base, _ := url.Parse("http://" + f.address)
	oldJar, _ := cookiejar.New(nil)
	oldJar.SetCookies(base, f.client.Jar.Cookies(base))
	oldClient := &http.Client{Jar: oldJar, Timeout: 5 * time.Second}
	elapsed.Store(int64(time.Hour))
	response, _ := f.post(f.client, "/console/login", url.Values{"password": {"incorrect-password"}})
	if response.StatusCode != http.StatusUnauthorized || len(response.Cookies()) != 0 {
		t.Fatal("failed reauthentication renewed identity")
	}
	response, err := f.client.Get(base.String() + "/console/reauthenticate")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) != 0 {
		t.Fatal("abandoned reauthentication page renewed identity")
	}
	response, _ = f.post(f.client, "/console/login", url.Values{"password": {"api-principal-password"}})
	cookies := response.Cookies()
	if response.StatusCode != http.StatusSeeOther || len(cookies) != 1 {
		t.Fatal("successful reauthentication did not replace identity")
	}
	if !cookies[0].Expires.Equal(start.Add(7*time.Hour)) || cookies[0].MaxAge != 21600 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("reauthentication policy/cookie protection incorrect")
	}
	response, err = oldClient.Get(base.String() + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("old subject survived replacement")
	}
	response, err = f.client.Get(base.String() + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		CSRF string `json:"csrf"`
	}
	err = json.NewDecoder(response.Body).Decode(&state)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || state.CSRF == "" {
		t.Fatal("new session state missing")
	}
	elapsed.Store(int64(2 * time.Hour))
	response, _ = f.post(f.client, "/console/logout", url.Values{"csrf": {state.CSRF}})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatal("logout denied before deadline")
	}
	response, err = f.client.Get(base.String() + "/console/api/state")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("logout did not revoke session")
	}
}
