package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"strings"
	"testing"
	"time"
)

func TestExactAgentAuthoritySelection(t *testing.T) {
	svc := testService(t)
	subject, err := svc.Authenticate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	exact := registry.AgentRevision{AgentID: "office", Revision: 1, Digest: digest, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "office", Revision: 1, Digest: digest}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "local"}}
	svc.FleetRepository = fleetCommandRepository{agent: exact}
	svc.Fleet = &orchestration.FleetService{}
	svc.QueueWorker = &orchestration.QueueWorker{}
	if _, err = svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); !errors.Is(err, ErrDenied) {
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
	digest = "sha256:" + strings.Repeat("b", 64)
	create("wrong-charter")
	if _, err = svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); !errors.Is(err, ErrDenied) {
		t.Fatal("wrong charter admitted")
	}
	digest = exact.Charter.Digest
	first := create("one")
	if binding, err := svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); err != nil || binding.Authority.ID != first.ID || binding.Publisher.ID != "office" {
		t.Fatalf("exactly one current context was not selected: binding=%+v err=%v", binding, err)
	}
	second := create("two")
	if _, err = svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); !errors.Is(err, ErrDenied) {
		t.Fatalf("multiple current contexts were not denied: %v", err)
	}
	if err = svc.processAuthorityCommand(context.Background(), core.AuthorityCommandRevoke, second, "test revocation"); err != nil {
		t.Fatal(err)
	}
	if binding, err := svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); err != nil || binding.Authority.ID != first.ID {
		t.Fatalf("revoked context was not excluded: binding=%+v err=%v", binding, err)
	}
	svc.Now = func() time.Time { return first.ExpiresAt }
	if _, err = svc.fleetCommandAuthorityForAgent(context.Background(), subject, &exact); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired contexts were not denied: %v", err)
	}
}
