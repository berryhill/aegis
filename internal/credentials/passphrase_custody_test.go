package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPassphraseCustodyEncryptsAndUnlocksKEK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.kek.enc")
	passphrase := []byte("correct horse battery staple")
	if err := CreatePassphraseKey(path, "authority-kek", passphrase); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, passphrase) || bytes.Contains(stored, []byte(`"key"`)) {
		t.Fatal("encrypted credential contains passphrase or plaintext key field")
	}
	var envelope map[string]any
	if err = json.Unmarshal(stored, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["kdf"] != "argon2id" || envelope["ciphertext"] == "" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
	custodian, err := LoadPassphraseCustodian(path, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	defer custodian.Close()
	if err = custodian.ActiveKEK(context.Background(), func(metadata KEKMetadata, key []byte) error {
		if metadata.ID != "authority-kek" || metadata.Version != 1 || len(key) != 32 {
			t.Fatalf("metadata=%+v key length=%d", metadata, len(key))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPassphraseCustodian(path, []byte("wrong passphrase value")); err == nil || !strings.Contains(err.Error(), "could not be unlocked") {
		t.Fatalf("wrong passphrase error=%v", err)
	}
}

func TestPassphraseCustodyRejectsWeakInputAndTampering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.kek.enc")
	if err := CreatePassphraseKey(path, "authority-kek", []byte("short")); err == nil {
		t.Fatal("short passphrase accepted")
	}
	passphrase := []byte("long enough test passphrase")
	if err := CreatePassphraseKey(path, "authority-kek", passphrase); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err = json.Unmarshal(stored, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["ciphertext"] = "AAAAAAAAAAAAAAAAAAAAAA=="
	tampered, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadPassphraseCustodian(path, passphrase); err == nil {
		t.Fatal("tampered envelope accepted")
	}
}
