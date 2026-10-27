package command

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func issue351ReviewWireFixture(t *testing.T) app.DoerProtectedSetupReview {
	t.Helper()
	now := time.Now().UTC()
	charter, err := core.Canonicalize(core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "participant", Name: "Participant", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{"controller"}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 300},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 300, IdleTimeoutSec: 120, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 300, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "proof-no-key", Provider: "none"}}}, CreatedBy: "controller", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := registry.SealRevision(registry.AgentRevision{SchemaVersion: registry.AgentRevisionSchemaVersion, AgentID: "participant", Revision: 1,
		Source: registry.FleetSource{FleetID: "fixture", Kind: "current-fleet", SourceID: "selected-participant"}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}, Ownership: registry.Ownership{OwnerID: "participant-owner", AccountabilityID: "controller"}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "participant", Revision: 1, Digest: charter.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	contract := loop.DoerContract{Task: "Write the selected result", Workspace: "/approved-workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	candidate, _, err := loop.NewDoerRevision("review-scope", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}
	draft := app.DoerDraft{ID: "doerdraft-" + strings.Repeat("a", 32), PrincipalID: "controller", Version: 1, Agent: ref, LoopID: candidate.LoopID, Revision: 1, Contract: contract, CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour)}
	return app.DoerProtectedSetupReview{Action: "host", Draft: draft, Agent: &agent, Charter: &charter, Host: &app.DoerHostApprovalInput{DraftID: draft.ID, DraftVersion: 1, ExpectedCandidateDigest: candidate.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"}, Receipt: "synthetic-private-review-handle"}
}

func TestIssue351ProtectedReviewPreservesCanonicalNullableFacts(t *testing.T) {
	review := issue351ReviewWireFixture(t)
	wire, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	var decoded app.DoerProtectedSetupReview
	if !strictCompanionJSON(wire, &decoded) {
		t.Fatal("actual producer review rejected by strict native consumer")
	}
	if !reflect.DeepEqual(review, decoded) {
		t.Fatal("canonical Agent/charter bytes or review commitments changed")
	}
	if decoded.Agent.Validate() != nil || decoded.Charter.Digest != review.Charter.Digest || string(decoded.Charter.Canonical) != string(review.Charter.Canonical) {
		t.Fatal("canonical facts were normalized instead of preserved")
	}
	description := doerProtectedReviewDescription(decoded)
	if strings.Contains(description, review.Receipt) || !strings.Contains(description, `"owner_id": "participant-owner"`) {
		t.Fatal("human review omitted readable exact owner or exposed private handle")
	}
}
