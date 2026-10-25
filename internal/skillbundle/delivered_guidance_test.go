package skillbundle

import (
	"path/filepath"
	"strings"
	"testing"
)

// These are trusted source-text regressions for structural documentation coverage.
// They are NOT behavioral acceptance, managed-provider proof, installed adoption,
// or whole-contribution verification. Manifest refresh/integration belongs to the parent.
func TestDeliveredGuidanceQueueAndEvidence(t *testing.T) {
	for _, path := range []string{
		"skills/aegis-execution-queue/SKILL.md",
		"skills/aegis-evidence-disposition/SKILL.md",
	} {
		t.Run(path, func(t *testing.T) {
			text := string(mustRead(t, filepath.Join(repositoryRoot(t), path)))
			for _, required := range []string{
				"../../.hermes/contribution-happy-path-contract.md",
				"partial #376 managed-candidate", "prepare/build/freeze/test/repair/review",
				"internal/implementation/contribution.go", "internal/orchestration/contribution_executor.go",
				"candidate_accepted", "ErrContributionCandidatePending",
				"#377", "#378", "#379", "Historical profiles",
				"aegis queue preparations", "GET /v1/preparations",
				"queue preparations|list|show|result|bind-runtime|process",
				"delivered by #350", "online_operation_unsupported",
				"low-level controller primitives, not operator",
				"chores for one high-level authenticated Run",
				"New direct Loop snapshots", "historical", "published",
				"nonexecutable", "runtime-stop acknowledgment", "lease_reclaimed",
				"Same-intent recovery preserves immutable failed history and cumulative budgets.",
			} {
				if !strings.Contains(text, required) {
					t.Errorf("missing guidance %q", required)
				}
			}
			for _, obsolete := range []string{
				"Current v6 intake is not the whole autonomous journey",
				"normalized Graph snapshot, Graph run, authority reference",
				"pinned Graph and Loop revisions",
				"Missing prerequisites remain durable non-executable preparation.",
				"all Queue lifecycle operations support --target",
			} {
				if strings.Contains(text, obsolete) {
					t.Errorf("obsolete or contradictory guidance %q", obsolete)
				}
			}
		})
	}
}

func TestDeliveredGuidanceSourceDocs(t *testing.T) {
	cases := []struct {
		path     string
		required []string
		forbid   []string
	}{
		{"docs/VERIFIED_IMPLEMENTATION.md", []string{
			"SubmitLoopAs", "historical synthetic Graph replay", "publish no synthetic Graph",
			"Native Go tests are trusted code, not a sandbox.",
		}, []string{"New single-operation Loop Run intents require a published Graph"}},
		{"docs/DOER_LOOP.md", []string{
			"same immutable Loop", "Selected-file host consent does not authorize native code.",
			"cumulative remaining-attempt budget", "immutable explicit source/test/module context",
		}, []string{"Selected-file host consent authorizes native code."}},
		{"docs/HERMES_SKILLS.md", []string{
			"Queue preparations/list/result/show/process/bind-runtime",
			"adapters are delivered by #350", "structural_fixture_validation",
			"does not run an agent", "Unknown mutation outcomes",
		}, []string{"structural_fixture_validation proves installed-agent behavior"}},
		{"skills/aegis/SKILL.md", []string{
			"queue preparations|list|result|show|process|bind-runtime",
			"delivered by #350", "No local-store fallback",
		}, []string{"all Queue lifecycle operations support --target"}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			text := string(mustRead(t, filepath.Join(repositoryRoot(t), tc.path)))
			for _, required := range tc.required {
				if !strings.Contains(text, required) {
					t.Errorf("missing guidance %q", required)
				}
			}
			for _, forbidden := range tc.forbid {
				if strings.Contains(text, forbidden) {
					t.Errorf("contradictory guidance %q", forbidden)
				}
			}
		})
	}
}
