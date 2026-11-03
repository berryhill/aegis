package api

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func agentObservationCharter(t *testing.T, svc *app.Service, subject core.Subject, id string) core.CanonicalCharter {
	t.Helper()
	b, err := os.ReadFile("../../examples/office-charter.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := core.DecodeCharter(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	c.AgentID = id
	c.Name = id
	c.CreatedBy = subject.PrincipalID
	c.Stanzas[0].Authentication.Methods = []string{"local-os"}
	c.Stanzas[0].Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{subject.ID}, PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{subject.Issuer}, Environments: []string{"local"}}}
	cc, err := core.Canonicalize(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	return cc
}

func provisionAgentObservation(t *testing.T, svc *app.Service, subject core.Subject, cc core.CanonicalCharter) core.Receipt {
	t.Helper()
	ctx := context.Background()
	review, err := svc.PreviewPlanAs(ctx, subject, cc.Charter.AgentID, cc.Charter.Revision, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := svc.RequestApprovalAs(ctx, subject, review.Plan.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	approval, err = svc.DecideApprovalAs(ctx, subject, approval.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := svc.ApplyAs(ctx, subject, review.Plan.ID, approval.ID)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestAgentObservedReadinessRequiresCurrentApprovedArtifacts(t *testing.T) {
	scheduleInstalledAPIJourneyGroup(t)
	for _, name := range []string{"missing", "receipt-only", "current", "tampered", "removed", "artifact-escape", "root-symlink", "agent-symlink", "revision-symlink", "artifact-symlink", "runtime-unavailable", "runtime-incompatible", "wrong-plan", "wrong-approval"} {
		t.Run(name, func(t *testing.T) {
			scheduleInstalledAPIJourney(t)
			svc := apiService(t)
			configureAPIFleet(t, svc)
			ctx := context.Background()
			subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
			if err != nil {
				t.Fatal(err)
			}
			cc := agentObservationCharter(t, svc, subject, "observed")
			revision := registry.AgentRevision{AgentID: "observed", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64), Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{ID: cc.Charter.AgentID, Revision: cc.Charter.Revision, Digest: cc.Digest}, Runtime: registry.RuntimeBinding{Adapter: cc.Charter.Runtime.Adapter, Runtime: cc.Charter.Runtime.Runtime, Target: cc.Charter.Runtime.Target}}
			want := "unverified"
			switch name {
			case "missing":
				want = "missing"
			case "receipt-only":
				err = svc.Store.Save("receipts", "receipt-only", core.Receipt{ID: "receipt-only", CharterDigest: cc.Digest, Status: "verified"})
			default:
				receipt := provisionAgentObservation(t, svc, subject, cc)
				switch name {
				case "root-symlink", "agent-symlink", "revision-symlink", "artifact-symlink":
					base := svc.Store.Root() + "/provisioned"
					if name != "root-symlink" {
						base += "/observed"
					}
					if name == "revision-symlink" || name == "artifact-symlink" {
						base += "/1"
					}
					if name == "artifact-symlink" {
						base += "/hermes-config.json"
					}
					if err = os.Rename(base, base+"-relocated"); err == nil {
						err = os.Symlink(base+"-relocated", base)
					}
				case "current":
					want = "verified-current"
				case "tampered":
					err = os.WriteFile(receipt.Artifacts[0].Path, []byte(`{"agent_id":"not-observed"}`), 0600)
				case "removed":
					err = os.Remove(receipt.Artifacts[0].Path)
				case "artifact-escape":
					receipt.Artifacts[0].Path = "/etc/passwd"
					err = svc.Store.Save("receipts", receipt.ID, receipt)
				case "runtime-unavailable":
					svc.Hermes = nil
					want = "verified-current"
				case "runtime-incompatible":
					err = os.WriteFile(svc.Config.HermesExecutable, []byte("#!/bin/sh\necho 'Hermes Agent v0.1.0'\n"), 0700)
					want = "verified-current"
				case "wrong-plan":
					receipt.PlanDigest = "sha256:" + strings.Repeat("b", 64)
					err = svc.Store.Save("receipts", receipt.ID, receipt)
				case "wrong-approval":
					receipt.ApprovalID = "unknown"
					err = svc.Store.Save("receipts", receipt.ID, receipt)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			before := agentReadOnlyState(t, svc)
			observed, err := svc.ObserveAgentPreconditionsAs(ctx, subject, revision)
			if err != nil {
				t.Fatal(err)
			}
			if observed.Provisioning != want {
				t.Fatalf("got %+v want provisioning %s", observed, want)
			}
			record, err := consoleAgentDetail(ctx, svc, subject, app.FleetAgent{Revision: revision}, app.FleetAgent{Revision: revision}, app.FleetSurface{})
			if err != nil {
				t.Fatal(err)
			}
			if record.Agent.RuntimeVersionConstraint != cc.Charter.Runtime.VersionConstraint {
				t.Fatal("lost exact verified runtime constraint")
			}
			ready := strings.Contains(record.Readiness, "Observed preconditions satisfied")
			if ready != (name == "current") {
				t.Fatalf("false or missing readiness: %s", record.Readiness)
			}
			if !strings.Contains(record.Readiness, "Not execution admission") {
				t.Fatal("observation presented as admission")
			}
			if name == "runtime-unavailable" && observed.Runtime != "unavailable" {
				t.Fatal(observed)
			}
			if agentReadOnlyState(t, svc) != before {
				t.Fatal("observation mutated authority or evidence")
			}
		})
	}
}

// All currently accepted selectors are finite equality constraints. Two stanzas
// that can match the same authenticated subject are rejected at canonicalization,
// including distinct claim keys (AND within a selector, OR across selectors).
// A rendered ambiguous fixture is therefore UNAVAILABLE for accepted charters;
// never bypass validation or store an invalid policy to manufacture that screen.
func TestAgentAmbiguityCannotEnterAcceptedCharter(t *testing.T) {
	svc := apiService(t)
	subject, err := svc.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	cc := agentObservationCharter(t, svc, subject, "ambiguous-check")
	for _, claims := range []map[string]string{{"department": "ops"}, {"region": "west"}} {
		c := cc.Charter
		c.Stanzas = append([]core.TrustStanza(nil), cc.Charter.Stanzas...)
		c.Stanzas[1].Authentication = core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{subject.PrincipalID}, Issuers: []string{subject.Issuer}, Environments: []string{"local"}, Claims: claims}}}
		if _, err := core.Canonicalize(c); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("overlapping authenticated claim accepted: %v", err)
		}
	}
}
