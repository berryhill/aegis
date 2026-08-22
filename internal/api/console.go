package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/principalauth"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/starfederation/datastar-go/datastar"
)

const maxConsolePatchBytes = 1 << 20

func validateBrowserHandoff(raw, consoleHost string) (string, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Scheme != "http" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Port() == "" || !strings.EqualFold(target.Hostname(), consoleHost) {
		return "", errors.New("invalid browser handoff confirmation")
	}
	port, err := strconv.Atoi(target.Port())
	if err != nil || port < 1 || port > 65535 || !isLoopbackConsoleHost(target.Hostname()) {
		return "", errors.New("invalid browser handoff confirmation")
	}
	token := strings.TrimPrefix(target.EscapedPath(), "/confirmed/")
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || target.EscapedPath() != "/confirmed/"+token {
		return "", errors.New("invalid browser handoff confirmation")
	}
	return target.String(), nil
}

func isLoopbackConsoleHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type consoleDomain string

const (
	consoleAgents      consoleDomain = "agents"
	consoleLoops       consoleDomain = "loops"
	consoleGraphs      consoleDomain = "graphs"
	consoleQueue       consoleDomain = "queue"
	consoleCredentials consoleDomain = "credentials"
)

type consoleSignals struct {
	Bootstrap string `json:"bootstrap,omitempty"`
	CSRF      string `json:"csrf,omitempty"`
}

func validateConsoleSignals(request *http.Request) error {
	var reader io.Reader
	if request.Method == http.MethodGet {
		raw := request.URL.Query().Get(datastar.DatastarKey)
		if raw == "" {
			return nil
		}
		if len(raw) > 8192 {
			return errors.New("invalid console signals: payload too large")
		}
		reader = strings.NewReader(raw)
	} else {
		if request.Body == nil || request.ContentLength == 0 {
			return nil
		}
		reader = io.LimitReader(request.Body, 8193)
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var signals consoleSignals
	if err := decoder.Decode(&signals); err != nil {
		return fmt.Errorf("invalid console signals: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid console signals: trailing data")
	}
	return nil
}

func parseConsoleDomain(raw string) (consoleDomain, error) {
	domain := consoleDomain(raw)
	if domain == "" {
		return consoleAgents, nil
	}
	switch domain {
	case consoleAgents, consoleLoops, consoleGraphs, consoleQueue, consoleCredentials:
		return domain, nil
	default:
		return "", errors.New("unknown console domain")
	}
}

func wantsDatastar(request *http.Request) bool {
	return strings.Contains(request.Header.Get("Accept"), "text/event-stream")
}

func isConsoleForm(request *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/x-www-form-urlencoded"
}

func decodeConsoleForm(request *http.Request, field string) (string, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > 8192 {
		return "", errors.New("invalid console form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 8193))
	if err != nil || len(body) > 8192 {
		return "", errors.New("invalid console form")
	}
	values, err := url.ParseQuery(string(body))
	if err != nil || len(values) != 1 || len(values[field]) != 1 {
		return "", errors.New("invalid console form")
	}
	return values[field][0], nil
}

type passwordRotationForm struct {
	Current      string
	New          string
	Confirmation string
	CSRF         string
	Approved     bool
}

func decodePasswordRotationForm(request *http.Request) (passwordRotationForm, error) {
	if !isConsoleForm(request) || request.Body == nil || request.ContentLength > 8192 {
		return passwordRotationForm{}, errors.New("invalid principal password rotation form")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 8193))
	if err != nil || len(body) > 8192 {
		return passwordRotationForm{}, errors.New("invalid principal password rotation form")
	}
	values, err := url.ParseQuery(string(body))
	fields := []string{"current_password", "new_password", "confirmation", "csrf", "approve"}
	if err != nil || len(values) != len(fields) {
		return passwordRotationForm{}, errors.New("invalid principal password rotation form")
	}
	for _, field := range fields {
		if len(values[field]) != 1 {
			return passwordRotationForm{}, errors.New("invalid principal password rotation form")
		}
	}
	return passwordRotationForm{Current: values.Get("current_password"), New: values.Get("new_password"), Confirmation: values.Get("confirmation"), CSRF: values.Get("csrf"), Approved: values.Get("approve") == "rotate"}, nil
}

func replacePrincipalVerifier(path string, current, replacement principalauth.Record, authorize, complete func() error) error {
	if authorize == nil || complete == nil {
		return errors.New("principal password rotation audit callbacks are required")
	}
	if err := authorize(); err != nil {
		return err
	}
	if err := principalauth.Replace(path, current, replacement); err != nil {
		return err
	}
	if err := complete(); err != nil {
		rollbackErr := principalauth.Replace(path, replacement, current)
		return errors.Join(err, rollbackErr)
	}
	return nil
}

func renderConsole(ctx context.Context, component templ.Component) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := component.Render(ctx, &output); err != nil {
		return nil, err
	}
	if output.Len() > maxConsolePatchBytes {
		return nil, errors.New("console render exceeds bounded response size")
	}
	return output.Bytes(), nil
}

func patchConsole(writer http.ResponseWriter, request *http.Request, component templ.Component) error {
	html, err := renderConsole(request.Context(), component)
	if err != nil {
		return err
	}
	if err = request.Context().Err(); err != nil {
		return err
	}
	sse := datastar.NewSSE(writer, request, datastar.WithContext(request.Context()))
	if sse.IsClosed() {
		return request.Context().Err()
	}
	return sse.PatchElements(string(html))
}

func consoleSurfaceModel(surface app.FleetSurface, domain consoleDomain) (consoleweb.SurfaceModel, error) {
	model := consoleweb.SurfaceModel{Domain: string(domain), State: "ready", Records: []consoleweb.RecordModel{}}
	var values []any
	readinessKey := string(domain)
	switch domain {
	case consoleAgents:
		model.Title, model.Eyebrow, model.Description, readinessKey = "Agent Registry", "Participants", "Agents derived from immutable charter revisions. Select one to inspect its effective authority, provisioning evidence and readiness.", "registry"
		model.CharterImportProposal = consoleweb.CharterImportProposal{
			Notice:          "Review only. The browser does not import charters, provision agents, or grant authority; run the exact CLI commands from an authenticated terminal.",
			ValidateCommand: "aegis charter validate <charter-file.json>",
			ImportCommand:   "aegis charter import <charter-file.json>",
		}
		for _, value := range surface.Agents {
			values = append(values, value)
		}
	case consoleLoops:
		model.Title, model.Eyebrow, model.Description = "Loops", "Definitions", "Versioned units of bounded agent work. A Loop declares its required inputs, internal control flow, and the evidence a completion must produce. Select one to inspect it and prepare an execution request."
		for _, value := range surface.Loops {
			values = append(values, value)
		}
	case consoleGraphs:
		model.Title, model.Eyebrow, model.Description = "Graphs", "Definitions", "Versioned workflow definitions. A graph wires versioned Loops into a directed dependency structure. Select one to inspect it and prepare an execution request."
		for _, value := range surface.Graphs {
			values = append(values, value)
		}
	case consoleQueue:
		model.Title, model.Eyebrow, model.Description = "Execution Queue", "Runtime", "Submissions judged at admission. An admitted submission becomes an execution against an exact pinned definition version; a refused one never does. Select a record to inspect it against the revision it pinned."
		for _, value := range surface.Queue {
			values = append(values, value)
		}
	case consoleCredentials:
		model.Title, model.Eyebrow, model.Description = "Credentials", "Encrypted credential authority", "Authoritative encrypted credential records, lifecycle history, and vault metadata. Secret values, ciphertext, and key material are never read or shown here."
		if surface.VaultStatus.State != "" {
			model.Source = surface.VaultStatus.Source()
		}
		for _, value := range surface.Credentials {
			values = append(values, value)
		}
	default:
		return consoleweb.SurfaceModel{}, errors.New("unknown console domain")
	}
	readiness, ok := surface.Readiness[readinessKey]
	model.Actions = consoleActions(surface, domain)
	if !ok || !readiness.Authoritative {
		model.Authoritative = false
		model.State = readiness.State
		if model.State == "" {
			model.State = "unavailable"
		}
		model.ReasonCode, model.Source = readiness.ReasonCode, readiness.Source
		model.Status = fmt.Sprintf("%s · source %s · reason %s", model.Title, fallback(readiness.Source, "unknown"), fallback(readiness.ReasonCode, "readiness_missing"))
		return model, nil
	}
	model.Authoritative, model.TotalCount, model.TotalRecords = true, readiness.Count, readiness.Count
	model.State, model.ReasonCode, model.Source = readiness.State, readiness.ReasonCode, readiness.Source
	shown := len(values)
	count := strconv.Itoa(readiness.Count)
	if shown != readiness.Count {
		count = fmt.Sprintf("showing %d of %d", shown, readiness.Count)
	}
	model.Status = fmt.Sprintf("%s authoritative %s record%s · source %s · reason %s", count, strings.ToLower(model.Title), plural(readiness.Count), readiness.Source, readiness.ReasonCode)
	for index, value := range values {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return consoleweb.SurfaceModel{}, err
		}
		label := consoleRecordLabel(domain, value)
		record := consoleweb.RecordModel{Key: strconv.Itoa(index), Label: label, Summary: label, JSON: string(data)}
		if domain == consoleAgents {
			agent, ok := value.(app.FleetAgent)
			if !ok {
				return consoleweb.SurfaceModel{}, errors.New("invalid Agent Registry record")
			}
			record = consoleAgentRecord(agent)
		} else if domain == consoleLoops {
			loopView, ok := value.(app.LoopView)
			if !ok {
				return consoleweb.SurfaceModel{}, errors.New("invalid Loop record")
			}
			record = consoleLoopRecord(loopView)
		} else if domain == consoleGraphs {
			graphView, ok := value.(app.GraphView)
			if !ok {
				return consoleweb.SurfaceModel{}, errors.New("invalid Graph record")
			}
			record = consoleGraphRecord(graphView, surface.Submissions, string(data))
		} else if domain == consoleQueue {
			queueView, ok := value.(app.QueueExecutionView)
			if !ok {
				return consoleweb.SurfaceModel{}, errors.New("invalid Execution Queue record")
			}
			record = consoleQueueRecord(queueView)
		} else if domain == consoleCredentials {
			cred, ok := value.(app.CredentialView)
			if !ok {
				return consoleweb.SurfaceModel{}, errors.New("invalid Credentials record")
			}
			record = consoleCredentialRecord(cred, surface.VaultStatus)
		}
		model.Records = append(model.Records, record)
		if domain == consoleQueue {
			if !containsString(model.QueueStates, record.Lifecycle) {
				model.QueueStates = append(model.QueueStates, record.Lifecycle)
			}
			if record.Lifecycle == "active" {
				model.ActiveRecords = append(model.ActiveRecords, record)
			}
			if record.Lifecycle == "failed" {
				model.FailedRecords = append(model.FailedRecords, record)
			}
		}
	}
	return model, nil
}

