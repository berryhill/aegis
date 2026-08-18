package orchestration

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

type routedAdmission struct{ allowed bool }

func (admission routedAdmission) CheckRuntimeAdmission(_ context.Context, launch execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
	return execution.AdmissionDecision{
		Allowed: admission.allowed, AuthorityContextID: launch.AuthorityContext.ID,
		AuthorityContextDigest: launch.AuthorityContext.Digest, CheckedAt: at, Reason: "test",
	}, nil
}

func TestRoutedRuntimeAdapterExecutesRegisteredHermesBoundedTurn(t *testing.T) {
	root := t.TempDir()
	adapter := routedHermesTestAdapter(t, root, `#!/bin/sh
[ "$HERMES_TUI_MODEL" = "proof-no-key" ] || exit 90
[ "$HERMES_TUI_PROVIDER" = "none" ] || exit 91
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"queue-runtime-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"queue-runtime-session","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"queue-runtime-session","payload":{"delta":"verified Hermes output"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"queue-runtime-session","payload":{}}}'
while read rest; do :; done
`)
	request := routedRuntimeRequest(root)
	result, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Output) != "verified Hermes output" || result.MediaType != "text/plain" {
		t.Fatalf("routed result = %#v", result)
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("bounded queue attempt retained disposable runtime home: %v", entries)
	}
}

func TestRoutedRuntimeAdapterPreservesCredentialIndependentNoKeyFixture(t *testing.T) {
	root := t.TempDir()
	adapter := routedHermesTestAdapter(t, root, "#!/bin/sh\nexit 99\n")
	request := routedRuntimeRequest(root)
	request.Participant.Runtime.Adapter = "no-key"
	request.Participant.Runtime.Runtime = "no-key"
	result, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) == 0 || result.MediaType != "application/json" {
		t.Fatalf("no-key fixture result = %#v", result)
	}
}

func TestRoutedRuntimeAdapterFailsClosedForUnregisteredOrDeniedHermesBinding(t *testing.T) {
	root := t.TempDir()
	adapter := routedHermesTestAdapter(t, root, "#!/bin/sh\nexit 99\n")
	tests := []struct {
		name string
		edit func(*RuntimeRequest)
	}{
		{"unsupported registered adapter", func(request *RuntimeRequest) { request.Participant.Runtime.Adapter = "other" }},
		{"runtime mismatch", func(request *RuntimeRequest) { request.Participant.Runtime.Runtime = "other-runtime" }},
		{"missing admission", func(request *RuntimeRequest) { request.Admission = nil }},
		{"fresh admission denied", func(request *RuntimeRequest) { request.Admission = routedAdmission{allowed: false} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := routedRuntimeRequest(root)
			test.edit(&request)
			if _, err := adapter.Execute(context.Background(), request); err == nil {
				t.Fatal("invalid registered Hermes binding executed")
			}
		})
	}
}

func routedHermesTestAdapter(t *testing.T, root, gatewayScript string) *RoutedRuntimeAdapter {
	t.Helper()
	installation := filepath.Join(root, "install")
	if err := os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "hermes")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\necho 'Hermes Agent v0.18.2'\necho 'Install directory: "+installation+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(gatewayScript), 0700); err != nil {
		t.Fatal(err)
	}
	runtime := hermesruntime.New(executable, slog.New(slog.NewTextHandler(io.Discard, nil)))
	adapter, err := NewRoutedRuntimeAdapter(runtime, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func routedRuntimeRequest(_ string) RuntimeRequest {
	now := time.Now().UTC()
	runtime := core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.18.2"}
	subject := core.Subject{ID: "subject-1", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	mandate := core.Mandate{ID: "mandate-1", Subject: subject, AgentID: "agent-1", StanzaID: "operator", CharterRevision: 1, CharterDigest: "sha256:" + strings.Repeat("a", 64), Runtime: runtime, Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "proof-no-key", Provider: "none"}, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	authority := core.AuthorityContext{ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: subject.ID, AgentID: mandate.AgentID, CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID, Hermes: mandate.Hermes}, IssuedAt: mandate.IssuedAt, ExpiresAt: mandate.ExpiresAt}
	authority.Digest = core.AuthorityContextDigest(authority)
	started, finished := now.Add(-time.Second), now
	return RuntimeRequest{
		GraphRunID: "graph-run-1", LoopExecutionID: "loop-execution-1", GraphNodeID: "node-1",
		Authority:   reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest},
		Inputs:      []graph.NormalizedInput{{PortID: "prompt", Type: graph.TypeString, Value: []byte(`"do bounded work"`)}},
		Participant: registry.AgentRevision{AgentID: "agent-1", Revision: 1, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}, Ownership: registry.Ownership{OwnerID: "owner-1", AccountabilityID: "accountability-1"}, Lifecycle: registry.LifecycleEnabled},
		Launch:      execution.LaunchContract{OwnerID: "owner-1", Mandate: mandate, AuthorityContext: authority, ParentDispatch: execution.Dispatch{ID: "attempt-1", AuthorityContextID: authority.ID, State: execution.StateSucceeded, RequestedAt: now.Add(-2 * time.Second), StartedAt: &started, FinishedAt: &finished}},
		Admission:   routedAdmission{allowed: true},
	}
}
