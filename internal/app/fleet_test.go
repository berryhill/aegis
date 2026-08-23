package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
)

type fleetCommandRepository struct {
	fleet.Repository
	agent registry.AgentRevision
}

func (r fleetCommandRepository) LatestAgentRevision(context.Context, string) (registry.AgentRevision, error) {
	return r.agent, nil
}

func TestCollectionReadinessPreservesAuthoritativeEmptyAndReady(t *testing.T) {
	tests := []struct {
		name   string
		count  int
		state  string
		reason string
	}{
		{name: "empty", count: 0, state: "empty", reason: "collection_read_succeeded_empty"},
		{name: "ready", count: 3, state: "ready", reason: "collection_read_succeeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectionReadiness(test.count, "fleet.test_records", nil)
			if got.State != test.state || got.ReasonCode != test.reason || got.Source != "fleet.test_records" || got.Count != test.count || !got.Authoritative {
				t.Fatalf("readiness=%+v", got)
			}
		})
	}
}

func TestCollectionReadinessFailureStatesNeverAssertAZeroCount(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		state  string
		reason string
	}{
		{name: "denied", err: ErrDenied, state: "denied", reason: "collection_read_denied"},
		{name: "unavailable", err: fleet.ErrClosed, state: "unavailable", reason: "collection_read_unavailable"},
		{name: "repair required", err: fleet.ErrCorrupt, state: "degraded_repair_required", reason: "fleet_store_corrupt"},
		{name: "error", err: errors.New("unexpected read failure"), state: "error", reason: "collection_read_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectionReadiness(9, "fleet.test_records", test.err)
			if got.State != test.state || got.ReasonCode != test.reason || got.Source != "fleet.test_records" || got.Count != 0 || got.Authoritative {
				t.Fatalf("failure readiness asserted collection facts: %+v", got)
			}
		})
	}
}

func TestFleetCommandAuthorityFailsClosedBeforeAuthorityLookup(t *testing.T) {
	now := time.Now().UTC()
	svc, principal := graphReadService(&graphReadRepository{}, now)
	if _, err := svc.FleetCommandAuthorityAs(context.Background(), principal); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing authority repository was not denied: %v", err)
	}
	wrongPrincipal := core.Subject{ID: principal.ID, PrincipalID: "other-principal", ExpiresAt: now.Add(time.Minute)}
	if _, err := svc.FleetCommandAuthorityAs(context.Background(), wrongPrincipal); !errors.Is(err, ErrDenied) {
		t.Fatalf("substituted principal was not denied: %v", err)
	}
}

func TestFleetCommandAuthorityRequiresExactlyOneCurrentContext(t *testing.T) {
	svc := testService(t)
	subject, err := svc.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	svc.FleetRepository = fleetCommandRepository{agent: registry.AgentRevision{AgentID: "office", Revision: 1, Digest: digest, Lifecycle: registry.LifecycleEnabled}}
	svc.Fleet = &orchestration.FleetService{}
	svc.QueueWorker = &orchestration.QueueWorker{}
	if _, err = svc.FleetCommandAuthorityAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("zero current contexts were not denied: %v", err)
	}

	runtime := core.RuntimeDescriptor{Name: "Hermes Agent", Runtime: "hermes-agent", Version: "0.18.2", Executable: "/usr/bin/hermes", Installation: "system", AdapterVersion: "1"}
	create := func(id string) core.AuthorityContext {
		mandate := core.Mandate{ID: "mandate-" + id, Subject: subject, AgentID: "office", StanzaID: "principal", CharterRevision: 1, CharterDigest: digest, Runtime: runtime, Target: "local", IssuedAt: svc.Now().Add(-time.Minute), ExpiresAt: svc.Now().Add(2 * time.Minute)}
		if err := svc.Authority.CreateMandate(context.Background(), mandate); err != nil {
			t.Fatal(err)
		}
		authority := core.AuthorityContext{ID: "authority-" + id, MandateID: mandate.ID, SessionID: "runtime-session-" + id, SubjectID: subject.ID, AgentID: mandate.AgentID, CharterRevision: mandate.CharterRevision, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID}, IssuedAt: mandate.IssuedAt, ExpiresAt: mandate.ExpiresAt}
		authority.Digest = core.AuthorityContextDigest(authority)
		if err := svc.Authority.CreateAuthorityContext(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
		if err := svc.processAuthorityCommand(context.Background(), core.AuthorityCommandActivate, authority, "test activation"); err != nil {
			t.Fatal(err)
		}
		return authority
	}
	first := create("one")
	if binding, err := svc.FleetCommandAuthorityAs(context.Background(), subject); err != nil || binding.Authority.ID != first.ID || binding.Publisher.ID != "office" {
		t.Fatalf("exactly one current context was not selected: binding=%+v err=%v", binding, err)
	}
	second := create("two")
	if _, err = svc.FleetCommandAuthorityAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("multiple current contexts were not denied: %v", err)
	}
	if err = svc.processAuthorityCommand(context.Background(), core.AuthorityCommandRevoke, second, "test revocation"); err != nil {
		t.Fatal(err)
	}
	if binding, err := svc.FleetCommandAuthorityAs(context.Background(), subject); err != nil || binding.Authority.ID != first.ID {
		t.Fatalf("revoked context was not excluded: binding=%+v err=%v", binding, err)
	}
	svc.Now = func() time.Time { return first.ExpiresAt }
	if _, err = svc.FleetCommandAuthorityAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired contexts were not denied: %v", err)
	}
}
