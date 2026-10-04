package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/core"
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
	s.FleetRepository = draftAgentRepository{exact: old, latest: next}
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
	got, err := s.ContinueDoerDraftSuccessorAs(ctx, sub, draft.ID, draft.Version, input.Agent, next.Charter)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != draft.ID || !reflect.DeepEqual(got.Contract, draft.Contract) || got.PublicationKey == draft.PublicationKey || got.LoopID == draft.LoopID || got.Agent != agentRevisionRef(next) || got.Revision != 1 || got.PreviousDigest != "" || got.Version != draft.Version+1 || got.CreatedAt != draft.CreatedAt || got.ExpiresAt != draft.ExpiresAt {
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
