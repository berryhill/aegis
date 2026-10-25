package managergateway

import (
	"context"
	"crypto/sha256"
	"github.com/berryhill/aegis/internal/app"
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
		selected := gateway.sessions[entry.id]
		if selected.contributionKey != c.Key || selected.contributionLoop != c.Loop {
			t.Fatal("did not restore exact selection")
		}
	}
	projection, err := repo.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || projection.Attempts != 0 {
		t.Fatal("observation launched runtime")
	}
}
