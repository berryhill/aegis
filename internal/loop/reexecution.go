package loop

import "github.com/berryhill/aegis/internal/reference"

// Reexecution selects immutable prior failure facts, not authority or a count.
type Reexecution struct {
	PreviousQueue       reference.DigestRef `json:"previous_queue"`
	PreviousDisposition reference.DigestRef `json:"previous_disposition"`
}
