package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/core"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// QueueResult is an execution view, never a definition output port. Duration
// includes Queue waiting: immutable item.enqueued_at -> disposition.occurred_at.
// Usage is unavailable because canonical execution facts contain no measurement.
type QueueResult struct {
	QueueItem    reference.DigestRef   `json:"queue_item"`
	GraphRunID   string                `json:"graph_run_id"`
	Source       reference.RevisionRef `json:"source"`
	Status       queue.State           `json:"status"`
	Reason       string                `json:"reason"`
	Verification string                `json:"verification"`
	Task         string                `json:"task,omitempty"`
	Usage        string                `json:"usage"`
	Elapsed      *QueueResultElapsed   `json:"elapsed,omitempty"`
	Result       *SelectedQueueResult  `json:"result,omitempty"`
}
type QueueResultElapsed struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Nanoseconds int64     `json:"nanoseconds"`
	Source      string    `json:"source"`
}
type SelectedQueueResult struct {
	Name            string                       `json:"name"`
	Loop            reference.RevisionRef        `json:"loop"`
	Participant     reference.RevisionRef        `json:"participant"`
	LoopExecutionID string                       `json:"loop_execution_id"`
	AttemptID       string                       `json:"attempt_id"`
	Artifact        evidence.RuntimeArtifact     `json:"artifact"`
	Receipt         evidence.VerificationReceipt `json:"receipt"`
	ContractDigest  string                       `json:"contract_digest"`
	FileRef         string                       `json:"file_ref"`
	SizeBytes       int                          `json:"size_bytes"`
	MediaType       string                       `json:"media_type"`
	Encoding        string                       `json:"encoding"`
	Content         string                       `json:"content"`
}

func (s *Service) GetQueueResult(ctx context.Context, id string) (QueueResult, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return QueueResult{}, err
	}
	return s.GetQueueResultAs(ctx, subject, id)
}

// GetQueueResultAs first uses existing authenticated metadata visibility, then
// requires the pinned participant's stable owner before disclosing task/content.
// No Agent selector, raw blob reference or filesystem path is accepted.
func (s *Service) GetQueueResultAs(ctx context.Context, subject core.Subject, id string) (QueueResult, error) {
	if id == "" || len(id) > 255 {
		return QueueResult{}, fleet.ErrNotFound
	}
	v, err := s.GetQueueItemAs(ctx, subject, id)
	if err != nil {
		return QueueResult{}, err
	}
	out := QueueResult{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: v.Item.ItemID, Digest: v.Item.Digest}, GraphRunID: v.Item.GraphRunID, Source: v.Source, Status: v.Projection.State, Verification: "unverified", Usage: "unavailable"}
	snapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, v.Item.Snapshot.ID)
	if err != nil || snapshot.Digest != v.Item.Snapshot.Digest || v.GraphRun.Snapshot != v.Item.Snapshot || v.Submission.Snapshot != v.Item.Snapshot {
		return QueueResult{}, fleet.ErrCorrupt
	}
	topology, err := s.Fleet.ResolveRunTopology(ctx, snapshot)
	if err != nil {
		return QueueResult{}, fleet.ErrCorrupt
	}
	if snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || len(topology.Nodes) != 1 {
		out.Reason = "result_source_unsupported"
		return out, nil
	}
	node := topology.Nodes[0]
	participant, err := s.FleetRepository.GetAgentRevision(ctx, node.Participant.ID, node.Participant.Revision)
	if err != nil || participant.Digest != node.Participant.Digest {
		return QueueResult{}, fleet.ErrCorrupt
	}
	if participant.Ownership.OwnerID != subject.PrincipalID {
		return QueueResult{}, ErrDenied
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
	if err != nil || revision.Digest != node.Loop.Digest || v.Source != node.Loop {
		return QueueResult{}, fleet.ErrCorrupt
	}
	if revision.SchemaVersion != loop.DoerReusableSchemaVersion {
		out.Reason = "result_source_unsupported"
		return out, nil
	}
	contract, err := orchestration.BindDirectDoerContract(revision, node, snapshot)
	if err != nil {
		return QueueResult{}, fleet.ErrCorrupt
	}
	out.Task = contract.Task
	if v.Disposition == nil {
		out.Reason = "execution_not_terminal"
		return out, nil
	}
	d := v.Disposition
	out.Reason = d.ReasonCode
	if d.GraphRunID != v.Item.GraphRunID || d.QueueItem != out.QueueItem {
		return QueueResult{}, fleet.ErrCorrupt
	}
	if d.OccurredAt.Before(v.Item.EnqueuedAt) {
		return QueueResult{}, fleet.ErrCorrupt
	}
	out.Elapsed = &QueueResultElapsed{Start: v.Item.EnqueuedAt, End: d.OccurredAt, Nanoseconds: d.OccurredAt.Sub(v.Item.EnqueuedAt).Nanoseconds(), Source: "queue.item.enqueued_at_to_disposition.occurred_at"}
	if d.State != execution.StateSucceeded || v.Projection.State != queue.StateSucceeded {
		return out, nil
	}
	selected, err := s.verifiedSelectedQueueResult(ctx, v, node, contract)
	if err != nil {
		return QueueResult{}, err
	}
	out.Result = &selected
	out.Verification = "verified"
	return out, nil
}

