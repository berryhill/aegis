package managergateway

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Controlled Hermes protocol, actual owning Service/Queue/compiler/checks/final
// verifier. Local-only target: not live provider or GitHub merge qualification.
func TestEngineeringProseRepairLocalTarget(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}

func TestEngineeringProseHugeAssetsLocalTarget(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}

func engineeringProseGateway(t *testing.T, script, root string, files map[string]string) string {
	t.Helper()
	scope, _ := json.Marshal(contribution.IssueEngineeringScope{SchemaVersion: loop.IssueEngineeringSchema, Outcome: "Repair feature value", Acceptance: "Value returns 42 with regression and preserved trusted checks", WritableFiles: []string{"feature/value.go", "feature/regression_test.go"}, ContextFiles: []string{}, Exclusions: []string{"preserve trusted tests"}})
	proposal := func(value int) string {
		b, _ := json.Marshal(map[string]any{"edits": []implementation.Edit{{Path: "feature/value.go", Content: []byte(fmt.Sprintf("package feature\nfunc Value() int{return %d}\n", value))}, {Path: "feature/regression_test.go", Content: []byte("package feature\nimport \"testing\"\nfunc TestRegression(t *testing.T){if Value()!=42{t.Fatal(\"regression\")}}\n")}}})
		return string(b)
	}
	outputs := []string{`{"read_files":["feature/value.go","feature/regression_test.go"]}`, string(scope), proposal(41), `{"verdict":"PASS"}`, proposal(42), `{"verdict":"PASS"}`}
	if strings.Contains(t.Name(), "EngineeringProseHuge") {
		paths := []string{"feature/value.go", "feature/regression_test.go", "README.md", "docs/feature.md", "web/feature.templ"}
		spec := contribution.IssueEngineeringScope{SchemaVersion: loop.IssueEngineeringSchema, Outcome: "Repair feature and launch assets", Acceptance: "Value returns42; assets are consistent and trusted tests unchanged", WritableFiles: paths, ContextFiles: []string{}, Exclusions: []string{"preserve trusted tests"}}
		scope, _ := json.Marshal(spec)
		selectWire, _ := json.Marshal(map[string]any{"read_files": paths})
		outputs = []string{string(selectWire)}
		for _, path := range paths {
			for range contribution.EngineeringChunks(path, contribution.Digest([]byte(files[path])), files[path]) {
				outputs = append(outputs, `{"observations":"Preserve exact bytes except explicit issue repair; update first asset line."}`)
			}
		}
		outputs = append(outputs, string(scope), proposal(41), `{"verdict":"PASS"}`)
		var good struct {
			Edits []implementation.Edit `json:"edits"`
		}
		if e := json.Unmarshal([]byte(proposal(42)), &good); e != nil {
			t.Fatal(e)
		}
		// Exact wire hunks OMIT content, including null. The legacy Edit marshaler
		// emits null content, so construct the closed engineering variant explicitly.
		edits := []any{}
		for _, edit := range good.Edits {
			edits = append(edits, map[string]any{"path": edit.Path, "content": edit.Content})
		}
		for _, path := range paths[2:] {
			hunks := []implementation.ByteHunk{{Start: 0, End: 0, Content: []byte("Issue-specific reviewed asset update.\n")}}
			if strings.HasSuffix(path, ".templ") {
				start := strings.Index(files[path], "before")
				hunks = []implementation.ByteHunk{{Start: 0, End: 0, Content: []byte("// Issue-specific reviewed asset update.\n")}, {Start: start, End: start + len("before"), Content: []byte("NEW_REQUIRED")}}
			}
			edits = append(edits, map[string]any{"path": path, "preimage_digest": contribution.Digest([]byte(files[path])), "preimage_mode": engineeringCheckoutMode(t), "hunks": hunks})
		}
		body, _ := json.Marshal(map[string]any{"edits": edits})
		outputs = append(outputs, string(body), `{"verdict":"PASS"}`)
	}
	counter := filepath.Join(root, "engineering-turns")
	sequence := fmt.Sprintf("n=0; [ ! -f '%s' ] || n=$(cat '%s'); n=$((n+1)); printf '%%s' \"$n\" > '%s'\ncase \"$n\" in\n", counter, counter, counter)
	for i, output := range outputs {
		sequence += fmt.Sprintf("%d) text='%s';;\n", i+1, nativeJSON(output))
	}
	sequence += "*) text='\"unexpected turn\"';;\nesac"
	start := strings.Index(script, "text='")
	end := strings.Index(script[start:], "\nprintf ") + start
	return script[:start] + sequence + script[end:]
}

