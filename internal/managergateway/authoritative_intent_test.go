package managergateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
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

type gatewayReadOperations struct {
	managerdomain.Operations
	records []credentials.SecretRecord
	value   []byte
}

type emptyAgentRegistry struct{ fleet.Repository }

func (emptyAgentRegistry) ListAgentRegistrations(context.Context) ([]registry.AgentRegistration, error) {
	return nil, nil
}

func (o gatewayReadOperations) List(_ context.Context, query string, _ int) ([]credentials.SecretRecord, error) {
	if query != "" && !strings.Contains(o.records[0].Reference, query) {
		return nil, nil
	}
	return o.records, nil
}

func (o gatewayReadOperations) Counts(context.Context) (credentials.SecretCounts, error) {
	return credentials.SecretCounts{Total: len(o.records), Active: len(o.records)}, nil
}

func (o gatewayReadOperations) ReadValue(_ context.Context, reference string, consume func(credentials.SecretRecord, []byte) error) error {
	if len(o.records) != 1 || o.records[0].Reference != reference {
		return credentials.ErrNotFound
	}
	return consume(o.records[0], o.value)
}

func TestCredentialReadsBypassModelThroughAuthorityWhileRuntimeDegraded(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	canary := []byte("gateway-value-canary")
	operations := gatewayReadOperations{records: []credentials.SecretRecord{{ID: "secret-one", Reference: "build-token", Kind: "opaque", Status: credentials.StatusActive, CurrentVersion: 1}}, value: canary}
	service.readOperations = func(got core.Subject) managerdomain.Operations {
		if got.ID != subject.ID || got.PrincipalID != subject.PrincipalID {
			t.Fatalf("credential read lost authenticated subject: %+v", got)
		}
		return operations
	}

	for _, test := range []struct {
		input, kind, contains string
	}{
		{input: "how many secrets do I have?", kind: "credential_count", contains: "Credential inventory"},
		{input: "what secrets do I have?", kind: "credential_list", contains: "Credentials (1)"},
		{input: "find credentials matching build", kind: "credential_search", contains: `Credentials matching "build" (1)`},
		{input: "show the value for credential build-token", kind: "credential_value", contains: string(canary)},
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", test.input)
		if err != nil {
			t.Fatalf("credential read %q: %v", test.input, err)
		}
		wantSensitive := test.kind == "credential_value"
		if result.Kind != test.kind || result.Origin != TurnOriginAuthoritative || result.Sensitive != wantSensitive || result.Data["model_bypassed"] != true || !strings.Contains(result.Message, test.contains) {
			t.Fatalf("credential read %q returned %+v", test.input, result)
		}
		if strings.Contains(fmt.Sprint(result.Data), string(canary)) {
			t.Fatalf("credential value entered structured turn data: %+v", result.Data)
		}
	}
}

func TestObservedAgentCountTypoReadsRegistryWithoutModel(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	service.app.Config.Principal.ID = subject.PrincipalID
	service.app.Now = func() time.Time { return now }
	service.app.FleetRepository = emptyAgentRegistry{}
	service.app.Fleet = &orchestration.FleetService{}
	service.app.QueueWorker = &orchestration.QueueWorker{}

	result, err := service.Turn(context.Background(), subject, "degraded", "token", "how many agents are resistered?")
	if err != nil {
		t.Fatalf("observed count typo reached unavailable model: %v", err)
	}
	if result.Kind != "agent_registry_count" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["count"] != 0 {
		t.Fatalf("observed count typo returned %+v", result)
	}
}

func TestExactOperatorTranscriptsBypassConversationalRuntime(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	service.sessions["degraded"] = session{
		id: "degraded", token: sha256.Sum256([]byte("token")), subject: subject,
		expires: now.Add(time.Hour), mode: "conversational",
	}
	service.app.Config.Principal.ID = subject.PrincipalID
	service.app.Now = func() time.Time { return now }
	service.app.FleetRepository = emptyAgentRegistry{}
	service.app.Fleet = &orchestration.FleetService{}
	service.app.QueueWorker = &orchestration.QueueWorker{}

	for _, test := range []struct {
		input string
		kind  string
	}{
		{input: "can we register an agent?", kind: "agent_registration_guidance"},
		{input: "how many agents are resistered?", kind: "agent_registry_count"},
		{input: "can you ensure our aegis gateway and dashboard are up to date?", kind: "manager_lifecycle_guidance"},
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", test.input)
		if err != nil {
			t.Fatalf("conversational transcript %q reached nil runtime: %v", test.input, err)
		}
		if result.Kind != test.kind || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true {
			t.Fatalf("conversational transcript %q returned %+v", test.input, result)
		}
	}
}

