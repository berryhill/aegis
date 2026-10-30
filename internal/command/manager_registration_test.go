package command

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/managergateway"
	"testing"
)

// The terminal uses this shared dispatcher before Session.HandleStream; ordinary
// registration must remain unhandled, while nil-runtime fallback is nonmutating.
func TestRepairTerminalRegistrationSharedRouting(t *testing.T) {
	for _, text := range []string{"please add an agent", "can you register an agent?"} {
		if _, handled, err := managergateway.DispatchDeterministicAegisTurn(context.Background(), nil, core.Subject{}, text); handled || err != nil {
			t.Fatal("terminal general path consumed", text, err)
		}
		if out, ok := managergateway.RegistrationFallback(text); !ok || out.Data["registered"] != false {
			t.Fatal("unsafe degraded fallback")
		}
	}
}
