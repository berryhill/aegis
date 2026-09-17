package api

import (
	"github.com/berryhill/aegis/internal/managergateway"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestManagerCreatedCredentialVisibleOnFreshConsoleRequest(t *testing.T) {
	f := newCredentialRouteFixture(t)
	read := func() string {
		response, err := f.client.Get("http://" + f.address + "/console/credentials?q=manager-fresh&status=active")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("console status %d", response.StatusCode)
		}
		if response.Header.Get("Cache-Control") == "" {
			t.Fatal("missing cache policy")
		}
		return string(body)
	}
	before := read()
	f.svc.Config.Manager.Inference.Model = "" // guaranteed degraded: no runtime or model invocation
	manager, err := managergateway.New(f.ctx, f.svc)
	if err != nil {
		t.Fatal(err)
	}
	subject := f.principal()
	opened, err := manager.Open(f.ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.TurnWithProtectedIntake(f.ctx, subject, opened.ID, opened.Token, "create a credential", true)
	if err != nil || result.Intake == nil {
		t.Fatalf("begin: %v", err)
	}
	operation := result.Intake.OperationID
	for _, input := range []managergateway.CredentialIntakeRequest{{OperationID: operation, Action: "review", Reference: "manager-fresh", Kind: "opaque"}, {OperationID: operation, Action: "approve"}} {
		if _, err = manager.CredentialIntake(f.ctx, subject, opened.ID, opened.Token, input); err != nil {
			t.Fatal(err)
		}
	}
	result, err = manager.ConsumeCredentialIntake(f.ctx, subject, opened.ID, opened.Token, operation, []byte("synthetic-console-canary"))
	if err != nil || result.Kind != "credential_created" {
		t.Fatalf("create: %+v %v", result, err)
	}
	id := result.Data["record_id"].(string)
	after := read()
	if strings.Contains(before, id) || !strings.Contains(after, id) || strings.Contains(after, "synthetic-console-canary") {
		t.Fatal("fresh console did not reflect metadata-only manager creation")
	}
}
