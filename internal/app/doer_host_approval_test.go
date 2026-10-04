package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
 "github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type hostApprovalRepository struct {
	fleet.Repository
	initial      registry.AgentRevision
	latest       registry.AgentRevision
	registration registry.AgentRegistration
}

func (r *hostApprovalRepository) GetAgentRevision(_ context.Context, _ string, n uint64) (registry.AgentRevision, error) {
	if n == 1 {
		return r.initial, nil
	}
	return r.latest, nil
}
func (r *hostApprovalRepository) LatestAgentRevision(context.Context, string) (registry.AgentRevision, error) {
	return r.latest, nil
}
func (r *hostApprovalRepository) GetAgentRegistration(context.Context, string) (registry.AgentRegistration, error) {
	return r.registration, nil
}
func hostApprovalFixture(t *testing.T) (*Service, core.Subject, DoerHostApprovalInput, DoerDraft, *hostApprovalRepository) {
	t.Helper()
	s, subject, input := draftFixture(t)
	input.Contract.Workspace = t.TempDir()
	s.Config.Credentials.Authority.DeploymentID = "test-deployment"
	agent, e := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: input.Agent.ID, Revision: 1, Source: registry.FleetSource{FleetID: "test", Kind: "test", SourceID: "agent"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "test"}, Ownership: registry.Ownership{OwnerID: subject.PrincipalID, AccountabilityID: subject.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: input.Agent})
	if e != nil {
		t.Fatal(e)
	}
	input.Agent = agentRevisionRef(agent)
	repo := &hostApprovalRepository{initial: agent, latest: agent, registration: registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: input.Agent}}
	s.FleetRepository = repo
	draft, e := s.SaveDoerDraftAs(context.Background(), subject, input)
	if e != nil {
		t.Fatal(e)
	}
	candidate, _, e := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if e != nil {
		t.Fatal(e)
	}
	digest, _ := draft.Contract.Digest()
	return s, subject, DoerHostApprovalInput{DraftID: draft.ID, DraftVersion: draft.Version, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"}, draft, repo
}
func TestDoerHostApprovalIndependentExactAndFresh(t *testing.T) {
	s, subject, input, draft, repo := hostApprovalFixture(t)
	ctx := context.Background()
	if s.resolveDoerHostApproval(ctx, draft.Contract, repo.latest) == nil {
		t.Fatal("draft inferred authority")
	}
	signed, e := s.ApproveDoerHostWriteAs(ctx, subject, input)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.ReadDoerHostWriteApprovalAs(ctx, subject, draft.Agent, draft.Contract)
	if e != nil || got.KeyID != signed.KeyID {
		t.Fatal("readback", e)
	}
	if e = s.resolveDoerHostApproval(ctx, draft.Contract, repo.latest); e != nil {
        t.Fatal(e)
    }
    policy := evidence.SelectedFilePolicy{Version:evidence.SelectedFilePolicyV1,RelativePath:draft.Contract.VerifyFile,Mode:evidence.SelectedFilePresence}
    pd,_:=policy.Digest()
    result,e:=evidence.VerifySelectedFile(ctx,draft.Contract.Workspace,policy,pd,evidence.SelectedFileBinding{AttemptID:"attempt",ActionID:"verify",RunID:"run",OwnerID:"agent",AuthorityContextID:"authority",AuthorityContextDigest:"sha256:authority"})
    if e!=nil || result.Outcome==evidence.Passed { t.Fatal("host approval mistaken for file verification",e) }
	again, e := s.ApproveDoerHostWriteAs(ctx, subject, input)
	if e != nil || !again.ApprovedAt.Equal(signed.ApprovedAt) {
		t.Fatal("repeat", e)
	}
	changed := draft.Contract
	changed.MaxAttempts = 1
	if s.resolveDoerHostApproval(ctx, changed, repo.latest) == nil {
		t.Fatal("changed contract inferred approval")
	}
	s.Config.Credentials.Authority.DeploymentID = "other"
	if s.resolveDoerHostApproval(ctx, draft.Contract, repo.latest) == nil {
		t.Fatal("cross deployment")
	}
	s.Config.Credentials.Authority.DeploymentID = "test-deployment"
	old := repo.latest
	repo.latest.Revision = 2
	repo.latest, _ = registry.SealRevision(repo.latest)
	if s.resolveDoerHostApproval(ctx, draft.Contract, old) == nil {
		t.Fatal("stale Agent")
	}
	repo.latest = old
	future := s.Now().Add(25 * time.Hour)
	s.Now = func() time.Time { return future }
	if s.resolveDoerHostApproval(ctx, draft.Contract, old) == nil {
		t.Fatal("expired approval")
	}
}

