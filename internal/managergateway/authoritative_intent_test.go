package managergateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

func degradedRoutingService(now time.Time, subject core.Subject, token string) *Service {
	return &Service{
		app: &app.Service{Config: config.Config{}},
		now: func() time.Time { return now },
		sessions: map[string]session{"degraded": {
			id: "degraded", token: sha256.Sum256([]byte(token)), subject: subject,
			expires: now.Add(time.Hour), mode: "degraded", reason: "manager_model_absent",
		}},
	}
}

func TestGenericAgentRegistrationIntentBypassesModelAuthoritatively(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	for _, input := range []string{
		"register an agent",
		"I want to register a new Agent",
		"Can you register an agent?",
		"Could you register an agent?",
		"Would you register an agent?",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err != nil {
			t.Fatalf("registration intent reached unavailable model for %q: %v", input, err)
		}
		if result.Kind != "agent_registration_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["registered"] != false {
			t.Fatalf("unexpected registration routing: %+v", result)
		}
		for _, required := range []string{"Registration was not performed", "/agents readiness", "/agents prepare", "Hermes profile", "not Agent registration"} {
			if !strings.Contains(result.Message, required) {
				t.Errorf("registration guidance missing %q: %q", required, result.Message)
			}
		}
	}
}

func TestCredentialCreationIntentBypassesModelAndNeverReturnsValue(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	canary := "test-only-canary-value-91d0"

	result, err := service.Turn(context.Background(), subject, "degraded", "token", "create a credential named build-token with a value of "+canary)
	if err != nil {
		t.Fatalf("credential intent reached unavailable model: %v", err)
	}
	if result.Kind != "credential_creation_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["created"] != false {
		t.Fatalf("unexpected credential routing: %+v", result)
	}
	if strings.Contains(result.Message, canary) || strings.Contains(strings.ToLower(result.Message), "created successfully") {
		t.Fatalf("credential value or false mutation escaped: %q", result.Message)
	}
	for _, required := range []string{"Credential creation was not performed", "protected no-echo intake", "unavailable through the gateway turn endpoint"} {
		if !strings.Contains(result.Message, required) {
			t.Errorf("credential guidance missing %q: %q", required, result.Message)
		}
	}
}

func TestPoliteCredentialCreationRequestsBypassModelAuthoritatively(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	for _, input := range []string{
		"Can you create a credential named build-token?",
		"Could you create a credential named build-token?",
		"Would you create a credential named build-token?",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err != nil {
			t.Fatalf("polite credential request reached unavailable model for %q: %v", input, err)
		}
		if result.Kind != "credential_creation_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["reference"] != "build-token" {
			t.Fatalf("unexpected credential routing for %q: %+v", input, result)
		}
	}
}

func TestInlineCredentialCanaryIsAbsentFromRetainedRoutingAndResponse(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	canary := "runtime-input-canary-c97f"
	input := "Could you create a credential named build-token with a value of " + canary + "?"

	route := detectAuthoritativeIntent(input)
	if route.kind != intentCredentialCreate || !route.credentialParsed {
		t.Fatalf("inline credential request was not parsed authoritatively: %+v", route)
	}
	if strings.Contains(route.credential.SafeInput, canary) {
		t.Fatal("inline credential value survived in retained runtime input")
	}
	route.Wipe()
	if route.credential.Value != nil {
		t.Fatal("parsed credential value survived route wipe")
	}

	result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
	if err != nil {
		t.Fatalf("inline credential request reached unavailable model: %v", err)
	}
	if strings.Contains(result.Message, canary) || strings.Contains(fmt.Sprint(result.Data), canary) {
		t.Fatalf("inline credential canary survived response: %+v", result)
	}
}

func TestUnrecognizedInlineCredentialRequestFailsClosedBeforeModel(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	result, err := service.Turn(context.Background(), subject, "degraded", "token", "Could you frobnicate a credential with a value of secret-canary?")
	if err != nil {
		t.Fatalf("possible inline credential create request reached unavailable model: %v", err)
	}
	if result.Kind != "credential_intake_required" || result.Data["model_bypassed"] != true {
		t.Fatalf("possible inline credential request did not fail closed: %+v", result)
	}
}

func TestConversationalIntentDetectionDoesNotMutateQuestions(t *testing.T) {
	for _, input := range []string{"How does Agent registration work?", "How do I register an agent?", "What is a credential?", "Could you explain how to create a credential?"} {
		if got := authoritativeIntent(input); got != "" {
			t.Fatalf("question %q routed as authoritative mutation intent %q", input, got)
		}
	}
}

func TestExplicitCredentialMaterialFailsClosedBeforeRuntime(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	for _, input := range []string{
		"my password is correct-horse-battery-staple",
		"please check api_key=synthetic0123456789",
		"use this access token: synthetic-token-value-12345",
		"Authorization: Bearer synthetic-redacted-value",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err != nil {
			t.Fatalf("detected credential reached unavailable runtime for %q: %v", input, err)
		}
		if result.Kind != "credential_intake_required" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true {
			t.Fatalf("detected credential did not fail closed for %q: %+v", input, result)
		}
	}
}

func TestOrdinaryCredentialQuestionsRemainConversational(t *testing.T) {
	for _, input := range []string{"What is an API key?", "How should I rotate a password?", "Can Aegis store credentials?", "Explain protected intake"} {
		route := detectAuthoritativeIntent(input)
		route.Wipe()
		if route.kind != "" {
			t.Fatalf("ordinary question %q was routed authoritatively as %q", input, route.kind)
		}
	}
}
