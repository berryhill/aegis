package loop

import (
	"encoding/json"
	"testing"
)

func TestDoerNilCodingHistoricalBytes(t *testing.T) {
	text := "hello"
	c := DoerContract{Task: "Write hello", Workspace: "/workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2}
	const historical = `{"task":"Write hello","workspace":"/workspace","writable_files":["result.txt"],"verify_file":"result.txt","expected_text":"hello","max_attempts":2}`
	wire, err := json.Marshal(c)
	if err != nil || string(wire) != historical {
		t.Fatalf("legacy bytes changed: %s %v", wire, err)
	}
	got, err := c.Digest()
	if err != nil || got != sha256Digest([]byte(historical)) {
		t.Fatalf("legacy digest changed: %s %v", got, err)
	}
}
