package implementation

import (
	"bytes"
	"context"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestEngineeringSourcePolicyPreservesLimitsAndSecretDenial(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	policy := loop.SourceInventoryPolicy{MaxFiles: 32, MaxFileBytes: 2 << 20, MaxTotalBytes: 16 << 20}
	for i := 0; i < 9; i++ {
		if err := os.WriteFile(filepath.Join(root, string(rune('a'+i))+".go"), bytes.Repeat([]byte("x"), 1<<20), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Inventory(ctx, root); err == nil {
		t.Fatal("legacy source budget widened")
	}
	if _, err := InventoryWithPolicy(ctx, root, &policy); err != nil {
		t.Fatal(err)
	}
	fixture := []byte("package fixture\n// " + "-----BEGIN " + "PRIVATE " + "KEY-----" + " synthetic incomplete fixture\n")
	target := filepath.Join(root, "fixture_test.go")
	if err := os.WriteFile(target, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryWithPolicy(ctx, root, &policy); err == nil {
		t.Fatal("unapproved heading admitted")
	}
	policy.SyntheticFixtures = []loop.SourceFixtureIdentity{{Path: "fixture_test.go", Digest: digest(fixture)}}
	if _, err := InventoryWithPolicy(ctx, root, &policy); err != nil {
		t.Fatal(err)
	}
	changed := append(append([]byte{}, fixture...), []byte("// drift\n")...)
	if err := os.WriteFile(target, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InventoryWithPolicy(ctx, root, &policy); err == nil {
		t.Fatal("fixture digest drift admitted")
	}
	realBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("deny any complete encoded key block")})
	if err := os.WriteFile(target, realBlock, 0600); err != nil {
		t.Fatal(err)
	}
	policy.SyntheticFixtures[0].Digest = digest(realBlock)
	if _, err := InventoryWithPolicy(ctx, root, &policy); err == nil {
		t.Fatal("approved fixture permitted complete private key block")
	}
}

func TestEngineeringActualCompleteSourcePolicy(t *testing.T) {
	root := os.Getenv("AEGIS_ENGINEERING_COMPLETE_SOURCE")
	if root == "" {
		root = actualEngineeringSourceCopy(t)
	}
	policy := loop.SourceInventoryPolicy{MaxFiles: 4096, MaxFileBytes: 2 << 20, MaxTotalBytes: 24 << 20}
	// This test constructs an explicit definition-time policy from the independent
	// frozen fixture identities. Production never auto-approves discovered text.
	for _, p := range []string{"internal/command/doer_companion_test.go", "internal/delivery/objects_test.go", "internal/delivery/publication_privacy_test.go", "internal/manager/contracts_test.go"} {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		policy.SyntheticFixtures = append(policy.SyntheticFixtures, loop.SourceFixtureIdentity{Path: p, Digest: digest(b)})
	}
	source, err := InventoryWithPolicy(context.Background(), root, &policy)
	if err != nil {
		t.Fatal(err)
	}
	var total int
	for _, f := range source.Files {
		total += len(f.Content)
	}
	if len(source.Files) < 1800 || total <= 8<<20 {
		t.Fatal("not actual complete Aegis scale")
	}
	t.Logf("complete_files=%d source_bytes=%d digest=%s", len(source.Files), total, source.Digest)
	if _, err = Inventory(context.Background(), root); err == nil {
		t.Fatal("legacy denial lost")
	}
}
