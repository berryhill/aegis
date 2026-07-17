package specs

import (
	"context"
	"time"
)

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalConsumed ApprovalStatus = "consumed"
	ApprovalExpired  ApprovalStatus = "expired"
)

type ReviewArtifact struct {
	AgentID       AgentID
	Revision      CharterRevision
	CharterDigest Digest
	Runtime       RuntimeDescriptor
	Environment   Environment
	Summary       string
	Diff          string
	Effects       []PlannedEffect
}

type Approval struct {
	ID          ApprovalID
	Artifact    ReviewArtifact
	RequestedBy PrincipalID
	ApprovedBy  PrincipalID
	Status      ApprovalStatus
	RequestedAt time.Time
	DecidedAt   time.Time
	ExpiresAt   time.Time
	ConsumedAt  time.Time
}

type ApprovalDecisionRequest struct {
	ApprovalID ApprovalID
	Principal  AuthenticatedSubject
	Decision   ApprovalStatus
	Reason     string
	DecidedAt  time.Time
}

type ApprovalConsumption struct {
	ApprovalID  ApprovalID
	Digest      Digest
	Runtime     RuntimeDescriptor
	Environment Environment
	ConsumedAt  time.Time
}

// ApprovalService binds principal approval to an exact charter digest,
// runtime, and environment. Consume must atomically verify and consume a
// single-use approval immediately before the protected operation.
type ApprovalService interface {
	Request(context.Context, ReviewArtifact, PrincipalID) (Approval, error)
	Decide(context.Context, ApprovalDecisionRequest) (Approval, error)
	Get(context.Context, ApprovalID) (Approval, error)
	Consume(context.Context, ApprovalConsumption) (Approval, error)
}
