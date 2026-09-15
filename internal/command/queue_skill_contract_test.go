package command

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/spf13/cobra"
)

// These assertions qualify distributed wire examples, not authority admission,
// runtime behavior, or installed-agent acceptance.
func TestQueueSkillBundledContracts(t *testing.T) {
	base := "../../skills/aegis-execution-queue/references/"
	var binding app.BindQueueRuntimeInput
	if err := decodeJSONFile(base+"bind-runtime.v1.json", &binding); err != nil {
		t.Fatal(err)
	}
	if binding.AgentID == "" || binding.QueueItemID == "" || binding.BindingID == "" || binding.TransitionID == "" || binding.Authority.ID == "" {
		t.Fatal("incomplete binding example")
	}
	var retry app.RetryQueueItemInput
	if err := decodeJSONFile(base+"reclaim.v1.json", &retry); err != nil {
		t.Fatal(err)
	}
	if !retry.Reclaimed || retry.ReasonCode != app.QueueReasonLeaseReclaimed || retry.Backoff != time.Second || retry.RetryID == "" || retry.TransitionID == "" || retry.QueueItemID != binding.QueueItemID || retry.Authority != binding.Authority {
		t.Fatal("incorrect reclaim contract")
	}
	for _, tc := range []struct{ operation, file string }{{"bind-runtime", "bind-runtime.v1.json"}, {"retry", "reclaim.v1.json"}} {
		t.Run(tc.operation, func(t *testing.T) {
			stop := errors.New("authenticated service boundary")
			calls := 0
			build := func(*cobra.Command) (*app.Service, error) { calls++; return nil, stop }
			cmd := fleetQueueCmd(build)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{tc.operation, base + tc.file})
			if err := cmd.Execute(); !errors.Is(err, stop) || calls != 1 {
				t.Fatalf("example did not reach service: calls=%d err=%v", calls, err)
			}
			raw, err := os.ReadFile(base + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			malformed := strings.Replace(string(raw), "{", `{"invented_authority":true,`, 1)
			path := filepath.Join(t.TempDir(), "unknown-field.json")
			if err := os.WriteFile(path, []byte(malformed), 0600); err != nil {
				t.Fatal(err)
			}
			cmd = fleetQueueCmd(build)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{tc.operation, path})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unknown field") || calls != 1 {
				t.Fatalf("unknown field crossed service boundary: calls=%d err=%v", calls, err)
			}
		})
	}
}
