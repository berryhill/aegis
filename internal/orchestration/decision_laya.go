package orchestration

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const layaDecisionInputLimit = 64 << 10
const layaDecisionOutputLimit = 8 << 10
const layaDecisionTimeout = 90 * time.Second

//go:embed laya_helper.py
var layaHelperSource string

// LayaDecisionProcess is a proposal-only local process seam; it does not admit,
// authenticate, or authorize work. Inject an inert fake for controller tests.
type LayaDecisionProcess interface {
	Run(context.Context, []byte) ([]byte, error)
}

// LayaDecisionAdapter produces typed, non-authoritative gate/judgment proposals.
// A caller MUST separately enforce Aegis admission, independent verification,
// and durable state transitions. An error always means fail closed, never retry
// as an implicit yes.
type LayaDecisionAdapter struct{ process LayaDecisionProcess }

func NewLayaDecisionAdapter(process LayaDecisionProcess) *LayaDecisionAdapter {
	return &LayaDecisionAdapter{process: process}
}

type LayaGate struct {
	Specified     bool
	ResultDefined bool
}
type LayaVerdict struct {
	Done         bool
	StaysInScope bool
	Fulfills     bool
	Works        bool
	Practices    bool
}

var layaQuestions = map[string]map[string]string{
	"gate": {
		"specified":      "Does the user request one concrete actionable outcome, rather than only discuss an idea?",
		"result_defined": "Does the request identify an observable result or artifact that could count as success?",
	},
	"verdict": {
		"done":           "Does the report describe a completed result rather than merely an intention or partial work?",
		"stays_in_scope": "Does the reported work stay within the original request, with no unrelated changes?",
		"fulfills":       "Does the reported result address the requested outcome?",
		"works":          "Does the report include specific observed checks or evidence that the result works?",
		"practices":      "Does the report show reasonable task-appropriate practices without demanding perfection?",
	},
}

type layaRequest struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
}
type layaAnswer struct {
	Choice     string  `json:"choice"`
	Confidence float64 `json:"answer_confidence"`
}
type layaResponse struct {
	Version int                   `json:"version"`
	Kind    string                `json:"kind"`
	Answers map[string]layaAnswer `json:"answers"`
}

