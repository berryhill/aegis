package registry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCanonicalBuiltInAegisAgentIsDeterministicAndNotAProfile(t *testing.T) {
	firstRegistration, firstRevision, err := CanonicalBuiltInAegisAgent("principal-1")
	if err != nil {
		t.Fatal(err)
	}
	secondRegistration, secondRevision, err := CanonicalBuiltInAegisAgent("principal-1")
	if err != nil {
		t.Fatal(err)
	}
	if !registrationsEqual(firstRegistration, secondRegistration) || !revisionsEqual(firstRevision, secondRevision) {
		t.Fatal("canonical built-in Aegis Agent changed between constructions")
	}
	if firstRegistration.AgentID != BuiltInAegisAgentID || firstRevision.AgentID != BuiltInAegisAgentID {
		t.Fatalf("agent id is not canonical: registration=%+v revision=%+v", firstRegistration, firstRevision)
	}
	if firstRevision.Source.Kind != BuiltInSystemSourceKind || firstRevision.Source != firstRegistration.Source {
		t.Fatalf("built-in source provenance missing: %+v", firstRevision.Source)
	}
	if firstRevision.Runtime.Adapter != "hermes" || firstRevision.Runtime.Runtime != "hermes-agent" || firstRevision.Runtime.Target != BuiltInAegisRuntimeTarget || BuiltInAegisRuntimeTarget != "manager-disposable" {
		t.Fatalf("runtime does not preserve exact disposable manager target: %+v", firstRevision.Runtime)
	}
	if firstRevision.Ownership.OwnerID != BuiltInAegisOwnerID || firstRevision.Ownership.AccountabilityID != "principal-1" {
		t.Fatalf("built-in ownership/accountability binding is wrong: %+v", firstRevision.Ownership)
	}
	if firstRevision.Charter.ID != BuiltInAegisAgentID || firstRevision.Charter.Revision != 1 || firstRevision.Charter.Digest == "" {
		t.Fatalf("sealed system representation missing: %+v", firstRevision.Charter)
	}
	if err = firstRegistration.Validate(); err != nil {
		t.Fatalf("registration invalid: %v", err)
	}
	if err = firstRevision.Validate(); err != nil {
		t.Fatalf("revision invalid: %v", err)
	}
}

func TestCanonicalBuiltInAegisAgentBindsDigestToAccountablePrincipal(t *testing.T) {
	firstRegistration, firstRevision, err := CanonicalBuiltInAegisAgent("principal-1")
	if err != nil {
		t.Fatal(err)
	}
	secondRegistration, secondRevision, err := CanonicalBuiltInAegisAgent("principal-2")
	if err != nil {
		t.Fatal(err)
	}
	if firstRevision.Ownership.OwnerID != secondRevision.Ownership.OwnerID || firstRevision.Ownership.OwnerID != BuiltInAegisOwnerID {
		t.Fatalf("product ownership changed with principal: first=%+v second=%+v", firstRevision.Ownership, secondRevision.Ownership)
	}
	if firstRevision.Digest == secondRevision.Digest || firstRegistration.InitialRevision.Digest == secondRegistration.InitialRevision.Digest {
		t.Fatal("accountable principal was not bound into the canonical revision and registration")
	}
}

func TestBuiltInAegisAgentPrincipalCollisionIsDenied(t *testing.T) {
	repository := NewMemoryRepository()
	firstRegistration, firstRevision, err := CanonicalBuiltInAegisAgent("principal-1")
	if err != nil {
		t.Fatal(err)
	}
	if created, registerErr := repository.Register(context.Background(), firstRegistration, firstRevision); registerErr != nil || !created {
		t.Fatalf("register first principal: created=%t err=%v", created, registerErr)
	}
	secondRegistration, secondRevision, err := CanonicalBuiltInAegisAgent("principal-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.Register(context.Background(), secondRegistration, secondRevision); !errors.Is(err, ErrConflict) {
		t.Fatalf("different accountable principal replaced canonical built-in identity: %v", err)
	}
	stored, err := repository.GetRevision(context.Background(), BuiltInAegisAgentID, 1)
	if err != nil || !revisionsEqual(stored, firstRevision) {
		t.Fatalf("collision changed first canonical revision: stored=%+v err=%v", stored, err)
	}
}

func TestBuiltInAegisAgentRejectsGenericRegistrationAndAllLaterRevisions(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}

	generic := testCandidate(BuiltInAegisAgentID, "generic-aegis")
	if _, _, _, err = service.RegisterFromSource(ctx, staticSource{generic}, generic.Source); !errors.Is(err, ErrBuiltInImmutable) {
		t.Fatalf("generic service registration occupied reserved Agent ID: %v", err)
	}
	genericRevision, err := SealRevision(candidateRevision(generic, 1))
	if err != nil {
		t.Fatal(err)
	}
	genericRegistration := AgentRegistration{
		SchemaVersion:   AgentRegistrationSchemaVersion,
		AgentID:         genericRevision.AgentID,
		Source:          genericRevision.Source,
		InitialRevision: revisionRef(genericRevision),
	}
	if _, err = repository.Register(ctx, genericRegistration, genericRevision); !errors.Is(err, ErrBuiltInImmutable) {
		t.Fatalf("generic repository registration occupied reserved Agent ID: %v", err)
	}

	canonicalRegistration, canonicalRevision, err := CanonicalBuiltInAegisAgent("principal-1")
	if err != nil {
		t.Fatal(err)
	}
	if created, registerErr := repository.Register(ctx, canonicalRegistration, canonicalRevision); registerErr != nil || !created {
		t.Fatalf("canonical built-in registration failed: created=%t err=%v", created, registerErr)
	}
	next := canonicalRevision
	next.Revision = 2
	next.Lifecycle = LifecycleDisabled
	next, err = SealRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.PublishRevision(ctx, next); !errors.Is(err, ErrBuiltInImmutable) {
		t.Fatalf("service published built-in revision 2: %v", err)
	}
	if err = repository.PublishRevision(ctx, next); !errors.Is(err, ErrBuiltInImmutable) {
		t.Fatalf("memory repository published built-in revision 2: %v", err)
	}
	latest, err := repository.LatestRevision(ctx, BuiltInAegisAgentID)
	if err != nil || latest.Revision != 1 || latest.Digest != canonicalRevision.Digest {
		t.Fatalf("built-in changed after denied publications: latest=%+v err=%v", latest, err)
	}
}

func TestCurrentFleetFixtureCannotClaimBuiltInAegisAgentID(t *testing.T) {
	wire := strings.Replace(testFleetFixture(), `"agent_id":"agent-alpha"`, `"agent_id":"aegis"`, 1)
	if _, err := NewCurrentFleetFixtureSource([]byte(wire)); !errors.Is(err, ErrBuiltInImmutable) {
		t.Fatalf("current-fleet fixture claimed reserved Agent ID: %v", err)
	}
}
