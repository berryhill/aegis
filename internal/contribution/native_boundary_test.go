package contribution

import "testing"

func TestNativeRoleJoinRejectsUnknownMissingStale(t *testing.T) {
	r := RoleAuthority{Role: "builder", Epoch: 2, SessionID: "session"}
	binding := NativeBinding{IntentID: "native", Role: "builder", Epoch: 2}
	state := Continuity{RoleAuthorities: map[string]RoleAuthority{"builder": r}, RoleStops: map[string]RoleStop{"builder": {Authority: r, Epoch: 2, Acknowledged: true}}, NativeBoundaries: map[string]NativeBoundary{"builder": {Role: "builder", Epoch: 2, Closed: true, Bindings: []NativeBinding{binding}}}, NativeEffects: map[string]NativeEffect{"native": {Binding: binding, State: "completed"}}}
	if !NativeRoleQuiescent(state, "builder") {
		t.Fatal("exact join unavailable")
	}
	for _, status := range []string{"unknown", "reserved", ""} {
		state.NativeEffects["native"] = NativeEffect{Binding: binding, State: status}
		if NativeRoleQuiescent(state, "builder") {
			t.Fatal("unjoined effect", status)
		}
	}
	state.NativeEffects["native"] = NativeEffect{Binding: binding, State: "completed"}
	b := state.NativeBoundaries["builder"]
	b.Epoch = 1
	state.NativeBoundaries["builder"] = b
	if NativeRoleQuiescent(state, "builder") {
		t.Fatal("stale closure")
	}
	delete(state.NativeBoundaries, "builder")
	if NativeRoleQuiescent(state, "builder") {
		t.Fatal("legacy coverage inferred")
	}
}
