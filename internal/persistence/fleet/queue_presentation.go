package fleet

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

var ErrPresentationDenied = errors.New("queue presentation ownership or terminal identity denied")

// QueuePresentationRepository is optional; read-only/older repositories need
// not implement mutations. Expected revision zero means no saved record exists.
// Each write atomically appends the canonical revision, replay binding and audit.
type QueuePresentationRepository interface {
	SetQueueArchive(context.Context, queue.Archive, uint64) (queue.Archive, bool, error)
	GetQueueArchive(context.Context, string, reference.DigestRef) (queue.Archive, error)
	SetQueuePreferences(context.Context, queue.Preferences, uint64) (queue.Preferences, bool, error)
	GetQueuePreferences(context.Context, string) (queue.Preferences, error)
}
