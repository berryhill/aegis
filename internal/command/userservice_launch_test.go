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
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

func TestLaunchConsoleEstablishesBrowserSessionWithoutPuttingCredentialsInURL(t *testing.T) {
	configPath, origin, stop := consoleBootstrapFixture(t, http.StatusCreated, `{"bootstrap":"single-use-test-bootstrap","expires_at":"2030-01-01T00:00:00Z"}`)
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	var opened string
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(_ context.Context, target string) error {
		opened = target
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			return err
		}
		if request.URL.RawQuery != "" || request.URL.Fragment != "" || strings.Contains(target, "single-use-test-bootstrap") {
			t.Fatalf("browser handoff leaked credential in URL: %s", target)
		}
		if !strings.HasPrefix(request.URL.Path, "/handoff/") || len(strings.TrimPrefix(request.URL.Path, "/handoff/")) < 32 {
			t.Fatalf("browser handoff did not include an unguessable launch correlation: %s", target)
		}
		probeClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, probe := range []string{
			request.URL.Scheme + "://" + request.URL.Host + "/handoff",
			request.URL.Scheme + "://" + request.URL.Host + "/handoff/wrong-correlation",
		} {
			response, probeErr := probeClient.Get(probe) //nolint:gosec // probe targets the loopback-only handoff listener.
			if probeErr != nil {
				return probeErr
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNotFound || len(response.Cookies()) != 0 {
				t.Fatalf("unauthenticated handoff probe status=%d cookies=%v", response.StatusCode, response.Cookies())
			}
		}
		jar, err := cookiejar.New(nil)
		if err != nil {
			return err
		}
		client := &http.Client{Jar: jar}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || response.Request.URL.String() != origin+"/console" {
			t.Fatalf("authenticated console response status=%d url=%q", response.StatusCode, response.Request.URL)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		if string(body) != "authenticated console" {
			t.Fatalf("final console request was not authenticated: %q", body)
		}
		replay, replayErr := probeClient.Get(target) //nolint:gosec // target is the loopback-only handoff listener.
		if replayErr != nil {
			return replayErr
		}
		defer replay.Body.Close()
		if replay.StatusCode != http.StatusNotFound || len(replay.Cookies()) != 0 {
			t.Fatalf("replayed handoff status=%d cookies=%v", replay.StatusCode, replay.Cookies())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened == origin+"/console" {
		t.Fatalf("browser opened unauthenticated console directly: %q", opened)
	}
	text := output.String()
	for _, expected := range []string{`"browser_opened": true`, `"browser_session_established": true`, `"single_use": true`, `"reusable_bearer_exposed": false`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("launch output missing %s: %s", expected, text)
		}
	}
	if strings.Contains(text, "single-use-test-bootstrap") {
		t.Fatalf("completed browser handoff exposed consumed bootstrap: %s", text)
	}
}

func TestLaunchConsoleReturnsManualHandoffWhenBrowserCannotOpen(t *testing.T) {
	configPath, origin, stop := consoleBootstrapFixture(t, http.StatusCreated, `{"bootstrap":"single-use-test-bootstrap","expires_at":"2030-01-01T00:00:00Z"}`)
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(context.Context, string) error {
		return fmt.Errorf("synthetic opener failure")
	})
	if err == nil || !strings.Contains(err.Error(), "browser launch failed") {
		t.Fatalf("browser failure was not actionable: %v", err)
	}
	text := output.String()
	for _, expected := range []string{`"browser_opened": false`, `"manual_url": "` + origin + `/console"`, `"single_use": true`, `"bootstrap": "single-use-test-bootstrap"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("manual handoff output missing %s: %s", expected, text)
		}
	}
}

func TestLaunchConsoleDoesNotOfferConsumedBootstrapAfterExchangeValidationFails(t *testing.T) {
	configPath, origin, stop := consoleBootstrapFixtureWithCookie(t, http.StatusCreated, `{"bootstrap":"single-use-test-bootstrap","expires_at":"2030-01-01T00:00:00Z"}`, &http.Cookie{
		Name:     "aegis-console",
		Value:    "test-session",
		Path:     "/console",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	defer stop()
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(_ context.Context, target string) error {
		response, requestErr := http.Get(target) //nolint:gosec // target is the loopback-only handoff listener.
		if requestErr == nil {
			_ = response.Body.Close()
		}
		return requestErr
	})
	if err == nil || !strings.Contains(err.Error(), "request a fresh bootstrap") {
		t.Fatalf("consumed bootstrap failure was not actionable: %v", err)
	}
	text := output.String()
	if strings.Contains(text, "single-use-test-bootstrap") || strings.Contains(text, `"manual_url": "`+origin+`/console"`) {
		t.Fatalf("consumed bootstrap was presented as a usable manual fallback: %s", text)
	}
}

func TestLaunchConsoleDoesNotOpenBrowserWhenBootstrapIsDenied(t *testing.T) {
	configPath, _, stop := consoleBootstrapFixture(t, http.StatusForbidden, `{"error":"denied"}`)
	defer stop()
	called := false
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	err := launchConsole(cmd, &rootOptions{configFile: configPath}, func(context.Context, string) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("bootstrap denial was not preserved: %v", err)
	}
	if called {
		t.Fatal("browser opened before authority admitted a bootstrap")
	}
}

func TestLaunchBrowserSessionWaitsForAuthenticatedBrowserConfirmation(t *testing.T) {
	consoleReached := make(chan struct{})
	releaseConsole := make(chan struct{})
	consoleServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/console/session":
			http.SetCookie(writer, &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			writer.WriteHeader(http.StatusCreated)
		case request.Method == http.MethodGet && request.URL.Path == "/console":
			cookie, err := request.Cookie("aegis-console")
			if err != nil || cookie.Value != "test-session" {
				http.Error(writer, "unauthenticated", http.StatusUnauthorized)
				return
			}
			confirmation := request.URL.Query().Get("browser_handoff")
			if confirmation == "" {
				writer.WriteHeader(http.StatusOK)
				return
			}
			close(consoleReached)
			<-releaseConsole
			writer.Header().Set("Location", confirmation)
			writer.WriteHeader(http.StatusSeeOther)
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer consoleServer.Close()

	browserResult := make(chan error, 1)
	launchResult := make(chan error, 1)
	go func() {
		launchResult <- launchBrowserSession(context.Background(), consoleServer.URL, "single-use-test-bootstrap", func(_ context.Context, target string) error {
			go func() {
				jar, err := cookiejar.New(nil)
				if err != nil {
					browserResult <- err
					return
				}
				response, err := (&http.Client{Jar: jar}).Get(target) //nolint:gosec // target is the loopback-only handoff listener.
				if err == nil {
					defer response.Body.Close()
					if response.StatusCode != http.StatusOK || response.Request.URL.String() != consoleServer.URL+"/console" {
						err = fmt.Errorf("final browser response status=%d url=%s", response.StatusCode, response.Request.URL)
					}
				}
				browserResult <- err
			}()
			return nil
		})
	}()

	select {
	case <-consoleReached:
	case err := <-launchResult:
		t.Fatalf("browser handoff reported success before authenticated console confirmation: %v", err)
	}
	select {
	case err := <-launchResult:
		t.Fatalf("browser handoff reported success before confirmation completed: %v", err)
	default:
	}
	close(releaseConsole)
	if err := <-launchResult; err != nil {
		t.Fatal(err)
	}
	if err := <-browserResult; err != nil {
		t.Fatal(err)
	}
}

func TestLaunchBrowserSessionAllowsOnlyOneCorrelatedRaceWinner(t *testing.T) {
	var exchanges atomic.Int32
	consoleServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		exchanges.Add(1)
		http.SetCookie(writer, &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writer.WriteHeader(http.StatusCreated)
	}))
	defer consoleServer.Close()

	err := launchBrowserSession(context.Background(), consoleServer.URL, "single-use-test-bootstrap", func(_ context.Context, target string) error {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		start := make(chan struct{})
		statuses := make(chan int, 2)
		locations := make(chan string, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				response, requestErr := client.Get(target) //nolint:gosec // target is the loopback-only handoff listener.
				if requestErr != nil {
					statuses <- 0
					return
				}
				defer response.Body.Close()
				statuses <- response.StatusCode
				locations <- response.Header.Get("Location")
			}()
		}
		close(start)
		wait.Wait()
		close(statuses)
		close(locations)
		counts := map[int]int{}
		for status := range statuses {
			counts[status]++
		}
		if counts[http.StatusSeeOther] != 1 || counts[http.StatusNotFound] != 1 {
			t.Fatalf("correlated race statuses = %v", counts)
		}
		for location := range locations {
			parsed, parseErr := url.Parse(location)
			if parseErr != nil || parsed.Query().Get("browser_handoff") == "" {
				continue
			}
			confirmation, requestErr := client.Get(parsed.Query().Get("browser_handoff")) //nolint:gosec // confirmation is the exact loopback capability emitted by the handoff listener.
			if requestErr != nil {
				return requestErr
			}
			_ = confirmation.Body.Close()
			if confirmation.StatusCode != http.StatusSeeOther {
				return fmt.Errorf("confirmation status=%d", confirmation.StatusCode)
			}
			return nil
		}
		return errors.New("winning handoff did not emit a browser confirmation capability")
	})
	if err != nil {
		t.Fatal(err)
	}
	if exchanges.Load() != 1 {
		t.Fatalf("bootstrap exchanged %d times", exchanges.Load())
	}
}

func TestLaunchBrowserSessionRejectsUnsafeConsoleCookie(t *testing.T) {
	consoleServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.SetCookie(writer, &http.Cookie{Name: "aegis-console", Value: "unsafe-session", Path: "/console"})
		writer.WriteHeader(http.StatusCreated)
	}))
	defer consoleServer.Close()

	err := launchBrowserSession(context.Background(), consoleServer.URL, "single-use-test-bootstrap", func(_ context.Context, target string) error {
		response, requestErr := http.Get(target) //nolint:gosec // target is a loopback-only listener created by launchBrowserSession.
		if requestErr == nil {
			_ = response.Body.Close()
		}
		return requestErr
	})
	if err == nil || !strings.Contains(err.Error(), "denied browser session exchange") {
		t.Fatalf("unsafe session cookie was not denied: %v", err)
	}
}

func TestLaunchBrowserSessionRejectsCookiePolicyDowngrade(t *testing.T) {
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
	}{
		{name: "same-site-lax", cookie: &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteLaxMode}},
		{name: "unexpected-secure-on-http", cookie: &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}},
	} {
		t.Run(test.name, func(t *testing.T) {
			consoleServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				http.SetCookie(writer, test.cookie)
				writer.WriteHeader(http.StatusCreated)
			}))
			defer consoleServer.Close()

			err := launchBrowserSession(context.Background(), consoleServer.URL, "single-use-test-bootstrap", func(_ context.Context, target string) error {
				response, requestErr := http.Get(target) //nolint:gosec // target is the loopback-only handoff listener.
				if requestErr == nil {
					_ = response.Body.Close()
				}
				return requestErr
			})
			if err == nil || !strings.Contains(err.Error(), "denied browser session exchange") {
				t.Fatalf("cookie policy downgrade was not denied: %v", err)
			}
		})
	}
}

func TestLaunchBrowserSessionMarksBootstrapUnsafeWhenCancelledAfterClaim(t *testing.T) {
	claimed := make(chan struct{})
	release := make(chan struct{})
	consoleServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		close(claimed)
		<-release
		http.SetCookie(writer, &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		writer.WriteHeader(http.StatusCreated)
	}))
	defer func() {
		close(release)
		consoleServer.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	err := launchBrowserSession(ctx, consoleServer.URL, "single-use-test-bootstrap", func(_ context.Context, target string) error {
		go func() {
			response, requestErr := http.Get(target) //nolint:gosec // target is the loopback-only handoff listener.
			if requestErr == nil {
				_ = response.Body.Close()
			}
		}()
		<-claimed
		cancel()
		return nil
	})
	var handoffErr *browserSessionError
	if !errors.As(err, &handoffErr) || !handoffErr.bootstrapMayBeConsumed {
		t.Fatalf("claimed cancellation did not mark bootstrap as possibly consumed: %T %v", err, err)
	}
}

func TestLaunchBrowserSessionRejectsOriginsOutsidePlaintextLoopback(t *testing.T) {
	for _, origin := range []string{"https://127.0.0.1:18443", "http://console.example.test:18443"} {
		t.Run(origin, func(t *testing.T) {
			called := false
			err := launchBrowserSession(context.Background(), origin, "single-use-test-bootstrap", func(context.Context, string) error {
				called = true
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "plaintext loopback console") {
				t.Fatalf("unsafe origin was not denied: %v", err)
			}
			if called {
				t.Fatal("browser opened for an unsafe handoff origin")
			}
		})
	}
}

func consoleBootstrapFixture(t *testing.T, status int, body string) (string, string, func()) {
	return consoleBootstrapFixtureWithCookie(t, status, body, &http.Cookie{Name: "aegis-console", Value: "test-session", Path: "/console", HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func consoleBootstrapFixtureWithCookie(t *testing.T, status int, body string, sessionCookie *http.Cookie) (string, string, func()) {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	socketRoot, err := os.MkdirTemp("", "aegis-console-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	socket := filepath.Join(socketRoot, "a.sock")
	tokenValue := strings.Repeat("b", 64)
	tokenPath := filepath.Join(root, "api.token")
	if err = os.WriteFile(tokenPath, []byte(tokenValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	consoleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, consolePort, err := net.SplitHostPort(consoleListener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://" + net.JoinHostPort("localhost", consolePort)
	configPath := filepath.Join(root, "aegis.yaml")
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Local operator\n  uid: %q\n  user: %q\n  auth_ttl: 5m\napi:\n  token_file: %q\n  unix_socket: %q\n  console:\n    origin: %s\naudit:\n  checkpoint_dir: %q\n", filepath.Join(root, "state"), current.Uid, current.Username, tokenPath, socket, origin, filepath.Join(root, "checkpoints"))
	if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/console/bootstrap" || request.Header.Get("Authorization") != "Bearer "+tokenValue {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(status)
		_, _ = writer.Write([]byte(body))
	})}
	consoleServer := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && request.URL.Path == "/console" {
			cookie, err := request.Cookie("aegis-console")
			if err != nil || cookie.Value != "test-session" {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			if confirmation := request.URL.Query().Get("browser_handoff"); confirmation != "" {
				writer.Header().Set("Location", confirmation)
				writer.WriteHeader(http.StatusSeeOther)
				return
			}
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("authenticated console"))
			return
		}
		if request.Method != http.MethodPost || request.URL.Path != "/console/session" || request.Header.Get("Origin") != origin {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		var input struct {
			Bootstrap string `json:"bootstrap"`
		}
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil || input.Bootstrap != "single-use-test-bootstrap" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.SetCookie(writer, sessionCookie)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"csrf":"test-csrf","expires":"2030-01-01T00:00:00Z"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	go func() { _ = consoleServer.Serve(consoleListener) }()
	return configPath, origin, func() {
		_ = server.Close()
		_ = consoleServer.Close()
		_ = listener.Close()
		_ = consoleListener.Close()
	}
}
