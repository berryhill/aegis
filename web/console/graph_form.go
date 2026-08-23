package consoleweb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/reference"
)

const maxGraphConsoleFormBytes = 2 << 20

func GraphReferenceOptions(surface app.FleetSurface) ([]GraphReferenceOption, []GraphReferenceOption) {
	agents := make([]GraphReferenceOption, 0, len(surface.Agents))
	for _, item := range surface.Agents {
		revision := item.Revision
		agents = append(agents, GraphReferenceOption{Value: revision.Digest, Label: fmt.Sprintf("%s revision %d @ %s · %s", revision.AgentID, revision.Revision, revision.Digest, revision.Lifecycle)})
	}
	loops := make([]GraphReferenceOption, 0, len(surface.Loops))
	for _, item := range surface.Loops {
		revision := item.Revision
		loops = append(loops, GraphReferenceOption{Value: revision.Digest, Label: fmt.Sprintf("%s revision %d @ %s · %s", revision.LoopID, revision.Revision, revision.Digest, item.Lifecycle.State)})
	}
	return agents, loops
}

func DecodeGraphConsoleForm(request *http.Request) (url.Values, error) {
	mediaType, _, mediaTypeErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if mediaTypeErr != nil || mediaType != "application/x-www-form-urlencoded" || request.Body == nil || request.ContentLength > maxGraphConsoleFormBytes {
		return nil, errors.New("invalid Graph console form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxGraphConsoleFormBytes+1))
	if err != nil || len(body) > maxGraphConsoleFormBytes {
		return nil, errors.New("invalid Graph console form")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, errors.New("invalid Graph console form")
	}
	for key, entries := range values {
		if key == "" || len(entries) == 0 || (len(entries) != 1 && !strings.HasPrefix(key, "input.")) {
			return nil, errors.New("duplicate or empty Graph console field")
		}
	}
	return values, nil
}

func ExactFormValue(values url.Values, key string) (string, error) {
	entries, ok := values[key]
	if !ok || len(entries) != 1 || strings.TrimSpace(entries[0]) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return strings.TrimSpace(entries[0]), nil
}

func OptionalFormValue(values url.Values, key string) string {
	if entries := values[key]; len(entries) == 1 {
		return strings.TrimSpace(entries[0])
	}
	return ""
}

func authorizeGraphConsoleMutation(request *http.Request, values url.Values, authorize func(*http.Request) error) error {
	csrf, err := ExactFormValue(values, "csrf")
	if err != nil {
		return err
	}
	request.Header.Set("X-CSRF-Token", csrf)
	return authorize(request)
}

func ParseGraphPublication(values url.Values, surface app.FleetSurface, authority reference.DigestRef) (app.PublishGraphInput, error) {
	allowed := map[string]bool{"csrf": true, "authority_session_id": true, "graph_id": true, "revision": true, "expected_previous_digest": true, "idempotency_key": true}
	for i := 1; i <= 8; i++ {
		for _, prefix := range []string{"input_id_", "input_type_", "input_required_", "output_id_", "output_type_", "output_required_", "node_id_", "node_agent_", "node_loop_", "input_map_graph_", "input_map_node_", "input_map_port_", "dependency_id_", "dependency_from_", "dependency_to_", "dependency_mappings_", "output_map_node_", "output_map_port_", "output_map_graph_"} {
			allowed[prefix+strconv.Itoa(i)] = true
		}
	}
	for i := 1; i <= 4; i++ {
		for _, prefix := range []string{"policy_rule_", "policy_id_", "policy_digest_"} {
			allowed[prefix+strconv.Itoa(i)] = true
		}
	}
	for key := range values {
		if !allowed[key] {
			return app.PublishGraphInput{}, fmt.Errorf("unknown Graph publication field %q", key)
		}
	}
	graphID, err := ExactFormValue(values, "graph_id")
	if err != nil {
		return app.PublishGraphInput{}, err
	}
	revisionRaw, err := ExactFormValue(values, "revision")
	if err != nil {
		return app.PublishGraphInput{}, err
	}
	revisionNumber, err := strconv.ParseUint(revisionRaw, 10, 64)
	if err != nil || revisionNumber == 0 {
		return app.PublishGraphInput{}, errors.New("revision must be a positive integer")
	}
	idempotencyKey, err := ExactFormValue(values, "idempotency_key")
	if err != nil {
		return app.PublishGraphInput{}, err
	}
	candidate := graph.GraphRevision{
		SchemaVersion: graph.RevisionSchemaVersion, GraphID: graphID, Revision: revisionNumber,
		PreviousDigest: OptionalFormValue(values, "expected_previous_digest"),
		Inputs:         []graph.Port{}, Outputs: []graph.Port{}, Nodes: []graph.Node{}, InputMappings: []graph.InputMapping{},
		Dependencies: []graph.Dependency{}, OutputMappings: []graph.OutputMapping{}, AdmissionRules: []graph.AdmissionRule{},
		Validator: graph.ValidatorSpec{ID: graph.ValidatorID, Version: graph.ValidatorVersion},
	}
	for i := 1; i <= 8; i++ {
		suffix := strconv.Itoa(i)
		inputID, inputType := OptionalFormValue(values, "input_id_"+suffix), OptionalFormValue(values, "input_type_"+suffix)
		if inputID != "" || inputType != "" || OptionalFormValue(values, "input_required_"+suffix) != "" {
			if inputID == "" || inputType == "" {
				return app.PublishGraphInput{}, fmt.Errorf("input row %d is incomplete", i)
			}
			candidate.Inputs = append(candidate.Inputs, graph.Port{ID: inputID, Type: graph.ValueType(inputType), Required: OptionalFormValue(values, "input_required_"+suffix) == "true"})
		}
		outputID, outputType := OptionalFormValue(values, "output_id_"+suffix), OptionalFormValue(values, "output_type_"+suffix)
		if outputID != "" || outputType != "" || OptionalFormValue(values, "output_required_"+suffix) != "" {
			if outputID == "" || outputType == "" {
				return app.PublishGraphInput{}, fmt.Errorf("output row %d is incomplete", i)
			}
			candidate.Outputs = append(candidate.Outputs, graph.Port{ID: outputID, Type: graph.ValueType(outputType), Required: OptionalFormValue(values, "output_required_"+suffix) == "true"})
		}
	}
	for i := 1; i <= 8; i++ {
		suffix := strconv.Itoa(i)
		nodeID, agentToken, loopToken := OptionalFormValue(values, "node_id_"+suffix), OptionalFormValue(values, "node_agent_"+suffix), OptionalFormValue(values, "node_loop_"+suffix)
		if nodeID == "" && agentToken == "" && loopToken == "" {
			continue
		}
		if nodeID == "" || agentToken == "" || loopToken == "" {
			return app.PublishGraphInput{}, fmt.Errorf("node row %d is incomplete", i)
		}
		participant, found := findAgentRef(surface, agentToken)
		if !found {
			return app.PublishGraphInput{}, fmt.Errorf("node row %d selects an unavailable exact Agent revision", i)
		}
		loopRef, inputs, outputs, found := findLoopRef(surface, loopToken)
		if !found {
			return app.PublishGraphInput{}, fmt.Errorf("node row %d selects an unavailable exact Loop revision", i)
		}
		candidate.Nodes = append(candidate.Nodes, graph.Node{ID: nodeID, Participant: participant, Loop: loopRef, Inputs: inputs, Outputs: outputs})
	}
	for i := 1; i <= 8; i++ {
		suffix := strconv.Itoa(i)
		graphInput, nodeID, portID := OptionalFormValue(values, "input_map_graph_"+suffix), OptionalFormValue(values, "input_map_node_"+suffix), OptionalFormValue(values, "input_map_port_"+suffix)
		if graphInput != "" || nodeID != "" || portID != "" {
			if graphInput == "" || nodeID == "" || portID == "" {
				return app.PublishGraphInput{}, fmt.Errorf("input mapping row %d is incomplete", i)
			}
			candidate.InputMappings = append(candidate.InputMappings, graph.InputMapping{GraphInput: graphInput, ToNodeID: nodeID, ToPort: portID})
		}
		dependencyID, fromNode, toNode, rawMappings := OptionalFormValue(values, "dependency_id_"+suffix), OptionalFormValue(values, "dependency_from_"+suffix), OptionalFormValue(values, "dependency_to_"+suffix), OptionalFormValue(values, "dependency_mappings_"+suffix)
		if dependencyID != "" || fromNode != "" || toNode != "" || rawMappings != "" {
			if dependencyID == "" || fromNode == "" || toNode == "" || rawMappings == "" {
				return app.PublishGraphInput{}, fmt.Errorf("dependency row %d is incomplete", i)
			}
			mappings, parseErr := parsePortMappings(rawMappings)
			if parseErr != nil {
				return app.PublishGraphInput{}, fmt.Errorf("dependency row %d: %w", i, parseErr)
			}
			candidate.Dependencies = append(candidate.Dependencies, graph.Dependency{ID: dependencyID, FromNodeID: fromNode, ToNodeID: toNode, Mappings: mappings})
		}
		fromOutputNode, fromOutputPort, graphOutput := OptionalFormValue(values, "output_map_node_"+suffix), OptionalFormValue(values, "output_map_port_"+suffix), OptionalFormValue(values, "output_map_graph_"+suffix)
		if fromOutputNode != "" || fromOutputPort != "" || graphOutput != "" {
			if fromOutputNode == "" || fromOutputPort == "" || graphOutput == "" {
				return app.PublishGraphInput{}, fmt.Errorf("output mapping row %d is incomplete", i)
			}
			candidate.OutputMappings = append(candidate.OutputMappings, graph.OutputMapping{FromNodeID: fromOutputNode, FromPort: fromOutputPort, GraphOutput: graphOutput})
		}
	}
	for i := 1; i <= 4; i++ {
		suffix := strconv.Itoa(i)
		ruleID, policyID, digest := OptionalFormValue(values, "policy_rule_"+suffix), OptionalFormValue(values, "policy_id_"+suffix), OptionalFormValue(values, "policy_digest_"+suffix)
		if ruleID != "" || policyID != "" || digest != "" {
			if ruleID == "" || policyID == "" || digest == "" {
				return app.PublishGraphInput{}, fmt.Errorf("policy row %d is incomplete", i)
			}
			candidate.AdmissionRules = append(candidate.AdmissionRules, graph.AdmissionRule{ID: ruleID, PolicyRef: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: policyID, Digest: digest}})
		}
	}
	return app.PublishGraphInput{Authority: authority, Revision: candidate, ExpectedPreviousDigest: candidate.PreviousDigest, IdempotencyKey: idempotencyKey}, nil
}

func findAgentRef(surface app.FleetSurface, digest string) (reference.RevisionRef, bool) {
	for _, item := range surface.Agents {
		revision := item.Revision
		if revision.Digest == digest {
			return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.AgentID, Revision: revision.Revision, Digest: revision.Digest}, true
		}
	}
	return reference.RevisionRef{}, false
}

