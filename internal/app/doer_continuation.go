package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/continuation"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
	"os"
	"path/filepath"
	"reflect"
	"time"
)

var ErrDoerContinuationUnavailable = errors.New("Doer canonical signed continuation custody unavailable")

type DoerContinuationIntent struct {
	DraftID         string
	DraftVersion    uint64
	CandidateDigest string
	Agent           reference.RevisionRef
	Loop            reference.RevisionRef
	Charter         reference.RevisionRef
	PublicationKey  string
	Run             QueueLoopInput
	Requester       core.Subject
	SessionID       string
	DeploymentID    string
	ConfigIdentity  string
	ExpiresAt       time.Time
}

// ApproveDoerContinuationAs is called only after explicit approval by the
// authenticated Unix peer. It grants no host, model, provisioning or session gate.
func (s *Service) ApproveDoerContinuationAs(ctx context.Context, controller core.Subject, intent DoerContinuationIntent) (string, error) {
	if err := doerContinuationContext(ctx); err != nil {
		return "", err
	}
	if err := s.requireLocalHermesImportPrincipal(controller, localHermesImportUnixPeer); err != nil {
		return "", err
	}
	now := s.Now()
	if controller.Kind != "human" || s.Config.Principal.AuthTTL <= 0 || !now.Before(controller.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return "", ErrDenied
	}
	if !now.Before(intent.ExpiresAt) || intent.ExpiresAt.After(controller.ExpiresAt) || intent.ExpiresAt.After(controller.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return "", ErrExpired
	}
	if err := s.validateDoerContinuationIntent(ctx, intent.Requester, intent.SessionID, intent); err != nil {
		return "", err
	}
	stanza, err := s.doerContinuationSelection(ctx, controller, intent)
	if err != nil {
		return "", err
	}
	a := continuation.Approval{Purpose: continuation.Purpose, Intent: continuationIntent(intent), Executor: controller, StanzaID: stanza}
	a.ID = continuation.StableID(a.Intent)
	approved, err := s.Store.ApproveDoerContinuation(a)
	if errors.Is(err, continuation.ErrConflict) {
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	readback, err := s.Store.ReadDoerContinuation(approved.ID)
	if err != nil || !reflect.DeepEqual(readback, approved) {
		return "", ErrDenied
	}
	return approved.ID, nil
}

// RecoverDoerContinuationAs reads the stable signed slot before any new native
// review. An empty ID means only that the canonical record is absent; any
// existing unreadable, mismatched, or expired approval fails closed. Recovery
// never signs, initializes custody, authenticates a replacement, or renews time.
func (s *Service) RecoverDoerContinuationAs(ctx context.Context, requester core.Subject, sessionID string, intent DoerContinuationIntent) (string, core.Subject, error) {
	if err := doerContinuationContext(ctx); err != nil {
		return "", core.Subject{}, err
	}
	if err := s.validateDoerContinuationIntent(ctx, requester, sessionID, intent); err != nil {
		return "", core.Subject{}, err
	}
	id := continuation.StableID(continuationIntent(intent))
	_, err := s.Store.ReadDoerContinuation(id)
	if err != nil {
		// Read verifies custody before the document. A missing signing key is
		// NOT a missing approval: an existing slot must never reopen review.
		if errors.Is(err, os.ErrNotExist) {
			path := filepath.Join(s.Store.Root(), "controller-continuation", "approvals", id+".json")
			if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
				return "", core.Subject{}, nil
			}
		}
		return "", core.Subject{}, ErrDenied
	}
	executor, err := s.ResolveDoerContinuationAs(ctx, requester, sessionID, id, intent)
	if err != nil {
		return "", core.Subject{}, err
	}
	if s.Config.Principal.AuthTTL <= 0 || intent.ExpiresAt.After(executor.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return "", core.Subject{}, ErrExpired
	}
	return id, executor, nil
}

