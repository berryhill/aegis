//go:build linux || darwin

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/skillbundle"
)

// This is installed-artifact/real-decoder proof, not an agent behavior score,
// authenticated service admission, a live runtime, or publication evidence.
func TestDistributedProtectedIntakeContracts(t *testing.T) {
	root := filepath.Join("..", "..")
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	// Unit-test archive identity only; never presented as a source Git receipt.
	revision := strings.Repeat("a", 40)
	digest, err := skillbundle.BuildArchive(root, archive, "0.2.18", revision)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := skillbundle.InstallArchive(context.Background(), archive, digest, revision, home); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(home, "skills", "aegis-credential-authority", "references", "protected-intake.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var suite struct {
		SchemaVersion int               `json:"schema_version"`
		EvidenceClass string            `json:"evidence_class"`
		Protocol      string            `json:"protocol"`
		Qualification string            `json:"qualification"`
		Requests      []json.RawMessage `json:"requests"`
	}
	d := json.NewDecoder(bytes.NewReader(content))
	d.DisallowUnknownFields()
	if err := d.Decode(&suite); err != nil {
		t.Fatal(err)
	}
	if suite.SchemaVersion != 1 || suite.Protocol != managergateway.ProtectedIntakeProtocol || suite.EvidenceClass != "synthetic_metadata_request_contract" || suite.Qualification == "" || len(suite.Requests) != 3 {
		t.Fatal("distributed protocol declaration drift")
	}
	seen := map[string]bool{}
	for _, body := range suite.Requests {
		r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		input, err := decodeManagerIntake(r)
		if err != nil {
			t.Fatal("distributed metadata rejected by actual public decoder")
		}
		if seen[input.Action] || input.OperationID != "synthetic-operation-not-live" {
			t.Fatal("invalid synthetic case identity")
		}
		seen[input.Action] = true
		switch input.Action {
		case "review":
			if input.Reference != "disposable-test" || input.Kind != "opaque" {
				t.Fatal("review metadata drift")
			}
		case "approve", "cancel":
			if input.Reference != "" || input.Kind != "" {
				t.Fatal("approval or cancellation invents mutable metadata")
			}
		default:
			t.Fatal("unsupported action in distributed contract")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
		// Missing required inputs and prompt-supplied authority are rejected
		// before any application service, credentials or store is constructed.
		for _, key := range []string{"action", "operation_id"} {
			original := fields[key]
			delete(fields, key)
			assertDistributedIntakeDenied(t, fields)
			fields[key] = original
		}
		for _, key := range []string{"value", "principal_id", "stage", "expires_at", "authority", "session_id"} {
			fields[key] = json.RawMessage(`"synthetic-untrusted-field"`)
			assertDistributedIntakeDenied(t, fields)
			delete(fields, key)
		}
	}
	if !seen["review"] || !seen["approve"] || !seen["cancel"] {
		t.Fatal("missing operation contract")
	}
	if _, err := skillbundle.InspectInstalled(home); err != nil {
		t.Fatal("decoder checks altered installed inventory")
	}
}

func assertDistributedIntakeDenied(t *testing.T, fields map[string]json.RawMessage) {
	t.Helper()
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if _, err := decodeManagerIntake(r); err == nil {
		t.Fatal("invalid distributed metadata admitted")
	}
}
