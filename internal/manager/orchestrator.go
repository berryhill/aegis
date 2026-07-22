package manager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/credentials"
)

// Gateway is the bounded Hermes conversation boundary used by Session.
type Gateway interface {
	Turn(context.Context, string, string, int) ([]byte, error)
}

type streamingGateway interface {
	TurnStream(context.Context, string, string, int, func([]byte) error) ([]byte, error)
}

// Operations is the shared authoritative manager service. Implementations own
// authentication, authorization, persistence, encryption, and audit.
type Operations interface {
	Status(context.Context) (map[string]any, error)
	List(context.Context, string, int) ([]credentials.SecretRecord, error)
	Counts(context.Context) (credentials.SecretCounts, error)
	ReadValue(context.Context, string, func(credentials.SecretRecord, []byte) error) error
	Metadata(context.Context, string) (credentials.SecretRecord, error)
	History(context.Context, string, int) ([]credentials.SecretVersionMetadata, error)
	Create(context.Context, CreateArguments, []byte) (credentials.SecretRecord, error)
	Rotate(context.Context, RotateArguments, []byte) (credentials.SecretRecord, error)
	Revoke(context.Context, RevokeArguments) error
	Bind(context.Context, BindingArguments) error
	VerifyAudit(context.Context) error
}

type Confirm func(context.Context, string) (bool, error)
type Intake func(context.Context, string) ([]byte, error)
type ReceiptSink func(context.Context, SessionReceipt) error

type SessionConfig struct {
	SessionID            string
	SubjectID            string
	PrincipalID          string
	Route                RoutePlan
	Gateway              Gateway
	GatewaySessionID     string
	Guard                *Guard
	Operations           Operations
	Confirm              Confirm
	Intake               Intake
	Receipt              ReceiptSink
	MaximumResponseBytes int
	Now                  func() time.Time
}

type Session struct {
	config    SessionConfig
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closing   bool
	closed    bool
	finalized bool
	receipt   SessionReceipt
}

func NewSession(parent context.Context, config SessionConfig) (*Session, error) {
	if parent == nil || config.SessionID == "" || config.SubjectID == "" || config.PrincipalID == "" || config.Gateway == nil || config.GatewaySessionID == "" || config.Guard == nil || config.Operations == nil || config.Confirm == nil || config.Intake == nil || config.Receipt == nil || config.MaximumResponseBytes < 1024 || config.MaximumResponseBytes > 16<<20 {
		return nil, errors.New("manager session configuration is incomplete")
	}
	if err := config.Route.Validate(); err != nil {
		return nil, err
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	routeDigest, err := config.Route.Digest()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Session{config: config, ctx: ctx, cancel: cancel}
	s.receipt = SessionReceipt{SchemaVersion: "aegis.manager.receipt.v1", SessionID: config.SessionID, SubjectID: config.SubjectID, PrincipalID: config.PrincipalID, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, PolicyVersion: PolicyVersion, PolicyDigest: PolicyDigest(), RouteDigest: routeDigest, Model: config.Route.Model, StartedAt: config.Now()}
	return s, nil
}

func (s *Session) Handle(ctx context.Context, text string) (string, error) {
	message, _, err := s.HandleStream(ctx, text, nil)
	return message, err
}

// HandleCreateIntent executes an Aegis-parsed metadata-only create proposal.
// The credential value is still collected by the configured protected intake.
func (s *Session) HandleCreateIntent(ctx context.Context, arguments CreateArguments) (string, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", err
	}
	if err := validateCreate(arguments); err != nil {
		return "", err
	}
	value, err := s.config.Intake(activeCtx, "new secret value")
	if err != nil {
		return "", err
	}
	defer wipe(value)
	return s.storeCreateValue(activeCtx, arguments, value)
}

func (s *Session) HandleCredentialCount(ctx context.Context) (string, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", err
	}
	counts, err := s.config.Operations.Counts(activeCtx)
	return operationResult(SecretCount, counts, err)
}

func (s *Session) HandleCredentialList(ctx context.Context) (string, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", err
	}
	records, err := s.config.Operations.List(activeCtx, "", 100)
	return operationResult(SecretList, records, err)
}

func (s *Session) HandleCredentialValueRead(ctx context.Context, reference string) (string, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", err
	}
	var message string
	err = s.config.Operations.ReadValue(activeCtx, reference, func(record credentials.SecretRecord, value []byte) error {
		message = fmt.Sprintf("Aegis authoritative credential value (%s): %s", record.Reference, strconv.Quote(string(value)))
		return nil
	})
	return message, err
}

