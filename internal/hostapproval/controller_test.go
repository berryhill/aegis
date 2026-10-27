package hostapproval

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestControllerHostApprovalRequiresSignedExactReviewBinding(t *testing.T) {
	now := time.Now().UTC()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	expected := "hello"
	contract := loop.DoerContract{Task: "Write hello", Workspace: "/approved-workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &expected, MaxAttempts: 2}
	reusable := &loop.DoerReusableContract{MaxAttempts: 2}
	candidate, _, err := loop.NewDoerReusableRevision("shared", 1, "", *reusable)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	agent := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "participant", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	base := Record{Purpose: Purpose, DeploymentID: "deployment", PrincipalID: "controller", OwnerID: "participant-owner", ControllerSubjectID: "protected-review-subject", RunInputDigest: "sha256:" + strings.Repeat("b", 64), Agent: agent, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}, Contract: contract, ContractDigest: digest, ApprovedAt: now, ExpiresAt: now.Add(time.Hour), RunRequestID: "exact-run", DoerReusable: reusable}
	signed, err := Sign(base, key)
	if err != nil || Verify(signed, public, base.DeploymentID, base.OwnerID, agent, contract, now) != nil {
		t.Fatal("reviewed exact scope rejected", err)
	}
	tests := map[string]func(*Record){
		"missing-review":           func(r *Record) { r.ControllerSubjectID = "" },
		"different-review-subject": func(r *Record) { r.ControllerSubjectID = "other-review-subject" },
		"different-owner":          func(r *Record) { r.OwnerID = "another-owner" },
		"different-principal":      func(r *Record) { r.PrincipalID = "another-controller" },
		"different-run":            func(r *Record) { r.RunRequestID = "another-run" },
		"different-run-payload":    func(r *Record) { r.RunInputDigest = "sha256:" + strings.Repeat("c", 64) },
		"different-deployment":     func(r *Record) { r.DeploymentID = "other-deployment" },
		"different-agent":          func(r *Record) { r.Agent.Revision++ },
		"different-candidate":      func(r *Record) { r.Candidate.Revision++ },
		"different-contract":       func(r *Record) { r.Contract.Task = "Unreviewed task" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			bad := signed
			mutate(&bad)
			if Verify(bad, public, base.DeploymentID, base.OwnerID, agent, contract, now) == nil {
				t.Fatal("substituted signed scope accepted")
			}
		})
	}
	unreviewed := base
	unreviewed.ControllerSubjectID = ""
	if _, err := Sign(unreviewed, key); err == nil {
		t.Fatal("non-owner scope signed without protected review binding")
	}
	if Verify(signed, public, base.DeploymentID, "controller", agent, contract, now) == nil {
		t.Fatal("principal substituted for recorded owner")
	}
	if Verify(signed, public, base.DeploymentID, base.OwnerID, agent, contract, signed.ExpiresAt) == nil {
		t.Fatal("expired scope accepted")
	}

	// Omitted new fields preserve the legacy owner-principal wire shape and
	// signature verification; this does not retrofit delegation into old facts.
	legacy := base
	legacy.OwnerID = legacy.PrincipalID
	legacy.ControllerSubjectID = ""
	legacy.RunInputDigest = ""
	old, err := Sign(legacy, key)
	if err != nil || Verify(old, public, legacy.DeploymentID, legacy.OwnerID, agent, contract, now) != nil {
		t.Fatal("legacy owner-principal approval rejected", err)
	}
	wire, err := json.Marshal(old)
	if err != nil || bytes.Contains(wire, []byte("controller_subject_id")) || bytes.Contains(wire, []byte("run_input_digest")) {
		t.Fatal("legacy canonical wire gained new field", err)
	}
	roundtrip, err := Decode(wire)
	if err != nil || roundtrip.SignedDigest() != old.SignedDigest() {
		t.Fatal("legacy canonical roundtrip changed", err)
	}
	legacy.ControllerSubjectID = "invented-delegation"
	if _, err := Sign(legacy, key); err == nil {
		t.Fatal("legacy owner-principal scope accepted invented controller delegation")
	}
}
