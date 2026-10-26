package command

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestIssue351ProtectedResultRejectsRunSubstitution(t *testing.T) {
	review := issue351ReviewWireFixture(t)
	draft := review.Draft
	reusable := &loop.DoerReusableContract{MaxAttempts: draft.Contract.MaxAttempts}
	candidate, _, err := loop.NewDoerReusableRevision(draft.LoopID, 1, "", *reusable)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	draft.HostRun = &app.QueueLoopInput{Agent: draft.Agent, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: 1, Digest: candidate.Digest}, IdempotencyKey: "reviewed-run", Activate: true, Inputs: []graph.NormalizedInput{
		{PortID: "task", Type: graph.TypeString, Value: encode(draft.Contract.Task)},
		{PortID: "workspace", Type: graph.TypeString, Value: encode(draft.Contract.Workspace)},
		{PortID: "writable_files", Type: graph.TypeArray, Value: encode(draft.Contract.WritableFiles)},
		{PortID: "verify_file", Type: graph.TypeString, Value: encode(draft.Contract.VerifyFile)},
	}}
	review.Draft = draft
	review.Host.ExpectedCandidateDigest = candidate.Digest
	cfg := config.Defaults()
	cfg.Principal.ID = draft.PrincipalID
	cfg.Credentials.Authority.DeploymentID = "reviewed-deployment"
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := draft.Contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	grant, err := hostapproval.Sign(hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: cfg.Credentials.Authority.DeploymentID, PrincipalID: draft.PrincipalID, OwnerID: review.Agent.Ownership.OwnerID, ControllerSubjectID: "independent-fixture-subject", RunInputDigest: draft.HostReviewInputDigest(), Agent: draft.Agent, Candidate: draft.HostRun.Loop, Contract: draft.Contract, ContractDigest: digest, ApprovedAt: now, ExpiresAt: now.Add(time.Hour), RunRequestID: draft.HostReviewRequestID(), DoerReusable: reusable}, key)
	if err != nil {
		t.Fatal(err)
	}
	result := app.DoerProtectedSetupResult{Status: "approved", Draft: draft, Host: &grant}
	if !verifyDoerProtectedResult(cfg, review, result) {
		t.Fatal("matching exact reviewed commitments rejected")
	}
	for name, mutate := range map[string]func(*hostapproval.Record){
		"wrong-run":       func(r *hostapproval.Record) { r.RunRequestID = "other-run" },
		"wrong-payload":   func(r *hostapproval.Record) { r.RunInputDigest = "sha256:" + strings.Repeat("c", 64) },
		"wrong-owner":     func(r *hostapproval.Record) { r.OwnerID = "other-owner" },
		"wrong-principal": func(r *hostapproval.Record) { r.PrincipalID = "other-principal" },
		"missing-review":  func(r *hostapproval.Record) { r.ControllerSubjectID = "" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := grant
			mutate(&bad)
			value := result
			value.Host = &bad
			if verifyDoerProtectedResult(cfg, review, value) {
				t.Fatal("substituted returned commitments accepted")
			}
		})
	}
	// Same task/key but a changed reviewed activation choice is still another
	// payload; it cannot borrow the old signed complete-input commitment.
	changed := draft
	run := *draft.HostRun
	run.Activate = false
	changed.HostRun = &run
	changedReview := review
	changedReview.Draft = changed
	changedResult := result
	changedResult.Draft = changed
	if verifyDoerProtectedResult(cfg, changedReview, changedResult) {
		t.Fatal("changed complete Run payload accepted")
	}
	wrongConfig := cfg
	wrongConfig.Principal.ID = "other-controller"
	if verifyDoerProtectedResult(wrongConfig, review, result) {
		t.Fatal("wrong configured principal accepted")
	}
}

func TestIssue351ProtectedReviewRejectsMalformedEncodedFacts(t *testing.T) {
	review := issue351ReviewWireFixture(t)
	raw, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]json.RawMessage){
		"null-agent-data":        func(v map[string]json.RawMessage) { v["agent_json"] = json.RawMessage(`"null"`) },
		"malformed-charter-data": func(v map[string]json.RawMessage) { v["charter_json"] = json.RawMessage(`"{"`) },
		"outer-null":             func(v map[string]json.RawMessage) { v["agent"] = json.RawMessage(`null`) },
		"unknown-field":          func(v map[string]json.RawMessage) { v["unreviewed"] = json.RawMessage(`true`) },
		"mixed-agent-sources":    func(v map[string]json.RawMessage) { v["agent"], _ = json.Marshal(review.Agent) },
		"unsigned-agent-data":    func(v map[string]json.RawMessage) { v["agent_json"] = json.RawMessage(`"{}"`) },
	} {
		t.Run(name, func(t *testing.T) {
			value := map[string]json.RawMessage{}
			for k, v := range original {
				value[k] = v
			}
			mutate(value)
			wire, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var decoded app.DoerProtectedSetupReview
			if strictCompanionJSON(wire, &decoded) {
				t.Fatal("malformed/ambiguous protected review accepted")
			}
		})
	}
}
