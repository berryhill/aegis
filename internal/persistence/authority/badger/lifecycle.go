package badger

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	authoritypersistence "github.com/berryhill/aegis/internal/persistence/authority"
	badgerdb "github.com/dgraph-io/badger/v4"
	"golang.org/x/sys/unix"
)

const (
	SchemaVersion = "authority-v1"
	CodecVersion  = "aegis-binary-v1"
)

var (
	ErrAlreadyInitialized = errors.New("authority persistence is already initialized")
	ErrNotInitialized     = errors.New("authority persistence is not initialized")
	ErrCorruptGeneration  = errors.New("authority persistence generation is corrupt")
	ErrClosed             = errors.New("authority persistence is closed")
)

type Generation struct {
	GenerationID string `json:"generation_id"`
	StoreID      string `json:"store_id"`
	Schema       string `json:"schema"`
	Codec        string `json:"codec"`
	Directory    string `json:"directory"`
	Activation   uint64 `json:"activation"`
	Digest       string `json:"digest"`
}

type State string

const (
	StateAbsent  State = "absent"
	StateReady   State = "ready"
	StateInvalid State = "invalid"
)

type Inspection struct {
	State      State      `json:"state"`
	Generation Generation `json:"generation,omitempty"`
	Err        error      `json:"-"`
}

type Store struct {
	db         *badgerdb.DB
	root       string
	generation Generation
	lease      *maintenanceLease
	mu         sync.Mutex
	closed     bool
	closeErr   error
}

func Initialize(ctx context.Context, root string) (Generation, error) {
	return initialize(ctx, root, false)
}

// InitializeEmpty publishes a new generation after confirmed exact absence, or
// returns the same generation when a concurrent initializer won and the closed
// store still contains identity metadata only.
func InitializeEmpty(ctx context.Context, root string) (Generation, error) {
	return initialize(ctx, root, true)
}

