package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/spf13/cobra"
)

// Real constructor and Serve: no injected subject extractor, independent Console
// registry or startup principal. No runtime/provider or publication is invoked.
func TestPublicRootHTTPAuthenticationDenials(t *testing.T) {
	ctx := context.Background()
	cfg := config.Defaults().WithStateDir(filepath.Join(t.TempDir(), "state"))
	cfg.Principal.ID = "isolated-owner"
	cfg.Principal.Name = "Isolated owner"
	cfg.Principal.UID = fmt.Sprint(os.Getuid())
	cfg.Principal.User = os.Getenv("USER")
	port := os.Getenv("AEGIS_ACCEPTANCE_PORT")
	if port == "" {
		port = "0"
	}
	cfg.API.Listen = "127.0.0.1:" + port
	cfg.API.Console.Origin = "http://" + cfg.API.Listen
	cfg.API.Token = "isolated-denial-transport-token"
	cfg.API.UnixSocket = filepath.Join(filepath.Dir(cfg.StateDir), "api.sock")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := authoritybadger.Initialize(ctx, filepath.Join(cfg.StateDir, "persistence", "authority-v1"))
	must(err)
	path := filepath.Join(filepath.Dir(cfg.StateDir), "aegis.json")
	wire, err := json.Marshal(cfg)
	must(err)
	must(os.WriteFile(path, wire, 0600))
	var build func(*cobra.Command) (*app.Service, error)
	var closeService func() error
	root := newRoot(Dependencies{Out: io.Discard, Err: io.Discard}, func(b func(*cobra.Command) (*app.Service, error), c func() error) { build = b; closeService = c })
	must(root.PersistentFlags().Set("config", path))
	command := &cobra.Command{Use: "serve"}
	root.AddCommand(command)
	command.SetContext(ctx)
	s, err := build(command)
	must(err)
	t.Cleanup(func() { must(closeService()) })
	var clock atomic.Int64
	clock.Store(time.Now().UTC().UnixNano())
	s.Now = func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	verifier, err := principalauth.Enroll(cfg.Principal.ID, []byte("isolated-denial-password"))
	must(err)
	must(principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier))
	h := startContributionHTTPFixture(t, s)
	assertContributionHTTPDenials(t, h)
	original, err := h.Open(ctx, core.Subject{})
	must(err)
	clock.Add(int64(cfg.Principal.AuthTTL + time.Second))
	// Every request still obtains fresh SO_PEERCRED, but the old manager's hard
	// expiry cannot inherit that later authentication window.
	if _, err = h.Execute(ctx, core.Subject{}, original.ID, original.Token, "/contribute run https://github.com/fixture/controlled/issues/1"); err == nil {
		t.Fatal("expired original manager renewed")
	}
	fresh, err := h.Open(ctx, core.Subject{})
	must(err)
	if fresh.ID == original.ID || fresh.Token == original.Token {
		t.Fatal("fresh authentication reused manager")
	}
	h.stop()
	must(closeService())
	s, err = build(command)
	must(err)
	h = startContributionHTTPFixture(t, s)
	if _, err = h.Execute(ctx, core.Subject{}, fresh.ID, fresh.Token, "/contribute run https://github.com/fixture/controlled/issues/1"); err == nil {
		t.Fatal("old manager survived exact-store restart")
	}
	_, err = h.Open(ctx, core.Subject{})
	must(err)
	items, err := s.FleetRepository.ListQueueItems(ctx)
	must(err)
	if len(items) != 0 {
		t.Fatalf("denials created %d durable Queue items", len(items))
	}
}
