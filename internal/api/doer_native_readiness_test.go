package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// This fixture exercises real Unix-peer authentication and public HTTP readiness,
// with synthetic charter custody only. It never launches a native checker.
func TestDoerNativeReadinessAuthenticatedBoundary(t *testing.T) {
	s, subject, old := candidateReadinessFixture(t, "proof-no-key")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	initial, err := s.ReadDoerCandidateReadinessAs(ctx, subject, old)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "native-readiness", core.Receipt{ID: "native-readiness", CharterDigest: initial.RequiredCharter.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	hash := func(b []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(b)) }
	workspace := old.Candidate.Doer.Workspace
	manifest := &loop.RepositoryContext{TaskDigest: hash([]byte("Return 42")), MaxFiles: 3, MaxFileBytes: 4096, MaxTotalBytes: 8192}
	for _, f := range []struct{ path, role, text string }{
		{"value.go", "source", "package fixture\nfunc Value() int { return 0 }\n"},
		{"value_test.go", "trusted_test", "package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=42 { t.Fatal(Value()) } }\n"},
		{"go.mod", "checker_input", "module fixture\n\ngo 1.25\n"},
	} {
		if err = os.WriteFile(filepath.Join(workspace, f.path), []byte(f.text), 0600); err != nil {
			t.Fatal(err)
		}
		manifest.Files = append(manifest.Files, loop.RepositoryContextFile{Path: f.path, Role: f.role, Bytes: int64(len(f.text)), Digest: hash([]byte(f.text))})
	}
	source, err := json.Marshal(struct {
		Kind  string                       `json:"kind"`
		Files []loop.RepositoryContextFile `json:"files"`
	}{"explicit-go-source.v1", manifest.Files})
	if err != nil {
		t.Fatal(err)
	}
	manifest.SourceDigest = hash(source)
	policy := loop.ImplementationDraft("Return 42", "TestValue passes").Policy
	policy.Packages = []string{"."}
	policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	contract := loop.DoerContract{Task: "Return 42", Workspace: workspace, WritableFiles: []string{"value.go"}, MaxAttempts: 1, Coding: &loop.DoerCoding{Acceptance: "TestValue passes", RepositoryContext: manifest, Policy: policy}}
	candidate, _, err := loop.NewDoerRevision("native-readiness", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	outer, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	derived, err := contract.CodingImplementation()
	if err != nil {
		t.Fatal(err)
	}
	inner, err := derived.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: old.Agent.ID, Revision: candidate, IdempotencyKey: "publish-native-readiness"}); err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}
	// A valid signed host-write record, even for this exact contract, must not
	// replace independent native-code authorization. The resolver is never used.
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	consent, err := hostapproval.Sign(hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: "fixture", PrincipalID: subject.PrincipalID, OwnerID: subject.PrincipalID, Agent: old.Agent, Candidate: ref, Contract: contract, ContractDigest: outer, ApprovedAt: now, ExpiresAt: now.Add(time.Minute)}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = hostapproval.Verify(consent, pub, "fixture", subject.PrincipalID, old.Agent, contract, now); err != nil {
		t.Fatal(err)
	}
	resolverCalls := 0
	s.QueueWorker.SetDoerHostApprovalResolver(func(context.Context, loop.DoerContract, registry.AgentRevision) error {
		resolverCalls++
		return hostapproval.Verify(consent, pub, "fixture", subject.PrincipalID, old.Agent, contract, now)
	})
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "helper")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "absent-go")
	untrusted := filepath.Join(t.TempDir(), "untrusted-go")
	if err = os.WriteFile(untrusted, []byte("private-checker-payload"), 0600); err != nil {
		t.Fatal(err)
	}
	// Configure before serving; later cases change only startup test wiring,
	// between completed requests. No live operator configuration is modified.
	configure := func(allowed []string, checker string) {
		t.Helper()
		if err := s.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: checker, AuthorizedContracts: allowed, LayaPython: helper, LayaHome: home}, s.Config.StateDir, s.Hermes); err != nil {
			t.Fatal(err)
		}
	}
	configure(nil, missing)
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
	for _, tc := range []struct {
		name                    string
		allowed                 []string
		checker, reason, action string
	}{
		{"absent-allowlist", nil, missing, "doer_native_contract_unauthorized", "review_exact_native_contract_authorization"},
		{"loop-digest", []string{candidate.Digest}, missing, "doer_native_contract_unauthorized", "review_exact_native_contract_authorization"},
		{"derived-digest", []string{inner}, missing, "doer_native_contract_unauthorized", "review_exact_native_contract_authorization"},
		{"signed-host-consent-insufficient", nil, missing, "doer_native_contract_unauthorized", "review_exact_native_contract_authorization"},
		{"outer-authorized-missing-checker", []string{outer}, missing, "doer_go_executable_missing", "review_controller_dependency_integrity"},
		{"outer-authorized-untrusted-checker", []string{outer}, untrusted, "doer_go_executable_untrusted", "review_controller_dependency_integrity"},
		{"private-preflight-failure", []string{outer}, helper, "doer_prerequisite_unavailable", "controller_diagnostic_adapter_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "private-preflight-failure" {
				// Fail in the actual native backend after authorization, not in
				// a test-only classifier. Immutable test drift is never disclosed.
				if err := os.WriteFile(filepath.Join(workspace, "value_test.go"), []byte("private-backend-secret=/private/token"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			configure(tc.allowed, tc.checker)
			input := app.DoerReadinessInput{Agent: old.Agent, Candidate: candidate, Probe: true}
			var observed app.DoerCandidateReadiness
			apiRequest(t, client, http.MethodPost, "/v1/loops/doer/readiness", input, &observed, http.StatusOK)
			if observed.Reason != tc.reason || observed.RequiredAction != tc.action || observed.Controller.State != "blocked" || observed.CanExecute || observed.Helper.State != "not_checked" {
				t.Fatalf("readiness %+v", observed)
			}
			wire, _ := json.Marshal(observed)
			for _, secret := range []string{missing, untrusted, "private-checker-payload", "private-backend-secret", "/private/token"} {
				if strings.Contains(string(wire), secret) {
					t.Fatal("private checker diagnostic escaped")
				}
			}
			var blocked app.QueueLoopResult
			apiRequest(t, client, http.MethodPost, "/v1/loops/queue", app.QueueLoopInput{Agent: old.Agent, Loop: ref, IdempotencyKey: "native-" + tc.name, Activate: true}, &blocked, http.StatusOK)
			if blocked.Reason != tc.reason || blocked.RequiredAction != tc.action || blocked.QueueItemID != "" || blocked.Execution != nil {
				t.Fatalf("admission %+v", blocked)
			}
			wire, _ = json.Marshal(blocked)
			for _, secret := range []string{missing, untrusted, "private-checker-payload", "private-backend-secret", "/private/token"} {
				if strings.Contains(string(wire), secret) {
					t.Fatal("private admission diagnostic escaped")
				}
			}
		})
	}
	if resolverCalls != 0 {
		t.Fatalf("native authorization consulted host consent %d times", resolverCalls)
	}
	items, err := s.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 0 {
		t.Fatalf("Queue mutation %+v %v", items, err)
	}
	graphs, err := s.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 0 {
		t.Fatalf("Graph mutation %+v %v", graphs, err)
	}
	view, err := s.GetLoopViewAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil || view.Lifecycle.State == loop.LifecycleActive {
		t.Fatalf("activation %+v %v", view, err)
	}
	original, err := os.ReadFile(filepath.Join(workspace, "value.go"))
	if err != nil || string(original) != "package fixture\nfunc Value() int { return 0 }\n" {
		t.Fatal("readiness changed source")
	}
}
