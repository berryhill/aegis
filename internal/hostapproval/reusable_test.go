package hostapproval

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func reusableApprovalFixture(t *testing.T) (Record, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	text := "hello"
	contract := loop.DoerContract{Task: "Write result", Workspace: "/project", WritableFiles: []string{"out"}, VerifyFile: "out", ExpectedText: &text, MaxAttempts: 2}
	source := loop.DoerReusableContract{MaxAttempts: 2}
	previous := "sha256:" + strings.Repeat("b", 64)
	candidate, _, err := loop.NewDoerReusableRevision("reusable", 2, previous, source)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return Record{
		Purpose: Purpose, DeploymentID: "deployment", PrincipalID: "owner", OwnerID: "owner",
		Agent:          reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)},
		Candidate:      reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest},
		PreviousDigest: previous, Contract: contract, ContractDigest: digest,
		ApprovedAt: now, ExpiresAt: now.Add(time.Hour), RunRequestID: "exact-run", DoerReusable: &source,
	}, pub, key
}

func TestReusableHostApprovalSignedRoundTrip(t *testing.T) {
	for _, reexecution := range []bool{false, true} {
		t.Run(map[bool]string{false: "run", true: "reexecution"}[reexecution], func(t *testing.T) {
			r, pub, key := reusableApprovalFixture(t)
			if reexecution {
				r.ImplementationPassCap = 1
				r.ReexecutionDigest = "sha256:" + strings.Repeat("c", 64)
				r.Generation = 1
				r.ApprovalPreviousDigest = "sha256:" + strings.Repeat("d", 64)
			}
			signed, err := Sign(r, key)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := json.Marshal(signed)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(wire)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.DoerReusable == nil || *decoded.DoerReusable != *r.DoerReusable || decoded.RunRequestID != r.RunRequestID {
				t.Fatal("exact v5 source or intent lost")
			}
			if err := Verify(decoded, pub, r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt); err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(decoded)
			if !bytes.Equal(wire, again) || signed.SignedDigest() != decoded.SignedDigest() {
				t.Fatal("canonical signed record changed")
			}
		})
	}
}

func TestReusableHostApprovalRejectsInvalidScope(t *testing.T) {
	cases := map[string]func(*Record){
		"zero source budget":       func(r *Record) { r.DoerReusable.MaxAttempts = 0 },
		"excess source budget":     func(r *Record) { r.DoerReusable.MaxAttempts = 4 },
		"attempt mismatch":         func(r *Record) { r.Contract.MaxAttempts = 1; r.ContractDigest, _ = r.Contract.Digest() },
		"missing intent":           func(r *Record) { r.RunRequestID = "" },
		"unsupported exact bytes":  func(r *Record) { r.Contract.ExactBytes = true; r.ContractDigest, _ = r.Contract.Digest() },
		"missing source":           func(r *Record) { r.DoerReusable = nil },
		"wrong candidate digest":   func(r *Record) { r.Candidate.Digest = r.Agent.Digest },
		"wrong candidate id":       func(r *Record) { r.Candidate.ID = "other" },
		"wrong candidate revision": func(r *Record) { r.Candidate.Revision++ },
		"wrong previous digest":    func(r *Record) { r.PreviousDigest = r.Agent.Digest },
		"v4 candidate": func(r *Record) {
			v4, _, err := loop.NewDoerRevision(r.Candidate.ID, r.Candidate.Revision, r.PreviousDigest, r.Contract)
			if err != nil {
				t.Fatal(err)
			}
			r.Candidate.Digest = v4.Digest
		},
		"changed contract digest":    func(r *Record) { r.ContractDigest = r.Agent.Digest },
		"nonowner principal":         func(r *Record) { r.PrincipalID = "other" },
		"excess approval lifetime":   func(r *Record) { r.ExpiresAt = r.ApprovedAt.Add(25 * time.Hour) },
		"reexecution without digest": func(r *Record) { r.ImplementationPassCap = 1 },
		"reexecution without cap":    func(r *Record) { r.ReexecutionDigest = r.Agent.Digest },
		"reexecution resets budget":  func(r *Record) { r.ImplementationPassCap = 2; r.ReexecutionDigest = r.Agent.Digest },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, pub, key := reusableApprovalFixture(t)
			mutate(&r)
			if _, err := Sign(r, key); !errors.Is(err, ErrDenied) {
				t.Fatalf("Sign accepted invalid scope: %v", err)
			}
			if err := Verify(r, pub, r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt); !errors.Is(err, ErrDenied) {
				t.Fatalf("Verify accepted invalid scope: %v", err)
			}
		})
	}
}

