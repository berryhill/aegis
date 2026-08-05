package badger

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"golang.org/x/sys/unix"
)

const (
	BackupFormat         = "aegis-authority-backup-v1"
	maximumBackupBytes   = int64(256 << 20)
	maximumBackupRecords = uint64(1_000_000)
	maximumBackupKey     = uint32(4096)
	maximumBackupValue   = uint32(64 << 20)
)

var (
	ErrMaintenanceUnsafe = errors.New("authority maintenance requires one closed, clean, unambiguous generation")
	ErrBackupCorrupt     = errors.New("authority backup is corrupt")
	ErrGenerationActive  = errors.New("authority generation is active")
)

var backupMagic = [16]byte{'A', 'E', 'G', 'I', 'S', '-', 'A', 'U', 'T', 'H', '-', 'B', 'K', 'P', '1', 0}

type BackupManifest struct {
	Format        string     `json:"format"`
	Generation    Generation `json:"generation"`
	RecordCount   uint64     `json:"record_count"`
	PayloadBytes  int64      `json:"payload_bytes"`
	PayloadSHA256 string     `json:"payload_sha256"`
}

type maintenanceLease struct {
	file *os.File
}

func (lease *maintenanceLease) release() {
	if lease == nil || lease.file == nil {
		return
	}
	_ = unix.Flock(int(lease.file.Fd()), unix.LOCK_UN)
	_ = lease.file.Close()
}

// acquireMaintenance serializes all lifecycle-changing operations across
// processes. Open stores hold a shared lease for their entire lifetime, so an
// exclusive maintenance lease also proves that the authority is not open.
func acquireMaintenance(ctx context.Context, root string, exclusive bool) (*maintenanceLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireSecureDirectory(root); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMaintenanceUnsafe, err)
	}
	path := filepath.Join(root, "MAINTENANCE")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open authority maintenance coordinator: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open authority maintenance coordinator: invalid file descriptor")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: maintenance coordinator is unsafe", ErrMaintenanceUnsafe)
	}
	operation := unix.LOCK_SH | unix.LOCK_NB
	if exclusive {
		operation = unix.LOCK_EX | unix.LOCK_NB
	}
	for {
		if err = unix.Flock(int(file.Fd()), operation); err == nil {
			return &maintenanceLease{file: file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock authority maintenance coordinator: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func inspectClosedGeneration(root string) (Generation, error) {
	for _, path := range []string{root, filepath.Join(root, "stores"), filepath.Join(root, "staging"), filepath.Join(root, "retired")} {
		if err := requireSecureDirectory(path); err != nil {
			return Generation{}, fmt.Errorf("%w: %v", ErrMaintenanceUnsafe, err)
		}
	}
	active, err := readMarker(filepath.Join(root, "ACTIVE"))
	if err != nil {
		return Generation{}, fmt.Errorf("%w: ACTIVE: %v", ErrMaintenanceUnsafe, err)
	}
	clean, err := readMarker(filepath.Join(root, "CLEAN"))
	if err != nil || clean != active {
		return Generation{}, fmt.Errorf("%w: CLEAN does not authenticate ACTIVE", ErrMaintenanceUnsafe)
	}
	if _, err = os.Lstat(filepath.Join(root, "DIRTY")); err == nil {
		return Generation{}, fmt.Errorf("%w: DIRTY is present", ErrMaintenanceUnsafe)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, fmt.Errorf("%w: inspect DIRTY: %v", ErrMaintenanceUnsafe, err)
	}
	if err = verifyStoreIdentity(filepath.Join(root, "stores", active.Directory), active); err != nil {
		return Generation{}, fmt.Errorf("%w: %v", ErrMaintenanceUnsafe, err)
	}
	return active, nil
}

func requireSecureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return fmt.Errorf("authority directory %s is unsafe", path)
	}
	return nil
}

// Export writes a bounded, versioned, integrity-authenticated representation
// of the exact closed active generation. The destination is published
// atomically and is never replaced.
func Export(ctx context.Context, root, destination string) (BackupManifest, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return BackupManifest{}, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return BackupManifest{}, err
	}
	defer lease.release()
	generation, err := inspectClosedGeneration(root)
	if err != nil {
		return BackupManifest{}, err
	}
	destination, err = cleanBackupPath(root, destination)
	if err != nil {
		return BackupManifest{}, err
	}
	if err = secureMkdir(filepath.Dir(destination)); err != nil {
		return BackupManifest{}, err
	}
	if _, err = os.Lstat(destination); err == nil {
		return BackupManifest{}, errors.New("backup destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return BackupManifest{}, err
	}

	payload, err := os.CreateTemp(filepath.Dir(destination), ".authority-payload-*")
	if err != nil {
		return BackupManifest{}, err
	}
	payloadName := payload.Name()
	defer os.Remove(payloadName)
	if err = payload.Chmod(0600); err != nil {
		_ = payload.Close()
		return BackupManifest{}, err
	}
	manifest := BackupManifest{Format: BackupFormat, Generation: generation}
	digest := sha256.New()
	writer := &boundedHashWriter{writer: io.MultiWriter(payload, digest), remaining: maximumBackupBytes}
	db, err := badgerdb.Open(options(filepath.Join(root, "stores", generation.Directory)).WithReadOnly(true))
	if err != nil {
		_ = payload.Close()
		return BackupManifest{}, err
	}
	err = db.View(func(txn *badgerdb.Txn) error {
		iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			item := iterator.Item()
			key := item.KeyCopy(nil)
			if _, decodeErr := DecodeKey(key); decodeErr != nil {
				return fmt.Errorf("%w: unregistered key", ErrCorruptRecord)
			}
			value, valueErr := item.ValueCopy(nil)
			if valueErr != nil {
				return valueErr
			}
			if len(key) > int(maximumBackupKey) || len(value) > int(maximumBackupValue) || manifest.RecordCount == maximumBackupRecords {
				return errors.New("authority backup exceeds a format bound")
			}
			var lengths [8]byte
			binary.BigEndian.PutUint32(lengths[0:4], uint32(len(key)))
			binary.BigEndian.PutUint32(lengths[4:8], uint32(len(value)))
			if _, writeErr := writer.Write(lengths[:]); writeErr != nil {
				return writeErr
			}
			if _, writeErr := writer.Write(key); writeErr != nil {
				return writeErr
			}
			if _, writeErr := writer.Write(value); writeErr != nil {
				return writeErr
			}
			manifest.RecordCount++
		}
		return nil
	})
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = payload.Sync()
	}
	if closeErr := payload.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return BackupManifest{}, err
	}
	manifest.PayloadBytes = maximumBackupBytes - writer.remaining
	manifest.PayloadSHA256 = hex.EncodeToString(digest.Sum(nil))
	if err = publishBackup(destination, payloadName, manifest); err != nil {
		return BackupManifest{}, err
	}
	return manifest, nil
}

type boundedHashWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *boundedHashWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > writer.remaining {
		return 0, errors.New("authority backup exceeds maximum size")
	}
	n, err := writer.writer.Write(value)
	writer.remaining -= int64(n)
	return n, err
}

func publishBackup(destination, payload string, manifest BackupManifest) error {
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > 16384 {
		return errors.New("authority backup manifest is invalid")
	}
	output, err := os.CreateTemp(filepath.Dir(destination), ".authority-backup-*")
	if err != nil {
		return err
	}
	temporary := output.Name()
	defer os.Remove(temporary)
	if err = output.Chmod(0600); err == nil {
		_, err = output.Write(backupMagic[:])
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	if err == nil {
		_, err = output.Write(length[:])
	}
	if err == nil {
		_, err = output.Write(encoded)
	}
	if err == nil {
		var input *os.File
		input, err = os.Open(payload)
		if err == nil {
			_, err = io.Copy(output, input)
			_ = input.Close()
		}
	}
	if err == nil {
		err = output.Sync()
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = renameNoReplace(filepath.Dir(destination), filepath.Base(temporary), filepath.Dir(destination), filepath.Base(destination)); err != nil {
		return fmt.Errorf("publish authority backup: %w", err)
	}
	return syncDirectory(filepath.Dir(destination))
}

func cleanBackupPath(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil || filepath.Clean(absolute) != absolute || absolute == filepath.VolumeName(absolute)+string(filepath.Separator) {
		return "", errors.New("backup path must be a clean absolute non-root path")
	}
	if absolute == root || strings.HasPrefix(absolute+string(filepath.Separator), root+string(filepath.Separator)) {
		return "", errors.New("backup path must be outside the live authority root")
	}
	return absolute, nil
}

// Import verifies a complete backup before creating a fresh, inactive
// generation. Source generation and store identities are never reused.
func Import(ctx context.Context, root, source string) (Generation, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return Generation{}, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return Generation{}, err
	}
	defer lease.release()
	active, err := inspectClosedGeneration(root)
	if err != nil {
		return Generation{}, err
	}
	manifest, records, closeRecords, err := openBackup(source)
	if err != nil {
		return Generation{}, err
	}
	defer closeRecords()
	generationID, err := randomID()
	if err != nil {
		return Generation{}, err
	}
	storeID, err := randomID()
	if err != nil {
		return Generation{}, err
	}
	generation := Generation{GenerationID: generationID, StoreID: storeID, Schema: SchemaVersion, Codec: CodecVersion, Directory: "store-" + generationID + ".badger", Activation: active.Activation + 1}
	generation.Digest = generationDigest(generation)
	staged := filepath.Join(root, "staging", generation.Directory)
	if err = os.Mkdir(staged, 0700); err != nil {
		return Generation{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staged)
		}
	}()
	db, err := badgerdb.Open(options(staged))
	if err != nil {
		return Generation{}, err
	}
	batch := db.NewWriteBatch()
	err = importRecords(ctx, records, manifest, batch)
	if err == nil {
		err = batch.Set(mustMetadataKey(KeyMetadataStoreID), []byte(storeID))
	}
	if err == nil {
		err = batch.Set(mustMetadataKey(KeyMetadataSchema), []byte(SchemaVersion))
	}
	if err == nil {
		err = batch.Set(mustMetadataKey(KeyMetadataCodec), []byte(CodecVersion))
	}
	if err == nil {
		err = batch.Flush()
	} else {
		batch.Cancel()
	}
	if err == nil {
		err = db.Sync()
	}
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return Generation{}, err
	}
	if err = normalizeGenerationModes(staged); err != nil {
		return Generation{}, err
	}
	if err = verifyStoreIdentity(staged, generation); err != nil {
		return Generation{}, err
	}
	if err = renameNoReplace(filepath.Join(root, "staging"), generation.Directory, filepath.Join(root, "stores"), generation.Directory); err != nil {
		return Generation{}, err
	}
	published = true
	if err = syncDirectory(filepath.Join(root, "stores")); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