func findLoopRef(surface app.FleetSurface, digest string) (reference.RevisionRef, []graph.Port, []graph.Port, bool) {
	for _, item := range surface.Loops {
		revision := item.Revision
		if revision.Digest != digest {
			continue
		}
		inputs := make([]graph.Port, 0, len(revision.Inputs))
		for _, port := range revision.Inputs {
			inputs = append(inputs, graph.Port{ID: port.ID, Type: graph.ValueType(port.Type), Required: port.Required})
		}
		outputs := make([]graph.Port, 0, len(revision.Outputs))
		for _, port := range revision.Outputs {
			outputs = append(outputs, graph.Port{ID: port.ID, Type: graph.ValueType(port.Type), Required: port.Required})
		}
		return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: revision.Digest}, inputs, outputs, true
	}
	return reference.RevisionRef{}, nil, nil, false
}

func parsePortMappings(raw string) ([]graph.PortMapping, error) {
	parts := strings.Split(raw, ",")
	mappings := make([]graph.PortMapping, 0, len(parts))
	for _, part := range parts {
		pair := strings.Split(strings.TrimSpace(part), ">")
		if len(pair) != 2 || strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			return nil, errors.New("mappings must use source_port>target_port")
		}
		mappings = append(mappings, graph.PortMapping{FromPort: strings.TrimSpace(pair[0]), ToPort: strings.TrimSpace(pair[1])})
	}
	return mappings, nil
}

