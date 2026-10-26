package delivery

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
	"io"
	"regexp"
)

// HistorySnapshot is a read-only projection of one canonical store view. It is
// not a verifier verdict, current authority, or permission to renew an epoch.
type HistorySnapshot struct {
	Contract      contribution.Contract
	ContractBytes []byte
	RootRun       string
	Root          PublicationRoot
	RootBytes     []byte
	Histories     [][]HistoryEnvelope
}

var historySHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// HistoryEnvelope seals every append and links its predecessor. Head is only
// a checked projection of this complete, append-only canonical history.
type HistoryEnvelope struct {
	Record                 Record
	PreviousDigest, Digest string
}

// Record embeds Observation with a second Outcome field; its default
// JSON encoding loses the embedded field. Use an explicit disk-only envelope.
type historyEnvelopeWire struct {
	Intent                 Intent
	Outcome                Outcome
	Observation            Observation
	Revision               uint64
	PreviousDigest, Digest string
}

func (v HistoryEnvelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(historyEnvelopeWire{v.Record.Intent, v.Record.Outcome, v.Record.Observation, v.Record.Revision, v.PreviousDigest, v.Digest})
}
func (v *HistoryEnvelope) UnmarshalJSON(b []byte) error {
	var w historyEnvelopeWire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if reference.RejectDuplicateObjectKeys(b) != nil || d.Decode(&w) != nil || d.Decode(new(any)) != io.EOF {
		return ErrDenied
	}
	*v = HistoryEnvelope{Record: Record{Intent: w.Intent, Outcome: w.Outcome, Observation: w.Observation, Revision: w.Revision}, PreviousDigest: w.PreviousDigest, Digest: w.Digest}
	return nil
}

type PublicationRoot struct {
	QueueItemID, OwnerID, Key, Run, Contract, Repository, Base, Branch string
	RepositoryID                                                       uint64
	Digest                                                             string
}

func ValidHistoryOutcome(o Outcome) bool {
	return o == Unknown || o == Confirmed || o == Failed || o == Denied
}
func ValidateHistoryTransition(old Record, o Observation) error {
	if o.ProofJSON != "" || o.ProofDigest != "" {
		if e := ValidateRetainedReadback(old, o); e != nil {
			return e
		}
	}
	if !ValidHistoryOutcome(o.Outcome) || len(o.Code) > 256 {
		return ErrDenied
	}
	for _, s := range []string{o.Head, o.Tree, o.Merge, o.BaseHead} {
		if s != "" && !historySHA.MatchString(s) {
			return ErrDenied
		}
	}
	if old.Outcome != Unknown && old.Observation != o {
		return ErrConflict
	}
	// Positive remote facts never disappear or change, even on a denied or unknown
	// post-merge result. A later correction requires a separate admitted intent.
	for _, p := range [][2]string{{old.Head, o.Head}, {old.Tree, o.Tree}, {old.Merge, o.Merge}, {old.BaseHead, o.BaseHead}} {
		if p[0] != "" && p[0] != p[1] {
			return ErrConflict
		}
	}
	if old.PR != 0 && old.PR != o.PR {
		return ErrConflict
	}
	return nil
}

func historyWire(v any) []byte { b, _ := json.Marshal(v); return b }
func strictHistory(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if reference.RejectDuplicateObjectKeys(b) != nil || d.Decode(v) != nil || d.Decode(new(any)) != io.EOF || !bytes.Equal(b, historyWire(v)) {
		return ErrDenied
	}
	return nil
}

// ValidateHistoryEnvelope validates the existing sealed envelope digest, not a
// new DTO hash. Chain position/predecessor checks belong to ValidateHistorySnapshot.
func ValidateHistoryEnvelope(v HistoryEnvelope) error {
	seal := v.Digest
	v.Digest = ""
	if !roleDigest.MatchString(seal) || seal != contribution.Digest(historyWire(v)) || v.Record.Revision == 0 || v.Record.Intent.ID == "" || v.Record.Outcome != v.Record.Observation.Outcome || !ValidHistoryOutcome(v.Record.Outcome) {
		return ErrDenied
	}
	if v.Record.Revision == 1 && (v.PreviousDigest != "" || v.Record.Outcome != Unknown) {
		return ErrDenied
	}
	if v.Record.Revision > 1 && !roleDigest.MatchString(v.PreviousDigest) {
		return ErrDenied
	}
	if v.Record.ProofJSON != "" || v.Record.ProofDigest != "" {
		if _, err := ParseReadback(v.Record.Intent, v.Record.Observation); err != nil {
			return ErrDenied
		}
	}
	return nil
}
func ParseHistoryEnvelope(b []byte) (HistoryEnvelope, error) {
	var v HistoryEnvelope
	if strictHistory(b, &v) != nil || ValidateHistoryEnvelope(v) != nil {
		return HistoryEnvelope{}, ErrDenied
	}
	return v, nil
}

// ValidateHistorySnapshot checks canonical root/contract bytes, owner/root binding,
// sorted unique intent chains and exact revisions/predecessors. No current runtime
// admission is consulted and historical role/epoch values are never replaced.
func ValidateHistorySnapshot(v HistorySnapshot) error {
	c, err := contribution.Unmarshal(v.ContractBytes)
	canonical, e := contribution.Marshal(v.Contract)
	if err != nil || e != nil || !bytes.Equal(canonical, v.ContractBytes) || !bytes.Equal(historyWire(c), historyWire(v.Contract)) {
		return ErrDenied
	}
	var root PublicationRoot
	if strictHistory(v.RootBytes, &root) != nil || root != v.Root {
		return ErrDenied
	}
	seal := root.Digest
	root.Digest = ""
	if !roleDigest.MatchString(seal) || seal != contribution.Digest(historyWire(root)) {
		return ErrDenied
	}
	if root.OwnerID != c.OwnerID || root.Key != c.Key || root.QueueItemID != c.QueueItemID || root.Run != c.RunID || v.RootRun != c.RunID || root.Contract != c.Digest || root.Repository != c.Repository || root.RepositoryID != c.Source.RepositoryID || root.Base == "" || root.Branch == "" || root.Base == root.Branch {
		return ErrDenied
	}
	if len(v.Histories) == 0 || len(v.Histories) > 512 {
		return ErrDenied
	}
	lastID := ""
	for _, h := range v.Histories {
		if len(h) == 0 || len(h) > 2048 {
			return ErrDenied
		}
		i := h[0].Record.Intent
		if i.ID <= lastID || i.Run != root.Run || i.Contract != root.Contract || i.Repository != root.Repository || i.RepositoryID != root.RepositoryID || i.Base != root.Base || i.Branch != root.Branch {
			return ErrDenied
		}
		lastID = i.ID
		previous := ""
		for n, envelope := range h {
			if ValidateHistoryEnvelope(envelope) != nil || envelope.Record.Intent != i || envelope.Record.Revision != uint64(n+1) || envelope.PreviousDigest != previous {
				return ErrDenied
			}
			if n > 0 && ValidateHistoryTransition(h[n-1].Record, envelope.Record.Observation) != nil {
				return ErrDenied
			}
			previous = envelope.Digest
		}
	}
	return nil
}
