package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Repository instruction policy, not a natural-language intent classifier or
// proof of engineering execution. Runs in the existing architecture gate.
func TestHermesIssueContributionRoutingPolicy(t *testing.T) {
	required := map[string][]string{
		"AGENTS.md": {
			"Temporary Hermes GitHub issue contribution routing",
			"Hey, implement this GitHub issue", "implement this issue", "fix this GitHub issue",
			"must load and use `aegis-implementation-model` and `aegis-development-loop`",
			"`aegis_implementation_loop`", "execution-capable conductor",
			".hermes/implementation-delivery-contract.md", "Aegis-managed contribution",
		},
		".hermes/README.md": {
			"Temporary Hermes GitHub issue contribution routing", "Hey, implement this GitHub issue",
			"AGENTS.md", "implementation-delivery-contract.md", "Aegis-managed contribution",
		},
		".hermes/implementation-delivery-contract.md": {
			"Temporary Hermes GitHub issue contribution routing", "Hey, implement this GitHub issue",
			"implement this issue", "fix this GitHub issue", "Aegis-managed contribution",
			"must load and use `aegis-implementation-model` and `aegis-development-loop`",
			"active `aegis_implementation_loop`", "execution-capable conductor",
			"`FlowExecutor.run_task_flow()`", "not engineering execution",
			"parent-attended conduction", "approval_transport_unavailable",
			"one source-owning builder", "fresh independent tester", "staged, unstaged and untracked",
			"acceptance before release", "Do not create PlanStore/TaskQueue records",
			"No new queue, daemon, dispatcher, conductor framework or management UI",
			"local head, remote feature head and PR head equality", "PR MERGED",
			"post-merge CI", "no unsolicited release or live activation",
			"A timeout, undeliverable approval and human denial are distinct",
			"fresh effective-loader verification", "Narrower explicit scope wins",
		},
	}
	for name, terms := range required {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repositoryRoot(t), name))
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(strings.Fields(string(data)), " ")
			for _, term := range terms {
				if !strings.Contains(text, term) {
					t.Errorf("%s lost required contribution policy %q", name, term)
				}
			}
		})
	}
}

func TestHermesIssueContributionEngineeringLifecycleOrder(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".hermes/implementation-delivery-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{
		"prepare_feature_worktree", "understand", "inspect", "trace", "implement_minimally",
		"test_the_behavior", "diagnose_and_repeat", "review_security_boundaries",
		"review_launch_assets", "final_verification", "report_concisely",
		"push_feature_branch", "watch_cicd", "merge_on_success_or_loop",
	}
	remaining := string(data)
	for _, step := range steps {
		index := strings.Index(remaining, step)
		if index < 0 {
			t.Fatalf("engineering lifecycle lost or reordered %q", step)
		}
		remaining = remaining[index+len(step):]
	}
}
