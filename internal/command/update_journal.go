package command

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
)

type upgradeReceipt struct {
	Schema                string `json:"schema"`
	Executable            string `json:"executable"`
	ConfigDigest          string `json:"config_digest"`
	Version               string `json:"version"`
	Source                string `json:"source"`
	Digest                string `json:"digest"`
	UnitDigest            string `json:"unit_digest"`
	PreviousUnitDigest    string `json:"previous_unit_digest,omitempty"`
	Stage                 string `json:"stage"`
	RecoveryEvidence      string `json:"recovery_evidence,omitempty"`
	FleetRecoveryEvidence string `json:"fleet_recovery_evidence,omitempty"`
	SupersededDigest      string `json:"superseded_digest,omitempty"`
}
type upgradeJournal struct {
	root    *os.Root
	lock    *os.File
	receipt upgradeReceipt
	prior   []byte
}

func privateUpgradeFile(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode().Perm() == 0600 && int(s.Uid) == os.Geteuid() && s.Nlink == 1
}
func upgradeDirectory(dir string) (*os.Root, error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, errors.New("unsafe upgrade journal parent")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || int(s.Uid) != os.Geteuid() || s.Nlink < 2 {
		return nil, errors.New("unsafe upgrade journal directory")
	}
	return os.OpenRoot(dir)
}

func validUpgradeReceipt(r upgradeReceipt, plan userservice.Plan, digest [sha256.Size]byte) bool {
	if r.Schema != "aegis-upgrade-v1" || r.Executable != plan.Executable || !filepath.IsAbs(r.Executable) || r.ConfigDigest != hex.EncodeToString(digest[:]) || (r.UnitDigest != plan.UnitDigest && !(r.Stage == "complete" && r.UnitDigest == plan.LegacyUnitDigest)) || len(r.Source) != 40 || len(r.Digest) != 64 {
		return false
	}
	if _, ok := selfupdate.CompareVersions(r.Version, r.Version); !ok {
		return false
	}
	if b, err := hex.DecodeString(r.Source); err != nil || hex.EncodeToString(b) != r.Source {
		return false
	}
	if b, err := hex.DecodeString(r.Digest); err != nil || hex.EncodeToString(b) != r.Digest {
		return false
	}
	if r.PreviousUnitDigest != "" && r.PreviousUnitDigest != plan.LegacyUnitDigest {
		return false
	}
	for _, evidence := range []string{r.RecoveryEvidence, r.FleetRecoveryEvidence} {
		if evidence != "" && (!filepath.IsAbs(evidence) || strings.Contains(evidence, "..")) {
			return false
		}
	}
	if r.SupersededDigest != "" {
		if b, err := hex.DecodeString(r.SupersededDigest); err != nil || len(b) != 32 || hex.EncodeToString(b) != r.SupersededDigest {
			return false
		}
	}
	switch r.Stage {
	case "unit_migration", "prepared", "gateway_stop", "binary_install", "authority_validation", "gateway_activation", "verification", "complete":
		return true
	default:
		return false
	}
}

// inspectUpgradeJournal never creates a path or takes the upgrade mutation lock.
func inspectUpgradeJournal(plan userservice.Plan, state string, digest [sha256.Size]byte) (upgradeReceipt, bool, error) {
	dir := filepath.Join(state, "upgrade")
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return upgradeReceipt{}, false, nil
	} else if err != nil {
		return upgradeReceipt{}, false, err
	}
	root, err := upgradeDirectory(dir)
	if err != nil {
		return upgradeReceipt{}, false, err
	}
	defer root.Close()
	j := &upgradeJournal{root: root}
	b, err := j.read()
	if errors.Is(err, os.ErrNotExist) {
		return upgradeReceipt{}, false, nil
	}
	if err != nil {
		return upgradeReceipt{}, false, err
	}
	var r upgradeReceipt
	if err := json.Unmarshal(b, &r); err != nil {
		return upgradeReceipt{}, false, errors.New("malformed upgrade journal")
	}
	canonical, _ := json.Marshal(r)
	if !bytes.Equal(canonical, b) || !validUpgradeReceipt(r, plan, digest) {
		return upgradeReceipt{}, false, errors.New("unsafe or conflicting upgrade journal")
	}
	if err := verifySuperseded(root, r, plan, digest); err != nil {
		return upgradeReceipt{}, false, err
	}
	return r, true, nil
}

