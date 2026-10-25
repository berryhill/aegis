//go:build linux

package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"os/exec"
	"time"
)

// This observer belongs to one synchronous worker invocation. All producers are
// serialized by ManagedContribution's existing source custody, not a new daemon.
func (s *Service) contributionExecutionObserver(ctx context.Context, sub core.Subject, c contribution.Contract, base orchestration.WorkResult, launch execution.LaunchContract) (*orchestration.ContributionExecutionObserver, error) {
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return nil, ErrDenied
	}
	head := func(ctx context.Context) (contribution.Continuity, error) {
		st, _, e := repo.GetContributionContinuity(ctx, c.QueueItemID)
		return st, e
	}
	appendEvent := func(ctx context.Context, e contribution.ContinuityEvent) error {
		st, err := head(ctx)
		if err != nil {
			return err
		}
		e.SchemaVersion = contribution.ContinuitySchema
		e.QueueItemID = c.QueueItemID
		e.ContractDigest = c.Digest
		e.OwnerID = c.OwnerID
		e.Sequence = st.Sequence + 1
		e.PreviousDigest = st.Digest
		e.At = s.Now()
		if e.SessionID == "" {
			e.SessionID = c.SessionID
		}
		e, err = contribution.SealContinuityEvent(e)
		if err != nil {
			return err
		}
		_, err = repo.AppendContributionContinuity(ctx, e)
		return err
	}
	st, err := head(ctx)
	if err != nil {
		return nil, err
	}
	if st.Sequence == 0 {
		if err = appendEvent(ctx, contribution.ContinuityEvent{Kind: "initialize", Limits: contribution.ContinuityLimits{Candidates: uint32(c.CandidateBudget), QueueAttempts: 1, RuntimeTurns: 128, InfrastructureRechecks: 2, DecisionDeliveries: 8}}); err != nil {
			return nil, err
		}
	}
	if st.Stage == "" {
		if err = appendEvent(ctx, contribution.ContinuityEvent{Kind: "checkpoint", Stage: "prepare", Candidate: c.RunID, Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.Key, Digest: c.Digest}}); err != nil {
			return nil, err
		}
	}
	sessions := map[string]string{}
	role := func(ctx context.Context, index int, session string) error {
		name := []string{"controller", "builder", "tester"}[index]
		st, e := head(ctx)
		if e != nil {
			return e
		}
		if current, exists := st.RoleAuthorities[name]; exists && current.SessionID == session {
			checked, err := s.CheckContributionRoleAs(ctx, sub, session, c.Key, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest})
			if err != nil || checked != current {
				return ErrDenied
			}
			sessions[session] = name
			return nil
		}
		st, e = s.AttachContributionObservationAs(ctx, sub, session, c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest})
		if e != nil {
			return e
		}
		_, e = s.AdmitContributionRoleAs(ctx, sub, session, c.Key, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest})
		if e == nil {
			sessions[session] = name
		}
		return e
	}
	if err = role(ctx, 0, launch.AuthorityContext.SessionID); err != nil {
		return nil, err
	}
	serial := uint64(0)
	native := func(ctx context.Context, session string, cmd *exec.Cmd) error {
		name, ok := sessions[session]
		if !ok {
			return ErrDenied
		}
		st, e := head(ctx)
		if e != nil {
			return e
		}
		r := st.RoleAuthorities[name]
		if r.SessionID != session {
			return ErrDenied
		}
		serial++
		binding := contribution.NativeBinding{IntentID: fmt.Sprintf("native-%s-%d-%d", base.Attempt.AttemptID, r.Epoch, serial), RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: base.Claim.ClaimID, AttemptID: base.Attempt.AttemptID, Role: name, Epoch: r.Epoch, Authority: r.Ref(), Candidate: st.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: hermes.NativeCommandDigest(cmd)}
		boundary, exists := st.NativeBoundaries[name]
		kind := "native-boundary-extend"
		if !exists {
			kind = "native-boundary-open"
			boundary = contribution.NativeBoundary{Role: name, Epoch: r.Epoch}
		}
		boundary.Bindings = append(append([]contribution.NativeBinding(nil), boundary.Bindings...), binding)
		if e = appendEvent(ctx, contribution.ContinuityEvent{SessionID: session, Kind: kind, NativeBoundary: &boundary}); e != nil {
			return fmt.Errorf("native boundary %s/%d: %w", name, r.Epoch, e)
		}
		if e = s.runContributionNativeCommandAs(ctx, sub, session, c.Key, binding, cmd); e != nil {
			return fmt.Errorf("native command %s/%d/%d: %w", name, r.Epoch, serial, e)
		}
		return nil
	}
	closed := map[string]bool{}
	closeRole := func(ctx context.Context, session string) error {
		if closed[session] {
			return nil
		}
		name, ok := sessions[session]
		if !ok {
			return ErrDenied
		}
		st, e := head(ctx)
		if e != nil {
			return e
		}
		if b, exists := st.NativeBoundaries[name]; exists && !b.Closed {
			b.Closed = true
			if e = appendEvent(ctx, contribution.ContinuityEvent{SessionID: session, Kind: "native-boundary-closed", NativeBoundary: &b}); e != nil {
				return e
			}
		}
		st, e = head(ctx)
		if e != nil {
			return e
		}
		_, e = s.stopContributionRole(ctx, c, st, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest})
		if e == nil {
			closed[session] = true
		}
		return e
	}
	return &orchestration.ContributionExecutionObserver{ContinueBuilder: func(ctx context.Context, old, fresh implementation.ContributionRoleIdentity) error {
		st, err := head(ctx)
		if err != nil {
			return err
		}
		r := st.RoleAuthorities["builder"]
		if r.Epoch < 2 || r.SessionID != fresh.SessionID || r.Authority.ID != fresh.AuthorityID || r.Authority.Digest != fresh.AuthorityDigest || r.MandateID != fresh.MandateID || old.AgentDigest != fresh.AgentDigest || old.CharterDigest != fresh.CharterDigest {
			return ErrDenied
		}
		if _, err = s.CheckContributionRoleAs(ctx, sub, fresh.SessionID, c.Key, "builder", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest}); err != nil {
			return err
		}
		previous, err := s.Authority.GetMandate(ctx, old.MandateID)
		if err != nil {
			return err
		}
		current, err := s.Authority.GetMandate(ctx, fresh.MandateID)
		if err != nil || !compatibleContributionMandates(previous, current) {
			return ErrDenied
		}
		_, events, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Kind == "role-admitted" && event.RoleAuthority != nil {
				prior := event.RoleAuthority
				if prior.Role == "builder" && prior.Epoch < r.Epoch && prior.SessionID == old.SessionID && prior.Authority.ID == old.AuthorityID && prior.Authority.Digest == old.AuthorityDigest && prior.MandateID == old.MandateID {
					return nil
				}
			}
		}
		return ErrDenied
	}, Checkpoint: func(ctx context.Context, cp implementation.ContributionCheckpoint) error {
		if cp.RunID != c.RunID || cp.AttemptID != base.Attempt.AttemptID {
			return ErrDenied
		}
		lineage := cp.Lineage()
		if cp.Stage == "build" {
			if err := appendEvent(ctx, contribution.ContinuityEvent{Kind: "reserve", Counter: "candidate", OperationID: lineage}); err != nil {
				return err
			}
		}
		evidence := c.Digest
		if cp.Candidate != "" {
			evidence = cp.Candidate
		}
		return appendEvent(ctx, contribution.ContinuityEvent{Kind: "checkpoint", Stage: cp.Stage, Candidate: lineage, Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: lineage, Digest: evidence}})
	}, Turn: func(ctx context.Context, id string) error {
		return appendEvent(ctx, contribution.ContinuityEvent{Kind: "reserve", Counter: "runtime-turn", OperationID: id})
	}, Role: role, Native: native, StopRole: closeRole, Close: func(ctx context.Context) error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		var result error
		for session := range sessions {
			result = errors.Join(result, closeRole(cleanup, session))
		}
		return result
	}}, nil
}
