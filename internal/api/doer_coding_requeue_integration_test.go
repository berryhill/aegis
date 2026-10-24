package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Synthetic provider and decision protocol processes; real Badger, Unix peer
// authentication, app Run/requeue admission, native Go checks and disposition.
// No continuation/pass resolver is replaced by this fixture.
func TestAuthenticatedCodingCorrectionRequeue(t *testing.T) {
	authenticatedCodingCorrectionRequeue(t, false)
}

func TestAuthenticatedCodingNoApplyCorrectionRequeue(t *testing.T) {
	authenticatedCodingCorrectionRequeue(t, true)
}

func authenticatedCodingCorrectionRequeue(t *testing.T, noApply bool) {
	s := apiService(t)
	clock := newRequeueStageClock(s.Now())
	s.Now = clock.Now
	configureAPIFleet(t, s)
	s.Config.Credentials.Authority.DeploymentID = "coding-requeue-fixture"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "coding-agent", Name: "Coding fixture", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{sub.PrincipalID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Session:        core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "proof-no-key", Provider: "none"}}}, CreatedBy: sub.PrincipalID, CreatedAt: s.Now()}
	raw, _ := json.Marshal(c)
	charter, err := s.ImportCharterAs(ctx, sub, raw)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "coding-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "coding-source", AgentID: c.AgentID, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: app.RevisionReference(c.AgentID, 1, charter.Digest)}}})
	agent, _, err := s.RegisterFleetAgentAs(ctx, sub, app.NewRegisterFleetAgentInput(raw, "coding-fleet", "coding-source"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.25\n", "value.go": "package fixture\nfunc Value() int { return 0 }\n", "value_test.go": "package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=42 { t.Fatal(Value()) } }\n"}
	hash := func(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
	manifest := &loop.RepositoryContext{TaskDigest: hash([]byte("Return 42")), MaxFiles: 3, MaxFileBytes: 4096, MaxTotalBytes: 8192}
	for _, entry := range []struct{ path, role string }{{"value.go", "source"}, {"value_test.go", "trusted_test"}, {"go.mod", "checker_input"}} {
		text := files[entry.path]
		write(filepath.Join(workspace, entry.path), text)
		manifest.Files = append(manifest.Files, loop.RepositoryContextFile{Path: entry.path, Role: entry.role, Bytes: int64(len(text)), Digest: hash([]byte(text))})
	}
	manifest.SourceDigest = implementation.RepositorySourceDigest(manifest)
	policy := loop.ImplementationDraft("Return 42", "TestValue passes").Policy
	policy.Packages = []string{"."}
	policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	policy.TimeoutSeconds = 120
	contract := loop.DoerContract{Task: "Return 42", Workspace: workspace, WritableFiles: []string{"value.go"}, MaxAttempts: 2, Coding: &loop.DoerCoding{Acceptance: "TestValue passes", Policy: policy, RepositoryContext: manifest}}
	derived, err := contract.CodingImplementation()
	if err != nil {
		t.Fatal(err)
	}
	expectedContext, err := implementation.CollectRepositoryContext(ctx, derived, manifest.SourceDigest)
	if err != nil {
		t.Fatal(err)
	}
	contextWire, err := json.Marshal(expectedContext)
	if err != nil {
		t.Fatal(err)
	}
	inner, err := derived.Digest()
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err := loop.NewDoerRevision("coding-correction", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	laya := filepath.Join(root, "laya")
	write(laya, `#!/bin/sh
read request
case "$request" in
 *'"kind":"gate"'*) printf '%s\n' '{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}' ;;
 *'"kind":"verdict"'*) printf '%s\n' '{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"yes","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}' ;;
 *) exit 1 ;;
esac
`)
	home := filepath.Join(root, "laya-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: []string{outer}, LayaPython: laya, LayaHome: home}, s.Config.StateDir, s.Hermes); err != nil {
		t.Fatal(err)
	}
	install := filepath.Join(root, "install")
	if err = os.MkdirAll(filepath.Join(install, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	write(s.Config.HermesExecutable, "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+install+"'; exit 0; fi\nsleep 60 &\nwait\n")
	patchEvent := func(path, content string) string {
		proposal, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: path, Content: []byte(content)}}, "report": "Implemented value"})
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "fixture", "payload": map[string]string{"status": "complete", "text": string(proposal)}}})
		return string(b)
	}
	event := func(value string) string {
		return patchEvent("value.go", "package fixture\nfunc Value() int { return "+value+" }\n")
	}
	firstEvent := event("0")
	if noApply {
		firstEvent = `{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"fixture","payload":{"status":"error","text":"synthetic first proposal failure","error":"synthetic backend failure"}}}`
	}
	write(filepath.Join(install, "venv", "bin", "python"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 90
IFS= read -r tools || exit 1
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
IFS= read -r create || exit 0
printf '%%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"fixture"}}'
IFS= read -r prompt || exit 0
printf '%%s\n' "$prompt" > '%s/prompt-latest'
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"fixture","payload":{}}}'
case "$prompt" in
 *'Write reusable hello'*) printf '%%s\n' '%s'; while IFS= read -r rest; do :; done; exit 0 ;;
 *'Write reusable world'*) printf '%%s\n' '%s'; while IFS= read -r rest; do :; done; exit 0 ;;
