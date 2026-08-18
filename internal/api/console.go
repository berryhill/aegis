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
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/berryhill/aegis/internal/app"
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
		model.Title, model.Eyebrow, model.Description = "Credentials", "Configured bindings", "Configured provider-auth bindings, presented as metadata only. Secret values and environment mappings are never read or shown here."
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
		}
		model.Records = append(model.Records, record)
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
			return fmt.Sprintf("%s · %s binding", record.ID, record.Type)
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
	if model.Domain == string(consoleAgents) {
		for index := range model.Records {
			if model.Records[index].Key == raw {
				model.Inspector = &model.Records[index]
				model.InspectorOpen = true
				return nil
			}
		}
		return errors.New("unknown console record")
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index >= len(model.Records) {
		return errors.New("unknown console record")
	}
	model.Inspector = &model.Records[index]
	model.InspectorOpen = true
	return nil
}
