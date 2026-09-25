package manager

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func checkpointFixture(t *testing.T) (string, CertificationCheckpoint, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	c := Certification{SchemaVersion: "aegis.manager.certification.v1", CandidateID: Candidates()[0].ID, ArtifactName: Candidates()[0].OllamaName, ArtifactDigest: "sha256:" + strings.Repeat("a", 64), ContextLength: 64000, Quantization: "Q4_K_M", HermesVersion: "0.18.0", OllamaVersion: "0.12.0", InstructionDigest: digestString(ManagerSystemInstruction()), ResponseSchema: ResponseSchemaVersion, CorpusDigest: CorpusDigest()}
	return filepath.Join(t.TempDir(), "checkpoints", "candidate.json"), CertificationCheckpoint{SchemaVersion: "aegis.manager.certification-checkpoint.v1", PrincipalID: "operator-1", Certification: c, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, now
}

func TestCertificationCheckpointRoundTripAndCAS(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	if _, present, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err != nil || present {
		t.Fatalf("absent: %v %v", present, err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	loaded, present, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now)
	if err != nil || !present || len(loaded.Certification.Results) != 0 {
		t.Fatalf("load: %#v %v %v", loaded, present, err)
	}
	cp.Certification.Results = []ConformanceResult{{CaseID: ConformanceCorpus()[0].ID, Passed: true, Reason: "passed"}}
	if err := SaveCertificationCheckpoint(path, cp, 0); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, 0); err == nil {
		t.Fatal("stale CAS accepted")
	}
	loaded, present, err = LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now)
	if err != nil || !present || len(loaded.Certification.Results) != 1 || !loaded.Certification.CertifiedAt.IsZero() {
		t.Fatalf("partial finalized: %#v %v %v", loaded, present, err)
	}
}

func TestCertificationCheckpointRejectsIdentityAndExpiry(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	other := cp.Certification
	other.Quantization = "Q8"
	for _, tc := range []struct {
		name      string
		expected  Certification
		principal string
		at        time.Time
	}{
		{"tuple", other, cp.PrincipalID, now}, {"principal", cp.Certification, "another", now}, {"expired", cp.Certification, cp.PrincipalID, cp.ExpiresAt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, present, err := LoadCertificationCheckpoint(path, tc.expected, tc.principal, tc.at)
			if err == nil || !present {
				t.Fatalf("present=%v err=%v", present, err)
			}
		})
	}
	cp.PrincipalID = ""
	if err := SaveCertificationCheckpoint(path, cp, 0); err == nil {
		t.Fatal("empty principal")
	}
}

func TestRetireExpiredCheckpointRequiresExactIdentityAndPreservesEvidence(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	if err := RetireExpiredCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("live checkpoint retired")
	}
	wrong := cp.Certification
	wrong.Quantization = "wrong"
	if err := RetireExpiredCertificationCheckpoint(path, wrong, cp.PrincipalID, cp.ExpiresAt); err == nil {
		t.Fatal("wrong identity retired")
	}
	if err := RetireExpiredCertificationCheckpoint(path, cp.Certification, "wrong-principal", cp.ExpiresAt); err == nil {
		t.Fatal("wrong principal retired")
	}
	if err := RetireExpiredCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, cp.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expired checkpoint still active: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), filepath.Base(path)+".expired.") {
			found = true
		}
	}
	if !found {
		t.Fatal("retired evidence missing")
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatalf("new campaign blocked: %v", err)
	}
}

func TestCertificationCheckpointRejectsInvalidPrefix(t *testing.T) {
	cases := ConformanceCorpus()
	for _, tc := range []struct {
		name    string
		results []ConformanceResult
	}{
		{"unknown", []ConformanceResult{{CaseID: "unknown", Passed: true, Reason: "passed"}}},
		{"duplicate", []ConformanceResult{{CaseID: cases[0].ID, Passed: true, Reason: "passed"}, {CaseID: cases[0].ID, Passed: true, Reason: "passed"}}},
		{"reordered", []ConformanceResult{{CaseID: cases[1].ID, Passed: true, Reason: "passed"}, {CaseID: cases[0].ID, Passed: true, Reason: "passed"}}},
		{"failed", []ConformanceResult{{CaseID: cases[0].ID, Passed: false, Reason: "failed"}}},
		{"wrong reason", []ConformanceResult{{CaseID: cases[0].ID, Passed: true, Reason: "unexpected"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, cp, _ := checkpointFixture(t)
			cp.Certification.Results = tc.results
			if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
				t.Fatal("invalid prefix saved")
			}
		})
	}
}

func TestCertificationCheckpointRejectsMalformedAndUnsafeFiles(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, present, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil || !present {
		t.Fatalf("corrupt %v %v", present, err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
		t.Fatal("corrupt predecessor replaced")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	data = append(data, byte(' '))
	data[len(data)-2] = 'X'
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("tampering accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("leave alone"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
		t.Fatal("symlink replaced")
	}
	if _, _, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("symlink followed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
		t.Fatal("unsafe directory accepted")
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("unsafe file accepted")
	}
}

func TestCertificationCheckpointRejectsLinkedPathAndLock(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	alias := path + ".alias"
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("hardlink accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".lock"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, path+".lock"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertificationCheckpoint(path, cp, 0); err == nil {
		t.Fatal("symlink lock accepted")
	}
}

func TestCertificationCheckpointRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	_, cp, now := checkpointFixture(t)
	path := filepath.Join(root, "alias", "checkpoint.json")
	if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
		t.Fatal("symlink directory accepted")
	}
	if _, _, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err == nil {
		t.Fatal("symlink directory loaded")
	}
}

func TestCertificationCheckpointRejectsPrematureFinalization(t *testing.T) {
	path, cp, now := checkpointFixture(t)
	cp.Certification.CertifiedAt = now
	if err := SaveCertificationCheckpoint(path, cp, -1); err == nil {
		t.Fatal("partial certification marked final")
	}
	if _, present, err := LoadCertificationCheckpoint(path, cp.Certification, cp.PrincipalID, now); err != nil || present {
		t.Fatalf("partial final wrote file: present=%v err=%v", present, err)
	}
}

func TestCertificationCheckpointConcurrentCAS(t *testing.T) {
	path, cp, _ := checkpointFixture(t)
	if err := SaveCertificationCheckpoint(path, cp, -1); err != nil {
		t.Fatal(err)
	}
	cp.Certification.Results = []ConformanceResult{{CaseID: ConformanceCorpus()[0].ID, Passed: true, Reason: "passed"}}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- SaveCertificationCheckpoint(path, cp, 0) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful writes=%d", success)
	}
}
