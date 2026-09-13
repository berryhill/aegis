package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/spf13/cobra"
)

// Normal submission must still go through the application service, not treat
// request-shape success as admission or silently turn malformed input into a
// preflight-only result.
func TestGraphSubmissionWithoutCheckRetainsServiceBoundary(t *testing.T) {
	called := false
	stop := errors.New("service boundary reached")
	cmd := fleetGraphsCmd(func(*cobra.Command) (*app.Service, error) {
		called = true
		return nil, stop
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"submit", "../../skills/aegis-graph-authoring/references/workspace-submit.v1.json"})
	if err := cmd.Execute(); !errors.Is(err, stop) || !called {
		t.Fatalf("normal submission bypassed service: called=%v error=%v", called, err)
	}
}

// This is decoder/Cobra evidence from the distributed example, not authority
// admission, service execution, or model behavior.
func TestGraphSubmissionCheckUsesBundledContractWithoutOpeningStore(t *testing.T) {
	raw, err := os.ReadFile("../../skills/aegis-graph-authoring/references/workspace-submit.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, want string
		edit       func(map[string]any)
	}{
		{"complete workspace", "", func(m map[string]any) {}},
		{"complete runtime", "", func(m map[string]any) {
			delete(m, "agent_id")
			m["authority"] = map[string]any{"schema_version": "aegis.reference.digest.v1", "id": "synthetic-authority", "digest": "sha256:" + strings.Repeat("b", 64)}
		}},
		{"mixed authority", "authority", func(m map[string]any) {
			m["authority"] = map[string]any{"schema_version": "aegis.reference.digest.v1", "id": "synthetic-authority", "digest": "sha256:" + strings.Repeat("b", 64)}
		}},
		{"missing transition", "transition_id", func(m map[string]any) { delete(m, "transition_id") }},
		{"invented rejection key", "unknown field", func(m map[string]any) { m["rejection_idempotency_key"] = "invented" }},
		{"forged workspace", "workspace", func(m map[string]any) { m["workspace"] = map[string]any{} }},
		{"missing selector", "authority", func(m map[string]any) { delete(m, "agent_id") }},
		{"invalid graph", "graph", func(m map[string]any) { m["graph"].(map[string]any)["revision"] = 0 }},
		{"unbounded attempts", "max_attempts", func(m map[string]any) { m["max_attempts"] = 1000000 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
			tc.edit(payload)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "submission.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			built := false
			cmd := fleetGraphsCmd(func(*cobra.Command) (*app.Service, error) {
				built = true
				return nil, errors.New("unexpected store construction")
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"submit", "--check", path})
			err = cmd.Execute()
			if built {
				t.Fatal("preflight opened a store")
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, err)
				}
				if strings.Contains(out.String(), `"status"`) {
					t.Fatal("failed check emitted success")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["status"] != "valid" || result["evidence_class"] != "request_shape_validation" || result["authority_admission"] != "not_run" || result["submitted"] != false {
				t.Fatalf("misleading check result: %s", out.String())
			}
		})
	}
}
