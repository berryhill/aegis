package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDoerReadinessActualCauseChainBothSchemas(t *testing.T) {
	s, subject, old := candidateReadinessFixture(t, "proof-no-key")
	ctx := context.Background()
	r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, old)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "cause-fixture", core.Receipt{ID: "cause-fixture", CharterDigest: r.RequiredCharter.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
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
	os.Chmod(home, 0700)
	helper := filepath.Join(t.TempDir(), "helper")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = s.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: binary, LayaPython: helper, LayaHome: home}, s.Config.StateDir, s.Hermes); err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{loop.DoerRevisionSchemaVersion, loop.DoerReusableSchemaVersion} {
		candidate := old.Candidate
		var inputs []graph.NormalizedInput
		if schema == loop.DoerReusableSchemaVersion {
			candidate, _, err = loop.NewDoerReusableRevision("cause-reusable", 1, "", loop.DoerReusableContract{MaxAttempts: 1})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []struct {
				id    string
				typ   graph.ValueType
				value any
			}{{"task", graph.TypeString, old.Candidate.Doer.Task}, {"workspace", graph.TypeString, old.Candidate.Doer.Workspace}, {"writable_files", graph.TypeArray, old.Candidate.Doer.WritableFiles}, {"verify_file", graph.TypeString, old.Candidate.Doer.VerifyFile}} {
				b, _ := json.Marshal(p.value)
				inputs = append(inputs, graph.NormalizedInput{PortID: p.id, Type: p.typ, Value: b})
			}
		}
		if _, err = s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: old.Agent.ID, Revision: candidate, IdempotencyKey: "publish-" + candidate.LoopID}); err != nil {
			t.Fatal(err)
		}
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}
		for _, tc := range []struct {
			name  string
			cause error
			want  string
		}{{"absent", hostapproval.ErrAbsent, "doer_host_approval_absent"}, {"invalid", hostapproval.ErrDenied, "doer_host_approval_invalid"}, {"expired", hostapproval.ErrExpired, "doer_host_approval_expired"}, {"secret", errors.New("secret=/private/token"), "doer_prerequisite_unavailable"}} {
			t.Run(schema+"/"+tc.name, func(t *testing.T) {
				s.QueueWorker.SetDoerHostApprovalResolver(func(context.Context, loop.DoerContract, registry.AgentRevision) error { return tc.cause })
				input := app.DoerReadinessInput{Agent: old.Agent, Candidate: candidate, Probe: true}
				if schema == loop.DoerReusableSchemaVersion {
					input.Candidate = loop.LoopRevision{}
					input.Loop = ref
					input.Inputs = inputs
					input.IdempotencyKey = "observe-" + tc.name
				}
				observed, err := s.ReadDoerReadinessAs(ctx, subject, input)
				wantAction := "review_exact_host_write_contract"
				if schema == loop.DoerReusableSchemaVersion {
					// The supported v5 review binds the exact Run payload;
					// missing, invalid or expired consent still denies execution.
					wantAction = "review_exact_reusable_run_host_write"
				}
				if tc.name == "secret" {
					wantAction = "controller_diagnostic_adapter_unavailable"
				}
				if err != nil || observed.Reason != tc.want || observed.RequiredAction != wantAction || observed.Controller.State != "blocked" || observed.Helper.State != "not_checked" || observed.CanExecute {
					t.Fatalf("readiness %+v %v", observed, err)
				}
				if observed.RequestID != "" {
					var retained string
					if err = s.Store.Load("queue-loop-intent", observed.RequestID, &retained); !os.IsNotExist(err) {
						t.Fatalf("observation reserved: %v", err)
					}
				}
			})
		}
	}
	items, err := s.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 0 {
		t.Fatalf("effects %+v %v", items, err)
	}
}
