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
	"syscall"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
)

type upgradeReceipt struct {
	Schema             string `json:"schema"`
	Executable         string `json:"executable"`
	ConfigDigest       string `json:"config_digest"`
	Version            string `json:"version"`
	Source             string `json:"source"`
	Digest             string `json:"digest"`
	UnitDigest         string `json:"unit_digest"`
	PreviousUnitDigest string `json:"previous_unit_digest,omitempty"`
	Stage              string `json:"stage"`
	RecoveryEvidence   string `json:"recovery_evidence,omitempty"`
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
func openUpgradeJournal(plan userservice.Plan, state string, digest [sha256.Size]byte, id selfupdate.CandidateIdentity) (*upgradeJournal, error) {
	dir := filepath.Join(state, "upgrade")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return nil, errors.New("unsafe upgrade journal parent")
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || int(s.Uid) != os.Geteuid() {
		return nil, errors.New("unsafe upgrade journal directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	j := &upgradeJournal{root: root, receipt: upgradeReceipt{Schema: "aegis-upgrade-v1", Executable: id.Executable, ConfigDigest: hex.EncodeToString(digest[:]), Version: id.Version, Source: id.Source, Digest: id.Digest, UnitDigest: plan.UnitDigest, Stage: "prepared"}}
	fail := func(e error) (*upgradeJournal, error) { j.close(); return nil, e }
	j.lock, err = root.OpenFile("LOCK", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fail(err)
	}
	info, err = j.lock.Stat()
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
	expected := j.receipt
	if previous.PreviousUnitDigest != "" && previous.PreviousUnitDigest == plan.LegacyUnitDigest {
		expected.PreviousUnitDigest = previous.PreviousUnitDigest
	}
	expected.Stage = previous.Stage
	expected.RecoveryEvidence = previous.RecoveryEvidence
	if previous != expected {
		// A terminal receipt may be retained as history for the next verified
		// intent, only while its exact installed image/config/unit still match.
		// This never skips authentication or any lifecycle phase.
		image, readErr := os.ReadFile(plan.Executable)
		imageDigest := sha256.Sum256(image)
		if previous.Stage != "complete" || readErr != nil || previous.Executable != id.Executable || previous.ConfigDigest != expected.ConfigDigest || previous.UnitDigest != expected.UnitDigest || previous.Digest != hex.EncodeToString(imageDigest[:]) {
			return fail(errors.New("conflicting upgrade intent; exact journal reconciliation required"))
		}
		historyDigest := sha256.Sum256(b)
		if err = root.Rename("journal.json", "completed-"+hex.EncodeToString(historyDigest[:])+".json"); err != nil {
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
		return j, nil
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
