package bbolt

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
)

const (
	schemaVersion = "1"
	maximumValue  = 2 << 20
)

var topLevelBuckets = [][]byte{
	[]byte("meta"), []byte("agents"), []byte("deployments"), []byte("secret_records"),
	[]byte("secret_versions"), []byte("credential_bindings"), []byte("roles"),
	[]byte("role_bindings"), []byte("projection_generations"), []byte("revocations"), []byte("receipts"),
}

var (
	metaBucket       = []byte("meta")
	recordBucket     = []byte("secret_records")
	versionBucket    = []byte("secret_versions")
	bindingBucket    = []byte("credential_bindings")
	revocationBucket = []byte("revocations")
)

type Store struct {
	db           *bolt.DB
	path         string
	storeID      string
	deploymentID string
}

type keyCheck struct {
	Encrypted credentials.EncryptedSecretVersion `json:"encrypted"`
	Hash      string                             `json:"hash"`
}

func Open(ctx context.Context, path, deploymentID string, custodian credentials.KeyCustodian) (*Store, error) {
	if !credentials.ValidateIdentifier(deploymentID) {
		return nil, errors.New("invalid deployment identifier")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = secureParent(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if _, err = os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if err = initialize(ctx, path, deploymentID, custodian); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err = validateFile(path); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open credential authority: %w", err)
	}
	store := &Store{db: db, path: path}
	if err = store.validate(ctx, deploymentID, custodian); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(metaBucket).Put([]byte("last_clean_shutdown"), []byte("false"))
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// Inspect opens an existing authority read-only and validates its schema,
// deployment binding, encrypted key sentinel, ownership, and permissions. It is
// deliberately separate from Open: readiness inspection must never initialize
// a missing database or update last_clean_shutdown.
func Inspect(ctx context.Context, path, deploymentID string, custodian credentials.KeyCustodian) error {
	if !credentials.ValidateIdentifier(deploymentID) || custodian == nil {
		return errors.New("invalid credential authority inspection")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err = validateFile(path); err != nil {
		return err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{ReadOnly: true, Timeout: 2 * time.Second})
	if err != nil {
		return fmt.Errorf("inspect credential authority: %w", err)
	}
	defer db.Close()
	return (&Store{db: db, path: path}).validate(ctx, deploymentID, custodian)
}

func initialize(ctx context.Context, path, deploymentID string, custodian credentials.KeyCustodian) error {
	staging := path + ".initialize"
	if err := os.Remove(staging); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	db, err := bolt.Open(staging, 0600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = db.Close()
		if remove {
			_ = os.Remove(staging)
		}
	}()
	storeID, err := randomID("store")
	if err != nil {
		return err
	}
	sentinel := make([]byte, 32)
	if _, err = rand.Read(sentinel); err != nil {
		return err
	}
	defer wipe(sentinel)
	encrypted, err := credentials.Encrypt(ctx, custodian, storeID, "key-check", 1, "key-check", sentinel)
	if err != nil {
		return err
	}
	encrypted.CreatedAt = time.Now().UTC()
	hash := sha256.Sum256(sentinel)
	check, err := encode(keyCheck{Encrypted: encrypted, Hash: hex.EncodeToString(hash[:])})
	if err != nil {
		return err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range topLevelBuckets {
			if _, createErr := tx.CreateBucket(name); createErr != nil {
				return createErr
			}
		}
		meta := tx.Bucket(metaBucket)
		values := map[string]string{"schema_version": schemaVersion, "store_id": storeID, "deployment_id": deploymentID, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "active_projection_generation": "0", "last_clean_shutdown": "true"}
		for key, value := range values {
			if putErr := meta.Put([]byte(key), []byte(value)); putErr != nil {
				return putErr
			}
		}
		return meta.Put([]byte("key_check"), check)
	})
	if err != nil {
		return err
	}
	if err = db.Sync(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if err = os.Rename(staging, path); err != nil {
		return err
	}
	remove = false
	return syncDirectory(filepath.Dir(path))
}

func secureParent(path string) error {
	path = filepath.Clean(path)
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	_, statErr := os.Lstat(path)
	created := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !created {
		return statErr
	}
	if created {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		if err := os.Chmod(path, 0700); err != nil {
			return err
		}
	}
	if err := rejectSymlinkComponents(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	fileStat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ok || int(fileStat.Uid) != os.Geteuid() || int(fileStat.Gid) != os.Getegid() {
		return errors.New("credential authority directory ownership or mode is invalid")
	}
	var filesystem unix.Statfs_t
	if err = unix.Statfs(path, &filesystem); err != nil {
		return err
	}
	// NFS, SMB, and CIFS mounts are unsupported for bbolt authority files.
	switch uint64(filesystem.Type) {
	case 0x6969, 0x517b, 0xff534d42:
		return errors.New("credential authority must use a supported local filesystem")
	}
	return nil
}

func rejectSymlinkComponents(path string) error {
	current := string(os.PathSeparator)
	for _, component := range strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("credential authority path contains a symlink or non-directory component")
		}
	}
	return nil
}

func validateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("credential authority must be a regular mode-0600 file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || stat.Nlink != 1 {
		return errors.New("credential authority ownership or link count is invalid")
	}
	return nil
}

func (s *Store) validate(ctx context.Context, deploymentID string, custodian credentials.KeyCustodian) error {
	var check keyCheck
	err := s.db.View(func(tx *bolt.Tx) error {
		for _, bucket := range topLevelBuckets {
			if tx.Bucket(bucket) == nil {
				return errors.New("credential authority is missing a required bucket")
			}
		}
		meta := tx.Bucket(metaBucket)
		if string(meta.Get([]byte("schema_version"))) != schemaVersion {
			return errors.New("credential authority schema is unsupported")
		}
		s.storeID = string(append([]byte(nil), meta.Get([]byte("store_id"))...))
		s.deploymentID = string(append([]byte(nil), meta.Get([]byte("deployment_id"))...))
		if !credentials.ValidateIdentifier(s.storeID) || s.deploymentID != deploymentID {
			return errors.New("credential authority deployment identity mismatch")
		}
		if err := decode(meta.Get([]byte("key_check")), &check); err != nil {
			return errors.New("credential authority key check is invalid")
		}
		for checkErr := range tx.Check() {
			if checkErr != nil {
				return errors.New("credential authority structural integrity check failed")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return credentials.Decrypt(ctx, custodian, s.storeID, "key-check", check.Encrypted, func(plaintext []byte) error {
		hash := sha256.Sum256(plaintext)
		if check.Hash != hex.EncodeToString(hash[:]) {
			return errors.New("credential authority key check failed")
		}
		return nil
	})
}

func (s *Store) StoreID() string      { return s.storeID }
func (s *Store) DeploymentID() string { return s.deploymentID }

func (s *Store) Create(ctx context.Context, record credentials.SecretRecord, version credentials.EncryptedSecretVersion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if credentials.ValidateRecord(record) != nil || credentials.ValidateEncryptedVersion(version) != nil || record.CurrentVersion != 1 || version.RecordID != record.ID || version.Version != 1 || version.CreatedAt.IsZero() {
		return errors.New("invalid initial credential record")
	}
	recordBytes, err := encode(record)
	if err != nil {
		return err
	}
	versionBytes, err := encode(version)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		records := tx.Bucket(recordBucket)
		if records.Get([]byte(record.ID)) != nil || records.Get(referenceKey(record.Reference)) != nil {
			return credentials.ErrConflict
		}
		if err := records.Put([]byte(record.ID), recordBytes); err != nil {
			return err
		}
		if err := records.Put(referenceKey(record.Reference), []byte(record.ID)); err != nil {
			return err
		}
		return tx.Bucket(versionBucket).Put(versionKey(record.ID, 1), versionBytes)
	})
}

func (s *Store) AddVersion(ctx context.Context, version credentials.EncryptedSecretVersion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := credentials.ValidateEncryptedVersion(version); err != nil || version.CreatedAt.IsZero() {
		return errors.New("invalid encrypted credential version")
	}
	versionBytes, err := encode(version)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		var record credentials.SecretRecord
		records := tx.Bucket(recordBucket)
		if err := decode(records.Get([]byte(version.RecordID)), &record); err != nil {
			return credentials.ErrNotFound
		}
		if err := credentials.ValidateRecord(record); err != nil {
			return err
		}
		if record.Status != credentials.StatusActive {
			return credentials.ErrRevoked
		}
		if version.Version != record.CurrentVersion+1 || tx.Bucket(versionBucket).Get(versionKey(version.RecordID, version.Version)) != nil {
			return credentials.ErrConflict
		}
		record.CurrentVersion = version.Version
		recordBytes, err := encode(record)
		if err != nil {
			return err
		}
		if err = tx.Bucket(versionBucket).Put(versionKey(version.RecordID, version.Version), versionBytes); err != nil {
			return err
		}
		return records.Put([]byte(record.ID), recordBytes)
	})
}

func (s *Store) Metadata(ctx context.Context, recordID string) (credentials.SecretRecord, error) {
	var record credentials.SecretRecord
	if err := ctx.Err(); err != nil {
		return record, err
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(recordBucket).Get([]byte(recordID))
		if value == nil {
			return credentials.ErrNotFound
		}
		if err := decode(value, &record); err != nil {
			return err
		}
		return credentials.ValidateRecord(record)
	})
	return record, err
}

