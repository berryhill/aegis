package continuation

import "github.com/berryhill/aegis/internal/reference"

// Reexecution selects immutable prior failure facts, not authority or a count.
// It belongs to operational run selection, never an immutable Loop definition.
type Reexecution struct {
	PreviousQueue       reference.DigestRef `json:"previous_queue"`
	PreviousDisposition reference.DigestRef `json:"previous_disposition"`
}
