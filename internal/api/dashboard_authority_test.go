package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/principalauth"
)

// A real browser session may authorize dashboard control at 1h, but neither its
// identity nor live validator may be laundered into general principal authority.
func TestDashboardAuthenticationDoesNotWidenSharedAuthority(t *testing.T) {
	svc := apiService(t)
	start := svc.Now()
	now := start
	svc.Now = func() time.Time { return now }
	verifier, err := principalauth.Enroll(svc.Config.Principal.ID, []byte("dashboard-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := console.New(console.Config{Origin: "https://console.example.test", SessionTTL: 6 * time.Hour, ActionAuthTTL: 6 * time.Hour, MaxPageSize: 100, PrincipalID: svc.Config.Principal.ID, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	svc.DashboardActionAuthentication = manager.ValidateActionSubject
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	request.Header.Set("Origin", "https://console.example.test")
	value, csrf, _, subject, err := manager.Login(request, "client", []byte("dashboard-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	// A separately authenticated native principal remains fresh for shared
	// protected decisions, but cannot impersonate an issued browser identity.
	independent, err := console.New(console.Config{Origin: "https://console.example.test", SessionTTL: time.Minute, ActionAuthTTL: time.Minute, MaxPageSize: 100, PrincipalID: svc.Config.Principal.ID, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, native, err := independent.Login(request, "native-client", []byte("dashboard-test-password"))
	if err != nil {
		t.Fatal(err)
	}
	if svc.RequirePrincipal(native) != nil {
		t.Fatal("fresh independent protected principal denied")
	}
	if svc.RequireDashboardPrincipal(native) == nil {
		t.Fatal("foreign issued password identity entered browser registry")
	}
	request.AddCookie(&http.Cookie{Name: console.CookieName, Value: value})
	request.Header.Set("X-CSRF-Token", csrf)
	now = start.Add(time.Hour)
	admitted, err := manager.AuthorizeMutation(request)
	if err != nil {
		t.Fatal(err)
	}
	if svc.RequireDashboardPrincipal(admitted) != nil {
		t.Fatal("fresh dashboard control denied")
	}
	if !errors.Is(svc.RequirePrincipal(admitted), app.ErrDenied) {
		t.Fatal("dashboard identity widened shared principal authority")
	}
	for _, action := range []orchestration.FleetAction{orchestration.FleetActionDirectLoopRuntime, orchestration.FleetActionClaim, orchestration.FleetActionReclaim, orchestration.FleetActionRuntimeEffect, orchestration.FleetActionEvidenceVerify, orchestration.FleetActionDisposition, "unknown"} {
		if !errors.Is(svc.AuthorizeFleetAction(t.Context(), action, admitted), app.ErrDenied) {
			t.Fatalf("dashboard widened unrelated %s authority", action)
		}
	}
	if err := svc.AuthorizeFleetAction(t.Context(), orchestration.FleetActionLoopPublish, admitted); err != nil {
		t.Fatal("dashboard publish authentication denied")
	}
	if _, _, err := svc.PreviewSessionAs(t.Context(), admitted, "unavailable", 1, "principal", core.Environment{}); !errors.Is(err, app.ErrDenied) {
		t.Fatal("stale dashboard identity crossed mandate admission")
	}
	if _, err := svc.ApproveDoerHostWriteAs(t.Context(), admitted, app.DoerHostApprovalInput{Decision: "approve-host-write"}); !errors.Is(err, app.ErrDenied) {
		t.Fatal("stale dashboard identity crossed host-write approval")
	}
	if _, err := svc.PreviewPlanAs(t.Context(), admitted, "unavailable", 1, core.Environment{}); !errors.Is(err, app.ErrDenied) {
		t.Fatal("stale dashboard identity crossed provisioning plan admission")
	}
	if _, err := svc.RequestApprovalAs(t.Context(), admitted, "unavailable", time.Minute); !errors.Is(err, app.ErrDenied) {
		t.Fatal("stale dashboard identity crossed provisioning approval")
	}
	if _, err := svc.ApplyAs(t.Context(), admitted, "unavailable", "unavailable"); !errors.Is(err, app.ErrDenied) {
		t.Fatal("stale dashboard identity crossed provisioning effect admission")
	}
	manager.RevokeSessionValue(value)
	if svc.RequireDashboardPrincipal(subject) == nil {
		t.Fatal("revoked captured subject authorized application effect")
	}
}