func consoleAgentRecord(agent app.FleetAgent) consoleweb.RecordModel {
	revision := agent.Revision
	readiness := "Lifecycle eligible; fresh authority admission required"
	if revision.Lifecycle == "disabled" {
		readiness = "Execution denied until a new enabled revision"
	} else if revision.Lifecycle == "retired" {
		readiness = "Terminal; no later revisions permitted"
	}
	capabilities := strings.Join(revision.CapabilityDeclarations, ", ")
	if capabilities == "" {
		capabilities = "None declared"
	}
	policies := make([]string, 0, len(revision.PolicyRefs))
	for _, policy := range revision.PolicyRefs {
		policies = append(policies, policy.ID+" @ "+policy.Digest)
	}
	if len(policies) == 0 {
		policies = append(policies, "None declared")
	}
	return consoleweb.RecordModel{
		Key: revision.AgentID, Label: revision.AgentID, Summary: revision.AgentID + " · " + string(revision.Lifecycle),
		Lifecycle: string(revision.Lifecycle), Readiness: readiness,
		Revision: fmt.Sprintf("r%d", revision.Revision), Runtime: revision.Runtime.Target,
		Source: revision.Source.FleetID + " / " + revision.Source.Kind + " / " + revision.Source.SourceID, Owner: revision.Ownership.OwnerID,
		Authority:    fmt.Sprintf("%d capabilities · %d policies declared", len(revision.CapabilityDeclarations), len(revision.PolicyRefs)),
		Provisioning: "Not asserted by Registry record",
		Fields: []consoleweb.FieldModel{
			{Label: "Stable Agent ID", Value: revision.AgentID},
			{Label: "Fleet provenance", Value: revision.Source.FleetID + " / " + revision.Source.Kind + " / " + revision.Source.SourceID},
			{Label: "Owner", Value: revision.Ownership.OwnerID},
			{Label: "Accountability", Value: revision.Ownership.AccountabilityID},
			{Label: "Runtime binding", Value: revision.Runtime.Adapter + " / " + revision.Runtime.Runtime + " / " + revision.Runtime.Target},
			{Label: "Charter binding", Value: fmt.Sprintf("%s revision %d @ %s", revision.Charter.ID, revision.Charter.Revision, revision.Charter.Digest)},
			{Label: "Current revision", Value: fmt.Sprintf("%d @ %s", revision.Revision, revision.Digest)},
			{Label: "Revision history", Value: fmt.Sprintf("%d immutable revision%s", revision.Revision, plural(int(revision.Revision)))},
			{Label: "Declared capabilities", Value: capabilities},
			{Label: "Declared policies", Value: strings.Join(policies, "\n")},
			{Label: "Effective authority", Value: "Not evaluated by this Registry read; execution requires fresh stanza and mandate admission"},
			{Label: "Provisioning evidence", Value: "Not present on the Agent Registry revision"},
		},
	}
}

