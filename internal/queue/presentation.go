package queue

import (
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	ArchiveSchemaVersion     = "aegis.queue.archive.v1"
	PreferencesSchemaVersion = "aegis.queue.preferences.v1"
)

// LifecycleSelection is presentation only. It never admits execution or changes
// the authoritative lifecycle. Empty explicit selection means use preferences.
type LifecycleSelection string

const (
	SelectionAll       LifecycleSelection = "all"
	SelectionSucceeded LifecycleSelection = "succeeded"
)

func (s LifecycleSelection) Validate() error {
	if terminalState(State(s)) || State(s).IsPreparation() || State(s) == StateClaimed {
		return nil
	}
	switch s {
	case "all", "queued", "claimable", "active", "retrying", "cancelled", "expired", "denied", "failed", "exhausted", "succeeded":
		return nil
	}

	return errors.New("invalid queue lifecycle selection")
}

type ArchiveVisibility string

const (
	VisibilityUnarchived ArchiveVisibility = "unarchived"
	VisibilityArchived   ArchiveVisibility = "archived"
	VisibilityAll        ArchiveVisibility = "all"
)

func (v ArchiveVisibility) Validate() error {
	if v != VisibilityUnarchived && v != VisibilityArchived && v != VisibilityAll {
		return errors.New("invalid queue archive visibility")
	}
	return nil
}

// Archive is an append-only principal-scoped visibility revision, not an
// execution state or deletion. Queue references and execution facts never change.
type Archive struct {
	SchemaVersion string              `json:"schema_version"`
	PrincipalID   string              `json:"principal_id"`
	SubjectID     string              `json:"subject_id"`
	RequestID     string              `json:"request_id"`
	QueueItem     reference.DigestRef `json:"queue_item"`
	Archived      bool                `json:"archived"`
	Revision      uint64              `json:"revision"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Digest        string              `json:"digest"`
}

type Preferences struct {
	SchemaVersion string             `json:"schema_version"`
	PrincipalID   string             `json:"principal_id"`
	SubjectID     string             `json:"subject_id"`
	RequestID     string             `json:"request_id"`
	Selection     LifecycleSelection `json:"selection"`
	Visibility    ArchiveVisibility  `json:"visibility"`
	Revision      uint64             `json:"revision"`
	UpdatedAt     time.Time          `json:"updated_at"`
	Digest        string             `json:"digest"`
}

func NewArchive(v Archive) (Archive, error) {
	v.SchemaVersion, v.Digest = ArchiveSchemaVersion, ""
	return seal(v, validateArchive)
}
func MarshalArchive(v Archive) ([]byte, error)   { return marshal(v, validateArchive) }
func UnmarshalArchive(b []byte) (Archive, error) { return decode[Archive](b, validateArchive) }
func validateArchive(v Archive) error {
	if v.SchemaVersion != ArchiveSchemaVersion || !validID(v.PrincipalID) || !validID(v.SubjectID) || !validID(v.RequestID) || v.QueueItem.Validate() != nil || v.Revision == 0 || v.UpdatedAt.IsZero() {
		return errors.New("invalid queue archive")
	}
	return validateDigest(v, v.Digest)
}
func NewPreferences(v Preferences) (Preferences, error) {
	v.SchemaVersion, v.Digest = PreferencesSchemaVersion, ""
	return seal(v, validatePreferences)
}
func MarshalPreferences(v Preferences) ([]byte, error) { return marshal(v, validatePreferences) }
func UnmarshalPreferences(b []byte) (Preferences, error) {
	return decode[Preferences](b, validatePreferences)
}
func validatePreferences(v Preferences) error {
	if v.SchemaVersion != PreferencesSchemaVersion || !validID(v.PrincipalID) || !validID(v.SubjectID) || !validID(v.RequestID) || v.Selection.Validate() != nil || v.Visibility.Validate() != nil || v.Revision == 0 || v.UpdatedAt.IsZero() {
		return errors.New("invalid queue preferences")
	}
	return validateDigest(v, v.Digest)
}

// ResolveSelection validates even explicit query choices. An override does not
// persist anything and can never alter authority.
func ResolveSelection(saved Preferences, explicit LifecycleSelection, visibility ArchiveVisibility) (LifecycleSelection, ArchiveVisibility, error) {
	selection := saved.Selection
	if selection == "" {
		selection = SelectionSucceeded
	}
	if explicit != "" {
		selection = explicit
	}
	selectedVisibility := saved.Visibility
	if selectedVisibility == "" {
		selectedVisibility = VisibilityUnarchived
	}
	if visibility != "" {
		selectedVisibility = visibility
	}
	if err := selection.Validate(); err != nil {
		return "", "", err
	}
	if err := selectedVisibility.Validate(); err != nil {
		return "", "", err
	}
	return selection, selectedVisibility, nil
}
