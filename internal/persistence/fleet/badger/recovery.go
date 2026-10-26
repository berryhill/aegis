package badger

// Recovery changes physical engine state only. It cannot renew a role, replay
// an effect, reclaim a claim, or infer a previously unknown outcome.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	physical "github.com/berryhill/aegis/internal/persistence/recoveryphysical"
	badgerdb "github.com/dgraph-io/badger/v4"
	"golang.org/x/sys/unix"
)

type InterruptedRecovery struct {
	EvidencePath string `json:"evidence_path"`
	Records      uint64 `json:"records"`
	Recovered    bool   `json:"recovered"`
}
type fleetRecoveryReceipt struct {
	Principal string
	Evidence  string
	Original  string
	Candidate string
	Phase     string
	Records   uint64
}

func recoverySecure(path string, directory bool) error {
	i, e := os.Lstat(path)
	if e != nil {
		return e
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != uint32(os.Geteuid()) {
		return errors.New("unsafe fleet recovery owner")
	}
	if directory {
		if !i.IsDir() || i.Mode().Perm() != 0700 {
			return errors.New("unsafe fleet recovery directory")
		}
	} else if !i.Mode().IsRegular() || i.Mode().Perm() != 0600 || s.Nlink != 1 {
		return errors.New("unsafe fleet recovery file")
	}
	return nil
}
func readFleetRecovery(root string) (r fleetRecoveryReceipt, err error) {
	p := filepath.Join(root, "recovery-evidence", "receipt.json")
	if err = recoverySecure(filepath.Dir(p), true); err != nil {
		return
	}
	if err = recoverySecure(p, false); err != nil {
		return
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return r, e
	}
	if len(b) > 16384 {
		return r, errors.New("oversized recovery receipt")
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	c, _ := json.Marshal(r)
	if !bytes.Equal(b, append(c, '\n')) || filepath.Dir(r.Evidence) != filepath.Dir(p) || r.Principal == "" || (r.Phase != "preserved" && r.Phase != "verified" && r.Phase != "completed") {
		return r, errors.New("invalid fleet recovery receipt")
	}
	return
}
func saveFleetRecovery(root string, r fleetRecoveryReceipt) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return writeMarker(filepath.Join(root, "recovery-evidence"), "receipt.json", string(b))
}
func requireFleetRecoveryComplete(root string) error {
	r, e := readFleetRecovery(root)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if r.Phase != "completed" {
		return errors.New("fleet recovery incomplete; authenticated recovery required")
	}
	return nil
}
func recoveryOptions(p string) badgerdb.Options {
	return badgerdb.DefaultOptions(p).WithValueDir(p).WithSyncWrites(true).WithLogger(nil).WithVerifyValueChecksum(true)
}

// RecoverInterruptedAs requires a principal authenticated by the owning
// initialization boundary. No normal Open path invokes this operation.
func RecoverInterruptedAs(ctx context.Context, root, principal string) (out InterruptedRecovery, err error) {
	if principal == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != schemaVersion {
		return out, errors.New("invalid fleet recovery scope")
	}
	if err = ctx.Err(); err != nil {
		return
	}
	for _, p := range []string{filepath.Dir(root), root} {
		if err = recoverySecure(p, true); err != nil {
			return
		}
	}
	lock := filepath.Join(root, "WRITER.lock")
	if err = recoverySecure(lock, false); err != nil {
		return
	}
	fd, e := unix.Open(lock, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return out, e
	}
	defer unix.Close(fd)
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return out, errors.New("fleet writer lock is held")
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return
	}
	var named unix.Stat_t
	if err = unix.Lstat(lock, &named); err != nil {
		return
	}
	if st.Ino != named.Ino || st.Dev != named.Dev || st.Nlink != 1 {
		return out, errors.New("fleet writer lock identity changed")
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return out, e
	}
	for _, v := range entries {
		switch v.Name() {
		case "WRITER.lock", "store.badger", "CLEAN", "DIRTY", "recovery-evidence":
		case "contribution-evidence":
			// Existing managed native checks keep operational evidence beside,
			// never inside, the engine. Preserve it in place; it is not replayed
			// or promoted into canonical records by physical recovery.
			if err = recoverySecure(filepath.Join(root, v.Name()), true); err != nil {
				return
			}
		default:
			return out, errors.New("unknown fleet root entry")
		}
	}
	selected := filepath.Join(root, "store.badger")
	r, e := readFleetRecovery(root)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return out, e
	}
	// A completed operation is reusable until normal Open changes its engine.
	if e == nil && r.Phase == "completed" {
		d, x := physical.Directory(ctx, selected, "")
		if x != nil {
			return out, x
		}
		if d != r.Candidate {
			e = os.ErrNotExist
		}
	}
	if errors.Is(e, os.ErrNotExist) {
		if _, x := os.Lstat(filepath.Join(root, "CLEAN")); !errors.Is(x, os.ErrNotExist) {
			return out, errors.New("fleet recovery requires absent CLEAN")
		}
		if err = recoverySecure(filepath.Join(root, "DIRTY"), false); err != nil {
			return
		}
		b, x := os.ReadFile(filepath.Join(root, "DIRTY"))
		if x != nil || string(b) != schemaVersion+"\n" {
			return out, errors.New("invalid fleet DIRTY marker")
		}
		parent := filepath.Join(root, "recovery-evidence")
		if err = os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return
		}
		if err = recoverySecure(parent, true); err != nil {
			return
		}
		// A stable operation directory retains interrupted snapshots too; never delete evidence.
		evidence, x := os.MkdirTemp(parent, "interrupted-")
		if x != nil {
			return out, x
		}
		if _, err = physical.Copy(ctx, filepath.Join(root, "DIRTY"), filepath.Join(evidence, "DIRTY")); err != nil {
			return
		}
		if _, err = physical.Copy(ctx, lock, filepath.Join(evidence, "WRITER.lock")); err != nil {
			return
		}
		original, x := physical.Directory(ctx, selected, filepath.Join(evidence, "store"))
		if x != nil {
			return out, x
		}
		r = fleetRecoveryReceipt{Principal: principal, Evidence: evidence, Original: original, Phase: "preserved"}
		if err = saveFleetRecovery(root, r); err != nil {
			return
		}
	}
	out.EvidencePath = r.Evidence
	out.Records = r.Records
	if r.Principal != principal {
		return out, errors.New("fleet recovery principal conflict")
	}
	original, e := physical.Directory(ctx, filepath.Join(r.Evidence, "store"), "")
	if e != nil {
		return out, e
	}
	if original != r.Original {
		return out, errors.New("fleet recovery evidence changed")
	}
	work := filepath.Join(r.Evidence, "work")
	if r.Phase == "preserved" {
		// Preserve an interrupted working copy, never replay it as fresh evidence.
		if _, x := os.Lstat(work); x == nil {
			archive, x := os.MkdirTemp(r.Evidence, "interrupted-work-")
			if x != nil {
				return out, x
			}
			if err = os.Rename(work, filepath.Join(archive, "work")); err != nil {
				return
			}
		}
		if _, err = physical.Directory(ctx, filepath.Join(r.Evidence, "store"), work); err != nil {
			return
		}
		if err = physical.ProveAndTrimLogs(ctx, work); err != nil {
			return
		}
		db, x := badgerdb.Open(recoveryOptions(work).WithReadOnly(true))
		if x != nil {
			return out, x
		}
		before, count, x := verifyFleetRecovery(ctx, db)
		err = errors.Join(x, db.VerifyChecksum(), db.Close())
		if err != nil {
			return
		}
		db, x = badgerdb.Open(recoveryOptions(work))
		if x != nil {
			return out, x
		}
		if err = db.Close(); err != nil {
			return
		}
		if err = normalizeStoreModes(work); err != nil {
			return
		}
		db, x = badgerdb.Open(recoveryOptions(work).WithReadOnly(true))
		if x != nil {
			return out, x
		}
		after, _, x := verifyFleetRecovery(ctx, db)
		err = errors.Join(x, db.VerifyChecksum(), db.Close())
		if err != nil {
			return
		}
		if before != after {
			return out, errors.New("fleet canonical bytes changed during replay")
		}
		r.Candidate, err = physical.Directory(ctx, work, "")
		if err != nil {
			return
		}
		r.Records = count
		r.Phase = "verified"
		if err = saveFleetRecovery(root, r); err != nil {
			return
		}
	}
	a, e := physical.Directory(ctx, selected, "")
	if e != nil {
		return out, e
	}
	w, e := physical.Directory(ctx, work, "")
	if e != nil {
		return out, e
	}
	if a == r.Original && w == r.Candidate && r.Phase == "verified" {
		if err = ctx.Err(); err != nil {
			return
		}
		if err = physical.Exchange(selected, work); err != nil {
			return
		}
	} else if a != r.Candidate || w != r.Original {
		return out, errors.New("fleet recovery activation identity conflict")
	}
	for _, p := range []string{root, r.Evidence} {
		if err = syncDirectory(p); err != nil {
			return
		}
	}
	if err = ctx.Err(); err != nil {
		return
	}
	// The receipt fences normal Open across either marker publication crash.
	if err = writeMarker(root, "CLEAN", schemaVersion); err != nil {
		return
	}
	if err = os.Remove(filepath.Join(root, "DIRTY")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return
	}
	if err = syncDirectory(root); err != nil {
		return
	}
	r.Phase = "completed"
	if err = saveFleetRecovery(root, r); err != nil {
		return
	}
	out.Records = r.Records
	out.Recovered = true
	return out, nil
}

var _ = fmt.Errorf
