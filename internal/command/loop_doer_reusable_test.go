package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestReusableDoerDraftNeedsNoConfigurationOrTaskAuthority(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "reusable.json")
	input := doerReusableDraftInput{AgentID: "existing-agent", LoopID: "reusable", Revision: 1, IdempotencyKey: "draft", DoerReusable: loop.DoerReusableContract{MaxAttempts: 2}}
	wire, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, wire, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := NewRoot(Dependencies{Out: &out, Err: &bytes.Buffer{}})
	cmd.SetArgs([]string{"loops", "doer-reusable", source, "--config", filepath.Join(root, "absent.yaml")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var publication app.PublishLoopInput
	if err := json.Unmarshal(out.Bytes(), &publication); err != nil {
		t.Fatal(err)
	}
	if publication.Revision.SchemaVersion != loop.DoerReusableSchemaVersion || publication.Revision.Doer != nil || publication.Revision.DoerReusable == nil || publication.Authority.Digest != "" || publication.Publisher.Digest != "" {
		t.Fatalf("draft granted authority or lost template: %+v", publication)
	}
}
