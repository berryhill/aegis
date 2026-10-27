package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestHistoricalDoerHostApprovalDoesNotRenewLiveScope(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := hostRecord(t)
	r.PrincipalID = "controller"
	r.OwnerID = "participant-owner"
	r.ControllerSubjectID = "protected-review"
	r.RunInputDigest = "sha256:" + strings.Repeat("b", 64)
	r.RunRequestID = "completed-run"
	r.DoerReusable = &loop.DoerReusableContract{MaxAttempts: r.Contract.MaxAttempts}
	candidate, _, err := loop.NewDoerReusableRevision("shared", 1, "", *r.DoerReusable)
	if err != nil {
		t.Fatal(err)
	}
	r.Candidate = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}
	if _, err := s.ReadHistoricalDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.RunRequestID, r.ApprovedAt); err == nil {
		t.Fatal("missing history accepted")
	}
	if _, err := os.Stat(filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key")); !os.IsNotExist(err) {
		t.Fatal("evidence read initialized signer")
	}
	first, err := s.SaveDoerHostApproval(r)
	if err != nil {
		t.Fatal(err)
	}
	completedAt := first.ApprovedAt.Add(time.Second)
	afterExpiry := first.ExpiresAt.Add(time.Second)
	if _, err := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, afterExpiry); err == nil {
		t.Fatal("expired live scope accepted")
	}
	// A later independently created generation must not replace the old fact.
	later := r
	later.ApprovedAt = afterExpiry
	later.ExpiresAt = afterExpiry.Add(time.Hour)
	later.RunRequestID = "later-run"
	later.RunInputDigest = "sha256:" + strings.Repeat("c", 64)
	next, err := s.SaveDoerHostApproval(later)
	if err != nil || next.Generation != 1 {
		t.Fatal("separate fixture consent generation failed", err)
	}
	got, err := s.ReadHistoricalDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.RunRequestID, completedAt)
	if err != nil || got.SignedDigest() != first.SignedDigest() {
		t.Fatal("completed exact signed fact lost after renewal", err)
	}
	for name, read := range map[string]func() error{
		"wrong-run": func() error {
			_, e := s.ReadHistoricalDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, "other-run", completedAt)
			return e
		},
		"wrong-owner": func() error {
			_, e := s.ReadHistoricalDoerHostApproval(r.DeploymentID, "other-owner", r.Agent, r.Contract, r.RunRequestID, completedAt)
			return e
		},
		"wrong-deployment": func() error {
			_, e := s.ReadHistoricalDoerHostApproval("other", r.OwnerID, r.Agent, r.Contract, r.RunRequestID, completedAt)
			return e
		},
		"outside-interval": func() error {
			_, e := s.ReadHistoricalDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.RunRequestID, afterExpiry)
			return e
		},
	} {
		t.Run(name, func(t *testing.T) {
			if read() == nil {
				t.Fatal("substituted historical scope accepted")
			}
		})
	}
	live, err := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, afterExpiry)
	if err != nil || live.SignedDigest() != next.SignedDigest() || live.RunRequestID == first.RunRequestID {
		t.Fatal("historical read rewrote/renewed live scope", err)
	}
	slot := hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)
	name := filepath.Join(s.Root(), "doer-host-approvals", hostApprovalGenerationName(slot, next.Generation))
	if err := os.WriteFile(name, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadHistoricalDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.RunRequestID, completedAt); err == nil {
		t.Fatal("historical read ignored later corrupt history")
	}
}