func (s *Store) List(ctx context.Context, query string, limit int) ([]credentials.SecretRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 || len(query) > 255 {
		return nil, errors.New("invalid credential list bounds")
	}
	query = strings.ToLower(query)
	result := make([]credentials.SecretRecord, 0, limit)
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(recordBucket).Cursor()
		for key, value := cursor.First(); key != nil && len(result) < limit; key, value = cursor.Next() {
			if bytes.HasPrefix(key, []byte("ref\x00")) {
				continue
			}
			var record credentials.SecretRecord
			if err := decode(value, &record); err != nil {
				return err
			}
			if err := credentials.ValidateRecord(record); err != nil {
				return err
			}
			if query == "" || strings.Contains(strings.ToLower(record.Reference), query) || strings.Contains(strings.ToLower(record.Kind), query) || strings.Contains(strings.ToLower(record.ID), query) {
				result = append(result, record)
			}
		}
		return nil
	})
	return result, err
}

func (s *Store) Version(ctx context.Context, recordID string, version uint64) (credentials.EncryptedSecretVersion, error) {
	var encrypted credentials.EncryptedSecretVersion
	if err := ctx.Err(); err != nil {
		return encrypted, err
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(versionBucket).Get(versionKey(recordID, version))
		if value == nil {
			return credentials.ErrNotFound
		}
		if err := decode(value, &encrypted); err != nil {
			return err
		}
		return credentials.ValidateEncryptedVersion(encrypted)
	})
	return encrypted, err
}

