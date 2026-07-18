package bbolt

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/credentials"
)

type testCustodian struct {
	id  string
	key []byte
}

func newCustodian(t *testing.T, id string) *testCustodian {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return &testCustodian{id: id, key: key}
}
func (c *testCustodian) ActiveKEK(ctx context.Context, fn func(credentials.KEKMetadata, []byte) error) error {
	return fn(credentials.KEKMetadata{ID: c.id, Version: 1}, append([]byte(nil), c.key...))
}
func (c *testCustodian) KEK(ctx context.Context, id string, version uint64, fn func([]byte) error) error {
	if id != c.id || version != 1 {
		return errors.New("missing key")
	}
	return fn(append([]byte(nil), c.key...))
}

func openAuthority(t *testing.T) (*Store, *credentials.Authority, *testCustodian, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "authority.db")
	custodian := newCustodian(t, "test-kek")
	store, err := Open(context.Background(), path, "deployment-test", custodian)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, credentials.NewAuthority(store, custodian), custodian, path
}

func TestAuthorityCreateBindUseRotateRevokeAndNoPlaintextPersistence(t *testing.T) {
	store, authority, _, path := openAuthority(t)
	ctx := context.Background()
	firstSecret := []byte("first-secret-value-never-persist-plaintext")
	record, err := authority.Create(ctx, "provider/test", "api-token", "principal-1", firstSecret)
	if err != nil {
		t.Fatal(err)
	}
	records, err := authority.List(ctx, "provider", 10)
	if err != nil || len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("metadata list/search mismatch: %#v %v", records, err)
	}
	if records, err = authority.List(ctx, "not-present", 10); err != nil || len(records) != 0 {
		t.Fatalf("metadata search should be empty: %#v %v", records, err)
	}
	binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: "agent-1", StanzaID: "principal", DeploymentID: "deployment-test", Scope: "provider:test"}, SecretRecord: record.ID, VersionPolicy: credentials.VersionCurrent, Mode: "brokered", Destinations: []string{"api.example.test"}, Enabled: true}
	if err = authority.Bind(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err = authority.Bind(ctx, binding); !errors.Is(err, credentials.ErrConflict) {
		t.Fatalf("duplicate binding error = %v", err)
	}
	var used string
	if err = authority.Use(ctx, binding.Key, "api.example.test", func(value []byte) error { used = string(value); return nil }); err != nil {
		t.Fatal(err)
	}
	if used != string(firstSecret) {
		t.Fatal("resolved secret mismatch")
	}
	if err = authority.Use(ctx, binding.Key, "other.example.test", func([]byte) error { return nil }); !errors.Is(err, credentials.ErrRevoked) {
		t.Fatalf("wrong destination error = %v", err)
	}

	secondSecret := []byte("second-secret-value-never-persist-plaintext")
	record, err = authority.Rotate(ctx, record.ID, secondSecret)
	if err != nil || record.CurrentVersion != 2 {
		t.Fatalf("rotate: record=%+v err=%v", record, err)
	}
	if err = authority.Use(ctx, binding.Key, "api.example.test", func(value []byte) error { used = string(value); return nil }); err != nil || used != string(secondSecret) {
		t.Fatalf("current binding did not resolve rotated value: %v", err)
	}
	if err = authority.Revoke(ctx, record.ID, 2, "key-compromise"); err != nil {
		t.Fatal(err)
	}
	if err = authority.Use(ctx, binding.Key, "api.example.test", func([]byte) error { return nil }); !errors.Is(err, credentials.ErrRevoked) {
		t.Fatalf("revoked current version error = %v", err)
	}

	if err = store.db.Sync(); err != nil {
		t.Fatal(err)
	}
	database, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if containsBytes(database, firstSecret) || containsBytes(database, secondSecret) {
		t.Fatal("authority database contains plaintext secret")
	}
}

func TestAuthorityRejectsWrongKeyDeploymentModeAndSecondWriter(t *testing.T) {
	store, _, _, path := openAuthority(t)
	if _, err := Open(context.Background(), path, "another-deployment", newCustodian(t, "test-kek")); err == nil {
		t.Fatal("second writer or wrong deployment was accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := newCustodian(t, "wrong-kek")
	if _, err := Open(context.Background(), path, "deployment-test", wrong); err == nil {
		t.Fatal("wrong key-encryption key was accepted")
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path, "deployment-test", wrong); err == nil {
		t.Fatal("unsafe authority mode was accepted")
	}
}

func TestAuthorityRejectsUnsafeDirectoryAndSymlinkComponents(t *testing.T) {
	custodian := newCustodian(t, "test-kek")
	unsafeDirectory := filepath.Join(t.TempDir(), "unsafe")
	if err := os.Mkdir(unsafeDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(unsafeDirectory, "authority.db"), "deployment-test", custodian); err == nil {
		t.Fatal("unsafe authority directory mode was accepted")
	}

	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(linkedDirectory, "nested", "authority.db"), "deployment-test", custodian); err == nil {
		t.Fatal("symlinked authority path component was accepted")
	}
}

func TestConcurrentReadersSerializedRotationAndBackupRestore(t *testing.T) {
	store, authority, custodian, _ := openAuthority(t)
	ctx := context.Background()
	record, err := authority.Create(ctx, "provider/test", "api-token", "principal-1", []byte("initial"))
	if err != nil {
		t.Fatal(err)
	}
	binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: "agent-1", StanzaID: "principal", DeploymentID: "deployment-test", Scope: "provider:test"}, SecretRecord: record.ID, VersionPolicy: credentials.VersionCurrent, Mode: "brokered", Destinations: []string{"api.example.test"}, Enabled: true}
	if err = authority.Bind(ctx, binding); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for read := 0; read < 20; read++ {
				if useErr := authority.Use(ctx, binding.Key, "api.example.test", func(value []byte) error {
					if len(value) == 0 {
						return errors.New("empty")
					}
					return nil
				}); useErr != nil {
					t.Errorf("concurrent use: %v", useErr)
					return
				}
			}
		}()
	}
	if _, err = authority.Rotate(ctx, record.ID, []byte("rotated")); err != nil {
		t.Fatal(err)
	}
	wait.Wait()

	backup := filepath.Join(t.TempDir(), "secure", "backup.db")
	if err = authority.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, backup, "deployment-test", custodian)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	metadata, err := restored.Metadata(ctx, record.ID)
	if err != nil || metadata.CurrentVersion != 2 {
		t.Fatalf("restored metadata = %+v, %v", metadata, err)
	}
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for index := 0; index <= len(haystack)-len(needle); index++ {
		match := true
		for offset := range needle {
			if haystack[index+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func FuzzStrictDecode(f *testing.F) {
	f.Add([]byte(`{"id":"secret-test"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maximumValue+1 {
			t.Skip()
		}
		var record credentials.SecretRecord
		_ = decode(input, &record)
	})
}
