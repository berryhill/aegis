package loop

import (
	"reflect"
	"testing"
)

func TestDoerStepLanesExtractAndApply(t *testing.T) {
	contract := DoerContract{Task: "Write output", Workspace: "/project", WritableFiles: []string{"out.txt"}, VerifyFile: "out.txt", MaxAttempts: 2}
	v4Base, _, err := NewDoerRevision("doer-v4-lanes", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	v5Base, _, err := NewDoerReusableRevision("doer-v5-lanes", 1, "", DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	lanes := []DoerStepLane{
		{ID: "completion", Provider: "ollama", Model: "qwen3.5:4b", ExecutableProvider: "ollama", ExecutableModel: "qwen3.5:4b"},
		{ID: "implement", Provider: "ollama", Model: "qwen3.5:4b", ExecutableProvider: "ollama", ExecutableModel: "qwen3.5:4b"},
	}
	for name, base := range map[string]LoopRevision{"v4": v4Base, "v5": v5Base} {
		t.Run(name, func(t *testing.T) {
			if got := ExtractDoerStepLanes(base.Steps); len(got) != 0 {
				t.Fatalf("expected no default step lanes, got %+v", got)
			}
			applied, res, err := ApplyDoerStepLanes(base, lanes)
			if err != nil || res.Outcome != ValidationValid {
				t.Fatalf("ApplyDoerStepLanes failed: %v (%+v)", err, res)
			}
			if applied.Digest == base.Digest {
				t.Fatal("step lane binding did not change canonical digest")
			}
			if got := ExtractDoerStepLanes(applied.Steps); !reflect.DeepEqual(got, lanes) {
				t.Fatalf("ExtractDoerStepLanes mismatch: got %+v, want %+v", got, lanes)
			}
			for badName, badLanes := range map[string][]DoerStepLane{
				"unknown-step":    {{ID: "unknown", Provider: "ollama", Model: "qwen3.5:4b"}},
				"duplicate-step":  {{ID: "implement", Provider: "ollama"}, {ID: "implement", Model: "qwen3.5:4b"}},
				"unsorted-steps":  {lanes[1], lanes[0]},
				"empty-lane":      {{ID: "implement"}},
				"non-exec-action": {{ID: "eligibility", ExecutableProvider: "ollama", ExecutableModel: "qwen3.5:4b"}},
			} {
				if _, _, err := ApplyDoerStepLanes(base, badLanes); err == nil {
					t.Fatalf("accepted invalid step lanes (%s): %+v", badName, badLanes)
				}
			}
		})
	}
}