func (d *LayaDecisionAdapter) Gate(ctx context.Context, task string) (LayaGate, error) {
	answers, err := d.ask(ctx, "gate", task)
	if err != nil {
		return LayaGate{}, err
	}
	return LayaGate{Specified: answers["specified"], ResultDefined: answers["result_defined"]}, nil
}
func (d *LayaDecisionAdapter) Verdict(ctx context.Context, task, report string) (LayaVerdict, error) {
	if strings.TrimSpace(task) == "" || strings.TrimSpace(report) == "" {
		return LayaVerdict{}, errors.New("Laya requires task and report")
	}
	state, err := json.Marshal(struct {
		Request           string `json:"request"`
		ImplementerReport string `json:"implementer_report"`
	}{task, report})
	if err != nil {
		return LayaVerdict{}, err
	}
	answers, err := d.ask(ctx, "verdict", string(state))
	if err != nil {
		return LayaVerdict{}, err
	}
	return LayaVerdict{Done: answers["done"], StaysInScope: answers["stays_in_scope"], Fulfills: answers["fulfills"], Works: answers["works"], Practices: answers["practices"]}, nil
}
func (d *LayaDecisionAdapter) ask(ctx context.Context, kind, state string) (map[string]bool, error) {
	if d == nil || d.process == nil {
		return nil, errors.New("Laya process unavailable")
	}
	if strings.TrimSpace(state) == "" || len(state) > layaDecisionInputLimit {
		return nil, errors.New("Laya state missing or too large")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request, _ := json.Marshal(layaRequest{Version: 1, Kind: kind, State: state})
	if len(request) > layaDecisionInputLimit {
		return nil, errors.New("Laya encoded request exceeds bound")
	}
	ctx, cancel := context.WithTimeout(ctx, layaDecisionTimeout)
	defer cancel()
	output, err := d.process.Run(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("Laya unavailable: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(output) == 0 || len(output) > layaDecisionOutputLimit {
		return nil, errors.New("Laya response missing or too large")
	}
	if err := rejectDuplicateJSONKeys(output); err != nil {
		return nil, fmt.Errorf("Laya response invalid: %w", err)
	}
	var response layaResponse
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("Laya response invalid: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("Laya response has trailing data")
	}
	questions := layaQuestions[kind]
	if err := strictLayaFields(output, questions); err != nil {
		return nil, err
	}
	if response.Version != 1 || response.Kind != kind || len(response.Answers) != len(questions) {
		return nil, errors.New("Laya response schema mismatch")
	}
	result := make(map[string]bool, len(questions))
	for label := range questions {
		answer, ok := response.Answers[label]
		if !ok || (answer.Choice != "yes" && answer.Choice != "no") || !(answer.Confidence >= 0.6 && answer.Confidence <= 1) {
			return nil, errors.New("Laya answer ambiguous or low confidence")
		}
		result[label] = answer.Choice == "yes"
	}
	return result, nil
}

// encoding/json accepts case-insensitive struct aliases. Require exact wire
// names so Version/version and Choice/choice cannot override one another.
func strictLayaFields(output []byte, questions map[string]string) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(output, &top); err != nil || len(top) != 3 {
		return errors.New("Laya response fields invalid")
	}
	for _, name := range []string{"version", "kind", "answers"} {
		if _, ok := top[name]; !ok {
			return errors.New("Laya response fields invalid")
		}
	}
	var answers map[string]json.RawMessage
	if err := json.Unmarshal(top["answers"], &answers); err != nil || len(answers) != len(questions) {
		return errors.New("Laya answer fields invalid")
	}
	for label := range questions {
		wire, ok := answers[label]
		if !ok {
			return errors.New("Laya answer fields invalid")
		}
		var answer map[string]json.RawMessage
		if err := json.Unmarshal(wire, &answer); err != nil || len(answer) != 2 {
			return errors.New("Laya answer fields invalid")
		}
		if _, ok := answer["choice"]; !ok {
			return errors.New("Laya answer fields invalid")
		}
		if _, ok := answer["answer_confidence"]; !ok {
			return errors.New("Laya answer fields invalid")
		}
	}
	return nil
}

// rejectDuplicateJSONKeys rejects ambiguous object keys at every nesting depth;
// DisallowUnknownFields alone would accept duplicate version/choice overrides.
func rejectDuplicateJSONKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for d.More() {
				keyTok, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok {
					return errors.New("non-string JSON key")
				}
				if _, exists := seen[key]; exists {
					return errors.New("duplicate JSON key")
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

// LocalLayaProcess executes only the embedded fixed helper through one
// controller-selected absolute Python executable. No shell, URL, user command,
// environment-supplied model endpoint, or credential is part of this protocol.
type LocalLayaProcess struct {
	PythonExecutable string
	Home             string
}

func (p LocalLayaProcess) Run(ctx context.Context, input []byte) ([]byte, error) {
	if !filepath.IsAbs(p.PythonExecutable) || !filepath.IsAbs(p.Home) || filepath.Clean(p.Home) != p.Home || p.Home == "/" || len(input) > layaDecisionInputLimit+1024 {
		return nil, errors.New("invalid local Laya executable or input")
	}
	info, err := os.Lstat(p.Home)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("local Laya home must be an existing private directory")
	}
	cmd := exec.CommandContext(ctx, p.PythonExecutable, "-I", "-c", layaHelperSource)
	// Do not project the controller's ambient credentials into a local model
	// subprocess. A separately configured private cache is the only home state.
	cmd.Env = []string{"HOME=" + p.Home, "HF_HOME=" + filepath.Join(p.Home, ".cache", "huggingface"), "PYTHONNOUSERSITE=1", "HF_HUB_OFFLINE=1", "TRANSFORMERS_OFFLINE=1"}
	cmd.Dir = p.Home
	cmd.Stdin = bytes.NewReader(input)
	var stdout limitedLayaWriter
	cmd.Stdout = &stdout
	// Never return stderr: Python/model errors may include untrusted text.
	if err := cmd.Run(); err != nil {
		return nil, errors.New("local Laya helper failed")
	}
	if stdout.exceeded {
		return nil, errors.New("local Laya output too large")
	}
	return stdout.Bytes(), nil
}

type limitedLayaWriter struct {
	bytes.Buffer
	exceeded bool
}

func (w *limitedLayaWriter) Write(p []byte) (int, error) {
	if w.Len()+len(p) > layaDecisionOutputLimit {
		w.exceeded = true
		return 0, errors.New("Laya output limit")
	}
	return w.Buffer.Write(p)
}