func (s *Service) verifiedSelectedQueueResult(ctx context.Context, v QueueExecutionView, node graph.Node, contract loop.DoerContract) (SelectedQueueResult, error) {
	bad := func() (SelectedQueueResult, error) { return SelectedQueueResult{}, fleet.ErrCorrupt }
	d := v.Disposition
	if d == nil || v.Artifact == nil || len(d.ArtifactIDs) != 1 || len(d.ReceiptIDs) != 1 || len(v.Receipts) != 1 || len(v.LoopExecutions) != 1 {
		return bad()
	}
	authority := v.Item.Authority
	if v.RuntimeAuthorityBinding != nil {
		authority = v.RuntimeAuthorityBinding.Authority
	}
	if d.Authority != authority {
		return bad()
	}
	child := v.LoopExecutions[0]
	if child.LoopExecutionID != d.LoopExecutionID || child.GraphRunID != d.GraphRunID || child.GraphNodeID != node.ID || child.Loop != node.Loop || child.Participant != node.Participant {
		return bad()
	}
	var attempt *execution.Attempt
	for i := range v.Attempts {
		if v.Attempts[i].AttemptID == d.AttemptID {
			if attempt != nil {
				return bad()
			}
			attempt = &v.Attempts[i]
		}
	}
	if attempt == nil || attempt.QueueItem != d.QueueItem || attempt.GraphRunID != d.GraphRunID || attempt.LoopExecutionID != d.LoopExecutionID {
		return bad()
	}
	found := false
	for _, claim := range v.Claims {
		if claim.ClaimID == attempt.ClaimID {
			if found || claim.QueueItem != d.QueueItem || claim.AttemptID != d.AttemptID || claim.Authority != d.Authority {
				return bad()
			}
			found = true
		}
	}
	if !found {
		return bad()
	}
	a, r := *v.Artifact, v.Receipts[0]
	if a.Validate() != nil || r.Validate() != nil || a.ID != d.ArtifactIDs[0] || r.ID != d.ReceiptIDs[0] || a.AttemptID != d.AttemptID || a.RunID != d.LoopExecutionID || a.OwnerID != node.Participant.ID || a.AuthorityContextID != d.Authority.ID || a.AuthorityContextDigest != d.Authority.Digest || a.MediaType != "application/json" {
		return bad()
	}
	action := false
	revision, err := s.FleetRepository.GetLoopRevision(ctx, node.Loop.ID, node.Loop.Revision)
	if err != nil || revision.Digest != node.Loop.Digest || len(revision.RequiredEvidence) != 1 {
		return bad()
	}
	for _, step := range revision.Steps {
		if step.ID == a.ActionID && step.Executable != nil && step.Executable.Operation == loop.DoerVerify {
			action = true
		}
	}
	if !action || revision.RequiredEvidence[0].Claim != r.Claim || revision.RequiredEvidence[0].ProducerStepID != a.ActionID || r.AttemptID != a.AttemptID || r.ArtifactID != a.ID || r.ActionID != a.ActionID || r.RunID != a.RunID || r.OwnerID != a.OwnerID || r.AuthorityContextID != a.AuthorityContextID || r.AuthorityContextDigest != a.AuthorityContextDigest || r.Outcome != evidence.Passed || r.VerifierID != evidence.DoerFileVerifierID || r.PolicyVersion != evidence.DoerFileVerifierPolicyV1 || r.Claim != "selected-file-verified" || r.MediaType != a.MediaType || r.ExpectedDigest != a.Digest || r.ObservedDigest != a.Digest {
		return bad()
	}
	verifier, err := evidence.NewBlobVerifier(s.Store)
	if err != nil {
		return bad()
	}
	if _, err = verifier.AuthorizeCompletion(ctx, a, v.Receipts); err != nil {
		return bad()
	}
	wire, err := s.Store.GetBlob(a.ContentRef)
	if err != nil {
		return bad()
	}
	var proof struct {
		ContractDigest string `json:"contract_digest"`
		FileDigest     string `json:"file_digest"`
		FileRef        string `json:"file_ref"`
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proof) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return bad()
	}
	canonical, marshalErr := json.Marshal(proof)
	if marshalErr != nil || !bytes.Equal(canonical, wire) {
		return bad()
	}
	digest, err := contract.Digest()
	if err != nil || proof.ContractDigest != digest || proof.FileDigest != proof.FileRef {
		return bad()
	}
	content, err := s.Store.GetBlob(proof.FileRef)
	// Preserve the verifier's selected-file limit; never silently truncate bytes.
	if err != nil || len(content) > evidence.SelectedFileMaxBytes {
		return bad()
	}
	if contract.ExpectedText != nil && (!utf8.Valid(content) || strings.TrimSpace(string(content)) != strings.TrimSpace(*contract.ExpectedText)) {
		return bad()
	}
	result := SelectedQueueResult{Name: "result", Loop: node.Loop, Participant: node.Participant, LoopExecutionID: child.LoopExecutionID, AttemptID: attempt.AttemptID, Artifact: a, Receipt: r, ContractDigest: digest, FileRef: proof.FileRef, SizeBytes: len(content), MediaType: "application/octet-stream", Encoding: "base64", Content: base64.StdEncoding.EncodeToString(content)}
	// JSON escaping can expand valid UTF-8 sixfold. Fall back to base64 so
	// the complete result remains below the existing 4 MiB online transport cap.
	textJSON, _ := json.Marshal(string(content))
	if utf8.Valid(content) && len(textJSON) <= 2<<20 {
		result.Encoding = "utf-8"
		result.Content = string(content)
	}
	return result, nil
}
