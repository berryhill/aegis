package hermes

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestLegacyLaunchSurvivesLaunchingRequestCancellation(t *testing.T) {
	probe := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
while read line; do :; done
`
	a, root := attemptTestAdapter(t, probe)
	marker := filepath.Join(root, "legacy-started")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then\necho 'Hermes Agent v0.18.2'\necho 'Install directory: " + filepath.Join(root, "install") + "'\nexit 0\nfi\ntouch '" + marker + "'\nwhile read line; do :; done\n"
	if err := os.WriteFile(a.executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	request := providerAttempt(root)
	m, authority := request.Launch.Mandate, request.Launch.AuthorityContext
	m.Hermes.ProviderAuthentication = nil
	authority.Authority.Hermes = m.Hermes
	authority.Digest = core.AuthorityContextDigest(authority)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id, _, _, _, err := a.Launch(ctx, root, m, authority, nil, BrokerBridge{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Terminate(context.Background(), id, true)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("legacy runtime never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	if !a.Alive(id) {
		t.Fatal("legacy persistent runtime was coupled to launch request cancellation")
	}
}
