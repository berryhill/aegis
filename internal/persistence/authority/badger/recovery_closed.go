package badger

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	badgerdb "github.com/dgraph-io/badger/v4"
)

// Called only under the exclusive maintenance lease, after resumeRecovery has
// either handled a pending/exact completed operation or validated completed
// history belonging to this principal. CLEAN alone never proves integrity.
func verifyClosedForRecovery(ctx context.Context, root string) (out InterruptedRecovery, err error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "ACTIVE", "CLEAN", "MAINTENANCE", "stores", "staging", "retired":
		case "recovery-evidence":
			// An incomplete snapshot without a receipt is not a clean no-op.
			if err = requireRecoveryComplete(root); err != nil {
				return out, err
			}
			b, e := readRecoveryReceiptBytes(filepath.Join(root, "recovery-evidence", "receipt.json"))
			if e != nil {
				return out, e
			}
			var receipt recoveryReceipt
			if err = json.Unmarshal(b, &receipt); err != nil {
				return out, err
			}
			// A later ordinary close can change the selected engine bytes, but
			// never the two retained copies of the original interrupted store.
			for _, name := range []string{"store", "work"} {
				digest, e := recoveryDirectory(ctx, filepath.Join(receipt.Evidence, name), "")
				if e != nil {
					return out, e
				}
				if digest != receipt.EvidenceDigest {
					return out, errors.New("completed authority recovery evidence changed")
				}
			}
			out.EvidencePath = receipt.Evidence
		default:
			return out, ErrMaintenanceUnsafe
		}
	}
	active, err := readMarker(filepath.Join(root, "ACTIVE"))
	if err != nil {
		return out, err
	}
	for _, name := range []string{"stores", "staging", "retired"} {
		path := filepath.Join(root, name)
		if err = requireSecureDirectory(path); err != nil {
			return out, err
		}
		entries, err = os.ReadDir(path)
		if err != nil {
			return out, err
		}
		if name == "stores" {
			if len(entries) != 1 || entries[0].Name() != active.Directory {
				return out, ErrMaintenanceUnsafe
			}
		} else if len(entries) != 0 {
			return out, ErrMaintenanceUnsafe
		}
	}
	selected := filepath.Join(root, "stores", active.Directory)
	// Reject unsafe engine files before even the identity verifier opens them.
	if _, err = recoveryDirectory(ctx, selected, ""); err != nil {
		return out, err
	}
	out.Generation, err = inspectClosedGeneration(root)
	if err != nil {
		return out, err
	}
	db, err := badgerdb.Open(options(selected).WithReadOnly(true).WithVerifyValueChecksum(true))
	if err != nil {
		return out, err
	}
	err = db.VerifyChecksum()
	if err == nil {
		err = db.View(func(txn *badgerdb.Txn) error {
			var e error
			out.Records, e = verifyAllAuthority(ctx, txn)
			return e
		})
	}
	err = errors.Join(err, db.Close(), ctx.Err())
	out.VerifiedClean = err == nil
	return out, err
}