func TestAmbiguousCredentialFollowUpDoesNotRepeatAuthorityRead(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	service.readOperations = func(core.Subject) managerdomain.Operations {
		t.Fatal("ambiguous follow-up reached credential authority")
		return nil
	}
	_, err := service.Turn(context.Background(), subject, "degraded", "token", "what about now?")
	if err == nil {
		t.Fatal("ambiguous follow-up should require the unavailable conversational runtime")
	}
}

func TestQualifiedAgentRegistrationIntentReturnsGuidanceWithoutModel(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	result, err := service.Turn(context.Background(), subject, "degraded", "token", "register agent alpha")
	if err != nil {
		t.Fatalf("qualified registration intent reached unavailable model: %v", err)
	}
	if result.Kind != "agent_registration_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["registered"] != false {
		t.Fatalf("unexpected registration routing: %+v", result)
	}
}

func TestRealWorldRegistrationTranscriptsRecommendTypedControlsWithoutMutation(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	for _, input := range []string{
		"I want to register an agent",
		"hey, let's register an agent",
		"can we register an agent?",
		"I'd like to register a new agent.",
		"I’d like to register a new agent.",
		"Can you register agent alpha?",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err != nil {
			t.Fatalf("registration transcript %q reached unavailable model: %v", input, err)
		}
		if result.Kind != "agent_registration_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["registered"] != false || result.Data["prepared"] != false || result.Data["activated"] != false {
			t.Fatalf("registration transcript %q returned %+v", input, result)
		}
		for _, forbidden := range []string{"Prepared a non-authorizing import review", "registered successfully", "activated"} {
			if strings.Contains(result.Message, forbidden) {
				t.Fatalf("registration transcript %q made a mutation claim: %q", input, result.Message)
			}
		}
		for _, required := range []string{"/agents readiness", "/agents prepare", "/agents register"} {
			if !strings.Contains(result.Message, required) {
				t.Errorf("registration transcript %q missing %q: %q", input, required, result.Message)
			}
		}
	}
}

func TestManagerLifecycleRequestsReturnNoMutationTypedGuidance(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	for _, input := range []string{
		"can you ensure our aegis gateway and dashboard are up to date?",
		"Please ensure the Aegis manager is running and up to date.",
		"Please update and restart the Aegis manager.",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err != nil {
			t.Fatalf("lifecycle transcript %q reached unavailable model: %v", input, err)
		}
		if result.Kind != "manager_lifecycle_guidance" || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true || result.Data["mutated"] != false || result.Data["checked"] != false || result.Data["restarted"] != false || result.Data["updated"] != false {
			t.Fatalf("lifecycle transcript %q returned %+v", input, result)
		}
		for _, required := range []string{"aegis version --provenance", "aegis update --check", "aegis gateway status", "aegis console", "aegis update", "aegis gateway restart", "No status check, update, or restart was performed"} {
			if !strings.Contains(result.Message, required) {
				t.Errorf("lifecycle transcript %q missing %q: %q", input, required, result.Message)
			}
		}
		for _, forbidden := range []string{"is running", "is current", "is up to date", "was restarted", "successfully updated"} {
			if strings.Contains(strings.ToLower(result.Message), forbidden) {
				t.Errorf("lifecycle transcript %q contains false claim %q: %q", input, forbidden, result.Message)
			}
		}
	}
}