type backupRecords struct {
	reader    *bufio.Reader
	hash      hash.Hash
	remaining int64
	count     uint64
	previous  []byte
	identity  map[KeyFamily]string
}

func openBackup(path string) (BackupManifest, *backupRecords, func(), error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maximumBackupBytes+32768 {
		return BackupManifest{}, nil, func() {}, fmt.Errorf("%w: source is unsafe or oversized", ErrBackupCorrupt)
	}
	file, err := os.Open(path)
	if err != nil {
		return BackupManifest{}, nil, func() {}, err
	}
	closeFile := func() { _ = file.Close() }
	var magic [16]byte
	var length [4]byte
	if _, err = io.ReadFull(file, magic[:]); err != nil || magic != backupMagic {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	if _, err = io.ReadFull(file, length[:]); err != nil {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	manifestLength := binary.BigEndian.Uint32(length[:])
	if manifestLength == 0 || manifestLength > 16384 {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	encoded := make([]byte, manifestLength)
	if _, err = io.ReadFull(file, encoded); err != nil {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	var manifest BackupManifest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF || manifest.Format != BackupFormat || validateGeneration(manifest.Generation) != nil || manifest.Generation.Schema != SchemaVersion || manifest.Generation.Codec != CodecVersion || manifest.RecordCount > maximumBackupRecords || manifest.PayloadBytes < 0 || manifest.PayloadBytes > maximumBackupBytes || len(manifest.PayloadSHA256) != 64 {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	if _, err = hex.DecodeString(manifest.PayloadSHA256); err != nil || info.Size() != int64(16+4+manifestLength)+manifest.PayloadBytes {
		closeFile()
		return BackupManifest{}, nil, func() {}, ErrBackupCorrupt
	}
	digest := sha256.New()
	reader := bufio.NewReader(io.TeeReader(io.LimitReader(file, manifest.PayloadBytes), digest))
	return manifest, &backupRecords{reader: reader, hash: digest, remaining: manifest.PayloadBytes, identity: make(map[KeyFamily]string)}, closeFile, nil
}

func importRecords(ctx context.Context, records *backupRecords, manifest BackupManifest, batch *badgerdb.WriteBatch) error {
	for records.count < manifest.RecordCount {
		if err := ctx.Err(); err != nil {
			return err
		}
		var lengths [8]byte
		if _, err := io.ReadFull(records.reader, lengths[:]); err != nil {
			return ErrBackupCorrupt
		}
		keyLength, valueLength := binary.BigEndian.Uint32(lengths[0:4]), binary.BigEndian.Uint32(lengths[4:8])
		if keyLength == 0 || keyLength > maximumBackupKey || valueLength > maximumBackupValue || int64(8)+int64(keyLength)+int64(valueLength) > records.remaining {
			return ErrBackupCorrupt
		}
		key, value := make([]byte, keyLength), make([]byte, valueLength)
		if _, err := io.ReadFull(records.reader, key); err != nil {
			return ErrBackupCorrupt
		}
		if _, err := io.ReadFull(records.reader, value); err != nil {
			return ErrBackupCorrupt
		}
		records.remaining -= int64(8 + keyLength + valueLength)
		decoded, decodeErr := DecodeKey(key)
		if decodeErr != nil || (records.previous != nil && bytes.Compare(records.previous, key) >= 0) {
			return ErrBackupCorrupt
		}
		records.previous = append(records.previous[:0], key...)
		records.count++
		if decoded.Family == KeyMetadataStoreID || decoded.Family == KeyMetadataSchema || decoded.Family == KeyMetadataCodec {
			records.identity[decoded.Family] = string(value)
			continue
		}
		if err := batch.Set(key, value); err != nil {
			return err
		}
	}
	if records.remaining != 0 {
		return ErrBackupCorrupt
	}
	if records.identity[KeyMetadataStoreID] != manifest.Generation.StoreID || records.identity[KeyMetadataSchema] != manifest.Generation.Schema || records.identity[KeyMetadataCodec] != manifest.Generation.Codec {
		return ErrBackupCorrupt
	}
	if got := hex.EncodeToString(records.hash.Sum(nil)); got != manifest.PayloadSHA256 {
		return ErrBackupCorrupt
	}
	return nil
}

// Activate atomically selects an exact verified inactive generation. The prior
// active generation is retained for an explicit rollback.
func Activate(ctx context.Context, root string, candidate Generation) (Generation, error) {
	return selectGeneration(ctx, root, candidate)
}

// Rollback uses the same fail-closed selection path as activation and requires
// the caller to name the exact retained generation; it never guesses.
func Rollback(ctx context.Context, root string, target Generation) (Generation, error) {
	return selectGeneration(ctx, root, target)
}

func selectGeneration(ctx context.Context, root string, target Generation) (Generation, error) {
	root, err := cleanRoot(root)
	if err != nil {
		return Generation{}, err
	}
	if err = validateGeneration(target); err != nil {
		return Generation{}, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return Generation{}, err
	}
	defer lease.release()
	active, err := inspectClosedGeneration(root)
	if err != nil {
		return Generation{}, err
	}
	if active.GenerationID == target.GenerationID {
		return Generation{}, ErrGenerationActive
	}
	retiredActive := filepath.Join(root, "retired", active.Directory)
	if _, err = os.Lstat(retiredActive); err == nil {
		return Generation{}, fmt.Errorf("%w: prior generation retirement target already exists", ErrMaintenanceUnsafe)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, err
	}
	location := filepath.Join(root, "stores", target.Directory)
	if _, err = os.Lstat(location); errors.Is(err, os.ErrNotExist) {
		retired := filepath.Join(root, "retired", target.Directory)
		if err = verifyStoreIdentity(retired, target); err != nil {
			return Generation{}, err
		}
		if err = renameNoReplace(filepath.Join(root, "retired"), target.Directory, filepath.Join(root, "stores"), target.Directory); err != nil {
			return Generation{}, err
		}
		location = filepath.Join(root, "stores", target.Directory)
	} else if err != nil {
		return Generation{}, err
	}
	if err = verifyStoreIdentity(location, target); err != nil {
		return Generation{}, err
	}
	if err = ctx.Err(); err != nil {
		return Generation{}, err
	}
	target.Activation = active.Activation + 1
	target.Digest = generationDigest(target)
	if err = writeMarker(root, "ACTIVE", target); err != nil {
		return Generation{}, err
	}
	if err = writeMarker(root, "CLEAN", target); err != nil {
		return Generation{}, err
	}
	if err = renameNoReplace(filepath.Join(root, "stores"), active.Directory, filepath.Join(root, "retired"), active.Directory); err != nil {
		// Selection is already durable and safe. Retention failure is surfaced;
		// the old non-active generation remains verified under stores.
		return Generation{}, fmt.Errorf("generation activated but prior generation was not retired: %w", err)
	}
	if err = syncDirectory(filepath.Join(root, "retired")); err != nil {
		return Generation{}, err
	}
	return target, nil
}

// GarbageCollect removes only verified non-active generations. retainNewest is
// an explicit retention policy across stores and retired; negative values deny.
func GarbageCollect(ctx context.Context, root string, retainNewest int) ([]string, error) {
	if retainNewest < 0 {
		return nil, errors.New("garbage-collection retention must not be negative")
	}
	root, err := cleanRoot(root)
	if err != nil {
		return nil, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return nil, err
	}
	defer lease.release()
	active, err := inspectClosedGeneration(root)
	if err != nil {
		return nil, err
	}
	type candidate struct {
		directory string
		parent    string
		modified  time.Time
	}
	candidates := make([]candidate, 0)
	for _, parent := range []string{"stores", "retired"} {
		entries, readErr := os.ReadDir(filepath.Join(root, parent))
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if entry.Name() == active.Directory && parent == "stores" {
				continue
			}
			generation, inspectErr := generationFromDirectory(filepath.Join(root, parent, entry.Name()), entry.Name())
			if inspectErr != nil {
				return nil, fmt.Errorf("%w: refusing to collect %s: %v", ErrMaintenanceUnsafe, entry.Name(), inspectErr)
			}
			if generation.GenerationID == active.GenerationID {
				return nil, fmt.Errorf("%w: active identity is ambiguous", ErrMaintenanceUnsafe)
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return nil, infoErr
			}
			candidates = append(candidates, candidate{directory: entry.Name(), parent: parent, modified: info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modified.Equal(candidates[j].modified) {
			return candidates[i].directory > candidates[j].directory
		}
		return candidates[i].modified.After(candidates[j].modified)
	})
	removed := make([]string, 0)
	for index, candidate := range candidates {
		if index < retainNewest {
			continue
		}
		path := filepath.Join(root, candidate.parent, candidate.directory)
		if err = os.RemoveAll(path); err != nil {
			return removed, err
		}
		removed = append(removed, candidate.directory)
	}
	for _, parent := range []string{"stores", "retired"} {
		if err = syncDirectory(filepath.Join(root, parent)); err != nil {
			return removed, err
		}
	}
	sort.Strings(removed)
	return removed, nil
}

func generationFromDirectory(path, directory string) (Generation, error) {
	if !strings.HasPrefix(directory, "store-") || !strings.HasSuffix(directory, ".badger") {
		return Generation{}, ErrCorruptGeneration
	}
	generationID := strings.TrimSuffix(strings.TrimPrefix(directory, "store-"), ".badger")
	if len(generationID) != 32 {
		return Generation{}, ErrCorruptGeneration
	}
	db, err := badgerdb.Open(options(path).WithReadOnly(true))
	if err != nil {
		return Generation{}, err
	}
	defer db.Close()
	generation := Generation{GenerationID: generationID, Schema: SchemaVersion, Codec: CodecVersion, Directory: directory, Activation: 1}
	err = db.View(func(txn *badgerdb.Txn) error {
		value, getErr := getValue(txn, mustMetadataKey(KeyMetadataStoreID))
		if getErr != nil {
			return getErr
		}
		generation.StoreID = string(value)
		return nil
	})
	generation.Digest = generationDigest(generation)
	if err != nil || validateGeneration(generation) != nil {
		return Generation{}, ErrCorruptGeneration
	}
	if err = verifyStoreIdentity(path, generation); err != nil {
		return Generation{}, err
	}
	return generation, nil
}