func TestReusableHostApprovalRejectsSignedTampering(t *testing.T) {
	cases := map[string]func(*Record){
		"task":      func(r *Record) { r.Contract.Task = "Other task" },
		"workspace": func(r *Record) { r.Contract.Workspace = "/other" },
		"allowlist": func(r *Record) { r.Contract.WritableFiles = []string{"out", "other"} },
		"assertion": func(r *Record) { text := "other"; r.Contract.ExpectedText = &text },
		"budget and source": func(r *Record) {
			r.Contract.MaxAttempts = 3
			r.DoerReusable = &loop.DoerReusableContract{MaxAttempts: 3}
			candidate, _, err := loop.NewDoerReusableRevision(r.Candidate.ID, r.Candidate.Revision, r.PreviousDigest, *r.DoerReusable)
			if err != nil {
				t.Fatal(err)
			}
			r.Candidate.Digest = candidate.Digest
		},
		"intent":    func(r *Record) { r.RunRequestID = "other-run" },
		"agent":     func(r *Record) { r.Agent.Revision++ },
		"signature": func(r *Record) { r.Signature = append([]byte(nil), r.Signature...); r.Signature[0] ^= 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, pub, key := reusableApprovalFixture(t)
			signed, err := Sign(r, key)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&signed)
			// Even self-consistent edited contracts cannot reuse the signature.
			signed.ContractDigest, err = signed.Contract.Digest()
			if err != nil {
				t.Fatal(err)
			}
			if err := Verify(signed, pub, signed.DeploymentID, signed.OwnerID, signed.Agent, signed.Contract, signed.ApprovedAt); !errors.Is(err, ErrDenied) {
				t.Fatalf("signed tampering accepted: %v", err)
			}
		})
	}
}

func TestReusableHostApprovalFreshnessAndCallerScope(t *testing.T) {
	r, pub, key := reusableApprovalFixture(t)
	r, err := Sign(r, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(r, pub, r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt.Add(-time.Second)); !errors.Is(err, ErrDenied) {
		t.Fatal("future approval accepted")
	}
	if err := Verify(r, pub, r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ExpiresAt); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry: %v", err)
	}
	other := r.Contract
	other.Workspace = "/other"
	if Verify(r, pub, r.DeploymentID, r.OwnerID, r.Agent, other, r.ApprovedAt) == nil {
		t.Fatal("different caller contract accepted")
	}
	if Verify(r, pub, "other", r.OwnerID, r.Agent, r.Contract, r.ApprovedAt) == nil {
		t.Fatal("different deployment accepted")
	}
	if Verify(r, pub, r.DeploymentID, "other", r.Agent, r.Contract, r.ApprovedAt) == nil {
		t.Fatal("different owner accepted")
	}
}

func TestReusableHostApprovalDecodeRejectsMalformedSource(t *testing.T) {
	r, _, key := reusableApprovalFixture(t)
	signed, err := Sign(r, key)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(signed)
	for _, source := range []string{`null`, `"v5"`, `{"max_attempts":"2"}`, `{"max_attempts":2,"unknown":true}`, `{"max_attempts":2,"max_attempts":2}`} {
		bad := bytes.Replace(wire, []byte(`{"max_attempts":2}`), []byte(source), 1)
		if _, err := Decode(bad); !errors.Is(err, ErrDenied) {
			t.Fatalf("malformed source accepted: %s: %v", source, err)
		}
	}
}

func TestHistoricalV4HostApprovalCanonicalCompatibility(t *testing.T) {
	r, pub, key := reusableApprovalFixture(t)
	r.DoerReusable = nil
	r.RunRequestID = ""
	candidate, _, err := loop.NewDoerRevision(r.Candidate.ID, r.Candidate.Revision, r.PreviousDigest, r.Contract)
	if err != nil {
		t.Fatal(err)
	}
	r.Candidate.Digest = candidate.Digest
	r.KeyID = KeyID(pub)
	// Historical wire shape, predating any optional v5 source field.
	legacy := struct {
		Purpose        string                `json:"purpose"`
		DeploymentID   string                `json:"deployment_id"`
		PrincipalID    string                `json:"principal_id"`
		OwnerID        string                `json:"owner_id"`
		Agent          reference.RevisionRef `json:"agent"`
		Candidate      reference.RevisionRef `json:"candidate"`
		PreviousDigest string                `json:"previous_digest,omitempty"`
		Contract       loop.DoerContract     `json:"contract"`
		ContractDigest string                `json:"contract_digest"`
		ApprovedAt     time.Time             `json:"approved_at"`
		ExpiresAt      time.Time             `json:"expires_at"`
		KeyID          string                `json:"key_id"`
		Signature      []byte                `json:"signature"`
	}{r.Purpose, r.DeploymentID, r.PrincipalID, r.OwnerID, r.Agent, r.Candidate, r.PreviousDigest, r.Contract, r.ContractDigest, r.ApprovedAt, r.ExpiresAt, r.KeyID, nil}
	oldCanonical, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := r.canonical()
	if err != nil || !bytes.Equal(canonical, oldCanonical) {
		t.Fatalf("v4 canonical bytes changed: %v", err)
	}
	legacy.Signature = ed25519.Sign(key, oldCanonical)
	oldWire, _ := json.Marshal(legacy)
	decoded, err := Decode(oldWire)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(decoded, pub, r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt); err != nil {
		t.Fatalf("historical signature rejected: %v", err)
	}
	signed, err := Sign(r, key)
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(signed)
	if !bytes.Equal(oldWire, wire) {
		t.Fatal("v4 signed wire bytes changed")
	}
}