func initialize(ctx context.Context, root string, acceptConcurrentEmpty bool) (Generation, error) {
	if err := ctx.Err(); err != nil {
		return Generation{}, err
	}
	root, err := cleanRoot(root)
	if err != nil {
		return Generation{}, err
	}
	stateRoot := filepath.Dir(filepath.Dir(root))
	if _, err = authoritypersistence.ClassifyLegacyAuthority(stateRoot); err != nil {
		return Generation{}, err
	}
	for _, path := range []string{stateRoot, filepath.Dir(root)} {
		if err = secureMkdir(path); err != nil {
			return Generation{}, err
		}
	}
	initialization, err := acquireInitialization(ctx, filepath.Dir(root))
	if err != nil {
		return Generation{}, err
	}
	defer initialization.release()
	if _, err = authoritypersistence.ClassifyLegacyAuthority(stateRoot); err != nil {
		return Generation{}, err
	}
	if info, statErr := os.Lstat(root); statErr == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || !ownedByCurrentEUID(info) {
			return Generation{}, errors.New("existing authority persistence root is unsafe")
		}
		inspection := Inspect(ctx, root)
		if inspection.State == StateReady {
			if acceptConcurrentEmpty {
				if err = verifyEmptyClosedGeneration(root, inspection.Generation); err != nil {
					return Generation{}, err
				}
				return inspection.Generation, nil
			}
			return Generation{}, ErrAlreadyInitialized
		}
		return Generation{}, fmt.Errorf("existing authority persistence is invalid and will not be replaced: %w", inspection.Err)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Generation{}, fmt.Errorf("inspect authority persistence root: %w", statErr)
	}
	if err = secureMkdir(root); err != nil {
		return Generation{}, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return Generation{}, err
	}
	defer lease.release()
	if _, err = os.Lstat(filepath.Join(root, "ACTIVE")); err == nil {
		return Generation{}, ErrAlreadyInitialized
	} else if !errors.Is(err, os.ErrNotExist) {
		return Generation{}, fmt.Errorf("inspect ACTIVE: %w", err)
	}
	for _, path := range []string{filepath.Join(root, "stores"), filepath.Join(root, "staging"), filepath.Join(root, "retired")} {
		if err = secureMkdir(path); err != nil {
			return Generation{}, err
		}
	}
	if err = CheckDiskReserve(root, uint64(maximumBackupBytes)); err != nil {
		return Generation{}, err
	}
	generationID, err := randomID()
	if err != nil {
		return Generation{}, err
	}
	storeID, err := randomID()
	if err != nil {
		return Generation{}, err
	}
	basename := "store-" + generationID + ".badger"
	staged := filepath.Join(root, "staging", basename)
	if err = os.Mkdir(staged, 0700); err != nil {
		return Generation{}, fmt.Errorf("create authority candidate: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = removeTreeAt(filepath.Join(root, "staging"), basename)
		}
	}()
	generation := Generation{GenerationID: generationID, StoreID: storeID, Schema: SchemaVersion, Codec: CodecVersion, Directory: basename, Activation: 1}
	generation.Digest = generationDigest(generation)
	db, err := badgerdb.Open(options(staged))
	if err != nil {
		return Generation{}, fmt.Errorf("open authority candidate: %w", err)
	}
	closeCandidate := true
	defer func() {
		if closeCandidate {
			_ = db.Close()
		}
	}()
	if err = db.Update(func(txn *badgerdb.Txn) error {
		if err := txn.Set(mustMetadataKey(KeyMetadataStoreID), []byte(storeID)); err != nil {
			return err
		}
		if err := txn.Set(mustMetadataKey(KeyMetadataSchema), []byte(SchemaVersion)); err != nil {
			return err
		}
		return txn.Set(mustMetadataKey(KeyMetadataCodec), []byte(CodecVersion))
	}); err != nil {
		return Generation{}, fmt.Errorf("write authority identity: %w", err)
	}
	if err = db.Sync(); err != nil {
		return Generation{}, fmt.Errorf("sync authority candidate: %w", err)
	}
	if err = db.Close(); err != nil {
		return Generation{}, fmt.Errorf("close authority candidate: %w", err)
	}
	closeCandidate = false
	if err = normalizeGenerationModes(staged); err != nil {
		return Generation{}, fmt.Errorf("secure authority candidate: %w", err)
	}
	if err = renameNoReplace(filepath.Join(root, "staging"), basename, filepath.Join(root, "stores"), basename); err != nil {
		return Generation{}, fmt.Errorf("publish authority candidate: %w", err)
	}
	published = true
	if err = syncDirectory(filepath.Join(root, "staging")); err != nil {
		return Generation{}, err
	}
	if err = syncDirectory(filepath.Join(root, "stores")); err != nil {
		return Generation{}, err
	}
	if err = verifyStoreIdentity(filepath.Join(root, "stores", basename), generation); err != nil {
		return Generation{}, err
	}
	if err = writeMarker(root, "ACTIVE", generation); err != nil {
		return Generation{}, err
	}
	if err = writeMarker(root, "CLEAN", generation); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func verifyEmptyClosedGeneration(root string, generation Generation) error {
	db, err := badgerdb.Open(options(filepath.Join(root, "stores", generation.Directory)).WithReadOnly(true))
	if err != nil {
		return fmt.Errorf("verify concurrently initialized operational authority: %w", err)
	}
	defer db.Close()
	records := 0
	err = db.View(func(txn *badgerdb.Txn) error {
		iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			decoded, decodeErr := DecodeKey(iterator.Item().Key())
			if decodeErr != nil {
				return decodeErr
			}
			switch decoded.Family {
			case KeyMetadataStoreID, KeyMetadataSchema, KeyMetadataCodec:
				records++
			default:
				return errors.New("concurrently initialized operational authority is not an empty generation")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if records != 3 {
		return errors.New("concurrently initialized operational authority has incomplete identity metadata")
	}
	return nil
}

// Inspect classifies the operational authority without creating directories,
// lifecycle markers, locks, or writable database handles.
func Inspect(ctx context.Context, root string) Inspection {
	if err := ctx.Err(); err != nil {
		return Inspection{State: StateInvalid, Err: err}
	}
	clean, err := cleanRoot(root)
	if err != nil {
		return Inspection{State: StateInvalid, Err: err}
	}
	stateRoot := filepath.Dir(filepath.Dir(clean))
	if _, err = authoritypersistence.ClassifyLegacyAuthority(stateRoot); err != nil {
		return Inspection{State: StateInvalid, Err: err}
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, os.ErrNotExist) {
		return Inspection{State: StateAbsent}
	}
	if err != nil {
		return Inspection{State: StateInvalid, Err: err}
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || !ownedByCurrentEUID(info) {
		return Inspection{State: StateInvalid, Err: errors.New("operational authority root is unsafe")}
	}
	generation, err := inspectClosedGeneration(clean)
	if err != nil {
		return Inspection{State: StateInvalid, Err: err}
	}
	return Inspection{State: StateReady, Generation: generation}
}

func Open(ctx context.Context, root string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := cleanRoot(root)
	if err != nil {
		return nil, err
	}
	if _, err = authoritypersistence.ClassifyLegacyAuthority(filepath.Dir(filepath.Dir(root))); err != nil {
		return nil, err
	}
	if _, err = os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	} else if err != nil {
		return nil, err
	}
	lease, err := acquireMaintenance(ctx, root, false)
	if err != nil {
		return nil, err
	}
	opened := false
	defer func() {
		if !opened {
			lease.release()
		}
	}()
	generation, err := readMarker(filepath.Join(root, "ACTIVE"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	if err = validateGeneration(generation); err != nil {
		return nil, err
	}
	selected := filepath.Join(root, "stores", generation.Directory)
	if err = verifyStoreIdentity(selected, generation); err != nil {
		return nil, err
	}
	if err = removeFileAt(root, "CLEAN"); err != nil {
		return nil, err
	}
	if err = writeMarker(root, "DIRTY", generation); err != nil {
		return nil, err
	}
	db, err := badgerdb.Open(options(selected))
	if err != nil {
		return nil, fmt.Errorf("open selected authority generation: %w", err)
	}
	opened = true
	return &Store{db: db, root: root, generation: generation, lease: lease}, nil
}

func (s *Store) Generation() Generation { return s.generation }

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	defer s.lease.release()
	if err := s.db.Sync(); err != nil {
		s.closeErr = err
		return err
	}
	if err := s.db.Close(); err != nil {
		s.closeErr = err
		return err
	}
	if err := normalizeGenerationModes(filepath.Join(s.root, "stores", s.generation.Directory)); err != nil {
		s.closeErr = err
		return err
	}
	if err := removeFileAt(s.root, "DIRTY"); err != nil {
		s.closeErr = err
		return err
	}
	s.closeErr = writeMarker(s.root, "CLEAN", s.generation)
	return s.closeErr
}

func options(path string) badgerdb.Options {
	return badgerdb.DefaultOptions(path).WithDir(path).WithValueDir(path).WithSyncWrites(true).WithDetectConflicts(true).WithLogger(nil)
}

func cleanRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil || filepath.Clean(absolute) != absolute || absolute == filepath.VolumeName(absolute)+string(filepath.Separator) {
		return "", errors.New("authority persistence root must be a clean absolute non-root path")
	}
	if filepath.Base(absolute) != "authority-v1" || filepath.Base(filepath.Dir(absolute)) != "persistence" {
		return "", errors.New("authority persistence root must end in persistence/authority-v1")
	}
	return absolute, nil
}

func secureMkdir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.Mkdir(path, 0700); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create secure authority directory %s: %w", path, err)
			}
			info, err = os.Lstat(path)
			if err != nil {
				return fmt.Errorf("inspect concurrently created authority directory %s: %w", path, err)
			}
		} else {
			return syncDirectory(filepath.Dir(path))
		}
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || !ownedByCurrentEUID(info) {
		return fmt.Errorf("authority directory %s is unsafe", path)
	}
	return nil
}

