// Package continuation owns one purpose-bound canonical Doer approval. It is
// an exact operational approval, not a mandate or a generic signing service.
package continuation

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/reference"
)

var ErrInvalid = errors.New("invalid signed Doer continuation")
var ErrConflict = errors.New("Doer continuation content conflict")

const Purpose = "aegis.doer-continuation.v1"

type Run struct {
	Agent          reference.RevisionRef   `json:"agent"`
	Loop           reference.RevisionRef   `json:"loop"`
	IdempotencyKey string                  `json:"idempotency_key"`
	QueueItemID    string                  `json:"queue_item_id,omitempty"`
	Activate       bool                    `json:"activate,omitempty"`
	Inputs         []graph.NormalizedInput `json:"inputs,omitempty"`
}

type Intent struct {
	DraftID         string                `json:"draft_id"`
	DraftVersion    uint64                `json:"draft_version"`
	CandidateDigest string                `json:"candidate_digest"`
	Agent           reference.RevisionRef `json:"agent"`
	Loop            reference.RevisionRef `json:"loop"`
	Charter         reference.RevisionRef `json:"charter"`
	PublicationKey  string                `json:"publication_key"`
	Run             Run                   `json:"run"`
	Requester       core.Subject          `json:"requester"`
	SessionID       string                `json:"session_id"`
	DeploymentID    string                `json:"deployment_id"`
	ConfigIdentity  string                `json:"config_identity"`
	ExpiresAt       time.Time             `json:"expires_at"`
}

type Approval struct {
	Purpose   string       `json:"purpose"`
	ID        string       `json:"id"`
	Intent    Intent       `json:"intent"`
	Executor  core.Subject `json:"executor"`
	StanzaID  string       `json:"stanza_id"`
	PublicKey string       `json:"public_key"`
	Signature string       `json:"signature"`
}

// StableID reserves a session/draft/execution-key slot independent of content.
// Changing any other approved content therefore conflicts instead of reminting.
func StableID(i Intent) string {
	b, _ := json.Marshal([]string{Purpose, i.Requester.PrincipalID, i.SessionID, i.DraftID, i.Run.IdempotencyKey})
	h := sha256.Sum256(b)
	return "doercont-" + hex.EncodeToString(h[:])
}

func subjectValid(s core.Subject) bool {
	return s.ID != "" && (s.Kind == "human" || s.Kind == "principal") && s.PrincipalID != "" && s.Issuer != "" && s.Method != "" && !s.AuthenticatedAt.IsZero() && s.AuthenticatedAt.Before(s.ExpiresAt)
}

// Payload returns only the strict typed purpose-bound preimage. Raw input values
// must already use the Graph canonical encoding; duplicate keys never get signed.
func Payload(a Approval) ([]byte, error) {
	i := a.Intent
	if i.Requester.Kind != "principal" || i.Requester.Method != "password" || i.Requester.Issuer != "aegis-principal-auth" || a.Executor.Kind != "human" || a.Executor.Method != "local-os" || a.Executor.Issuer != "linux-so-peercred" {
		return nil, ErrInvalid
	}
	if a.Purpose != Purpose || a.ID != StableID(i) || a.StanzaID == "" || i.DraftID == "" || i.DraftVersion == 0 || i.PublicationKey == "" || i.SessionID == "" || i.DeploymentID == "" || i.ConfigIdentity == "" || i.Agent.Validate() != nil || i.Loop.Validate() != nil || i.Charter.Validate() != nil || i.CandidateDigest != i.Loop.Digest || i.Run.Agent != i.Agent || i.Run.Loop != i.Loop || i.Run.IdempotencyKey == "" || len(i.Run.IdempotencyKey) > 256 || !subjectValid(i.Requester) || !subjectValid(a.Executor) || i.Requester.PrincipalID != a.Executor.PrincipalID || !i.Requester.AuthenticatedAt.Before(i.ExpiresAt) || !a.Executor.AuthenticatedAt.Before(i.ExpiresAt) || i.ExpiresAt.After(i.Requester.ExpiresAt) || i.ExpiresAt.After(a.Executor.ExpiresAt) {
		return nil, ErrInvalid
	}
	key, e := base64.StdEncoding.DecodeString(a.PublicKey)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrInvalid
	}
	previous := ""
	for _, input := range i.Run.Inputs {
		canonical, err := graph.CanonicalInputJSON(input.Value)
		if err != nil || !bytes.Equal(canonical, input.Value) || input.PortID == "" || input.PortID <= previous {
			return nil, ErrInvalid
		}
		previous = input.PortID
	}
	a.Signature = ""
	encoded, err := json.Marshal(a)
	if err != nil || len(encoded) > 128*1024 {
		return nil, ErrInvalid
	}
	return encoded, nil
}

func Verify(a Approval, trusted ed25519.PublicKey) error {
	b, err := Payload(a)
	if err != nil || len(trusted) != ed25519.PublicKeySize || a.PublicKey != base64.StdEncoding.EncodeToString(trusted) {
		return ErrInvalid
	}
	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil || !ed25519.Verify(trusted, b, sig) {
		return ErrInvalid
	}
	return nil
}

// Decode requires byte-for-byte canonical encoding, excluding ambiguous JSON,
// unknown fields, duplicate fields, alternate case, trailing data and whitespace.
func Decode(b []byte) (Approval, error) {
	var a Approval
	if len(b) > 128*1024 || json.Unmarshal(b, &a) != nil {
		return a, ErrInvalid
	}
	wire, err := json.Marshal(a)
	if err != nil || !bytes.Equal(b, wire) {
		return Approval{}, ErrInvalid
	}
	if _, err = Payload(a); err != nil {
		return Approval{}, err
	}
	return a, nil
}
