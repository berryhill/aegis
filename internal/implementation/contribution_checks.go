package implementation

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/loop"
)

func previousBytes(db Store, d string) []byte {
	b, err := db.Get(blobKey(d))
	if err != nil || digest(b) != d {
		return nil
	}
	return b
}

// Compile failures and skipped/missing tests are not a reproduced behavior defect.
func requiredRegressionFailed(output []byte, required []loop.RequiredGoTest) bool {
	states := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(output))
	for dec.More() {
		var event struct{ Action, Package, Test string }
		if dec.Decode(&event) != nil {
			return false
		}
		if event.Test != "" && (event.Action == "pass" || event.Action == "fail" || event.Action == "skip") {
			states[event.Package+"/"+event.Test] = event.Action
		}
	}
	failed := false
	for _, test := range required {
		state := states[test.Package+"/"+test.Name]
		if state != "pass" && state != "fail" {
			return false
		}
		failed = failed || state == "fail"
	}
	return failed
}
