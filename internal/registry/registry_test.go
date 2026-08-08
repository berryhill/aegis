package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	testCharterDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testPolicyDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestCurrentFleetFixtureRegistersImmutableAgentAndReadsBackProvenance(t *testing.T) {
	ctx := context.Background()
	source, err := NewCurrentFleetFixtureSource([]byte(testFleetFixture()))
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := source.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Source.SourceID != "fleet-agent-1" || candidates[1].Source.SourceID != "fleet-agent-2" {
		t.Fatalf("discovery was not deterministic: %+v", candidates)
	}
	// A caller cannot mutate the fixture adapter's retained values.
	candidates[0].CapabilityDeclarations[0] = "mutated"
	rediscovered, err := source.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rediscovered[0].CapabilityDeclarations[0] != "a2a.request" {
		t.Fatalf("discovery returned aliased capability state: %+v", rediscovered[0])
	}

	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	identity := FleetSource{FleetID: "fleet-primary", Kind: CurrentFleetSourceKind, SourceID: "fleet-agent-1"}
	registration, revision, created, err := service.RegisterFromSource(ctx, source, identity)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first exact registration was not created")
	}
	if registration.AgentID != "agent-alpha" || registration.Source != identity || registration.InitialRevision.Digest != revision.Digest {
		t.Fatalf("registration lost immutable identity: registration=%+v revision=%+v", registration, revision)
	}
	if revision.Runtime != (RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/alpha"}) ||
		revision.Ownership != (Ownership{OwnerID: "operator-primary", AccountabilityID: "team-platform"}) ||
		revision.Lifecycle != LifecycleEnabled || revision.Charter.Revision != 7 || revision.Charter.Digest != testCharterDigest {
		t.Fatalf("revision lost fleet provenance: %+v", revision)
	}
	if len(revision.CapabilityDeclarations) != 2 || revision.CapabilityDeclarations[0] != "a2a.request" ||
		len(revision.PolicyRefs) != 1 || revision.PolicyRefs[0].Digest != testPolicyDigest {
		t.Fatalf("revision lost capabilities or policies: %+v", revision)
	}
	if err := revision.Validate(); err != nil {
		t.Fatalf("stored revision is not canonically sealed: %v", err)
	}

	byAgent, err := service.GetAgentRegistration(ctx, "agent-alpha")
	if err != nil || byAgent != registration {
		t.Fatalf("agent registration readback mismatch: got %+v, err %v", byAgent, err)
	}
	bySource, err := service.GetAgentRegistrationBySource(ctx, identity)
	if err != nil || bySource != registration {
		t.Fatalf("source registration readback mismatch: got %+v, err %v", bySource, err)
	}
	stored, err := service.GetRevision(ctx, "agent-alpha", 1)
	if err != nil || !revisionsEqual(stored, revision) {
		t.Fatalf("revision readback mismatch: got %+v, err %v", stored, err)
	}

	stored.CapabilityDeclarations[0] = "mutated"
	stored.PolicyRefs[0].Digest = testCharterDigest
	again, err := service.GetRevision(ctx, "agent-alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	if again.CapabilityDeclarations[0] != "a2a.request" || again.PolicyRefs[0].Digest != testPolicyDigest {
		t.Fatalf("repository exposed mutable stored state: %+v", again)
	}

	replayedRegistration, replayedRevision, replayCreated, err := service.RegisterFromSource(ctx, source, identity)
	if err != nil {
		t.Fatal(err)
	}
	if replayCreated || replayedRegistration != registration || !revisionsEqual(replayedRevision, revision) {
		t.Fatalf("exact replay was not idempotent: created=%v registration=%+v revision=%+v", replayCreated, replayedRegistration, replayedRevision)
	}
}

func TestRegistryRejectsIdentityConflictsAndAmbiguousDiscovery(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	original := testCandidate("agent-alpha", "fleet-agent-1")
	identity := original.Source
	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{original}, identity); err != nil {
		t.Fatal(err)
	}

	changed := original
	changed.Runtime.Target = "profile/substituted"
	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{changed}, identity); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay was not rejected as conflict: %v", err)
	}

	reusedSource := testCandidate("agent-beta", "fleet-agent-1")
	reusedSource.Charter.ID = "agent-beta"
	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{reusedSource}, identity); !errors.Is(err, ErrConflict) {
		t.Fatalf("source identity reuse was not rejected: %v", err)
	}

	secondSource := testCandidate("agent-alpha", "fleet-agent-2")
	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{secondSource}, secondSource.Source); !errors.Is(err, ErrConflict) {
		t.Fatalf("agent identity rebinding was not rejected: %v", err)
	}

	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{original, original}, identity); !errors.Is(err, ErrAmbiguousSource) {
		t.Fatalf("ambiguous source discovery was not denied: %v", err)
	}
	if _, _, _, err := service.RegisterFromSource(ctx, staticSource{original}, FleetSource{FleetID: "fleet-primary", Kind: CurrentFleetSourceKind, SourceID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing source identity was not denied: %v", err)
	}
}