func consoleLoopRecord(view app.LoopView) consoleweb.RecordModel {
	revision := view.Revision
	steps := make([]string, 0, len(revision.Steps))
	maxAttempts := uint16(0)
	claimCount := 0
	for _, step := range revision.Steps {
		steps = append(steps, fmt.Sprintf("%s · %s · max %d attempt(s)", step.ID, step.Kind, step.Retry.MaxAttempts))
		if step.Retry.MaxAttempts > maxAttempts {
			maxAttempts = step.Retry.MaxAttempts
		}
		claimCount += len(step.EvidenceClaims)
	}
	transitions := make([]string, 0, len(revision.Transitions))
	for _, transition := range revision.Transitions {
		transitions = append(transitions, fmt.Sprintf("%s: %s → %s", transition.ID, transition.FromStepID, transition.ToStepID))
	}
	inputs := make([]string, 0, len(revision.Inputs))
	for _, input := range revision.Inputs {
		inputs = append(inputs, fmt.Sprintf("%s · %s · required=%t", input.ID, input.Type, input.Required))
	}
	outputs := make([]string, 0, len(revision.Outputs))
	for _, output := range revision.Outputs {
		outputs = append(outputs, fmt.Sprintf("%s · %s · required=%t", output.ID, output.Type, output.Required))
	}
	requirements := make([]string, 0, len(revision.RequiredEvidence))
	for _, requirement := range revision.RequiredEvidence {
		requirements = append(requirements, fmt.Sprintf("%s · producer %s", requirement.Claim, requirement.ProducerStepID))
	}
	validation := "missing"
	if len(view.Validations) > 0 {
		validation = fmt.Sprintf("%s · %s %s", view.Validations[0].Outcome, view.Validations[0].Validator.ID, view.Validations[0].Validator.Version)
	}
	lifecycle := string(view.Lifecycle.State)
	readiness := "Draft; activation requires authenticated lifecycle admission"
	if view.Lifecycle.State == "active" {
		if view.Lifecycle.ActiveRevision == revision.Revision && view.Lifecycle.ActiveDigest == revision.Digest {
			readiness = "Active exact revision; fresh authority admission still required at execution"
		} else {
			lifecycle = "inactive"
			readiness = "Published immutable revision; active Loop is " + exactRevisionLabel(revision.LoopID, view.Lifecycle.ActiveRevision, view.Lifecycle.ActiveDigest)
		}
	} else if view.Lifecycle.State == "retired" {
		readiness = "Retired; terminal lifecycle"
	}
	return consoleweb.RecordModel{
		Key: revision.LoopID + ":" + strconv.FormatUint(revision.Revision, 10), Label: revision.LoopID,
		Summary:   fmt.Sprintf("revision %d · %d steps · %d transitions", revision.Revision, len(revision.Steps), len(revision.Transitions)),
		Lifecycle: lifecycle, Readiness: readiness, Revision: fmt.Sprintf("r%d", revision.Revision),
		Runtime: view.Provenance.Runtime.Runtime, Source: view.Provenance.PublisherAgent.ID, Authority: view.Provenance.Authority.ID,
		Fields: []consoleweb.FieldModel{
			{Label: "Executable steps", Value: strings.Join(steps, "\n")},
			{Label: "Transitions", Value: strings.Join(transitions, "\n")},
			{Label: "Inputs", Value: fallback(strings.Join(inputs, "\n"), "None")},
			{Label: "Outputs", Value: fallback(strings.Join(outputs, "\n"), "None")},
			{Label: "Retry bound", Value: fmt.Sprintf("maximum %d attempts on any step", maxAttempts)},
			{Label: "Evidence contract", Value: fmt.Sprintf("%d step claims\n%s", claimCount, fallback(strings.Join(requirements, "\n"), "No Loop-level requirements"))},
			{Label: "Validation", Value: validation},
			{Label: "Lifecycle history", Value: fmt.Sprintf("%d immutable event(s)", len(view.History))},
			{Label: "Publisher Agent", Value: fmt.Sprintf("%s revision %d @ %s", view.Provenance.PublisherAgent.ID, view.Provenance.PublisherAgent.Revision, view.Provenance.PublisherAgent.Digest)},
			{Label: "Authority provenance", Value: fmt.Sprintf("%s · mandate %s · stanza %s", view.Provenance.Authority.ID, view.Provenance.MandateID, view.Provenance.StanzaID)},
			{Label: "Immutable revision", Value: fmt.Sprintf("%s revision %d @ %s", revision.LoopID, revision.Revision, revision.Digest)},
		},
	}
}

