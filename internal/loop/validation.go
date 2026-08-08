package loop

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)

const digestPrefixLength = len("sha256:") + 64

func ValidateRevision(revision LoopRevision) LoopValidationResult {
	issues := validateRevision(revision, true)
	result := LoopValidationResult{
		SchemaVersion:  ValidationSchemaVersion,
		LoopID:         revision.LoopID,
		Revision:       revision.Revision,
		RevisionDigest: revision.Digest,
		Validator:      revision.Validator,
		Outcome:        ValidationValid,
		Issues:         issues,
	}
	if len(issues) != 0 {
		result.Outcome = ValidationInvalid
	}
	digest, err := digestLoopValidationResult(result)
	if err == nil {
		result.Digest = digest
	}
	return result
}

func validateRevision(revision LoopRevision, verifyDigest bool) []ValidationIssue {
	var issues []ValidationIssue
	add := func(code, path, message string) {
		issues = append(issues, ValidationIssue{Code: code, Path: path, Message: message})
	}
	if revision.SchemaVersion != RevisionSchemaVersion {
		add("schema.unsupported", "schema_version", "unsupported Loop revision schema version")
	}
	if !validID(revision.LoopID) {
		add("loop_id.invalid", "loop_id", "Loop ID is required and must be a bounded identifier")
	}
	if revision.Revision == 0 {
		add("revision.invalid", "revision", "revision must be positive")
	}
	if revision.Revision == 1 && revision.PreviousDigest != "" {
		add("revision.previous_unexpected", "previous_digest", "first revision cannot have a previous digest")
	}
	if revision.Revision > 1 && !validDigest(revision.PreviousDigest) {
		add("revision.previous_missing", "previous_digest", "later revisions require an exact previous digest")
	}
	if revision.Validator.ID != ValidatorID || revision.Validator.Version != ValidatorVersion {
		add("validator.unsupported", "validator", "validator identity and version must match this implementation")
	}
	if len(revision.Inputs) > MaxPorts || len(revision.Outputs) > MaxPorts {
		add("complexity.ports", "inputs", "Loop port count exceeds the bounded limit")
	}
	validatePorts(revision.Inputs, "inputs", add)
	validatePorts(revision.Outputs, "outputs", add)
	if len(revision.Steps) == 0 || len(revision.Steps) > MaxSteps {
		add("complexity.steps", "steps", "Loop must contain a bounded non-empty step set")
	}
	if len(revision.Transitions) > MaxTransitions {
		add("complexity.transitions", "transitions", "transition count exceeds the bounded limit")
	}
	if len(revision.RequiredEvidence) > MaxEvidence {
		add("complexity.evidence", "required_evidence", "evidence requirement count exceeds the bounded limit")
	}

	steps := make(map[string]Step, len(revision.Steps))
	for index, step := range revision.Steps {
		path := fmt.Sprintf("steps[%d]", index)
		if !validID(step.ID) {
			add("step.id_invalid", path+".id", "step ID is malformed")
		} else if _, exists := steps[step.ID]; exists {
			add("step.duplicate", path+".id", "step ID is duplicated")
		} else {
			steps[step.ID] = step
		}
		validateStep(step, path, add)
	}
	entry, entryExists := steps[revision.EntryStepID]
	if !entryExists || !validID(revision.EntryStepID) {
		add("entry.invalid", "entry_step_id", "entry step must identify exactly one existing step")
	} else if entry.Kind == StepTerminal {
		add("entry.terminal", "entry_step_id", "entry step cannot be terminal")
	} else {
		validateBoundaryCompatibility(revision.Inputs, entry.InputPorts, "entry", add)
	}

	incoming := make(map[string][]Transition)
	outgoing := make(map[string][]Transition)
	transitionIDs := make(map[string]struct{}, len(revision.Transitions))
	for index, transition := range revision.Transitions {
		path := fmt.Sprintf("transitions[%d]", index)
		if !validID(transition.ID) {
			add("transition.id_invalid", path+".id", "transition ID is malformed")
		} else if _, exists := transitionIDs[transition.ID]; exists {
			add("transition.duplicate", path+".id", "transition ID is duplicated")
		} else {
			transitionIDs[transition.ID] = struct{}{}
		}
		from, fromExists := steps[transition.FromStepID]
		to, toExists := steps[transition.ToStepID]
		if !fromExists {
			add("transition.source_missing", path+".from_step_id", "transition source does not exist")
		}
		if !toExists {
			add("transition.target_missing", path+".to_step_id", "transition target does not exist")
		}
		if fromExists && from.Kind == StepTerminal {
			add("terminal.transition", path+".from_step_id", "terminal steps cannot have outgoing transitions")
		}
		if transition.MaxTraversals > MaxTraversals {
			add("cycle.bound_exceeded", path+".max_traversals", "transition traversal bound exceeds the maximum")
		}
		if fromExists && toExists {
			validateMappings(from.OutputPorts, to.InputPorts, transition.Mappings, path+".mappings", true, add)
			outgoing[from.ID] = append(outgoing[from.ID], transition)
			incoming[to.ID] = append(incoming[to.ID], transition)
		}
	}

	terminalCount := 0
	for _, step := range revision.Steps {
		outs := outgoing[step.ID]
		switch step.Kind {
		case StepTerminal:
			terminalCount++
			if len(outs) != 0 {
				add("terminal.transition", "steps."+step.ID, "terminal steps cannot transition")
			}
			if step.Terminal != nil {
				validateMappings(step.OutputPorts, revision.Outputs, step.Terminal.OutputMappings, "steps."+step.ID+".terminal.output_mappings", false, add)
			}
		case StepGate:
			if len(outs) < 2 {
				add("branch.insufficient", "steps."+step.ID, "an exclusive gate requires at least two branches")
			}
			seenConditions := map[string]struct{}{}
			for _, transition := range outs {
				condition := strings.TrimSpace(transition.Condition)
				if condition == "" {
					add("branch.condition_missing", "transitions."+transition.ID+".condition", "gate branches require an explicit condition label")
				} else if _, duplicate := seenConditions[condition]; duplicate {
					add("branch.ambiguous", "transitions."+transition.ID+".condition", "gate branch conditions must be unique")
				}
				seenConditions[condition] = struct{}{}
			}
		default:
			if len(outs) != 1 {
				add("flow.outgoing", "steps."+step.ID, "non-terminal non-gate steps require exactly one outgoing transition")
			}
		}
		if step.Kind == StepJoin && len(incoming[step.ID]) < 2 {
			add("join.insufficient", "steps."+step.ID, "join steps require at least two incoming transitions")
		}
	}
	if terminalCount == 0 {
		add("terminal.missing", "steps", "Loop requires at least one terminal step")
	}

	if entryExists {
		reachable := reachableFrom(revision.EntryStepID, outgoing)
		for _, step := range revision.Steps {
			if !reachable[step.ID] {
				add("step.unreachable", "steps."+step.ID, "step is unreachable from the entry")
			}
		}
		for _, transition := range revision.Transitions {
			if steps[transition.FromStepID].ID != "" && steps[transition.ToStepID].ID != "" &&
				reachablePath(transition.ToStepID, transition.FromStepID, outgoing, map[string]bool{}) && transition.MaxTraversals == 0 {
				add("cycle.unbounded", "transitions."+transition.ID+".max_traversals", "every transition participating in a cycle requires a positive traversal bound")
			}
		}
	}
	validateEvidence(revision, steps, add)

	if verifyDigest {
		if !validDigest(revision.Digest) {
			add("digest.invalid", "digest", "revision digest must be lowercase sha256:<64-hex>")
		} else if digest, err := digestRevision(revision); err != nil || digest != revision.Digest {
			add("digest.mismatch", "digest", "revision digest does not match canonical content")
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
	return issues
}

func validateStep(step Step, path string, add func(string, string, string)) {
	validatePorts(step.InputPorts, path+".input_ports", add)
	validatePorts(step.OutputPorts, path+".output_ports", add)
	if len(step.InputPorts) > MaxPorts || len(step.OutputPorts) > MaxPorts {
		add("complexity.step_ports", path, "step port count exceeds the bounded limit")
	}
	if step.Retry.MaxAttempts == 0 || step.Retry.MaxAttempts > MaxAttempts {
		add("retry.unbounded", path+".retry.max_attempts", "retry attempts must be within the bounded range")
	}
	switch step.Kind {
	case StepAction, StepJoin:
		if step.Gate != nil || step.Terminal != nil {
			add("step.shape", path, "action and join steps cannot contain gate or terminal definitions")
		}
	case StepGate:
		if step.Gate == nil || step.Gate.Mode != "exclusive" || step.Terminal != nil {
			add("gate.invalid", path+".gate", "gate step requires the exclusive gate definition only")
		}
	case StepTerminal:
		if step.Terminal == nil || step.Gate != nil {
			add("terminal.invalid", path+".terminal", "terminal step requires a terminal definition only")
		} else if step.Terminal.Outcome != OutcomeSucceeded && step.Terminal.Outcome != OutcomeFailed {
			add("terminal.outcome", path+".terminal.outcome", "terminal outcome is unsupported")
		}
		if step.Retry.MaxAttempts != 1 {
			add("terminal.retry", path+".retry.max_attempts", "terminal steps cannot retry")
		}
	default:
		add("step.kind", path+".kind", "step kind is unsupported")
	}
	claims := map[string]struct{}{}
	for index, claim := range step.EvidenceClaims {
		claimPath := fmt.Sprintf("%s.evidence_claims[%d]", path, index)
		if !validID(claim.Claim) || strings.TrimSpace(claim.MediaType) == "" || len(claim.MediaType) > 255 {
			add("evidence.claim_invalid", claimPath, "evidence claim and media type must be bounded")
		}
		if _, duplicate := claims[claim.Claim]; duplicate {
			add("evidence.claim_duplicate", claimPath+".claim", "evidence claim is duplicated on the step")
		}
		claims[claim.Claim] = struct{}{}
	}
}

func validatePorts(ports []Port, path string, add func(string, string, string)) {
	seen := map[string]struct{}{}
	for index, port := range ports {
		portPath := fmt.Sprintf("%s[%d]", path, index)
		if !validID(port.ID) {
			add("port.id_invalid", portPath+".id", "port ID is malformed")
		} else if _, duplicate := seen[port.ID]; duplicate {
			add("port.duplicate", portPath+".id", "port ID is duplicated")
		}
		seen[port.ID] = struct{}{}
		if !validValueType(port.Type) {
			add("port.type_invalid", portPath+".type", "port type is unsupported")
		}
	}
}

func validateBoundaryCompatibility(from, to []Port, path string, add func(string, string, string)) {
	mappings := make([]PortMapping, 0, len(from))
	for _, port := range from {
		mappings = append(mappings, PortMapping{SourcePort: port.ID, TargetPort: port.ID})
	}
	validateMappings(from, to, mappings, path, true, add)
}

func validateMappings(from, to []Port, mappings []PortMapping, path string, requireInputs bool, add func(string, string, string)) {
	sources := portsByID(from)
	targets := portsByID(to)
	mappedTargets := map[string]struct{}{}
	for index, mapping := range mappings {
		mappingPath := fmt.Sprintf("%s[%d]", path, index)
		source, sourceExists := sources[mapping.SourcePort]
		target, targetExists := targets[mapping.TargetPort]
		if !sourceExists {
			add("mapping.source_missing", mappingPath+".source_port", "mapped source port does not exist")
		}
		if !targetExists {
			add("mapping.target_missing", mappingPath+".target_port", "mapped target port does not exist")
		}
		if _, duplicate := mappedTargets[mapping.TargetPort]; duplicate {
			add("mapping.target_duplicate", mappingPath+".target_port", "target port is mapped more than once")
		}
		mappedTargets[mapping.TargetPort] = struct{}{}
		if sourceExists && targetExists && source.Type != target.Type {
			add("mapping.type_mismatch", mappingPath, "mapped port types are incompatible")
		}
	}
	if requireInputs {
		for _, target := range to {
			if target.Required {
				if _, mapped := mappedTargets[target.ID]; !mapped {
					add("mapping.required_missing", path, "required target port "+target.ID+" is not mapped")
				}
			}
		}
	} else {
		for _, target := range to {
			if _, mapped := mappedTargets[target.ID]; !mapped {
				add("output.unsatisfied", path, "Loop output "+target.ID+" is not produced by the terminal")
			}
		}
	}
}

func validateEvidence(revision LoopRevision, steps map[string]Step, add func(string, string, string)) {
	seen := map[string]struct{}{}
	for index, requirement := range revision.RequiredEvidence {
		path := fmt.Sprintf("required_evidence[%d]", index)
		if !validID(requirement.Claim) {
			add("evidence.requirement_invalid", path+".claim", "evidence requirement claim is malformed")
		}
		if _, duplicate := seen[requirement.Claim]; duplicate {
			add("evidence.requirement_duplicate", path+".claim", "evidence requirement is duplicated")
		}
		seen[requirement.Claim] = struct{}{}
		producer, exists := steps[requirement.ProducerStepID]
		if !exists || producer.Kind == StepTerminal {
			add("evidence.producer_invalid", path+".producer_step_id", "evidence producer must be an existing non-terminal step")
			continue
		}
		produced := false
		for _, claim := range producer.EvidenceClaims {
			produced = produced || claim.Claim == requirement.Claim
		}
		if !produced {
			add("evidence.unsatisfied", path, "required evidence claim is not declared by its producer")
		}
	}
}

func reachableFrom(entry string, outgoing map[string][]Transition) map[string]bool {
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, transition := range outgoing[id] {
			visit(transition.ToStepID)
		}
	}
	visit(entry)
	return reachable
}

func reachablePath(current, target string, outgoing map[string][]Transition, visited map[string]bool) bool {
	if current == target {
		return true
	}
	if visited[current] {
		return false
	}
	visited[current] = true
	for _, transition := range outgoing[current] {
		if reachablePath(transition.ToStepID, target, outgoing, visited) {
			return true
		}
	}
	return false
}

func portsByID(ports []Port) map[string]Port {
	result := make(map[string]Port, len(ports))
	for _, port := range ports {
		result[port.ID] = port
	}
	return result
}

func validID(value string) bool {
	return identifierPattern.MatchString(value) && strings.TrimSpace(value) == value
}
func validDigest(value string) bool {
	if len(value) != digestPrefixLength || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
func validValueType(value ValueType) bool {
	switch value {
	case TypeString, TypeBoolean, TypeInteger, TypeNumber, TypeObject, TypeArray, TypeArtifact:
		return true
	default:
		return false
	}
}
