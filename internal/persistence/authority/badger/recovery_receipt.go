package badger

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type recoveryReceipt struct {
	Principal       string     `json:"principal"`
	Operation       string     `json:"operation"`
	OperationID     string     `json:"operation_id"`
	Generation      Generation `json:"generation"`
	Evidence        string     `json:"evidence"`
	EvidenceDigest  string     `json:"evidence_digest"`
	CandidateDigest string     `json:"candidate_digest"`
	Phase           string     `json:"phase"`
	Outcome         string     `json:"outcome"`
	Records         uint64     `json:"records"`
}

// A clean marker cannot advertise readiness before the durable recovery outcome.
// Only authenticated recovery can complete this observational fence.
func requireRecoveryComplete(root string) error {
	parent := filepath.Join(root, "recovery-evidence")
	if _, err := os.Lstat(parent); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	b, err := readRecoveryReceiptBytes(filepath.Join(parent, "receipt.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var r recoveryReceipt
	if err = json.Unmarshal(b, &r); err != nil {
		return err
	}
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(b, append(canonical, '\n')) || r.Operation != "recover-interrupted" || r.Phase != "completed" || r.Outcome != "recovered" || r.Principal == "" || filepath.Dir(r.Evidence) != parent || r.OperationID != filepath.Base(r.Evidence) {
		return errors.New("authority recovery is incomplete; resume authenticated authority recover-interrupted")
	}
	history, err := readRecoveryReceiptBytes(filepath.Join(r.Evidence, "completed.json"))
	if err != nil || !bytes.Equal(b, history) {
		return errors.New("authority recovery completion is unverifiable")
	}
	return nil
}

// Each phase is immutable; receipt.json is only the latest operation pointer.
func saveRecoveryReceipt(root string, r recoveryReceipt) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	parent := filepath.Join(root, "recovery-evidence")
	if e = requireSecureDirectory(parent); e != nil {
		return e
	}
	if e = requireSecureDirectory(r.Evidence); e != nil {
		return e
	}
	history := filepath.Join(r.Evidence, r.Phase+".json")
	old, e := readRecoveryReceiptBytes(history)
	if e == nil {
		if !bytes.Equal(old, b) {
			return errors.New("immutable recovery receipt conflict")
		}
	} else if errors.Is(e, os.ErrNotExist) {
		// The exclusive maintenance lease serializes publication. Atomic write
		// avoids leaving a partial immutable phase file after process exit.
		if e = writeFileAtomicAt(r.Evidence, r.Phase+".json", b); e != nil {
			return e
		}
	} else {
		return e
	}
	if e = syncDirectory(r.Evidence); e != nil {
		return e
	}
	return writeFileAtomicAt(parent, "receipt.json", b)
}

func readRecoveryReceiptBytes(path string) ([]byte, error) {
	if e := requireSecureDirectory(filepath.Dir(path)); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ownedByCurrentEUID(info) || st.Nlink != 1 || info.Size() > 16384 {
		return nil, errors.New("unsafe recovery receipt")
	}
	b, e := io.ReadAll(io.LimitReader(f, 16385))
	if e != nil {
		return nil, e
	}
	if len(b) > 16384 {
		return nil, errors.New("oversized recovery receipt")
	}
	return b, nil
}

type recoveryFaultKey struct{}

func recoveryBoundary(ctx context.Context, stage string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if hook, ok := ctx.Value(recoveryFaultKey{}).(func(string) error); ok {
		if e := hook(stage); e != nil {
			return e
		}
	}
	return ctx.Err()
}

// Only an exact unordered pair proves which side of the exchange was reached.
func resumeRecovery(ctx context.Context, root, principal string) (InterruptedRecovery, bool, error) {
	var result InterruptedRecovery
	if err := requireSecureDirectory(filepath.Dir(root)); err != nil {
		return result, true, err
	}
	parent := filepath.Join(root, "recovery-evidence")
	if _, e := os.Lstat(parent); errors.Is(e, os.ErrNotExist) {
		return result, false, nil
	}
	b, e := readRecoveryReceiptBytes(filepath.Join(parent, "receipt.json"))
	if errors.Is(e, os.ErrNotExist) {
		return result, false, nil
	}
	if e != nil {
		return result, true, e
	}
	fail := func() (InterruptedRecovery, bool, error) {
		return result, true, errors.New("recovery receipt does not prove publication")
	}
	var r recoveryReceipt
	if json.Unmarshal(b, &r) != nil {
		return fail()
	}
	canonical, _ := json.Marshal(r)
	if !bytes.Equal(b, append(canonical, '\n')) {
		return fail()
	}
	digestOK := func(s string) bool {
		v, e := hex.DecodeString(s)
		return e == nil && len(v) == 32 && s == strings.ToLower(s)
	}
	if principal == "" || r.Principal != principal || r.Operation != "recover-interrupted" || validateGeneration(r.Generation) != nil || r.OperationID != filepath.Base(r.Evidence) || !strings.HasPrefix(r.OperationID, "interrupted-") || filepath.Dir(r.Evidence) != parent || !digestOK(r.EvidenceDigest) || !digestOK(r.CandidateDigest) || r.Records > maximumBackupRecords {
		return fail()
	}
	if !((r.Phase == "verified" && r.Outcome == "pending") || (r.Phase == "completed" && r.Outcome == "recovered")) {
		return fail()
	}
	history, e := readRecoveryReceiptBytes(filepath.Join(r.Evidence, r.Phase+".json"))
	if e != nil || !bytes.Equal(history, b) {
		return fail()
	}
	result = InterruptedRecovery{Generation: r.Generation, EvidencePath: r.Evidence, Records: r.Records}
	for _, p := range []string{root, filepath.Join(root, "stores"), filepath.Join(root, "staging"), filepath.Join(root, "retired"), r.Evidence, filepath.Join(r.Evidence, "store"), filepath.Join(r.Evidence, "work")} {
		if requireSecureDirectory(p) != nil {
			return fail()
		}
	}
	selected := filepath.Join(root, "stores", r.Generation.Directory)
	// Completed history is not a perpetual claim about a subsequently opened store.
	if r.Phase == "completed" {
		g, e := inspectClosedGeneration(root)
		if e != nil || g != r.Generation {
			return InterruptedRecovery{}, false, nil
		}
		got, e := recoveryDirectory(ctx, selected, "")
		if e != nil {
			return result, true, e
		}
		if got != r.CandidateDigest {
			return InterruptedRecovery{}, false, nil
		}
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return result, true, e
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "ACTIVE", "CLEAN", "DIRTY", "MAINTENANCE", "stores", "staging", "retired", "recovery-evidence":
		default:
			return fail()
		}
	}
	for _, name := range []string{"stores", "staging", "retired"} {
		entries, e := os.ReadDir(filepath.Join(root, name))
		if e != nil {
			return result, true, e
		}
		if name == "stores" {
			if len(entries) != 1 || entries[0].Name() != r.Generation.Directory {
				return fail()
			}
		} else if len(entries) != 0 {
			return fail()
		}
	}
	active, e := readMarker(filepath.Join(root, "ACTIVE"))
	if e != nil || active != r.Generation {
		return fail()
	}
	dirty, clean := false, false
	for _, name := range []string{"CLEAN", "DIRTY"} {
		g, e := readMarker(filepath.Join(root, name))
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return fail()
		}
		if e == nil {
			if g != r.Generation {
				return fail()
			}
			if name == "CLEAN" {
				clean = true
			} else {
				dirty = true
			}
		}
	}
	if !dirty && !clean {
		return fail()
	}
	evidence, e := recoveryDirectory(ctx, filepath.Join(r.Evidence, "store"), "")
	if e != nil {
		return result, true, e
	}
	if evidence != r.EvidenceDigest {
		return fail()
	}
	work := filepath.Join(r.Evidence, "work")
	a, e := recoveryDirectory(ctx, selected, "")
	if e != nil {
		return result, true, e
	}
	w, e := recoveryDirectory(ctx, work, "")
	if e != nil {
		return result, true, e
	}
	post := a == r.CandidateDigest && w == r.EvidenceDigest
	pre := a == r.EvidenceDigest && w == r.CandidateDigest
	if !post && !pre {
		return fail()
	}
	if r.Phase == "completed" {
		if !post {
			return fail()
		}
		if e = ctx.Err(); e != nil {
			return result, true, e
		}
		result.Recovered = true
		return result, true, nil
	}
	if !post {
		if clean || !dirty {
			return fail()
		}
		if e = recoveryBoundary(ctx, "before-exchange"); e != nil {
			return result, true, e
		}
		if e = exchangeRecoveryDirectories(selected, work); e != nil {
			return result, true, e
		}
	}
	if e = recoveryBoundary(ctx, "exchange"); e != nil {
		return result, true, e
	}
	for _, p := range []string{filepath.Dir(selected), r.Evidence} {
		if e = syncDirectory(p); e != nil {
			return result, true, e
		}
	}
	if e = recoveryBoundary(ctx, "fsync"); e != nil {
		return result, true, e
	}
	if e = verifyStoreIdentity(selected, r.Generation); e != nil {
		return result, true, e
	}
	if e = recoveryBoundary(ctx, "before-clean"); e != nil {
		return result, true, e
	}
	if e = writeMarker(root, "CLEAN", r.Generation); e != nil {
		return result, true, e
	}
	if e = recoveryBoundary(ctx, "clean"); e != nil {
		return result, true, e
	}
	if e = removeFileAt(root, "DIRTY"); e != nil && !errors.Is(e, os.ErrNotExist) {
		return result, true, e
	}
	if e = syncDirectory(root); e != nil {
		return result, true, e
	}
	if e = recoveryBoundary(ctx, "dirty-removed"); e != nil {
		return result, true, e
	}
	if _, e = inspectClosedGeneration(root); e != nil {
		return result, true, e
	}
	r.Phase = "completed"
	r.Outcome = "recovered"
	if e = recoveryBoundary(ctx, "before-completion"); e != nil {
		return result, true, e
	}
	if e = saveRecoveryReceipt(root, r); e != nil {
		return result, true, e
	}
	if e = recoveryBoundary(ctx, "completed"); e != nil {
		return result, true, e
	}
	result.Recovered = true
	return result, true, nil
}
