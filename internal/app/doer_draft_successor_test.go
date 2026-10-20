package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestDoerDraftSuccessorRetainsInputsAndArchivesExactBinding(t *testing.T) {
	s, sub, input := draftFixture(t)
	ctx := context.Background()
	c := testCharter(s.Now())
	c.AgentID = input.Agent.ID
	c.Revision = 2
	canonical, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	old := registry.AgentRevision{AgentID: input.Agent.ID, Revision: input.Agent.Revision, Digest: input.Agent.Digest, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: input.Agent.ID, Revision: 1, Digest: input.Agent.Digest}, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID}}
	next := old
	next.Revision++
	next.Digest = canonical.Digest
	next.Charter = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 2, Digest: canonical.Digest}
	next.CharterSuccessor = &registry.CharterSuccessor{Previous: input.Agent, ApprovedBy: sub.PrincipalID}
	text := "exact\ntext"
	input.Contract.ExpectedText = &text
	draft, err := s.SaveDoerDraftAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	revision, validation, err := NewDoerLoopRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil {
		t.Fatal(err)
	}
	provenance, err := loop.NewPublicationProvenance(loop.PublicationProvenance{
		Loop:           loop.NewProvenanceRevision(revision.LoopID, revision.Revision, revision.Digest),
		PublisherAgent: loop.NewProvenanceRevision(old.AgentID, old.Revision, old.Digest),
		Charter:        loop.NewProvenanceRevision(old.Charter.ID, old.Charter.Revision, old.Charter.Digest),
		Authority:      loop.NewProvenanceDigest("workspace-agent", old.Digest),
		MandateID:      "workspace-agent", StanzaID: "workspace", Runtime: loop.ProvenanceRuntime{Runtime: "hermes-agent"},
		ValidationDigest: validation.Digest, OwnerID: sub.PrincipalID, PrincipalID: sub.PrincipalID, AuthorityKind: "registered-agent-workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	provenance = fixtureWorkspaceProvenance(t, provenance, old)
	s.FleetRepository = retainedPublicationRepository{draftAgentRepository: draftAgentRepository{exact: old, latest: next}, revision: revision, validation: validation, provenance: provenance}
	for _, name := range []string{"principal", "ref", "version", "charter"} {
		who, ref, ch, version := sub, input.Agent, next.Charter, draft.Version
		switch name {
		case "principal":
			who.PrincipalID = "wrong"
		case "ref":
			ref.Digest = canonical.Digest
		case "version":
			version++
		case "charter":
			ch.Digest = input.Agent.Digest
		}
		if _, err = s.ContinueDoerDraftSuccessorAs(ctx, who, draft.ID, version, ref, ch); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	// Isolated fixture injection proves the execution freeze cannot be rebound.
	setFreeze := func(digest string) {
		t.Helper()
		if e := s.Store.Update(doerDraftKind, doerDraftSlot(sub.PrincipalID), func(raw []byte) (any, error) {
			set, e := decodeDoerDraftSet(raw, sub.PrincipalID)
			if e != nil {
				return nil, e
			}
			set.Drafts[0].ExecutionDigest = digest
			return set, nil
		}); e != nil {
			t.Fatal(e)
		}
	}
	setFreeze(revision.Digest)
	if _, e := s.ContinueDoerDraftSuccessorAs(ctx, sub, draft.ID, draft.Version, input.Agent, next.Charter); e == nil {
		t.Fatal("rebound frozen execution")
	}
	setFreeze("")
	got, err := s.ContinueDoerDraftSuccessorAs(ctx, sub, draft.ID, draft.Version, input.Agent, next.Charter)
	if err != nil {
		t.Fatal(err)
	}
	if publication, e := s.retainedDoerPublication(ctx, sub, got); e != nil || publication == nil || publication.Revision.Digest != revision.Digest || !publication.Decision.Idempotent {
		t.Fatalf("successor cannot reuse original publication: %+v %v", publication, e)
	}
	if got.ID != draft.ID || !reflect.DeepEqual(got.Contract, draft.Contract) || got.PublicationKey != draft.PublicationKey || got.LoopID != draft.LoopID || got.Agent != agentRevisionRef(next) || got.Revision != draft.Revision || got.PreviousDigest != draft.PreviousDigest || got.Version != draft.Version+1 || got.CreatedAt != draft.CreatedAt || got.ExpiresAt != draft.ExpiresAt {
		t.Fatalf("bad rebind: %+v", got)
	}
	var archived []DoerDraft
	err = s.Store.List("doer-draft-bindings", func(raw json.RawMessage) error {
		var d DoerDraft
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		archived = append(archived, d)
		return nil
	})
	if err != nil || len(archived) != 1 || !reflect.DeepEqual(archived[0], draft) {
		t.Fatalf("archive=%+v err=%v", archived, err)
	}
	if _, err = s.ContinueDoerDraftSuccessorAs(ctx, sub, draft.ID, draft.Version, input.Agent, next.Charter); err == nil {
		t.Fatal("accepted rebind replay")
	}
	approvals, err := s.ListApprovals()
	if err != nil || len(approvals) != 0 {
		t.Fatalf("auto approval: %v %v", approvals, err)
	}
	receipts, err := s.ListReceipts()
	if err != nil || len(receipts) != 0 {
		t.Fatalf("auto provisioning: %v %v", receipts, err)
	}
}
