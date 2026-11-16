//go:build linux

package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// Run only inside the explicit networkless native harness. The synthetic
// qualifier is a test seam, NOT a new production qualification record. Native
// gateway/AIAgent/SDK and the public installation launcher remain real.
func TestModernNativeAttemptIntegration(t *testing.T) {
	root := os.Getenv("AEGIS_TEST_MODERN_NATIVE_EVIDENCE")
	if root == "" {
		t.Skip("networkless native harness required")
	}
	launcher := os.Getenv("AEGIS_TEST_MODERN_NATIVE_LAUNCHER")
	if launcher == "" {
		t.Fatal("exact published launcher required")
	}
	cases := strings.Split(os.Getenv("AEGIS_TEST_MODERN_NATIVE_CASES"), ",")
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			state := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Join(state, "state/runtime"), 0700); err != nil {
				t.Fatal(err)
			}
			fixtureCase := name
			if name == "output" {
				fixtureCase = "complete"
			}
			if err := os.WriteFile(filepath.Join(state, "state/runtime/case"), []byte(fixtureCase), 0600); err != nil {
				t.Fatal(err)
			}
			a := New(launcher, slog.New(slog.NewTextHandler(io.Discard, nil)))
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			d, err := a.Discover(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = qualifyProviderTransport(d); !errors.Is(err, ErrProviderAuthRuntimeUnqualified) {
				t.Fatal("modern production gate unexpectedly open", err)
			}
			a.providerTransportQualifier = func(got core.RuntimeDescriptor) error {
				if got.Installation != d.Installation || got.Version != d.Version {
					return ErrProviderAuthRuntimeUnqualified
				}
				return nil
			}
			r := providerAttempt(state)
			h := r.Launch.Mandate.Hermes
			h.Provider = "openai-codex"
			h.Model = "gpt-5.3-codex"
			r.Launch.Mandate.Hermes = h
			r.Launch.AuthorityContext.Authority.Hermes = h
			r.Model = h.Model
			r.Provider = h.Provider
			r.Launch.Mandate.Runtime = d
			r.Launch.AuthorityContext.Runtime = d
			if name == "expire" {
				expiry := time.Now().UTC().Add(22 * time.Second)
				r.Launch.Mandate.ExpiresAt = expiry
				r.Launch.AuthorityContext.ExpiresAt = expiry
			}
			r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			r.Bounds.Duration = 30 * time.Second
			r.Bounds.OutputBytes = 1024
			if name == "output" {
				r.Bounds.OutputBytes = 4
			}
			if name == "cancel" {
				r.SessionCapability = func(context.Context, execution.Turn) error {
					go func() {
						tick := time.NewTicker(25 * time.Millisecond)
						defer tick.Stop()
						for {
							select {
							case <-ctx.Done():
								return
							case <-tick.C:
							}
							data, _ := os.ReadFile(filepath.Join(state, "state/runtime/wire.json"))
							if strings.Contains(string(data), "codex_responses") {
								cancel()
								return
							}
						}
					}()
					return nil
				}
			}
			expiry := time.Now().Add(time.Hour)
			if name == "token-expire" {
				expiry = time.Now().Add(142 * time.Second)
			}
			token := syntheticProviderToken(expiry)
			a.SetProviderAuthenticationResolver(func(context.Context, core.HermesConfig) (ProviderAccessToken, error) {
				return NewProviderAccessToken(h.Provider, token, time.Now())
			})
			start := time.Now()
			result, err := a.AttemptTurn(ctx, r)
			success := err == nil
			wantSuccess := name == "complete" || name == "tool" || name == "tool-text"
			evidence := map[string]any{"case": name, "scope": "native AttemptTurn with synthetic qualifier and vendor boundary; NOT production qualification", "state": result.Attempt.State, "reason": result.Attempt.Reason, "success": success, "elapsed_ms": time.Since(start).Milliseconds(), "output_bytes": len(result.Output), "runtime_version": d.Version, "production_gate_closed": true}
			if err != nil {
				evidence["error"] = err.Error()
			}
			data, _ := json.MarshalIndent(evidence, "", "  ")
			if err := os.WriteFile(filepath.Join(state, "adapter.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if success != wantSuccess {
				t.Errorf("success=%v want=%v reason=%s error=%v", success, wantSuccess, result.Attempt.Reason, err)
			}
			if name == "complete" && success && result.Output != "SYNTHETIC_OK" {
				t.Error("unexpected native text")
			}
			if !success && result.Output != "" {
				t.Error("failure leaked response")
			}
			if strings.Contains(string(data), token) {
				t.Error("token disclosure")
			}
			if name == "output" && !errors.Is(err, ErrAttemptOutputBound) {
				t.Error("output bound not enforced", err)
			}
			if name == "cancel" && result.Attempt.State != execution.StateCancelled {
				t.Error("cancel disposition", err)
			}
			if name == "expire" && result.Attempt.State != execution.StateExpired {
				t.Error("expiry disposition", err)
			}
			entries, readErr := os.ReadDir(filepath.Join(state, "state/runtime"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, e := range entries {
				if e.IsDir() && strings.HasPrefix(e.Name(), "attempt-") {
					t.Error("disposable home retained")
				}
			}
			wire, readErr := os.ReadFile(filepath.Join(state, "state/runtime/wire.json"))
			if readErr != nil {
				t.Fatal("native gateway did not reach vendor boundary", readErr)
			}
			var w struct {
				Requests  []map[string]any `json:"requests"`
				Network   int              `json:"network_attempts"`
				Init      int              `json:"agent_init"`
				Run       int              `json:"agent_run"`
				PID       int              `json:"pid"`
				Forbidden int              `json:"forbidden_effect_attempts"`
			}
			if err = json.Unmarshal(wire, &w); err != nil {
				t.Fatal(err)
			}
			if w.Network != 0 || w.Init == 0 || w.Run == 0 || len(w.Requests) == 0 || len(w.Requests) > 4 {
				t.Errorf("native path/bounds invalid: %+v", w)
			}
			if w.Forbidden != 0 || w.PID <= 0 || !errors.Is(syscall.Kill(w.PID, 0), syscall.ESRCH) {
				t.Error("forbidden effect or surviving gateway")
			}
			for _, req := range w.Requests {
				if req["bearer_exact"] != true || req["account_exact"] != true {
					t.Error("account drift")
				}
			}
		})
	}
}
