package credentials

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	encryptedKeyFormatVersion       = 1
	AuthorityPassphraseMinimumBytes = 12
	AuthorityPassphraseMaximumBytes = 1024
	argonTime                       = 3
	argonMemory                     = 64 * 1024
	argonThreads                    = 4
)

type encryptedKeyFile struct {
	FormatVersion uint16 `json:"format_version"`
	KDF           string `json:"kdf"`
	Time          uint32 `json:"time"`
	Memory        uint32 `json:"memory_kib"`
	Threads       uint8  `json:"threads"`
	Salt          string `json:"salt"`
	Nonce         string `json:"nonce"`
	Ciphertext    string `json:"ciphertext"`
}

var encryptedKeyAAD = []byte("aegis/passphrase-encrypted-kek/v1")

var ErrPassphraseAuthentication = errors.New("encrypted key-encryption key credential could not be unlocked")

func IsPassphraseAuthentication(err error) bool { return errors.Is(err, ErrPassphraseAuthentication) }

// CreatePassphraseKey creates a random KEK and persists only an
// Argon2id-derived-key encrypted envelope. The passphrase and plaintext KEK are
// never written to disk.
func CreatePassphraseKey(path, id string, passphrase []byte) error {
	if !ValidateIdentifier(id) {
		return errors.New("invalid key-encryption key identifier")
	}
	if len(passphrase) < AuthorityPassphraseMinimumBytes || len(passphrase) > AuthorityPassphraseMaximumBytes {
		return fmt.Errorf("authority passphrase must be between %d and %d bytes", AuthorityPassphraseMinimumBytes, AuthorityPassphraseMaximumBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	key := make([]byte, chacha20poly1305.KeySize)
	salt := make([]byte, 16)
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	defer wipe(key)
	defer wipe(salt)
	defer wipe(nonce)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plaintext, err := json.Marshal(keyFile{FormatVersion: FormatVersion, ID: id, Version: 1, Key: base64.StdEncoding.EncodeToString(key)})
	if err != nil {
		return err
	}
	defer wipe(plaintext)
	wrappingKey := argon2.IDKey(passphrase, salt, argonTime, argonMemory, argonThreads, chacha20poly1305.KeySize)
	defer wipe(wrappingKey)
	aead, err := chacha20poly1305.NewX(wrappingKey)
	if err != nil {
		return err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, encryptedKeyAAD)
	defer wipe(ciphertext)
	value, err := json.Marshal(encryptedKeyFile{
		FormatVersion: encryptedKeyFormatVersion,
		KDF:           "argon2id",
		Time:          argonTime,
		Memory:        argonMemory,
		Threads:       argonThreads,
		Salt:          base64.StdEncoding.EncodeToString(salt),
		Nonce:         base64.StdEncoding.EncodeToString(nonce),
		Ciphertext:    base64.StdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return err
	}
	return writeEncryptedCredential(path, value)
}

// LoadPassphraseCustodian unlocks an encrypted KEK envelope without retaining
// the passphrase. Authentication failures deliberately return one generic
// error that reveals no plaintext or envelope details.
func LoadPassphraseCustodian(path string, passphrase []byte) (*FileCustodian, error) {
	if len(passphrase) < AuthorityPassphraseMinimumBytes || len(passphrase) > AuthorityPassphraseMaximumBytes {
		return nil, ErrPassphraseAuthentication
	}
	if err := validateEncryptedCredentialFile(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	var stored encryptedKeyFile
	if err = decoder.Decode(&stored); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("encrypted key-encryption key credential is invalid")
	}
	if stored.FormatVersion != encryptedKeyFormatVersion || stored.KDF != "argon2id" || stored.Time != argonTime || stored.Memory != argonMemory || stored.Threads != argonThreads {
		return nil, errors.New("encrypted key-encryption key credential uses unsupported parameters")
	}
	salt, saltErr := base64.StdEncoding.Strict().DecodeString(stored.Salt)
	nonce, nonceErr := base64.StdEncoding.Strict().DecodeString(stored.Nonce)
	ciphertext, ciphertextErr := base64.StdEncoding.Strict().DecodeString(stored.Ciphertext)
	defer wipe(salt)
	defer wipe(nonce)
	defer wipe(ciphertext)
	if saltErr != nil || nonceErr != nil || ciphertextErr != nil || len(salt) != 16 || len(nonce) != chacha20poly1305.NonceSizeX || len(ciphertext) < chacha20poly1305.Overhead {
		return nil, errors.New("encrypted key-encryption key credential is invalid")
	}
	wrappingKey := argon2.IDKey(passphrase, salt, stored.Time, stored.Memory, stored.Threads, chacha20poly1305.KeySize)
	defer wipe(wrappingKey)
	aead, err := chacha20poly1305.NewX(wrappingKey)
	if err != nil {
		return nil, ErrPassphraseAuthentication
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, encryptedKeyAAD)
	if err != nil {
		return nil, ErrPassphraseAuthentication
	}
	defer wipe(plaintext)
	var key keyFile
	plainDecoder := json.NewDecoder(bytes.NewReader(plaintext))
	plainDecoder.DisallowUnknownFields()
	if err = plainDecoder.Decode(&key); err != nil || plainDecoder.Decode(&struct{}{}) != io.EOF || key.FormatVersion != FormatVersion || !ValidateIdentifier(key.ID) || key.Version == 0 {
		return nil, errors.New("encrypted key-encryption key credential is invalid")
	}
	material, err := base64.StdEncoding.Strict().DecodeString(key.Key)
	if err != nil || len(material) != chacha20poly1305.KeySize {
		wipe(material)
		return nil, errors.New("encrypted key-encryption key credential is invalid")
	}
	return &FileCustodian{metadata: KEKMetadata{ID: key.ID, Version: key.Version}, key: material}, nil
}

// InspectPassphraseCredential validates the strict encrypted envelope without
// asking for or decrypting with the passphrase.
func InspectPassphraseCredential(path string) error {
	if err := validateEncryptedCredentialFile(path); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	var stored encryptedKeyFile
	if err = decoder.Decode(&stored); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("encrypted key-encryption key credential is invalid")
	}
	if stored.FormatVersion != encryptedKeyFormatVersion || stored.KDF != "argon2id" || stored.Time != argonTime || stored.Memory != argonMemory || stored.Threads != argonThreads {
		return errors.New("encrypted key-encryption key credential uses unsupported parameters")
	}
	salt, saltErr := base64.StdEncoding.Strict().DecodeString(stored.Salt)
	nonce, nonceErr := base64.StdEncoding.Strict().DecodeString(stored.Nonce)
	ciphertext, ciphertextErr := base64.StdEncoding.Strict().DecodeString(stored.Ciphertext)
	defer wipe(salt)
	defer wipe(nonce)
	defer wipe(ciphertext)
	if saltErr != nil || nonceErr != nil || ciphertextErr != nil || len(salt) != 16 || len(nonce) != chacha20poly1305.NonceSizeX || len(ciphertext) < chacha20poly1305.Overhead {
		return errors.New("encrypted key-encryption key credential is invalid")
	}
	return nil
}

func validateEncryptedCredentialFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	permissions := info.Mode().Perm()
	if !info.Mode().IsRegular() || permissions&0077 != 0 || permissions&0100 != 0 || permissions&0400 == 0 || !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || stat.Nlink != 1 {
		return errors.New("encrypted key-encryption key file must be a regular file with no group or other permissions")
	}
	return nil
}

func writeEncryptedCredential(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(value, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
