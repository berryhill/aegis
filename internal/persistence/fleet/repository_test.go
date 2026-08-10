package fleet

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicMutationEnvelopesUseStableJSONFields(t *testing.T) {
	for name, value := range map[string]any{
		"accepted submission": AcceptedSubmission{},
		"completion":          Completion{},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			text := string(encoded)
			if strings.Contains(text, `"Snapshot"`) || strings.Contains(text, `"Disposition"`) {
				t.Fatalf("unstable exported Go field names leaked into JSON: %s", text)
			}
			if name == "accepted submission" && !strings.Contains(text, `"snapshot"`) {
				t.Fatalf("stable snapshot field missing: %s", text)
			}
			if name == "completion" && !strings.Contains(text, `"disposition"`) {
				t.Fatalf("stable disposition field missing: %s", text)
			}
		})
	}
}
