package implementation

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
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

// RetainEngineeringContract preserves the complete validated preparation in
// private custody, independently of the bounded model task view. Not a runtime
// input allowance: this body is never sent to a model.
func (e *ManagedContribution) RetainEngineeringContract(wire []byte) (string, error) {
	if len(wire) == 0 || len(wire) > 32<<20 {
		return "", errors.New("engineering contract custody bound")
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
	if req.Contract.SchemaVersion == loop.RepositoryEngineeringSchema {
		var task struct {
			ContractID        string   `json:"contract_id"`
			ContractDigest    string   `json:"contract_digest"`
			ContractCustody   string   `json:"contract_custody"`
			PreparationDigest string   `json:"preparation_digest"`
			SourceDigest      string   `json:"source_digest"`
			IssueURL          string   `json:"issue_url"`
			Exclusions        []string `json:"exclusions"`
		}
		if json.Unmarshal(req.Contribution, &task) != nil || task.ContractID != ref.ID || task.ContractDigest != ref.Digest {
			return bad
		}
		full := previousBytes(e.DB, task.ContractCustody)
		if len(full) == 0 || digest(full) != task.ContractCustody {
			return bad
		}
		var retained struct {
			Preparation struct {
				Digest       string `json:"digest"`
				SourceDigest string `json:"source_digest"`
			} `json:"engineering_preparation"`
			Source struct {
				URL struct {
					URL string `json:"url"`
				} `json:"url"`
			} `json:"source"`
			Exclusions []string `json:"exclusions"`
		}
		if json.Unmarshal(full, &retained) != nil || retained.Preparation.Digest != task.PreparationDigest || retained.Preparation.SourceDigest != task.SourceDigest || retained.Source.URL.URL != task.IssueURL {
			return bad
		}
		a, _ := json.Marshal(retained.Exclusions)
		b, _ := json.Marshal(task.Exclusions)
		if !bytes.Equal(a, b) {
			return bad
		}
		req.Contribution = full
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
