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
