package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

// Real route, Unix peer authentication, persisted migration and exact readback.
// Bounded route fixture, not full Serve/installed acceptance. Synthetic profile
// and runtime only; the unchanged runtime gate must still deny.
func TestAgentProviderSetupOwningTransport(t *testing.T) {
	s := apiService(t)
	configureAPIFleet(t, s)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, ".hermes")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(marker, []byte("model: {provider: openai-codex, default: gpt-5.4, api_key: source-secret-canary}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.LocalHermesHome = func(string, string) (string, error) { return home, nil }
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	synthetic := *u
	synthetic.HomeDir = root
	s.Current = func() (*user.User, error) { return &synthetic, nil }

	s.ConfigFile = filepath.Join(root, "owner.json")
	raw, _ := json.Marshal(s.Config)
	if err := os.WriteFile(s.ConfigFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.PrepareLocalHermesAgentImportAs(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := s.ConfirmLocalHermesAgentImportAs(ctx, sub, p.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = apiHTTPErrorHandler(s, nil)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get("Authorization") != "Bearer "+s.Config.API.Token {
				return app.ErrUnauthenticated
			}
			uid, ok := c.Request().Context().Value(peerUIDKey{}).(uint32)
			if !ok {
				return app.ErrUnauthenticated
			}
			subject, err := s.AuthenticateUnixPeer(c.Request().Context(), uid)
			if err != nil {
				return err
			}
			c.Set("subject", subject)
			return next(c)
		}
	})
	registerControllerCodexRoutes(e.Group("/v1"), s)
	listener, err := net.Listen("unix", s.Config.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: e, ConnContext: unixPeerContext}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { server.Close(); <-done }()
	client := unixClient(s.Config.API.UnixSocket)
	call := func(body string, token string, want int) app.AgentProviderSetupResult {
		t.Helper()
		time.Sleep(300 * time.Millisecond)
		r, _ := http.NewRequest("POST", "http://unix/v1/agents/"+p.AgentID+"/providers", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if bytes.Contains(data, []byte("source-secret-canary")) {
			t.Fatal("private config leaked")
		}
		if response.StatusCode != want {
			t.Fatalf("status %d want %d: %s", response.StatusCode, want, data)
		}
		var out app.AgentProviderSetupResult
		if want == 200 && json.Unmarshal(data, &out) != nil {
			t.Fatal("response decode")
		}
		return out
	}
	call(`{"action":"models"}`, "wrong", 401)
	for _, body := range []string{`{"action":"status","action":"models"}`, `{"action":"models","approved_by":"forged"}`, `{"action":"approve","review_id":"x","charter_digest":"x","subject":{}}`} {
		call(body, s.Config.API.Token, 400)
	}
	before := call(`{"action":"status"}`, s.Config.API.Token, 200)
	if before.Agent.Revision.Digest != initial.Revision.Digest || before.Connection.Status != "not_connected" || before.Executable {
		t.Fatal("status overclaim")
	}
	preview := call(`{"action":"models"}`, s.Config.API.Token, 200)
	if preview.Review == nil {
		t.Fatal("review missing")
	}
	in := app.AgentProviderSetupInput{Action: "approve", ReviewID: preview.Review.ID, CharterDigest: preview.Review.Charter.Digest}
	request, _ := json.Marshal(in)
	applied := call(string(request), s.Config.API.Token, 200)
	if applied.Agent.Revision.Revision != 2 || applied.Models.ProviderAuthentication.Mode != core.ConfiguredProviderV1 || applied.Executable {
		t.Fatal("migration incorrect")
	}
	replay := call(string(request), s.Config.API.Token, 200)
	if replay.Agent.Revision.Digest != applied.Agent.Revision.Digest {
		t.Fatal("transport replay duplicated")
	}
	input, _ := json.Marshal(app.AgentProviderSetupInput{Action: "candidates", ProfileHome: home})
	call(string(input), s.Config.API.Token, 503) // synthetic 0.18.2 is unqualified; no auth.json read
	shown, err := s.GetFleetAgentAs(ctx, sub, p.AgentID, 2)
	if err != nil || shown.Revision.Digest != applied.Agent.Revision.Digest {
		t.Fatal("exact service readback differs")
	}
}
