package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/credentials/broker"
)

const brokerCapabilityFile = "aegis-broker-capability.json"

const maxBrokerRequestsPerCapability = 4096

func (s *Service) issueBrokerCapability(session *core.Session) error {
	cfg := s.Config.Credentials.Authority.Broker
	if session.Mandate.DeploymentID == "" || session.Mandate.Subject.ID == "" || cfg.CapabilityTTL <= 0 {
		return errors.New("cannot issue broker authority for an incomplete session mandate")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return errors.New("generate broker session capability")
	}
	token := hex.EncodeToString(raw)
	for i := range raw {
		raw[i] = 0
	}
	now := s.Now()
	expires := now.Add(cfg.CapabilityTTL)
	if session.Mandate.ExpiresAt.Before(expires) {
		expires = session.Mandate.ExpiresAt
	}
	capability := broker.Capability{SessionID: session.ID, MandateID: session.Mandate.ID, SubjectID: session.Mandate.Subject.ID, AgentID: session.Mandate.AgentID, StanzaID: session.Mandate.StanzaID, DeploymentID: session.Mandate.DeploymentID, CharterDigest: session.Mandate.CharterDigest, IssuedAt: now, ExpiresAt: expires, RuntimePID: session.RuntimePID, ProcessStart: session.ProcessStart}
	digest := sha256.Sum256([]byte(token))
	s.capMu.Lock()
	s.capabilities[digest] = capability
	s.brokerRequests[digest] = make(map[[32]byte]struct{})
	s.capMu.Unlock()
	material := struct {
		Socket     string    `json:"socket"`
		Capability string    `json:"capability"`
		ExpiresAt  time.Time `json:"expires_at"`
	}{cfg.Socket, token, expires}
	encoded, err := json.Marshal(material)
	if err != nil {
		s.revokeBrokerCapabilities(session.ID)
		return errors.New("encode broker session material")
	}
	path := filepath.Join(session.RuntimeHome, brokerCapabilityFile)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		err = file.Chown(int(cfg.AllowedUID), int(cfg.AllowedGID))
	}
	if err == nil {
		_, err = file.Write(encoded)
		if syncErr := file.Sync(); err == nil {
			err = syncErr
		}
	}
	if file != nil {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	for i := range encoded {
		encoded[i] = 0
	}
	if err != nil {
		_ = os.Remove(path)
		s.revokeBrokerCapabilities(session.ID)
		return errors.New("materialize broker session capability")
	}
	return nil
}

func (s *Service) revokeBrokerCapabilities(sessionID string) {
	s.capMu.Lock()
	for digest, capability := range s.capabilities {
		if capability.SessionID == sessionID {
			delete(s.capabilities, digest)
			delete(s.brokerRequests, digest)
		}
	}
	s.capMu.Unlock()
	if session, err := s.GetSession(sessionID); err == nil && session.RuntimeHome != "" {
		_ = os.Remove(filepath.Join(session.RuntimeHome, brokerCapabilityFile))
	}
}

func (s *Service) brokerAudit(ctx context.Context, grant broker.Grant, outcome, reason string) error {
	metadata := map[string]string{"action": grant.Operation, "destination": grant.Destination, "scope": grant.Scope, "deployment_id": grant.DeploymentID, "request_id": grant.RequestID}
	if grant.RecordID != "" {
		metadata["record_id"] = grant.RecordID
		metadata["version"] = strconv.FormatUint(grant.Version, 10)
		metadata["binding_revision"] = strconv.FormatUint(grant.BindingRevision, 10)
	}
	return s.audit(ctx, core.AuditEvent{Type: "credential_broker", SubjectID: grant.SubjectID, AgentID: grant.AgentID, StanzaID: grant.StanzaID, SessionID: grant.SessionID, MandateID: grant.MandateID, CharterDigest: grant.CharterDigest, Outcome: outcome, Reason: reason, Metadata: metadata})
}

