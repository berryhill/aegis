package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
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

type consoleDomain string

const (
	consoleAgents consoleDomain = "agents"
	consoleLoops  consoleDomain = "loops"
	consoleGraphs consoleDomain = "graphs"
	consoleQueue  consoleDomain = "queue"
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
	case consoleAgents, consoleLoops, consoleGraphs, consoleQueue:
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
		model.Title, readinessKey = "Agent Registry", "registry"
		for _, value := range surface.Agents {
			values = append(values, value)
		}
	case consoleLoops:
		model.Title = "Loops"
		for _, value := range surface.Loops {
			values = append(values, value)
		}
	case consoleGraphs:
		model.Title = "Graphs"
		for _, value := range surface.Graphs {
			values = append(values, value)
		}
	case consoleQueue:
		model.Title = "Execution Queue"
		for _, value := range surface.Queue {
			values = append(values, value)
		}
	default:
		return consoleweb.SurfaceModel{}, errors.New("unknown console domain")
	}
	readiness, ok := surface.Readiness[readinessKey]
	if !ok || !readiness.Authoritative {
		model.State = "unavailable"
		model.Status = model.Title + " state is unavailable."
		return model, nil
	}
	if len(values) == 0 {
		model.State = "empty"
	}
	shown := len(values)
	count := strconv.Itoa(readiness.Count)
	if shown != readiness.Count {
		count = fmt.Sprintf("showing %d of %d", shown, readiness.Count)
	}
	model.Status = fmt.Sprintf("%s authoritative %s record%s. Exact revisions and digests shown.", count, strings.ToLower(model.Title), plural(readiness.Count))
	for index, value := range values {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return consoleweb.SurfaceModel{}, err
		}
		label := consoleRecordLabel(domain, value)
		model.Records = append(model.Records, consoleweb.RecordModel{Key: strconv.Itoa(index), Label: label, Summary: label, JSON: string(data)})
	}
	return model, nil
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
	}
	return "unknown record"
}

func selectConsoleRecord(model *consoleweb.SurfaceModel, raw string) error {
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 || index >= len(model.Records) {
		return errors.New("unknown console record")
	}
	model.Inspector = &model.Records[index]
	model.InspectorOpen = true
	return nil
}