func TestRealWorldAuthoritativeRoutingRejectsOvermatch(t *testing.T) {
	for _, input := range []string{
		"Do not ensure the Aegis manager is running and up to date.",
		"Say 'Please update and restart the Aegis manager.'",
		"Please update the Aegis manager and delete its state.",
		"Please ensure the Aegis manager is running\nand up to date.",
		"can you \nensure our aegis gateway and dashboard are up to date?",
		"can we \nregister an agent?",
		"\"can you ensure our aegis gateway and dashboard are up to date?\"",
		"\"how many agents are resistered?\"",
		"“can you ensure our aegis gateway and dashboard are up to date?”",
		"“how many agents are resistered?”",
		"“register the default Hermes profile on this computer”",
		"\"register the default Hermes profile on this computer\".",
		"“Please update and restart the Aegis manager”.",
		"“how many agents are resistered?”.",
		"“how many agents are resistered?\"",
		"Please update and restart the Aegis manager\",",
		"(“Please update and restart the Aegis manager.”)",
		"(\"how many agents are resistered?\").",
		"„Please update and restart the Aegis manager.‟",
		"「register the default Hermes profile on this computer」",
		"`how many agents are resistered?`",
		"〝can we register an agent?〞",
		"Please ensure the Aegis managers are running and up to date.",
	} {
		if IsDeterministicRequest(input) {
			t.Fatalf("unsafe near-match routed authoritatively: %q", input)
		}
	}
}

func TestAgentRegistrationGuidanceRejectsCompoundQuotedAndNegatedTextEndToEnd(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")
	for _, input := range []string{
		"hey,\nlet's register an agent",
		"do not register an agent",
		"review this sentence: add an agent",
		"say 'register agent alpha'",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", input)
		if err == nil || result.Kind == "agent_registration_guidance" {
			t.Fatalf("unsafe registration text reached authoritative route: input=%q result=%+v err=%v", input, result, err)
		}
	}
}

func TestAegisSelfExpertiseQuestionsBypassModelAuthoritatively(t *testing.T) {
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	service := degradedRoutingService(now, subject, "token")

	for _, test := range []struct {
		input, kind string
		required    []string
		falseField  string
	}{
		{input: "how do I install the Aegis skills in Hermes?", kind: "aegis_skills_installation_guidance", required: []string{"hermes skills tap add berryhill/aegis", "hermes skills install berryhill/aegis/skills/aegis --yes", "does not register an Agent"}, falseField: "mutated"},
		{input: "does our registers hermes agent know how to use aegis/", kind: "registered_agent_expertise_guidance", required: []string{"Registry record", "does not import", "skills, prompts, memories", "manager receives"}, falseField: "registration_imports_profile_contents"},
		{input: "can you change the name of the default Hermes agent we registered to javi?", kind: "agent_rename_guidance", required: []string{"canonical Agent ID is immutable", "not renamed", "display-name or alias operation is not shipped"}, falseField: "renamed"},
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", "token", test.input)
		if err != nil {
			t.Fatalf("self-expertise question %q reached unavailable model: %v", test.input, err)
		}
		if result.Kind != test.kind || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true {
			t.Fatalf("unexpected self-expertise routing for %q: %+v", test.input, result)
		}
		if value, present := result.Data[test.falseField]; !present || value != false {
			t.Fatalf("self-expertise route %q did not prove %s=false: %+v", test.input, test.falseField, result.Data)
		}
		for _, required := range test.required {
			if !strings.Contains(result.Message, required) {
				t.Errorf("%q response missing %q: %q", test.input, required, result.Message)
			}
		}
	}
}

func TestManagerSessionExpiryUsesPrincipalBoundaryNotBrowserConsoleTTL(t *testing.T) {
	now := time.Date(2026, 9, 2, 17, 0, 0, 0, time.UTC)
	subject := core.Subject{ExpiresAt: now.Add(15 * time.Minute)}
	if got := managerSessionExpiry(now, subject); !got.Equal(subject.ExpiresAt) {
		t.Fatalf("manager expiry=%s want principal expiry=%s", got, subject.ExpiresAt)
	}
}

func TestManagerSessionContextEndsAtMandateBoundary(t *testing.T) {
	expires := time.Now().Add(20 * time.Millisecond)
	ctx, cancel := managerSessionContext(context.Background(), expires)
	defer cancel()
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("session context ended with %v", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("session context outlived mandate boundary")
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
