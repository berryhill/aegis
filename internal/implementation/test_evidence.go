package implementation

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/loop"
	"io"
)

// Native repository code is trusted; test2json is not a hostile-code attestation.
func requiredTestsPassed(output []byte, required []loop.RequiredGoTest) bool {
	if len(required) == 0 {
		return false
	}
	type event struct{ Action, Package, Test string }
	started := map[loop.RequiredGoTest]bool{}
	passed := map[loop.RequiredGoTest]bool{}
	dec := json.NewDecoder(bytes.NewReader(output))
	for {
		var ev event
		err := dec.Decode(&ev)
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		key := loop.RequiredGoTest{Package: ev.Package, Name: ev.Test}
		switch ev.Action {
		case "run":
			started[key] = true
		case "pass":
			if started[key] {
				passed[key] = true
			}
		case "fail", "skip":
			delete(passed, key)
			if ev.Action == "fail" {
				return false
			}
		}
	}
	for _, test := range required {
		if !started[test] || !passed[test] {
			return false
		}
	}
	return true
}

// Closed feedback contains policy identities and fixed statuses only, never
// host Output, stderr, arbitrary event identities or shell text.
func closedCheckDiagnostics(output []byte, passed bool, required []loop.RequiredGoTest) []byte {
	type result struct {
		Package string `json:"package"`
		Test    string `json:"test"`
		Status  string `json:"status"`
	}
	feedback := struct {
		Code  string   `json:"code"`
		Tests []result `json:"tests"`
	}{Code: "go_test_failed"}
	if passed {
		feedback.Code = "go_test_passed"
	}
	for _, test := range required {
		status := "missing_or_failed"
		if requiredTestsPassed(output, []loop.RequiredGoTest{test}) {
			status = "passed"
		}
		feedback.Tests = append(feedback.Tests, result{test.Package, test.Name, status})
	}
	b, _ := json.Marshal(feedback)
	if len(b) > 65536 {
		return []byte(`{"code":"go_test_feedback_limit","tests":[]}`)
	}
	return b
}
