package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/registry"
)

// Strict shape/source validation only, not authentication or registration proof.
// The real owning-service regression separately exercises admitted registration.
func TestPackagedRegistrationInput(t *testing.T) {
	name := "../../skills/aegis-agent-registry/references/register-input.v1.json"
	var input app.RegisterFleetAgentInput
	if err := decodeJSONFile(name, &input); err != nil {
		t.Fatal(err)
	}
	source, err := registry.NewCurrentFleetFixtureSource(input.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := source.Discover(context.Background())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidate shape: %v", err)
	}
	if candidates[0].Source != input.Identity {
		t.Fatal("fixture selector mismatch")
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"principal_id", "authority", "token", "mandate"} {
		malformed := strings.Replace(string(raw), "{", `{"`+key+`":"not-authority",`, 1)
		path := filepath.Join(t.TempDir(), "unknown.json")
		if err := os.WriteFile(path, []byte(malformed), 0600); err != nil {
			t.Fatal(err)
		}
		var denied app.RegisterFleetAgentInput
		if err := decodeJSONFile(path, &denied); err == nil {
			t.Fatalf("accepted unknown authority field %s", key)
		}
	}
}
