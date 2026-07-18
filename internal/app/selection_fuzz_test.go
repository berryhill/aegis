package app

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func FuzzDeterministicSelectionNeverUnions(f *testing.F) {
	f.Add("local-uid:4242", "principal-1", "local-os", "local-os", "local", "")
	f.Add("team-user", "", "local-os", "local-os", "local", "teamwide")
	f.Add("attacker", "", "prompt", "model-output", "production", "principal")
	f.Fuzz(func(t *testing.T, subjectID, principalID, issuer, method, environment, requested string) {
		now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
		charter, err := core.Canonicalize(testCharter(now))
		if err != nil {
			t.Fatal(err)
		}
		service := &Service{Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		subject := core.Subject{ID: subjectID, Kind: "human", PrincipalID: principalID, Issuer: issuer, Method: method, AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)}
		decision, selectionErr := service.Select(charter, subject, requested, core.Environment{Name: environment})
		if selectionErr == nil {
			if !decision.Allowed || decision.Selected == nil || decision.MatchingCount != 1 {
				t.Fatalf("successful selection was not exact: %+v", decision)
			}
			selected := decision.Selected
			if selected.ID == "principal" && (contains(selected.Grant.Tools, "web") || contains(selected.Scopes.Memory, "team-memory")) {
				t.Fatal("principal selection unioned team authority")
			}
			if selected.ID == "teamwide" && (contains(selected.Grant.Tools, "no_mcp") || contains(selected.Scopes.Memory, "principal-memory")) {
				t.Fatal("team selection unioned principal authority")
			}
			return
		}
		if decision.Allowed || decision.Selected != nil {
			t.Fatalf("denial exposed selected authority: %+v err=%v", decision, selectionErr)
		}
	})
}