// HandleCreateIntentWithValue executes a deterministic authenticated create
// without involving the conversational model. This keeps an already-clear
// typed operation from poisoning or being vetoed by the Hermes conversation.
func (s *Session) HandleCreateIntentWithValue(ctx context.Context, text string, arguments CreateArguments, value []byte) (string, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", err
	}
	if err = validateCreate(arguments); err != nil || len(value) == 0 || len(value) > 1<<20 {
		return "", errors.New(ReasonProposalInvalid)
	}
	finding := s.config.Guard.Inspect(activeCtx, ContentEnvelope{Source: SourceUser, SubjectID: s.config.SubjectID, SessionID: s.config.SessionID, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: "text/plain", ProvenanceID: "trusted-local-credential-turn", RouteClass: "local", Content: []byte(text), PlaintextAuthorized: true})
	if finding.Decision != AllowLocal {
		return "", errors.New(finding.Reason)
	}
	return s.storeCreateValue(activeCtx, arguments, value)
}

// HandleStream releases only the message field of a canonical message-only
// envelope. Proposal envelopes and non-canonical responses remain completely
// buffered. The returned bool reports whether any assistant text was released.
func (s *Session) HandleStream(ctx context.Context, text string, emit func(string) error) (string, bool, error) {
	activeCtx, err := s.activeContext(ctx)
	if err != nil {
		return "", false, err
	}
	ctx = activeCtx
	finding := s.config.Guard.Inspect(ctx, ContentEnvelope{Source: SourceUser, SubjectID: s.config.SubjectID, SessionID: s.config.SessionID, ManagerID: LogicalAgentID, SecurityContext: SecurityContext, ContentType: "text/plain", ProvenanceID: "terminal-turn", RouteClass: "local", Content: []byte(text)})
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if finding.Decision != AllowLocal {
		return "", false, errors.New(finding.Reason)
	}
	preview := newMessagePreview(s.config.MaximumResponseBytes)
	var responseBytes []byte
	if gateway, ok := s.config.Gateway.(streamingGateway); ok && emit != nil {
		responseBytes, err = gateway.TurnStream(ctx, s.config.GatewaySessionID, text, s.config.MaximumResponseBytes, func(chunk []byte) error {
			return preview.Feed(chunk, emit)
		})
	} else {
		responseBytes, err = s.config.Gateway.Turn(ctx, s.config.GatewaySessionID, text, s.config.MaximumResponseBytes)
	}
	if err != nil {
		return "", preview.Released(), fmt.Errorf("%s: %w", ReasonGatewayProtocol, err)
	}
	response, arguments, err := DecodeResponse(responseBytes, s.config.MaximumResponseBytes)
	if err != nil {
		return "", preview.Released(), fmt.Errorf("%s: %w", ReasonResponseInvalid, err)
	}
	if response.Kind == "message" {
		if emit != nil {
			if err := preview.Complete(response.Message, emit); err != nil {
				return "", preview.Released(), fmt.Errorf("%s: %w", ReasonResponseInvalid, err)
			}
		}
		return response.Message, preview.Released(), nil
	}
	message, err := s.execute(ctx, *response.Proposal, arguments, response.Message)
	return message, false, err
}

