package api

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"

	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestAgentCardEvidenceUsesAuthenticatedEvaluationAndExactCharter(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	cc := agentObservationCharter(t, svc, subject, "card-evidence")
	revision := registry.AgentRevision{AgentID: cc.Charter.AgentID, Revision: 1, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{ID: cc.Charter.AgentID, Revision: cc.Charter.Revision, Digest: cc.Digest}, Runtime: registry.RuntimeBinding{Adapter: cc.Charter.Runtime.Adapter, Runtime: cc.Charter.Runtime.Runtime, Target: cc.Charter.Runtime.Target}}
	makeModel := func() consoleweb.SurfaceModel {
		return consoleweb.SurfaceModel{Records: []consoleweb.RecordModel{consoleAgentRecord(app.FleetAgent{Revision: revision})}}
	}
	model := makeModel()
	before := agentReadOnlyState(t, svc)
	if err := enrichConsoleAgentCards(ctx, svc, subject, &model); err != nil {
		t.Fatal(err)
	}
	d := model.Records[0].Agent
	if d.DeclaredStanzaSummary != "2 of 2 on" || d.EffectiveAuthoritySummary != "principal" || d.ReceiptSummary != "None recorded" {
		t.Fatalf("incorrect authenticated card evidence: %+v", d)
	}
	denied := subject
	denied.ID = "local-uid:unmatched"
	model = makeModel()
	if err := enrichConsoleAgentCards(ctx, svc, denied, &model); err != nil {
		t.Fatal(err)
	}
	if model.Records[0].Agent.EffectiveAuthoritySummary != "None · denied" {
		t.Fatal("declarations selected authority")
	}
	revision.Charter.Digest = "sha256:" + strings.Repeat("a", 64)
	model = makeModel()
	if err := enrichConsoleAgentCards(ctx, svc, subject, &model); err != nil {
		t.Fatal(err)
	}
	if model.Records[0].Agent.DeclaredStanzaSummary != "Unavailable" || model.Records[0].Agent.EffectiveAuthoritySummary != "Unavailable" {
		t.Fatal("wrong exact charter exposed card authority")
	}
	if agentReadOnlyState(t, svc) != before {
		t.Fatal("summary read mutated authority/evidence")
	}
}

func TestAgentSessionSelectionOnlyUsesExactBoundReadback(t *testing.T) {
	detail := &consoleweb.AgentDetailModel{SessionRecords: []consoleweb.AgentSessionModel{{ID: "session-exact", Status: "ended", StanzaID: "principal", MandateID: "mandate-recorded"}}}
	for _, id := range []string{"session-other", "session-exact\n", strings.Repeat("x", 257)} {
		if selectConsoleAgentSession(detail, id) == nil || detail.SelectedSession != nil {
			t.Fatal("unbound session selected")
		}
	}
	if err := selectConsoleAgentSession(detail, "session-exact"); err != nil {
		t.Fatal(err)
	}
	if detail.SelectedSession.ID != "session-exact" || detail.SelectedSession.Status != "ended" {
		t.Fatal("session readback changed")
	}
	if err := selectConsoleAgentSession(nil, "session-exact"); err == nil {
		t.Fatal("missing exact detail accepted session")
	}
}
