package execution_test

import (
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/reference"
)

func TestNativeCommandMetadataCompatibility(t *testing.T) {
	for _, env := range [][]string{nil, {}, {"KEY=value", "OTHER=two"}} {
		cmd := exec.Command("/qualified/go", "test", "./...")
		cmd.Dir = "/qualified/source"
		cmd.Env = env
		raw, err := json.Marshal(struct {
			Path string
			Args []string
			Dir  string
			Env  []string
		}{cmd.Path, cmd.Args, cmd.Dir, cmd.Env})
		if err != nil {
			t.Fatal(err)
		}
		if got := execution.NativeCommandDigest(cmd); got != contribution.Digest(raw) {
			t.Fatalf("canonical digest changed: %s", got)
		}
	}
	if execution.NativeCommandDigest(nil) != "" {
		t.Fatal("nil command digest")
	}
	original := contribution.NativeBinding{IntentID: "intent", RunID: "run", QueueItemID: "queue", ClaimID: "claim", AttemptID: "attempt", Role: "builder", Epoch: 2, Authority: reference.DigestRef{ID: "authority"}, Candidate: "candidate", PolicyDigest: "policy", CommandDigest: "command"}
	var neutral execution.NativeBinding = original
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(neutral)
	if string(a) != string(b) {
		t.Fatal("wire identity changed")
	}
	if !errors.Is(execution.ErrNativeQuiescenceUnavailable, contribution.ErrNativeQuiescenceUnavailable) {
		t.Fatal("sentinel identity changed")
	}
}
