package architecture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Repository acceptance policy ONLY. This neither validates runtime artifacts nor
// creates a product verdict. #379 must implement and qualify the real verifier.
const contributionContract = ".hermes/contribution-happy-path-contract.md"

type contributionStage struct {
	ID         string   `json:"id"`
	Obligation string   `json:"obligation"`
	Owners     []int    `json:"owners"`
	Status     string   `json:"status"`
	Source     string   `json:"source"`
	Symbol     string   `json:"symbol"`
	Missing    string   `json:"missing"`
	Evidence   []string `json:"evidence"`
	Recovery   string   `json:"recovery"`
	Boundary   string   `json:"boundary"`
}

// Independent acceptance oracle: deliberate changes require reviewing the
// contract AND this gate. Do not infer required stages from whatever is present.
var contributionRules = []struct {
	id       string
	owners   []int
	evidence string
}{
	{"resolve", []int{375, 377}, "invocation instruction-snapshot ownership"},
	{"seal", []int{375, 377}, "contract role-authority budget"},
	{"prepare", []int{376, 377}, "worktree-custody base-readback inspection"},
	{"build", []int{376, 377}, "builder-dispatch regression-red build"},
	{"freeze", []int{376, 377}, "source-inventory build-inputs executable acceptance-policy"},
	{"test", []int{376, 377}, "tester-dispatch identity-before-after behavior-acceptance"},
	{"repair", []int{376, 377}, "repair-lineage counters"},
	{"review", []int{376, 379}, "security-review launch-review final-verification"},
	{"publish", []int{378, 377}, "publication-intent scoped-commit feature-readback pr-readback"},
	{"checks", []int{378, 377}, "exact-head-ci required-review current-base"},
	{"merge", []int{378, 377}, "merge-intent merged-pr tested-tree-integration base-containment post-merge"},
	{"dispose", []int{379, 378, 377}, "joined-verification contribution-disposition issue-readback retained-result"},
}

var contributionConsumers = []string{
	"AGENTS.md", ".hermes/README.md", ".hermes/implementation-delivery-contract.md", "CONTRIBUTING.md",
	"docs/contribution-intake.md", "skills/aegis/SKILL.md", "skills/aegis-loop-authoring/SKILL.md",
	"skills/aegis-session-operations/SKILL.md", "skills/aegis-execution-queue/SKILL.md", "skills/aegis-evidence-disposition/SKILL.md",
}

func readContributionMatrix(t *testing.T) []contributionStage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repositoryRoot(t), contributionContract))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile("(?s)<!-- contribution-matrix-v1 -->\\s*```json\\s*(.*?)\\s*```\\s*<!-- /contribution-matrix-v1 -->")
	matches := re.FindAllSubmatch(b, -1)
	if len(matches) != 1 {
		t.Fatal("exactly one canonical matrix required")
	}
	var stages []contributionStage
	decoder := json.NewDecoder(strings.NewReader(string(matches[0][1])))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stages); err != nil {
		t.Fatal(err)
	}
	return stages
}

func validateContributionMatrix(stages []contributionStage, read func(string) (string, error)) error {
	if len(stages) != len(contributionRules) {
		return fmt.Errorf("stage count")
	}
	for i, rule := range contributionRules {
		s := stages[i]
		if s.ID != rule.id {
			return fmt.Errorf("stage order")
		}
		if !reflect.DeepEqual(s.Owners, rule.owners) {
			return fmt.Errorf("stage owner")
		}
		if !reflect.DeepEqual(s.Evidence, strings.Fields(rule.evidence)) {
			return fmt.Errorf("stage evidence")
		}
		if s.Obligation == "" || s.Missing == "" || s.Recovery == "" || s.Boundary == "" {
			return fmt.Errorf("incomplete obligation")
		}
		if s.Status != "partial" && s.Status != "unavailable" {
			return fmt.Errorf("unqualified capability")
		}
		if filepath.Clean(s.Source) != s.Source || filepath.IsAbs(s.Source) || strings.HasPrefix(s.Source, "../") || s.Symbol == "" {
			return fmt.Errorf("source anchor")
		}
		text, err := read(s.Source)
		if err != nil || !strings.Contains(text, s.Symbol) {
			return fmt.Errorf("source anchor")
		}
	}
	return nil
}

