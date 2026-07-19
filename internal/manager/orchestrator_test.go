package manager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
)

type fakeGateway struct {
	outputs [][]byte
	inputs  []string
}

func (f *fakeGateway) Turn(_ context.Context, _, text string, _ int) ([]byte, error) {
	f.inputs = append(f.inputs, text)
	if len(f.outputs) == 0 {
		return nil, errors.New("no output")
	}
	out := f.outputs[0]
	f.outputs = f.outputs[1:]
	return out, nil
}

type fakeOperations struct {
	records                   []credentials.SecretRecord
	created, rotated, revoked int
	valueHash                 string
}

func (f *fakeOperations) Status(context.Context) (map[string]any, error) {
	return map[string]any{"status": "active"}, nil
}
func (f *fakeOperations) List(context.Context, string, int) ([]credentials.SecretRecord, error) {
	return f.records, nil
}
func (f *fakeOperations) Metadata(_ context.Context, id string) (credentials.SecretRecord, error) {
	for _, r := range f.records {
		if r.ID == id {
			return r, nil
		}
	}
	return credentials.SecretRecord{}, credentials.ErrNotFound
}
func (f *fakeOperations) History(context.Context, string, int) ([]credentials.SecretVersionMetadata, error) {
	return nil, nil
}
func (f *fakeOperations) Create(_ context.Context, a CreateArguments, v []byte) (credentials.SecretRecord, error) {
	f.created++
	sum := sha256.Sum256(v)
	f.valueHash = hex.EncodeToString(sum[:])
	r := credentials.SecretRecord{ID: "secret-created", Reference: a.Reference, Kind: a.Kind, Status: credentials.StatusActive, CurrentVersion: 1, CreatedAt: time.Now(), CreatedBy: "principal"}
	f.records = append(f.records, r)
	return r, nil
}
func (f *fakeOperations) Rotate(_ context.Context, _ RotateArguments, v []byte) (credentials.SecretRecord, error) {
	f.rotated++
	sum := sha256.Sum256(v)
	f.valueHash = hex.EncodeToString(sum[:])
	return f.records[0], nil
}
func (f *fakeOperations) Revoke(context.Context, RevokeArguments) error { f.revoked++; return nil }
func (f *fakeOperations) Bind(context.Context, BindingArguments) error  { return nil }
func (f *fakeOperations) VerifyAudit(context.Context) error             { return nil }

func certifiedRoute(t *testing.T) RoutePlan {
	t.Helper()
	now := time.Now().UTC()
	model := ModelIdentity{Registry: "ollama", Name: "exact:1", Digest: "sha256:" + strings.Repeat("a", 64), ContextLength: 65536, TemplateIdentity: "exact", InstructionVersion: InstructionVersion, SchemaVersion: ResponseSchemaVersion, OllamaVersion: "0.32.0", HermesVersion: "0.18.2", ConformanceVersion: ConformanceVersion, Certified: true, CertifiedAt: now}
	return RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: LogicalAgentID, SecurityContext: SecurityContext, HermesPath: "/fake/hermes", HermesVersion: "0.18.2", OllamaMode: "external-local", OllamaEndpoint: "http://127.0.0.1:11434", OllamaVersion: "0.32.0", Model: model, ProxyIdentity: "ephemeral", IssuedAt: now, ExpiresAt: now.Add(time.Minute)}
}
func envelope(op Operation, args any) []byte {
	raw, _ := json.Marshal(args)
	out, _ := json.Marshal(Response{SchemaVersion: ResponseSchemaVersion, Kind: "proposal", Message: "proposal", Proposal: &Proposal{Operation: op, Arguments: raw}})
	return out
}

