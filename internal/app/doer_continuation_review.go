package app

import "github.com/berryhill/aegis/internal/core"
import "github.com/berryhill/aegis/internal/loop"

// DoerContinuationReview is bounded review data returned only to the independent
// authenticated local companion. It contains no bearer, cookie or password.
// Digest covers the exact original intent, retained task and owning origin.
type DoerContinuationReview struct {
	Requeue   *QueueRequeueBinding   `json:"requeue,omitempty"`
	Published *loop.LoopRevision     `json:"published,omitempty"`
	Intent    DoerContinuationIntent `json:"intent"`
	// A predecessor review has no retained draft. Omit that zero source rather
	// than emitting a null writable-files collection that the strict protocol denies.
	Draft  DoerDraft `json:"draft,omitzero"`
	Origin string    `json:"origin"`
	Digest string    `json:"digest"`
}

func (r DoerContinuationReview) ContentDigest() string {
	r.Digest = ""
	return core.Digest(r)
}
