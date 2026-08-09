package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,254}$`)

const digestLength = len("sha256:") + 64

// ValidateRevision validates a sealed Graph revision and returns an immutable,
// content-addressed result for that exact revision.
func ValidateRevision(revision GraphRevision) GraphValidationResult {
	issues := validateRevision(revision, true)
	return makeValidationResult(revision, issues)
}

func validateRevision(revision GraphRevision, verifyDigest bool) []ValidationIssue {
	var issues []ValidationIssue
	add := func(code, path, message string) {
		issues = append(issues, ValidationIssue{Code: code, Path: path, Message: message})
	}
	if revision.SchemaVersion != RevisionSchemaVersion {
		add("schema.unsupported", "schema_version", "unsupported Graph revision schema version")
	}
	if !validID(revision.GraphID) {
		add("graph_id.invalid", "graph_id", "Graph ID is required and must be a bounded identifier")
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
		add("complexity.ports", "inputs", "Graph port count exceeds the bounded limit")
	}
	validatePorts(revision.Inputs, "inputs", add)
	validatePorts(revision.Outputs, "outputs", add)
	if len(revision.Nodes) == 0 || len(revision.Nodes) > MaxNodes {
		add("complexity.nodes", "nodes", "Graph must contain a bounded non-empty node set")
	}
	if len(revision.Dependencies) > MaxDependencies {
		add("complexity.dependencies", "dependencies", "dependency count exceeds the bounded limit")
	}
	mappingCount := len(revision.InputMappings) + len(revision.OutputMappings)
	for _, dependency := range revision.Dependencies {
		mappingCount += len(dependency.Mappings)
	}
	if mappingCount > MaxMappings {
		add("complexity.mappings", "dependencies", "mapping count exceeds the bounded limit")
	}
	if len(revision.AdmissionRules) > MaxAdmissionRules {
		add("complexity.admission_rules", "admission_rules", "admission rule count exceeds the bounded limit")
	}

	nodes := make(map[string]Node, len(revision.Nodes))
	for index, node := range revision.Nodes {
		path := fmt.Sprintf("nodes[%d]", index)
		if !validID(node.ID) {
			add("node.id_invalid", path+".id", "node ID is malformed")
		} else if _, duplicate := nodes[node.ID]; duplicate {
			add("node.duplicate", path+".id", "node ID is duplicated")
		} else {
			nodes[node.ID] = node
		}
		if err := node.Participant.Validate(); err != nil {
			add("node.participant_invalid", path+".participant", "node must bind an exact Agent revision and digest")
		}
		if err := node.Loop.Validate(); err != nil {
			add("node.loop_invalid", path+".loop", "node must bind an exact Loop revision and digest")
		}
		if len(node.Inputs) > MaxPorts || len(node.Outputs) > MaxPorts {
			add("complexity.node_ports", path, "node port count exceeds the bounded limit")
		}
		validatePorts(node.Inputs, path+".inputs", add)
		validatePorts(node.Outputs, path+".outputs", add)
	}

	mappedInputs := make(map[string]struct{})
	for index, mapping := range revision.InputMappings {
		path := fmt.Sprintf("input_mappings[%d]", index)
		source, sourceOK := portByID(revision.Inputs, mapping.GraphInput)
		targetNode, nodeOK := nodes[mapping.ToNodeID]
		target, targetOK := portByID(targetNode.Inputs, mapping.ToPort)
		if !sourceOK {
			add("mapping.graph_input_missing", path+".graph_input", "Graph input does not exist")
		}
		if !nodeOK {
			add("mapping.target_node_missing", path+".to_node_id", "target node does not exist")
		}
		if nodeOK && !targetOK {
			add("mapping.target_port_missing", path+".to_port", "target input port does not exist")
		}
		checkMappedInput(mapping.ToNodeID, mapping.ToPort, path, mappedInputs, add)
		if sourceOK && targetOK && source.Type != target.Type {
			add("mapping.type_mismatch", path, "mapped ports have incompatible types")
		}
	}

	outgoing := make(map[string][]string)
	dependencyIDs := make(map[string]struct{}, len(revision.Dependencies))
	for index, dependency := range revision.Dependencies {
		path := fmt.Sprintf("dependencies[%d]", index)
		if !validID(dependency.ID) {
			add("dependency.id_invalid", path+".id", "dependency ID is malformed")
		} else if _, duplicate := dependencyIDs[dependency.ID]; duplicate {
			add("dependency.duplicate", path+".id", "dependency ID is duplicated")
		} else {
			dependencyIDs[dependency.ID] = struct{}{}
		}
		from, fromOK := nodes[dependency.FromNodeID]
		to, toOK := nodes[dependency.ToNodeID]
		if !fromOK {
			add("dependency.source_missing", path+".from_node_id", "dependency source node does not exist")
		}
		if !toOK {
			add("dependency.target_missing", path+".to_node_id", "dependency target node does not exist")
		}
		if fromOK && toOK && from.ID == to.ID {
			add("dependency.self", path, "node cannot depend on itself")
		}
		if fromOK && toOK && from.ID != to.ID {
			outgoing[from.ID] = append(outgoing[from.ID], to.ID)
		}
		if len(dependency.Mappings) == 0 {
			add("dependency.mapping_missing", path+".mappings", "dependency requires at least one typed mapping")
		}
		for mappingIndex, mapping := range dependency.Mappings {
			mappingPath := fmt.Sprintf("%s.mappings[%d]", path, mappingIndex)
			source, sourceOK := portByID(from.Outputs, mapping.FromPort)
			target, targetOK := portByID(to.Inputs, mapping.ToPort)
			if fromOK && !sourceOK {
				add("mapping.source_port_missing", mappingPath+".from_port", "source output port does not exist")
			}
			if toOK && !targetOK {
				add("mapping.target_port_missing", mappingPath+".to_port", "target input port does not exist")
			}
			checkMappedInput(dependency.ToNodeID, mapping.ToPort, mappingPath, mappedInputs, add)
			if sourceOK && targetOK && source.Type != target.Type {
				add("mapping.type_mismatch", mappingPath, "mapped ports have incompatible types")
			}
		}
	}
	if hasCycle(nodes, outgoing) {
		add("dependency.cycle", "dependencies", "Graph dependencies must be acyclic")
	}
	for _, node := range revision.Nodes {
		for _, input := range node.Inputs {
			if input.Required {
				if _, ok := mappedInputs[node.ID+"\x00"+input.ID]; !ok {
					add("mapping.required_input_missing", "nodes."+node.ID+".inputs."+input.ID, "required node input is not mapped")
				}
			}
		}
	}

	mappedOutputs := make(map[string]struct{})
	for index, mapping := range revision.OutputMappings {
		path := fmt.Sprintf("output_mappings[%d]", index)
		node, nodeOK := nodes[mapping.FromNodeID]
		source, sourceOK := portByID(node.Outputs, mapping.FromPort)
		target, targetOK := portByID(revision.Outputs, mapping.GraphOutput)
		if !nodeOK {
			add("mapping.source_node_missing", path+".from_node_id", "source node does not exist")
		}
		if nodeOK && !sourceOK {
			add("mapping.source_port_missing", path+".from_port", "source output port does not exist")
		}
		if !targetOK {
			add("mapping.graph_output_missing", path+".graph_output", "Graph output does not exist")
		}
		if _, duplicate := mappedOutputs[mapping.GraphOutput]; duplicate {
			add("mapping.graph_output_duplicate", path+".graph_output", "Graph output is mapped more than once")
		}
		mappedOutputs[mapping.GraphOutput] = struct{}{}
		if sourceOK && targetOK && source.Type != target.Type {
			add("mapping.type_mismatch", path, "mapped ports have incompatible types")
		}
	}
	for _, output := range revision.Outputs {
		if _, ok := mappedOutputs[output.ID]; !ok {
			add("mapping.graph_output_unsatisfied", "outputs."+output.ID, "Graph output is not mapped")
		}
	}

	rules := make(map[string]struct{}, len(revision.AdmissionRules))
	for index, rule := range revision.AdmissionRules {
		path := fmt.Sprintf("admission_rules[%d]", index)
		if !validID(rule.ID) {
			add("admission_rule.id_invalid", path+".id", "admission rule ID is malformed")
		} else if _, duplicate := rules[rule.ID]; duplicate {
			add("admission_rule.duplicate", path+".id", "admission rule ID is duplicated")
		}
		rules[rule.ID] = struct{}{}
		if err := rule.PolicyRef.Validate(); err != nil {
			add("admission_rule.policy_invalid", path+".policy_ref", "admission rule requires an exact policy digest")
		}
	}
	if verifyDigest {
		if !validDigest(revision.Digest) {
			add("digest.invalid", "digest", "revision digest must be lowercase sha256:<64-hex>")
		} else if digest, err := digestRevision(revision); err != nil || digest != revision.Digest {
			add("digest.mismatch", "digest", "revision digest does not match canonical content")
		}
	}
	sortIssues(issues)
	return issues
}

func validatePorts(ports []Port, path string, add func(string, string, string)) {
	seen := make(map[string]struct{}, len(ports))
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

func checkMappedInput(nodeID, portID, path string, mapped map[string]struct{}, add func(string, string, string)) {
	key := nodeID + "\x00" + portID
	if _, duplicate := mapped[key]; duplicate {
		add("mapping.target_duplicate", path, "node input is mapped more than once")
	}
	mapped[key] = struct{}{}
}

func portByID(ports []Port, id string) (Port, bool) {
	for _, port := range ports {
		if port.ID == id {
			return port, true
		}
	}
	return Port{}, false
}

func hasCycle(nodes map[string]Node, outgoing map[string][]string) bool {
	state := make(map[string]uint8, len(nodes))
	var visit func(string) bool
	visit = func(id string) bool {
		if state[id] == 1 {
			return true
		}
		if state[id] == 2 {
			return false
		}
		state[id] = 1
		for _, next := range outgoing[id] {
			if visit(next) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for id := range nodes {
		if visit(id) {
			return true
		}
	}
	return false
}

func validateInputValue(input NormalizedInput) error {
	if !validID(input.PortID) || !validValueType(input.Type) || len(input.Value) == 0 || len(input.Value) > MaxInputValueBytes {
		return fmt.Errorf("normalized input metadata is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(input.Value))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode normalized input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("normalized input contains trailing JSON")
		}
		return fmt.Errorf("normalized input contains trailing data: %w", err)
	}
	switch input.Type {
	case TypeString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("normalized input type mismatch")
		}
	case TypeArtifact:
		artifact, ok := value.(string)
		if !ok || !validDigest(artifact) {
			return fmt.Errorf("normalized artifact must be an exact digest")
		}
	case TypeBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("normalized input type mismatch")
		}
	case TypeInteger:
		number, ok := value.(json.Number)
		if !ok || strings.ContainsAny(number.String(), ".eE") {
			return fmt.Errorf("normalized input type mismatch")
		}
		if _, err := number.Int64(); err != nil {
			return fmt.Errorf("normalized integer is out of range")
		}
	case TypeNumber:
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("normalized input type mismatch")
		}
		parsed, err := number.Float64()
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return fmt.Errorf("normalized number is invalid")
		}
	case TypeObject:
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("normalized input type mismatch")
		}
	case TypeArray:
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("normalized input type mismatch")
		}
	}
	return nil
}

func validID(value string) bool {
	return identifierPattern.MatchString(value) && strings.TrimSpace(value) == value
}

func validDigest(value string) bool {
	if len(value) != digestLength || !strings.HasPrefix(value, "sha256:") {
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

func sortIssues(issues []ValidationIssue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path != issues[j].Path {
			return issues[i].Path < issues[j].Path
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Message < issues[j].Message
	})
}
