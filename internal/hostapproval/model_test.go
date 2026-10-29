package hostapproval

import (
	"crypto/ed25519"
	"crypto/rand"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
	"time"
)

func TestSignedHostApprovalExactBindings(t *testing.T) {
	now := time.Now().UTC()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	c := loop.DoerContract{Task: "Write result", Workspace: "/project", WritableFiles: []string{"out"}, VerifyFile: "out", MaxAttempts: 1}
	candidate, _, e := loop.NewDoerRevision("test", 1, "", c)
	if e != nil {
		t.Fatal(e)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	d, _ := c.Digest()
	r, e := Sign(Record{Purpose: Purpose, DeploymentID: "deployment", PrincipalID: "owner", OwnerID: "owner", Agent: ref, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "test", Revision: 1, Digest: candidate.Digest}, Contract: c, ContractDigest: d, ApprovedAt: now, ExpiresAt: now.Add(time.Hour)}, key)
	if e != nil {
		t.Fatal(e)
	}
	if Verify(r, pub, "deployment", "owner", ref, c, now) != nil {
		t.Fatal("valid signature denied")
	}
	for _, mutation := range []func(*Record){func(r *Record) { r.OwnerID = "other" }, func(r *Record) { r.Agent.Revision++ }, func(r *Record) { r.ContractDigest = ref.Digest }, func(r *Record) { r.DeploymentID = "other" }, func(r *Record) { r.KeyID = ref.Digest }, func(r *Record) { r.Signature = nil }, func(r *Record) { r.Contract.Task = "other" }, func(r *Record) { r.Purpose = "audit" }, func(r *Record) { r.Candidate.Digest = ref.Digest }} {
		bad := r
		mutation(&bad)
		if Verify(bad, pub, "deployment", "owner", ref, c, now) == nil {
			t.Fatal("tamper authorized")
		}
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if Verify(r, other, "deployment", "owner", ref, c, now) == nil {
		t.Fatal("wrong key authorized")
	}
	if Verify(r, pub, "deployment", "owner", ref, c, r.ExpiresAt) == nil {
		t.Fatal("expired authorized")
	}
	c.MaxAttempts = 2
	if Verify(r, pub, "deployment", "owner", ref, c, now) == nil {
		t.Fatal("changed budget authorized")
	}
}

func TestSignedHostApprovalNamedCandidate(t *testing.T) {
	now := time.Now().UTC()
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	contract := loop.DoerContract{Task: "Write result", Workspace: "/project", WritableFiles: []string{"out"}, VerifyFile: "out", MaxAttempts: 1}
	candidate, _, err := loop.NewDoerRevision("named", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Name = "Review selected output"
	candidate, _, err = loop.NewRevision(candidate)
	if err != nil {
		t.Fatal(err)
	}
	agent := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	digest, _ := contract.Digest()
	r, err := Sign(Record{CandidateName: candidate.Name, Purpose: Purpose, DeploymentID: "deployment", PrincipalID: "owner", OwnerID: "owner", Agent: agent, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}, Contract: contract, ContractDigest: digest, ApprovedAt: now, ExpiresAt: now.Add(time.Hour)}, key)
	if err != nil {
		t.Fatal(err)
	}
	if Verify(r, pub, "deployment", "owner", agent, contract, now) != nil {
		t.Fatal("exact named candidate denied")
	}
	for _, name := range []string{"", "renamed"} {
		bad := r
		bad.CandidateName = name
		if Verify(bad, pub, "deployment", "owner", agent, contract, now) == nil {
			t.Fatal("changed name admitted")
		}
	}
}
