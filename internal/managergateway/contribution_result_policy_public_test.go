package managergateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
)

func TestContributionManagerJoinedPublicResultHistoricalPolicy(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}

// The final disposition is produced by the existing successful native+delivery
// fixture. This read-only repository projection substitutes only the report and
// its matching receipt, so denial must come from public historical joins rather
// than a trivially stale report hash. It never writes a finalization or PASS.
type substitutedPolicyResult struct {
	fleet.Repository
	artifact evidence.RuntimeArtifact
	receipt  evidence.VerificationReceipt
}

func (r substitutedPolicyResult) GetRuntimeArtifact(context.Context, string) (evidence.RuntimeArtifact, error) {
	return r.artifact, nil
}
func (r substitutedPolicyResult) GetVerificationReceipt(context.Context, string) (evidence.VerificationReceipt, error) {
	return r.receipt, nil
}

func publicHistoricalPolicyReadback(t *testing.T, s *app.Service, sub core.Subject, c contribution.Contract) {
	t.Helper()
	ctx := context.Background()
	original, err := s.GetContributionResultAs(ctx, sub, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.Store.GetBlob(original.Report.Policy)
	if err != nil {
		t.Fatal(err)
	}
	var policy app.ContributionDeliveryPolicy
	if err = json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	repo := s.FleetRepository
	for _, mode := range []string{"exact", "missing", "corrupt", "no-reference", "owner", "actor", "fence", "role", "reference", "branch", "expected-old", "policy"} {
		t.Run(mode, func(t *testing.T) {
			report := original.Report
			p := policy
			switch mode {
			case "owner":
				p.Binding.OwnerID = "foreign"
			case "actor":
				p.Actor = "foreign"
			case "fence":
				p.Binding.FenceEpoch++
			case "role":
				p.Binding.RoleBinding = contribution.Digest([]byte("foreign"))
			case "reference":
				p.Binding.Reference.Candidate = contribution.Digest([]byte("foreign"))
			case "branch":
				p.Branch = "contribution/foreign"
			case "expected-old":
				p.ExpectedOld = strings.Repeat("f", 40)
			case "policy":
				p.RequiredChecks = append(p.RequiredChecks, "foreign-check")
			}
			b, _ := json.Marshal(p)
			report.Policy, err = s.Store.PutBlob(b)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "no-reference" {
				report.Policy = ""
			}
			if mode == "missing" || mode == "corrupt" {
				path := filepath.Join(s.Store.Root(), "blobs", "sha256", strings.TrimPrefix(report.Policy, "sha256:"))
				saved, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				defer os.WriteFile(path, saved, 0600)
				if mode == "missing" {
					err = os.Remove(path)
				} else {
					err = os.WriteFile(path, []byte("corrupt"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			b, _ = json.Marshal(report)
			ref, e := s.Store.PutBlob(b)
			if e != nil {
				t.Fatal(e)
			}
			a := original.Artifact
			a.ContentRef = ref
			a.Digest = ref
			receipt := original.Receipts[0]
			receipt.ExpectedDigest = ref
			receipt.ObservedDigest = ref
			// Receipt canonical encoding excludes its self-reference in the established writer.
			receipt.EvidenceRef = ""
			b, e = json.Marshal(receipt)
			if e != nil {
				t.Fatal(e)
			}
			receipt.EvidenceRef, e = s.Store.PutBlob(b)
			if e != nil {
				t.Fatal(e)
			}
			s.FleetRepository = substitutedPolicyResult{repo, a, receipt}
			defer func() { s.FleetRepository = repo }()
			if _, e = s.GetContributionResultAs(ctx, sub, c.Key); (e == nil) != (mode == "exact") || app.IsContributionResultNotFound(e) {
				t.Fatalf("public result mode %s: %v", mode, e)
			}
		})
	}
	// Real close/open of canonical Badger, then fresh authentication and readback
	// with contradictory current operational policy. No source/remote replay.
	old := repo.(*fleetbadger.Store)
	root := filepath.Join(s.Config.StateDir, "persistence", "fleet-v1")
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fleetbadger.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.FleetRepository = reopened
	s.Config.ContributionDelivery = &config.ContributionDelivery{Convention: "contribution-publication.v1", Actor: "current-foreign", Author: config.ContributionGitIdentity{Name: "Current", Email: "current@example.invalid"}, Committer: config.ContributionGitIdentity{Name: "Current", Email: "current@example.invalid"}, RequiredChecks: []string{"different"}, MergeMethod: "rebase", ProtectionPolicy: "different"}
	fresh, err := s.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetContributionResultAs(ctx, fresh, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatal("historical result drifted after persistent reopen")
	}
}