func verifySuperseded(root *os.Root, r upgradeReceipt, plan userservice.Plan, digest [sha256.Size]byte) error {
	return verifyUpgradeHistory(root, r, plan, digest, 0)
}
func verifyUpgradeHistory(root *os.Root, r upgradeReceipt, plan userservice.Plan, digest [sha256.Size]byte, depth int) error {
	if depth > 64 {
		return errors.New("upgrade history exceeds bound")
	}
	if r.SupersededDigest == "" {
		return nil
	}
	f, err := root.OpenFile("superseded-"+r.SupersededDigest+".json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return errors.New("missing upgrade history")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !privateUpgradeFile(info) || info.Size() > 64<<10 {
		return errors.New("unsafe upgrade history")
	}

	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != r.SupersededDigest {
		return errors.New("substituted upgrade history")
	}
	var old upgradeReceipt
	if err := json.Unmarshal(b, &old); err != nil {
		return errors.New("malformed upgrade history")
	}
	canonical, _ := json.Marshal(old)
	if !bytes.Equal(canonical, b) || !validUpgradeReceipt(old, plan, digest) {
		return errors.New("invalid upgrade history")
	}
	order, ok := selfupdate.CompareVersions(old.Version, r.Version)
	if !ok || order >= 0 {
		return errors.New("upgrade history version conflict")
	}
	return verifyUpgradeHistory(root, old, plan, digest, depth+1)
}

func openUpgradeJournal(plan userservice.Plan, state string, digest [sha256.Size]byte, id selfupdate.CandidateIdentity) (*upgradeJournal, error) {
	return openUpgradeJournalWithReconcile(plan, state, digest, id, nil)
}
func reconcileUpgradeJournal(plan userservice.Plan, state string, digest [sha256.Size]byte, id selfupdate.CandidateIdentity, authenticate func() error) error {
	j, err := openUpgradeJournalWithReconcile(plan, state, digest, id, authenticate)
	if err != nil {
		return err
	}
	j.close()
	return nil
}
func openUpgradeJournalWithReconcile(plan userservice.Plan, state string, digest [sha256.Size]byte, id selfupdate.CandidateIdentity, authenticate func() error) (*upgradeJournal, error) {
	dir := filepath.Join(state, "upgrade")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	root, err := upgradeDirectory(dir)
	if err != nil {
		return nil, err
	}
	j := &upgradeJournal{root: root, receipt: upgradeReceipt{Schema: "aegis-upgrade-v1", Executable: id.Executable, ConfigDigest: hex.EncodeToString(digest[:]), Version: id.Version, Source: id.Source, Digest: id.Digest, UnitDigest: plan.UnitDigest, Stage: "prepared"}}
	fail := func(e error) (*upgradeJournal, error) { j.close(); return nil, e }
	j.lock, err = root.OpenFile("LOCK", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail(err)
	}
	info, err := j.lock.Stat()
	if err != nil || !privateUpgradeFile(info) {
		return fail(errors.New("unsafe upgrade operation lock"))
	}
	if err = syscall.Flock(int(j.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(errors.New("another upgrade owns this installation"))
	}
	b, err := j.read()
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return fail(err)
	}
	var previous upgradeReceipt
	if err = json.Unmarshal(b, &previous); err != nil {
		return fail(err)
	}
	canonical, _ := json.Marshal(previous)
	if !bytes.Equal(canonical, b) {
		return fail(errors.New("noncanonical upgrade journal"))
	}
	if !validUpgradeReceipt(previous, plan, digest) {
		return fail(errors.New("unsafe or conflicting upgrade journal"))
	}
	if err := verifySuperseded(root, previous, plan, digest); err != nil {
		return fail(err)
	}
	expected := j.receipt
	if previous.PreviousUnitDigest != "" && previous.PreviousUnitDigest == plan.LegacyUnitDigest {
		expected.PreviousUnitDigest = previous.PreviousUnitDigest
	}
	expected.Stage = previous.Stage
	expected.RecoveryEvidence = previous.RecoveryEvidence
	expected.FleetRecoveryEvidence = previous.FleetRecoveryEvidence
	expected.SupersededDigest = previous.SupersededDigest
	if previous != expected {
		{
			order, comparable := selfupdate.CompareVersions(previous.Version, id.Version)
			if !comparable || order >= 0 || previous.Executable != id.Executable || previous.ConfigDigest != expected.ConfigDigest || (previous.UnitDigest != expected.UnitDigest && !(previous.Stage == "complete" && previous.UnitDigest == plan.LegacyUnitDigest)) || authenticate == nil {
				return fail(errors.New("conflicting upgrade intent; exact journal reconciliation required"))
			}
			if previous.Stage == "complete" {
				image, readErr := os.ReadFile(plan.Executable)
				imageDigest := sha256.Sum256(image)
				if readErr != nil || previous.Digest != hex.EncodeToString(imageDigest[:]) {
					return fail(errors.New("completed upgrade image changed"))
				}
			}
			if err := authenticate(); err != nil {
				return fail(err)
			}
			// The old receipt never supplies authority or phase progress to the new
			// candidate. Keep its exact bytes, durably, before retiring its name.
			current, readErr := j.read()
			if readErr != nil || !bytes.Equal(current, b) {
				return fail(errors.New("upgrade journal changed during authentication"))
			}
			historyDigest := sha256.Sum256(b)
			history := hex.EncodeToString(historyDigest[:])
			archive := "superseded-" + history + ".json"
			if err := publishUpgradeHistory(root, archive, b); err != nil {
				return fail(err)
			}
			directory, openErr := root.Open(".")
			if openErr != nil {
				return fail(openErr)
			}
			syncErr := directory.Sync()
			directory.Close()
			if syncErr != nil {
				return fail(syncErr)
			}
			j.receipt.SupersededDigest = history
			j.prior = b
			if err := j.save(); err != nil {
				return fail(err)
			}
			return j, nil
		}
	}
	switch previous.Stage {
	case "unit_migration", "prepared", "gateway_stop", "binary_install", "authority_validation", "gateway_activation", "verification", "complete":
	default:
		return fail(errors.New("unknown upgrade journal stage"))
	}
	j.receipt = previous
	j.prior = b
	return j, nil
}

