package credentials

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
)

type memoryCustodian struct {
	metadata KEKMetadata
	key      []byte
}

func newMemoryCustodian(t *testing.T, id string) *memoryCustodian {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return &memoryCustodian{metadata: KEKMetadata{ID: id, Version: 1}, key: key}
}

func (c *memoryCustodian) ActiveKEK(ctx context.Context, fn func(KEKMetadata, []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(c.metadata, append([]byte(nil), c.key...))
}
func (c *memoryCustodian) KEK(ctx context.Context, id string, version uint64, fn func([]byte) error) error {
	if id != c.metadata.ID || version != c.metadata.Version {
		return errors.New("missing key")
	}
	return fn(append([]byte(nil), c.key...))
}

func TestEnvelopeEncryptionAuthenticatesEveryContext(t *testing.T) {
	ctx := context.Background()
	custodian := newMemoryCustodian(t, "test-kek")
	plaintext := []byte("credential-value-that-must-not-leak")
	encrypted, err := Encrypt(ctx, custodian, "store-test", "secret-test", 1, "api-token", plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted.Ciphertext) == string(plaintext) {
		t.Fatal("plaintext was not encrypted")
	}
	var used []byte
	if err = Decrypt(ctx, custodian, "store-test", "api-token", encrypted, func(value []byte) error {
		used = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(used) != string(plaintext) {
		t.Fatal("decrypted value mismatch")
	}

	mutations := []func(*EncryptedSecretVersion){
		func(value *EncryptedSecretVersion) { value.RecordID = "secret-other" },
		func(value *EncryptedSecretVersion) { value.Version++ },
		func(value *EncryptedSecretVersion) { value.KEKID = "other-kek" },
		func(value *EncryptedSecretVersion) { value.RecordNonce[0] ^= 1 },
		func(value *EncryptedSecretVersion) { value.WrapNonce[0] ^= 1 },
		func(value *EncryptedSecretVersion) { value.WrappedDEK[0] ^= 1 },
		func(value *EncryptedSecretVersion) { value.Ciphertext[0] ^= 1 },
	}
	for index, mutate := range mutations {
		candidate := encrypted
		candidate.RecordNonce = append([]byte(nil), encrypted.RecordNonce...)
		candidate.WrapNonce = append([]byte(nil), encrypted.WrapNonce...)
		candidate.WrappedDEK = append([]byte(nil), encrypted.WrappedDEK...)
		candidate.Ciphertext = append([]byte(nil), encrypted.Ciphertext...)
		mutate(&candidate)
		if err = Decrypt(ctx, custodian, "store-test", "api-token", candidate, func([]byte) error { return nil }); err == nil {
			t.Fatalf("mutation %d was accepted", index)
		}
	}
	if err = Decrypt(ctx, custodian, "different-store", "api-token", encrypted, func([]byte) error { return nil }); err == nil {
		t.Fatal("wrong store AAD was accepted")
	}
	if err = Decrypt(ctx, custodian, "store-test", "different-kind", encrypted, func([]byte) error { return nil }); err == nil {
		t.Fatal("wrong kind AAD was accepted")
	}
}

func TestEncryptionUsesFreshNoncesAndDEKs(t *testing.T) {
	custodian := newMemoryCustodian(t, "test-kek")
	first, err := Encrypt(context.Background(), custodian, "store-test", "secret-test", 1, "api-token", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encrypt(context.Background(), custodian, "store-test", "secret-test", 1, "api-token", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first.RecordNonce) == string(second.RecordNonce) || string(first.WrapNonce) == string(second.WrapNonce) || string(first.Ciphertext) == string(second.Ciphertext) || string(first.WrappedDEK) == string(second.WrappedDEK) {
		t.Fatal("encryption reused random material")
	}
}

func FuzzEncryptedCredentialDecode(f *testing.F) {
	f.Add([]byte("ciphertext"), []byte("wrapped"), []byte("nonce-value-that-is-long"))
	f.Fuzz(func(t *testing.T, ciphertext, wrapped, nonce []byte) {
		if len(ciphertext) > maximumPlaintextBytes+100 || len(wrapped) > 100 || len(nonce) > 100 {
			t.Skip()
		}
		value := EncryptedSecretVersion{RecordID: "secret-test", Version: 1, FormatVersion: FormatVersion, Algorithm: Algorithm, KEKID: "test-kek", KEKVersion: 1, RecordNonce: nonce, Ciphertext: ciphertext, WrapNonce: nonce, WrappedDEK: wrapped}
		_ = ValidateEncryptedVersion(value)
	})
}
