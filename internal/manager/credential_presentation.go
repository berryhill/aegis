package manager

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
)

func credentialCountResult(counts credentials.SecretCounts, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Credential inventory\n  total    %d\n  active   %d\n  revoked  %d", counts.Total, counts.Active, counts.Revoked), nil
}

func credentialListResult(records []credentials.SecretRecord, query string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	title := fmt.Sprintf("Credentials (%d)", len(records))
	if query != "" {
		title = fmt.Sprintf("Credentials matching %s (%d)", strconv.Quote(query), len(records))
	}
	if len(records) == 0 {
		return title + "\n  No matching credentials.", nil
	}

	var output strings.Builder
	output.WriteString(title)
	for index, record := range records {
		fmt.Fprintf(&output, "\n\n  %d. %s\n", index+1, record.Reference)
		fmt.Fprintf(&output, "     %s | %s | version %d\n", record.Status, record.Kind, record.CurrentVersion)
		fmt.Fprintf(&output, "     created %s by %s\n", displayTime(record.CreatedAt), record.CreatedBy)
		fmt.Fprintf(&output, "     id %s", record.ID)
		if record.Status == credentials.StatusRevoked {
			fmt.Fprintf(&output, "\n     revoked %s", displayTime(record.RevokedAt))
			if record.Revocation != "" {
				fmt.Fprintf(&output, " | reason %s", record.Revocation)
			}
		}
	}
	return output.String(), nil
}

func credentialRecordResult(action string, record credentials.SecretRecord, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if record.ID == "" || record.Reference == "" {
		return "", errors.New("manager credential result is invalid")
	}

	var output strings.Builder
	output.WriteString(action)
	fmt.Fprintf(&output, "\n  reference  %s", record.Reference)
	fmt.Fprintf(&output, "\n  status     %s", record.Status)
	fmt.Fprintf(&output, "\n  kind       %s", record.Kind)
	fmt.Fprintf(&output, "\n  version    %d", record.CurrentVersion)
	fmt.Fprintf(&output, "\n  created    %s", displayTime(record.CreatedAt))
	fmt.Fprintf(&output, "\n  created by %s", record.CreatedBy)
	fmt.Fprintf(&output, "\n  record id  %s", record.ID)
	if record.Status == credentials.StatusRevoked {
		fmt.Fprintf(&output, "\n  revoked    %s", displayTime(record.RevokedAt))
		if record.Revocation != "" {
			fmt.Fprintf(&output, "\n  reason     %s", record.Revocation)
		}
	}
	return output.String(), nil
}

func credentialHistoryResult(history []credentials.SecretVersionMetadata, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if len(history) == 0 {
		return "Credential version history (0)\n  No versions found.", nil
	}
	var output strings.Builder
	fmt.Fprintf(&output, "Credential version history (%d)", len(history))
	for index, version := range history {
		fmt.Fprintf(&output, "\n\n  %d. version %d\n", index+1, version.Version)
		fmt.Fprintf(&output, "     created %s | %s\n", displayTime(version.CreatedAt), version.Algorithm)
		fmt.Fprintf(&output, "     KEK version %d | format %d\n", version.KEKVersion, version.FormatVersion)
		fmt.Fprintf(&output, "     ciphertext %s", version.CiphertextHash)
	}
	return output.String(), nil
}

func credentialValueResult(record credentials.SecretRecord, value []byte) string {
	return fmt.Sprintf("Credential value (terminal only)\n  reference  %s\n  record id  %s\n  value      %s\n\nTerminal scrollback remains outside Aegis cleanup.", record.Reference, record.ID, strconv.Quote(string(value)))
}

func credentialMutationResult(action, recordID string, version uint64) string {
	message := action + "\n  record id  " + recordID
	if version != 0 {
		message += fmt.Sprintf("\n  version    %d", version)
	}
	return message
}

func displayTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}