// The exclusive upgrade lock serializes supported publishers. Never expose a
// partially written archive at its final content-addressed name. Retained
// staging files from a crash are not authoritative and do not block a retry.
func publishUpgradeHistory(root *os.Root, archive string, content []byte) error {
	f, err := root.OpenFile(archive, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err == nil {
		defer f.Close()
		info, statErr := f.Stat()
		stored, readErr := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		if statErr != nil || !privateUpgradeFile(info) || readErr != nil || !bytes.Equal(stored, content) {
			return errors.New("upgrade history collision")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".history-" + hex.EncodeToString(nonce[:])
	f, err = root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(name)
	if _, err = f.Write(content); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	// All supported writers hold LOCK; hostile same-UID races remain outside
	// this filesystem trust boundary, as with journal publication itself.
	return root.Rename(name, archive)
}

func (j *upgradeJournal) read() ([]byte, error) {
	f, err := j.root.OpenFile("journal.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !privateUpgradeFile(info) || info.Size() > 64<<10 {
		return nil, errors.New("unsafe upgrade journal")
	}
	return io.ReadAll(io.LimitReader(f, 64<<10))
}
func (j *upgradeJournal) save() error {
	current, err := j.read()
	if len(j.prior) == 0 {
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("upgrade journal substituted")
		}
	} else if err != nil || !bytes.Equal(current, j.prior) {
		return errors.New("upgrade journal changed")
	}
	b, err := json.Marshal(j.receipt)
	if err != nil {
		return err
	}
	// Unique exclusive temporary file in the pinned private directory.
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".journal-" + hex.EncodeToString(nonce[:])
	f, err := j.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer j.root.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = j.root.Rename(name, "journal.json"); err != nil {
		return err
	}
	d, err := j.root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	if err = d.Sync(); err != nil {
		return err
	}
	j.prior = b
	return nil
}
func (j *upgradeJournal) close() {
	if j.lock != nil {
		j.lock.Close()
	}
	if j.root != nil {
		j.root.Close()
	}
}