func validateContributionConsumers(consumers map[string]string) error {
	for _, path := range contributionConsumers {
		text, ok := consumers[path]
		if !ok {
			return fmt.Errorf("missing consumer")
		}
		// Resolve a real Markdown link, not an incidental filename or prose token.
		links := regexp.MustCompile(`\]\(([^)#]+)(?:#[^)]*)?\)`).FindAllStringSubmatch(text, -1)
		found := false
		for _, link := range links {
			if filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(path), link[1]))) == contributionContract {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("stale consumer")
		}
		for _, bad := range []string{"→ `understand`", "prepare_feature_worktree` →", "composes the minimal exact Graph", "contribution completion requires only", "whole-contribution success from selected-file", "implement -> done is the complete contribution"} {
			if strings.Contains(text, bad) {
				return fmt.Errorf("contradictory consumer")
			}
		}
	}
	return nil
}

// Synthetic private fixture references: no expected product output, remote
// response, approval or runtime evidence is authored here.
type contributionProof struct {
	Run                string              `json:"run"`
	Candidate          string              `json:"candidate"`
	Head               string              `json:"head"`
	Policy             string              `json:"policy"`
	Builder            string              `json:"builder"`
	Tester             string              `json:"tester"`
	Target             string              `json:"target"`
	Claim              string              `json:"claim"`
	CandidateAttempts  int                 `json:"candidate_attempts"`
	CandidateLimit     int                 `json:"candidate_limit"`
	QueueAttempts      int                 `json:"queue_attempts"`
	RuntimeTurns       int                 `json:"runtime_turns"`
	DecisionDeliveries int                 `json:"decision_deliveries"`
	Events             []contributionEvent `json:"events"`
}
type contributionEvent struct {
	Stage    string               `json:"stage"`
	Evidence []contributionRecord `json:"evidence"`
}
type contributionRecord struct {
	Kind      string `json:"kind"`
	Run       string `json:"run"`
	Candidate string `json:"candidate"`
	Head      string `json:"head"`
	Policy    string `json:"policy"`
	Role      string `json:"role"`
	Outcome   string `json:"outcome"`
	Tier      string `json:"tier"`
	Content   string `json:"content"`
	Digest    string `json:"digest"`
}

func fixtureDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func syntheticContributionProof(stages []contributionStage) contributionProof {
	p := contributionProof{Run: "fixture-root", Candidate: "fixture-candidate", Head: strings.Repeat("a", 40), Policy: fixtureDigest("trusted-fixture-policy"), Builder: "fixture-builder-session", Tester: "fixture-fresh-tester-session", Target: "verified-protected-base-merge", Claim: "whole-contribution", CandidateAttempts: 1, CandidateLimit: 3, QueueAttempts: 1, RuntimeTurns: 4, DecisionDeliveries: 0}
	for _, stage := range stages {
		event := contributionEvent{Stage: stage.ID}
		role := "fixture-controller-session"
		if stage.ID == "build" {
			role = p.Builder
		}
		if stage.ID == "test" {
			role = p.Tester
		}
		for _, kind := range stage.Evidence {
			content := "synthetic architecture fixture: " + stage.ID + "/" + kind
			event.Evidence = append(event.Evidence, contributionRecord{Kind: kind, Run: p.Run, Candidate: p.Candidate, Head: p.Head, Policy: p.Policy, Role: role, Outcome: "passed", Tier: "synthetic-policy-fixture", Content: content, Digest: fixtureDigest(content)})
		}
		p.Events = append(p.Events, event)
	}
	return p
}
func validateContributionProof(p contributionProof, stages []contributionStage) error {
	if p.Target != "verified-protected-base-merge" || p.Claim != "whole-contribution" {
		return fmt.Errorf("scope boundary")
	}
	if p.Builder == "" || p.Tester == "" || p.Builder == p.Tester {
		return fmt.Errorf("independent role")
	}
	if p.Run == "" || p.Candidate == "" || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(p.Head) || !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(p.Policy) {
		return fmt.Errorf("proof identity")
	}
	if p.CandidateAttempts < 1 || p.CandidateLimit < 1 || p.CandidateLimit > 3 || p.CandidateAttempts > p.CandidateLimit || p.QueueAttempts < 1 || p.RuntimeTurns < 1 || p.DecisionDeliveries < 0 {
		return fmt.Errorf("budget")
	}
	if len(p.Events) != len(stages) {
		return fmt.Errorf("proof stages")
	}
	for i, s := range stages {
		e := p.Events[i]
		if e.Stage != s.ID {
			return fmt.Errorf("proof order")
		}
		if len(e.Evidence) != len(s.Evidence) {
			return fmt.Errorf("proof coverage")
		}
		for j, r := range e.Evidence {
			if r.Kind != s.Evidence[j] {
				return fmt.Errorf("weak proof substitution")
			}
			if r.Run != p.Run || r.Candidate != p.Candidate || r.Head != p.Head || r.Policy != p.Policy {
				return fmt.Errorf("proof binding")
			}
			role := "fixture-controller-session"
			if s.ID == "build" {
				role = p.Builder
			}
			if s.ID == "test" {
				role = p.Tester
			}
			if r.Role != role {
				return fmt.Errorf("role binding")
			}
			if r.Outcome != "passed" {
				return fmt.Errorf("uncovered criterion")
			}
			if r.Tier != "synthetic-policy-fixture" {
				return fmt.Errorf("tier promotion")
			}
			if r.Content == "" || r.Digest != fixtureDigest(r.Content) {
				return fmt.Errorf("corrupt content")
			}
		}
	}
	return nil
}

