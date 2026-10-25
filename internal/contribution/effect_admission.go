package contribution

import "github.com/berryhill/aegis/internal/reference"

// EffectAdmissionRequest is a binding, not a lease or native stop receipt.
// The delivery owner must repeat admission at Fence/Reserve/Finish and keep
// accepted-source custody through the actual synchronous effect.
type EffectAdmissionRequest struct {
	Contract          reference.DigestRef
	RunID             string
	QueueItemID       string
	Candidate         string
	CandidateEvidence reference.DigestRef
	Role              string
	Epoch             uint64
	RoleAuthority     reference.DigestRef
	Scope             string
}
