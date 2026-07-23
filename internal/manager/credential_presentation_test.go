package manager

import (
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
)

func TestCredentialListResultIsReadableAndComplete(t *testing.T) {
	created := time.Date(2026, time.July, 23, 15, 53, 1, 0, time.UTC)
	records := []credentials.SecretRecord{
		{ID: "secret-one", Reference: "bd-site-doppler-prod", Kind: "opaque", Status: credentials.StatusActive, CurrentVersion: 1, CreatedAt: created, CreatedBy: "principal"},
		{ID: "secret-two", Reference: "old-token", Kind: "api-key", Status: credentials.StatusRevoked, CurrentVersion: 3, CreatedAt: created, CreatedBy: "principal", RevokedAt: created.Add(time.Hour), Revocation: "retired"},
	}

	message, err := credentialListResult(records, "doppler", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`Credentials matching "doppler" (2)`,
		"1. bd-site-doppler-prod",
		"active | opaque | version 1",
		"created 2026-07-23 15:53:01 UTC by principal",
		"id secret-one",
		"2. old-token",
		"revoked 2026-07-23 16:53:01 UTC | reason retired",
	} {
		if !strings.Contains(message, expected) {
			t.Errorf("readable list missing %q:\n%s", expected, message)
		}
	}
	if strings.Contains(message, `{"id"`) || strings.Contains(message, "0001-01-01") {
		t.Fatalf("list leaked raw JSON or zero-value noise:\n%s", message)
	}
}

func TestCredentialRecordAndEmptyListResultsAreReadable(t *testing.T) {
	record := credentials.SecretRecord{ID: "secret-one", Reference: "demo", Kind: "opaque", Status: credentials.StatusActive, CurrentVersion: 1, CreatedAt: time.Date(2026, time.July, 23, 15, 53, 1, 0, time.UTC), CreatedBy: "principal"}
	message, err := credentialRecordResult("Credential created", record, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Credential created", "reference  demo", "status     active", "record id  secret-one"} {
		if !strings.Contains(message, expected) {
			t.Errorf("detail missing %q:\n%s", expected, message)
		}
	}

	empty, err := credentialListResult(nil, "missing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty != "Credentials matching \"missing\" (0)\n  No matching credentials." {
		t.Fatalf("empty result=%q", empty)
	}
}
