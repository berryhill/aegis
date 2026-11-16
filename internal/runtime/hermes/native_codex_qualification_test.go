package hermes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// A general eligible runtime is not an access-only transport qualification.
// In particular, the current native stream assembler can report complete after
// EOF without response.completed. Keep custody and launch behind qualification.
func TestProviderTransportUnqualifiedBeforeCustodyAndLaunch(t *testing.T) {
	for _, version := range []string{"0.23.0", "0.21.3"} {
		t.Run(version, func(t *testing.T) {
			a, root := attemptTestAdapter(t, "#!/bin/sh\ntouch gateway-started\nexit 99\n")
			d, err := a.Discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Metadata fixture only: neither version nor Python presence supplies
			// the qualified source bytes. Never inject a passing qualifier here.
			if err := os.WriteFile(a.executable, []byte(fmt.Sprintf("#!/bin/sh\nprintf 'Hermes Agent v%s\\nInstall directory: %s\\n'\n", version, d.Installation)), 0700); err != nil {
				t.Fatal(err)
			}
			r := providerAttempt(root)
			r.Bounds.Duration = 5 * time.Second
			r.Launch.Mandate.Runtime.Version = version
			r.Launch.AuthorityContext.Runtime.Version = version
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			called := false
			a.SetProviderAuthenticationResolver(func(context.Context, core.HermesConfig) (ProviderAccessToken, error) {
				called = true
				return ProviderAccessToken{}, ErrProviderAuthAbsent
			})
			result, err := a.AttemptTurn(context.Background(), r)
			if !errors.Is(err, ErrProviderAuthRuntimeUnqualified) || called || result.Output != "" || result.Attempt.StartedAt != nil {
				t.Fatalf("unqualified runtime reached custody/launch: err=%v called=%v", err, called)
			}
			entries, err := os.ReadDir(filepath.Join(r.StateRoot, "runtime"))
			if err != nil || len(entries) != 0 {
				t.Fatal("unqualified attempt home not cleaned")
			}
		})
	}
}

// Opt-in acceptance, NOT a fake AIAgent or a source-gate bypass. This exercises
// the actual installed gateway + AIAgent + SDK in a network/mount namespace.
// It intentionally remains RED for a runtime that accepts a truncated stream.
// Downstream AttemptTurn qualification must not be enabled to make it green.
func TestNativeCodexGatewayRequiresTerminalStream(t *testing.T) {
	source, evidence := os.Getenv("AEGIS_TEST_NATIVE_CODEX_SOURCE"), os.Getenv("AEGIS_TEST_NATIVE_CODEX_EVIDENCE")
	if source == "" || evidence == "" {
		t.Skip("explicit installed source and private retained evidence root required")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Fatal("network/mount namespace prerequisite unavailable")
	}
	parent, err := os.MkdirTemp(evidence, "terminal-regression-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "testdata/native_codex_gateway_probe.py", source,
		filepath.Join(parent, "evidence"), "--bounded-config", "--cases", "complete", "truncated", "--require-terminal")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("native terminal-stream acceptance failed (retained %s): %v\n%s", parent, err, out)
	}
	t.Logf("native terminal outcomes passed; this is not whole transport qualification; evidence %s", parent)
}
