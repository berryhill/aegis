package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/core"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

type engineeringAppFixture struct {
	s        *Service
	subject  core.Subject
	ref      reference.RevisionRef
	reader   *fixedContributionReader
	counter  string
	revision loop.LoopRevision
	template loop.VerifiedImplementation
	spec     contribution.IssueEngineeringScope
}

func newEngineeringAppFixture(t *testing.T, outputs []string) engineeringAppFixture {
	ctx := context.Background()
	s, repo, subject, oldRef, reader := contributionAuthorityFixture(t, func(c *core.Charter) {
		for i := range c.Stanzas {
			c.Stanzas[i].Grant = core.Grant{}
			c.Stanzas[i].Scopes = core.Scopes{}
			c.Stanzas[i].Hermes.Toolsets = []string{}
			c.Stanzas[i].Hermes.Provider = "none"
		}
	})
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	subject.AuthenticatedAt = now
	subject.ExpiresAt = now.Add(time.Minute)
	configureLocalImportFleet(t, s, repo)
	old, err := s.GetLoopViewAs(ctx, subject, oldRef.ID, oldRef.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p := *old.Revision.Contribution
	root := t.TempDir()
	if err = os.Mkdir(filepath.Join(root, "feature"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"go.mod": "module fixture\n\ngo 1.24\n", "trusted_test.go": "package main\nimport \"testing\"\nfunc TestBehavior(t *testing.T){}\nfunc TestSecurity(t *testing.T){}\nfunc TestLaunch(t *testing.T){}\nfunc TestDenial(t *testing.T){}\n", "feature/value.go": "package feature\n", "feature/regression_test.go": "package feature\n"}
	for path, b := range files {
		if err = os.WriteFile(filepath.Join(root, path), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := loop.ImplementationDraft("template, not issue outcome", "immutable checks")
	c.Workspace = filepath.Join(t.TempDir(), "template")
	c.WritableFiles = []string{"unused.go"}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(c.Task)), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
	for _, path := range []string{"go.mod", "trusted_test.go"} {
		role := "trusted_test"
		if path == "go.mod" {
			role = "checker_input"
		}
		b := files[path]
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: path, Role: role, Bytes: int64(len(b)), Digest: contribution.Digest([]byte(b))})
	}
	c.RepositoryContext.SourceDigest = implementation.RepositorySourceDigest(c.RepositoryContext)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.EvalSymlinks(git)
	if err != nil {
		t.Fatal(err)
	}
	managed := loop.ContributionExecutionPolicy{Kind: loop.EngineeringExecutionKind, ReviewMode: "issue-journeys.v1", RepositoryPath: root, GitBinary: git, BaseBranch: "main", BuildPackage: "."}
	for _, kind := range []string{"behavior", "security", "launch", "denial"} {
		name := "Test" + map[string]string{"behavior": "Behavior", "security": "Security", "launch": "Launch", "denial": "Denial"}[kind]
		test := loop.RequiredGoTest{Package: "fixture", Name: name}
		c.Policy.RequiredTests = append(c.Policy.RequiredTests, test)
		managed.Criteria = append(managed.Criteria, loop.ContributionCriterion{ID: kind, Kind: kind, Test: test})
	}
	for _, kind := range []string{"security", "launch"} {
		managed.ReviewAssets = append(managed.ReviewAssets, loop.ContributionReviewAsset{ID: kind, Scope: kind, CriterionID: kind, Path: "trusted_test.go", BaselineDigest: contribution.Digest([]byte(files["trusted_test.go"]))})
	}
	p.Managed = &managed
	p.FinalVerification = "aegis.contribution.final.v1"
	p.Engineering = &loop.IssueEngineeringPolicy{WritablePrefixes: []string{"feature"}, MaxWritableFiles: 4}
	revision, _, err := loop.NewContributionRevision("issue-engineering", 1, "", c, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: p.Controller.ID, Revision: revision, IdempotencyKey: "engineering-publish"}); err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: revision.Digest}
	if _, err = s.SetLoopLifecycleAs(ctx, subject, ref.ID, SetLoopLifecycleInput{AgentID: p.Controller.ID, Loop: ref, State: loop.LifecycleActive, EventID: "engineering-active"}); err != nil {
		t.Fatal(err)
	}
	spec := contribution.IssueEngineeringScope{SchemaVersion: loop.IssueEngineeringSchema, Outcome: "Issue-specific repair", Acceptance: "actual trusted behavior", WritableFiles: []string{"feature/value.go", "feature/regression_test.go"}, ContextFiles: []string{}, Exclusions: []string{"do not expand"}}
	wire, _ := json.Marshal(spec)
	reader.source.Body = "Repair the value behavior and add a regression. Preserve the published trusted tests."
	// Real managed gateway protocol, controlled outputs: not a live-provider claim.
	descriptor, e := s.Hermes.Discover(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_ = descriptor
	executable, e := os.ReadFile(s.Config.HermesExecutable)
	if e != nil {
		t.Fatal(e)
	}
	_ = executable
	// testService publishes the installation location in its version response.
	version, e := exec.Command(s.Config.HermesExecutable, "--version").Output()
	if e != nil {
		t.Fatal(e)
	}
	installation := strings.TrimSpace(strings.SplitN(string(version), "Install directory: ", 2)[1])
	scopeText, _ := json.Marshal(string(wire))
	inspectText, _ := json.Marshal(`{"read_files":["feature/value.go","feature/regression_test.go"]}`)
	counter := filepath.Join(t.TempDir(), "turns")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read tools
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
read create || exit 0
n=0; [ ! -f '%s' ] || n=$(cat '%s'); n=$((n+1)); printf '%%s' "$n" > '%s'
sid="engineering-runtime-$n"
printf '{"jsonrpc":"2.0","id":"create","result":{"session_id":"%%s"}}\n' "$sid"
read prompt
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"%%s","payload":{}}}\n' "$sid"
case "$n" in 1) text='%s';; *) text='%s';; esac
printf '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"%%s","payload":{"status":"complete","text":%%s}}}\n' "$sid" "$text"
while read rest;do :;done
`, counter, counter, counter, inspectText, scopeText)
	if outputs != nil {
		cases := "case \"$n\" in "
		for i, output := range outputs {
			b, _ := json.Marshal(output)
			cases += fmt.Sprintf("%d) text='%s';; ", i+1, b)
		}
		cases += "*) text='\"unexpected turn\"';; esac"
		old := fmt.Sprintf("case \"$n\" in 1) text='%s';; *) text='%s';; esac", inspectText, scopeText)
		script = strings.Replace(script, old, cases, 1)
	}
	if e = os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return engineeringAppFixture{s, subject, ref, reader, counter, revision, c, spec}
}

func TestContributionIssueDerivedAdmissionAndBinding(t *testing.T) {
	f := newEngineeringAppFixture(t, nil)
	s, subject, ref, reader := f.s, f.subject, f.ref, f.reader
	revision, c, spec := f.revision, f.template, f.spec
	ctx := context.Background()
	sealed, err := s.admitContributionAs(ctx, subject, "session", ref, reader.source.URL.URL, "engineering-intent", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	// The public URL is an actual normalized runtime datum, not an inert port.
	if len(revision.Inputs) != 1 || revision.Inputs[0].ID != "github_issue_url" || !revision.Inputs[0].Required {
		t.Fatal("missing URL-only definition")
	}
	item, e := s.FleetRepository.GetQueueItem(ctx, sealed.QueueItemID)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.FleetRepository.GetGraphRunSnapshot(ctx, item.Snapshot.ID)
	if e != nil {
		t.Fatal(e)
	}
	var issue string
	if len(snapshot.Inputs) != 1 || snapshot.Inputs[0].PortID != "github_issue_url" || json.Unmarshal(snapshot.Inputs[0].Value, &issue) != nil || issue != sealed.Source.URL.URL {
		t.Fatal("issue URL not bound in actual execution snapshot")
	}
	bound, err := contribution.BindLoop(revision, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Task != spec.Outcome || sealed.Worktree == c.Workspace || len(bound.WritableFiles) != 2 || bound.SchemaVersion != loop.RepositoryEngineeringSchema {
		t.Fatal("template used instead of sealed issue scope")
	}
	deliveryFiles, err := contributionDeliveryFiles(revision, sealed)
	if err != nil || !slices.Equal(deliveryFiles, bound.WritableFiles) || slices.Equal(deliveryFiles, c.WritableFiles) {
		t.Fatal("delivery used template rather than sealed engineering scope", err)
	}
	deliveryFiles[0] = "caller-mutated.go"
	again, err := contributionDeliveryFiles(revision, sealed)
	if err != nil || !slices.Equal(again, bound.WritableFiles) {
		t.Fatal("delivery scope aliases sealed contract", err)
	}
	drifted := sealed
	drifted.Engineering = new(loop.VerifiedImplementation)
	*drifted.Engineering = *sealed.Engineering
	drifted.Engineering.WritableFiles = append(slices.Clone(sealed.Engineering.WritableFiles), "unapproved.go")
	if _, err = contributionDeliveryFiles(revision, drifted); err == nil {
		t.Fatal("delivery admitted changed sealed scope")
	}
	if err = contribution.ValidateImplementationContext(bound, &sealed); err != nil {
		t.Fatal(err)
	}
	bound.Task = "changed"
	if err = contribution.ValidateImplementationContext(bound, &sealed); err == nil {
		t.Fatal("changed role payload admitted")
	}
	replay, err := s.admitContributionAs(ctx, subject, "fresh-session", ref, reader.source.URL.URL, "engineering-intent", "local-only", reader)
	if err != nil || replay.Digest != sealed.Digest || reader.calls != 1 {
		t.Fatal("same intent re-resolved scope", err)
	}
}