func (s *Store) History(ctx context.Context, recordID string, limit int) ([]credentials.SecretVersionMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !credentials.ValidateIdentifier(recordID) || limit < 1 || limit > 100 {
		return nil, errors.New("invalid credential history bounds")
	}
	prefix := append([]byte(recordID), 0)
	result := make([]credentials.SecretVersionMetadata, 0, limit)
	err := s.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(recordBucket).Get([]byte(recordID)) == nil {
			return credentials.ErrNotFound
		}
		cursor := tx.Bucket(versionBucket).Cursor()
		for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix) && len(result) < limit; key, value = cursor.Next() {
			var version credentials.EncryptedSecretVersion
			if err := decode(value, &version); err != nil {
				return err
			}
			if err := credentials.ValidateEncryptedVersion(version); err != nil {
				return err
			}
			result = append(result, credentials.SecretVersionMetadata{RecordID: version.RecordID, Version: version.Version, FormatVersion: version.FormatVersion, Algorithm: version.Algorithm, KEKVersion: version.KEKVersion, CreatedAt: version.CreatedAt, CiphertextHash: version.CiphertextHash})
		}
		return nil
	})
	return result, err
}

func (s *Store) Bind(ctx context.Context, binding credentials.CredentialBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := credentials.ValidateBinding(binding, s.deploymentID); err != nil {
		return err
	}
	sort.Strings(binding.Destinations)
	for i := 1; i < len(binding.Destinations); i++ {
		if binding.Destinations[i] == binding.Destinations[i-1] {
			return errors.New("credential binding has duplicate destinations")
		}
	}
	key := bindingKey(binding.Key)
	return s.db.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(recordBucket).Get([]byte(binding.SecretRecord)) == nil {
			return credentials.ErrNotFound
		}
		bindings := tx.Bucket(bindingBucket)
		if bindings.Get(key) != nil {
			return credentials.ErrConflict
		}
		binding.BindingRevision = 1
		encoded, err := encode(binding)
		if err != nil {
			return err
		}
		return bindings.Put(key, encoded)
	})
}

