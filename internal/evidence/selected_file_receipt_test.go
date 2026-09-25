package evidence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/store"
)

func TestSelectedFileReceiptBindsActualBytesAndRechecksAtCompletion(t *testing.T) {
	workspace := t.TempDir()
	file := filepath.Join(workspace, "result.txt")
	if err := os.WriteFile(file, []byte(" hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	policy := SelectedFilePolicy{Version: SelectedFilePolicyV1, RelativePath: "result.txt", Mode: SelectedFileText, Text: "hello"}
	pin, _ := policy.Digest()
	binding := SelectedFileBinding{AttemptID: "attempt", ActionID: "verify", RunID: "loop-execution", OwnerID: "agent", AuthorityContextID: "authority", AuthorityContextDigest: "sha256:authority"}
	content := []byte(" hello\n")
	ref, err := blobs.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	artifact := RuntimeArtifact{ID: "artifact", AttemptID: binding.AttemptID, ActionID: binding.ActionID, RunID: binding.RunID, OwnerID: binding.OwnerID, AuthorityContextID: binding.AuthorityContextID, AuthorityContextDigest: binding.AuthorityContextDigest, Digest: ref, ContentRef: ref, MediaType: "application/octet-stream", CreatedAt: time.Now().UTC()}
	contract := "sha256:" + strings.Repeat("a", 64)
	receipt, proof, err := verifier.VerifySelectedFileArtifact(context.Background(), artifact, workspace, policy, pin, contract, binding)
	if err != nil || receipt.Outcome != Passed || receipt.ExpectedDigest != ref || !ValidateSelectedFileProvenance(proof, contract, artifact, []VerificationReceipt{receipt}) {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if _, err := verifier.ReloadReceipt(context.Background(), receipt.EvidenceRef); err != nil {
		t.Fatal(err)
	}
	if ValidateSelectedFileProvenance(proof, "sha256:"+strings.Repeat("b", 64), artifact, []VerificationReceipt{receipt}) {
		t.Fatal("substituted contract accepted")
	}
	if err := os.WriteFile(file, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if ValidateSelectedFileProvenance(proof, contract, artifact, []VerificationReceipt{receipt}) {
		t.Fatal("file changed after observation but completion still passed")
	}
}
