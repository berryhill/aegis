package hermes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestProviderSessionRejectsActualProcessToolSnapshotAndReaps(t *testing.T) {
	a, root := attemptTestAdapter(t, `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
if [ -f "$HERMES_HOME/probe-done" ]; then
 printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":1,"sections":[]}}'
else
 touch "$HERMES_HOME/probe-done"
 printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
fi
while read rest; do :; done
`)
	a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil } // synthetic launcher, not installed qualification
	a.SetProviderAuthenticationResolver(func(_ context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
		return NewProviderAccessToken(h.Provider, syntheticProviderToken(time.Now().Add(time.Hour)), time.Now())
	})
	r := providerAttempt(root)
	_, _, _, _, err := a.Launch(context.Background(), root, r.Launch.Mandate, r.Launch.AuthorityContext, nil, BrokerBridge{}, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("separate preflight substituted for actual launched process snapshot")
	}
	a.mu.Lock()
	count := len(a.processes)
	a.mu.Unlock()
	if count != 0 {
		t.Fatal("failed launcher returned before process custody cleanup")
	}
	entries, e := os.ReadDir(filepath.Join(root, "runtime"))
	if e != nil || len(entries) != 0 {
		t.Fatal("failed token-bearing home retained", e)
	}
}
