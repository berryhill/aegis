package badger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
	"golang.org/x/sys/unix"
)

const (
	schemaVersion = "fleet-v1"
	minimumFree   = uint64(256 << 20)
)

var _ fleet.Repository = (*Store)(nil)

// Store owns one exclusive fleet-v1 Badger process lifetime.
type Store struct {
	db       *badgerdb.DB
	lockFile *os.File
	root     string
	mu       sync.Mutex
	closed   bool
}

// Open opens exactly one fleet-v1 root. A previous unclean open is denied;
// verified recovery is intentionally a separate, controller-authorized action.
func Open(ctx context.Context, root string) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clean, err := filepath.Abs(filepath.Clean(root))
	if err != nil || clean == string(filepath.Separator) || !filepath.IsAbs(clean) || filepath.Base(clean) != schemaVersion {
		return nil, errors.New("fleet root must be an absolute non-root fleet-v1 path")
	}
	if info, statErr := os.Lstat(clean); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 {
			return nil, errors.New("fleet root type or mode is unsafe")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	} else if err = os.MkdirAll(clean, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(clean, 0700); err != nil {
		return nil, err
	}
	if err = checkReserve(clean); err != nil {
		return nil, err
	}

	lockFile, err := os.OpenFile(filepath.Join(clean, "WRITER.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, errors.New("fleet writer lock is held")
	}
	fail := func(cause error) (*Store, error) {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, cause
	}

	dbPath := filepath.Join(clean, "store.badger")
	_, dbErr := os.Lstat(dbPath)
	_, cleanErr := os.Lstat(filepath.Join(clean, "CLEAN"))
	_, dirtyErr := os.Lstat(filepath.Join(clean, "DIRTY"))
	if cleanErr == nil && dirtyErr == nil {
		return fail(errors.New("conflicting fleet lifecycle markers require verified recovery"))
	}
	if dbErr == nil && errors.Is(cleanErr, os.ErrNotExist) {
		return fail(errors.New("fleet store is dirty and requires verified recovery"))
	}
	if dbErr != nil && !errors.Is(dbErr, os.ErrNotExist) {
		return fail(dbErr)
	}
	if cleanErr != nil && !errors.Is(cleanErr, os.ErrNotExist) {
		return fail(cleanErr)
	}
	if dirtyErr != nil && !errors.Is(dirtyErr, os.ErrNotExist) {
		return fail(dirtyErr)
	}
	if err = writeMarker(clean, "DIRTY", schemaVersion); err != nil {
		return fail(err)
	}
	if err = os.Remove(filepath.Join(clean, "CLEAN")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	if err = syncDirectory(clean); err != nil {
		return fail(err)
	}
	if err = os.Mkdir(dbPath, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return fail(err)
	}
	options := badgerdb.DefaultOptions(dbPath).WithValueDir(dbPath).WithSyncWrites(true).WithDetectConflicts(true).WithLogger(nil)
	db, err := badgerdb.Open(options)
	if err != nil {
		return fail(fmt.Errorf("open fleet Badger: %w", err))
	}
	store := &Store{db: db, lockFile: lockFile, root: clean}
	if err = store.ensureSchema(ctx); err != nil {
		_ = db.Close()
		return fail(err)
	}
	return store, nil
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		item, err := txn.Get([]byte("\x01schema"))
		if errors.Is(err, badgerdb.ErrKeyNotFound) {
			return txn.Set([]byte("\x01schema"), []byte(schemaVersion))
		}
		if err != nil {
			return err
		}
		value, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		if string(value) != schemaVersion {
			return errors.New("fleet schema does not match fleet-v1")
		}
		return nil
	})
}

