package manager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validModel() ModelIdentity {
	return ModelIdentity{Registry: "ollama", Name: "model:1", Digest: "sha256:" + strings.Repeat("a", 64), ContextLength: 65536, TemplateIdentity: "ollama-template-v1", InstructionVersion: InstructionVersion, SchemaVersion: ResponseSchemaVersion, OllamaVersion: "0.32.0", HermesVersion: "0.18.2", ConformanceVersion: ConformanceVersion, Certified: true, CertifiedAt: time.Unix(10, 0).UTC()}
}

func TestDecodeResponseStrict(t *testing.T) {
	valid := `{"schema_version":"aegis.manager.response.v1","kind":"proposal","message":"List metadata","proposal":{"operation":"secret.list","arguments":{"limit":10}}}`
	response, arguments, err := DecodeResponse([]byte(valid), 4096)
	if err != nil || response.Kind != "proposal" || arguments.(*PageArguments).Limit != 10 {
		t.Fatalf("decode: %#v %#v %v", response, arguments, err)
	}
	bad := []string{
		`{"schema_version":"aegis.manager.response.v1","schema_version":"aegis.manager.response.v1","kind":"message","message":"x","proposal":null}`,
		`{"schema_version":"aegis.manager.response.v1","kind":"message","message":"x","proposal":null} {}`,
		`{"schema_version":"aegis.manager.response.v1","kind":"message","message":"x","proposal":null,"unknown":true}`,
		`{"schema_version":"aegis.manager.response.v1","kind":"proposal","message":"x","proposal":{"operation":"shell.exec","arguments":{}}}`,
		`{"schema_version":"aegis.manager.response.v1","kind":"message","message":"x","proposal":{"operation":"session.exit","arguments":{}}}`,
	}
	for _, input := range bad {
		if _, _, err := DecodeResponse([]byte(input), 4096); err == nil {
			t.Fatalf("unsafe response accepted: %s", input)
		}
	}
}

func TestSystemInstructionDefinesStrictEnvelopeAndOperations(t *testing.T) {
	for _, required := range []string{
		`exactly these four keys`,
		`"schema_version":"aegis.manager.response.v1"`,
		`Use kind "message" with proposal null`,
		`Use kind "proposal" with proposal`,
		`answer the user's actual message directly and naturally`,
		`Never substitute a generic acknowledgement`,
		`trusted plaintext conversational component`,
		`Accept credential values supplied by the authenticated principal`,
		`purged with the disposable runtime`,
		`secret.propose_create`,
		`Never include a credential value`,
		`disclosure "protected"`,
		`Return no markdown fence`,
	} {
		if !strings.Contains(SystemInstruction, required) {
			t.Fatalf("system instruction omits %q", required)
		}
	}
	if strings.Contains(SystemInstruction, "Acknowledged safely.") {
		t.Fatal("system instruction retains a canned ordinary response")
	}
	if strings.Contains(SystemInstruction, `disclosure "none"`) {
		t.Fatal("system instruction conflicts with create validation disclosure")
	}
}

func TestRouteDigestDeterministicAndFailClosed(t *testing.T) {
	plan := RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: LogicalAgentID, SecurityContext: SecurityContext, HermesPath: "/opt/hermes", HermesVersion: "0.18.2", OllamaMode: "managed", OllamaEndpoint: "http://127.0.0.1:1234", OllamaVersion: "0.32.0", Model: validModel(), ProxyIdentity: "proxy-1", IssuedAt: time.Unix(10, 0).UTC(), ExpiresAt: time.Unix(20, 0).UTC()}
	one, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := plan.Digest()
	if err != nil || one != two {
		t.Fatalf("digest changed: %s %s %v", one, two, err)
	}
	plan.Fallback = true
	if _, err := plan.Digest(); err == nil {
		t.Fatal("fallback route accepted")
	}
}

func TestGuardBlocksSecretsAndFailures(t *testing.T) {
	guard, err := NewGuard(4096, 4096, 2, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	envelope := ContentEnvelope{Source: SourceUser, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, RouteClass: "local", Content: []byte("hello")}
	if finding := guard.Inspect(context.Background(), envelope); finding.Decision != AllowLocal {
		t.Fatalf("benign blocked: %#v", finding)
	}
	envelope.Content = []byte("first benign line\nsecond benign line")
	if finding := guard.Inspect(context.Background(), envelope); finding.Decision != AllowLocal {
		t.Fatalf("benign multiline input blocked: %#v", finding)
	}
	cases := []string{"Authorization: Bearer abcdefghijklmnopqrstuvwxyz", "password=correct-horse-battery-staple", "postgres://user:password@localhost/db", "-----BEGIN PRIVATE KEY-----", "token=QUtJQUFCQ0RFRkdISUpLTE1OT1BRUlNUVVZX", "first line\npassword=correct-horse-battery-staple\nlast line"}
	for _, input := range cases {
		envelope.Content = []byte(input)
		if finding := guard.Inspect(context.Background(), envelope); finding.Decision != BlockSecret {
			t.Fatalf("secret allowed %q: %#v", input, finding)
		}
		envelope.PlaintextAuthorized = true
		if finding := guard.Inspect(context.Background(), envelope); finding.Decision != AllowLocal {
			t.Fatalf("trusted-local plaintext blocked %q: %#v", input, finding)
		}
		envelope.PlaintextAuthorized = false
	}
	envelope.Content = []byte(strings.Repeat("x", 5000))
	if guard.Inspect(context.Background(), envelope).Decision != BlockOversize {
		t.Fatal("oversize allowed")
	}
	guard.scan = func([]byte, int) (string, bool) { panic("boom") }
	envelope.Content = []byte("benign")
	if guard.Inspect(context.Background(), envelope).Decision != BlockScannerError {
		t.Fatal("panic did not fail closed")
	}
	guard.scan = func([]byte, int) (string, bool) { time.Sleep(time.Second); return "", false }
	if guard.Inspect(context.Background(), envelope).Decision != BlockScannerError {
		t.Fatal("timeout did not fail closed")
	}
}

func TestReceiptJSONExcludesContent(t *testing.T) {
	receipt := SessionReceipt{SchemaVersion: "aegis.manager.receipt.v1", SessionID: "s", SubjectID: "u", PrincipalID: "p", ManagerID: LogicalAgentID, SecurityContext: SecurityContext, Model: validModel()}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"prompt":`, `"response":`, `"secret_value":`, `"transcript":`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("receipt contains %s", forbidden)
		}
	}
}