func TestDoerHostApprovalDenialsDoNotWrite(t *testing.T) {
	for _, which := range []string{"decision", "candidate", "contract", "version", "stale-auth", "owner", "unsafe-file", "unsafe-parent", "hardlink", "missing-workspace"} {
		t.Run(which, func(t *testing.T) {
			s, subject, input, draft, repo := hostApprovalFixture(t)
			switch which {
			case "decision":
				input.Decision = "publish"
			case "candidate":
				input.ExpectedCandidateDigest = "wrong"
			case "contract":
				input.ExpectedContractDigest = "wrong"
			case "version":
				input.DraftVersion++
			case "stale-auth":
				subject.AuthenticatedAt = s.Now().Add(-2 * s.Config.Principal.AuthTTL)
			case "owner":
				repo.initial.Ownership.OwnerID = "other"
				repo.initial, _ = registry.SealRevision(repo.initial)
			case "unsafe-file":
				if e := os.Symlink("elsewhere", filepath.Join(draft.Contract.Workspace, "result.txt")); e != nil {
					t.Fatal(e)
				}
			case "unsafe-parent":
				if e := os.Rename(draft.Contract.Workspace, draft.Contract.Workspace+".real"); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					os.Remove(draft.Contract.Workspace)
					os.Rename(draft.Contract.Workspace+".real", draft.Contract.Workspace)
				})
				if e := os.Symlink(draft.Contract.Workspace+".real", draft.Contract.Workspace); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				name := filepath.Join(draft.Contract.Workspace, "result.txt")
				if e := os.WriteFile(name, []byte("existing"), 0600); e != nil {
					t.Fatal(e)
				}
				if e := os.Link(name, name+".alias"); e != nil {
					t.Fatal(e)
				}
			case "missing-workspace":
				if e := os.Remove(draft.Contract.Workspace); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := s.ApproveDoerHostWriteAs(context.Background(), subject, input); e == nil {
				t.Fatal("denial authorized")
			}
			if _, e := os.Stat(filepath.Join(s.Store.Root(), "doer-host-approvals")); !os.IsNotExist(e) {
				t.Fatal("denial wrote approval", e)
			}
		})
	}
	for _, raw := range []string{`null`, `{"decision":"approve-host-write","decision":"publish"}`, `{"decision":"approve-host-write","authority":"all"}`} {
		var v DoerHostApprovalInput
		if json.Unmarshal([]byte(raw), &v) == nil {
			t.Fatal("invalid typed decision accepted", raw)
		}
	}
}
func TestDoerHostApprovalWorkerResolvesFreshWithoutAllowlist(t *testing.T) {
	s, subject, input, draft, repo := hostApprovalFixture(t)
	ctx := context.Background()
	worker := &orchestration.QueueWorker{}
	if e := s.ConfigureFleet(repo, &orchestration.FleetService{}, worker); e != nil {
		t.Fatal(e)
	}
	executable := filepath.Join(s.Store.Root(), "explicit-test-helper")
	e := os.WriteFile(executable, []byte("#!/bin/sh\nexit 1\n"), 0700)
	if e != nil { t.Fatal(e) }
	home := t.TempDir()
	if e = os.Chmod(home, 0700); e != nil {
		t.Fatal(e)
	}
	// Explicit prerequisite paths initialize the controller, but grant nothing.
	if e = worker.ConfigureImplementation(config.Implementation{GoBinary: executable, LayaPython: executable, LayaHome: home}, s.Store.Root(), s.Hermes); e != nil {
		t.Fatal(e)
	}
	revision, _, e := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if e != nil {
		t.Fatal(e)
	}
	if e = worker.ValidateLoopAdmission(revision, repo.latest); !errors.Is(e, orchestration.ErrDoerHostApprovalRequired) {
		t.Fatal("missing approval", e)
	}
	if _, e = s.ApproveDoerHostWriteAs(ctx, subject, input); e != nil {
		t.Fatal(e)
	}
	if e = worker.ValidateLoopAdmission(revision, repo.latest); e != nil {
		t.Fatal("signed approval not resolved", e)
	}
	// Tampering after successful preflight must not leave a cached grant.
	slot := hostapproval.Slot("test-deployment", subject.PrincipalID, draft.Agent, input.ExpectedContractDigest)
	if e = os.WriteFile(filepath.Join(s.Store.Root(), "doer-host-approvals", slot+".json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e = worker.ValidateLoopAdmission(revision, repo.latest); !errors.Is(e, orchestration.ErrDoerHostApprovalRequired) {
		t.Fatal("cached grant", e)
	}
}
