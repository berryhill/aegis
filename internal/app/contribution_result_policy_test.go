package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"

	"github.com/berryhill/aegis/internal/store"
)

func TestContributionResultReloadsExactHistoricalPolicy(t *testing.T) {
	for _, mode := range []string{"exact", "missing", "corrupt", "noncanonical", "owner", "key", "run", "contract", "candidate", "queue", "loop", "workspace", "base", "attempt", "tree", "source", "candidate-policy", "toolchain", "integrated-exact", "integrated-original", "integrated-candidate-current", "integrated-repair-current", "integrated-missing-repair", "integrated-missing-native"} {
		t.Run(mode, func(t *testing.T) {
			blobs, err := store.Open(filepath.Join(t.TempDir(), "canonical"))
			if err != nil {
				t.Fatal(err)
			}
			s := &Service{Store: blobs}
			p := historyPolicy()
			p.Workspace = "workspace"
			p.BaseHead = "base"
			p.QueueAttempt = "attempt"
			p.Source = contribution.Digest([]byte("source"))
			p.CandidatePolicy = contribution.Digest([]byte("candidate-policy"))
			p.Toolchain = "original-toolchain"
			c := contribution.Contract{OwnerID: p.Binding.OwnerID, Key: p.Binding.Key, RunID: p.Binding.Run, Digest: p.Binding.Contract, QueueItemID: p.Binding.QueueItemID, Loop: p.Loop, Worktree: p.Workspace, Base: p.BaseHead}
			r := evidence.ContributionTerminalReport{Owner: c.OwnerID, Key: c.Key, Run: c.RunID, Contract: c.Digest, Candidate: p.Binding.Candidate, Attempt: p.QueueAttempt}
			candidate := orchestration.ContributionCandidate{Digest: r.Candidate, Contract: c.Digest, Base: p.BaseHead, QueueAttempt: p.QueueAttempt, CandidateTree: p.Tree, Policy: p.CandidatePolicy, Toolchain: p.Toolchain}
			candidate.Source.Digest = p.Source
			var repairs []orchestration.ContributionDeliveryRepair
			if strings.HasPrefix(mode, "integrated-") {
				p.OriginalBase, p.BaseHead = c.Base, "current-base"
				p.RepairFailure, p.ExpectedOld, p.PR = "failure", "owned-head", 7
				candidate.CurrentBase, candidate.Pass = p.BaseHead, 2
				repairs = []orchestration.ContributionDeliveryRepair{{Failure: p.RepairFailure, Candidate: "predecessor", Head: p.ExpectedOld, Base: c.Base, CurrentBase: p.BaseHead, Class: "base_advanced", PR: p.PR, AfterPass: 1}}
				switch mode {
				case "integrated-original":
					p.OriginalBase = "foreign"
				case "integrated-candidate-current":
					candidate.CurrentBase = "foreign"
				case "integrated-repair-current":
					repairs[0].CurrentBase = "foreign"
				case "integrated-missing-repair":
					repairs = nil
				}
			}
			native, _ := json.Marshal(map[string]any{"schema": "aegis.contribution.native-acceptance.v1", "run_id": c.RunID, "contract": c.Digest, "queue_attempt": r.Attempt, "delivery_repairs": repairs, "passes": []any{map[string]any{"candidate": candidate}}})
			if r.Native, err = blobs.PutBlob(native); err != nil {
				t.Fatal(err)
			}
			if mode == "integrated-missing-native" {
				r.Native = ""
			}
			switch mode {
			case "owner":
				p.Binding.OwnerID = "foreign"
			case "key":
				p.Binding.Key = "foreign"
			case "run":
				p.Binding.Run = "foreign"
			case "contract":
				p.Binding.Contract = contribution.Digest([]byte("foreign"))
			case "candidate":
				p.Binding.Candidate = contribution.Digest([]byte("foreign"))
			case "queue":
				p.Binding.QueueItemID = "foreign"
			case "loop":
				p.Loop.Revision++
			case "workspace":
				p.Workspace = "foreign"
			case "base":
				p.BaseHead = "foreign"
			case "attempt":
				p.QueueAttempt = "foreign"
			case "tree":
				p.Tree = "foreign"
			case "source":
				p.Source = contribution.Digest([]byte("foreign"))
			case "candidate-policy":
				p.CandidatePolicy = contribution.Digest([]byte("foreign"))
			case "toolchain":
				p.Toolchain = "foreign"
			}
			wire, _ := json.Marshal(p)
			if mode == "noncanonical" {
				wire = append(wire, '\n')
			}
			r.Policy = contribution.Digest(wire)
			if mode != "missing" {
				if _, err = blobs.PutBlob(wire); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "corrupt" {
				if err = os.WriteFile(filepath.Join(blobs.Root(), "blobs", "sha256", strings.TrimPrefix(r.Policy, "sha256:")), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Store owns no open handles: discard both objects and reopen the actual
			// persistent blobs, as a restarted service does. Contradictory current
			// operational policy must not retcon the immutable historical joins.
			reopened, openErr := store.Open(blobs.Root())
			if openErr != nil {
				t.Fatal(openErr)
			}
			s = &Service{Store: reopened, Config: config.Config{ContributionDelivery: &config.ContributionDelivery{
				Convention: "contribution-publication.v1", Actor: "different-current-actor",
				Author:         config.ContributionGitIdentity{Name: "Current", Email: "current@example.invalid"},
				Committer:      config.ContributionGitIdentity{Name: "Current", Email: "current@example.invalid"},
				RequiredChecks: []string{"different-current-ci"}, RequiredReviewers: []string{"different-current-reviewer"},
				PostMergeChecks: []string{"different-current-post"}, MergeMethod: "rebase", ProtectionPolicy: "different-current-protection",
			}}}
			if err := s.Config.ContributionDelivery.Validate(); err != nil {
				t.Fatal(err)
			}
			err = s.reloadContributionResultEvidence(context.Background(), c, r)
			if (err == nil) != (mode == "exact" || mode == "integrated-exact") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}
