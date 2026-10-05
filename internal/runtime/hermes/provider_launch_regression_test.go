package hermes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestProviderTokenSelectorMargin(t *testing.T) {
	now := time.Now().UTC()
	for _, ttl := range []time.Duration{30 * time.Second, 60 * time.Second, 119 * time.Second, 120 * time.Second} {
		if _, err := NewProviderAccessToken("codex", syntheticProviderToken(now.Add(ttl)), now); !errors.Is(err, ErrProviderAuthExpired) {
			t.Fatalf("ttl %v: %v", ttl, err)
		}
	}
	if _, err := NewProviderAccessToken("codex", syntheticProviderToken(now.Add(121*time.Second)), now); err != nil {
		t.Fatal(err)
	}
}

func TestProviderTransportQualificationFailsClosed(t *testing.T) {
	for _, version := range []string{"0.18.2", "0.21.3", "0.22.0"} {
		d := core.RuntimeDescriptor{Version: version, Installation: t.TempDir()}
		if !errors.Is(qualifyProviderTransport(d), ErrProviderAuthRuntimeUnqualified) {
			t.Fatal("missing transport admitted", version)
		}
	}
	// General legacy discovery remains accepted independently of provider transport.
	if !SupportedVersion("0.18.2") {
		t.Fatal("legacy admission globally narrowed")
	}
}

func TestProviderLaunchCancellationExpiryAndProbeRevocation(t *testing.T) {
	for _, kind := range []string{"cancel", "expiry", "revoked-during-probe"} {
		t.Run(kind, func(t *testing.T) {
			probe := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while read line; do :; done
`
			a, root := attemptTestAdapter(t, probe)
			marker := filepath.Join(root, "probe-ran")
			probe = strings.Replace(probe, "read tools", "touch '"+marker+"'\nread tools", 1)
			if err := os.WriteFile(filepath.Join(root, "install", "venv", "bin", "python"), []byte(probe), 0700); err != nil {
				t.Fatal(err)
			}
			startMarker := filepath.Join(root, "runtime-started")
			script := "#!/bin/sh\nif [ \"$1\" = --version ]; then\necho 'Hermes Agent v0.18.2'\necho 'Install directory: " + filepath.Join(root, "install") + "'\nexit 0\nfi\n[ -s \"$HERMES_HOME/auth.json\" ] || exit 70\ntouch '" + startMarker + "'\nwhile read line; do :; done\n"
			if err := os.WriteFile(a.executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil } // Does not qualify a real runtime.
			a.SetProviderAuthenticationResolver(func(_ context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
				return NewProviderAccessToken(h.Provider, syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
			})
			request := providerAttempt(root)
			m, authority := request.Launch.Mandate, request.Launch.AuthorityContext
			if kind == "expiry" {
				m.ExpiresAt = time.Now().Add(900 * time.Millisecond)
				authority.ExpiresAt = m.ExpiresAt
				authority.Digest = core.AuthorityContextDigest(authority)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			fresh := func(context.Context) error {
				calls++
				if kind == "revoked-during-probe" {
					if _, err := os.Stat(marker); err == nil {
						return errors.New("revoked")
					}
				}
				return nil
			}
			id, home, _, _, err := a.Launch(ctx, root, m, authority, nil, BrokerBridge{}, fresh)
			if kind == "revoked-during-probe" {
				if !errors.Is(err, ErrProviderAuthUnauthorized) || calls < 2 {
					t.Fatalf("revocation was not freshly admitted: %d %v", calls, err)
				}
				if _, err := os.Stat(startMarker); !os.IsNotExist(err) {
					t.Fatal("revoked runtime started")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if kind == "cancel" {
					cancel()
				}
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					_, err := os.Stat(home)
					if !a.Alive(id) && os.IsNotExist(err) {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatal("token-bearing process or disposable home survived cutoff")
			}
			dirs, err := os.ReadDir(filepath.Join(root, "runtime"))
			if err != nil || len(dirs) != 0 {
				t.Fatal("disposable home retained", err)
			}
		})
	}
}

// Opt-in actual installed source qualification. This launches only an offline
// Python parser/pool probe, using synthetic JWTs and a disposable home.
func TestInstalledProviderTransportSynthetic(t *testing.T) {
	root := os.Getenv("AEGIS_TEST_PROVIDER_HERMES_INSTALL")
	if root == "" {
		t.Skip("set AEGIS_TEST_PROVIDER_HERMES_INSTALL for offline installed-source qualification")
	}
	d := core.RuntimeDescriptor{Version: "0.21.3", Installation: root}
	if err := qualifyProviderTransport(d); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	home := t.TempDir()
	token, err := NewProviderAccessToken("codex", syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err = writeProviderAuthentication(home, token); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, gatewayPython(d), "testdata/provider_transport_probe.py", root, home)
	cmd.Env = providerAuthenticationEnv(minimalEnv(t.TempDir(), nil))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("offline probe: %v %s", err, output)
	}
	t.Log(string(output))
}
