package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/loop"
)

func TestHelloDraftCLI(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "input.json"), filepath.Join(dir, "draft.json")
	input := helloDraftInput{AgentID: "existing-agent", LoopID: "hello", Revision: 1, IdempotencyKey: "hello-one"}
	run := func(raw []byte, dest string) error {
		t.Helper()
		if err := os.WriteFile(source, raw, 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		root := NewRoot(Dependencies{Out: &out, Err: &out})
		root.SetArgs([]string{"loops", "hello", source, "--output", dest})
		return root.Execute()
	}
	raw, _ := json.Marshal(input)
	if err := run(raw, destination); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(destination)
	if err := run(raw, destination); err == nil {
		t.Fatal("overwrote existing output")
	}
	after, _ := os.ReadFile(destination)
	if !bytes.Equal(first, after) {
		t.Fatal("existing output changed")
	}
	var publication app.PublishLoopInput
	if err := json.Unmarshal(first, &publication); err != nil {
		t.Fatal(err)
	}
	canonical, validation, err := loop.NewRevision(publication.Revision)
	if err != nil || validation.Outcome != loop.ValidationValid || canonical.Digest != publication.Revision.Digest || canonical.SchemaVersion != loop.RevisionSchemaVersion {
		t.Fatalf("invalid canonical draft: %v", err)
	}
	actions := 0
	for _, step := range canonical.Steps {
		if step.Kind == loop.StepAction {
			actions++
			if len(step.EvidenceClaims) != 1 {
				t.Fatal("missing claim")
			}
			c := step.EvidenceClaims[0]
			if c.ExpectedDigest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("hello"))) || c.VerifierID != evidence.ArtifactVerifierID || c.PolicyVersion != evidence.VerifierPolicyV1 || c.MediaType != "text/plain" || len(canonical.RequiredEvidence) != 1 || canonical.RequiredEvidence[0].ProducerStepID != step.ID || canonical.RequiredEvidence[0].Claim != c.Claim {
				t.Fatal("evidence readiness mismatch")
			}
		}
	}
	if actions != 1 {
		t.Fatal("not single action")
	}
	input.PreviousDigest = canonical.Digest
	input.Revision = 2
	input.IdempotencyKey = "hello-two"
	raw, _ = json.Marshal(input)
	successor := filepath.Join(dir, "successor.json")
	if err := run(raw, successor); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(successor)
	if err := json.Unmarshal(data, &publication); err != nil {
		t.Fatal(err)
	}
	if publication.ExpectedPreviousDigest != canonical.Digest || publication.Revision.PreviousDigest != canonical.Digest {
		t.Fatal("predecessor lost")
	}
	for _, invalid := range []string{`{}`, `{"agent_id":"a","loop_id":"hello","revision":2,"idempotency_key":"x"}`, `{"agent_id":"a","loop_id":"hello","revision":1,"idempotency_key":"x","unknown":true}`, `{} {}`} {
		dest := filepath.Join(dir, "invalid.json")
		if err := run([]byte(invalid), dest); err == nil {
			t.Fatalf("accepted %s", invalid)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatal("invalid input created output")
		}
	}
}
