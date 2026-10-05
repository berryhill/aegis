package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func candidateReadinessFixture(t *testing.T, model string, modifiers ...func(*core.TrustStanza)) (*app.Service, core.Subject, app.DoerCandidateReadinessInput) {
	t.Helper()
	s := apiService(t)
	configureAPIFleet(t, s)
	ctx := context.Background()
	subject, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "candidate-doer", Name: "Candidate Doer", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{s.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true},
			Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: model, Provider: "none"}}}, CreatedBy: s.Config.Principal.ID, CreatedAt: s.Now()}
	for _, modify := range modifiers {
		modify(&c.Stanzas[0])
	}
	wire, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := s.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "candidate-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "candidate-source", AgentID: c.AgentID, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: "candidate-owner", AccountabilityID: "candidate-owner"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 1, Digest: charter.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	agent, _, err := s.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(fixture, "candidate-fleet", "candidate-source"))
	if err != nil {
		t.Fatal(err)
	}
	candidate, _, err := loop.NewDoerRevision("unpublished-candidate", 1, "", loop.DoerContract{Task: "Create selected file", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	return s, subject, app.DoerCandidateReadinessInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.Revision.AgentID, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}, Candidate: candidate}
}

func TestDoerCandidateReadinessFoundationalBlockers(t *testing.T) {
	for _, model := range []string{"none", "proof-no-key"} {
		t.Run(model, func(t *testing.T) {
			s, subject, input := candidateReadinessFixture(t, model)
			ctx := context.Background()
			want := "doer_model_required"
			if model != "none" {
				want = "provisioning_receipt_missing"
			}
			for i := 0; i < 2; i++ {
				r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, input)
				if err != nil || !r.CanAuthor || r.CanExecute || r.Reason != want || r.RequiredCharter == nil || r.ContractDigest == "" || r.Candidate.Digest != input.Candidate.Digest {
					t.Fatalf("readiness: %+v, %v", r, err)
				}
				if r.Helper.State != "not_checked" || r.Runtime.State != "not_checked" || r.HelperQualification != "protocol_availability_only_not_task_eligibility" {
					t.Fatalf("misleading qualification: %+v", r)
				}
				probe, err := s.ProbeDoerCandidateReadinessAs(ctx, subject, input)
				if err != nil || probe.Reason != want || probe.CanExecute {
					t.Fatalf("probe bypassed prerequisite: %+v %v", probe, err)
				}
			}
			// A receipt for a different immutable charter is not foundational authority.
			if err := s.Store.Save("receipts", "wrong-charter", core.Receipt{ID: "wrong-charter", CharterDigest: "sha256:" + strings.Repeat("a", 64), Status: "verified"}); err != nil {
				t.Fatal(err)
			}
			r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, input)
			if err != nil || r.Reason != want {
				t.Fatalf("unrelated receipt accepted: %+v %v", r, err)
			}
			loops, err := s.ListLoopsAs(ctx, subject)
			if err != nil || len(loops) != 0 {
				t.Fatalf("candidate was published: %+v %v", loops, err)
			}
			items, err := s.ListQueueAs(ctx, subject)
			if err != nil || len(items) != 0 {
				t.Fatalf("candidate reserved queue work: %+v %v", items, err)
			}
			graphs, err := s.ListGraphsAs(ctx, subject)
			if err != nil || len(graphs) != 0 {
				t.Fatalf("candidate created graph: %+v %v", graphs, err)
			}
			if _, err := os.Stat(input.Candidate.Doer.Workspace + "/result.txt"); !os.IsNotExist(err) {
				t.Fatalf("candidate wrote output: %v", err)
			}
		})
	}
}

func TestDoerCandidateReadinessRejectsSubjectAndReferenceSubstitution(t *testing.T) {
	s, subject, input := candidateReadinessFixture(t, "proof-no-key")
	ctx := context.Background()
	for _, bad := range []core.Subject{{}, func() core.Subject { v := subject; v.ExpiresAt = s.Now().Add(-time.Second); return v }(), func() core.Subject { v := subject; v.PrincipalID = "other"; return v }()} {
		r, err := s.ProbeDoerCandidateReadinessAs(ctx, bad, input)
		if !errors.Is(err, app.ErrDenied) || r.CanAuthor || r.CanExecute || r.Subject.State != "blocked" {
			t.Fatalf("subject substituted: %+v %v", r, err)
		}
	}
	mismatch := subject
	mismatch.ID = "nonmatching-subject"
	r, err := s.ReadDoerCandidateReadinessAs(ctx, mismatch, input)
	if err != nil || !r.CanAuthor || r.CanExecute || r.Reason != "session_selection_zero_authorized_matches" {
		t.Fatalf("selection substituted authenticated peer: %+v %v", r, err)
	}
	for _, bad := range []app.DoerCandidateReadinessInput{func() app.DoerCandidateReadinessInput {
		v := input
		v.Agent.Digest = "sha256:" + strings.Repeat("b", 64)
		return v
	}(), func() app.DoerCandidateReadinessInput { v := input; v.Agent.Revision++; return v }(), func() app.DoerCandidateReadinessInput {
		v := input
		v.Candidate.Digest = "sha256:" + strings.Repeat("c", 64)
		return v
	}(), func() app.DoerCandidateReadinessInput { v := input; v.Candidate.EntryStepID = "implement"; return v }()} {
		r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, bad)
		if !errors.Is(err, app.ErrDenied) || r.CanAuthor || r.CanExecute {
			t.Fatalf("noncanonical reference accepted: %+v %v", r, err)
		}
	}
	// An enabled successor makes an otherwise exact historic Agent ref stale.
	_, err = s.SetAgentLifecycleAs(ctx, subject, input.Agent.ID, app.SetAgentLifecycleInput{Expected: input.Agent, Lifecycle: registry.LifecycleDisabled})
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.ReadDoerCandidateReadinessAs(ctx, subject, input)
	if !errors.Is(err, app.ErrDenied) || r.AgentReference.Reason != "exact_latest_agent_required" {
		t.Fatalf("stale Agent accepted: %+v %v", r, err)
	}
}

