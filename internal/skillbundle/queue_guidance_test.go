package skillbundle

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Source-text coverage only; executable service/Badger mappings are retained in
// the Queue skill's evaluation ledger, not promoted by Evaluate.
func TestQueueGuidanceExactReadbackAndLifecycle(t *testing.T) {
	text := string(mustRead(t, filepath.Join(repositoryRoot(t), "skills/aegis-execution-queue/SKILL.md")))
	for _, required := range []string{
		"`source_kind`", "`source`", "`inputs`", "QueueExecutionView",
		"`failed` with reason `retry_exhausted`", "no `exhausted` state enum",
		"`max_attempts: 1`", "no generic retry/reclaim",
		"even after lease expiry", "workspace lifecycle branch",
		"online expire/exhaust", "online_operation_unsupported",
		"references/evaluation-ledger.v1.md",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("missing exact Queue guidance %q", required)
		}
	}
	if strings.Contains(text, "Cancelled, expired, exhausted, revoked") {
		t.Error("exhaustion presented as a state enum")
	}
}

func TestQueueSpecificEvaluationCases(t *testing.T) {
	var suite EvaluationSuite
	if err := json.Unmarshal(mustRead(t, filepath.Join(repositoryRoot(t), EvaluationsName)), &suite); err != nil {
		t.Fatal(err)
	}
	byID := map[string]EvaluationCase{}
	for _, c := range suite.Cases {
		byID[c.ID] = c
	}
	for id, signal := range map[string]string{
		"inspect-queue-direct-source": "source_kind",
		"deny-online-queue-expire":    "online_operation_unsupported",
		"deny-online-queue-exhaust":   "online_operation_unsupported",
		"explain-queue-exhaustion":    "retry_exhausted",
		"deny-two-node-reclaim":       "max_attempts: 1",
		"deny-expired-ordinary-retry": "reclaimed: false",
	} {
		c, ok := byID[id]
		if !ok || !strings.Contains(c.ExpectedSignal, signal) {
			t.Errorf("missing Queue evaluation %s / %s", id, signal)
		}
	}
}