func TestImmutableRevisionPublicationRejectsOverwriteSubstitutionAndInvalidSequence(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	candidate := testCandidate("agent-alpha", "fleet-agent-1")
	_, initial, _, err := service.RegisterFromSource(ctx, staticSource{candidate}, candidate.Source)
	if err != nil {
		t.Fatal(err)
	}

	second := initial
	second.Revision = 2
	second.Lifecycle = LifecycleDisabled
	second, err = SealRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, second); !errors.Is(err, ErrConflict) {
		t.Fatalf("exact overwrite was not rejected: %v", err)
	}

	substituted := second
	substituted.Runtime.Target = "profile/substituted"
	if err := service.PublishRevision(ctx, substituted); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("digest substitution did not fail canonical validation first: %v", err)
	}

	gap := second
	gap.Revision = 4
	gap.Lifecycle = LifecycleEnabled
	gap, err = SealRevision(gap)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, gap); !errors.Is(err, ErrConflict) {
		t.Fatalf("revision gap was not rejected: %v", err)
	}

	wrongSource := second
	wrongSource.Revision = 3
	wrongSource.Source.SourceID = "other-source"
	wrongSource, err = SealRevision(wrongSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, wrongSource); !errors.Is(err, ErrConflict) {
		t.Fatalf("source rebinding was not rejected: %v", err)
	}

	initialReadback, err := service.GetRevision(ctx, "agent-alpha", 1)
	if err != nil || !revisionsEqual(initialReadback, initial) {
		t.Fatalf("initial immutable revision changed: got %+v, err %v", initialReadback, err)
	}
	secondReadback, err := service.GetRevision(ctx, "agent-alpha", 2)
	if err != nil || !revisionsEqual(secondReadback, second) {
		t.Fatalf("published revision changed: got %+v, err %v", secondReadback, err)
	}
}

func TestExecutableResolutionFailsClosedForDigestLifecycleAndRetirement(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service, err := NewService(repository)
	if err != nil {
		t.Fatal(err)
	}
	candidate := testCandidate("agent-alpha", "fleet-agent-1")
	_, initial, _, err := service.RegisterFromSource(ctx, staticSource{candidate}, candidate.Source)
	if err != nil {
		t.Fatal(err)
	}
	initialRef := revisionRef(initial)
	resolved, err := service.ResolveExecutable(ctx, initialRef)
	if err != nil || !revisionsEqual(resolved, initial) {
		t.Fatalf("enabled exact revision did not resolve: got %+v, err %v", resolved, err)
	}
	wrongDigest := initialRef
	wrongDigest.Digest = testPolicyDigest
	if _, err := service.ResolveExecutable(ctx, wrongDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("digest substitution was not denied: %v", err)
	}

	disabled := initial
	disabled.Revision = 2
	disabled.Lifecycle = LifecycleDisabled
	disabled, err = SealRevision(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, disabled); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveExecutable(ctx, revisionRef(disabled)); !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("disabled revision was executable: %v", err)
	}

	retired := disabled
	retired.Revision = 3
	retired.Lifecycle = LifecycleRetired
	retired, err = SealRevision(retired)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, retired); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveExecutable(ctx, revisionRef(retired)); !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("retired revision was executable: %v", err)
	}
	afterRetirement := retired
	afterRetirement.Revision = 4
	afterRetirement.Lifecycle = LifecycleEnabled
	afterRetirement, err = SealRevision(afterRetirement)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.PublishRevision(ctx, afterRetirement); !errors.Is(err, ErrRetired) {
		t.Fatalf("revision after retirement was accepted: %v", err)
	}
}

