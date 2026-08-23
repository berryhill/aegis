package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
)

const (
	loopPublishCommandID   = "loop.publish"
	loopLifecycleCommandID = "loop.lifecycle"
	loopHeadTargetType     = "loop-head"
	loopRevisionTargetType = "loop-revision"
)

type loopPublishCommandInput struct {
	PublisherID            string            `json:"publisher_id"`
	Revision               app.LoopCandidate `json:"revision"`
	ExpectedPreviousDigest string            `json:"expected_previous_digest,omitempty"`
	PublicationKey         string            `json:"publication_key"`
}

type loopLifecycleCommandInput struct {
	PublisherID            string                 `json:"publisher_id"`
	State                  app.LoopLifecycleState `json:"state"`
	ExpectedPreviousDigest string                 `json:"expected_previous_digest,omitempty"`
	IdempotencyKey         string                 `json:"idempotency_key"`
}

func loopLifecycleEventID(key string) string {
	sum := sha256.Sum256([]byte("aegis.console.loop-lifecycle\x00" + key))
	return "loop-event-" + hex.EncodeToString(sum[:])
}

func emptyLoopHeadDigest(id string) string {
	sum := sha256.Sum256([]byte("aegis.console.loop-head.empty\x00" + id))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func loopRevisionTargetID(loopID string, revision uint64) string {
	return strconv.FormatUint(revision, 10) + ":" + loopID
}

func parseLoopRevisionTargetID(id string) (string, uint64, error) {
	rawRevision, loopID, ok := strings.Cut(id, ":")
	if !ok || loopID == "" {
		return "", 0, console.ErrInvalidInput
	}
	revision, err := strconv.ParseUint(rawRevision, 10, 64)
	if err != nil || revision == 0 {
		return "", 0, console.ErrInvalidInput
	}
	return loopID, revision, nil
}

func loopCommandDefinitions(svc *app.Service) []console.CommandDefinition {
	publish := console.CommandDefinition{
		ID: loopPublishCommandID, Version: "v1", TargetType: loopHeadTargetType,
		AuthorityRequirement: "fleet.loop.publish", ConfirmationClass: "create-immutable",
		ReplayPolicy: "exactly-once-readback", ResultType: "loop.publication-receipt",
		StableReasonCodes: []string{"loop_revision_published", "loop_revision_replayed"},
		Timeout:           15 * time.Second, MaxBodyBytes: console.CommandBodyBytesMax,
	}
	publish.Normalize = func(raw json.RawMessage) ([]byte, error) {
		var input loopPublishCommandInput
		if err := console.DecodeCommandRequest(raw, &input); err != nil || input.PublisherID == "" || input.PublicationKey == "" || input.Revision.LoopID == "" {
			return nil, console.ErrInvalidInput
		}
		// NewRevision is the bounded typed validator. Store only the canonical
		// candidate without its derived digest; PublishLoopAs repeats validation.
		revision, validation, err := app.NewLoopRevision(input.Revision)
		if err != nil || validation.Outcome != app.LoopValidationValid {
			return nil, console.ErrInvalidInput
		}
		revision.Digest = ""
		revision.Validator = app.LoopValidatorSpec{}
		input.Revision = revision
		return json.Marshal(input)
	}
	publish.ResolveTarget = func(ctx context.Context, id string) (console.CommandTargetState, error) {
		// Target resolution is principal-independent readback; final command
		// admission and the application mutation both repeat principal checks.
		views, err := svc.FleetRepository.ListLoopRevisions(ctx)
		if err != nil {
			return console.CommandTargetState{}, err
		}
		digest := emptyLoopHeadDigest(id)
		var revision uint64
		for _, value := range views {
			if value.LoopID == id && value.Revision > revision {
				revision, digest = value.Revision, value.Digest
			}
		}
		return console.CommandTargetState{ID: id, Type: loopHeadTargetType, Digest: digest}, nil
	}
	publish.Commit = func(ctx context.Context, invocation console.CommandInvocation) (console.CommandReceipt, error) {
		var input loopPublishCommandInput
		if err := console.DecodeCommandRequest(invocation.NormalizedInput, &input); err != nil {
			return console.CommandReceipt{}, err
		}
		binding, err := svc.FleetCommandAuthorityAs(ctx, invocation.Subject)
		if err != nil || binding.Authority.ID != invocation.Authority.AuthorityID || binding.Authority.Digest != invocation.Authority.AuthorityDigest || binding.Publisher.ID != input.PublisherID {
			return console.CommandReceipt{}, console.ErrDenied
		}
		published, err := svc.PublishLoopAs(ctx, invocation.Subject, app.PublishLoopInput{
			Authority: binding.Authority, Publisher: binding.Publisher, Revision: input.Revision,
			ExpectedPreviousDigest: input.ExpectedPreviousDigest, IdempotencyKey: input.PublicationKey,
		})
		if err != nil {
			return console.CommandReceipt{}, err
		}
		view, err := svc.GetLoopViewAs(ctx, invocation.Subject, published.Revision.LoopID, published.Revision.Revision)
		if err != nil {
			return console.CommandReceipt{}, err
		}
		readback, err := json.Marshal(struct {
			Published app.PublishedLoop `json:"published"`
			View      app.LoopView      `json:"view"`
		}{published, view})
		if err != nil {
			return console.CommandReceipt{}, err
		}
		reason := "loop_revision_published"
		if published.Decision.Idempotent {
			reason = "loop_revision_replayed"
		}
		return console.CommandReceipt{SchemaVersion: console.CommandCatalogVersion, IntentID: invocation.IntentID, CommandID: invocation.CommandID, Target: invocation.Target, Outcome: "committed", ReasonCode: reason, CommittedAt: svc.Now().UTC(), Readback: readback}, nil
	}

	lifecycle := console.CommandDefinition{
		ID: loopLifecycleCommandID, Version: "v1", TargetType: loopRevisionTargetType,
		AuthorityRequirement: "fleet.loop.lifecycle", ConfirmationClass: "lifecycle",
		ReplayPolicy: "exactly-once-readback", ResultType: "loop.lifecycle-receipt",
		StableReasonCodes: []string{"loop_activated", "loop_retired", "loop_lifecycle_replayed"},
		Timeout:           15 * time.Second, MaxBodyBytes: 4096,
	}
	lifecycle.Normalize = func(raw json.RawMessage) ([]byte, error) {
		var input loopLifecycleCommandInput
		if err := console.DecodeCommandRequest(raw, &input); err != nil || input.PublisherID == "" || input.IdempotencyKey == "" || input.State != app.LoopLifecycleActive && input.State != app.LoopLifecycleRetired {
			return nil, console.ErrInvalidInput
		}
		return json.Marshal(input)
	}
	lifecycle.ResolveTarget = func(ctx context.Context, id string) (console.CommandTargetState, error) {
		loopID, revision, err := parseLoopRevisionTargetID(id)
		if err != nil {
			return console.CommandTargetState{}, console.ErrInvalidInput
		}
		value, err := svc.FleetRepository.GetLoopRevision(ctx, loopID, revision)
		if err != nil {
			return console.CommandTargetState{}, err
		}
		return console.CommandTargetState{ID: id, Type: loopRevisionTargetType, Digest: value.Digest}, nil
	}
	lifecycle.Commit = func(ctx context.Context, invocation console.CommandInvocation) (console.CommandReceipt, error) {
		var input loopLifecycleCommandInput
		if err := console.DecodeCommandRequest(invocation.NormalizedInput, &input); err != nil {
			return console.CommandReceipt{}, err
		}
		binding, err := svc.FleetCommandAuthorityAs(ctx, invocation.Subject)
		if err != nil || binding.Authority.ID != invocation.Authority.AuthorityID || binding.Authority.Digest != invocation.Authority.AuthorityDigest || binding.Publisher.ID != input.PublisherID {
			return console.CommandReceipt{}, console.ErrDenied
		}
		loopID, revision, err := parseLoopRevisionTargetID(invocation.Target.ID)
		if err != nil {
			return console.CommandReceipt{}, console.ErrDenied
		}
		result, err := svc.SetLoopLifecycleAs(ctx, invocation.Subject, loopID, app.SetLoopLifecycleInput{
			Authority: binding.Authority, Publisher: binding.Publisher,
			Loop: app.RevisionReference(loopID, revision, invocation.Target.Digest), State: input.State,
			EventID: loopLifecycleEventID(input.IdempotencyKey), ExpectedPreviousDigest: input.ExpectedPreviousDigest,
		})
		if err != nil {
			return console.CommandReceipt{}, err
		}
		view, err := svc.GetLoopViewAs(ctx, invocation.Subject, loopID, revision)
		if err != nil {
			return console.CommandReceipt{}, err
		}
		readback, err := json.Marshal(struct {
			Lifecycle app.LoopLifecycleResult `json:"lifecycle"`
			View      app.LoopView            `json:"view"`
		}{result, view})
		if err != nil {
			return console.CommandReceipt{}, err
		}
		reason := "loop_retired"
		if input.State == app.LoopLifecycleActive {
			reason = "loop_activated"
		}
		if result.Idempotent {
			reason = "loop_lifecycle_replayed"
		}
		return console.CommandReceipt{SchemaVersion: console.CommandCatalogVersion, IntentID: invocation.IntentID, CommandID: invocation.CommandID, Target: invocation.Target, Outcome: "committed", ReasonCode: reason, CommittedAt: svc.Now().UTC(), Readback: readback}, nil
	}
	return []console.CommandDefinition{publish, lifecycle}
}

func loopCommandAuthorityProvider(svc *app.Service) console.CommandAuthorityProvider {
	return console.CommandAuthorityProviderFunc(func(ctx context.Context, subject core.Subject, sessionID, requirement string) (console.CommandAuthorityBinding, error) {
		if requirement != "fleet.loop.publish" && requirement != "fleet.loop.lifecycle" {
			return console.CommandAuthorityBinding{}, console.ErrDenied
		}
		binding, err := svc.FleetCommandAuthorityAs(ctx, subject)
		if err != nil {
			if errors.Is(err, app.ErrDenied) || app.IsFleetDenied(err) {
				return console.CommandAuthorityBinding{}, console.ErrDenied
			}
			return console.CommandAuthorityBinding{}, err
		}
		if binding.ExpiresAt.IsZero() || binding.Publisher.ID == "" {
			return console.CommandAuthorityBinding{}, fmt.Errorf("%w: incomplete fleet command authority", console.ErrDenied)
		}
		return console.CommandAuthorityBinding{
			SubjectID: subject.ID, SessionID: sessionID, StanzaID: binding.StanzaID, MandateID: binding.MandateID,
			AuthorityID: binding.Authority.ID, AuthorityDigest: binding.Authority.Digest, Runtime: binding.Runtime, ExpiresAt: binding.ExpiresAt,
		}, nil
	})
}