func (s *Store) view(ctx context.Context, fn func(*badgerdb.Txn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fleet.ErrClosed
	}
	return s.db.View(fn)
}
func (s *Store) update(ctx context.Context, fn func(*badgerdb.Txn) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fleet.ErrClosed
	}
	if err := checkReserve(s.root); err != nil {
		return err
	}
	return s.db.Update(fn)
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fleet.ErrClosed
	}
	s.closed = true
	err := s.db.Sync()
	dirtyRemoved := false
	if closeErr := s.db.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = normalizeStoreModes(filepath.Join(s.root, "store.badger"))
	}
	if err == nil {
		_, cleanErr := os.Lstat(filepath.Join(s.root, "CLEAN"))
		if cleanErr == nil {
			err = errors.New("unexpected CLEAN marker appeared while fleet store was open")
		} else if !errors.Is(cleanErr, os.ErrNotExist) {
			err = cleanErr
		}
	}
	if err == nil {
		err = os.Remove(filepath.Join(s.root, "DIRTY"))
		if err == nil {
			dirtyRemoved = true
		}
	}
	if err == nil {
		err = syncDirectory(s.root)
	}
	if err == nil {
		err = writeMarkerNoReplace(s.root, "CLEAN", schemaVersion)
	}
	if err != nil && dirtyRemoved {
		err = errors.Join(err, writeMarker(s.root, "DIRTY", schemaVersion))
	}
	unlockErr := syscall.Flock(int(s.lockFile.Fd()), syscall.LOCK_UN)
	closeLockErr := s.lockFile.Close()
	if err == nil {
		err = unlockErr
	}
	if err == nil {
		err = closeLockErr
	}
	return err
}

// normalizeStoreModes makes Badger's umask-dependent artifacts deterministic
// after the database has closed and before the store is declared clean. Reset
// and offline readers can then retain strict per-artifact mode validation.
func normalizeStoreModes(root string) error {
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	if err = normalizeDirectoryModes(rootFD); err != nil {
		return err
	}
	if err = unix.Fchmod(rootFD, 0700); err != nil {
		return err
	}
	return unix.Fsync(rootFD)
}

func normalizeDirectoryModes(directoryFD int) error {
	readFD, err := unix.Dup(directoryFD)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(readFD), "fleet-store-directory")
	if directory == nil {
		_ = unix.Close(readFD)
		return errors.New("open fleet store directory")
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "." || name == ".." || filepath.Base(name) != name {
			return errors.New("invalid fleet store entry")
		}
		var discovered unix.Stat_t
		if err = unix.Fstatat(directoryFD, name, &discovered, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		switch uint32(discovered.Mode) & unix.S_IFMT {
		case unix.S_IFDIR:
			childFD, openErr := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
			if openErr != nil {
				return openErr
			}
			if err = verifyOpenedIdentity(childFD, discovered, unix.S_IFDIR); err == nil {
				err = normalizeDirectoryModes(childFD)
			}
			if err == nil {
				err = unix.Fchmod(childFD, 0700)
			}
			if err == nil {
				err = unix.Fsync(childFD)
			}
			closeErr = unix.Close(childFD)
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
		case unix.S_IFREG:
			childFD, openErr := unix.Openat(directoryFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if openErr != nil {
				return openErr
			}
			if err = verifyOpenedIdentity(childFD, discovered, unix.S_IFREG); err == nil {
				err = unix.Fchmod(childFD, 0600)
			}
			if err == nil {
				err = unix.Fsync(childFD)
			}
			closeErr = unix.Close(childFD)
			if err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
		default:
			return errors.New("fleet store contains an unexpected file type")
		}
	}
	return nil
}

func verifyOpenedIdentity(fd int, discovered unix.Stat_t, fileType uint32) error {
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return err
	}
	if uint32(opened.Mode)&unix.S_IFMT != fileType || opened.Dev != discovered.Dev || opened.Ino != discovered.Ino {
		return errors.New("fleet store entry changed during mode normalization")
	}
	return nil
}

func checkReserve(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return err
	}
	if stat.Bavail*uint64(stat.Bsize) < minimumFree {
		return errors.New("fleet mutation denied: 256 MiB free reserve unavailable")
	}
	return nil
}

func writeMarker(root, name, value string) error {
	temporary, err := os.CreateTemp(root, ".marker-")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.WriteString(value + "\n")
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(path, filepath.Join(root, name)); err != nil {
		return err
	}
	return syncDirectory(root)
}

func writeMarkerNoReplace(root, name, value string) error {
	temporary, err := os.CreateTemp(root, ".marker-")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.WriteString(value + "\n")
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(path, filepath.Join(root, name)); err != nil {
		return err
	}
	return syncDirectory(root)
}

func syncDirectory(root string) error {
	directory, err := os.Open(root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