func (s *Service) ExecuteBroker(ctx context.Context, peer broker.Peer, request broker.Request, execute broker.Executor) (broker.Result, error) {
	grant := broker.Grant{Scope: broker.GitHubScope, Operation: broker.ActionGitHubGetRepository, Destination: broker.GitHubDestination}
	deny := func(reason string) (broker.Result, error) {
		_ = s.brokerAudit(ctx, grant, "deny", reason)
		return broker.Result{}, fmt.Errorf("%w: broker request denied", ErrDenied)
	}
	cfg := s.Config.Credentials.Authority.Broker
	if s.CredentialAuthority == nil || execute == nil {
		return deny("broker_unavailable")
	}
	if peer.UID != cfg.AllowedUID || peer.GID != cfg.AllowedGID {
		return deny("peer_identity_mismatch")
	}
	if len(request.Capability) != 64 {
		return deny("capability_missing_or_invalid")
	}
	decoded, err := hex.DecodeString(request.Capability)
	if err != nil || len(decoded) != 32 {
		return deny("capability_missing_or_invalid")
	}
	defer func() {
		for i := range decoded {
			decoded[i] = 0
		}
	}()
	digest := sha256.Sum256([]byte(request.Capability))
	requestID, requestIDErr := hex.DecodeString(request.RequestID)
	if request.SchemaVersion != 1 || requestIDErr != nil || len(requestID) != 16 || request.Deadline.IsZero() {
		return deny("request_freshness_invalid")
	}
	requestDigest := sha256.Sum256(requestID)
	for i := range requestID {
		requestID[i] = 0
	}
	grant.RequestID = request.RequestID
	now := s.Now()
	s.capMu.Lock()
	capability, ok := s.capabilities[digest]
	if !ok {
		s.capMu.Unlock()
		return deny("capability_unknown")
	}
	if requests := s.brokerRequests[digest]; requests == nil {
		s.capMu.Unlock()
		return deny("capability_unknown")
	} else if _, replayed := requests[requestDigest]; replayed {
		s.capMu.Unlock()
		return deny("request_replayed")
	} else if len(requests) >= maxBrokerRequestsPerCapability {
		s.capMu.Unlock()
		return deny("request_budget_exhausted")
	} else {
		requests[requestDigest] = struct{}{}
	}
	s.capMu.Unlock()
	grant.SessionID, grant.MandateID, grant.SubjectID = capability.SessionID, capability.MandateID, capability.SubjectID
	grant.AgentID, grant.StanzaID, grant.DeploymentID, grant.CharterDigest = capability.AgentID, capability.StanzaID, capability.DeploymentID, capability.CharterDigest
	if !now.Before(capability.ExpiresAt) {
		s.revokeBrokerCapabilities(capability.SessionID)
		return deny("capability_expired")
	}
	if !now.Before(request.Deadline) || request.Deadline.After(capability.ExpiresAt) || request.Deadline.After(now.Add(cfg.Timeout)) {
		return deny("request_expired_or_unbounded")
	}
	session, err := s.GetSession(capability.SessionID)
	if err != nil || session.Status != "running" || session.Mandate.ID != capability.MandateID {
		return deny("session_inactive")
	}
	mandate, err := s.GetMandate(capability.MandateID)
	if err != nil || s.validateMandate(mandate) != nil || mandate.Subject.ID == "" || mandate.Subject.ID != capability.SubjectID || mandate.AgentID != capability.AgentID || mandate.StanzaID != capability.StanzaID || mandate.DeploymentID != capability.DeploymentID || mandate.CharterDigest != capability.CharterDigest {
		return deny("mandate_invalid")
	}
	if !processMatches(capability.RuntimePID, capability.ProcessStart) || !processDescendsFrom(int(peer.PID), capability.RuntimePID, capability.ProcessStart) {
		return deny("runtime_process_identity_lost")
	}
	if !contains(mandate.Capabilities, broker.ActionGitHubGetRepository) || !contains(mandate.Scopes.Credentials, broker.GitHubScope) {
		return deny("operation_or_scope_denied")
	}
	destination, ok := cfg.Destinations[broker.GitHubDestination]
	if !ok {
		return deny("destination_denied")
	}
	approvedRepository := false
	for _, repository := range destination.Repositories {
		if repository == request.Owner+"/"+request.Repository {
			approvedRepository = true
			break
		}
	}
	if !approvedRepository {
		return deny("repository_denied")
	}
	key := credentials.CredentialBindingKey{AgentID: mandate.AgentID, StanzaID: mandate.StanzaID, DeploymentID: mandate.DeploymentID, Scope: broker.GitHubScope}
	var result broker.Result
	var actionAttempted bool
	err = s.CredentialAuthority.UseResolved(ctx, key, broker.GitHubDestination, func(resolved credentials.ResolvedSecret, plaintext []byte) error {
		grant.RecordID, grant.Version, grant.BindingRevision = resolved.Record.ID, resolved.Version.Version, resolved.Binding.BindingRevision
		if auditErr := s.brokerAudit(ctx, grant, "allow", "authorized"); auditErr != nil {
			return auditErr
		}
		actionAttempted = true
		var executeErr error
		result, executeErr = execute(ctx, plaintext, grant)
		return executeErr
	})
	if err != nil {
		if actionAttempted {
			_ = s.brokerAudit(ctx, grant, "use", "downstream_failure")
			return broker.Result{}, broker.ErrDownstream
		} else {
			_ = s.brokerAudit(ctx, grant, "deny", "credential_resolution_or_audit_denied")
		}
		return broker.Result{}, fmt.Errorf("%w: broker request denied", ErrDenied)
	}
	if err = s.brokerAudit(ctx, grant, "use", "success"); err != nil {
		return broker.Result{}, fmt.Errorf("%w: broker audit unavailable", ErrDenied)
	}
	return result, nil
}

func processDescendsFrom(pid, rootPID int, rootStart string) bool {
	if pid <= 0 || rootPID <= 0 || !processMatches(rootPID, rootStart) {
		return false
	}
	for depth := 0; depth < 64 && pid > 1; depth++ {
		if pid == rootPID {
			return true
		}
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			return false
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return false
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 2 {
			return false
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}