func consoleGraphRecord(view app.GraphView, history app.SubmissionHistory, raw string) consoleweb.RecordModel {
	revision := view.Revision
	detail := &consoleweb.GraphDetailModel{
		Digest: revision.Digest, PreviousDigest: fallback(revision.PreviousDigest, "Genesis revision"),
		Validation: "unavailable", InputSchema: []consoleweb.FieldModel{}, OutputSchema: []consoleweb.FieldModel{},
		Nodes: []consoleweb.GraphNodeModel{}, Edges: []consoleweb.GraphEdgeModel{}, Policies: []consoleweb.FieldModel{},
		AcceptedRuns: []consoleweb.GraphRunModel{}, RejectedSubmissions: []consoleweb.FieldModel{},
	}
	if len(view.Validations) > 0 {
		detail.Validation = string(view.Validations[0].Outcome) + " · " + view.Validations[0].Digest
	}
	for _, port := range revision.Inputs {
		detail.InputSchema = append(detail.InputSchema, consoleweb.FieldModel{Label: port.ID, Value: fmt.Sprintf("%s · required %t", port.Type, port.Required)})
	}
	for _, port := range revision.Outputs {
		detail.OutputSchema = append(detail.OutputSchema, consoleweb.FieldModel{Label: port.ID, Value: fmt.Sprintf("%s · required %t", port.Type, port.Required)})
	}
	for _, node := range revision.Nodes {
		inputs := make([]string, 0, len(node.Inputs))
		for _, port := range node.Inputs {
			inputs = append(inputs, fmt.Sprintf("%s:%s required=%t", port.ID, port.Type, port.Required))
		}
		if len(inputs) == 0 {
			inputs = append(inputs, "No ports")
		}
		outputs := make([]string, 0, len(node.Outputs))
		for _, port := range node.Outputs {
			outputs = append(outputs, fmt.Sprintf("%s:%s required=%t", port.ID, port.Type, port.Required))
		}
		if len(outputs) == 0 {
			outputs = append(outputs, "No ports")
		}
		detail.Nodes = append(detail.Nodes, consoleweb.GraphNodeModel{
			ID: node.ID, Participant: exactRevisionLabel(node.Participant.ID, node.Participant.Revision, node.Participant.Digest),
			Loop: exactRevisionLabel(node.Loop.ID, node.Loop.Revision, node.Loop.Digest), Inputs: strings.Join(inputs, ", "), Outputs: strings.Join(outputs, ", "),
		})
	}
	for _, dependency := range revision.Dependencies {
		mappings := make([]string, 0, len(dependency.Mappings))
		for _, mapping := range dependency.Mappings {
			mappings = append(mappings, mapping.FromPort+" → "+mapping.ToPort)
		}
		detail.Edges = append(detail.Edges, consoleweb.GraphEdgeModel{ID: dependency.ID, From: dependency.FromNodeID, To: dependency.ToNodeID, Mappings: strings.Join(mappings, ", ")})
	}
	for _, rule := range revision.AdmissionRules {
		detail.Policies = append(detail.Policies, consoleweb.FieldModel{Label: rule.ID, Value: rule.PolicyRef.ID + " @ " + rule.PolicyRef.Digest})
	}
	for _, accepted := range view.Runs {
		inputs := make([]string, 0, len(accepted.Snapshot.Inputs))
		for _, input := range accepted.Snapshot.Inputs {
			inputs = append(inputs, input.PortID+" ("+string(input.Type)+") = "+string(input.Value))
		}
		detail.AcceptedRuns = append(detail.AcceptedRuns, consoleweb.GraphRunModel{
			Submission: accepted.Submission.SubmissionID + " @ " + accepted.Submission.Digest,
			Snapshot:   accepted.Snapshot.SnapshotID + " @ " + accepted.Snapshot.Digest,
			QueueItem:  accepted.QueueItem.ItemID + " @ " + accepted.QueueItem.Digest,
			GraphRun:   accepted.GraphRun.GraphRunID, Authority: accepted.Submission.Authority.ID + " @ " + accepted.Submission.Authority.Digest,
			Mandate: accepted.Submission.MandateID, Runtime: accepted.Submission.Runtime, Inputs: strings.Join(inputs, "\n"),
		})
	}
	for _, rejection := range history.Rejected {
		detail.RejectedSubmissions = append(detail.RejectedSubmissions, consoleweb.FieldModel{Label: rejection.SubmissionID, Value: rejection.ReasonCode + " · " + rejection.Reason})
	}
	lifecycle := string(view.Lifecycle.State)
	readiness := "Draft; activation requires authenticated lifecycle admission"
	if view.Lifecycle.State == "active" {
		if view.Lifecycle.ActiveRevision == revision.Revision && view.Lifecycle.ActiveDigest == revision.Digest {
			readiness = "Active exact revision; fresh authority admission still required at execution"
		} else {
			lifecycle = "inactive"
			readiness = "Published immutable revision; active Graph is " + exactRevisionLabel(revision.GraphID, view.Lifecycle.ActiveRevision, view.Lifecycle.ActiveDigest)
		}
	} else if view.Lifecycle.State == "retired" {
		readiness = "Retired; terminal lifecycle"
	}
	return consoleweb.RecordModel{
		Key: revision.GraphID + ":" + strconv.FormatUint(revision.Revision, 10), Label: revision.GraphID,
		Summary: fmt.Sprintf("revision %d · %d nodes · %d dependencies", revision.Revision, len(revision.Nodes), len(revision.Dependencies)),
		JSON:    raw, Lifecycle: lifecycle, Readiness: readiness, Revision: fmt.Sprintf("r%d", revision.Revision), Graph: detail,
	}
}