func TestSessionLifecycleAndRandomCanaryBoundary(t *testing.T) {
	canaryBytes := make([]byte, 32)
	_, _ = rand.Read(canaryBytes)
	canary := "sk-live-" + hex.EncodeToString(canaryBytes)
	gateway := &fakeGateway{outputs: [][]byte{envelope(SecretProposeCreate, CreateArguments{Reference: "service-token", Kind: "opaque", Disclosure: "protected"}), envelope(SecretProposeRotate, RotateArguments{RecordID: "secret-created"}), envelope(SecretProposeRevoke, RevokeArguments{RecordID: "secret-created", Reason: "operator-request"}), envelope(SessionExit, EmptyArguments{})}}
	ops := &fakeOperations{}
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	var receipt SessionReceipt
	session, err := NewSession(context.Background(), SessionConfig{SessionID: "session-1", SubjectID: "local-uid:1", PrincipalID: "principal", Route: certifiedRoute(t), Gateway: gateway, GatewaySessionID: "gateway-1", Guard: guard, Operations: ops, Confirm: func(context.Context, string) (bool, error) { return true, nil }, Intake: func(context.Context, string) ([]byte, error) { return []byte(canary), nil }, Receipt: func(_ context.Context, r SessionReceipt) error { receipt = r; return nil }, MaximumResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range []string{"create a protected record", "rotate the record", "revoke the record", "exit"} {
		if _, err = session.Handle(context.Background(), turn); err != nil {
			t.Fatal(err)
		}
	}
	if err = session.Finalize(context.Background(), "user_exit", "complete"); err != nil {
		t.Fatal(err)
	}
	if ops.created != 1 || ops.rotated != 1 || ops.revoked != 1 || receipt.Cleanup != "complete" {
		t.Fatalf("ops=%+v receipt=%+v", ops, receipt)
	}
	encoded, _ := json.Marshal(struct {
		Inputs  []string
		Receipt SessionReceipt
		Records []credentials.SecretRecord
	}{gateway.inputs, receipt, ops.records})
	if strings.Contains(string(encoded), canary) {
		t.Fatal("protected canary crossed a model-visible or retained metadata boundary")
	}
}

func TestSessionBlocksSecretBeforeGatewayAndDeclineDenies(t *testing.T) {
	gateway := &fakeGateway{outputs: [][]byte{envelope(SecretProposeRevoke, RevokeArguments{RecordID: "secret-one", Reason: "operator-request"})}}
	ops := &fakeOperations{}
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	session, err := NewSession(context.Background(), SessionConfig{SessionID: "s", SubjectID: "u", PrincipalID: "p", Route: certifiedRoute(t), Gateway: gateway, GatewaySessionID: "g", Guard: guard, Operations: ops, Confirm: func(context.Context, string) (bool, error) { return false, nil }, Intake: func(context.Context, string) ([]byte, error) { return nil, errors.New("must not run") }, Receipt: func(context.Context, SessionReceipt) error { return nil }, MaximumResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Handle(context.Background(), "Authorization: Bearer abcdefghijklmnopqrstuvwxyz"); err == nil || len(gateway.inputs) != 0 {
		t.Fatal("secret reached gateway")
	}
	if _, err = session.Handle(context.Background(), "revoke record"); err == nil || ops.revoked != 0 {
		t.Fatal("declined mutation executed")
	}
}

func TestCanceledTurnNeverBecomesScannerFailureOrReachesGateway(t *testing.T) {
	gateway := &fakeGateway{}
	guard, _ := NewGuard(1<<20, 1<<20, 2, 100*time.Millisecond)
	session, err := NewSession(context.Background(), SessionConfig{SessionID: "cancel", SubjectID: "u", PrincipalID: "p", Route: certifiedRoute(t), Gateway: gateway, GatewaySessionID: "g", Guard: guard, Operations: &fakeOperations{}, Confirm: func(context.Context, string) (bool, error) { return false, nil }, Intake: func(context.Context, string) ([]byte, error) { return nil, errors.New("must not run") }, Receipt: func(context.Context, SessionReceipt) error { return nil }, MaximumResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = session.Handle(ctx, "ordinary text"); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), ReasonScannerFailed) {
		t.Fatalf("canceled turn error=%v", err)
	}
	if len(gateway.inputs) != 0 {
		t.Fatal("canceled input reached the gateway")
	}
}

func TestSessionExpiryAndIdempotentFinalReceipt(t *testing.T) {
	route := certifiedRoute(t)
	expiredNow := route.ExpiresAt.Add(time.Second)
	receipts := 0
	guard, err := NewGuard(1<<20, 1<<20, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSession(context.Background(), SessionConfig{SessionID: "session-expired", SubjectID: "subject-1", PrincipalID: "principal-1", Route: route, GatewaySessionID: "gw", Gateway: &fakeGateway{}, Guard: guard, Operations: &fakeOperations{}, Confirm: func(context.Context, string) (bool, error) { return true, nil }, Intake: func(context.Context, string) ([]byte, error) { return nil, context.Canceled }, Receipt: func(context.Context, SessionReceipt) error { receipts++; return nil }, MaximumResponseBytes: 1 << 20, Now: func() time.Time { return expiredNow }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Handle(context.Background(), "status"); err == nil || err.Error() != ReasonSessionExpired {
		t.Fatalf("expiry error=%v", err)
	}
	if err = session.Close(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if err = session.Finalize(context.Background(), "again", "complete"); err != nil {
		t.Fatal(err)
	}
	if err = session.Finalize(context.Background(), "duplicate", "incomplete"); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("final receipts=%d", receipts)
	}
}

type conformingExecutor struct{}

func (conformingExecutor) Execute(_ context.Context, test ConformanceCase) ([]byte, error) {
	if test.ExpectedOperation == "" {
		message := "safe"
		if len(test.RequiredAny) != 0 {
			message = test.RequiredAny[0]
		}
		encoded, _ := json.Marshal(Response{SchemaVersion: ResponseSchemaVersion, Kind: "message", Message: message})
		return encoded, nil
	}
	arguments := any(map[string]any{})
	switch test.ExpectedOperation {
	case SecretProposeCreate:
		arguments = CreateArguments{Reference: "service-token", Kind: "opaque", Disclosure: "protected"}
	case SecretProposeRotate:
		arguments = RotateArguments{RecordID: "secret-example"}
	case SecretProposeRevoke:
		arguments = RevokeArguments{RecordID: "secret-example", Version: 1, Reason: "operator-request"}
	case SecretSearch:
		arguments = SearchArguments{Query: "github", PageArguments: PageArguments{Limit: 20}}
	case SecretList:
		arguments = PageArguments{Limit: 20}
	case StatusShow:
		arguments = EmptyArguments{}
	}
	raw, _ := json.Marshal(arguments)
	encoded, _ := json.Marshal(Response{SchemaVersion: ResponseSchemaVersion, Kind: "proposal", Message: "safe", Proposal: &Proposal{Operation: test.ExpectedOperation, Arguments: raw}})
	return encoded, nil
}
func TestCertificationPersistenceAndDrift(t *testing.T) {
	candidate := Candidates()[0]
	now := time.Now().UTC()
	cert, err := RunCertification(context.Background(), conformingExecutor{}, candidate, candidate.OllamaName, "sha256:"+strings.Repeat("b", 64), "Q4", "0.18.2", "0.32.0", 65536, now)
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/cert.json"
	if err = SaveCertification(path, cert); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadCertification(path, cert.ArtifactName, cert.ArtifactDigest, "0.18.2", "0.32.0", 65536); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadCertification(path, cert.ArtifactName, "sha256:"+strings.Repeat("c", 64), "0.18.2", "0.32.0", 65536); err == nil {
		t.Fatal("certification drift accepted")
	}
}
