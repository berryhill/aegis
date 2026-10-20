//go:build linux

package hermes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestInstalledNativeCodexExplicitSharedRoot(t *testing.T) {
	installation := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
	if installation == "" {
		t.Skip("explicit installed source qualification required")
	}
	d := core.RuntimeDescriptor{Version: "0.21.3", Installation: installation}
	if err := qualifyProviderTransport(d); err != nil {
		t.Fatal(err)
	}
	profile := nativeFixture(t)
	shared := filepath.Dir(filepath.Dir(profile))
	now := time.Now().UTC()
	token := syntheticProviderToken(now.Add(time.Hour))
	nativeStore(t, shared, token, []any{map[string]any{"auth_type": "oauth", "source": "device_code", "access_token": token}})
	before, err := os.ReadFile(filepath.Join(shared, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveNativeCodexLogin(context.Background(), d, "codex", shared, "synthetic-account", now)
	if err != nil || got.value != token {
		t.Fatal("explicit shared source resolution failed", err)
	}
	after, err := os.ReadFile(filepath.Join(shared, "auth.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("shared source modified", err)
	}
	// Explicit shared-root support does not enable implicit profile fallback.
	if _, err := resolveNativeCodexLogin(context.Background(), d, "codex", profile, "synthetic-account", now); err == nil {
		t.Fatal("profile silently borrowed shared-root credentials")
	}
}