esac
if [ ! -f '%s/first' ]; then
 printf '%%s\n' "$prompt" > '%s/prompt-first'
 : > '%s/first'
 printf '%%s\n' '%s'
elif [ ! -f '%s/diagnosed' ]; then
 : > '%s/diagnosed'
 printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"fixture","payload":{"status":"error","text":"private diagnosis failure","error":"synthetic backend failure"}}}'
else
 case "$prompt" in *RepositoryContext*) printf '%%s\n' "$prompt" > '%s/prompt-correction' ;; esac
 printf '%%s\n' '%s'
fi
while IFS= read -r rest; do :; done
`, root, patchEvent("result.txt", "hello"), patchEvent("result.txt", "world"), root, root, root, firstEvent, root, root, root, event("42")))
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	client := unixClient(s.Config.API.UnixSocket)
	client.Timeout = 4 * time.Minute
	var published app.PublishedLoop
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: c.AgentID, Revision: revision, IdempotencyKey: "coding-publish"}, &published, http.StatusCreated)
	if published.Revision.Digest != revision.Digest {
		t.Fatal("publication drift")
	}
	input := app.QueueLoopInput{Agent: app.RevisionReference(agent.Revision.AgentID, agent.Revision.Revision, agent.Revision.Digest), Loop: app.RevisionReference(revision.LoopID, 1, revision.Digest), IdempotencyKey: "coding-run", Activate: true}
	var first app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &first, http.StatusOK)
	if first.Execution == nil || first.Execution.Projection.State != queue.StateFailed {
		t.Fatalf("first failure not durable: %+v", first)
	}
	old := *first.Execution
	if len(old.Attempts) != 1 || old.Disposition == nil || old.Disposition.ReasonCode != "implementation_failed" || len(old.CodingHistory) != 1 {
		t.Fatalf("failure evidence: %+v", old)
	}
	history := old.CodingHistory[old.Attempts[0].AttemptID]
	if history.ReservedPasses != 1 || history.RemainingBudget != 1 || len(history.Passes) != 1 || history.Passes[0].Passed {
		t.Fatalf("failed native pass: %+v", history)
	}
	oldWire, _ := json.Marshal(old)
	// Observe the exact serialized input received by the synthetic provider,
	// independently of the controller's content-free public evidence.
	assertInput := func(v app.QueueExecutionView, capture string) {
		t.Helper()
		h := v.CodingHistory[v.Attempts[0].AttemptID]
		p := h.Passes[0]
		if h.ContractDigest != inner || h.TaskDigest != manifest.TaskDigest || h.ContextDigest != hash(contextWire) || p.ContextIdentity != expectedContext.Identity {
			t.Fatal("exact contract/task/source context witness mismatch")
		}
		b, err := os.ReadFile(filepath.Join(root, capture))
		var rpc struct {
			Params struct {
				Text string `json:"text"`
			} `json:"params"`
		}
		if err != nil || json.Unmarshal(b, &rpc) != nil {
			t.Fatal("synthetic provider input capture unavailable", err)
		}
		var envelope struct {
			Request implementation.Request `json:"request"`
		}
		if json.Unmarshal([]byte(rpc.Params.Text), &envelope) != nil || !reflect.DeepEqual(envelope.Request.RepositoryContext, expectedContext) {
			t.Fatalf("serialized source/test/module input changed: capture=%s actual=%+v expected=%+v", capture, envelope.Request.RepositoryContext, expectedContext)
		}
		if p.Input == nil || !p.Input.Delivered || p.Input.InputDigest != hash([]byte(rpc.Params.Text)) || p.Input.InputBytes != len(rpc.Params.Text) || p.Input.ContractDigest != inner || p.Input.TaskDigest != manifest.TaskDigest || p.Input.ContextDigest != hash(contextWire) || p.Input.TurnID != p.ID || p.Input.RuntimeSessionID != "fixture" || envelope.Request.PassID != p.ID {
			t.Fatal("exact serialized input/turn witness mismatch")
		}
	}
	if noApply {
		p := history.Passes[0]
		if p.Input == nil || p.Input.Delivered || p.Input.InputDigest == "" || p.Input.InputBytes == 0 {
			t.Fatal("known managed no-apply failure lost prepared, undelivered input")
		}
		b, err := os.ReadFile(filepath.Join(workspace, "value.go"))
		if err != nil || string(b) != files["value.go"] {
			t.Fatal("no-apply failure changed candidate", err)
		}
	} else {
		assertInput(old, "prompt-first")
	}
	selector := app.RequeueQueueItemInput{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: old.Item.Digest}, IdempotencyKey: "coding-correction"}
	for _, tc := range []struct {
		name, path string
		ref        reference.DigestRef
		status     int
	}{
		{"swapped-digest", old.Item.ItemID, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: hash([]byte("wrong"))}, http.StatusForbidden},
		{"cross-queue", "another-queue", selector.QueueItem, http.StatusBadRequest},
		{"unknown", "unknown-queue", reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "unknown-queue", Digest: old.Item.Digest}, http.StatusNotFound},
		{"partial", old.Item.ItemID, reference.DigestRef{ID: old.Item.ItemID}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiRequest(t, client, http.MethodPost, "/v1/queue/"+tc.path+"/requeue", app.RequeueQueueItemInput{QueueItem: tc.ref, IdempotencyKey: tc.name}, nil, tc.status)
		})
	}
	// Actual preparation retires authority; no manual closure or expiry jump.
	runInterveningReusableTasks(t, client, input.Agent, c.AgentID)
	for _, duplicate := range []bool{false, true} {
		run := input
		run.IdempotencyKey = fmt.Sprintf("arbitrary-native-%v", duplicate)
		if duplicate {
			copy, _, err := loop.NewDoerRevision("duplicate-native-task", 1, "", contract)
			if err != nil {
				t.Fatal(err)
			}
			apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: c.AgentID, Revision: copy, IdempotencyKey: "duplicate-publish"}, nil, http.StatusCreated)
			run.Loop = app.RevisionReference(copy.LoopID, 1, copy.Digest)
		}
		var denied app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", run, &denied, http.StatusOK)
		if denied.Reason != "same_task_budget_link_required" || denied.QueueItemID != "" {
			t.Fatalf("same contract reset admitted: %+v", denied)
		}
	}
	var next app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+old.Item.ItemID+"/requeue", selector, &next, http.StatusOK)
	if next.Execution == nil || next.Execution.Projection.State != queue.StateSucceeded {
		t.Fatalf("correction not successful: %+v", next)
	}
	corrected := *next.Execution
	assertInput(corrected, "prompt-correction")
	if corrected.LoopExecutions[0].Loop != old.LoopExecutions[0].Loop || corrected.LoopExecutions[0].Participant != old.LoopExecutions[0].Participant || corrected.RuntimeAuthorityBinding.Authority == old.RuntimeAuthorityBinding.Authority {
		t.Fatal("requeue lost immutable identities or reused admission")
	}
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+corrected.Item.ItemID+"/requeue", app.RequeueQueueItemInput{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: corrected.Item.ItemID, Digest: corrected.Item.Digest}, IdempotencyKey: "successful-exhausted"}, nil, http.StatusForbidden)
	selector.IdempotencyKey = "second-successor"
	apiRequest(t, client, http.MethodPost, "/v1/queue/"+old.Item.ItemID+"/requeue", selector, nil, http.StatusForbidden)
	if len(corrected.Attempts) != 1 || len(corrected.Receipts) != 1 || corrected.Disposition == nil || corrected.Disposition.ReasonCode != "implementation_verified" {
		t.Fatalf("native disposition: %+v", corrected)
	}
	h := corrected.CodingHistory[corrected.Attempts[0].AttemptID]
	if h.ConsumedBefore != 1 || h.ReservedPasses != 1 || h.CumulativeConsumed != 2 || h.RemainingBudget != 0 || h.PredecessorHistory != history.Digest || h.PredecessorRun != old.Item.ItemID {
		t.Fatalf("cumulative history: %+v", h)
	}
	for _, v := range []struct {
		history string
		passes  int
	}{{history.Digest, len(history.Passes)}, {h.Digest, len(h.Passes)}} {
		if v.history == "" || v.passes != 1 {
			t.Fatal("missing pass witnesses")
		}
	}
	for _, p := range []struct {
		input  bool
		digest string
	}{{history.Passes[0].Input != nil, history.ContextDigest}, {h.Passes[0].Input != nil, h.Passes[0].ContextStateDigest}} {
		if !p.input || p.digest == "" {
			t.Fatal("missing context/input custody")
		}
	}
	for _, p := range []string{"go.mod", "value_test.go"} {
		b, err := os.ReadFile(filepath.Join(workspace, p))
		if err != nil || string(b) != files[p] {
			t.Fatal("immutable checker changed", p, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(workspace, "value.go"))
	if err != nil || !strings.Contains(string(b), "return 42") {
		t.Fatal("source correction absent", err)
	}
	var reread app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue/"+old.Item.ItemID, nil, &reread, http.StatusOK)
	after, _ := json.Marshal(reread)
	if !reflect.DeepEqual(oldWire, after) {
		t.Fatal("failed historical public readback mutated")
	}
	var replay app.QueueLoopResult
	apiRequest(t, client, http.MethodPost, "/v1/loops/queue", input, &replay, http.StatusOK)
	if replay.QueueItemID != old.Item.ItemID || replay.Execution == nil || len(replay.Execution.Attempts) != 1 {
		t.Fatal("original-key replay duplicated native effects")
	}
	var successful app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue/"+corrected.Item.ItemID, nil, &successful, http.StatusOK)
	if successful.CodingHistory[corrected.Attempts[0].AttemptID].Digest != h.Digest {
		t.Fatal("public successor history drift")
	}
	t.Logf("authenticated Run failed after 1 native pass; authenticated requeue succeeded with 1 remaining pass; cumulative=2; predecessor history=%s successor history=%s", history.Digest, h.Digest)
}

func runInterveningReusableTasks(t *testing.T, client *http.Client, agent reference.RevisionRef, agentID string) {
	t.Helper()
	reusable, _, err := loop.NewDoerReusableRevision("coding-intervening-reusable", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: agentID, Revision: reusable, IdempotencyKey: "reusable-publish"}, nil, http.StatusCreated)
	for _, text := range []string{"hello", "world"} {
		run := app.QueueLoopInput{Agent: agent, Loop: app.RevisionReference(reusable.LoopID, 1, reusable.Digest), IdempotencyKey: "intervening-" + text, Activate: true}
		space := t.TempDir()
		for _, v := range []struct {
			id    string
			kind  graph.ValueType
			value any
		}{
			{"task", graph.TypeString, "Write reusable " + text}, {"workspace", graph.TypeString, space}, {"writable_files", graph.TypeArray, []string{"result.txt"}}, {"verify_file", graph.TypeString, "result.txt"}, {"expected_text", graph.TypeString, text},
		} {
			raw, _ := json.Marshal(v.value)
			run.Inputs = append(run.Inputs, graph.NormalizedInput{PortID: v.id, Type: v.kind, Value: raw})
		}
		var result app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", run, &result, http.StatusOK)
		if result.Execution == nil || result.Execution.Projection.State != queue.StateSucceeded || len(result.Execution.Attempts) != 1 || len(result.Execution.Receipts) != 1 {
			t.Fatalf("distinct %s blocked: %+v", text, result)
		}
		b, err := os.ReadFile(filepath.Join(space, "result.txt"))
		if err != nil || string(b) != text {
			t.Fatalf("%s output=%q err=%v", text, b, err)
		}
		var replay app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", run, &replay, http.StatusOK)
		if replay.QueueItemID != result.QueueItemID || replay.Execution == nil || len(replay.Execution.Attempts) != 1 {
			t.Fatal("reusable replay duplicated effects")
		}
	}
}
