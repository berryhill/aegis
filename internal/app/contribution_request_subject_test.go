package app_test

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/principalauth"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestContributionRequestSubjectLiveRegistry(t *testing.T) {
	now := time.Now().UTC()
	s := app.New(config.Config{Principal: config.Principal{ID: "owner", AuthTTL: time.Minute}}, nil, nil, nil, nil, slog.Default())
	s.Now = func() time.Time { return now }
	verifier, err := principalauth.Enroll("owner", []byte("fixture-password"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := console.New(console.Config{Origin: "https://console.example.test", SessionTTL: time.Hour, ActionAuthTTL: time.Minute, MaxPageSize: 100, PrincipalID: "owner", PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, s.Now)
	if err != nil {
		t.Fatal(err)
	}
	s.DashboardActionAuthentication = manager.ValidateActionSubject
	p := &app.ContributionDeliveryServerPolicy{Convention: "contribution-publication.v1", Actor: "owner", Author: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, Committer: delivery.Identity{Name: "Fixture", Email: "fixture@example.invalid"}, ProtectionPolicy: "fixture", RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"post"}, MergeMethod: "squash"}
	controller, err := app.NewContributionDeliveryController(s, p, &credentials.Authority{}, delivery.PublicationPrivacy{})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "https://console.example.test/console/login", nil)
	req.Header.Set("Origin", "https://console.example.test")
	var cookies []string
	login := func() core.Subject {
		cookie, _, _, sub, e := manager.Login(req, "fixture", []byte("fixture-password"))
		cookies = append(cookies, cookie)
		if e != nil {
			t.Fatal(e)
		}
		return sub
	}
	sub := login()
	if _, e := s.CurrentContributionDeliverySubject(context.Background()); e == nil {
		t.Fatal("unbound accepted")
	}
	fake := sub
	fake.ID = "password:unissued"
	if _, e := controller.RunAs(context.Background(), fake, "fixture", "session"); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("asserted subject accepted", e)
	}
	for _, fresh := range []core.Subject{sub, login()} {
		if _, e := controller.RunAs(context.Background(), fresh, "fixture", "session"); !errors.Is(e, app.ErrContributionDeliveryUnavailable) {
			t.Fatal("live subject did not reach owner admission", e)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := controller.RunAs(canceled, sub, "fixture", "session"); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("canceled accepted", e)
	}
	for name, mutate := range map[string]func(*core.Subject){
		"foreign":           func(s *core.Subject) { s.PrincipalID = "foreign" },
		"method-mutation":   func(s *core.Subject) { s.Method = "os" },
		"identity-mutation": func(s *core.Subject) { s.ID = "service:forged" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := sub
			mutate(&altered)
			if _, e := controller.RunAs(context.Background(), altered, "fixture", "session"); !errors.Is(e, delivery.ErrDenied) {
				t.Fatalf("altered authority accepted: %v", e)
			}
		})
	}
	manager.RevokeSessionValue(cookies[0])
	if _, e := controller.RunAs(context.Background(), sub, "fixture", "session"); !errors.Is(e, delivery.ErrDenied) {
		t.Fatalf("revoked authority accepted: %v", e)
	}
	sub = login() // Independently issued, unrevoked subject for expiry denial.
	now = now.Add(2 * time.Minute)
	if _, e := controller.RunAs(context.Background(), sub, "fixture", "session"); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("expired accepted", e)
	}
}
