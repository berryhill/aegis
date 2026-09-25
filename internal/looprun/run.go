// Package looprun interprets one immutable Loop revision without granting runtime authority.
// The caller owns admission, durable checkpointing, execution idempotency and evidence verification.
package looprun

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/berryhill/aegis/internal/loop"
)

// Values are JSON port values. They are copied across every callback boundary.
type Values map[string]json.RawMessage

// Failure is a typed, non-authoritative step failure packet. Do not put secrets in Message.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	return f.Code + ": " + f.Message
}

// StepRequest.Key is stable across replays of an ambiguous effect. The injected
// executor MUST deduplicate by this key; a checkpoint cannot make an effect atomic.
type StepRequest struct {
	Key      string    `json:"key"`
	Step     loop.Step `json:"step"`
	Visit    uint32    `json:"visit"`
	Attempt  uint16    `json:"attempt"`
	Inputs   Values    `json:"inputs"`
	Previous *Failure  `json:"previous,omitempty"`
}
type StepResult struct {
	Outputs Values `json:"outputs"`
	Branch  string `json:"branch,omitempty"`
}
type StepExecutor interface {
	Execute(context.Context, StepRequest) (StepResult, error)
}

// CursorStore must atomically and durably replace one cursor and enforce exclusive
// ownership for a run; Load and Save must not share mutable memory with the caller.
type CursorStore interface {
	Load(context.Context) (Cursor, error)
	Save(context.Context, Cursor) error
}
type Cursor struct {
	RevisionDigest   string            `json:"revision_digest"`
	RunID            string            `json:"run_id"`
	InputDigest      string            `json:"input_digest"`
	StepID           string            `json:"step_id"`
	Inputs           Values            `json:"inputs"`
	Visits           map[string]uint32 `json:"visits"`
	Traversals       map[string]uint16 `json:"traversals"`
	TotalSteps       uint32            `json:"total_steps"`
	TotalTransitions uint32            `json:"total_transitions"`
	Pending          *StepRequest      `json:"pending,omitempty"`
	Failures         []Failure         `json:"failures"`
	Done             *Result           `json:"done,omitempty"`
}
type Result struct {
	Outcome     loop.TerminalOutcome `json:"outcome"`
	Outputs     Values               `json:"outputs,omitempty"`
	Failure     *Failure             `json:"failure,omitempty"`
	Failures    []Failure            `json:"failures,omitempty"`
	ReportError string               `json:"report_error,omitempty"`
}
type CompletionReporter interface {
	Report(context.Context, Result) error
}
type ReportFunc func(context.Context, Result) error

func (f ReportFunc) Report(ctx context.Context, r Result) error { return f(ctx, r) }

