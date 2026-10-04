package app

import "github.com/berryhill/aegis/internal/core"

// DoerContinuationReview is bounded review data returned only to the independent
// authenticated local companion. It contains no bearer, cookie or password.
// Digest covers the exact original intent, retained task and owning origin.
type DoerContinuationReview struct {
	Intent DoerContinuationIntent `json:"intent"`
	Draft  DoerDraft              `json:"draft"`
	Origin string                 `json:"origin"`
	Digest string                 `json:"digest"`
}

func (r DoerContinuationReview) ContentDigest() string {
	r.Digest = ""
	return core.Digest(r)
}
