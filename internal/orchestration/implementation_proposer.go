package orchestration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/implementation"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
	"io"
	"strings"
)

// HermesPatchProposer transports data only. The sealed runtime authority must
// already grant zero tools and zero credentials; this adapter never edits it.
// It cannot apply files, execute a checker, or assert successful verification.
type HermesPatchProposer struct {
	Adapter interface {
		AttemptTurn(context.Context, hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error)
	}
	Request hermesruntime.AttemptTurnRequest
}

func (p HermesPatchProposer) Propose(ctx context.Context, request implementation.Request) ([]implementation.Edit, error) {
	result, err := p.propose(ctx, request, false)
	return result.Edits, err
}

func (p HermesPatchProposer) ProposeReport(ctx context.Context, request implementation.Request) (implementation.Proposal, error) {
	return p.propose(ctx, request, true)
}

func (p HermesPatchProposer) propose(ctx context.Context, request implementation.Request, reported bool) (implementation.Proposal, error) {
	if p.Adapter == nil || len(p.Request.Launch.AuthorityContext.Authority.Tools) != 0 || len(p.Request.Launch.AuthorityContext.Authority.Credentials) != 0 || len(p.Request.Credentials) != 0 {
		return implementation.Proposal{}, errors.New("patch proposal requires a sealed tool-free credential-free Hermes authority")
	}
	if err := request.Contract.Validate(); err != nil {
		return implementation.Proposal{}, err
	}
	instruction := patchResponseInstruction(false)
	if reported {
		instruction = patchResponseInstruction(true)
	}
	wire, err := json.Marshal(struct {
		Instruction string                 `json:"instruction"`
		Request     implementation.Request `json:"request"`
	}{instruction, request})
	if err != nil {
		return implementation.Proposal{}, err
	}
	turn := p.Request
	if len(wire) > hermesruntime.MaxAttemptInputBytes || (turn.Bounds.InputBytes > 0 && len(wire) > turn.Bounds.InputBytes) {
		return implementation.Proposal{}, errors.New("serialized implementation input exceeds budget")
	}
	turn.AttemptID = request.PassID
	turn.Input = string(wire)
	if request.InputObserver != nil {
		if err = request.InputObserver(implementation.InputObservation{Bytes: wire}); err != nil {
			return implementation.Proposal{}, err
		}
	}
	result, err := p.Adapter.AttemptTurn(ctx, turn)
	if err != nil {
		failure := doerTurnFailure(result, err)
		if request.FailureObserver != nil {
			v := implementation.ManagedFailure{}
			switch string(result.Attempt.State) {
			case "failed", "denied", "expired", "cancelled", "interrupted", "revoked":
				v.State = string(result.Attempt.State)
			}
			if hermesruntime.IsAttemptFailureReason(result.Attempt.Reason) {
				v.Reason = result.Attempt.Reason
			}
			var closed *doerFailure
			if errors.As(failure, &closed) {
				v.Code = closed.code
			}
			if x := request.FailureObserver(v); x != nil {
				return implementation.Proposal{}, x
			}
		}
		return implementation.Proposal{}, failure
	}
	if request.InputObserver != nil {
		if err = request.InputObserver(implementation.InputObservation{Bytes: wire, Completed: true, TurnID: result.Attempt.ID, RuntimeSessionID: result.Attempt.RuntimeSessionID}); err != nil {
			return implementation.Proposal{}, err
		}
	}
	return decodePatch([]byte(result.Output), reported)
}
func decodePatchProposal(wire []byte) ([]implementation.Edit, error) {
	p, err := decodePatch(wire, false)
	return p.Edits, err
}

func decodeReportedPatch(wire []byte) (implementation.Proposal, error) {
	return decodePatch(wire, true)
}

func decodePatch(wire []byte, reported bool) (implementation.Proposal, error) {
	if len(wire) == 0 || len(wire) > hermesruntime.MaxAttemptOutputBytes {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_size_invalid", nil)
	}
	// Reject duplicates before typed decoding; accepting last-key-wins patches
	// would make operator review and controller interpretation disagree.
	var inspect func(*json.Decoder) error
	inspect = func(d *json.Decoder) error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return closedDoerFailure("doer_proposal_duplicate_field", nil)
				}
				seen[key] = true
				if e = inspect(d); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if err := inspect(d); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid proposal JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := inspect(json.NewDecoder(bytes.NewReader(wire))); err != nil {
		var failure *doerFailure
		if errors.As(err, &failure) {
			return implementation.Proposal{}, failure
		}
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_json_invalid", nil)
	}
	// Validate exact field names and JSON types before Go's permissive struct
	// matching (case-insensitive names and null/byte-array coercions).
	var fields map[string]json.RawMessage
	first := json.NewDecoder(bytes.NewReader(wire))
	if first.Decode(&fields) != nil || fields == nil {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_shape_invalid", nil)
	}
	if first.Decode(new(any)) != io.EOF {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_trailing_data", nil)
	}
	for key := range fields {
		if key != "edits" && key != "report" {
			return implementation.Proposal{}, closedDoerFailure("doer_proposal_unknown_field", nil)
		}
	}
	if raw, ok := fields["report"]; ok {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '"' {
			return implementation.Proposal{}, closedDoerFailure("doer_proposal_shape_invalid", nil)
		}
	}
	var edits []map[string]json.RawMessage
	if json.Unmarshal(fields["edits"], &edits) != nil {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_shape_invalid", nil)
	}
	for _, edit := range edits {
		for key := range edit {
			if key != "path" && key != "content" {
				return implementation.Proposal{}, closedDoerFailure("doer_proposal_unknown_field", nil)
			}
		}
		for _, key := range []string{"path", "content"} {
			raw := bytes.TrimSpace(edit[key])
			if len(raw) == 0 || raw[0] != '"' {
				return implementation.Proposal{}, closedDoerFailure("doer_proposal_shape_invalid", nil)
			}
		}
	}
	var proposal implementation.Proposal
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	if err := d.Decode(&proposal); err != nil {
		var invalidBase64 base64.CorruptInputError
		if errors.As(err, &invalidBase64) {
			return implementation.Proposal{}, closedDoerFailure("doer_proposal_base64_invalid", nil)
		}
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_shape_invalid", nil)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_trailing_data", nil)
	}
	if len(proposal.Edits) == 0 || len(proposal.Edits) > 128 {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_edits_invalid", nil)
	}
	if reported && (strings.TrimSpace(proposal.Report) == "" || len(proposal.Report) > 65536) || !reported && proposal.Report != "" {
		return implementation.Proposal{}, closedDoerFailure("doer_proposal_report_invalid", nil)
	}
	return proposal, nil
}