func consoleQueueRecord(view app.QueueExecutionView) consoleweb.RecordModel {
	state := queuePhase(view)
	detail := &consoleweb.QueueDetailModel{
		QueueItem: []consoleweb.FieldModel{
			{Label: "Queue item", Value: view.Item.ItemID + " @ " + view.Item.Digest},
			{Label: "Submission", Value: view.Item.Submission.ID + " @ " + view.Item.Submission.Digest},
			{Label: "Mandate", Value: queueMandateLabel(view)},
			{Label: "Snapshot", Value: view.Item.Snapshot.ID + " @ " + view.Item.Snapshot.Digest},
			{Label: "Authority", Value: view.Item.Authority.ID + " @ " + view.Item.Authority.Digest},
			{Label: "Enqueued", Value: consoleTime(view.Item.EnqueuedAt)},
			{Label: "Available", Value: consoleTime(view.Projection.AvailableAt)},
			{Label: "Attempt bound", Value: fmt.Sprintf("%d maximum · %d recorded by projection", view.Item.MaxAttempts, view.Projection.Attempts)},
		},
		Runtime: []consoleweb.FieldModel{
			{Label: "Adapter", Value: fallback(view.Runtime.Adapter, "Unavailable")},
			{Label: "Runtime", Value: fallback(view.Runtime.Runtime, "Unavailable")},
			{Label: "Target", Value: fallback(view.Runtime.Target, "Unavailable")},
		},
		GraphRun: consoleweb.QueueExecutionNodeModel{ID: view.GraphRun.GraphRunID, Kind: "Graph run", State: string(view.GraphRun.State), Binding: view.GraphRun.Snapshot.ID + " @ " + view.GraphRun.Snapshot.Digest, Digest: view.GraphRun.Digest},
		Loops:    []consoleweb.QueueExecutionNodeModel{}, Attempts: []consoleweb.QueueAttemptModel{}, Timeline: []consoleweb.QueueTimelineModel{}, Receipts: []consoleweb.QueueReceiptModel{},
		ArtifactState:    "Unavailable — no authoritative runtime artifact is attached.",
		ReceiptState:     "Unavailable — no authoritative verifier receipt is attached.",
		DispositionState: "Pending — no authoritative terminal disposition is attached.",
	}
	for _, dependency := range view.Item.Dependencies {
		detail.Dependencies = append(detail.Dependencies, consoleweb.FieldModel{Label: dependency.ID, Value: queueDependencyValue(view, dependency.ID)})
	}
	detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Queued", State: string(view.Item.State), At: consoleTime(view.Item.EnqueuedAt), Detail: view.Item.ItemID})
	for _, child := range view.LoopExecutions {
		detail.Loops = append(detail.Loops, consoleweb.QueueExecutionNodeModel{ID: child.LoopExecutionID, Kind: "Loop execution · " + child.GraphNodeID, State: string(child.State), Binding: exactRevisionLabel(child.Loop.ID, child.Loop.Revision, child.Loop.Digest) + " · participant " + exactRevisionLabel(child.Participant.ID, child.Participant.Revision, child.Participant.Digest), Digest: child.Digest})
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Loop execution", State: string(child.State), At: consoleTime(child.CreatedAt), Detail: child.LoopExecutionID + " · node " + child.GraphNodeID})
	}
	for _, attempt := range view.Attempts {
		detail.Attempts = append(detail.Attempts, consoleweb.QueueAttemptModel{ID: attempt.AttemptID, Number: attempt.AttemptNumber, State: string(attempt.State), LoopID: attempt.LoopExecutionID, ClaimID: attempt.ClaimID, Created: consoleTime(attempt.CreatedAt), Digest: attempt.Digest})
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: fmt.Sprintf("Attempt %d", attempt.AttemptNumber), State: string(attempt.State), At: consoleTime(attempt.CreatedAt), Detail: attempt.AttemptID + " · claim " + fallback(attempt.ClaimID, "unavailable")})
	}
	for _, claim := range view.Claims {
		detail.Claims = append(detail.Claims, consoleweb.FieldModel{Label: claim.ClaimID, Value: claim.WorkerID + " · " + consoleTime(claim.ClaimedAt) + " through " + consoleTime(claim.ExpiresAt)})
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Claimed by " + claim.WorkerID, State: "claimed", At: consoleTime(claim.ClaimedAt), Detail: claim.ClaimID})
	}
	for _, transition := range view.Transitions {
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Queue transition", State: string(transition.To), At: consoleTime(transition.OccurredAt), Detail: string(transition.From) + " → " + string(transition.To) + " · " + transition.Reason})
	}
	for _, retry := range view.Retries {
		label := "Retry"
		if retry.Reclaimed {
			label = "Expired lease reclaimed"
		}
		detail.Retries = append(detail.Retries, consoleweb.FieldModel{Label: retry.RetryID, Value: fmt.Sprintf("attempt %d · available %s · %s", retry.AttemptNumber, consoleTime(retry.AvailableAt), retry.Reason)})
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: label, State: "retrying", At: consoleTime(retry.OccurredAt), Detail: retry.RetryID + " · " + retry.Reason})
	}
	for _, cancellation := range view.Cancellations {
		detail.Cancellations = append(detail.Cancellations, consoleweb.FieldModel{Label: cancellation.CancellationID, Value: cancellation.Reason})
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Lifecycle terminalized", State: state, At: consoleTime(cancellation.OccurredAt), Detail: cancellation.CancellationID + " · " + cancellation.Reason})
	}
	detail.Controls = queueControls(view, state)
	if view.Artifact != nil {
		detail.ArtifactState = "Authoritative runtime artifact"
		detail.Artifact = []consoleweb.FieldModel{{Label: "Artifact", Value: view.Artifact.ID}, {Label: "Attempt", Value: view.Artifact.AttemptID}, {Label: "Action / run", Value: view.Artifact.ActionID + " / " + view.Artifact.RunID}, {Label: "Digest / content reference", Value: view.Artifact.Digest + " / " + view.Artifact.ContentRef}, {Label: "Media type", Value: view.Artifact.MediaType}, {Label: "Created", Value: consoleTime(view.Artifact.CreatedAt)}}
	}
	for _, receipt := range view.Receipts {
		detail.Receipts = append(detail.Receipts, consoleweb.QueueReceiptModel{ID: receipt.ID, Outcome: string(receipt.Outcome), Claim: receipt.Claim, Verifier: receipt.VerifierID + " / " + receipt.PolicyVersion, ExpectedDigest: receipt.ExpectedDigest, ObservedDigest: fallback(receipt.ObservedDigest, "Unavailable"), FailureCategory: fallback(receipt.FailureCategory, "None recorded"), ObservedAt: consoleTime(receipt.ObservedAt)})
	}
	if len(detail.Receipts) > 0 {
		detail.ReceiptState = "Authoritative verifier receipts"
	}
	if view.Disposition != nil {
		detail.DispositionState = "Authoritative terminal disposition"
		detail.Disposition = []consoleweb.FieldModel{{Label: "Disposition", Value: view.Disposition.DispositionID + " @ " + view.Disposition.Digest}, {Label: "State", Value: string(view.Disposition.State)}, {Label: "Reason code", Value: view.Disposition.ReasonCode}, {Label: "Attempt", Value: view.Disposition.AttemptID}, {Label: "Occurred", Value: consoleTime(view.Disposition.OccurredAt)}}
		detail.Timeline = append(detail.Timeline, consoleweb.QueueTimelineModel{Title: "Disposition", State: string(view.Disposition.State), At: consoleTime(view.Disposition.OccurredAt), Detail: view.Disposition.ReasonCode})
	}
	sort.SliceStable(detail.Timeline, func(i, j int) bool {
		if detail.Timeline[i].At == detail.Timeline[j].At {
			return detail.Timeline[i].Title < detail.Timeline[j].Title
		}
		return detail.Timeline[i].At < detail.Timeline[j].At
	})
	return consoleweb.RecordModel{Key: view.Item.ItemID, Label: view.Item.ItemID, Summary: view.GraphRun.GraphRunID + " · " + state, Lifecycle: state, Runtime: fallback(view.Runtime.Runtime, "Unavailable"), Revision: view.Item.Snapshot.ID, Queue: detail}
}