func TestContributionContractArchitecture(t *testing.T) {
	root := repositoryRoot(t)
	read := func(p string) (string, error) { b, e := os.ReadFile(filepath.Join(root, p)); return string(b), e }
	stages := readContributionMatrix(t)
	if err := validateContributionMatrix(stages, read); err != nil {
		t.Fatal(err)
	}
	consumers := map[string]string{}
	for _, p := range contributionConsumers {
		s, e := read(p)
		if e != nil {
			t.Fatal(e)
		}
		consumers[p] = s
	}
	if err := validateContributionConsumers(consumers); err != nil {
		t.Fatal(err)
	}
	if err := validateContributionProof(syntheticContributionProof(stages), stages); err != nil {
		t.Fatal(err)
	}
}

type contributionMutation struct {
	Name   string          `json:"name"`
	Target string          `json:"target"`
	Path   string          `json:"path"`
	Value  json.RawMessage `json:"value"`
	Want   string          `json:"want"`
}

// Apply an actual JSON-pointer replacement/removal to serialized fixtures. This
// exercises real malformed records rather than testing the presence of prose.
func mutateContributionJSON(node any, parts []string, value any) any {
	if len(parts) == 0 {
		return value
	}
	switch v := node.(type) {
	case map[string]any:
		v[parts[0]] = mutateContributionJSON(v[parts[0]], parts[1:], value)
	case []any:
		var i int
		if _, err := fmt.Sscanf(parts[0], "%d", &i); err != nil || i < 0 || i >= len(v) {
			panic("bad mutation index")
		}
		if len(parts) == 1 && value == nil {
			return append(v[:i], v[i+1:]...)
		}
		v[i] = mutateContributionJSON(v[i], parts[1:], value)
	default:
		panic("bad mutation path")
	}
	return node
}
func TestContributionContractNegativeFixtures(t *testing.T) {
	root := repositoryRoot(t)
	stages := readContributionMatrix(t)
	b, err := os.ReadFile(filepath.Join(root, "internal/architecture/testdata/contribution-negative-fixtures.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mutations []contributionMutation
	if err = json.Unmarshal(b, &mutations); err != nil {
		t.Fatal(err)
	}
	read := func(p string) (string, error) { b, e := os.ReadFile(filepath.Join(root, p)); return string(b), e }
	for _, m := range mutations {
		t.Run(m.Name, func(t *testing.T) {
			var original any
			switch m.Target {
			case "matrix":
				original = stages
			case "proof":
				original = syntheticContributionProof(stages)
			case "consumer":
				original = map[string]string{}
				for _, p := range contributionConsumers {
					s, e := read(p)
					if e != nil {
						t.Fatal(e)
					}
					original.(map[string]string)[p] = s
				}
			default:
				t.Fatal("unknown fixture target")
			}
			data, _ := json.Marshal(original)
			var node, value any
			if err = json.Unmarshal(data, &node); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(m.Value, &value); err != nil {
				t.Fatal(err)
			}
			// JSON pointers escape slash as ~1 and tilde as ~0.
			parts := strings.Split(strings.TrimPrefix(m.Path, "/"), "/")
			for i := range parts {
				parts[i] = strings.ReplaceAll(strings.ReplaceAll(parts[i], "~1", "/"), "~0", "~")
			}
			data, _ = json.Marshal(mutateContributionJSON(node, parts, value))
			var got error
			switch m.Target {
			case "matrix":
				var x []contributionStage
				if err = json.Unmarshal(data, &x); err != nil {
					t.Fatal(err)
				}
				got = validateContributionMatrix(x, read)
			case "proof":
				var x contributionProof
				if err = json.Unmarshal(data, &x); err != nil {
					t.Fatal(err)
				}
				got = validateContributionProof(x, stages)
			case "consumer":
				var x map[string]string
				if err = json.Unmarshal(data, &x); err != nil {
					t.Fatal(err)
				}
				got = validateContributionConsumers(x)
			}
			if got == nil || got.Error() != m.Want {
				t.Fatalf("mutation accepted/wrong denial: got %v want %s", got, m.Want)
			}
		})
	}
}