func TestDoerCandidateReadinessRequiresToolAndCredentialFreeStanza(t *testing.T) {
	for _, test := range []struct {
		name    string
		modify  func(*core.TrustStanza)
		blocked string
	}{
		{"tools", func(s *core.TrustStanza) { s.Hermes.Toolsets = []string{"web"}; s.Grant.Tools = []string{"web"} }, "tool"},
		{"credentials", func(s *core.TrustStanza) { s.Scopes.Credentials = []string{"example"} }, "credential"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, subject, input := candidateReadinessFixture(t, "proof-no-key", test.modify)
			r, err := s.ReadDoerCandidateReadinessAs(context.Background(), subject, input)
     wantReason:="doer_tool_free_authority_required";if test.blocked=="credential"{wantReason="doer_agent_credentials_denied"}
			if err != nil || !r.CanAuthor || r.CanExecute || r.Reason != wantReason || r.Receipt.State != "not_checked" {
				t.Fatalf("unsafe stanza accepted: %+v %v", r, err)
			}
			if test.blocked == "tool" && r.ToolFree.State != "blocked" || test.blocked == "credential" && r.CredentialFree.State != "blocked" {
				t.Fatalf("wrong prerequisite: %+v", r)
			}
		})
	}
}

func TestDoerCandidateReadinessHelperProbeQualification(t *testing.T) {
	s, subject, input := candidateReadinessFixture(t, "proof-no-key")
	ctx := context.Background()
	r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || r.RequiredCharter == nil {
		t.Fatalf("fixture: %+v %v", r, err)
	}
	// Synthetic custody fixture; no approval, provisioning or real runtime launch.
	if err := s.Store.Save("receipts", "exact-fixture", core.Receipt{ID: "exact-fixture", CharterDigest: r.RequiredCharter.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	r, err = s.ReadDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || r.Reason != "implementation_prerequisite_required" || r.Runtime.State != "ready" || r.CanExecute {
		t.Fatalf("missing controller: %+v %v", r, err)
	}
	binary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: binary, AuthorizedContracts: []string{r.ContractDigest}, LayaPython: helper, LayaHome: home}, s.Config.StateDir, s.Hermes); err != nil {
		t.Fatal(err)
	}
	r, err = s.ReadDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || r.Reason != "helper_probe_required" || r.Controller.State != "ready" || r.Helper.State != "not_checked" || r.CanExecute {
		t.Fatalf("read overclaimed: %+v %v", r, err)
	}
	r, err = s.ProbeDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || r.Reason != "local_laya_unavailable" || r.Helper.State != "blocked" || r.CanExecute {
		t.Fatalf("silent helper qualified: %+v %v", r, err)
	}
	// A well-formed negative verdict STILL proves protocol availability, not
	// eligibility. The actual task gate must independently evaluate execution.
	response := `{"version":1,"kind":"gate","answers":{"specified":{"choice":"no","answer_confidence":0.9},"result_defined":{"choice":"no","answer_confidence":0.9}}}`
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' '"+response+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r, err = s.ProbeDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || !r.CanExecute || !r.CanAuthor || r.Helper.State != "ready" || r.Reason != "" || r.HelperQualification != "protocol_availability_only_not_task_eligibility" {
		t.Fatalf("protocol qualification: %+v %v", r, err)
	}
	loops, err := s.ListLoopsAs(ctx, subject)
	if err != nil || len(loops) != 0 {
		t.Fatalf("probe published candidate: %+v %v", loops, err)
	}
	items, err := s.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 0 {
		t.Fatalf("probe reserved work: %+v %v", items, err)
	}
	if _, err := os.Stat(input.Candidate.Doer.Workspace + "/result.txt"); !os.IsNotExist(err) {
		t.Fatalf("probe executed task: %v", err)
	}
}

func TestDoerCandidateReadinessCanAuthorWithoutQueueController(t *testing.T) {
	s, subject, input := candidateReadinessFixture(t, "none")
	s.QueueWorker = nil
	r, err := s.ReadDoerCandidateReadinessAs(context.Background(), subject, input)
	if err != nil || !r.CanAuthor || r.CanExecute || r.Reason != "doer_model_required" {
		t.Fatalf("registry authoring unnecessarily requires controller: %+v %v", r, err)
	}
}
