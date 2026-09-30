package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func registerDoerWorkspaceAgent(t *testing.T, svc *app.Service, subject core.Subject, id, owner string) app.FleetAgent {
	t.Helper()
	fixture, err := json.Marshal(registry.CurrentFleetFixture{
		SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "doer-workspaces",
		Agents: []registry.CurrentFleetAgent{{SourceID: id, AgentID: id,
			Runtime:   registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/" + id},
			Ownership: registry.Ownership{OwnerID: owner, AccountabilityID: "team"}, Lifecycle: registry.LifecycleEnabled,
			Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, _, err := svc.RegisterFleetAgentAs(context.Background(), subject, app.NewRegisterFleetAgentInput(fixture, "doer-workspaces", id))
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func doerWorkspaceCommandFixture(t *testing.T) (*app.Service, core.Subject, *console.CommandService, app.FleetAgent) {
	t.Helper()
	svc := apiService(t)
	configureAPIFleet(t, svc)
	subject, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, subject, "existing-agent", "owner-one")
	commands, err := console.NewCommandService(loopCommandDefinitions(svc), loopCommandAuthorityProvider(svc), svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, subject, commands, agent
}

func previewDoerWorkspace(t *testing.T, svc *app.Service, commands *console.CommandService, subject core.Subject, agent app.FleetAgent, key string) (console.CommandPreview, error) {
	t.Helper()
	values := validDoerForm()
	values.Set("publication_key", key)
	form, err := decodeDoerComposerForm(doerFormRequest(values))
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(doerTemplatePublishInput{PublisherID: agent.Revision.AgentID, PublisherRevision: agent.Revision.Revision, PublisherDigest: agent.Revision.Digest, LoopID: form.Revision.LoopID, Revision: form.Revision.Revision, PreviousDigest: form.Revision.PreviousDigest, Doer: *form.Revision.Doer, PublicationKey: key})
	if err != nil {
		t.Fatal(err)
	}
	definitions := loopCommandDefinitions(svc)

	for _, definition := range definitions {
		if definition.ID == loopDoerWorkspaceCommandID {
			if _, err := definition.Normalize(input); err != nil {
				t.Fatalf("Doer workspace normalization: %v", err)
			}
		}
	}
	return commands.Preview(context.Background(), subject, "browser-session", console.CommandPreviewRequest{
		SchemaVersion: console.CommandCatalogVersion, CommandID: loopDoerWorkspaceCommandID,
		TargetID: form.Revision.LoopID, ExpectedDigest: emptyLoopHeadDigest(form.Revision.LoopID),
		IdempotencyKey: key, Input: input,
	})
}

func TestDoerWorkspacePublicationWithoutRuntimeSessionAndExactReplay(t *testing.T) {
	svc, subject, commands, agent := doerWorkspaceCommandFixture(t)
	if _, err := svc.FleetCommandAuthorityAs(context.Background(), subject); !errors.Is(err, app.ErrDenied) {
		t.Fatalf("fixture unexpectedly has runtime authority: %v", err)
	}
	before, err := svc.FleetRepository.ListLoopRevisions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	preview, err := previewDoerWorkspace(t, svc, commands, subject, agent, "publish-one")
	if err != nil {
		t.Fatal(err)
	}
	after, err := svc.FleetRepository.ListLoopRevisions(context.Background())
	if err != nil || len(after) != len(before) {
		t.Fatalf("preview mutated Loop: before=%d after=%d err=%v", len(before), len(after), err)
	}
	request := console.CommandExecuteRequest{SchemaVersion: console.CommandCatalogVersion, IntentID: preview.IntentID}
	receipt, err := commands.Execute(context.Background(), subject, "browser-session", request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CommandID != loopDoerWorkspaceCommandID || receipt.ReasonCode != "loop_revision_published" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	var readback struct {
		Published app.PublishedLoop `json:"published"`
		View      app.LoopView      `json:"view"`
	}
	if err := json.Unmarshal(receipt.Readback, &readback); err != nil {
		t.Fatal(err)
	}
	if readback.Published.Revision.Digest == "" || readback.View.Revision.Digest != readback.Published.Revision.Digest || readback.View.Provenance.AuthorityKind != "registered-agent-workspace" {
		t.Fatalf("missing workspace provenance/readback: %+v", readback)
	}
	replay, err := commands.Execute(context.Background(), subject, "browser-session", request)
	if err != nil || string(replay.Readback) != string(receipt.Readback) {
		t.Fatalf("exact confirmation replay: err=%v receipt=%+v", err, replay)
	}
	retained, err := previewDoerWorkspace(t, svc, commands, subject, agent, "publish-one")
	if err != nil || retained.IntentID != preview.IntentID {
		t.Fatalf("same-key preview lost exact retained intent: %+v, %v", retained, err)
	}
	views, err := svc.FleetRepository.ListLoopRevisions(context.Background())
	if err != nil || len(views) != len(before)+1 {
		t.Fatalf("expected one immutable revision: count=%d err=%v", len(views), err)
	}
}

func TestDoerWorkspacePublicationDeniesForeignDisabledAndStale(t *testing.T) {
	svc, subject, commands, agent := doerWorkspaceCommandFixture(t)
	foreign := subject
	foreign.PrincipalID = "foreign-principal"
	preview, err := previewDoerWorkspace(t, svc, commands, foreign, agent, "foreign-key")
	if err == nil || preview.IntentID != "" {
		t.Fatal("foreign principal received admitted publication preview")
	}
	preview, err = previewDoerWorkspace(t, svc, commands, subject, agent, "disabled-key")
	if err != nil {
		t.Fatal(err)
	}
	input, err := app.NewSetAgentLifecycleInput(agent.Revision.AgentID, agent.Revision.Revision, agent.Revision.Digest, "disabled")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetAgentLifecycleAs(context.Background(), subject, agent.Revision.AgentID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := commands.Execute(context.Background(), subject, "browser-session", console.CommandExecuteRequest{SchemaVersion: console.CommandCatalogVersion, IntentID: preview.IntentID}); err == nil {
		t.Fatal("disabled/stale Agent committed preview")
	}
	if fresh, err := previewDoerWorkspace(t, svc, commands, subject, agent, "disabled-after-key"); err == nil || fresh.IntentID != "" {
		t.Fatal("disabled Agent received admitted publication preview")
	}
	if _, err := svc.RegisteredAgentWorkspaceAs(context.Background(), subject, agent.Revision.AgentID); err == nil {
		t.Fatal("disabled Agent passed handler workspace admission")
	}
	views, err := svc.FleetRepository.ListLoopRevisions(context.Background())
	if err != nil || len(views) != 0 {
		t.Fatalf("denials mutated Loop revisions: count=%d err=%v", len(views), err)
	}
}
