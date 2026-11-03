package implementation

import (
	"bytes"
	"encoding/json"
	"errors"
)

// RetainContributionInput stores exact controller-prepared model input in existing
// private content-addressed custody. The owning service validates the typed
// contribution contract; this lower kernel treats it as opaque canonical data.
func (e *ManagedContribution) RetainContributionInput(wire []byte) (string, error) {
	if len(wire) == 0 || len(wire) > 64<<10 {
		return "", errors.New("contribution input exceeds custody bound")
	}
	return e.blob(wire)
}

func (e *ManagedContribution) validateBuilderInput(r ContributionRecord, candidate ContributionCandidate) error {
	identity := candidate.Builder
	if identity.InputDigest == "" && identity.InputBytes == 0 {
		return nil
	} // Historical binding.
	bad := errors.New("contribution builder input mismatch")
	wire := previousBytes(e.DB, identity.InputDigest)
	if wire == nil || len(wire) != identity.InputBytes || len(wire) > 64<<10 {
		return bad
	}
	var envelope struct {
		Instruction string  `json:"instruction"`
		Request     Request `json:"request"`
	}
	if json.Unmarshal(wire, &envelope) != nil || envelope.Instruction == "" || envelope.Request.PassID != identity.TurnID {
		return bad
	}
	req := envelope.Request
	d, err := req.Contract.Digest()
	ref := req.Contract.Contribution
	if err != nil || d != r.Implementation || ref == nil || ref.ID != r.RunID || ref.Digest != r.Contract {
		return bad
	}
	// Sealing hashes the canonical typed document with its final digest empty.
	// Do not confuse this sealing digest with the blob address of the full wire.
	suffix := []byte(`,"digest":"` + ref.Digest + `"}`)
	if !bytes.HasSuffix(req.Contribution, suffix) {
		return bad
	}
	unsigned := append(append([]byte(nil), req.Contribution[:len(req.Contribution)-len(suffix)]...), []byte(`,"digest":""}`)...)
	if digest(unsigned) != ref.Digest {
		return bad
	}
	return nil
}
