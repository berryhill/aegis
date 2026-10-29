package implementation

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIssue382RecordedCheckBinding(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		deny          bool
	}{
		{"failed", `{"output_digest":"OUTPUT","passed":false}`, false},
		{"passed-without-policy-proof", `{"output_digest":"OUTPUT","passed":true}`, true},
		{"unknown-secret", `{"output_digest":"OUTPUT","passed":false,"secret":"canary"}`, true},
		{"missing-field", `{"output_digest":"OUTPUT"}`, true},
		{"null", `null`, true},
		{"duplicate", `{"output_digest":"OUTPUT","passed":false,"passed":true}`, true},
		{"corrupt-json", `{`, true},
		{"wrong-output", `{"output_digest":"foreign","passed":false}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, c := fixture(t)
			cd, err := c.Digest()
			if err != nil {
				t.Fatal(err)
			}
			od, err := e.blob([]byte("recorded check failure"))
			if err != nil {
				t.Fatal(err)
			}
			wd, err := e.blob([]byte("workspace"))
			if err != nil {
				t.Fatal(err)
			}
			r := Record{RunID: "observed", ContractDigest: cd, State: "failed", Passes: []Pass{{ID: "observed/pass/1", WorkspaceDigest: wd, OutputDigest: od}}}
			var value struct {
				OutputDigest string `json:"output_digest"`
				Passed       bool   `json:"passed"`
			}
			value.OutputDigest = od
			canonical, _ := json.Marshal(value)
			payload := []byte(tc.payload)
			// Replace only the digest placeholder, never normalize the tested payload.
			payload = []byte(strings.ReplaceAll(string(payload), "OUTPUT", od))
			if tc.name == "failed" {
				payload = canonical
			}
			sd, err := e.blob(payload)
			if err != nil {
				t.Fatal(err)
			}
			r.Stages = []Stage{{PassID: r.Passes[0].ID, Name: "check", Digest: sd}}
			if err = e.save(r); err != nil {
				t.Fatal(err)
			}
			h, err := e.History(r.RunID, c)
			if tc.deny {
				if err == nil {
					t.Fatal("invalid check accepted")
				}
				return
			}
			if err != nil || len(h.Checks) != 1 || h.Checks[0].Outcome != "failed" || h.Checks[0].OutputDigest != od || h.Checks[0].Digest != sd {
				t.Fatalf("%+v %v", h, err)
			}
			r.Stages[0].PassID = "foreign/pass/1"
			if err = e.save(r); err != nil {
				t.Fatal(err)
			}
			if _, err = e.History(r.RunID, c); err == nil {
				t.Fatal("foreign pass accepted")
			}
			r.Stages = nil
			if err = e.save(r); err != nil {
				t.Fatal(err)
			}
			h, err = e.History(r.RunID, c)
			if err != nil || len(h.Checks) != 0 {
				t.Fatal("missing stage must stay unavailable", err)
			}
		})
	}
}
