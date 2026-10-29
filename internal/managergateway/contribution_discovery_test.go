package managergateway

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/slash"
	"testing"
)

func TestContributionManagerFreshOwnerDiscovery(t *testing.T) {
	ctx := context.Background()
	a, repo, owner, ref, reader := contributionFixture(t)
	a.ContributionReader = reader
	c, err := a.AdmitContributionAs(ctx, owner, "original-session", ref, reader.source.URL.URL, "fresh-discovery", "local-only")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.RecordContributionContinuityAs(ctx, owner, "original-session", c.Key, app.ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"}); err != nil {
		t.Fatal(err)
	}
	gateway, err := New(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	entry := session{id: "fresh-owner", token: sha256.Sum256([]byte("token")), subject: owner, expires: owner.ExpiresAt}
	gateway.sessions[entry.id] = entry
	registry, err := slash.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"/contribute status", "/contribute status " + c.Source.URL.URL, "/contribute status " + c.Key} {
		request, err := registry.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		result, err := gateway.contribute(ctx, entry, request)
		if err != nil {
			t.Fatal(err)
		}
		if result.State != "observed" {
			t.Fatalf("unexpected %+v", result)
		}
		sealed, ok := result.Data["contract"].(app.ContributionNavigationContract)
		if !ok || sealed.Digest != c.Digest || sealed.Acceptance != c.Acceptance || sealed.Source.ContentDigest != c.Source.ContentDigest || sealed.CandidateBudget != c.CandidateBudget || sealed.Deadline != c.Deadline {
			t.Fatal("status lost sealed invocation contract")
		}
		if result.Data["control_sequence"] != uint64(0) || result.Data["effect"] != c.Effect {
			t.Fatalf("status lost current owner restrictions: %+v", result.Data)
		}
		selected := gateway.sessions[entry.id]
		if selected.contributionKey != c.Key || selected.contributionLoop != c.Loop {
			t.Fatal("did not restore exact selection")
		}
	}
	if err = a.ControlContributionAs(ctx, owner, "later-owner-session", c.Key, false, "local-only"); err != nil {
		t.Fatal(err)
	}
	status, err := gateway.Execute(ctx, owner, entry.id, "token", "/contribute status")
	if err != nil {
		t.Fatal(err)
	}
	control, ok := status.Data["control"].(contribution.Control)
	if !ok || control.Sequence != 1 || control.SessionID != "later-owner-session" || control.ContractDigest != c.Digest || control.OwnerID != owner.PrincipalID {
		t.Fatal("status lost current authenticated owner provenance")
	}
	stale := entry
	stale.subject.ExpiresAt = a.Now().Add(-time.Second)
	limited := slash.Result{Data: map[string]any{"contract_digest": c.Digest}}
	if err = gateway.contributionContractStatus(ctx, stale, c, &limited); err != nil || len(limited.Data) != 1 {
		t.Fatal("identity-only navigation disclosed protected contract")
	}
	projection, err := repo.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || projection.Attempts != 0 {
		t.Fatal("observation launched runtime")
	}
}
