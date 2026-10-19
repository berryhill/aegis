package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
)

func fleetEvidenceFixture(t *testing.T, stage, evidence string) (string, userservice.Plan, selfupdate.CandidateIdentity, [32]byte, []byte) {
	t.Helper()
	state := t.TempDir()
	image := []byte("retained installed image")
	exe := filepath.Join(state, "aegis")
	if err := os.WriteFile(exe, image, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	plan := userservice.Plan{Executable: exe, UnitDigest: "unit"}
	id := selfupdate.CandidateIdentity{Executable: exe, Version: "0.2.36", Source: strings.Repeat("a", 40), Digest: hex.EncodeToString(sum[:])}
	config := sha256.Sum256([]byte("same configured deployment"))
	j, err := openUpgradeJournal(plan, state, config, id)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.Stage = stage
	if err = j.save(); err != nil {
		j.close()
		t.Fatal(err)
	}
	j.close()
	wire := bytes.Clone(j.prior)
	if evidence != "" {
		value, err := json.Marshal(evidence)
		if err != nil {
			t.Fatal(err)
		}
		wire = append(append(wire[:len(wire)-1], []byte(`,"fleet_recovery_evidence":`)...), append(value, '}')...)
		if err = os.WriteFile(filepath.Join(state, "upgrade", "journal.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return state, plan, id, config, wire
}

func TestUpgradeJournalRetainsFleetRecoveryEvidence(t *testing.T) {
	for _, stage := range []string{"authority_validation", "gateway_activation", "complete"} {
		for _, evidence := range []string{"", "/private/fleet-v1/recovery-evidence/interrupted-example"} {
			name := "old-receipt"
			if evidence != "" {
				name = "fleet-evidence"
			}
			t.Run(stage+"/"+name, func(t *testing.T) {
				state, plan, id, config, wire := fleetEvidenceFixture(t, stage, evidence)
				if _, found, err := inspectUpgradeJournal(plan, state, config); err != nil || !found {
					t.Fatalf("read-only inspection: found=%v err=%v", found, err)
				}
				j, err := openUpgradeJournal(plan, state, config, id)
				if err != nil {
					t.Fatalf("same-intent resume refused: %v", err)
				}
				if !bytes.Equal(j.prior, wire) {
					j.close()
					t.Fatal("resume changed retained evidence bytes")
				}
				if err = j.save(); err != nil {
					j.close()
					t.Fatal(err)
				}
				got, err := j.read()
				j.close()
				if err != nil || !bytes.Equal(got, wire) {
					t.Fatalf("save lost known recovery metadata: %v", err)
				}
			})
		}
	}
}

func TestUpgradeJournalFleetEvidenceDenials(t *testing.T) {
	for _, evidence := range []string{"relative/evidence", "/private/../other"} {
		t.Run(evidence, func(t *testing.T) {
			state, plan, id, config, wire := fleetEvidenceFixture(t, "gateway_activation", evidence)
			if _, _, err := inspectUpgradeJournal(plan, state, config); err == nil {
				t.Fatal("unsafe evidence path admitted")
			}
			if j, err := openUpgradeJournal(plan, state, config, id); err == nil {
				j.close()
				t.Fatal("unsafe evidence path resumed")
			}
			got, err := os.ReadFile(filepath.Join(state, "upgrade", "journal.json"))
			if err != nil || !bytes.Equal(got, wire) {
				t.Fatal("denial changed journal")
			}
		})
	}
	for _, mutation := range []string{"unknown-field", "foreign-config", "foreign-executable"} {
		t.Run(mutation, func(t *testing.T) {
			state, plan, id, config, wire := fleetEvidenceFixture(t, "gateway_activation", "/private/fleet-v1/recovery-evidence/interrupted-example")
			switch mutation {
			case "unknown-field":
				wire = append(wire[:len(wire)-1], []byte(`,"unreviewed_authority":true}`)...)
				if err := os.WriteFile(filepath.Join(state, "upgrade", "journal.json"), wire, 0600); err != nil {
					t.Fatal(err)
				}
			case "foreign-config":
				config = sha256.Sum256([]byte("foreign"))
			case "foreign-executable":
				plan.Executable = filepath.Join(state, "other")
				id.Executable = plan.Executable
			}
			if _, _, err := inspectUpgradeJournal(plan, state, config); err == nil {
				t.Fatal("substituted receipt admitted")
			}
			if j, err := openUpgradeJournal(plan, state, config, id); err == nil {
				j.close()
				t.Fatal("substituted receipt resumed")
			}
		})
	}
}

func TestUpgradeJournalFleetEvidenceAuthenticatedReconciliation(t *testing.T) {
	state, plan, id, config, wire := fleetEvidenceFixture(t, "complete", "/private/fleet-v1/recovery-evidence/interrupted-example")
	id.Version = "0.2.37"
	id.Digest = strings.Repeat("b", 64)
	authCalls := 0
	j, err := openUpgradeJournalWithReconcile(plan, state, config, id, func() error { authCalls++; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	if authCalls != 1 {
		t.Fatalf("authentication calls=%d", authCalls)
	}
	digest := sha256.Sum256(wire)
	if j.receipt.SupersededDigest != hex.EncodeToString(digest[:]) {
		t.Fatal("old receipt not bound in history")
	}
	archived, err := os.ReadFile(filepath.Join(state, "upgrade", "superseded-"+j.receipt.SupersededDigest+".json"))
	if err != nil || !bytes.Equal(archived, wire) {
		t.Fatalf("lost exact historical fleet evidence: %v", err)
	}
}