// consoleCredentialRecord builds the metadata-only surface model for a single
// authoritative encrypted credential record. It must never include plaintext
// values, ciphertext, wrapped DEKs, nonces, or KEK bytes. The only KEK field
// it surfaces is the immutable version (for ops history).
func consoleCredentialRecord(cred app.CredentialView, vault app.VaultStatusView) consoleweb.RecordModel {
	lifecycle := "active"
	readiness := "Active exact revision; secret value resolvable only via authenticated API/CLI authority admission"
	if cred.Status == "revoked" {
		lifecycle = "revoked"
		readiness = "Revoked; secret value reads are denied"
	}
	versions := make([]consoleweb.FieldModel, 0, len(cred.VersionHistory))
	versionDetails := make([]consoleweb.CredentialVersionDetail, 0, len(cred.VersionHistory))
	for _, version := range cred.VersionHistory {
		digest := version.CiphertextHash
		if digest == "" {
			digest = "Unavailable"
		}
		versions = append(versions, consoleweb.FieldModel{
			Label: fmt.Sprintf("v%d", version.Version),
			Value: fmt.Sprintf("algorithm %s · KEK v%d · digest %s · created %s", version.Algorithm, version.KEKVersion, digest, consoleTime(version.CreatedAt)),
		})
		versionDetails = append(versionDetails, consoleweb.CredentialVersionDetail{
			Version:        version.Version,
			Algorithm:      version.Algorithm,
			KEKVersion:     version.KEKVersion,
			CiphertextHash: version.CiphertextHash,
			CreatedAt:      consoleTime(version.CreatedAt),
		})
	}
	fields := []consoleweb.FieldModel{
		{Label: "Stable record ID", Value: fallback(cred.ID, "Unavailable")},
		{Label: "Reference", Value: fallback(cred.Reference, "Unavailable")},
		{Label: "Kind", Value: fallback(cred.Kind, "Unavailable")},
		{Label: "Status", Value: fallback(cred.Status, "Unavailable")},
		{Label: "Current version", Value: fmt.Sprintf("v%d", cred.CurrentVersion)},
		{Label: "Created", Value: fallback(cred.CreatedAt, "Unavailable")},
		{Label: "Created by", Value: fallback(cred.CreatedBy, "Unavailable")},
		{Label: "Bindings", Value: fmt.Sprintf("%d credential binding(s)", cred.BindingCount)},
	}
	if cred.Status == "revoked" {
		fields = append(fields, consoleweb.FieldModel{Label: "Revoked at", Value: fallback(cred.RevokedAt, "Unavailable")})
		fields = append(fields, consoleweb.FieldModel{Label: "Revocation reason", Value: fallback(cred.Revocation, "Unavailable")})
	}
	joined := make([]string, 0, len(versions))
	for _, v := range versions {
		joined = append(joined, v.Label+": "+v.Value)
	}
	fields = append(fields, consoleweb.FieldModel{Label: "Version history", Value: fallback(strings.Join(joined, "\n"), "No encrypted versions")})
	summary := cred.Reference
	if summary == "" {
		summary = cred.ID
	}
	if cred.Kind != "" {
		summary = summary + " · " + cred.Kind
	}
	recordLabel := fallback(cred.Reference, cred.ID)
	if cred.Status != "" {
		recordLabel = fmt.Sprintf("%s · %s · v%d", recordLabel, cred.Status, cred.CurrentVersion)
	}
	detail := &consoleweb.CredentialDetailModel{
		Reference:      fallback(cred.Reference, ""),
		Kind:           fallback(cred.Kind, ""),
		Status:         fallback(cred.Status, ""),
		CurrentVersion: cred.CurrentVersion,
		CreatedAt:      fallback(cred.CreatedAt, ""),
		CreatedBy:      fallback(cred.CreatedBy, ""),
		RevokedAt:      cred.RevokedAt,
		Revocation:     cred.Revocation,
		BindingCount:   cred.BindingCount,
		Versions:       versionDetails,
		Vault: consoleweb.CredentialVaultDetail{
			DeploymentID:      vault.DeploymentID,
			StoreID:           vault.StoreID,
			KEKID:             vault.KEKID,
			KEKVersion:        vault.KEKVersion,
			SchemaVersion:     vault.SchemaVersion,
			Custody:           vault.Custody,
			LastCleanShutdown: vault.LastCleanShutdown,
			InitializedAt:     consoleTime(vault.InitializedAt),
			State:             vault.State,
			ReasonCode:        vault.ReasonCode,
		},
		Backup: consoleweb.CredentialBackupDetail{
			Available:  cred.Backup.Available,
			TargetPath: cred.Backup.TargetPath,
			Note:       "Backups are ciphertext-only snapshots; the same KEK is required to reopen.",
		},
		Proposal: credentialProposal(cred, vault),
	}
	return consoleweb.RecordModel{
		Key:        cred.ID,
		Label:      recordLabel,
		Summary:    summary,
		JSON:       credentialJSON(cred, vault),
		Lifecycle:  lifecycle,
		Readiness:  readiness,
		Revision:   fmt.Sprintf("v%d", cred.CurrentVersion),
		Source:     vaultSourceLabel(vault),
		Owner:      "operator",
		Fields:     fields,
		Credential: detail,
	}
}

// credentialProposal builds the review-only CLI previews rendered in the
// inspector. The strings are documentation; the browser never POSTs them.
func credentialProposal(cred app.CredentialView, vault app.VaultStatusView) consoleweb.CredentialProposalDetail {
	reference := cred.Reference
	if reference == "" {
		reference = "provider:NAME"
	}
	put := fmt.Sprintf("aegis secret put %s --kind %s --created-by \"$OPERATOR\"", reference, fallback(cred.Kind, "opaque"))
	backup := "aegis secret backup"
	return consoleweb.CredentialProposalDetail{
		PutCommand:    put,
		BackupCommand: backup,
		Notice:        "Browser state cannot authorize credential mutation. The previews are copy-paste review; running them requires an authenticated operator session and the configured KEK.",
	}
}

