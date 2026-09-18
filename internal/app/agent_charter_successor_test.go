package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAgentCharterSuccessorExactApprovalAndBootstrapLineage(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	s.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	repo, err := fleetbadger.Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	configureLocalImportFleet(t, s, repo)
	subject := localBootstrapImportPrincipal(s)
	proposal, err := s.PrepareLocalHermesAgentImportForBootstrapAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	initial, _, err := s.ConfirmLocalHermesAgentImportForBootstrapAs(ctx, subject, proposal.RevisionDigest)
	if err != nil {
		t.Fatal(err)
	}
	charter, err := s.Store.GetCharter(initial.Revision.AgentID, 1)
	if err != nil {
		t.Fatal(err)
	}
	c := charter.Charter
	c.Revision = 2
	c.Stanzas[0].Hermes.Model = "test-model"
	c.Stanzas[0].Hermes.Provider = "none"
	successor, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(successor); err != nil {
		t.Fatal(err)
	}
	input := ApproveAgentCharterInput{Expected: agentRevisionRef(initial.Revision), Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 2, Digest: successor.Digest}}
	for _, name := range []string{"wrong principal", "stale digest", "unrelated charter", "unknown digest", "unauthenticated"} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			who := subject
			switch name {
			case "wrong principal":
				who.PrincipalID = "other"
			case "stale digest":
				candidate.Expected.Digest = charter.Digest
			case "unrelated charter":
				candidate.Charter.ID = "other"
			case "unknown digest":
				candidate.Charter.Digest = charter.Digest
			case "unauthenticated":
				who = core.Subject{}
			}
			if _, err := s.ApproveAgentCharterAs(ctx, who, c.AgentID, candidate); err == nil {
				t.Fatal("accepted invalid approval")
			}
			latest, err := repo.LatestAgentRevision(ctx, c.AgentID)
			if err != nil || latest.Digest != initial.Revision.Digest {
				t.Fatal("denial mutated Agent")
			}
		})
	}
	if _, err := s.ApproveAgentCharterAs(ctx, subject, registry.BuiltInAegisAgentID, input); err == nil {
		t.Fatal("approved built-in successor")
	}
	retired := initial.Revision
	retired.Lifecycle = registry.LifecycleRetired
	candidate := retired
	candidate.Revision++
	candidate.Charter = input.Charter
	candidate.CharterSuccessor = &registry.CharterSuccessor{Previous: agentRevisionRef(retired), ApprovedBy: subject.PrincipalID}
	if s.validAgentCharterSuccessor(retired, candidate) {
		t.Fatal("accepted retired successor")
	}
	candidate = initial.Revision
	candidate.Revision++
	candidate.Charter = input.Charter
	candidate.CharterSuccessor = &registry.CharterSuccessor{Previous: input.Expected, ApprovedBy: "wrong-owner"}
	if s.validAgentCharterSuccessor(initial.Revision, candidate) {
		t.Fatal("accepted wrong-owner approval")
	}
	next, err := s.ApproveAgentCharterAs(ctx, subject, c.AgentID, input)
	if err != nil {
		t.Fatal(err)
	}
	if next.Registration != initial.Registration || next.Revision.Revision != 2 || next.Revision.Charter != input.Charter {
		t.Fatal("successor rewrote provenance")
	}
	if _, err = s.ApproveAgentCharterAs(ctx, subject, c.AgentID, input); err == nil {
		t.Fatal("accepted stale replay")
	}
	if _, err = s.VerifyLocalHermesAgentImportForBootstrapAs(ctx, subject); err != nil {
		t.Fatal(err)
	}
	old, err := repo.GetAgentRevision(ctx, c.AgentID, 1)
	if err != nil || old.Digest != initial.Revision.Digest {
		t.Fatal("rewrote revision 1")
	}
	// A lifecycle-only revision cannot remove digest-bound approval metadata.
	tampered := next.Revision
	tampered.Revision++
	tampered.Lifecycle = registry.LifecycleDisabled
	tampered.CharterSuccessor = nil
	tampered, err = registry.SealRevision(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.PublishAgentRevision(ctx, tampered, fleet.AuditFact{Event: core.AuditEvent{Type: "test.metadata", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: c.AgentID, Outcome: "ok", Reason: "test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifyLocalHermesAgentImportForBootstrapAs(ctx, subject); err == nil {
		t.Fatal("accepted lifecycle metadata removal")
	}
	// An append-only attacker revision cannot silently alter import ownership.
	forged := next.Revision
	forged.Revision += 2
	forged.Ownership.AccountabilityID = "other"
	forged, err = registry.SealRevision(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.PublishAgentRevision(ctx, forged, fleet.AuditFact{Event: core.AuditEvent{Type: "test.forged", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: c.AgentID, Outcome: "ok", Reason: "test"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifyLocalHermesAgentImportForBootstrapAs(ctx, subject); err == nil {
		t.Fatal("accepted forged import lineage")
	}
}