func (s *Store) Resolve(ctx context.Context, key credentials.CredentialBindingKey) (credentials.ResolvedSecret, error) {
	var resolved credentials.ResolvedSecret
	if err := ctx.Err(); err != nil {
		return resolved, err
	}
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bindingBucket).Get(bindingKey(key))
		if value == nil {
			return credentials.ErrNotFound
		}
		if err := decode(value, &resolved.Binding); err != nil {
			return err
		}
		if err := credentials.ValidateBinding(resolved.Binding, s.deploymentID); err != nil {
			return err
		}
		if !resolved.Binding.Enabled {
			return credentials.ErrRevoked
		}
		if err := decode(tx.Bucket(recordBucket).Get([]byte(resolved.Binding.SecretRecord)), &resolved.Record); err != nil {
			return credentials.ErrNotFound
		}
		if err := credentials.ValidateRecord(resolved.Record); err != nil {
			return err
		}
		version := resolved.Record.CurrentVersion
		if resolved.Binding.VersionPolicy == credentials.VersionPinned {
			version = resolved.Binding.PinnedVersion
		}
		if tx.Bucket(revocationBucket).Get(versionKey(resolved.Record.ID, version)) != nil || tx.Bucket(revocationBucket).Get(versionKey(resolved.Record.ID, 0)) != nil {
			return credentials.ErrRevoked
		}
		if err := decode(tx.Bucket(versionBucket).Get(versionKey(resolved.Record.ID, version)), &resolved.Version); err != nil {
			return credentials.ErrNotFound
		}
		return credentials.ValidateEncryptedVersion(resolved.Version)
	})
	return resolved, err
}

func (s *Store) Revoke(ctx context.Context, recordID string, version uint64, reason string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		records := tx.Bucket(recordBucket)
		var record credentials.SecretRecord
		if err := decode(records.Get([]byte(recordID)), &record); err != nil {
			return credentials.ErrNotFound
		}
		if err := credentials.ValidateRecord(record); err != nil {
			return err
		}
		if version > record.CurrentVersion {
			return credentials.ErrNotFound
		}
		revocation, err := encode(struct {
			Reason string    `json:"reason"`
			At     time.Time `json:"at"`
		}{reason, at})
		if err != nil {
			return err
		}
		if err = tx.Bucket(revocationBucket).Put(versionKey(recordID, version), revocation); err != nil {
			return err
		}
		if version == 0 {
			record.Status, record.RevokedAt, record.Revocation = credentials.StatusRevoked, at, reason
			encoded, encodeErr := encode(record)
			if encodeErr != nil {
				return encodeErr
			}
			return records.Put([]byte(recordID), encoded)
		}
		return nil
	})
}

func (s *Store) Backup(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if path == s.path {
		return errors.New("backup path must differ from the live authority")
	}
	if err = secureParent(filepath.Dir(path)); err != nil {
		return err
	}
	if _, err = os.Lstat(path); err == nil {
		return errors.New("backup target already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = s.db.View(func(tx *bolt.Tx) error { return tx.CopyFile(path, 0600) }); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(metaBucket).Put([]byte("last_clean_shutdown"), []byte("true"))
	}); err != nil {
		_ = s.db.Close()
		s.db = nil
		return err
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func encode(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maximumValue {
		return nil, errors.New("credential encoding exceeds size limit")
	}
	return encoded, nil
}

func decode(value []byte, target any) error {
	if len(value) == 0 || len(value) > maximumValue {
		return errors.New("credential encoding is missing or oversized")
	}
	decoder := json.NewDecoder(io.LimitReader(bytes.NewReader(value), maximumValue+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("credential encoding is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("credential encoding has trailing data")
	}
	return nil
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func referenceKey(reference string) []byte { return []byte("ref\x00" + reference) }

func versionKey(recordID string, version uint64) []byte {
	key := make([]byte, len(recordID)+1+8)
	copy(key, recordID)
	binary.BigEndian.PutUint64(key[len(recordID)+1:], version)
	return key
}

func bindingKey(key credentials.CredentialBindingKey) []byte {
	var output bytes.Buffer
	for _, value := range []string{key.AgentID, key.StanzaID, key.DeploymentID, key.Scope} {
		_ = binary.Write(&output, binary.BigEndian, uint16(len(value)))
		_, _ = output.WriteString(value)
	}
	return output.Bytes()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