// Run validates the exact revision and starts or resumes its checkpoint. An
// operational error (load/save/cancellation) returns error; deterministic Loop
// failures return a failed Result and nil error. No model output authorizes a branch.
func Run(ctx context.Context, runID string, rev loop.LoopRevision, inputs Values, store CursorStore, executor StepExecutor, reporter CompletionReporter) (Result, error) {
	if runID == "" || len(runID) > 255 || strings.TrimSpace(runID) != runID || strings.ContainsAny(runID, "/\\\x00\n\r") {
		return Result{}, errors.New("bounded run ID required")
	}
	if store == nil || executor == nil {
		return Result{}, errors.New("cursor store and step executor required")
	}
	if loop.ValidateRevision(rev).Outcome != loop.ValidationValid {
		return Result{}, errors.New("invalid or modified Loop revision")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := checkValues(rev.Inputs, inputs); err != nil {
		return Result{}, fmt.Errorf("Loop inputs: %w", err)
	}
	initial := cloneValues(inputs)
	digest := valuesDigest(initial)
	c, err := store.Load(ctx)
	if err != nil {
		return Result{}, err
	}
	if c.RevisionDigest == "" {
		c = Cursor{RevisionDigest: rev.Digest, RunID: runID, InputDigest: digest, StepID: rev.EntryStepID, Inputs: initial, Visits: map[string]uint32{}, Traversals: map[string]uint16{}}
		if err = store.Save(ctx, cloneCursor(c)); err != nil {
			return Result{}, err
		}
	} else {
		if c.RevisionDigest != rev.Digest || c.RunID != runID || c.InputDigest != digest {
			return Result{}, errors.New("cursor binding mismatch")
		}
		if err = validateCursor(c, rev); err != nil {
			return Result{}, err
		}
	}
	steps := make(map[string]loop.Step, len(rev.Steps))
	edges := make(map[string][]loop.Transition)
	for _, s := range rev.Steps {
		steps[s.ID] = s
	}
	for _, e := range rev.Transitions {
		edges[e.FromStepID] = append(edges[e.FromStepID], e)
	}
	finish := func(result Result) (Result, error) {
		result.Failures = append([]Failure(nil), c.Failures...)
		c.Done = &result
		c.Pending = nil
		if err := store.Save(ctx, cloneCursor(c)); err != nil {
			return Result{}, err
		}
		if reporter != nil {
			if err := reporter.Report(ctx, cloneResult(result)); err != nil {
				result.ReportError = err.Error()
				c.Done = &result
				if saveErr := store.Save(ctx, cloneCursor(c)); saveErr != nil {
					return result, saveErr
				}
			}
		}
		return result, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if c.Done != nil {
			return cloneResult(*c.Done), nil
		}
		s := steps[c.StepID]
		if c.Pending == nil {
			if c.TotalSteps >= loop.MaxTraversals {
				return finish(failed("step_limit", "Loop step visit bound exceeded"))
			}
			c.TotalSteps++
			c.Visits[s.ID]++
			c.Pending = &StepRequest{Key: fmt.Sprintf("%s/%s/%s/%d/1", c.RunID, rev.Digest, s.ID, c.Visits[s.ID]), Step: s, Visit: c.Visits[s.ID], Attempt: 1, Inputs: cloneValues(c.Inputs)}
			if err := store.Save(ctx, cloneCursor(c)); err != nil {
				return Result{}, err
			}
		}
		q := cloneRequest(*c.Pending)
		output, executeErr := executor.Execute(ctx, q)
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if executeErr != nil {
			var packet *Failure
			if !errors.As(executeErr, &packet) || packet == nil || packet.Code == "" || len(packet.Code) > 128 || len(packet.Message) > 1024 {
				packet = &Failure{Code: "step_error", Message: "step execution failed"}
			}
			c.Failures = append(c.Failures, *packet)
			if c.Pending.Attempt >= s.Retry.MaxAttempts {
				return finish(failed("retry_exhausted", fmt.Sprintf("step %s exhausted attempts", s.ID)))
			}
			c.Pending.Attempt++
			c.Pending.Previous = packet
			c.Pending.Key = fmt.Sprintf("%s/%s/%s/%d/%d", c.RunID, rev.Digest, s.ID, c.Pending.Visit, c.Pending.Attempt)
			if err := store.Save(ctx, cloneCursor(c)); err != nil {
				return Result{}, err
			}
			continue
		}
		if err := checkValues(s.OutputPorts, output.Outputs); err != nil {
			return finish(failed("output_invalid", "step output ports invalid"))
		}
		if s.Kind == loop.StepTerminal {
			if output.Branch != "" {
				return finish(failed("branch_invalid", "terminal cannot branch"))
			}
			mapped, err := mapValues(output.Outputs, s.Terminal.OutputMappings)
			if err != nil {
				return finish(failed("output_invalid", "terminal output mapping invalid"))
			}
			if err = checkValues(rev.Outputs, mapped); err != nil {
				return finish(failed("output_invalid", "Loop outputs invalid"))
			}
			return finish(Result{Outcome: s.Terminal.Outcome, Outputs: mapped})
		}
		outgoing := edges[s.ID]
		var chosen *loop.Transition
		if s.Kind == loop.StepGate {
			// Labels are opaque exact strings. Unknown, whitespace-changed or missing labels deny.
			for i := range outgoing {
				if output.Branch != "" && outgoing[i].Condition == output.Branch {
					chosen = &outgoing[i]
					break
				}
			}
		} else if output.Branch == "" && len(outgoing) == 1 {
			chosen = &outgoing[0]
		}
		if chosen == nil {
			return finish(failed("branch_invalid", "no exact declared transition selected"))
		}
		if c.TotalTransitions >= loop.MaxTraversals || chosen.MaxTraversals > 0 && c.Traversals[chosen.ID] >= chosen.MaxTraversals {
			return finish(failed("traversal_exhausted", "transition bound exceeded"))
		}
		next, err := mapValues(output.Outputs, chosen.Mappings)
		if err != nil || checkValues(steps[chosen.ToStepID].InputPorts, next) != nil {
			return finish(failed("mapping_invalid", "transition input mapping invalid"))
		}
		c.Traversals[chosen.ID]++
		c.TotalTransitions++
		c.StepID = chosen.ToStepID
		c.Inputs = next
		c.Pending = nil
		if err := store.Save(ctx, cloneCursor(c)); err != nil {
			return Result{}, err
		}
	}
}
func failed(code, message string) Result {
	return Result{Outcome: loop.OutcomeFailed, Failure: &Failure{Code: code, Message: message}}
}
func checkValues(ports []loop.Port, v Values) error {
	declared := make(map[string]loop.Port, len(ports))
	for _, p := range ports {
		declared[p.ID] = p
		if p.Required {
			if _, ok := v[p.ID]; !ok {
				return fmt.Errorf("missing %s", p.ID)
			}
		}
	}
	for name, raw := range v {
		p, ok := declared[name]
		if !ok {
			return fmt.Errorf("unknown %s", name)
		}
		var x any
		if len(raw) == 0 || json.Unmarshal(raw, &x) != nil {
			return fmt.Errorf("malformed %s", name)
		}
		valid := false
		switch p.Type {
		case loop.TypeString, loop.TypeArtifact:
			_, valid = x.(string)
		case loop.TypeBoolean:
			_, valid = x.(bool)
		case loop.TypeInteger:
			n, ok := x.(float64)
			valid = ok && n == float64(int64(n))
		case loop.TypeNumber:
			_, valid = x.(float64)
		case loop.TypeObject:
			_, valid = x.(map[string]any)
		case loop.TypeArray:
			_, valid = x.([]any)
		}
		if !valid {
			return fmt.Errorf("wrong type %s", name)
		}
	}
	return nil
}
func mapValues(v Values, mappings []loop.PortMapping) (Values, error) {
	out := Values{}
	for _, m := range mappings {
		raw, ok := v[m.SourcePort]
		if !ok {
			return nil, errors.New("missing mapped source")
		}
		out[m.TargetPort] = append(json.RawMessage(nil), raw...)
	}
	return out, nil
}
func cloneValues(v Values) Values {
	out := Values{}
	for k, x := range v {
		out[k] = append(json.RawMessage(nil), x...)
	}
	return out
}
func valuesDigest(v Values) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func cloneRequest(q StepRequest) StepRequest {
	q.Inputs = cloneValues(q.Inputs)
	if q.Previous != nil {
		f := *q.Previous
		q.Previous = &f
	}
	return q
}
func cloneResult(r Result) Result {
	r.Outputs = cloneValues(r.Outputs)
	r.Failures = append([]Failure(nil), r.Failures...)
	if r.Failure != nil {
		f := *r.Failure
		r.Failure = &f
	}
	return r
}
func cloneCursor(c Cursor) Cursor {
	c.Inputs = cloneValues(c.Inputs)
	v := map[string]uint32{}
	for k, n := range c.Visits {
		v[k] = n
	}
	c.Visits = v
	t := map[string]uint16{}
	for k, n := range c.Traversals {
		t[k] = n
	}
	c.Traversals = t
	c.Failures = append([]Failure(nil), c.Failures...)
	if c.Pending != nil {
		q := cloneRequest(*c.Pending)
		c.Pending = &q
	}
	if c.Done != nil {
		r := cloneResult(*c.Done)
		c.Done = &r
	}
	return c
}
func validateCursor(c Cursor, r loop.LoopRevision) error {
	steps := map[string]loop.Step{}
	transitions := map[string]loop.Transition{}
	for _, s := range r.Steps {
		steps[s.ID] = s
	}
	for _, e := range r.Transitions {
		transitions[e.ID] = e
	}
	s, ok := steps[c.StepID]
	if !ok || c.TotalSteps > loop.MaxTraversals || c.TotalTransitions > loop.MaxTraversals || c.Visits == nil || c.Traversals == nil {
		return errors.New("invalid cursor")
	}
	if c.Done != nil {
		return nil
	}
	if err := checkValues(s.InputPorts, c.Inputs); err != nil {
		return fmt.Errorf("invalid cursor inputs: %w", err)
	}
	for id, n := range c.Visits {
		if _, ok := steps[id]; !ok || n > c.TotalSteps {
			return errors.New("invalid cursor visits")
		}
	}
	for id, n := range c.Traversals {
		e, ok := transitions[id]
		if !ok || uint32(n) > c.TotalTransitions || e.MaxTraversals > 0 && n > e.MaxTraversals {
			return errors.New("invalid cursor traversals")
		}
	}
	if c.Pending != nil {
		q := c.Pending
		if !reflect.DeepEqual(q.Step, s) || q.Visit != c.Visits[s.ID] || q.Attempt == 0 || q.Attempt > s.Retry.MaxAttempts || q.Key != fmt.Sprintf("%s/%s/%s/%d/%d", c.RunID, r.Digest, s.ID, q.Visit, q.Attempt) || checkValues(s.InputPorts, q.Inputs) != nil || !reflect.DeepEqual(q.Inputs, c.Inputs) {
			return errors.New("invalid pending request")
		}
	}
	if strings.TrimSpace(c.InputDigest) == "" {
		return errors.New("invalid cursor digest")
	}
	return nil
}
