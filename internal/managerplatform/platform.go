// Package managerplatform adapts the closed conversational proposal protocol to
// existing application services. It owns no persistence, grants or runtime tools.
package managerplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/registry"
	"io"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/reference"
)

type operation struct {
	capability manager.PlatformCapability
	prepare    func(context.Context, json.RawMessage) (json.RawMessage, error)
	execute    func(context.Context, json.RawMessage) (any, error)
}
type Adapter struct {
	service    *app.Service
	subject    core.Subject
	operations map[string]operation
}

func New(service *app.Service, subject core.Subject) *Adapter {
	a := &Adapter{service: service, subject: subject, operations: map[string]operation{}}
	a.register()
	return a
}
func (a *Adapter) authenticate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.service == nil {
		return app.ErrDenied
	}
	return a.service.RequirePrincipal(a.subject)
}
func (a *Adapter) Capabilities(ctx context.Context) ([]manager.PlatformCapability, error) {
	if err := a.authenticate(ctx); err != nil {
		return nil, err
	}
	result := make([]manager.PlatformCapability, 0, len(a.operations))
	for _, op := range a.operations {
		result = append(result, op.capability)
	}
	result = append(result, unavailableCapabilities()...)
	sort.Slice(result, func(i, j int) bool { return result[i].Operation < result[j].Operation })
	return result, nil
}
func (a *Adapter) Prepare(ctx context.Context, name string, input json.RawMessage) (manager.PlatformIntent, error) {
	if err := a.authenticate(ctx); err != nil {
		return manager.PlatformIntent{}, err
	}
	op, ok := a.operations[name]
	if !ok {
		return manager.PlatformIntent{}, fmt.Errorf("platform_operation_unavailable: %s", name)
	}
	if len(input) == 0 || len(input) > 256<<10 {
		return manager.PlatformIntent{}, errors.New("bounded input object required")
	}
	// Identity and grants are never accepted from the proposal. Selectors remain
	// data; application services resolve and authorize them afresh.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(input, &fields); err != nil || fields == nil {
		return manager.PlatformIntent{}, errors.New("input object required")
	}
	for _, key := range []string{"authority", "publisher", "workspace", "subject", "principal", "approval", "mandate", "stanza"} {
		if _, ok := fields[key]; ok {
			return manager.PlatformIntent{}, fmt.Errorf("model_authority_forbidden: %s", key)
		}
	}
	normalized, err := op.prepare(ctx, input)
	if err != nil {
		return manager.PlatformIntent{}, err
	}
	return manager.PlatformIntent{Operation: name, Input: normalized, Mutation: op.capability.Mutation}, nil
}
func (a *Adapter) Execute(ctx context.Context, intent manager.PlatformIntent) (any, error) {
	if err := a.authenticate(ctx); err != nil {
		return nil, err
	}
	op, ok := a.operations[intent.Operation]
	if !ok || op.capability.Mutation != intent.Mutation {
		return nil, app.ErrDenied
	}
	return op.execute(ctx, intent.Input)
}
func decode(data []byte, out any) error {
	if err := reference.RejectDuplicateObjectKeys(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing input")
	}
	return nil
}

// bind keeps the concrete request type in both preparation and dispatch. Schema
// is discovery only; strict decoding and the canonical service remain validators.
func bind[T any, R any](a *Adapter, name, service string, mutation bool, normalize func(context.Context, *T) error, call func(context.Context, T) (R, error)) {
	typ := reflect.TypeFor[T]()
	schema := inputSchema(typ, true)
	if properties, ok := schema.(map[string]any)["properties"].(map[string]any); ok {
		if _, ok := properties["fixture"]; ok {
			properties["fixture"] = inputSchema(reflect.TypeFor[registry.CurrentFleetFixture](), false)
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if strings.Split(f.Tag.Get("json"), ",")[0] == "charter" && f.Type == reflect.TypeFor[json.RawMessage]() {
				properties["charter"] = inputSchema(reflect.TypeFor[core.Charter](), false)
			}
		}
	}
	op := operation{capability: manager.PlatformCapability{Operation: name, Mutation: mutation, InputSchema: schema, Service: service, Availability: "implemented-and-exposed"}}
	op.prepare = func(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
		var in T
		if err := decode(data, &in); err != nil {
			return nil, fmt.Errorf("invalid %s input: %w", name, err)
		}
		if normalize != nil {
			if err := normalize(ctx, &in); err != nil {
				return nil, err
			}
		}
		return json.Marshal(in)
	}
	op.execute = func(ctx context.Context, data json.RawMessage) (any, error) {
		var in T
		if err := decode(data, &in); err != nil {
			return nil, err
		}
		return call(ctx, in)
	}
	a.operations[name] = op
}
func inputSchema(t reflect.Type, root bool) any {
	if t.Kind() == reflect.Pointer {
		return inputSchema(t.Elem(), false)
	}
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return map[string]any{"description": "canonical JSON as required by the named application schema"}
	}
	switch t.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" || tag == "" {
				continue
			}
			if root && (tag == "authority" || tag == "publisher" || tag == "workspace") {
				continue
			}
			properties[tag] = inputSchema(f.Type, false)
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	case reflect.Slice, reflect.Array:
		if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": inputSchema(t.Elem(), false)}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Interface:
		return map[string]any{}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": inputSchema(t.Elem(), false)}
	default:
		return map[string]any{"type": "string"}
	}
}

type empty struct{}
type object struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision,omitempty"`
}

func requireID(_ context.Context, in *object) error {
	if in.ID == "" {
		return errors.New("missing_input: id (exact canonical identifier) required")
	}
	return nil
}
func exactRevision(ctx context.Context, in *object) error {
	if err := requireID(ctx, in); err != nil {
		return err
	}
	if in.Revision == 0 {
		return errors.New("missing_input: positive exact revision required; discover inventory first")
	}
	return nil
}
func newID() string {
	id, err := manager.NewSessionID()
	if err != nil {
		return ""
	}
	return id
}
