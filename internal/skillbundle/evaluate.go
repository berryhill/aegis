package skillbundle

import (
	"fmt"
	"path/filepath"
)

var requiredEvaluationClasses = []string{
	"happy_path",
	"authority_denial",
	"malformed_input",
	"stale_replay",
	"interruption_recovery",
	"secret_canary",
}

// Evaluate validates fixture structure and declared safety expectations only.
// It does not execute requests, services, runtimes, or agent behavior.
func Evaluate(root string, manifest Manifest) (EvaluationResult, error) {
	data, err := readBounded(filepath.Join(root, EvaluationsName), MaxEvaluationBytes)
	if err != nil {
		return EvaluationResult{}, err
	}
	var suite EvaluationSuite
	if err := decodeStrictJSON(data, &suite); err != nil {
		return EvaluationResult{}, deny("evaluations_malformed", EvaluationsName, err.Error())
	}
	if suite.SchemaVersion != EvaluationSchema {
		return EvaluationResult{}, deny("unsupported_evaluation_schema", EvaluationsName, fmt.Sprintf("schema_version must be %d", EvaluationSchema))
	}
	knownOperations := make(map[string]string, len(manifest.Operations))
	for _, operation := range manifest.Operations {
		knownOperations[operation.Operation] = operation.Availability
	}
	classes := map[string]bool{}
	ids := map[string]bool{}
	for index, evaluation := range suite.Cases {
		location := fmt.Sprintf("%s#cases[%d]", EvaluationsName, index)
		if !slugPattern.MatchString(evaluation.ID) || ids[evaluation.ID] {
			return EvaluationResult{}, deny("invalid_evaluation_id", location, evaluation.ID)
		}
		ids[evaluation.ID] = true
		validClass := false
		for _, class := range requiredEvaluationClasses {
			if evaluation.Class == class {
				validClass = true
				break
			}
		}
		if !validClass {
			return EvaluationResult{}, deny("invalid_evaluation_class", location, evaluation.Class)
		}
		classes[evaluation.Class] = true
		if evaluation.Prompt == "" || evaluation.ExpectedSignal == "" {
			return EvaluationResult{}, deny("incomplete_evaluation", location, "prompt and expected_signal are required")
		}
		switch evaluation.Expected {
		case "route":
			availability, known := knownOperations[evaluation.Operation]
			if !known {
				return EvaluationResult{}, deny("orphan_evaluation_operation", location, evaluation.Operation)
			}
			if availability != "shipped" {
				return EvaluationResult{}, deny("unavailable_evaluation_operation", location, evaluation.Operation)
			}
		case "deny":
			if evaluation.Operation != "" {
				return EvaluationResult{}, deny("denial_routes_operation", location, "denial cases must not route an operation")
			}
		default:
			return EvaluationResult{}, deny("invalid_evaluation_expected", location, evaluation.Expected)
		}
		if err := scanEvaluation(location, evaluation); err != nil {
			return EvaluationResult{}, err
		}
	}
	for _, class := range requiredEvaluationClasses {
		if !classes[class] {
			return EvaluationResult{}, deny("evaluation_coverage_missing", EvaluationsName, class)
		}
	}
	return EvaluationResult{
		Status:              "valid",
		EvidenceClass:       "structural_fixture_validation",
		Cases:               len(suite.Cases),
		StructurallyValid:   len(suite.Cases),
		BehavioralExecution: "not_run",
		ServiceExecution:    "not_run",
		RuntimeExecution:    "not_run",
	}, nil
}

func scanEvaluation(location string, evaluation EvaluationCase) error {
	for _, pattern := range positiveAuthorityPatterns {
		if pattern.MatchString(evaluation.ExpectedSignal) {
			return deny("evaluation_authority_claim", location, "expected signal claims prompt authority")
		}
	}
	if evaluation.Class == "secret_canary" && evaluation.Expected != "deny" {
		return deny("secret_canary_not_denied", location, "secret canary evaluations must deny")
	}
	if evaluation.Class != "happy_path" && evaluation.Expected != "deny" {
		return deny("unsafe_evaluation_outcome", location, "non-happy-path evaluations must deny")
	}
	return nil
}