// credentialJSON serializes the authoritative credential view for the
// inspector JSON tab. It deliberately omits every secret-bearing field
// (RecordNonce, Ciphertext, WrappedDEK, KEK bytes) and is asserted by the
// templ source test.
func credentialJSON(cred app.CredentialView, vault app.VaultStatusView) string {
	versions := make([]map[string]any, 0, len(cred.VersionHistory))
	for _, v := range cred.VersionHistory {
		versions = append(versions, map[string]any{
			"version":         v.Version,
			"format_version":  v.FormatVersion,
			"algorithm":       v.Algorithm,
			"kek_version":     v.KEKVersion,
			"created_at":      consoleTime(v.CreatedAt),
			"ciphertext_hash": v.CiphertextHash,
		})
	}
	envelope := map[string]any{
		"id":                cred.ID,
		"reference":         cred.Reference,
		"kind":              cred.Kind,
		"status":            cred.Status,
		"current_version":   cred.CurrentVersion,
		"created_at":        cred.CreatedAt,
		"created_by":        cred.CreatedBy,
		"revoked_at":        cred.RevokedAt,
		"revocation_reason": cred.Revocation,
		"binding_count":     cred.BindingCount,
		"version_history":   versions,
		"vault": map[string]any{
			"deployment_id":       vault.DeploymentID,
			"store_id":            vault.StoreID,
			"kek_id":              vault.KEKID,
			"kek_version":         vault.KEKVersion,
			"schema_version":      vault.SchemaVersion,
			"custody":             vault.Custody,
			"last_clean_shutdown": vault.LastCleanShutdown,
		},
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Sprintf("credential view: %s", cred.ID)
	}
	return string(data)
}

func vaultSourceLabel(vault app.VaultStatusView) string {
	if vault.DeploymentID == "" {
		return "credentials.authority.unconfigured"
	}
	return "credentials.authority.bbolt · " + vault.DeploymentID
}

func queuePhase(view app.QueueExecutionView) string {
	state := string(view.Projection.State)
	if view.Disposition != nil && view.Disposition.ReasonCode == "retry_exhausted" {
		return "exhausted"
	}
	if state == "claimed" {
		return "active"
	}
	if state == "queued" && len(view.Retries) > 0 {
		if !view.Projection.AvailableAt.After(time.Now().UTC()) {
			return "claimable"
		}
		return "retrying"
	}
	if state == "queued" && !view.Projection.AvailableAt.After(time.Now().UTC()) {
		return "claimable"
	}
	return state
}

// queueMandateLabel renders the resolved mandate binding for the queued item.
// Empty submission or missing authority fall back to "Unavailable" rather than
// fabricating a mandate identifier.
func queueMandateLabel(view app.QueueExecutionView) string {
	if view.Submission.SubmissionID == "" {
		return "Unavailable"
	}
	if view.Submission.MandateID == "" {
		return "Unavailable — mandate not admitted with submission"
	}
	return view.Submission.MandateID
}

// queueDependencyValue resolves an exact authoritative dependency outcome.
// The dependency carries only an immutable DigestRef; the resolved outcome
// comes from the LoopExecution's authoritative state or, when terminalized,
// from the terminal disposition.
func queueDependencyValue(view app.QueueExecutionView, id string) string {
	if id == "" {
		return "Unavailable"
	}
	for _, child := range view.LoopExecutions {
		if child.LoopExecutionID != id {
			continue
		}
		state := string(child.State)
		switch child.State {
		case "succeeded", "failed", "denied", "cancelled", "expired", "revoked":
			return exactRevisionLabel(child.Loop.ID, child.Loop.Revision, child.Loop.Digest) + " · outcome " + state
		case "requested":
			return exactRevisionLabel(child.Loop.ID, child.Loop.Revision, child.Loop.Digest) + " · outcome pending"
		case "started":
			return exactRevisionLabel(child.Loop.ID, child.Loop.Revision, child.Loop.Digest) + " · outcome in_progress"
		default:
			return exactRevisionLabel(child.Loop.ID, child.Loop.Revision, child.Loop.Digest) + " · outcome " + state
		}
	}
	if view.Disposition != nil && view.Disposition.QueueItem.ID != "" {
		return "no exact dependency record · terminal " + string(view.Disposition.State) + " · " + view.Disposition.ReasonCode
	}
	return "no exact dependency record"
}

func queueControls(view app.QueueExecutionView, phase string) []consoleweb.QueueControlModel {
	terminal := phase == "cancelled" || phase == "expired" || phase == "denied" || phase == "failed" || phase == "exhausted" || phase == "succeeded"
	active := phase == "active"
	leaseExpired := active && len(view.Claims) > 0 && !view.Claims[len(view.Claims)-1].ExpiresAt.After(time.Now().UTC())
	retryBudget := view.Projection.Attempts < view.Item.MaxAttempts
	control := func(label string, enabled bool, reason string) consoleweb.QueueControlModel {
		if enabled {
			reason = "eligible; authenticated API/CLI authority admission required"
		}
		return consoleweb.QueueControlModel{Label: label, Enabled: enabled, Reason: reason}
	}
	return []consoleweb.QueueControlModel{
		control("Retry now", false, "live manual retry is denied; no authoritative durable runtime-stop acknowledgement exists"),
		control("Reclaim expired lease", active && retryBudget && leaseExpired, "requires an expired active lease and remaining pinned retry budget"),
		control("Cancel execution", !terminal, "terminal work cannot transition"),
		control("Expire execution", leaseExpired, "requires an expired active lease"),
		control("Mark retry exhausted", active && !retryBudget, "requires the final active attempt and exhausted pinned retry budget"),
		control("Record authority revocation", !terminal, "terminal work cannot transition"),
	}
}

func consoleTime(value time.Time) string {
	if value.IsZero() {
		return "Unavailable"
	}
	return value.UTC().Format(time.RFC3339)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func exactRevisionLabel(id string, revision uint64, digest string) string {
	return fmt.Sprintf("%s r%d @ %s", id, revision, digest)
}

// filterConsoleAgents filters the Agent Registry surface by stable ID substring
// and exact lifecycle. Lifecycle is one of "all", "enabled", "disabled",
// "retired". The reference search is exact-after-prefix and bounded.
func filterConsoleAgents(model *consoleweb.SurfaceModel, query, lifecycle string) error {
	query = strings.TrimSpace(query)
	if len(query) > 128 || strings.ContainsAny(query, "\r\n\x00") {
		return errors.New("invalid Agent Registry search")
	}
	if lifecycle == "" {
		lifecycle = "all"
	}
	switch lifecycle {
	case "all", "enabled", "disabled", "retired":
	default:
		return errors.New("invalid Agent Registry lifecycle filter")
	}
	model.Query, model.Lifecycle = query, lifecycle
	filtered := make([]consoleweb.RecordModel, 0, len(model.Records))
	needle := strings.ToLower(query)
	for _, record := range model.Records {
		if lifecycle != "all" && record.Lifecycle != lifecycle {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{record.Label, record.Runtime, record.Source, record.Owner}, " "))
		if needle != "" && !strings.Contains(haystack, needle) {
			continue
		}
		filtered = append(filtered, record)
	}
	model.Records = filtered
	if model.State == "ready" && len(filtered) == 0 {
		model.State = "filtered-empty"
	}
	return nil
}

