package managerplatform

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"reflect"
	"testing"
	"time"
)

func TestRepairNumericAndCustomSchema(t *testing.T) {
	for _, v := range []any{int8(0), int16(0), int32(0), int64(0), uint8(0), uint16(0), uint32(0), uint64(0), uint(0), int(0)} {
		if inputSchema(reflect.TypeOf(v), false).(map[string]any)["type"] != "integer" {
			t.Fatalf("%T", v)
		}
	}
	for _, v := range []any{float32(0), float64(0)} {
		if inputSchema(reflect.TypeOf(v), false).(map[string]any)["type"] != "number" {
			t.Fatalf("%T", v)
		}
	}
	for _, v := range []any{time.Time{}, []byte{}} {
		if inputSchema(reflect.TypeOf(v), false).(map[string]any)["type"] != "string" {
			t.Fatalf("%T", v)
		}
	}
	schema := inputSchema(reflect.TypeFor[app.SubmitGraphInput](), true).(map[string]any)
	if schema["properties"].(map[string]any)["max_attempts"].(map[string]any)["type"] != "integer" {
		t.Fatal(schema)
	}
	a := New(&app.Service{}, core.Subject{})
	for _, name := range []string{"agents.register", "agents.prepare", "charter.validate", "charter.import"} {
		props := a.operations[name].capability.InputSchema.(map[string]any)["properties"].(map[string]any)
		field := "charter"
		if name == "agents.register" {
			field = "fixture"
		}
		if props[field].(map[string]any)["type"] != "object" {
			t.Fatalf("%s: %v", name, props)
		}
	}
	var in app.SubmitGraphInput
	if decode(json.RawMessage(`{"max_attempts":"2"}`), &in) == nil {
		t.Fatal("string budget accepted")
	}
}
