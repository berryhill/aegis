package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
)

const loopComposerBytesMax = 64 << 10

type loopComposerForm struct {
	CSRF, PublisherID, PublicationKey string
	Revision                          app.LoopCandidate
}

type loopLifecycleForm struct {
	CSRF, TargetID, ExpectedDigest, PublisherID, ExpectedPreviousDigest, IdempotencyKey string
	State                                                                               app.LoopLifecycleState
}

func decodeLoopLifecycleForm(request *http.Request) (loopLifecycleForm, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > 8192 {
		return loopLifecycleForm{}, errors.New("invalid Loop lifecycle form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 8193))
	if err != nil || len(body) > 8192 {
		return loopLifecycleForm{}, errors.New("invalid Loop lifecycle form")
	}
	values, err := url.ParseQuery(string(body))
	keys := []string{"csrf", "target_id", "expected_digest", "publisher_id", "state", "expected_previous_digest", "idempotency_key"}
	if err != nil || len(values) != len(keys) {
		return loopLifecycleForm{}, errors.New("invalid Loop lifecycle form")
	}
	for _, key := range keys {
		if len(values[key]) != 1 {
			return loopLifecycleForm{}, errors.New("invalid Loop lifecycle form")
		}
	}
	form := loopLifecycleForm{CSRF: values.Get("csrf"), TargetID: values.Get("target_id"), ExpectedDigest: values.Get("expected_digest"), PublisherID: values.Get("publisher_id"), State: app.LoopLifecycleState(values.Get("state")), ExpectedPreviousDigest: values.Get("expected_previous_digest"), IdempotencyKey: values.Get("idempotency_key")}
	if form.CSRF == "" || form.TargetID == "" || form.ExpectedDigest == "" || form.PublisherID == "" || form.IdempotencyKey == "" || form.State != app.LoopLifecycleActive && form.State != app.LoopLifecycleRetired {
		return loopLifecycleForm{}, errors.New("invalid Loop lifecycle form")
	}
	return form, nil
}

func decodeLoopExecuteForm(request *http.Request) (csrf, intentID string, err error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > 4096 {
		return "", "", errors.New("invalid Loop confirmation form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 4097))
	if err != nil || len(body) > 4096 {
		return "", "", errors.New("invalid Loop confirmation form")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) != 2 || len(values["csrf"]) != 1 || len(values["intent_id"]) != 1 || values.Get("csrf") == "" || values.Get("intent_id") == "" {
		return "", "", errors.New("invalid Loop confirmation form")
	}
	return values.Get("csrf"), values.Get("intent_id"), nil
}

func decodeLoopComposerForm(request *http.Request) (loopComposerForm, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > loopComposerBytesMax {
		return loopComposerForm{}, errors.New("invalid Loop composer form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, loopComposerBytesMax+1))
	if err != nil || len(body) > loopComposerBytesMax {
		return loopComposerForm{}, errors.New("invalid Loop composer form")
	}
	values, err := url.ParseQuery(string(body))
	allowed := []string{"csrf", "publisher_id", "publication_key", "loop_id", "revision", "previous_digest", "entry_step_id", "inputs", "outputs", "steps", "step_ports", "terminal_mappings", "evidence_claims", "transitions", "transition_mappings", "required_evidence"}
	if err != nil || len(values) != len(allowed) {
		return loopComposerForm{}, errors.New("invalid Loop composer form")
	}
	for _, key := range allowed {
		if len(values[key]) != 1 {
			return loopComposerForm{}, fmt.Errorf("invalid Loop composer field %s", key)
		}
	}
	revisionNumber, err := strconv.ParseUint(values.Get("revision"), 10, 64)
	if err != nil || revisionNumber == 0 {
		return loopComposerForm{}, errors.New("revision must be a positive integer")
	}
	inputs, err := parsePorts(values.Get("inputs"), app.LoopMaxPorts)
	if err != nil {
		return loopComposerForm{}, fmt.Errorf("inputs: %w", err)
	}
	outputs, err := parsePorts(values.Get("outputs"), app.LoopMaxPorts)
	if err != nil {
		return loopComposerForm{}, fmt.Errorf("outputs: %w", err)
	}
	steps, err := parseSteps(values.Get("steps"))
	if err != nil {
		return loopComposerForm{}, fmt.Errorf("steps: %w", err)
	}
	stepIndex := make(map[string]int, len(steps))
	for index := range steps {
		stepIndex[steps[index].ID] = index
	}
	if err = applyStepPorts(values.Get("step_ports"), steps, stepIndex); err != nil {
		return loopComposerForm{}, fmt.Errorf("step ports: %w", err)
	}
	if err = applyTerminalMappings(values.Get("terminal_mappings"), steps, stepIndex); err != nil {
		return loopComposerForm{}, fmt.Errorf("terminal mappings: %w", err)
	}
	if err = applyEvidenceClaims(values.Get("evidence_claims"), steps, stepIndex); err != nil {
		return loopComposerForm{}, fmt.Errorf("evidence claims: %w", err)
	}
	transitions, transitionIndex, err := parseTransitions(values.Get("transitions"))
	if err != nil {
		return loopComposerForm{}, fmt.Errorf("transitions: %w", err)
	}
	if err = applyTransitionMappings(values.Get("transition_mappings"), transitions, transitionIndex); err != nil {
		return loopComposerForm{}, fmt.Errorf("transition mappings: %w", err)
	}
	required, err := parseRequiredEvidence(values.Get("required_evidence"))
	if err != nil {
		return loopComposerForm{}, fmt.Errorf("required evidence: %w", err)
	}
	form := loopComposerForm{CSRF: values.Get("csrf"), PublisherID: values.Get("publisher_id"), PublicationKey: values.Get("publication_key")}
	form.Revision = app.LoopCandidate{LoopID: values.Get("loop_id"), Revision: revisionNumber, PreviousDigest: values.Get("previous_digest"), Inputs: inputs, Outputs: outputs, EntryStepID: values.Get("entry_step_id"), Steps: steps, Transitions: transitions, RequiredEvidence: required}
	if form.CSRF == "" || form.PublisherID == "" || form.PublicationKey == "" {
		return loopComposerForm{}, errors.New("CSRF, publisher, and publication key are required")
	}
	return form, nil
}

func csvRows(raw string, columns, maximum int) ([][]string, error) {
	if strings.TrimSpace(raw) == "" {
		return [][]string{}, nil
	}
	reader := csv.NewReader(strings.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = columns
	rows := [][]string{}
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || len(rows) >= maximum {
			return nil, errors.New("malformed or excessive CSV rows")
		}
		for index := range row {
			row[index] = strings.TrimSpace(row[index])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseBool(raw string) (bool, error) {
	if raw == "true" {
		return true, nil
	}
	if raw == "false" {
		return false, nil
	}
	return false, errors.New("required must be true or false")
}

func parsePorts(raw string, maximum int) ([]app.LoopPort, error) {
	rows, err := csvRows(raw, 3, maximum)
	if err != nil {
		return nil, err
	}
	ports := make([]app.LoopPort, 0, len(rows))
	for _, row := range rows {
		required, err := parseBool(row[2])
		if err != nil {
			return nil, err
		}
		ports = append(ports, app.LoopPort{ID: row[0], Type: app.LoopValueType(row[1]), Required: required})
	}
	return ports, nil
}

func parseSteps(raw string) ([]app.LoopStep, error) {
	rows, err := csvRows(raw, 5, app.LoopMaxSteps)
	if err != nil || len(rows) == 0 {
		return nil, errors.New("at least one id,kind,max_attempts,gate_mode,terminal_outcome row is required")
	}
	steps := make([]app.LoopStep, 0, len(rows))
	for _, row := range rows {
		attempts, err := strconv.ParseUint(row[2], 10, 16)
		if err != nil {
			return nil, errors.New("max_attempts must be an integer")
		}
		step := app.LoopStep{ID: row[0], Kind: app.LoopStepKind(row[1]), Retry: app.LoopRetryPolicy{MaxAttempts: uint16(attempts)}, InputPorts: []app.LoopPort{}, OutputPorts: []app.LoopPort{}, EvidenceClaims: []app.LoopEvidenceClaim{}}
		if step.Kind == app.LoopStepGate {
			step.Gate = &app.LoopGateDefinition{Mode: row[3]}
		} else if row[3] != "" {
			return nil, errors.New("gate mode is allowed only for gate steps")
		}
		if step.Kind == app.LoopStepTerminal {
			step.Terminal = &app.LoopTerminalDefinition{Outcome: app.LoopTerminalOutcome(row[4]), OutputMappings: []app.LoopPortMapping{}}
		} else if row[4] != "" {
			return nil, errors.New("terminal outcome is allowed only for terminal steps")
		}
		steps = append(steps, step)
	}
	return steps, nil
}

func applyStepPorts(raw string, steps []app.LoopStep, indexes map[string]int) error {
	rows, err := csvRows(raw, 5, app.LoopMaxSteps*2)
	if err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indexes[row[0]]
		if !ok {
			return errors.New("unknown step ID")
		}
		required, err := parseBool(row[4])
		if err != nil {
			return err
		}
		port := app.LoopPort{ID: row[2], Type: app.LoopValueType(row[3]), Required: required}
		switch row[1] {
		case "input":
			steps[index].InputPorts = append(steps[index].InputPorts, port)
		case "output":
			steps[index].OutputPorts = append(steps[index].OutputPorts, port)
		default:
			return errors.New("direction must be input or output")
		}
	}
	return nil
}

func applyTerminalMappings(raw string, steps []app.LoopStep, indexes map[string]int) error {
	rows, err := csvRows(raw, 3, app.LoopMaxPorts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indexes[row[0]]
		if !ok || steps[index].Terminal == nil {
			return errors.New("unknown or non-terminal step ID")
		}
		steps[index].Terminal.OutputMappings = append(steps[index].Terminal.OutputMappings, app.LoopPortMapping{SourcePort: row[1], TargetPort: row[2]})
	}
	return nil
}

func applyEvidenceClaims(raw string, steps []app.LoopStep, indexes map[string]int) error {
	rows, err := csvRows(raw, 6, app.LoopMaxEvidence)
	if err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indexes[row[0]]
		if !ok {
			return errors.New("unknown evidence producer step")
		}
		steps[index].EvidenceClaims = append(steps[index].EvidenceClaims, app.LoopEvidenceClaim{Claim: row[1], MediaType: row[2], ExpectedDigest: row[3], VerifierID: row[4], PolicyVersion: row[5]})
	}
	return nil
}

func parseTransitions(raw string) ([]app.LoopTransition, map[string]int, error) {
	rows, err := csvRows(raw, 5, app.LoopMaxTransitions)
	if err != nil {
		return nil, nil, err
	}
	values := make([]app.LoopTransition, 0, len(rows))
	indexes := map[string]int{}
	for _, row := range rows {
		traversals, err := strconv.ParseUint(row[4], 10, 16)
		if err != nil {
			return nil, nil, errors.New("max_traversals must be an integer")
		}
		indexes[row[0]] = len(values)
		values = append(values, app.LoopTransition{ID: row[0], FromStepID: row[1], ToStepID: row[2], Condition: row[3], MaxTraversals: uint16(traversals), Mappings: []app.LoopPortMapping{}})
	}
	return values, indexes, nil
}

func applyTransitionMappings(raw string, transitions []app.LoopTransition, indexes map[string]int) error {
	rows, err := csvRows(raw, 3, app.LoopMaxTransitions)
	if err != nil {
		return err
	}
	for _, row := range rows {
		index, ok := indexes[row[0]]
		if !ok {
			return errors.New("unknown transition ID")
		}
		transitions[index].Mappings = append(transitions[index].Mappings, app.LoopPortMapping{SourcePort: row[1], TargetPort: row[2]})
	}
	return nil
}

func parseRequiredEvidence(raw string) ([]app.LoopEvidenceRequirement, error) {
	rows, err := csvRows(raw, 2, app.LoopMaxEvidence)
	if err != nil {
		return nil, err
	}
	values := make([]app.LoopEvidenceRequirement, 0, len(rows))
	for _, row := range rows {
		values = append(values, app.LoopEvidenceRequirement{Claim: row[0], ProducerStepID: row[1]})
	}
	return values, nil
}