func ParseGraphSubmission(values url.Values, revision graph.GraphRevision, authority reference.DigestRef) (app.SubmitGraphInput, error) {
	allowed := map[string]bool{"csrf": true, "authority_session_id": true, "graph_id": true, "graph_revision": true, "graph_digest": true, "idempotency_key": true, "max_attempts": true}
	ports := make(map[string]graph.Port, len(revision.Inputs))
	for _, port := range revision.Inputs {
		ports[port.ID] = port
		allowed["input."+port.ID] = true
	}
	for key := range values {
		if !allowed[key] && !strings.HasPrefix(key, "input.") {
			return app.SubmitGraphInput{}, fmt.Errorf("unknown Graph submission field %q", key)
		}
	}
	graphID, err := ExactFormValue(values, "graph_id")
	if err != nil || graphID != revision.GraphID {
		return app.SubmitGraphInput{}, errors.New("submitted Graph ID does not match the reviewed revision")
	}
	revisionRaw, err := ExactFormValue(values, "graph_revision")
	if err != nil {
		return app.SubmitGraphInput{}, err
	}
	revisionNumber, err := strconv.ParseUint(revisionRaw, 10, 64)
	if err != nil || revisionNumber != revision.Revision || OptionalFormValue(values, "graph_digest") != revision.Digest {
		return app.SubmitGraphInput{}, errors.New("submitted Graph reference does not match the reviewed exact revision")
	}
	idempotencyKey, err := ExactFormValue(values, "idempotency_key")
	if err != nil {
		return app.SubmitGraphInput{}, err
	}
	maxAttemptsRaw, err := ExactFormValue(values, "max_attempts")
	if err != nil {
		return app.SubmitGraphInput{}, err
	}
	maxAttempts64, err := strconv.ParseUint(maxAttemptsRaw, 10, 32)
	if err != nil || maxAttempts64 == 0 || maxAttempts64 > 100 {
		return app.SubmitGraphInput{}, errors.New("max_attempts must be between 1 and 100")
	}
	inputs := make([]graph.NormalizedInput, 0, len(revision.Inputs))
	for _, port := range revision.Inputs {
		entries := values["input."+port.ID]
		if len(entries) == 0 || (len(entries) == 1 && entries[0] == "") {
			if port.Required {
				// Preserve the omission for Graph admission, which records one
				// durable invalid_inputs rejection and creates no Queue item.
			}
			continue
		}
		for _, raw := range entries {
			var encoded []byte
			if port.Type == graph.TypeString || port.Type == graph.TypeArtifact {
				encoded, err = json.Marshal(raw)
			} else {
				// Preserve malformed or wrongly typed JSON for domain admission.
				// graph.NewRunSnapshot rejects it durably before Queue admission.
				encoded = []byte(raw)
			}
			if err != nil || len(encoded) > graph.MaxInputValueBytes {
				return app.SubmitGraphInput{}, fmt.Errorf("input %q is too large", port.ID)
			}
			inputs = append(inputs, graph.NormalizedInput{PortID: port.ID, Type: port.Type, Value: encoded})
		}
	}
	for key, entries := range values {
		if !strings.HasPrefix(key, "input.") {
			continue
		}
		portID := strings.TrimPrefix(key, "input.")
		if _, known := ports[portID]; known {
			continue
		}
		for _, raw := range entries {
			encoded, marshalErr := json.Marshal(raw)
			if marshalErr != nil || len(encoded) > graph.MaxInputValueBytes {
				return app.SubmitGraphInput{}, fmt.Errorf("unknown input %q is too large", portID)
			}
			inputs = append(inputs, graph.NormalizedInput{PortID: portID, Type: graph.TypeString, Value: encoded})
		}
	}
	identity := stableSubmissionIdentity(idempotencyKey)
	return app.SubmitGraphInput{
		Authority: authority,
		Graph:     reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.GraphID, Revision: revision.Revision, Digest: revision.Digest},
		Inputs:    inputs, SubmissionID: "submission-" + identity, IdempotencyKey: idempotencyKey,
		SnapshotID: "snapshot-" + identity, QueueItemID: "queue-" + identity, GraphRunID: "graph-run-" + identity,
		TransitionID: "queue-transition-" + identity, RejectionID: "rejection-" + identity, MaxAttempts: uint32(maxAttempts64),
	}, nil
}

func stableSubmissionIdentity(idempotencyKey string) string {
	digest := sha256.Sum256([]byte(idempotencyKey))
	return hex.EncodeToString(digest[:12])
}