func acquireInitialization(ctx context.Context, persistenceRoot string) (*maintenanceLease, error) {
	fd, err := unix.Open(persistenceRoot, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open authority initialization coordinator: %w", err)
	}
	file := os.NewFile(uintptr(fd), persistenceRoot)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open authority initialization coordinator: invalid file descriptor")
	}
	info, err := file.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrentEUID(info) {
		_ = file.Close()
		return nil, errors.New("authority initialization coordinator is unsafe")
	}
	for {
		if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
			return &maintenanceLease{file: file}, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = file.Close()
			return nil, fmt.Errorf("lock authority initialization coordinator: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// normalizeGenerationModes makes Badger's umask-dependent files deterministic.
// The enclosing 0700 tree prevents cross-user access while the store is open;
// normalization leaves a closed generation safe for reset and offline handling.
func normalizeGenerationModes(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("authority generation contains a symlink")
		}
		mode := os.FileMode(0600)
		if entry.IsDir() {
			mode = 0700
		} else if !info.Mode().IsRegular() {
			return errors.New("authority generation contains a non-regular file")
		}
		return os.Chmod(path, mode)
	})
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func generationDigest(g Generation) string {
	body := fmt.Sprintf("aegis-authority-generation-v1\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d", g.GenerationID, g.StoreID, g.Schema, g.Codec, g.Directory, g.Activation)
	digest := sha256.Sum256([]byte(body))
	return hex.EncodeToString(digest[:])
}

