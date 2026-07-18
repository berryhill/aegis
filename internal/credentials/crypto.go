package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const maximumPlaintextBytes = 1 << 20

type KEKMetadata struct {
	ID      string `json:"id"`
	Version uint64 `json:"version"`
}

// KeyCustodian lends a private copy of key material only for the duration of fn.
// Implementations must not log the key or expose printable key handles.
type KeyCustodian interface {
	ActiveKEK(context.Context, func(KEKMetadata, []byte) error) error
	KEK(context.Context, string, uint64, func([]byte) error) error
}

func aad(purpose, storeID, recordID string, recordVersion uint64, kind, kekID string, kekVersion uint64) ([]byte, error) {
	if purpose == "" || !ValidateIdentifier(storeID) || !ValidateIdentifier(recordID) || !ValidateIdentifier(kind) || !ValidateIdentifier(kekID) || recordVersion == 0 || kekVersion == 0 {
		return nil, errors.New("invalid credential encryption context")
	}
	var out bytes.Buffer
	fields := []string{fmt.Sprint(FormatVersion), Algorithm, storeID, recordID, fmt.Sprint(recordVersion), kind, kekID, fmt.Sprint(kekVersion), purpose}
	for _, field := range fields {
		if len(field) > 65535 {
			return nil, errors.New("credential encryption context is too large")
		}
		_ = binary.Write(&out, binary.BigEndian, uint16(len(field)))
		_, _ = out.WriteString(field)
	}
	return out.Bytes(), nil
}

func Encrypt(ctx context.Context, custodian KeyCustodian, storeID, recordID string, version uint64, kind string, plaintext []byte) (EncryptedSecretVersion, error) {
	var encrypted EncryptedSecretVersion
	if len(plaintext) == 0 || len(plaintext) > maximumPlaintextBytes {
		return encrypted, errors.New("secret value must be between 1 byte and 1 MiB")
	}
	err := custodian.ActiveKEK(ctx, func(metadata KEKMetadata, kek []byte) error {
		if len(kek) != chacha20poly1305.KeySize || !ValidateIdentifier(metadata.ID) || metadata.Version == 0 {
			return errors.New("active key-encryption key is invalid")
		}
		dek := make([]byte, chacha20poly1305.KeySize)
		defer wipe(dek)
		if _, err := rand.Read(dek); err != nil {
			return errors.New("generate data-encryption key")
		}
		recordAEAD, err := chacha20poly1305.NewX(dek)
		if err != nil {
			return errors.New("initialize record encryption")
		}
		wrapAEAD, err := chacha20poly1305.NewX(kek)
		if err != nil {
			return errors.New("initialize key wrapping")
		}
		recordAAD, err := aad("record-encryption", storeID, recordID, version, kind, metadata.ID, metadata.Version)
		if err != nil {
			return err
		}
		wrapAAD, err := aad("dek-wrapping", storeID, recordID, version, kind, metadata.ID, metadata.Version)
		if err != nil {
			return err
		}
		recordNonce := make([]byte, chacha20poly1305.NonceSizeX)
		wrapNonce := make([]byte, chacha20poly1305.NonceSizeX)
		if _, err = rand.Read(recordNonce); err != nil {
			return errors.New("generate record nonce")
		}
		if _, err = rand.Read(wrapNonce); err != nil {
			return errors.New("generate wrapping nonce")
		}
		ciphertext := recordAEAD.Seal(nil, recordNonce, plaintext, recordAAD)
		wrappedDEK := wrapAEAD.Seal(nil, wrapNonce, dek, wrapAAD)
		digest := sha256.Sum256(ciphertext)
		encrypted = EncryptedSecretVersion{RecordID: recordID, Version: version, FormatVersion: FormatVersion, Algorithm: Algorithm, KEKID: metadata.ID, KEKVersion: metadata.Version, RecordNonce: recordNonce, Ciphertext: ciphertext, WrapNonce: wrapNonce, WrappedDEK: wrappedDEK, CiphertextHash: "sha256:" + hex.EncodeToString(digest[:])}
		return nil
	})
	return encrypted, err
}

func Decrypt(ctx context.Context, custodian KeyCustodian, storeID, kind string, encrypted EncryptedSecretVersion, use func([]byte) error) error {
	if err := ValidateEncryptedVersion(encrypted); err != nil {
		return err
	}
	return custodian.KEK(ctx, encrypted.KEKID, encrypted.KEKVersion, func(kek []byte) error {
		if len(kek) != chacha20poly1305.KeySize {
			return errors.New("key-encryption key is invalid")
		}
		wrapAEAD, err := chacha20poly1305.NewX(kek)
		if err != nil {
			return errors.New("initialize key unwrapping")
		}
		wrapAAD, err := aad("dek-wrapping", storeID, encrypted.RecordID, encrypted.Version, kind, encrypted.KEKID, encrypted.KEKVersion)
		if err != nil {
			return err
		}
		dek, err := wrapAEAD.Open(nil, encrypted.WrapNonce, encrypted.WrappedDEK, wrapAAD)
		if err != nil {
			return errors.New("credential key authentication failed")
		}
		defer wipe(dek)
		recordAEAD, err := chacha20poly1305.NewX(dek)
		if err != nil {
			return errors.New("initialize record decryption")
		}
		recordAAD, err := aad("record-encryption", storeID, encrypted.RecordID, encrypted.Version, kind, encrypted.KEKID, encrypted.KEKVersion)
		if err != nil {
			return err
		}
		plaintext, err := recordAEAD.Open(nil, encrypted.RecordNonce, encrypted.Ciphertext, recordAAD)
		if err != nil {
			return errors.New("credential record authentication failed")
		}
		defer wipe(plaintext)
		return use(plaintext)
	})
}

func ValidateEncryptedVersion(value EncryptedSecretVersion) error {
	if value.FormatVersion != FormatVersion || value.Algorithm != Algorithm || !ValidateIdentifier(value.RecordID) || value.Version == 0 || !ValidateIdentifier(value.KEKID) || value.KEKVersion == 0 || len(value.RecordNonce) != chacha20poly1305.NonceSizeX || len(value.WrapNonce) != chacha20poly1305.NonceSizeX || len(value.WrappedDEK) != chacha20poly1305.KeySize+chacha20poly1305.Overhead || len(value.Ciphertext) <= chacha20poly1305.Overhead || len(value.Ciphertext) > maximumPlaintextBytes+chacha20poly1305.Overhead {
		return errors.New("encrypted credential encoding is invalid")
	}
	digest := sha256.Sum256(value.Ciphertext)
	if value.CiphertextHash != "sha256:"+hex.EncodeToString(digest[:]) {
		return errors.New("encrypted credential digest mismatch")
	}
	return nil
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