func TestCurrentFleetAndRegistryCodecsRejectMalformedOrAmbiguousInput(t *testing.T) {
	validFixture := testFleetFixture()
	fixtureCases := map[string]string{
		"unknown field":       strings.Replace(validFixture, `"fleet_id":`, `"unknown":true,"fleet_id":`, 1),
		"duplicate key":       strings.Replace(validFixture, `"fleet_id":"fleet-primary"`, `"fleet_id":"fleet-primary","fleet_id":"other"`, 1),
		"duplicate source":    strings.Replace(validFixture, `"source_id":"fleet-agent-2"`, `"source_id":"fleet-agent-1"`, 1),
		"unsorted capability": strings.Replace(validFixture, `"capability_declarations":["a2a.request","graph.publish"]`, `"capability_declarations":["graph.publish","a2a.request"]`, 1),
		"trailing value":      validFixture + `{}`,
	}
	for name, wire := range fixtureCases {
		t.Run("fixture/"+name, func(t *testing.T) {
			if _, err := NewCurrentFleetFixtureSource([]byte(wire)); err == nil {
				t.Fatal("malformed current-fleet fixture was accepted")
			}
		})
	}

	sealed, err := SealRevision(candidateRevision(testCandidate("agent-alpha", "fleet-agent-1"), 1))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalAgentRevision(sealed)
	if err != nil {
		t.Fatal(err)
	}
	revisionCases := map[string][]byte{
		"unknown field":  []byte(strings.Replace(string(wire), `"agent_id":`, `"unknown":true,"agent_id":`, 1)),
		"duplicate key":  []byte(strings.Replace(string(wire), `"revision":1`, `"revision":1,"revision":2`, 1)),
		"digest changed": []byte(strings.Replace(string(wire), sealed.Digest, testPolicyDigest, 1)),
		"trailing value": append(append([]byte(nil), wire...), []byte(` null`)...),
	}
	for name, malformed := range revisionCases {
		t.Run("revision/"+name, func(t *testing.T) {
			if _, err := UnmarshalAgentRevision(malformed); err == nil {
				t.Fatal("malformed agent revision was accepted")
			}
		})
	}

	unsortedPolicies := candidateRevision(testCandidate("agent-alpha", "fleet-agent-1"), 1)
	unsortedPolicies.PolicyRefs = []reference.DigestRef{
		{SchemaVersion: reference.DigestRefSchemaVersion, ID: "policy/z", Digest: testPolicyDigest},
		{SchemaVersion: reference.DigestRefSchemaVersion, ID: "policy/a", Digest: testCharterDigest},
	}
	if _, err := SealRevision(unsortedPolicies); err == nil {
		t.Fatal("non-canonical policy reference ordering was accepted")
	}
	duplicatePolicyIdentity := unsortedPolicies
	duplicatePolicyIdentity.PolicyRefs = []reference.DigestRef{
		{SchemaVersion: reference.DigestRefSchemaVersion, ID: "policy/a", Digest: testCharterDigest},
		{SchemaVersion: reference.DigestRefSchemaVersion, ID: "policy/a", Digest: testPolicyDigest},
	}
	if _, err := SealRevision(duplicatePolicyIdentity); err == nil {
		t.Fatal("one policy identity bound to multiple digests was accepted")
	}
}

func testFleetFixture() string {
	return `{"schema_version":"aegis.current-fleet.fixture.v1","fleet_id":"fleet-primary","agents":[` +
		`{"source_id":"fleet-agent-2","agent_id":"agent-beta","runtime":{"adapter":"hermes","runtime":"hermes-agent","target":"profile/beta"},"ownership":{"owner_id":"operator-primary","accountability_id":"team-platform"},"lifecycle":"disabled","charter":{"schema_version":"aegis.reference.revision.v1","id":"agent-beta","revision":3,"digest":"` + testCharterDigest + `"},"capability_declarations":[],"policy_refs":[]},` +
		`{"source_id":"fleet-agent-1","agent_id":"agent-alpha","runtime":{"adapter":"hermes","runtime":"hermes-agent","target":"profile/alpha"},"ownership":{"owner_id":"operator-primary","accountability_id":"team-platform"},"lifecycle":"enabled","charter":{"schema_version":"aegis.reference.revision.v1","id":"agent-alpha","revision":7,"digest":"` + testCharterDigest + `"},"capability_declarations":["a2a.request","graph.publish"],"policy_refs":[{"schema_version":"aegis.reference.digest.v1","id":"policy/agent-alpha","digest":"` + testPolicyDigest + `"}]}` +
		`]}`
}

type staticSource []Candidate

func (source staticSource) Discover(ctx context.Context) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]Candidate(nil), source...), nil
}

func testCandidate(agentID, sourceID string) Candidate {
	return Candidate{
		AgentID:   agentID,
		Source:    FleetSource{FleetID: "fleet-primary", Kind: CurrentFleetSourceKind, SourceID: sourceID},
		Runtime:   RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/alpha"},
		Ownership: Ownership{OwnerID: "operator-primary", AccountabilityID: "team-platform"},
		Lifecycle: LifecycleEnabled,
		Charter: reference.RevisionRef{
			SchemaVersion: reference.RevisionRefSchemaVersion,
			ID:            agentID,
			Revision:      7,
			Digest:        testCharterDigest,
		},
		CapabilityDeclarations: []string{"a2a.request", "graph.publish"},
		PolicyRefs: []reference.DigestRef{{
			SchemaVersion: reference.DigestRefSchemaVersion,
			ID:            "policy/" + agentID,
			Digest:        testPolicyDigest,
		}},
	}
}

func candidateRevision(candidate Candidate, number uint64) AgentRevision {
	return AgentRevision{
		SchemaVersion:          AgentRevisionSchemaVersion,
		AgentID:                candidate.AgentID,
		Revision:               number,
		Source:                 candidate.Source,
		Runtime:                candidate.Runtime,
		Ownership:              candidate.Ownership,
		Lifecycle:              candidate.Lifecycle,
		Charter:                candidate.Charter,
		CapabilityDeclarations: append([]string(nil), candidate.CapabilityDeclarations...),
		PolicyRefs:             append([]reference.DigestRef(nil), candidate.PolicyRefs...),
	}
}

func revisionRef(revision AgentRevision) reference.RevisionRef {
	return reference.RevisionRef{
		SchemaVersion: reference.RevisionRefSchemaVersion,
		ID:            revision.AgentID,
		Revision:      revision.Revision,
		Digest:        revision.Digest,
	}
}
