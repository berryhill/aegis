package app

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
)

// Direct connect, replacement and composition all use the same one-use native
// review. Native validation returning success does not admit a later effect.
func TestProviderDirectEffectAdmission(t *testing.T) {
	for _, replace := range []bool{false, true} {
		for _, mode := range []string{"subject-expiry", "review-expiry", "cancel", "staged-expiry"} {
			name := mode
			if replace {
				name = "replace-" + name
			}
			t.Run(name, func(t *testing.T) {
				s, sub, _, home, source, agent := setupProviderFixture(t, true)
				now := s.Now().UTC()
				s.Now = func() time.Time { return now }
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
				if replace {
					providerConnect(t, s, sub, agent.Revision.AgentID, home, source.entry)
					source.account = "replacement-account"
				}
				before, err := config.ReadControllerCodexBinding(s.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				review, err := s.PreviewControllerCodexAs(context.Background(), sub, ControllerCodexPreviewInput{ProfileHome: home, CredentialID: source.entry, Replace: replace})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "review-expiry" {
					now = now.Add(4 * time.Minute)
					sub.AuthenticatedAt = now
					sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if mode == "staged-expiry" {
					original := s.Current
					s.Current = func() (*user.User, error) {
						staged, err := filepath.Glob(filepath.Join(filepath.Dir(s.ConfigFile), ".controller-codex-*"))
						if err != nil {
							t.Fatal(err)
						}
						if len(staged) > 0 {
							now = sub.ExpiresAt.Add(time.Second)
						}
						return original()
					}
				} else {
					s.nativeCodexMetadata = &independentDelayedSource{inner: source, after: func() {
						switch mode {
						case "subject-expiry":
							now = sub.ExpiresAt.Add(time.Second)
						case "review-expiry":
							now = review.ExpiresAt.Add(time.Second)
						case "cancel":
							cancel()
						}
					}}
				}
				if _, err = s.ApplyControllerCodexAs(ctx, sub, ControllerCodexApplyInput{ReviewID: review.ReviewID}); err == nil {
					t.Fatal("invalid native effect admitted")
				}
				after, err := config.ReadControllerCodexBinding(s.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				if before == nil {
					if after != nil {
						t.Fatal("denied connect persisted")
					}
				} else if after == nil || *before != *after {
					t.Fatal("denied replacement persisted")
				}
				staged, _ := filepath.Glob(filepath.Join(filepath.Dir(s.ConfigFile), ".controller-codex-*"))
				if len(staged) != 0 {
					t.Fatal("staging leaked")
				}
				s.nativeConnectionMu.Lock()
				_, retained := s.nativeReviews[review.ReviewID]
				s.nativeConnectionMu.Unlock()
				if retained {
					t.Fatal("native review became reusable")
				}
			})
		}
	}
}

// A supported concurrent mutation must finish during source validation, not
// deadlock behind the final effect fence. Both lifecycle and successor writers
// then invalidate the exact pin, including attempts through direct native apply.
func TestProviderConcurrentAgentDrift(t *testing.T) {
	for _, mode := range []string{"lifecycle", "successor"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, home, source, agent := setupProviderFixture(t, true)
			id := agent.Revision.AgentID
			review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "review", ProfileHome: home, CredentialID: source.entry})
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			s.nativeCodexMetadata = &independentDelayedSource{inner: source, after: func() { close(entered); <-release }}
			result := make(chan error, 1)
			go func() {
				_, err := s.ApplyControllerCodexAs(context.Background(), sub, ControllerCodexApplyInput{ReviewID: review.Connection.ReviewID})
				result <- err
			}()
			<-entered
			mutation := make(chan error, 1)
			go func() {
				if mode == "lifecycle" {
					_, err := s.SetAgentLifecycleAs(context.Background(), sub, id, SetAgentLifecycleInput{Expected: agentRevisionRef(agent.Revision), Lifecycle: registry.LifecycleDisabled})
					mutation <- err
					return
				}
				c, err := s.Store.GetCharter(id, 1)
				if err != nil {
					mutation <- err
					return
				}
				c.Charter.Revision++
				c.Charter.Name = "separately approved successor"
				c, err = core.Canonicalize(c.Charter)
				if err == nil {
					err = s.Store.SaveCharter(c)
				}
				if err != nil {
					mutation <- err
					return
				}
				ref := agent.Revision.Charter
				ref.Revision = c.Charter.Revision
				ref.Digest = c.Digest
				_, err = s.ApproveAgentCharterAs(context.Background(), sub, id, ApproveAgentCharterInput{Expected: agentRevisionRef(agent.Revision), Charter: ref})
				mutation <- err
			}()
			select {
			case err := <-mutation:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Agent mutation blocked behind source I/O")
			}
			close(release)
			if err := <-result; !errors.Is(err, ErrConflict) {
				t.Fatal("stale pin admitted", err)
			}
			binding, err := config.ReadControllerCodexBinding(s.ConfigFile)
			if err != nil || binding != nil {
				t.Fatal("stale connection persisted", err)
			}
		})
	}
}

type publicationBoundaryRepository struct {
	fleet.Repository
	before func()
}

func (r *publicationBoundaryRepository) PublishAgentRevision(ctx context.Context, revision registry.AgentRevision, fact fleet.AuditFact, admission ...func() error) error {
	// Run after the real transaction has staged its data/audit but before commit.
	callbacks := []func() error{func() error { r.before(); return nil }}
	callbacks = append(callbacks, admission...)
	return r.Repository.PublishAgentRevision(ctx, revision, fact, callbacks...)
}