func assertEngineeringLargeAdmission(t *testing.T, ctx context.Context, s *app.Service, subject core.Subject, c contribution.Contract) {
	t.Helper()
	wire, e := contribution.Marshal(c)
	if e != nil || len(wire) <= 65536 || len(c.Preparation.Inputs) <= 4 {
		t.Fatalf("large preparation not exercised bytes=%d err=%v", len(wire), e)
	}
	for _, body := range c.Preparation.InputBodies {
		if len(body) > 65536 {
			t.Fatal("planner input cap")
		}
	}
	status, e := s.ReadContributionEngineeringAs(ctx, subject, c.Key)
	if e != nil || status.ReservedTurns != uint32(len(c.Preparation.Inputs)) || status.ReturnedTurns != status.ReservedTurns || status.TurnLimit > contribution.ContributionRuntimeTurns-contribution.EngineeringExecutionReserve {
		t.Fatalf("planning accounting %+v %v", status, e)
	}
	_, state, _, e := s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if e != nil || state.Used.RuntimeTurns != status.ReservedTurns || state.Limits.RuntimeTurns != 128 {
		t.Fatalf("preparation not carried into cumulative budget used=%d want=%d err=%v", state.Used.RuntimeTurns, status.ReservedTurns, e)
	}
	t.Logf("actual service admission: planning_calls=%d immutable_limit=%d contract_bytes=%d cumulative_used=%d", status.ReservedTurns, status.TurnLimit, len(wire), state.Used.RuntimeTurns)
}
func assertEngineeringLargeCandidate(t *testing.T, c contribution.Contract, result app.ContributionExecutionResult, files map[string]string) {
	t.Helper()
	if c.Engineering == nil {
		t.Fatal("missing issue-specific engineering contract")
	}
	derived := false
	for _, p := range c.Engineering.WritableFiles {
		derived = derived || p == "web/feature_templ.go"
	}
	if !derived {
		t.Fatal("controller did not derive exact generated output permission")
	}
	for _, p := range c.Preparation.Scope.WritableFiles {
		if p == "web/feature_templ.go" {
			t.Fatal("fixture supplied generated bytes/path instead of controller derivation")
		}
	}
	for i, pass := range result.Candidate.Passes {
		if pass.Candidate.Generation == "" {
			t.Fatal("missing generation custody")
		}
		generated := false
		for _, f := range pass.Candidate.Source.Files {
			if f.Path == "web/feature_templ.go" {
				generated = true
				if f.Mode != engineeringCheckoutMode(t) {
					t.Fatal("generated preimage mode lost")
				}
				if i == len(result.Candidate.Passes)-1 {
					if !strings.Contains(string(f.Content), "NEW_REQUIRED") || string(f.Content) == files[f.Path] {
						t.Fatal("actual compiler output did not change rendered semantics")
					}
					observed, e := os.ReadFile(filepath.Join(c.Worktree, f.Path))
					if e != nil || string(observed) != string(f.Content) {
						t.Fatal("generated bytes not in final owned worktree", e)
					}
				}
			}
		}
		if !generated {
			t.Fatal("generated source missing from full candidate inventory")
		}
	}
	for _, pass := range result.Candidate.Passes {
		if pass.Candidate.Builder.InputBytes == 0 || pass.Candidate.Builder.InputBytes > 65536 {
			t.Fatal("unbounded builder payload")
		}
	}
	for _, path := range []string{"README.md", "docs/feature.md", "web/feature.templ"} {
		b, e := os.ReadFile(filepath.Join(c.Worktree, path))
		if e != nil || string(b) != hugeAssetExpected(path, files[path]) {
			t.Fatalf("full exact hunk bytes lost %s %v", path, e)
		}
		info, e := os.Stat(filepath.Join(c.Worktree, path))
		if e != nil || info.Mode().Perm() != os.FileMode(engineeringCheckoutMode(t)) {
			t.Fatalf("asset mode lost %s %v", path, e)
		}
	}
	if result.Candidate.Expected == nil {
		t.Fatal("new candidate context was not frozen")
	}
	// SourceDigest identifies the immutable admitted source, not the mutable
	// expected preimages. Check the actual retained edited preimages instead.
	for _, path := range []string{"README.md", "docs/feature.md", "web/feature.templ"} {
		found := false
		for _, file := range result.Candidate.Expected.Files {
			if file.Path == path {
				found = true
				want := hugeAssetExpected(path, files[path])
				if file.Digest != contribution.Digest([]byte(want)) || file.Text != want {
					t.Fatalf("frozen edited context lost %s", path)
				}
			}
		}
		if !found {
			t.Fatalf("frozen edited context missing %s", path)
		}
	}
}

func assertEngineeringProseRepair(t *testing.T, result app.ContributionExecutionResult) {
	t.Helper()
	r := result.Candidate
	if r == nil || len(r.Passes) != 2 || r.Consumed != 2 {
		t.Fatalf("expected bounded bad->repair candidate history: %+v", r)
	}
	first, last := r.Passes[0], r.Passes[1]
	if first.Check.Outcome != "failed" || last.Check.Outcome != "passed" {
		t.Fatalf("independent trusted checks did not reject/accept: %s / %s", first.Check.Outcome, last.Check.Outcome)
	}
	if first.Candidate.Builder.SessionID != last.Candidate.Builder.SessionID || first.Tester.SessionID == last.Tester.SessionID || last.Tester.SessionID == last.Candidate.Builder.SessionID || first.Candidate.Digest == last.Candidate.Digest {
		t.Fatal("builder continuity/tester independence/candidate lineage lost")
	}
	t.Logf("ordinary prose -> two preparation turns -> bad native candidate rejected despite model PASS -> same builder repair -> fresh tester accepted; consumed=%d source=%s", r.Consumed, last.Candidate.Source.Digest)
}

// Git materializes nonexecutable tracked files using the inherited mask. The
// controlled proposer must bind that native preimage, not assume mask 0022.
func engineeringCheckoutMode(t *testing.T) uint32 {
	t.Helper()
	b, err := exec.Command("sh", "-c", "umask").Output()
	if err != nil {
		t.Fatal(err)
	}
	mask, err := strconv.ParseUint(strings.TrimSpace(string(b)), 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	return 0666 &^ uint32(mask)
}
