package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"strings"
	"testing"
)

func TestContributionOwnerDiscoveryNoRuntimeEffects(t *testing.T) {
	ctx := context.Background()
	s, repo, owner, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, owner, "old-session", ref, reader.source.URL.URL, "discover", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"", c.Key, c.Source.URL.URL} {
		got, err := s.ResolveContributionAs(ctx, owner, ref)
		if err != nil || got.Digest != c.Digest {
			t.Fatalf("resolve %q: %v", ref, err)
		}
	}
	reader.source.URL, _ = contribution.ParseIssueURL("https://github.com/owner/repo/issues/2")
	reader.source.IssueID = 10
	other, err := s.admitContributionAs(ctx, owner, "second-session", ref, reader.source.URL.URL, "discover-second", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveContributionAs(ctx, owner, ""); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("ambiguous discovery: %v", err)
	}
	if got, err := s.ResolveContributionAs(ctx, owner, c.Source.URL.URL); err != nil || got.Key != c.Key {
		t.Fatal("exact URL did not disambiguate")
	}
	if err := s.StopContributionInSessionAs(ctx, owner, "second-session", other.Key); err != nil {
		t.Fatal(err)
	}
	foreign := owner
	foreign.PrincipalID = "foreign"
	for _, ref := range []string{"", c.Key, c.Source.URL.URL, "https://github.com/owner/repo/issues/2", c.Source.URL.URL + "?x=1"} {
		if _, err := s.ResolveContributionAs(ctx, foreign, ref); err == nil {
			t.Fatal("foreign discovery")
		}
	}
	if _, err := s.ResolveContributionAs(ctx, owner, c.Source.URL.URL+"?x=1"); err == nil {
		t.Fatal("URL alias")
	}
	item, err := repo.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || item.Attempts != 0 {
		t.Fatalf("read started runtime: %v %v", item, err)
	}
	if err := s.StopContributionInSessionAs(ctx, owner, "fresh-session", c.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveContributionAs(ctx, owner, ""); err == nil {
		t.Fatal("terminal discovery")
	}
}

func TestContributionContinuityProjectionExcludesRuntimeSecrets(t *testing.T) {
	state := contribution.Continuity{Stage: "freeze", Candidate: "lineage", RoleEpochs: map[string]uint64{"builder": 2}, RoleStops: map[string]contribution.RoleStop{"builder": {PID: 98765, ProcessStart: "private-start", BootID: "private-boot", Acknowledged: true}}, Decisions: map[string]string{"request": "expired"}, NativeEffects: map[string]contribution.NativeEffect{"intent": {State: "unknown"}}}
	v := projectContributionContinuity(contribution.Contract{}, state)
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"98765", "private-start", "private-boot", "pid", "process_start", "boot_id", "command", "password", "runtime_home"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if v.CandidateLineage != "lineage" || v.FrozenDigest != "" || v.HeldReason != "native_effect_unresolved" || !v.Roles[1].StopAcknowledged || len(v.Decisions) != 1 {
		t.Fatalf("lost checkpoint: %+v", v)
	}
}
