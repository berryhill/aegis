package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/spf13/cobra"
)

// Explicit opt-in source-candidate acceptance. No fake source, subject extractor,
// provider, approval or grants. It tests intake and retained observation, not
// managed-provider execution or installed adoption. Run under acceptance_port.py.
func TestContributionIntakeCandidatePublicSource(t *testing.T) {
	binary := os.Getenv("AEGIS_375_CANDIDATE")
	if binary == "" {
		t.Skip("requires explicitly selected built candidate and public read")
	}
	port := os.Getenv("AEGIS_ACCEPTANCE_PORT")
	if port == "" {
		t.Fatal("requires shared lifetime candidate-port custody")
	}
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	issue, err := contribution.ParseIssueURL("https://github.com/berryhill/aegis/issues/375")
	must(err)
	source, err := (contribution.Reader{}).Read(ctx, issue)
	must(err)
	t.Logf("live public source: repository=%d issue=%d digest=%s discussion=%d", source.RepositoryID, source.IssueID, source.ContentDigest, len(source.Discussion))
	root := t.TempDir()
	if evidence := os.Getenv("AEGIS_375_EVIDENCE"); evidence != "" {
		root, err = os.MkdirTemp(evidence, "candidate-intake-")
		must(err)
	}
	cfg := config.Defaults().WithStateDir(filepath.Join(root, "state"))
	cfg.Principal.ID = "candidate-intake-owner"
	cfg.Principal.UID = fmt.Sprint(os.Getuid())
	cfg.Principal.User = os.Getenv("USER")
	cfg.API.Listen = "127.0.0.1:" + port
	cfg.API.Console.Origin = "http://" + cfg.API.Listen
	cfg.API.UnixSocket = filepath.Join(root, "api.sock")
	cfg.API.Token = "isolated-intake-transport-fixture"
	cfg.HermesExecutable = filepath.Join(root, "unavailable-hermes")
	_, err = authoritybadger.Initialize(ctx, filepath.Join(cfg.StateDir, "persistence", "authority-v1"))
	must(err)
	path := filepath.Join(root, "aegis.json")
	raw, err := json.Marshal(cfg)
	must(err)
	must(os.WriteFile(path, raw, 0600))
	verifier, err := principalauth.Enroll(cfg.Principal.ID, []byte("isolated-intake-fixture-password"))
	must(err)
	must(principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier))
	var build func(*cobra.Command) (*app.Service, error)
	var closeService func() error
	command := newRoot(Dependencies{Out: io.Discard, Err: io.Discard, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, func(b func(*cobra.Command) (*app.Service, error), c func() error) { build = b; closeService = c })
	must(command.PersistentFlags().Set("config", path))
	seed := &cobra.Command{Use: "serve"}
	command.AddCommand(seed)
	seed.SetContext(ctx)
	service, err := build(seed)
	must(err)
	closed := false
	defer func() {
		if !closed {
			_ = closeService()
		}
	}()
	subject, err := service.Authenticate(ctx)
	must(err)
	refs := []reference.RevisionRef{}
	for _, id := range []string{"intake-controller", "intake-builder", "intake-tester"} {
		agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "candidate-intake", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/" + id}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "isolated-intake"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		must(err)
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: agent.Digest}
		refs = append(refs, ref)
		_, err = service.FleetRepository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: agent.Source, InitialRevision: ref}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "isolated intake definition; no authority grant"}})
		must(err)
	}
	draft := loop.ImplementationDraft("Retain exact issue intake without execution authority", "Authenticated status returns the sealed contract after restart")
	draft.Workspace = filepath.Join(root, "uncreated-worktree")
	draft.WritableFiles = []string{"value.go"}
	draft.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	profile := loop.ContributionProfile{Controller: refs[0], Builder: refs[1], Tester: refs[2], Repository: "berryhill/aegis", RepositoryID: source.RepositoryID, Base: strings.Repeat("b", 40), ReadAction: contribution.ReadIssueAction, Effect: "verified-protected-base-merge", DeadlineSeconds: 300, Invariants: []string{"no authority from issue prose"}, Exclusions: []string{"no managed execution in this intake qualification"}}
	revision, _, err := loop.NewContributionRevision("public-intake", 1, "", draft, profile)
	must(err)
	_, err = service.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: refs[0].ID, Revision: revision, IdempotencyKey: "candidate-intake-publication"})
	must(err)
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: revision.Digest}
	_, err = service.SetLoopLifecycleAs(ctx, subject, ref.ID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: ref, State: loop.LifecycleActive, EventID: "candidate-intake-activation"})
	must(err)
	// Same real issue, supported immutable profile, admitted field bounds:
	// its complete encoded context must be rejected by the actual public route.
	oversizedDraft := draft
	oversizedDraft.Task = strings.Repeat("x", 16384)
	oversizedDraft.Acceptance = strings.Repeat("y", 16384)
	oversized, _, err := loop.NewContributionRevision("public-intake-oversized", 1, "", oversizedDraft, profile)
	must(err)
	_, err = service.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: refs[0].ID, Revision: oversized, IdempotencyKey: "oversized-profile"})
	must(err)
	oversizedRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: oversized.LoopID, Revision: 1, Digest: oversized.Digest}
	_, err = service.SetLoopLifecycleAs(ctx, subject, oversizedRef.ID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: oversizedRef, State: loop.LifecycleActive, EventID: "oversized-activation"})
	must(err)
	must(closeService())
	closed = true
	start := func() *contributionHTTPFixture {
		logPath := filepath.Join(root, fmt.Sprintf("serve-%d.log", time.Now().UnixNano()))
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		must(err)
		process := exec.Command(binary, "--config", path, "serve")
		process.Stdout = log
		process.Stderr = log
		must(process.Start())
		done := make(chan error, 1)
		go func() { done <- process.Wait(); log.Close() }()
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
		}}
		client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
		stopped := false
		h := &contributionHTTPFixture{client: client, token: cfg.API.Token, origin: cfg.API.Console.Origin}
		h.stop = func() {
			if stopped {
				return
			}
			stopped = true
			transport.CloseIdleConnections()
			_ = process.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				_ = process.Process.Kill()
				<-done
				t.Error("owned candidate stop timeout")
			}
		}
		t.Cleanup(h.stop)
		deadline := time.NewTimer(15 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(30 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case err := <-done:
				stopped = true
				t.Fatalf("candidate exited: %v; log=%s", err, logPath)
			case <-deadline.C:
				t.Fatalf("candidate readiness timeout; log=%s", logPath)
			case <-tick.C:
				r, e := client.Get("http://unix/livez")
				if e == nil {
					r.Body.Close()
					if r.StatusCode == 200 {
						return h
					}
				}
			}
		}
	}
	gateway := start()
	assertContributionHTTPDenials(t, gateway)
	opened, err := gateway.Open(ctx, core.Subject{})
	must(err)
	execute := func(input string) map[string]any {
		t.Helper()
		r, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, input)
		must(e)
		return r.Data
	}
	for _, input := range []string{"chat " + issue.URL, "do not /contribute run " + issue.URL, "`/contribute run " + issue.URL + "`", "tutorial: /contribute run " + issue.URL, "/contribute run https://github.com/berryhill/aegis/pull/375", "/contribute run https://evil.invalid/berryhill/aegis/issues/375", "/contribute run " + issue.URL + " local-only extra"} {
		if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, input); e == nil {
			t.Fatalf("non-invocation admitted %q", input)
		}
	}
	execute("/contribute select " + oversizedRef.ID)
	for range 2 {
		_, err = gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute run "+issue.URL+" no-merge")
		if err == nil || !strings.Contains(err.Error(), "status=413") || !strings.Contains(err.Error(), `"code":"contribution_input_bound"`) || strings.Contains(err.Error(), strings.Repeat("x", 64)) || strings.Contains(err.Error(), strings.Repeat("y", 64)) {
			t.Fatalf("public encoded bound missing safe413: %v", err)
		}
	}
	gateway.stop()
	service, err = build(seed)
	must(err)
	closed = false
	items, err := service.FleetRepository.ListQueueItems(ctx)
	must(err)
	custody := service.FleetRepository.(fleet.ContributionRepository)
	_, err = custody.GetContribution(ctx, subject.PrincipalID, app.ContributionIntentKey(oversizedRef, issue.URL, "no-merge"))
	if len(items) != 0 || !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("public rejected/replayed input allocated contribution or Queue", len(items), err)
	}
	if _, err = os.Lstat(draft.Workspace); !os.IsNotExist(err) {
		t.Fatal("public rejection created worktree", err)
	}
	t.Log("public encoded overflow: safe HTTP413 on same-key retry; durable readback has zero contributions/Queue/worktree effects")
	must(closeService())
	closed = true
	gateway = start()
	opened, err = gateway.Open(ctx, core.Subject{})
	must(err)
	execute("/contribute select " + ref.ID)
	for _, input := range []string{"/contribute run https://github.com/foreign/repo/issues/1", "/contribute run " + issue.URL + " no-merge session=spoof"} {
		if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, input); e == nil {
			t.Fatalf("foreign/spoofed input admitted %q", input)
		}
	}
	execute("/contribute run " + issue.URL + " no-merge")
	status := execute("/contribute status")
	decode := func(data map[string]any) contribution.Contract {
		t.Helper()
		wire, e := json.Marshal(data["contract"])
		must(e)
		c, e := contribution.Unmarshal(wire)
		must(e)
		return c
	}
	sealed := decode(status)
	expectedSource := source
	expectedSource.ObservedAt = sealed.Source.ObservedAt
	if !reflect.DeepEqual(sealed.Source, expectedSource) {
		t.Fatal("public intake lost issue/discussion bytes or provenance")
	}
	bound, err := contribution.BindLoop(revision, sealed)
	must(err)
	if len(source.Body)+func() int {
		n := 0
		for _, c := range source.Discussion {
			n += len(c.Body)
		}
		return n
	}() > 32768 && (bound.Contribution == nil || contribution.ValidateImplementationContext(bound, &sealed) != nil) {
		t.Fatal("large public source lacks resolvable exact binding")
	}
	sourceWire, err := json.Marshal(sealed.Source)
	must(err)
	t.Logf("complete source JSON bytes=%d; Task bytes=%d; reference-backed=%t", len(sourceWire), len(bound.Task), bound.Contribution != nil)
	if sealed.Source.IssueID != source.IssueID || sealed.Source.RepositoryID != source.RepositoryID || sealed.Loop != ref || sealed.OwnerID != cfg.Principal.ID || sealed.SessionID != opened.ID || sealed.Acceptance != draft.Acceptance || sealed.Effect != "no-merge" {
		t.Fatal("lost canonical intake bindings")
	}
	replay := execute("/contribute run " + issue.URL + " no-merge")
	if replay["run_id"] != sealed.RunID {
		t.Fatal("replay allocated another run")
	}
	if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute run "+issue.URL+" local-only"); e == nil {
		t.Fatal("different key stole reserved issue")
	}
	execute("/contribute narrow local-only")
	controlled := execute("/contribute status")
	if controlled["effect"] != "local-only" || controlled["control_sequence"] != float64(1) {
		t.Fatal("status lost owner narrowing")
	}
	if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute narrow no-merge"); e == nil {
		t.Fatal("expansion accepted")
	}
	gateway.stop()
	gateway = start()
	opened, err = gateway.Open(ctx, core.Subject{})
	must(err)
	restored := execute("/contribute status " + issue.URL)
	if !reflect.DeepEqual(decode(restored), sealed) || restored["effect"] != "local-only" || restored["control_sequence"] != float64(1) {
		t.Fatal("restart lost retained contract/control")
	}
	controlWire, err := json.Marshal(restored["control"])
	must(err)
	var control contribution.Control
	must(json.Unmarshal(controlWire, &control))
	if control.SessionID != sealed.SessionID || control.OwnerID != sealed.OwnerID {
		t.Fatal("restart rewrote control provenance")
	}
	stop := execute("/contribute stop")
	if stop["cancel_requested"] != true || stop["cancel_acknowledged"] != false || stop["stopped"] != false {
		t.Fatal("missing publication owner must retain truthful cancellation pending")
	}
	pending := execute("/contribute status")
	if pending["stop_requested"] != true || pending["effect"] != "local-only" {
		t.Fatal("pending stop lost sticky owner fence")
	}
	if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute run "+issue.URL+" no-merge"); e == nil {
		t.Fatal("replay escaped pending owner stop")
	}
	if _, e := gateway.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute narrow no-merge"); e == nil {
		t.Fatal("sticky stop expanded")
	}
	if _, err = os.Lstat(draft.Workspace); !os.IsNotExist(err) {
		t.Fatal("intake-only profile created worktree")
	}
	gateway.stop()
	service, err = build(seed)
	must(err)
	closed = false
	items, err = service.FleetRepository.ListQueueItems(ctx)
	must(err)
	stored, err := service.FleetRepository.(fleet.ContributionRepository).GetContribution(ctx, sealed.OwnerID, sealed.Key)
	must(err)
	if len(items) != 1 || items[0].ItemID != sealed.QueueItemID || !reflect.DeepEqual(stored, sealed) {
		t.Fatal("rejection/replay/restart duplicated or altered retained contribution")
	}
	must(closeService())
	closed = true
	t.Logf("candidate=%s loop=%s contract=%s run=%s owner controls retained across real process restart; exactly one contribution/Queue; no provider or GitHub writes", binary, ref.Digest, sealed.Digest, sealed.RunID)
}