func (s *Session) activeContext(ctx context.Context) (context.Context, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	if s.closing || s.closed {
		s.mu.Unlock()
		return nil, errors.New("manager session is closing")
	}
	s.mu.Unlock()
	if !s.config.Now().Before(s.config.Route.ExpiresAt) {
		_ = s.Close(context.Background(), ReasonSessionExpired)
		return nil, errors.New(ReasonSessionExpired)
	}
	if ctx == nil {
		ctx = s.ctx
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ctx, nil
}

func (s *Session) execute(ctx context.Context, proposal Proposal, arguments any, message string) (string, error) {
	switch proposal.Operation {
	case StatusShow:
		result, err := s.config.Operations.Status(ctx)
		return operationResult(proposal.Operation, result, err)
	case SecretList:
		a := arguments.(*PageArguments)
		limit := boundedLimit(a.Limit)
		result, err := s.config.Operations.List(ctx, "", limit)
		return operationResult(proposal.Operation, result, err)
	case SecretSearch:
		a := arguments.(*SearchArguments)
		result, err := s.config.Operations.List(ctx, a.Query, boundedLimit(a.Limit))
		return operationResult(proposal.Operation, result, err)
	case SecretMetadata:
		result, err := s.config.Operations.Metadata(ctx, arguments.(*RecordArguments).RecordID)
		return operationResult(proposal.Operation, result, err)
	case SecretHistory:
		result, err := s.config.Operations.History(ctx, arguments.(*RecordArguments).RecordID, 100)
		return operationResult(proposal.Operation, result, err)
	case AuditVerify:
		return message, s.config.Operations.VerifyAudit(ctx)
	case SessionExit:
		return message, s.Close(ctx, "operator_exit")
	case SecretProposeCreate:
		a := *arguments.(*CreateArguments)
		if err := validateCreate(a); err != nil {
			return "", err
		}
		ok, err := s.config.Confirm(ctx, createPreview(a))
		if err != nil || !ok {
			if err == nil {
				err = errors.New("manager proposal declined")
			}
			return "", err
		}
		value, err := s.config.Intake(ctx, "new secret value")
		if err != nil {
			return "", err
		}
		defer wipe(value)
		result, err := s.config.Operations.Create(ctx, a, value)
		return operationResult(proposal.Operation, result, err)
	case SecretProposeRotate:
		a := *arguments.(*RotateArguments)
		if !credentials.ValidateIdentifier(a.RecordID) {
			return "", errors.New(ReasonProposalInvalid)
		}
		ok, err := s.config.Confirm(ctx, preview(proposal.Operation, a.RecordID))
		if err != nil || !ok {
			if err == nil {
				err = errors.New("manager proposal declined")
			}
			return "", err
		}
		value, err := s.config.Intake(ctx, "replacement secret value")
		if err != nil {
			return "", err
		}
		defer wipe(value)
		result, err := s.config.Operations.Rotate(ctx, a, value)
		return operationResult(proposal.Operation, result, err)
	case SecretProposeRevoke:
		a := *arguments.(*RevokeArguments)
		if !credentials.ValidateIdentifier(a.RecordID) || !credentials.ValidateIdentifier(a.Reason) {
			return "", errors.New(ReasonProposalInvalid)
		}
		ok, err := s.config.Confirm(ctx, preview(proposal.Operation, a.RecordID))
		if err != nil || !ok {
			if err == nil {
				err = errors.New("manager proposal declined")
			}
			return "", err
		}
		err = s.config.Operations.Revoke(ctx, a)
		return operationResult(proposal.Operation, map[string]any{"record_id": a.RecordID, "version": a.Version, "status": "revoked"}, err)
	case SecretProposeBinding:
		a := *arguments.(*BindingArguments)
		ok, err := s.config.Confirm(ctx, preview(proposal.Operation, a.RecordID))
		if err != nil || !ok {
			if err == nil {
				err = errors.New("manager proposal declined")
			}
			return "", err
		}
		err = s.config.Operations.Bind(ctx, a)
		return operationResult(proposal.Operation, map[string]any{"record_id": a.RecordID, "status": "bound"}, err)
	default:
		return "", errors.New(ReasonProposalInvalid)
	}
}

func (s *Session) executeCreateValue(ctx context.Context, arguments CreateArguments, value []byte) (string, error) {
	ok, err := s.config.Confirm(ctx, createPreview(arguments))
	if err != nil || !ok {
		if err == nil {
			err = errors.New("manager proposal declined")
		}
		return "", err
	}
	return s.storeCreateValue(ctx, arguments, value)
}

func (s *Session) storeCreateValue(ctx context.Context, arguments CreateArguments, value []byte) (string, error) {
	result, err := s.config.Operations.Create(ctx, arguments, value)
	return operationResult(SecretProposeCreate, result, err)
}

func (s *Session) Close(ctx context.Context, reason string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	s.cancel()
	if s.receipt.EndReason == "" {
		s.receipt.EndReason = reason
	}
	s.mu.Unlock()
	return nil
}

func (s *Session) Finalize(ctx context.Context, reason, cleanup string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalized {
		return nil
	}
	s.finalized = true
	s.closing = true
	s.cancel()
	if s.receipt.EndReason == "" {
		s.receipt.EndReason = reason
	}
	s.receipt.EndedAt, s.receipt.Cleanup = s.config.Now(), cleanup
	err := s.config.Receipt(ctx, s.receipt)
	s.closed = true
	return err
}

func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "manager-" + hex.EncodeToString(b), nil
}
func boundedLimit(v int) int {
	if v <= 0 {
		return 50
	}
	if v > 100 {
		return 100
	}
	return v
}
func preview(operation Operation, target string) string {
	return fmt.Sprintf("%s target=%s", operation, target)
}
func createPreview(arguments CreateArguments) string {
	return fmt.Sprintf("create protected credential\nreference: %s\nkind: %s\ndisclosure: protected\nvalue handling: current authenticated exact-local-model session when supplied conversationally, otherwise protected no-echo intake; encrypted authority persists, plaintext session state is purged on close", arguments.Reference, arguments.Kind)
}
func validateCreate(a CreateArguments) error {
	if !credentials.ValidateIdentifier(a.Reference) || !credentials.ValidateIdentifier(a.Kind) || a.Disclosure != "protected" {
		return errors.New(ReasonProposalInvalid)
	}
	return nil
}
func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}

func operationResult(operation Operation, value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	encoded, encodeErr := json.Marshal(value)
	if encodeErr != nil || len(encoded) > 64<<10 {
		return "", errors.New("manager operation result is invalid or oversized")
	}
	return fmt.Sprintf("Aegis authoritative result (%s): %s", operation, encoded), nil
}