func validateGeneration(g Generation) error {
	if len(g.GenerationID) != 32 || len(g.StoreID) != 32 || g.Schema != SchemaVersion || g.Codec != CodecVersion || g.Directory != "store-"+g.GenerationID+".badger" || g.Activation == 0 || g.Digest != generationDigest(g) {
		return ErrCorruptGeneration
	}
	if _, err := hex.DecodeString(g.GenerationID); err != nil {
		return ErrCorruptGeneration
	}
	if _, err := hex.DecodeString(g.StoreID); err != nil {
		return ErrCorruptGeneration
	}
	return nil
}

func verifyStoreIdentity(path string, generation Generation) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || !ownedByCurrentEUID(info) {
		return fmt.Errorf("%w: selected store path is unsafe", ErrCorruptGeneration)
	}
	db, err := badgerdb.Open(options(path).WithReadOnly(true))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorruptGeneration, err)
	}
	defer db.Close()
	return db.View(func(txn *badgerdb.Txn) error {
		for family, want := range map[KeyFamily]string{KeyMetadataStoreID: generation.StoreID, KeyMetadataSchema: generation.Schema, KeyMetadataCodec: generation.Codec} {
			key := mustMetadataKey(family)
			decoded, decodeErr := DecodeKey(key)
			if decodeErr != nil || decoded.Family != family {
				return ErrCorruptGeneration
			}
			item, getErr := txn.Get(key)
			if getErr != nil {
				return ErrCorruptGeneration
			}
			value, valueErr := item.ValueCopy(nil)
			if valueErr != nil || string(value) != want {
				return ErrCorruptGeneration
			}
		}
		return nil
	})
}

func writeMarker(root, name string, generation Generation) error {
	if err := validateGeneration(generation); err != nil {
		return err
	}
	content, err := json.Marshal(generation)
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeFileAtomicAt(root, name, content)
}

func readMarker(path string) (Generation, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Generation{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentEUID(info) {
		return Generation{}, ErrCorruptGeneration
	}
	file, err := os.Open(path)
	if err != nil {
		return Generation{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(file, 4097)))
	decoder.DisallowUnknownFields()
	var generation Generation
	if err = decoder.Decode(&generation); err != nil {
		return Generation{}, ErrCorruptGeneration
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return Generation{}, ErrCorruptGeneration
	}
	if err = validateGeneration(generation); err != nil {
		return Generation{}, err
	}
	return generation, nil
}

func ownedByCurrentEUID(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid()
}

func renameNoReplace(fromDir, from, toDir, to string) error {
	source, err := openDirectoryNoFollow(fromDir)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := openDirectoryNoFollow(toDir)
	if err != nil {
		return err
	}
	defer destination.Close()
	return renameNoReplaceAt(int(source.Fd()), from, int(destination.Fd()), to)
}

func syncDirectory(path string) error {
	directory, err := openDirectoryNoFollow(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
