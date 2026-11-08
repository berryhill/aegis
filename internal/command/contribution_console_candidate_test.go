package command

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/implementation"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
func TestContributionCandidateRetainedDecisionConsole(t *testing.T) {
	binary := os.Getenv("AEGIS_375_CANDIDATE")
	if binary == "" {
		t.Skip("requires explicitly selected built candidate; controlled retained source only")
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
	source := contribution.Source{URL: issue, RepositoryID: 7, IssueID: 375, Title: "Controlled retained issue", Body: "Repair value with trusted regression", State: "open", CreatedAt: time.Now(), UpdatedAt: time.Now(), ObservedAt: time.Now(), Revision: "controlled", Author: contribution.Author{ID: 1, Login: "owner"}, Discussion: []contribution.Comment{}, ContentDigest: contribution.Digest([]byte("controlled source"))}
	t.Logf("CONTROLLED retained source: repository=%d issue=%d digest=%s discussion=%d", source.RepositoryID, source.IssueID, source.ContentDigest, len(source.Discussion))
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
	socketRoot, err := os.MkdirTemp("", "aegis-console-")
	must(err)
	defer os.RemoveAll(socketRoot)
	cfg.API.UnixSocket = filepath.Join(socketRoot, "api.sock")
	if _, e := os.Lstat(cfg.API.UnixSocket); !os.IsNotExist(e) {
		t.Fatal("isolated socket already present", e)
	}
	cfg.API.Token = "isolated-intake-transport-fixture"
	cfg.HermesExecutable = filepath.Join(root, "unavailable-hermes")
	must(os.WriteFile(cfg.HermesExecutable, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: "+root+"'; exit 0; fi\nexit 97\n"), 0700))
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
		charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: id, Name: id, Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: cfg.Principal.ID, CreatedAt: service.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{subject.ID}, PrincipalIDs: []string{cfg.Principal.ID}, Issuers: []string{subject.Issuer}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: 300}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "controlled", Provider: "none"}}}}
		charterBytes, err := json.Marshal(charter)
		must(err)
		canonical, err := service.ImportCharterAs(ctx, subject, charterBytes)
		must(err)
		agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: id, Revision: 1, Source: registry.FleetSource{FleetID: "candidate-intake", Kind: "current-fleet", SourceID: id}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/" + id}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: "isolated-intake"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: canonical.Digest}, CapabilityDeclarations: []string{}, PolicyRefs: []reference.DigestRef{}})
		must(err)
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: agent.Digest}
		refs = append(refs, ref)
		_, err = service.FleetRepository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: id, Source: agent.Source, InitialRevision: ref}, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "isolated intake definition; no authority grant"}})
		must(err)
	}

	// All definitions are disposable; publish with owning application service.
	data, err := os.ReadFile("testdata/contribution-console-author.json")
	must(err)
	var input contributionDraftInput
	must(json.Unmarshal(data, &input))
	input.AgentID = refs[0].ID
	input.LoopID = "independent-url-console"
	input.Revision = 1
	input.PreviousDigest = ""
	input.Profile.Controller = refs[0]
	input.Profile.Builder = refs[1]
	input.Profile.Tester = refs[2]
	input.Profile.Repository = "berryhill/aegis"
	input.Profile.RepositoryID = source.RepositoryID
	input.Profile.Effect = "verified-protected-base-merge"
	input.Implementation.Workspace = filepath.Join(root, "owned")
	input.Implementation.WritableFiles = []string{"value.go"}
	input.Profile.Engineering.WritablePrefixes = []string{"feature"}
	repoPath := filepath.Join(root, "source")
	must(os.MkdirAll(filepath.Join(repoPath, "feature"), 0700))
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.24\n", "main.go": "package main\nfunc main(){}\n", "feature/value.go": "package feature\nfunc Value()int{return 0}\n", "trusted_test.go": "package main\nimport \"testing\"\nfunc TestBehavior(t *testing.T){}\nfunc TestSecurity(t *testing.T){}\nfunc TestLaunch(t *testing.T){}\nfunc TestDenial(t *testing.T){}\n"}
	for name, text := range files {
		must(os.WriteFile(filepath.Join(repoPath, name), []byte(text), 0600))
	}
	// Replace escaped literal line delimiters in test data only.
	for name, text := range files {
		text = strings.ReplaceAll(text, `\n`, "\n")
		text = strings.ReplaceAll(text, `\"`, `"`)
		files[name] = text
		must(os.WriteFile(filepath.Join(repoPath, name), []byte(text), 0600))
	}
	input.Profile.Managed.RepositoryPath = repoPath
	input.Implementation.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(input.Implementation.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, name := range []string{"go.mod", "trusted_test.go"} {
		role := "trusted_test"
		if name == "go.mod" {
			role = "checker_input"
		}
		input.Implementation.RepositoryContext.Files = append(input.Implementation.RepositoryContext.Files, loop.RepositoryContextFile{Path: name, Role: role, Bytes: int64(len(files[name])), Digest: contribution.Digest([]byte(files[name]))})
	}
	input.Implementation.RepositoryContext.SourceDigest = implementation.RepositorySourceDigest(input.Implementation.RepositoryContext)
	input.Implementation.Policy.RequiredTests = nil
	for i := range input.Profile.Managed.Criteria {
		cr := &input.Profile.Managed.Criteria[i]
		cr.Test.Package = "fixture"
		input.Implementation.Policy.RequiredTests = append(input.Implementation.Policy.RequiredTests, cr.Test)
	}
	for i := range input.Profile.Managed.ReviewAssets {
		a := &input.Profile.Managed.ReviewAssets[i]
		a.BaselineDigest = contribution.Digest([]byte(files["trusted_test.go"]))
	}
	bytes, err := json.Marshal(input)
	must(err)
	authorInput := filepath.Join(root, "author.json")
	must(os.WriteFile(authorInput, bytes, 0600))
	publication := filepath.Join(root, "publication.json")
	cmd := exec.Command(binary, "--config", path, "loops", "contribution", authorInput, "--output", publication)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual author command: %v %s", err, output)
	}
	t.Logf("actual CLI author: %s", output)
	var pub app.PublishLoopInput
	bytes, err = os.ReadFile(publication)
	must(err)
	must(json.Unmarshal(bytes, &pub))
	if len(pub.Revision.Inputs) != 1 || pub.Revision.Inputs[0].ID != "github_issue_url" || !pub.Revision.Inputs[0].Required {
		t.Fatal("not URL-only", pub.Revision.Inputs)
	}
	_, err = service.PublishLoopAs(ctx, subject, pub)
	must(err)
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: pub.Revision.LoopID, Revision: 1, Digest: pub.Revision.Digest}
	_, err = service.SetLoopLifecycleAs(ctx, subject, ref.ID, app.SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: ref, State: loop.LifecycleActive, EventID: "independent-active"})
	must(err)
	service.ContributionReader = &publicRootContributionReader{source: source}
	_, preparationErr := service.AdmitContributionAs(ctx, subject, "controlled-original-session", ref, issue.URL, app.ContributionIntentKey(ref, issue.URL, ""), "")
	t.Logf("controlled seed owning-service preparation held: %v", preparationErr)
	if _, err = service.ReadContributionEngineeringAs(ctx, subject, app.ContributionIntentKey(ref, issue.URL, "")); err != nil {
		t.Fatalf("no retained intent %v", err)
	}
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
	jar, err := cookiejar.New(nil)
	must(err)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	origin := cfg.API.Console.Origin
	login := url.Values{"password": {"isolated-intake-fixture-password"}}
	req, err := http.NewRequest("POST", origin+"/console/login", strings.NewReader(login.Encode()))
	must(err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", origin)
	response, err := client.Do(req)
	must(err)
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	must(err)
	if response.StatusCode != 200 {
		t.Fatalf("login=%d", response.StatusCode)
	}
	response, err = client.Get(origin + "/console/api/state")
	must(err)
	var state struct {
		CSRF string `json:"csrf"`
	}
	must(json.NewDecoder(response.Body).Decode(&state))
	response.Body.Close()
	if state.CSRF == "" {
		t.Fatal("no authenticated CSRF")
	}
	response, err = client.Get(origin + "/console/loops?loop_id=" + ref.ID + "&record_key=" + url.QueryEscape(ref.ID+":1"))
	must(err)
	body, err = io.ReadAll(response.Body)
	response.Body.Close()
	must(err)
	must(os.WriteFile(filepath.Join(root, "console-detail.html"), body, 0600))
	t.Logf("actual Console detail status=%d URL-form=%t", response.StatusCode, strings.Contains(string(body), `name="github_issue_url"`))
	values := url.Values{"csrf": {state.CSRF}, "revision": {"1"}, "digest": {ref.Digest}, "github_issue_url": {issue.URL}, "action": {"run"}}
	stopSubmitted := false
	call := func(action string) (int, []byte) {
		t.Helper()
		values.Set("action", action)
		r, e := http.NewRequest("POST", origin+"/console/loops/contribution?loop_id="+ref.ID, strings.NewReader(values.Encode()))
		must(e)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		resp, e := client.Do(r)
		must(e)
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		must(e)
		must(os.WriteFile(filepath.Join(root, "console-"+action+".html"), b, 0600))
		t.Logf("actual Console %s HTTP%d held=%t independent-purpose=%t", action, resp.StatusCode, strings.Contains(string(b), "contribution_preparation_held"), strings.Contains(string(b), "engineering-preparation"))
		if resp.StatusCode != 200 {
			t.Fatalf("%s HTTP%d", action, resp.StatusCode)
		}
		for _, required := range []string{"engineering-preparation", "builder", "plan-", "Plan digest", "intake-builder", "unavailable", "No human approval request was delivered", "independently", "Current delivery target"} {
			if !strings.Contains(string(b), required) {
				t.Fatalf("%s missing rendered decision assertion %q", action, required)
			}
		}
		if action == "stop" || (action == "status" && stopSubmitted) {
			if !strings.Contains(string(b), "Stop retained") {
				t.Fatal("stop absent")
			}
		} else if !strings.Contains(string(b), `value="protected-review"`) {
			t.Fatal("no supported protected continuation")
		}
		return resp.StatusCode, b
	}
	code, body := call("run")
	if code != 200 {
		t.Fatalf("Run URL-only failed HTTP%d %s", code, body)
	}
	call("status")
	call("resume")
	call("protected-review")
	values.Set("delivery", "local-only")
	_, narrow := call("narrow")
	if !strings.Contains(string(narrow), "<strong>local-only</strong>") {
		t.Fatal("narrow target not projected")
	}
	values.Del("delivery")
	call("stop")
	stopSubmitted = true
	code, body = call("status")
	if code != 200 || !strings.Contains(string(body), "Stop retained") {
		t.Fatal("retained status stop absent")
	}
	gateway.stop()
	service, err = build(seed)
	must(err)
	closed = false
	pending, err := service.ReadContributionEngineeringAs(ctx, subject, app.ContributionIntentKey(ref, issue.URL, ""))
	must(err)
	t.Logf("retained engineering phase=%s outcome=%s turns=%d decision=%+v", pending.Phase, pending.Outcome, pending.ReservedTurns, pending.Decision)
	if pending.ReservedTurns != 0 || !pending.Control.Stop || pending.Control.Delivery != "local-only" || pending.Decision == nil || pending.Decision.Status != "unavailable" || pending.Decision.ApprovalID != "" || pending.Decision.ReceiptID != "" {
		t.Fatal("retained authority/control drift")
	}
	items, err := service.FleetRepository.ListQueueItems(ctx)
	must(err)
	if len(items) != 0 {
		t.Fatal("unadmitted authority created Queue", len(items))
	}
	must(closeService())
	closed = true
}