// Resolve preserves the genuine original SO_PEERCRED subject. No Authenticate
// fallback, browser-to-local relabeling, renewed lifetime or inferred consent.
func (s *Service) ResolveDoerContinuationAs(ctx context.Context, requester core.Subject, sessionID, id string, intent DoerContinuationIntent) (core.Subject, error) {
	if err := doerContinuationContext(ctx); err != nil {
		return core.Subject{}, err
	}
	if err := s.validateDoerContinuationIntent(ctx, requester, sessionID, intent); err != nil {
		return core.Subject{}, err
	}
	a, err := s.Store.ReadDoerContinuation(id)
	if err != nil {
		return core.Subject{}, ErrDenied
	}
	want, err := json.Marshal(continuationIntent(intent))
	if err != nil {
		return core.Subject{}, ErrDenied
	}
	got, err := json.Marshal(a.Intent)
	if err != nil || !bytes.Equal(want, got) {
		return core.Subject{}, ErrConflict
	}
	executor := a.Executor
	if err = s.requireLocalHermesImportPrincipal(executor, localHermesImportUnixPeer); err != nil {
		return core.Subject{}, err
	}
	if executor.Kind != "human" || !s.Now().Before(executor.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return core.Subject{}, ErrExpired
	}
	if s.Current == nil {
		return core.Subject{}, ErrDenied
	}
	current, err := s.Current()
	if err != nil || current == nil || current.Uid != s.Config.Principal.UID || current.Username != s.Config.Principal.User {
		return core.Subject{}, ErrDenied
	}
	stanza, err := s.doerContinuationSelection(ctx, executor, intent)
	if err != nil || stanza != a.StanzaID {
		return core.Subject{}, ErrDenied
	}
	return executor, nil
}

// Queue delegates only the exact signed input and original executor to the
// existing runtime-effect checks and durable QueueLoop fences. Immutable outcome
// observations preserve uncertainty; they are not execution authority.
func (s *Service) QueueDoerContinuationAs(ctx context.Context, requester core.Subject, sessionID, id string, intent DoerContinuationIntent) (QueueLoopResult, error) {
	executor, err := s.ResolveDoerContinuationAs(ctx, requester, sessionID, id, intent)
	if err != nil {
		return QueueLoopResult{}, err
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer_continuation_request", SubjectID: requester.ID, PrincipalID: requester.PrincipalID, Outcome: "accepted", Reason: "exact_signed_intent", Metadata: map[string]string{"continuation_id": id, "executor_id": executor.ID}}); err != nil {
		return QueueLoopResult{}, err
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer_continuation_executor", SubjectID: executor.ID, PrincipalID: executor.PrincipalID, Outcome: "accepted", Reason: "original_unix_peer", Metadata: map[string]string{"continuation_id": id, "requester_id": requester.ID}}); err != nil {
		return QueueLoopResult{}, err
	}
	result, queueErr := s.QueueLoopAs(ctx, executor, intent.Run)
	outcome := struct {
		ContinuationID string          `json:"continuation_id"`
		RequesterID    string          `json:"requester_id"`
		ExecutorID     string          `json:"executor_id"`
		ExecutionKey   string          `json:"execution_key"`
		Result         QueueLoopResult `json:"result"`
		Uncertain      bool            `json:"uncertain"`
	}{id, requester.ID, executor.ID, intent.Run.IdempotencyKey, result, queueErr != nil}
	observationID := core.Digest(outcome)[len("sha256:"):]
	if err = s.Store.Create("doer-continuation-outcomes", observationID, outcome); err != nil && !errors.Is(err, store.ErrAlreadyExists) {
		return result, err
	}
	var stored json.RawMessage
	if err = s.Store.Load("doer-continuation-outcomes", observationID, &stored); err != nil {
		return result, err
	}
	want, _ := json.Marshal(outcome)
	var compact bytes.Buffer
	if json.Compact(&compact, stored) != nil || !bytes.Equal(compact.Bytes(), want) {
		return result, ErrConflict
	}
	return result, queueErr
}

// Trusted transport callers derive this from current typed controller config,
// never from a browser field. A digest change invalidates every saved approval.
func (s *Service) DoerContinuationConfigIdentity() string { return core.Digest(s.Config) }

