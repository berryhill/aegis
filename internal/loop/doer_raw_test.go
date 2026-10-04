package loop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDoerExactBytesIsAdditiveAndDigestBound(t *testing.T) {
	text := "hello"
	legacy := DoerContract{Task: "Write hello", Workspace: "/approved/workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2}
	before, err := legacy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "exact_bytes") {
		t.Fatal("historical contract encoding changed")
	}
	var reloaded DoerContract
	if err = json.Unmarshal(wire, &reloaded); err != nil {
		t.Fatal(err)
	}
	after, err := reloaded.Digest()
	if err != nil || after != before {
		t.Fatal("historical digest did not round trip", err)
	}
	raw := legacy
	raw.ExactBytes = true
	pinned, err := raw.Digest()
	if err != nil || pinned == before {
		t.Fatal("raw assertion was not bound into contract digest", err)
	}
	if _, validation, err := NewDoerRevision("raw-doer", 1, "", raw); err != nil || validation.Outcome != ValidationValid {
		t.Fatal("raw candidate rejected", err)
	}
	raw.ExpectedText = nil
	if _, err := raw.Digest(); err == nil {
		t.Fatal("raw assertion without expected bytes accepted")
	}
}
