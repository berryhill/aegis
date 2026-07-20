//go:build linux

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/credentials/broker"
)

type brokerTestCustodian struct{ key []byte }

func validBrokerRequest(token string, now time.Time) broker.Request {
	return broker.Request{SchemaVersion: 1, RequestID: hex.EncodeToString(bytes.Repeat([]byte{0x42}, 16)), Deadline: now.Add(5 * time.Second), Capability: token, Owner: "approved-owner", Repository: "approved-repository"}
}

func nextBrokerRequest(request broker.Request, marker byte) broker.Request {
	request.RequestID = hex.EncodeToString(bytes.Repeat([]byte{marker}, 16))
	return request
}

func (custodian *brokerTestCustodian) ActiveKEK(_ context.Context, fn func(credentials.KEKMetadata, []byte) error) error {
	return fn(credentials.KEKMetadata{ID: "test-kek", Version: 1}, append([]byte(nil), custodian.key...))
}
func (custodian *brokerTestCustodian) KEK(_ context.Context, id string, version uint64, fn func([]byte) error) error {
	if id != "test-kek" || version != 1 {
		return errors.New("key unavailable")
	}
	return fn(append([]byte(nil), custodian.key...))
}

func brokerAuthorizedService(t *testing.T) (*Service, string, string, *credentials.Authority) {
	t.Helper()
	s := testService(t)
	directory := filepath.Join(t.TempDir(), "authority")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	custodian := &brokerTestCustodian{key: key}
	repository, err := credentialbolt.Open(context.Background(), filepath.Join(directory, "authority.db"), "deployment-test", custodian)
	if err != nil {
		t.Fatal(err)
	}
	authority := credentials.NewAuthority(repository, custodian)
	t.Cleanup(func() { _ = authority.Close() })
	s.CredentialAuthority = authority
	s.Config.Credentials.Authority.DeploymentID = "deployment-test"
	s.Config.Credentials.Authority.Broker.AllowedUID = uint32(os.Getuid())
	s.Config.Credentials.Authority.Broker.AllowedGID = uint32(os.Getgid())
	s.Config.Credentials.Authority.Broker.Timeout = 10 * time.Second
	s.Config.Credentials.Authority.Broker.Destinations = map[string]config.BrokerDestination{broker.GitHubDestination: {URL: "http://127.0.0.1/fixed", Repositories: []string{"approved-owner/approved-repository"}}}
	canary := "authority-canary-never-return"
	record, err := authority.Create(context.Background(), "local/api", "api-token", "principal-1", []byte(canary))
	if err != nil {
		t.Fatal(err)
	}
	binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: "office", StanzaID: "principal", DeploymentID: "deployment-test", Scope: broker.GitHubScope}, SecretRecord: record.ID, VersionPolicy: credentials.VersionCurrent, Mode: "brokered", Destinations: []string{broker.GitHubDestination}, Enabled: true, BindingRevision: 1}
	if err = authority.Bind(context.Background(), binding); err != nil {
		t.Fatal(err)
	}
	now := s.Now()
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)}
	charter := testCharter(now)
	charter.Stanzas[0].Scopes.Credentials = []string{"provider:test", broker.GitHubScope}
	charter.Stanzas[0].Grant.Capabilities = append(charter.Stanzas[0].Grant.Capabilities, broker.ActionGitHubGetRepository)
	charter.Stanzas[0].Grant.Tools = []string{"aegis"}
	charter.Stanzas[0].Hermes.Toolsets = []string{"aegis"}
	canonical, _ := core.Canonicalize(charter)
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Save("receipts", "broker-ready", core.Receipt{ID: "broker-ready", CharterDigest: canonical.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	mandate, _, err := s.PreviewSessionAs(context.Background(), subject, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	pid := os.Getpid()
	session := core.Session{ID: "session-broker", Mandate: mandate, RuntimePID: pid, ProcessStart: processStartToken(pid), Status: "running", StartedAt: now, RuntimeHome: t.TempDir()}
	if err = s.Store.Save("sessions", session.ID, session); err != nil {
		t.Fatal(err)
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	s.capabilities[digest] = broker.Capability{SessionID: session.ID, MandateID: mandate.ID, SubjectID: subject.ID, AgentID: mandate.AgentID, StanzaID: mandate.StanzaID, DeploymentID: mandate.DeploymentID, CharterDigest: mandate.CharterDigest, IssuedAt: now, ExpiresAt: now.Add(time.Minute), RuntimePID: pid, ProcessStart: session.ProcessStart}
	s.brokerRequests[digest] = make(map[[32]byte]struct{})
	return s, token, canary, authority
}

func TestBrokerFullTupleAndReplayRevocation(t *testing.T) {
	s, token, canary, authority := brokerAuthorizedService(t)
	peer := broker.Peer{PID: int32(os.Getpid()), UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	request := validBrokerRequest(token, s.Now())
	stale := nextBrokerRequest(request, 5)
	stale.Deadline = s.Now().Add(-time.Second)
	if _, err := s.ExecuteBroker(context.Background(), peer, stale, func(context.Context, []byte, broker.Grant) (broker.Result, error) {
		t.Fatal("stale request reached credential executor")
		return broker.Result{}, nil
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("stale request: %v", err)
	}
	calls := 0
	result, err := s.ExecuteBroker(context.Background(), peer, request, func(_ context.Context, secret []byte, grant broker.Grant) (broker.Result, error) {
		calls++
		if string(secret) != canary || grant.RecordID == "" || grant.Version != 1 {
			t.Fatal("wrong resolved credential grant")
		}
		return broker.Result{StatusCode: 204, Outcome: "credential_applied"}, nil
	})
	if err != nil || calls != 1 || result.StatusCode != 204 {
		events, _ := s.Store.AuditEvents()
		t.Fatalf("broker execution=%+v calls=%d err=%v audit=%+v", result, calls, err, events)
	}
	if _, err = s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) {
		t.Fatal("replayed request reached credential executor")
		return broker.Result{}, nil
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("request replay: %v", err)
	}
	denied := []broker.Request{nextBrokerRequest(broker.Request{SchemaVersion: 1, Deadline: request.Deadline, Owner: "approved-owner", Repository: "approved-repository"}, 1), nextBrokerRequest(broker.Request{SchemaVersion: 1, Deadline: request.Deadline, Capability: "bad", Owner: "approved-owner", Repository: "approved-repository"}, 2), nextBrokerRequest(broker.Request{SchemaVersion: 1, Deadline: request.Deadline, Capability: token, Owner: "other-owner", Repository: "approved-repository"}, 3)}
	for index, bad := range denied {
		if _, err = s.ExecuteBroker(context.Background(), peer, bad, func(context.Context, []byte, broker.Grant) (broker.Result, error) {
			t.Fatal("denied executor called")
			return broker.Result{}, nil
		}); !errors.Is(err, ErrDenied) {
			t.Fatalf("denial %d: %v", index, err)
		}
	}
	wrongPeer := peer
	wrongPeer.UID++
	if _, err = s.ExecuteBroker(context.Background(), wrongPeer, request, nil); !errors.Is(err, ErrDenied) {
		t.Fatalf("wrong peer: %v", err)
	}
	if err = authority.Revoke(context.Background(), func() string {
		var id string
		_ = authority.UseResolved(context.Background(), credentials.CredentialBindingKey{AgentID: "office", StanzaID: "principal", DeploymentID: "deployment-test", Scope: broker.GitHubScope}, broker.GitHubDestination, func(r credentials.ResolvedSecret, _ []byte) error { id = r.Record.ID; return nil })
		return id
	}(), 1, "test-revoke"); err != nil {
		t.Fatal(err)
	}
	request = nextBrokerRequest(request, 4)
	if _, err = s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked version replay: %v", err)
	}
	if calls != 1 {
		t.Fatalf("plaintext executor called after denial: %d", calls)
	}
}

func TestBrokerCapabilityMaterialIsEphemeralAndNotPersisted(t *testing.T) {
	s, _, canary, _ := brokerAuthorizedService(t)
	s.Config.Credentials.Authority.Broker.Socket = filepath.Join(t.TempDir(), "broker.sock")
	s.Config.Credentials.Authority.Broker.CapabilityTTL = time.Minute
	session, err := s.GetSession("session-broker")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.issueBrokerCapability(&session); err != nil {
		t.Fatal(err)
	}
	materialPath := filepath.Join(session.RuntimeHome, broker.CapabilityFileName)
	material, err := os.ReadFile(materialPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(material) == 0 || string(material) == canary {
		t.Fatal("invalid capability material")
	}
	stored, err := os.ReadFile(filepath.Join(s.Store.Root(), "sessions", session.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Capability string `json:"capability"`
	}
	if json.Unmarshal(material, &envelope) != nil || len(envelope.Capability) != 64 {
		t.Fatal("capability material is malformed")
	}
	if bytes.Contains(stored, []byte(envelope.Capability)) || bytes.Contains(stored, []byte(canary)) {
		t.Fatal("session persistence contains bearer capability or credential")
	}
	s.revokeBrokerCapabilities(session.ID)
	if _, err = os.Stat(materialPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capability material remained after cleanup: %v", err)
	}
}

func TestOperationalSessionBrokerLifecycleEndToEnd(t *testing.T) {
	s, _, canary, _ := brokerAuthorizedService(t)
	s.Config.Credentials.Authority.Broker.Socket = filepath.Join(t.TempDir(), "credential-broker.sock")
	s.Config.Credentials.Authority.Broker.CapabilityTTL = time.Minute
	subject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	prepared, err := s.GetSession("session-broker")
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSessionAs(context.Background(), subject, prepared.Mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	material, err := os.ReadFile(filepath.Join(session.RuntimeHome, broker.CapabilityFileName))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Capability string `json:"capability"`
	}
	if json.Unmarshal(material, &envelope) != nil || len(envelope.Capability) != 64 {
		t.Fatal("operational session did not receive bounded capability material")
	}
	if bytes.Contains(material, []byte(canary)) {
		t.Fatal("session capability material contains credential plaintext")
	}
	request := validBrokerRequest(envelope.Capability, s.Now())
	peer := broker.Peer{PID: int32(session.RuntimePID), UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	if _, err = s.ExecuteBroker(context.Background(), peer, request, func(_ context.Context, plaintext []byte, _ broker.Grant) (broker.Result, error) {
		if string(plaintext) != canary {
			t.Fatal("broker did not apply the authority credential")
		}
		return broker.Result{StatusCode: 200, Outcome: "credential_applied"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, processFile := range []string{"cmdline", "environ"} {
		data, readErr := os.ReadFile(filepath.Join("/proc", fmt.Sprint(session.RuntimePID), processFile))
		if readErr == nil && (bytes.Contains(data, []byte(canary)) || bytes.Contains(data, []byte(envelope.Capability))) {
			t.Fatalf("runtime %s contains credential or capability", processFile)
		}
	}
	if walkErr := filepath.Walk(session.RuntimeHome, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() || filepath.Base(path) == broker.CapabilityFileName {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(canary)) || bytes.Contains(data, []byte(envelope.Capability)) {
			return errors.New("runtime file contains credential or capability")
		}
		return nil
	}); walkErr != nil {
		t.Fatal(walkErr)
	}
	events, err := s.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	auditJSON, _ := json.Marshal(events)
	if bytes.Contains(auditJSON, []byte(canary)) || bytes.Contains(auditJSON, []byte(envelope.Capability)) {
		t.Fatal("audit contains credential or capability")
	}
	if err = s.TerminateSessionAs(context.Background(), subject, session.ID, "test-complete"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) {
		t.Fatal("terminated session reached credential executor")
		return broker.Result{}, nil
	}); !errors.Is(err, ErrDenied) {
		t.Fatalf("terminated session capability replay=%v", err)
	}
	if _, err = os.Stat(filepath.Join(session.RuntimeHome, broker.CapabilityFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session cleanup retained capability material: %v", err)
	}
}

func TestBrokerCapabilitySessionIsolationExpiryAndProcessLoss(t *testing.T) {
	s, token, _, _ := brokerAuthorizedService(t)
	peer := broker.Peer{PID: int32(os.Getpid()), UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
	request := validBrokerRequest(token, s.Now())
	// Capability bound to another session cannot substitute its session identity.
	digest := sha256.Sum256([]byte(token))
	capability := s.capabilities[digest]
	capability.SessionID = "another-session"
	s.capabilities[digest] = capability
	if _, err := s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	capability.SessionID = "session-broker"
	marker := byte(10)
	for _, mutate := range []func(*broker.Capability){
		func(value *broker.Capability) { value.AgentID = "another-agent" },
		func(value *broker.Capability) { value.StanzaID = "teamwide" },
		func(value *broker.Capability) { value.DeploymentID = "another-deployment" },
	} {
		changed := capability
		mutate(&changed)
		s.capabilities[digest] = changed
		marker++
		request = nextBrokerRequest(request, marker)
		if _, err := s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
			t.Fatal("capability authority substitution was accepted")
		}
	}
	s.capabilities[digest] = capability
	for _, status := range []string{"terminated", "revoked", "failed"} {
		session, err := s.GetSession("session-broker")
		if err != nil {
			t.Fatal(err)
		}
		session.Status = status
		if err = s.Store.Save("sessions", session.ID, session); err != nil {
			t.Fatal(err)
		}
		s.capabilities[digest] = capability
		marker++
		request = nextBrokerRequest(request, marker)
		if _, err = s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
			t.Fatalf("%s session replay: %v", status, err)
		}
		session.Status = "running"
		if err = s.Store.Save("sessions", session.ID, session); err != nil {
			t.Fatal(err)
		}
	}
	mandate, err := s.GetMandate(capability.MandateID)
	if err != nil {
		t.Fatal(err)
	}
	originalExpiry := mandate.ExpiresAt
	mandate.ExpiresAt = s.Now()
	if err = s.Store.Save("mandates", mandate.ID, mandate); err != nil {
		t.Fatal(err)
	}
	s.capabilities[digest] = capability
	marker++
	request = nextBrokerRequest(request, marker)
	if _, err = s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
		t.Fatal("expired mandate was accepted")
	}
	mandate.ExpiresAt = originalExpiry
	if err = s.Store.Save("mandates", mandate.ID, mandate); err != nil {
		t.Fatal(err)
	}
	capability.ExpiresAt = s.Now().Add(-time.Second)
	s.capabilities[digest] = capability
	marker++
	request = nextBrokerRequest(request, marker)
	if _, err := s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	// Restore under a new token and invalidate the defensible PID start identity.
	other := sha256.Sum256([]byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	capability.ExpiresAt = s.Now().Add(time.Minute)
	capability.ProcessStart = "reused"
	s.capabilities[other] = capability
	s.brokerRequests[other] = make(map[[32]byte]struct{})
	request.Capability = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	request = nextBrokerRequest(request, marker+1)
	if _, err := s.ExecuteBroker(context.Background(), peer, request, func(context.Context, []byte, broker.Grant) (broker.Result, error) { return broker.Result{}, nil }); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}