// filterConsoleCredentials filters the credentials surface by status and an
// exact reference substring. Status is one of "all", "active", "revoked".
// Reference search is exact-after-prefix and bounded.
func filterConsoleCredentials(model *consoleweb.SurfaceModel, query, status string) error {
	query = strings.TrimSpace(query)
	if len(query) > 128 || strings.ContainsAny(query, "\r\n\x00") {
		return errors.New("invalid Credentials search")
	}
	if status == "" {
		status = "all"
	}
	switch status {
	case "all", "active", "revoked":
	default:
		return errors.New("invalid Credentials status filter")
	}
	model.Query, model.Lifecycle = query, status
	filtered := make([]consoleweb.RecordModel, 0, len(model.Records))
	needle := strings.ToLower(query)
	for _, record := range model.Records {
		if status != "all" && record.Lifecycle != status {
			continue
		}
		haystack := strings.ToLower(strings.Join([]string{record.Label, record.Summary, record.Owner}, " "))
		if needle != "" && !strings.Contains(haystack, needle) {
			continue
		}
		filtered = append(filtered, record)
	}
	model.Records = filtered
	if model.State == "ready" && len(filtered) == 0 {
		model.State = "filtered-empty"
	}
	return nil
}

func filterConsoleQueue(model *consoleweb.SurfaceModel, state string) error {
	if state == "" {
		state = "all"
	}
	switch state {
	case "all", "queued", "claimable", "active", "retrying", "cancelled", "expired", "denied", "failed", "exhausted", "succeeded":
	default:
		return errors.New("invalid Execution Queue state filter")
	}
	model.QueueState = state
	if state == "all" {
		return nil
	}
	filtered := make([]consoleweb.RecordModel, 0, len(model.Records))
	for _, record := range model.Records {
		if record.Lifecycle == state {
			filtered = append(filtered, record)
		}
	}
	model.Records = filtered
	if model.State == "ready" && len(filtered) == 0 {
		model.State = "filtered-empty"
	}
	return nil
}

func consoleActions(surface app.FleetSurface, domain consoleDomain) []consoleweb.ActionModel {
	type actionSpec struct {
		key, label string
		primary    bool
	}
	var specs []actionSpec
	switch domain {
	case consoleAgents:
		specs = []actionSpec{{"register_fleet_agent", "Prepare charter import", true}}
	case consoleLoops:
		specs = []actionSpec{{"loop_publish", "Publish Loop revision", true}}
	case consoleGraphs:
		specs = []actionSpec{{"graph_publish", "Publish Graph revision", true}, {"submission", "Prepare execution request", false}}
	case consoleQueue:
		specs = []actionSpec{{"submission", "Prepare execution request", true}, {"queue_claim", "Claim", false}, {"runtime_effect", "Runtime effect", false}, {"evidence_verify", "Verify evidence", false}, {"disposition", "Disposition", false}}
	case consoleCredentials:
		specs = []actionSpec{{"prepare_credential", "Prepare credential", true}, {"prepare_vault_backup", "Prepare vault backup", false}}
	}
	actions := make([]consoleweb.ActionModel, 0, len(specs))
	for _, spec := range specs {
		readiness, ok := surface.Actions[spec.key]
		if !ok {
			actions = append(actions, consoleweb.ActionModel{Key: spec.key, Label: spec.label, State: "unavailable", ReasonCode: "action_readiness_missing", Primary: spec.primary})
			continue
		}
		repairs := make([]string, 0, len(readiness.RepairActions))
		for _, repair := range readiness.RepairActions {
			repairs = append(repairs, string(repair))
		}
		actions = append(actions, consoleweb.ActionModel{Key: spec.key, Label: spec.label, State: string(readiness.State), ReasonCode: readiness.ReasonCode, RepairActions: repairs, Primary: spec.primary})
	}
	return actions
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func consoleRecordLabel(domain consoleDomain, value any) string {
	switch domain {
	case consoleAgents:
		if record, ok := value.(app.FleetAgent); ok {
			return fmt.Sprintf("%s · revision %d", record.Registration.AgentID, record.Revision.Revision)
		}
	case consoleLoops:
		if record, ok := value.(app.LoopView); ok {
			return fmt.Sprintf("%s · revision %d", record.Revision.LoopID, record.Revision.Revision)
		}
	case consoleGraphs:
		if record, ok := value.(app.GraphView); ok {
			return fmt.Sprintf("%s · revision %d", record.Revision.GraphID, record.Revision.Revision)
		}
	case consoleQueue:
		if record, ok := value.(app.QueueExecutionView); ok {
			return fmt.Sprintf("%s · %s", record.Item.ItemID, record.Projection.State)
		}
	case consoleCredentials:
		if record, ok := value.(app.CredentialView); ok {
			label := record.Reference
			if label == "" {
				label = record.ID
			}
			return fmt.Sprintf("%s · %s · v%d", label, record.Status, record.CurrentVersion)
		}
	}
	return "unknown record"
}

func fallback(value, replacement string) string {
	if value == "" {
		return replacement
	}
	return value
}

func selectConsoleRecord(model *consoleweb.SurfaceModel, raw string) error {
	for index := range model.Records {
		if model.Records[index].Key == raw {
			model.Inspector = &model.Records[index]
			model.InspectorOpen = true
			return nil
		}
	}
	// Preserve bounded numeric selectors for existing non-typed collection URLs.
	if model.Domain != string(consoleAgents) && model.Domain != string(consoleGraphs) {
		index, err := strconv.Atoi(raw)
		if err == nil && index >= 0 && index < len(model.Records) {
			model.Inspector = &model.Records[index]
			model.InspectorOpen = true
			return nil
		}
	}
	return errors.New("unknown console record")
}
