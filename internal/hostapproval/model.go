// Package hostapproval owns purpose-separated exact host-write approvals.
// These records grant no session, model, provisioning or Graph authority.
package hostapproval

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"time"
)

const Purpose = "aegis.doer.host-write-approval.v1"

// MaxGenerations bounds verification work and retained per-scope history.
// Exhaustion denies new consent; prior facts are never pruned.
const MaxGenerations = 256

// SignedDigest binds a successor to the complete canonical signed fact.
func (r Record) SignedDigest() string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Custody adapters depend on this typed codec, not Loop policy or ref parsing.
type AgentReference = reference.RevisionRef
type Contract = loop.DoerContract

func Decode(raw []byte) (Record, error) {
	var value Record
	if len(raw) > 64*1024 || json.Unmarshal(raw, &value) != nil {
		return value, ErrDenied
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(raw, canonical) {
		return Record{}, ErrDenied
	}
	return value, nil
}

var ErrDenied = errors.New("exact Doer host-write approval absent or invalid")

// These closed custody causes retain the existing denial identity.
var ErrAbsent = errors.Join(ErrDenied, errors.New("doer_host_approval_absent"))
var ErrExpired = errors.Join(ErrDenied, errors.New("doer_host_approval_expired"))

type Record struct {
	// Nonempty only after an independent protected controller decision for a
	// differently owned participant. Omitted on historical owner-principal facts.
	ControllerSubjectID string                `json:"controller_subject_id,omitempty"`
	RunInputDigest      string                `json:"run_input_digest,omitempty"`
	Purpose             string                `json:"purpose"`
	DeploymentID        string                `json:"deployment_id"`
	PrincipalID         string                `json:"principal_id"`
	OwnerID             string                `json:"owner_id"`
	Agent               reference.RevisionRef `json:"agent"`
	Candidate           reference.RevisionRef `json:"candidate"`
	PreviousDigest      string                `json:"previous_digest,omitempty"`
	Contract            loop.DoerContract     `json:"contract"`
	ContractDigest      string                `json:"contract_digest"`
	ApprovedAt          time.Time             `json:"approved_at"`
	ExpiresAt           time.Time             `json:"expires_at"`
	KeyID               string                `json:"key_id"`
	Signature           []byte                `json:"signature"`
	// Nonempty only for an exact authenticated owner-principal Run, not a
	// separate human setup decision. Signed with the complete exact scope.
	RunRequestID          string `json:"run_request_id,omitempty"`
	ImplementationPassCap uint16 `json:"implementation_pass_cap,omitempty"`
	ReexecutionDigest     string `json:"reexecution_digest,omitempty"`
	// Generation zero retains the original canonical record format.
	Generation             uint64 `json:"generation,omitempty"`
	ApprovalPreviousDigest string `json:"approval_previous_digest,omitempty"`
	// DoerReusable binds an exact v5 source revision, never template-wide
	// authority. Contract and RunRequestID still seal one concrete Run.
	// Omission preserves historical v4 canonical signed bytes.
	DoerReusable *loop.DoerReusableContract `json:"doer_reusable,omitempty"`
}

func KeyID(key ed25519.PublicKey) string {
	h := sha256.Sum256(key)
	return "sha256:" + hex.EncodeToString(h[:])
}
func (r Record) canonical() ([]byte, error) {
	if r.Generation >= MaxGenerations || (r.Generation == 0 && r.ApprovalPreviousDigest != "") {
		return nil, ErrDenied
	}
	if r.Generation > 0 {
		if len(r.ApprovalPreviousDigest) != 71 || r.ApprovalPreviousDigest[:7] != "sha256:" {
			return nil, ErrDenied
		}
		decoded, err := hex.DecodeString(r.ApprovalPreviousDigest[7:])
		if err != nil || hex.EncodeToString(decoded) != r.ApprovalPreviousDigest[7:] {
			return nil, ErrDenied
		}
	}
	if (r.ImplementationPassCap == 0) != (r.ReexecutionDigest == "") || (r.ImplementationPassCap > 0 && (r.RunRequestID == "" || r.ImplementationPassCap >= r.Contract.MaxAttempts || (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "lineage", Digest: r.ReexecutionDigest}).Validate() != nil)) {
		return nil, ErrDenied
	}
	d, e := r.Contract.Digest()
	var candidate loop.LoopRevision
	var ce error
	if r.DoerReusable != nil {
		if r.RunRequestID == "" || r.Contract.ExactBytes || r.Contract.MaxAttempts != r.DoerReusable.MaxAttempts {
			return nil, ErrDenied
		}
		candidate, _, ce = loop.NewDoerReusableRevision(r.Candidate.ID, r.Candidate.Revision, r.PreviousDigest, *r.DoerReusable)
	} else {
		candidate, _, ce = loop.NewDoerRevision(r.Candidate.ID, r.Candidate.Revision, r.PreviousDigest, r.Contract)
	}
	if ce != nil || candidate.Digest != r.Candidate.Digest {
		return nil, ErrDenied
	}
	if (r.OwnerID != r.PrincipalID) != (r.ControllerSubjectID != "") || r.OwnerID == "" {
		return nil, ErrDenied
	}
	if (r.ControllerSubjectID != "" && r.DoerReusable != nil) != (r.RunInputDigest != "") || (r.RunInputDigest != "" && (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "run-input", Digest: r.RunInputDigest}).Validate() != nil) {
		return nil, ErrDenied
	}
	if e != nil || d != r.ContractDigest || r.Purpose != Purpose || r.DeploymentID == "" || r.PrincipalID == "" || r.Agent.Validate() != nil || r.Candidate.Validate() != nil || r.KeyID == "" || r.ApprovedAt.IsZero() || !r.ApprovedAt.Before(r.ExpiresAt) || r.ExpiresAt.Sub(r.ApprovedAt) > 24*time.Hour {
		return nil, ErrDenied
	}
	r.Signature = nil
	return json.Marshal(r)
}
func Sign(r Record, key ed25519.PrivateKey) (Record, error) {
	if len(key) != ed25519.PrivateKeySize {
		return Record{}, ErrDenied
	}
	r.KeyID = KeyID(key.Public().(ed25519.PublicKey))
	b, e := r.canonical()
	if e != nil {
		return Record{}, e
	}
	r.Signature = ed25519.Sign(key, b)
	return r, nil
}
func Verify(r Record, key ed25519.PublicKey, deployment, owner string, agent reference.RevisionRef, contract loop.DoerContract, now time.Time) error {
	d, e := contract.Digest()
	b, ce := r.canonical()
	if e != nil || ce != nil || len(key) != ed25519.PublicKeySize || r.KeyID != KeyID(key) || r.DeploymentID != deployment || r.OwnerID != owner || r.Agent != agent || r.ContractDigest != d || now.Before(r.ApprovedAt) || !ed25519.Verify(key, b, r.Signature) {
		return ErrDenied
	}
	if !now.Before(r.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
func Slot(deployment, owner string, agent reference.RevisionRef, digest string) string {
	b, _ := json.Marshal([]any{Purpose, deployment, owner, agent, digest})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