func continuationIntent(i DoerContinuationIntent) continuation.Intent {
	return continuation.Intent{DraftID: i.DraftID, DraftVersion: i.DraftVersion, CandidateDigest: i.CandidateDigest, Agent: i.Agent, Loop: i.Loop, Charter: i.Charter, PublicationKey: i.PublicationKey, Run: continuation.Run(i.Run), Requester: i.Requester, SessionID: i.SessionID, DeploymentID: i.DeploymentID, ConfigIdentity: i.ConfigIdentity, ExpiresAt: i.ExpiresAt}
}

func (s *Service) doerContinuationSelection(ctx context.Context, executor core.Subject, i DoerContinuationIntent) (string, error) {
	if s.FleetRepository == nil {
		return "", ErrFleetUnavailable
	}
	agent, err := s.FleetRepository.LatestAgentRevision(ctx, i.Agent.ID)
	if err != nil || agent.AgentID != i.Agent.ID || agent.Revision != i.Agent.Revision || agent.Digest != i.Agent.Digest || agent.Charter != i.Charter || agent.Lifecycle != registry.LifecycleEnabled {
		return "", ErrDenied
	}
	charter, err := s.GetCharter(i.Charter.ID, i.Charter.Revision)
	if err != nil || charter.Digest != i.Charter.Digest || core.VerifyCanonical(charter) != nil {
		return "", ErrDenied
	}
	selected, err := s.Select(charter, executor, "", core.Environment{Name: "local"})
	if err != nil || selected.Selected == nil || selected.MatchingCount != 1 {
		return "", ErrDenied
	}
	return selected.Selected.ID, nil
}

func doerContinuationContext(ctx context.Context) error {
	if ctx == nil {
		return ErrDenied
	}
	return ctx.Err()
}

// This checks the authenticated browser principal and exact non-authorizing
// draft intent. Signature and fresh original executor admission remain separate.
func (s *Service) validateDoerContinuationIntent(ctx context.Context, requester core.Subject, sessionID string, intent DoerContinuationIntent) error {
	now := s.Now()
	if requester.ID == "" || requester.Kind != "principal" || requester.Issuer != "aegis-principal-auth" || requester.Method != "password" || requester.AuthenticatedAt.IsZero() || requester.AuthenticatedAt.After(now) || !requester.AuthenticatedAt.Before(requester.ExpiresAt) || s.requirePrincipal(requester) != nil {
		return ErrDenied
	}
	if sessionID == "" || len(sessionID) > 256 || sessionID != intent.SessionID || !reflect.DeepEqual(requester, intent.Requester) {
		return ErrDenied
	}
	if intent.DeploymentID == "" || intent.DeploymentID != s.Config.Credentials.Authority.DeploymentID || intent.ConfigIdentity != s.DoerContinuationConfigIdentity() {
		return ErrDenied
	}
	if !now.Before(intent.ExpiresAt) || intent.ExpiresAt.After(requester.ExpiresAt) || intent.ExpiresAt.After(requester.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return ErrExpired
	}
	if intent.Agent.Validate() != nil || intent.Loop.Validate() != nil || intent.Charter.Validate() != nil || intent.Run.IdempotencyKey == "" || len(intent.Run.IdempotencyKey) > 256 || intent.Run.Agent != intent.Agent || intent.Run.Loop != intent.Loop {
		return ErrDenied
	}
	draft, err := s.ReadDoerDraftAs(ctx, requester, intent.DraftID)
	if err != nil {
		return err
	}
	if draft.Version != intent.DraftVersion || draft.Agent != intent.Agent || draft.PublicationKey != intent.PublicationKey || draft.LoopID != intent.Loop.ID || draft.Revision != intent.Loop.Revision {
		return ErrConflict
	}
	candidate, _, err := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil || candidate.Digest != intent.CandidateDigest || candidate.Digest != intent.Loop.Digest {
		return ErrConflict
	}
	if intent.ExpiresAt.After(draft.ExpiresAt) {
		return ErrExpired
	}
	return nil
}
