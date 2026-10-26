package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"reflect"
	"testing"
)

func TestContributionRecoveryUnknownEffectsRemainHeld(t *testing.T) {
	for _, kind := range []string{"reserved", "inflight", "unknown", "missing-coverage", "legacy-boot", "publication"} {
		t.Run(kind, func(t *testing.T) {
			r := contribution.RoleAuthority{Role: "builder", Epoch: 1, BootID: "boot", PID: 123, ProcessStart: "start"}
			binding := contribution.NativeBinding{IntentID: "native", Role: "builder", Epoch: 1}
			state := contribution.Continuity{Stage: "freeze", RoleAuthorities: map[string]contribution.RoleAuthority{"builder": r}, NativeBoundaries: map[string]contribution.NativeBoundary{"builder": {Role: "builder", Epoch: 1, Bindings: []contribution.NativeBinding{binding}}}, NativeEffects: map[string]contribution.NativeEffect{"native": {Binding: binding, State: kind}}}
			switch kind {
			case "missing-coverage":
				delete(state.NativeBoundaries, "builder")
			case "legacy-boot":
				r.BootID = ""
				state.RoleAuthorities["builder"] = r
			case "publication":
				state.Stage = "publish"
			}
			// No adapter/repository exists: denial must precede ANY stop, append or
			// replacement runtime. Native intent and all consumed counters stay intact.
			before := state
			got, err := (&Service{}).recoverContributionRoles(context.Background(), contribution.Contract{}, state)
			if err == nil || !reflect.DeepEqual(got, before) || len(got.RoleStops) != 0 {
				t.Fatalf("unknown effect changed or admitted: %+v %v", got, err)
			}
		})
	}
}
