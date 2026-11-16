package contribution

import (
	"reflect"
	"testing"
	"time"
)

// Recursively populate EVERY field, including future fields, rather than testing
// only today's hand-picked examples. time.Time's location is immutable via its
// public API; references, runtime bindings, comments and context files recurse.
func filledContractValue(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()
	v := reflect.New(typ).Elem()
	if typ == reflect.TypeFor[time.Time]() {
		v.Set(reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
		return v
	}
	switch typ.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			v.Field(i).Set(filledContractValue(t, typ.Field(i).Type))
		}
	case reflect.Pointer:
		v.Set(reflect.New(typ.Elem()))
		v.Elem().Set(filledContractValue(t, typ.Elem()))
	case reflect.Slice:
		v.Set(reflect.MakeSlice(typ, 1, 1))
		v.Index(0).Set(filledContractValue(t, typ.Elem()))
	case reflect.String:
		v.SetString("original")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	default:
		t.Fatalf("new contract field kind needs isolation coverage: %v", typ)
	}
	return v
}

func assertContractIsolation(t *testing.T, a, b reflect.Value, path string) {
	t.Helper()
	if a.Type() == reflect.TypeFor[time.Time]() {
		return
	}
	switch a.Kind() {
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			assertContractIsolation(t, a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name)
		}
	case reflect.Pointer:
		if a.Pointer() == b.Pointer() {
			t.Fatalf("shared pointer %s", path)
		}
		assertContractIsolation(t, a.Elem(), b.Elem(), path)
	case reflect.Slice:
		if a.Pointer() == b.Pointer() {
			t.Fatalf("shared slice %s", path)
		}
		assertContractIsolation(t, a.Index(0), b.Index(0), path+"[]")
	}
}

func TestContractCloneAllFields(t *testing.T) {
	c := filledContractValue(t, reflect.TypeFor[Contract]()).Interface().(Contract)
	copy := c.Clone()
	if !reflect.DeepEqual(c, copy) {
		t.Fatal("clone changed value")
	}
	assertContractIsolation(t, reflect.ValueOf(c), reflect.ValueOf(copy), "Contract")
	// Nil and empty collections must retain their distinct canonical encodings.
	for _, original := range []Contract{{}, {Invariants: []string{}, Exclusions: []string{}, Roles: []RoleBinding{}, Source: Source{Discussion: []Comment{}}}} {
		if !reflect.DeepEqual(original, original.Clone()) {
			t.Fatal("nil/empty drift")
		}
	}
}