func TestProviderSuccessorCommitAdmission(t *testing.T) {
	for _, mode := range []string{"subject-expiry", "review-expiry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, _, _, agent := setupProviderFixture(t, false)
			id := agent.Revision.AgentID
			now := s.Now().UTC()
			s.Now = func() time.Time { return now }
			sub.AuthenticatedAt = now
			sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			// Shorter subject lifetime separates subject expiry from review expiry.
			if mode == "subject-expiry" {
				sub.ExpiresAt = now.Add(time.Minute)
			}
			review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := s.FleetRepository
			s.FleetRepository = &publicationBoundaryRepository{Repository: original, before: func() {
				switch mode {
				case "subject-expiry":
					now = sub.ExpiresAt
				case "review-expiry":
					now = review.Review.ExpiresAt
				case "cancel":
					cancel()
				}
			}}
			input := AgentProviderSetupInput{Action: "approve", ReviewID: review.Review.ID, CharterDigest: review.Review.Charter.Digest}
			if _, err := s.AgentProviderSetupAs(ctx, sub, id, input); err == nil {
				t.Fatal("expired successor committed")
			}
			if _, err := original.GetAgentRevision(context.Background(), id, 2); !errors.Is(err, fleet.ErrNotFound) {
				t.Fatal("Agent not absent", err)
			}
			charter, err := s.Store.GetCharter(id, 2)
			if err != nil || charter.Digest != input.CharterDigest {
				t.Fatal("legitimately admitted charter lost", err)
			}
			// A valid charter-first orphan may recover only under fresh, unexpired
			// consent. Review expiry cannot be rescued by reauthentication.
			s.FleetRepository = original
			sub.AuthenticatedAt = now
			sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			_, err = s.AgentProviderSetupAs(context.Background(), sub, id, input)
			if mode == "review-expiry" {
				if err == nil {
					t.Fatal("expired orphan review recovered")
				}
			} else if err != nil {
				t.Fatal("admitted orphan did not recover", err)
			}
		})
	}
}

func TestProviderRejectsUnadmittedExactOrphan(t *testing.T) {
	s, sub, _, _, _, agent := setupProviderFixture(t, false)
	id := agent.Revision.AgentID
	review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
	if err := s.Store.SaveCharter(review.Review.Charter); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AgentProviderSetupAs(context.Background(), sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: review.Review.ID, CharterDigest: review.Review.Charter.Digest}); !errors.Is(err, ErrConflict) {
		t.Fatal("unadmitted orphan reused", err)
	}
	if _, err := s.FleetRepository.GetAgentRevision(context.Background(), id, 2); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("Agent not absent", err)
	}
}

func TestProviderConsumedReplayDoesNotReadNativeSource(t *testing.T) {
	s, sub, _, home, source, agent := setupProviderFixture(t, true)
	id := agent.Revision.AgentID
	providerConnect(t, s, sub, id, home, source.entry)
	review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "promote", Provider: "openai-codex", Model: "gpt-5.4"})
	input := AgentProviderSetupInput{Action: "approve", ReviewID: review.Review.ID, CharterDigest: review.Review.Charter.Digest}
	first := providerCall(t, s, sub, id, input)
	s.nativeCodexMetadata = &independentDelayedSource{inner: source, after: func() { t.Fatal("consumed history reread live native source") }}
	if err := os.Remove(s.ConfigFile); err != nil {
		t.Fatal(err)
	}
	replay := providerCall(t, s, sub, id, input)
	if replay.Agent.Revision.Digest != first.Agent.Revision.Digest {
		t.Fatal("history changed")
	}
}

func TestProviderCharterStagingAdmission(t *testing.T) {
	for _, mode := range []string{"subject-expiry", "review-expiry", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, _, _, agent := setupProviderFixture(t, false)
			id := agent.Revision.AgentID
			now := s.Now().UTC()
			s.Now = func() time.Time { return now }
			sub.AuthenticatedAt = now
			sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
			if mode == "review-expiry" {
				now = now.Add(4 * time.Minute)
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(s.Config.Principal.AuthTTL)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stagedObserved := false
			s.Now = func() time.Time {
				staged, _ := filepath.Glob(filepath.Join(s.Store.Root(), "charters", id, ".aegis-*"))
				if len(staged) > 0 {
					stagedObserved = true
					switch mode {
					case "subject-expiry":
						now = sub.ExpiresAt
					case "review-expiry":
						now = review.Review.ExpiresAt
					case "cancel":
						cancel()
					}
				}
				return now
			}
			_, err := s.AgentProviderSetupAs(ctx, sub, id, AgentProviderSetupInput{Action: "approve", ReviewID: review.Review.ID, CharterDigest: review.Review.Charter.Digest})
			if err == nil || !stagedObserved {
				t.Fatal("publication guard not exercised", err, stagedObserved)
			}
			if _, err = s.Store.GetCharter(id, 2); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("denied charter persisted", err)
			}
			if _, err = s.FleetRepository.GetAgentRevision(context.Background(), id, 2); !errors.Is(err, fleet.ErrNotFound) {
				t.Fatal("denied Agent persisted", err)
			}
			staged, _ := filepath.Glob(filepath.Join(s.Store.Root(), "charters", id, ".aegis-*"))
			if len(staged) != 0 {
				t.Fatal("denied stage leaked")
			}
		})
	}
}
