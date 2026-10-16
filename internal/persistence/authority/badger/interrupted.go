package badger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/berryhill/aegis/internal/core"
	authoritypersistence "github.com/berryhill/aegis/internal/persistence/authority"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// InterruptedRecovery is evidence of validation, not a new authority grant.
type InterruptedRecovery struct {
	Generation   Generation `json:"generation"`
	EvidencePath string     `json:"evidence_path"`
	Records      uint64     `json:"records"`
	Recovered    bool       `json:"recovered"`
}

// RecoverInterrupted admits only an exact interrupted-open generation. It never
// replays a damaged engine, selects another generation, or rebuilds projections.
// The caller must authenticate the operator before invoking this persistence API.
func RecoverInterrupted(ctx context.Context, root string) (InterruptedRecovery, error) {
	return RecoverInterruptedAs(ctx, root, "offline-maintenance")
}

func RecoverInterruptedAs(ctx context.Context, root, principal string) (result InterruptedRecovery, err error) {
	if err = recoveryExchangeSupported(); err != nil {
		return result, err
	}
	if principal == "" {
		return result, errors.New("missing recovery principal")
	}
	root, err = cleanRoot(root)
	if err != nil {
		return
	}
	if _, err = authoritypersistence.ClassifyLegacyAuthority(filepath.Dir(filepath.Dir(root))); err != nil {
		return
	}
	if err = requireSecureDirectory(filepath.Dir(root)); err != nil {
		return result, err
	}
	lease, err := acquireMaintenance(ctx, root, true)
	if err != nil {
		return result, err
	}
	defer lease.release()
	if resumed, found, e := resumeRecovery(ctx, root, principal); found || e != nil {
		return resumed, e
	}
	generation, err := inspectInterrupted(root)
	if err != nil {
		return result, err
	}
	result.Generation = generation
	// Snapshot before even a read-only engine open. Partial snapshots are retained.
	result.EvidencePath, err = preserveInterrupted(ctx, root, generation)
	if err != nil {
		return result, err
	}
	selected := filepath.Join(root, "stores", generation.Directory)
	work := filepath.Join(result.EvidencePath, "work")
	if _, err = recoveryDirectory(ctx, filepath.Join(result.EvidencePath, "store"), work); err != nil {
		return result, err
	}
	if err = proveAndTrimLogs(ctx, work); err != nil {
		return result, err
	}
	if err = verifyStoreIdentity(work, generation); err != nil {
		return result, err
	}
	db, err := badgerdb.Open(options(work).WithReadOnly(true).WithVerifyValueChecksum(true))
	if err != nil {
		return result, err
	}
	before, digestErr := recoveryCanonicalDigest(ctx, db)
	if digestErr != nil {
		return result, errors.Join(digestErr, db.Close())
	}
	err = db.VerifyChecksum()
	if err == nil {
		err = db.View(func(txn *badgerdb.Txn) error { var e error; result.Records, e = verifyAllAuthority(ctx, txn); return e })
	}
	err = errors.Join(err, db.Close())
	if err != nil {
		return result, fmt.Errorf("interrupted authority validation refused: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	current, err := inspectInterrupted(root)
	if err != nil || current != generation {
		return result, errors.Join(err, errors.New("interrupted authority changed during verification"))
	}
	// Flush only the independently proven working copy, never the original.
	db, err = badgerdb.Open(options(work).WithVerifyValueChecksum(true))
	if err != nil {
		return result, err
	}
	if err = db.Close(); err != nil {
		return result, err
	}
	if err = normalizeGenerationModes(work); err != nil {
		return result, err
	}
	if err = verifyStoreIdentity(work, generation); err != nil {
		return result, err
	}
	db, err = badgerdb.Open(options(work).WithReadOnly(true).WithVerifyValueChecksum(true))
	if err != nil {
		return result, err
	}
	after, digestErr := recoveryCanonicalDigest(ctx, db)
	verifyErr := db.VerifyChecksum()
	err = errors.Join(digestErr, verifyErr, db.Close())
	if err != nil {
		return result, err
	}
	if before != after {
		return result, errors.New("canonical bytes changed during replay")
	}
	candidate, err := recoveryDirectory(ctx, work, "")
	if err != nil {
		return result, err
	}
	original, err := recoveryDirectory(ctx, selected, "")
	if err != nil {
		return result, err
	}
	evidence, err := recoveryDirectory(ctx, filepath.Join(result.EvidencePath, "store"), "")
	if err != nil || original != evidence {
		return result, errors.New("original changed after snapshot")
	}
	receipt := recoveryReceipt{OperationID: filepath.Base(result.EvidencePath), Principal: principal, Operation: "recover-interrupted", Generation: generation, Evidence: result.EvidencePath, EvidenceDigest: evidence, CandidateDigest: candidate, Phase: "verified", Outcome: "pending", Records: result.Records}
	if err = recoveryBoundary(ctx, "before-receipt"); err != nil {
		return result, err
	}
	if err = saveRecoveryReceipt(root, receipt); err != nil {
		return result, err
	}
	if err = recoveryBoundary(ctx, "receipt"); err != nil {
		return result, err
	}
	resumed, _, err := resumeRecovery(ctx, root, principal)
	return resumed, err
}

func inspectInterrupted(root string) (Generation, error) {
	fail := func(reason string) (Generation, error) {
		return Generation{}, fmt.Errorf("%w: interrupted-open recovery refused: %s", ErrMaintenanceUnsafe, reason)
	}
	for _, name := range []string{"", "stores", "staging", "retired"} {
		if err := requireSecureDirectory(filepath.Join(root, name)); err != nil {
			return fail(err.Error())
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return Generation{}, err
	}
	for _, e := range entries {
		switch e.Name() {
		case "ACTIVE", "DIRTY", "MAINTENANCE", "stores", "staging", "retired", "recovery-evidence":
		default:
			return fail("unexpected root entry")
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "CLEAN")); !errors.Is(err, os.ErrNotExist) {
		return fail("CLEAN must be absent")
	}
	active, err := readMarker(filepath.Join(root, "ACTIVE"))
	if err != nil {
		return fail("invalid ACTIVE")
	}
	dirty, err := readMarker(filepath.Join(root, "DIRTY"))
	if err != nil || dirty != active {
		return fail("DIRTY must exactly match ACTIVE")
	}
	for _, name := range []string{"stores", "staging", "retired"} {
		entries, err := os.ReadDir(filepath.Join(root, name))
		if err != nil {
			return Generation{}, err
		}
		if name == "stores" {
			if len(entries) != 1 || entries[0].Name() != active.Directory {
				return fail("stores must contain only ACTIVE")
			}
		} else if len(entries) != 0 {
			return fail(name + " must be empty")
		}
	}
	if err := verifyStorePath(filepath.Join(root, "stores", active.Directory)); err != nil {
		return fail(err.Error())
	}
	return active, nil
}

func preserveInterrupted(ctx context.Context, root string, generation Generation) (string, error) {
	parent := filepath.Join(root, "recovery-evidence")
	if err := secureMkdir(parent); err != nil {
		return "", err
	}
	destination, err := os.MkdirTemp(parent, "interrupted-")
	if err != nil {
		return "", err
	}
	sums := map[string]struct {
		SHA256     string `json:"sha256"`
		Size       int64  `json:"size"`
		Mode       uint32 `json:"mode"`
		ModifiedAt string `json:"modified_at"`
	}{}
	copyFile := func(source, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Lstat(source)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || !ownedByCurrentEUID(info) || stat.Nlink != 1 || info.Mode().Perm()&0022 != 0 {
			return errors.New("unsafe interrupted authority evidence source")
		}
		if err := CheckDiskReserve(destination, uint64(info.Size())); err != nil {
			return err
		}
		sum, err := recoveryCopy(ctx, source, filepath.Join(destination, name))
		if err != nil {
			return err
		}
		entry := sums[name]
		entry.SHA256 = sum
		entry.Size = info.Size()
		entry.Mode = uint32(info.Mode().Perm())
		entry.ModifiedAt = info.ModTime().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		sums[name] = entry
		return nil
	}
	for _, name := range []string{"ACTIVE", "DIRTY"} {
		if err = copyFile(filepath.Join(root, name), name); err != nil {
			return destination, err
		}
	}
	selected := filepath.Join(root, "stores", generation.Directory)
	entries, err := os.ReadDir(selected)
	if err != nil {
		return destination, err
	}
	if err = os.Mkdir(filepath.Join(destination, "store"), 0700); err != nil {
		return destination, err
	}
	if len(entries) > recoveryFileLimit {
		return destination, errors.New("recovery file bound")
	}
	var total int64
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil {
			return destination, e
		}
		total += info.Size()
		if total > recoveryByteLimit {
			return destination, errors.New("recovery byte bound")
		}
		if err = copyFile(filepath.Join(selected, entry.Name()), filepath.Join("store", entry.Name())); err != nil {
			return destination, err
		}
	}
	encoded, err := json.Marshal(sums)
	if err != nil {
		return destination, err
	}
	if err = writeFileAtomicAt(destination, "SHA256.json", append(encoded, '\n')); err != nil {
		return destination, err
	}
	for _, path := range []string{filepath.Join(destination, "store"), destination, parent, root} {
		if err = syncDirectory(path); err != nil {
			return destination, err
		}
	}
	return destination, nil
}

// Validate every registered key, including orphan records that context-only
// scans miss. Existing canonical replay routines verify linked record groups.
func verifyAllAuthority(ctx context.Context, txn *badgerdb.Txn) (uint64, error) {
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	var count uint64
	for iterator.Rewind(); iterator.Valid(); iterator.Next() {
		if err := ctx.Err(); err != nil {
			return count, err
		}
		key, err := DecodeKey(iterator.Item().Key())
		if err != nil {
			return count, err
		}
		value, err := iterator.Item().ValueCopy(nil)
		if err != nil {
			return count, err
		}
		count++
		if count > maximumBackupRecords {
			return count, errors.New("authority verification record bound exceeded")
		}
		id := ""
		if len(key.Identifiers) > 0 {
			id = key.Identifiers[0]
		}
		switch key.Family {
		case KeyMetadataStoreID, KeyMetadataSchema, KeyMetadataCodec:
		case KeyMandate:
			_, err = mandateFromTxn(txn, id)
		case KeyContextBySession:
			var authority core.AuthorityContext
			authority, err = authorityContextFromTxn(txn, string(value))
			if err == nil && authority.SessionID != id {
				err = ErrCorruptRecord
			}
		case KeyAuthorityCommand, KeyAuthorityReceipt:
			var receipt core.AuthorityReceipt
			receipt, err = verifiedAuthorityReceiptFromTxn(txn, id)
			if err == nil {
				var command core.AuthorityCommand
				data, e := getValue(txn, mustCommandKey(id))
				if e != nil {
					err = e
				} else {
					command, err = core.DecodeAuthorityCommandCanonical(data)
					if err == nil {
						var authority core.AuthorityContext
						authority, err = authorityContextFromTxn(txn, command.AuthorityContextID)
						if err == nil && (command.MandateID != authority.MandateID || command.ActorSubjectID != authority.SubjectID || receipt.RecordedAt.Before(authority.IssuedAt) || !receipt.RecordedAt.Before(authority.ExpiresAt) || core.ValidateAuthorityCommandAt(command, receipt.RecordedAt) != nil || receipt.CommandID != id) {
							err = ErrCorruptRecord
						}
					}
				}
			}
		default:
			var authority core.AuthorityContext
			authority, err = authorityContextFromTxn(txn, id)
			if err != nil {
				break
			}
			facts, e := transitionFactsFromTxn(txn, id)
			if e != nil {
				err = e
				break
			}
			for _, fact := range facts {
				if fact.MandateID != authority.MandateID {
					err = ErrCorruptRecord
					break
				}
			}
			if err != nil {
				break
			}
			_, err = verifyTransitionRootFromTxn(txn, id, facts)
			if err != nil {
				break
			}
			commands, canonical, e := canonicalAuthorityFromTxn(txn, id)
			if e != nil {
				err = e
				break
			}
			if len(commands) == 0 && len(canonical) == 0 {
				if key.Family != KeyAuthorityContext && key.Family != KeyTransitionFact && key.Family != KeyTransitionRoot {
					err = ErrCorruptRecord
				}
				break
			}
			var outbox []core.AuthorityOutboxEntry
			outbox, _, err = verifiedAuthorityOutboxFromTxn(txn, id)
			if err == nil {
				_, err = authorityAuditEvidenceFromTxn(txn, id, outbox)
			}
		}
		if err != nil {
			return count, fmt.Errorf("%w: family %x: %v", ErrCorruptRecord, key.Family, err)
		}
	}
	return count, nil
}

func mustCommandKey(id string) []byte {
	key, _ := encodeKey(KeyAuthorityCommand, []string{id}, 0)
	return key
}
