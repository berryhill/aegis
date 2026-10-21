package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"path/filepath"
	"testing"
	"time"
)

func TestQueuePresentationAuthenticatedPreferences(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo, err := fleetbadger.Open(ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	svc, subject := graphReadService(repo, now)
	subject.AuthenticatedAt = now
	subject.Method = "password"
	got, err := svc.GetQueuePreferencesAs(ctx, subject)
	if err != nil || got.Revision != 0 || got.Selection != queue.SelectionSucceeded || got.Visibility != queue.VisibilityUnarchived {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	input := SetQueuePreferencesInput{RequestID: "preferences", Selection: queue.SelectionAll, Visibility: queue.VisibilityArchived}
	saved, created, err := svc.SetQueuePreferencesAs(ctx, subject, input)
	if err != nil || !created || saved.PrincipalID != subject.PrincipalID || saved.SubjectID != subject.ID {
		t.Fatalf("save: %+v %v %v", saved, created, err)
	}
	if replay, created, err := svc.SetQueuePreferencesAs(ctx, subject, input); err != nil || created || replay != saved {
		t.Fatalf("replay: %+v %v %v", replay, created, err)
	}
	for _, bad := range []core.Subject{
		{}, {ID: subject.ID, PrincipalID: "foreign", AuthenticatedAt: now, ExpiresAt: subject.ExpiresAt},
		{ID: subject.ID, PrincipalID: subject.PrincipalID, AuthenticatedAt: now, ExpiresAt: now},
		{ID: subject.ID, PrincipalID: subject.PrincipalID, ExpiresAt: subject.ExpiresAt},
		{ID: subject.ID, PrincipalID: subject.PrincipalID, AuthenticatedAt: now.Add(time.Second), ExpiresAt: subject.ExpiresAt},
		{ID: subject.ID, PrincipalID: subject.PrincipalID, Method: "password", AuthenticatedAt: now.Add(-svc.Config.Principal.AuthTTL), ExpiresAt: subject.ExpiresAt},
	} {
		if _, _, err := svc.SetQueuePreferencesAs(ctx, bad, input); !errors.Is(err, ErrDenied) {
			t.Fatalf("unauthenticated preferences: %v", err)
		}
		if _, _, err := svc.SetQueueArchiveAs(ctx, bad, SetQueueArchiveInput{}); !errors.Is(err, ErrDenied) {
			t.Fatalf("unauthenticated archive: %v", err)
		}
	}
	input.RequestID = "invalid"
	input.Selection = "unknown"
	if _, _, err := svc.SetQueuePreferencesAs(ctx, subject, input); err == nil {
		t.Fatal("invalid selection accepted")
	}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "missing", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if _, err := svc.GetQueueArchiveAs(ctx, subject, ref); !errors.Is(err, fleet.ErrPresentationDenied) {
		t.Fatalf("missing queue identity hidden as default: %v", err)
	}
	events, err := repo.AuditEvents(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("denial mutated audit: %d %v", len(events), err)
	}
}
