package credentials

import (
	"errors"
	"time"
)

// VaultStatus summarizes the read-only state of a credential authority store
// for the dashboard surface. It must never include key material, ciphertext,
// wrapped DEKs, KEK bytes, or any other keying data. It is built from
// persisted metadata only and is safe to render into the browser.
type VaultStatus struct {
	Database            string    `json:"database"`
	DeploymentID        string    `json:"deployment_id"`
	StoreID             string    `json:"store_id"`
	Custody             string    `json:"custody"`
	KEKID               string    `json:"kek_id"`
	KEKVersion          uint64    `json:"kek_version"`
	SchemaVersion       string    `json:"schema_version"`
	LastCleanShutdown   bool      `json:"last_clean_shutdown"`
	InitializedAt       time.Time `json:"initialized_at,omitempty"`
	BackupPathAvailable bool      `json:"backup_path_available"`
}

// VaultState is the readiness state a vault is reported as. The values are
// stable so the dashboard can map them to display headings.
const (
	VaultStateInitialized = "initialized"
	VaultStateEmpty       = "empty"
	VaultStateLocked      = "locked"
	VaultStateCorrupt     = "corrupt"
	VaultStateUnavailable = "unavailable"
)

// VaultClassifier maps a domain error or condition into a vault state. It is
// deliberately decoupled from the application service so the read model can be
// built by any caller that holds a VaultStatus and a domain error.
func VaultClassifier(err error) string {
	switch {
	case err == nil:
		return VaultStateInitialized
	case errors.Is(err, ErrNotFound):
		return VaultStateEmpty
	case IsPassphraseAuthentication(err):
		return VaultStateLocked
	default:
		return VaultStateUnavailable
	}
}

// HasInitializedVault reports whether the service is configured to expose a
// credential authority at all. The dashboard falls back to "credentials
// surface absent" when this returns false; no count is asserted and no
// proposal is rendered.
func HasInitializedVault(custody, database, deploymentID string) bool {
	return custody != "" && database != "" && deploymentID != ""
}
