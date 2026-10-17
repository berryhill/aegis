package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	consoleauth "github.com/berryhill/aegis/internal/console"
	"golang.org/x/net/html"
)

func upgradeConsoleClient(origin string) (*url.URL, *http.Client, error) {
	base, err := url.Parse(strings.TrimRight(origin, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.Path != "" {
		return nil, nil, errors.New("configured Console origin is invalid")
	}
	ip := net.ParseIP(base.Hostname())
	if base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return nil, nil, errors.New("Console verification requires HTTPS or loopback HTTP")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return base, client, nil
}

func consoleProbeRequest(ctx context.Context, client *http.Client, base *url.URL, method, path string, data []byte) (*http.Response, []byte, error) {
	target := *base
	target.Path = path
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	if method != http.MethodGet {
		request.Header.Set("Origin", base.String())
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return response, nil, errors.New("Console verification response is unavailable or oversized")
	}
	return response, body, nil
}

func recognizableUpgradeConsole(body []byte, authenticated bool) bool {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return false
	}
	var title, stylesheet, header, main, login, logout bool
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			attrs := make(map[string]string)
			for _, attr := range node.Attr {
				attrs[attr.Key] = attr.Val
			}
			if node.Data == "title" && node.FirstChild != nil && strings.Contains(node.FirstChild.Data, "Aegis") {
				title = true
			}
			if node.Data == "link" && attrs["href"] == "/console/assets/app.css" {
				stylesheet = true
			}
			if node.Data == "header" && attrs["id"] == "console-header" {
				header = true
			}
			if node.Data == "main" && attrs["id"] == "main" {
				main = true
			}
			if node.Data == "form" && attrs["action"] == "/console/login" {
				login = true
			}
			if node.Data == "form" && attrs["action"] == "/console/logout" {
				logout = true
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	if authenticated {
		return title && stylesheet && header && main && logout && !login && bytes.Contains(body, []byte("Authenticated control plane"))
	}
	return title && stylesheet && header && main && login
}

func verifyUpgradeConsoleShell(ctx context.Context, origin string) error {
	base, client, err := upgradeConsoleClient(origin)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	response, body, err := consoleProbeRequest(ctx, client, base, http.MethodGet, "/console", nil)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !recognizableUpgradeConsole(body, false) {
		return errors.New("configured origin did not serve the expected Console authentication page")
	}
	return nil
}

// The enrolled password comes only from the retained protected input capability.
// It and the probe's own short-lived cookie/CSRF values never enter tool output,
// journal metadata, URLs or logs. This is application verification, not a claim
// that a real browser's interaction/visual acceptance has been exercised.
func verifyAuthenticatedUpgradeConsole(ctx context.Context, origin string, password []byte) (err error) {
	base, client, err := upgradeConsoleClient(origin)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	response, body, err := consoleProbeRequest(ctx, client, base, http.MethodGet, "/console", nil)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !recognizableUpgradeConsole(body, false) {
		return errors.New("configured origin is not the expected Console")
	}
	if len(password) == 0 {
		return errors.New("protected authentication required for Console verification")
	}
	form := []byte("password=" + url.QueryEscape(string(password)))
	response, _, err = consoleProbeRequest(ctx, client, base, http.MethodPost, "/console/login", form)
	clear(form)
	var sessionCookie bool
	sessionURL := base.ResolveReference(&url.URL{Path: "/console"})
	for _, cookie := range client.Jar.Cookies(sessionURL) {
		sessionCookie = sessionCookie || cookie.Name == consoleauth.CookieName
	}
	var sessionCSRF string
	if response != nil {
		sessionCSRF = response.Header.Get("X-Aegis-Session-CSRF")
	}
	// Headers can establish a session before the body fails. Register revocation
	// before handling body/redirect errors, never leave the probe session behind.
	if sessionCookie && sessionCSRF != "" {
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			logout := []byte("csrf=" + url.QueryEscape(sessionCSRF))
			response, _, logoutErr := consoleProbeRequest(cleanup, client, base, http.MethodPost, "/console/logout", logout)
			clear(logout)
			if logoutErr != nil || response == nil || response.StatusCode != http.StatusSeeOther {
				err = errors.Join(err, errors.New("Console verification session revocation failed"))
			}
		}()
	}
	if err != nil {
		return err
	}
	location, locationErr := response.Location()
	if response.StatusCode != http.StatusSeeOther || locationErr != nil || location.Scheme != base.Scheme || location.Host != base.Host || location.Path != "/console/agents" {
		return errors.New("Console principal authentication did not reach the expected Agents route")
	}
	if !sessionCookie || sessionCSRF == "" {
		return errors.New("Console verification authenticated revocation binding unavailable")
	}
	response, body, err = consoleProbeRequest(ctx, client, base, http.MethodGet, "/console/api/state", nil)
	if err != nil {
		return err
	}
	var state struct {
		State string `json:"state"`
		CSRF  string `json:"csrf"`
	}
	if json.Unmarshal(body, &state) != nil || state.CSRF != sessionCSRF {
		return errors.New("authenticated Console state/CSRF unavailable")
	}
	if response.StatusCode != http.StatusOK || state.State != "ready" {
		return errors.New("authenticated Console data loading is unavailable")
	}
	response, body, err = consoleProbeRequest(ctx, client, base, http.MethodGet, "/console/agents", nil)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK || !recognizableUpgradeConsole(body, true) {
		return errors.New("authenticated Console Agents rendering failed")
	}
	for _, asset := range []string{"/console/assets/app.css", "/console/assets/navigation.js"} {
		response, body, err = consoleProbeRequest(ctx, client, base, http.MethodGet, asset, nil)
		if err != nil || response == nil || response.StatusCode != http.StatusOK || len(body) == 0 {
			return fmt.Errorf("Console asset verification failed: %s", asset)
		}
	}
	return nil
}
