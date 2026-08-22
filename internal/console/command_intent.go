package console

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/core"
)

var (
	ErrCommandUnknown  = errors.New("console command is not registered")
	ErrCommandConflict = errors.New("console command intent conflicts with retained state")
	ErrCommandExpired  = errors.New("console command intent expired")
	ErrCommandFailed   = errors.New("console command did not commit")
)

const (
	CommandCatalogVersion = "aegis.console-command-catalog.v1"
	CommandIntentTTLMax   = 2 * time.Minute
	CommandBodyBytesMax   = 64 << 10
	CommandResultBytesMax = 64 << 10
)

var commandIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
var commandOpaqueIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,255}$`)

// CommandDefinition is the closed, versioned server-side contract for one
// browser-accessible mutation. A page cannot enable a mutation unless its
// definition and implementation are registered together.
type CommandDefinition struct {
	ID                   string
	Version              string
	TargetType           string
	AuthorityRequirement string
	ConfirmationClass    string
	ReplayPolicy         string
	ResultType           string
	StableReasonCodes    []string
	Timeout              time.Duration
	MaxBodyBytes         int64
	Normalize            func(json.RawMessage) ([]byte, error)
	ResolveTarget        func(context.Context, string) (CommandTargetState, error)
	Commit               func(context.Context, CommandInvocation) (CommandReceipt, error)
}

type CommandTargetState struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Digest string `json:"digest"`
}

// CommandAuthorityBinding is obtained only from the controller-side authority
// provider. It is never decoded from a browser request.
type CommandAuthorityBinding struct {
	SubjectID       string
	SessionID       string
	StanzaID        string
	MandateID       string
	AuthorityID     string
	AuthorityDigest string
	Runtime         string
	ExpiresAt       time.Time
}

type CommandAuthorityProvider interface {
	ResolveCommandAuthority(context.Context, core.Subject, string, string) (CommandAuthorityBinding, error)
}

type CommandAuthorityProviderFunc func(context.Context, core.Subject, string, string) (CommandAuthorityBinding, error)

func (f CommandAuthorityProviderFunc) ResolveCommandAuthority(ctx context.Context, subject core.Subject, sessionID, requirement string) (CommandAuthorityBinding, error) {
	return f(ctx, subject, sessionID, requirement)
}

type CommandPreviewRequest struct {
	SchemaVersion  string          `json:"schema_version"`
	CommandID      string          `json:"command_id"`
	TargetID       string          `json:"target_id"`
	ExpectedDigest string          `json:"expected_digest"`
	IdempotencyKey string          `json:"idempotency_key"`
	Input          json.RawMessage `json:"input"`
}

type CommandPreview struct {
	SchemaVersion      string             `json:"schema_version"`
	IntentID           string             `json:"intent_id"`
	CommandID          string             `json:"command_id"`
	CommandVersion     string             `json:"command_version"`
	Target             CommandTargetState `json:"target"`
	InputDigest        string             `json:"input_digest"`
	ConfirmationClass  string             `json:"confirmation_class"`
	AuthorityState     string             `json:"authority_state"`
	AuthorityExpiresAt time.Time          `json:"authority_expires_at"`
	ExpiresAt          time.Time          `json:"expires_at"`
}

type CommandExecuteRequest struct {
	SchemaVersion string `json:"schema_version"`
	IntentID      string `json:"intent_id"`
}

type CommandInvocation struct {
	IntentID        string
	CommandID       string
	CommandVersion  string
	Subject         core.Subject
	SessionID       string
	Target          CommandTargetState
	NormalizedInput []byte
	InputDigest     string
	Authority       CommandAuthorityBinding
	RequestedAt     time.Time
}

// CommandReceipt is authoritative readback produced by the command's existing
// application service boundary. Commit must return only after its mutation and
// authoritative audit publication have both succeeded.
type CommandReceipt struct {
	SchemaVersion string             `json:"schema_version"`
	IntentID      string             `json:"intent_id"`
	CommandID     string             `json:"command_id"`
	Target        CommandTargetState `json:"target"`
	Outcome       string             `json:"outcome"`
	ReasonCode    string             `json:"reason_code,omitempty"`
	CommittedAt   time.Time          `json:"committed_at"`
	Readback      json.RawMessage    `json:"readback"`
}

type commandIntent struct {
	preview       CommandPreview
	requestDigest string
	idempotency   string
	subjectID     string
	sessionID     string
	normalized    []byte
	authority     CommandAuthorityBinding
	receipt       *CommandReceipt
	commitStarted bool
	commitErr     error
}

// CommandService owns the preview/confirmation state transition. The mutex is
// intentionally held across final admission and Commit so concurrent duplicate
// confirmations can never start two mutations. Registered Commit operations
// must be bounded by their definition timeout and must provide transactional
// mutation/audit semantics.
type CommandService struct {
	mu          sync.Mutex
	now         func() time.Time
	authority   CommandAuthorityProvider
	definitions map[string]CommandDefinition
	intents     map[string]*commandIntent
	idempotency map[string]string
}

func NewCommandService(definitions []CommandDefinition, authority CommandAuthorityProvider, now func() time.Time) (*CommandService, error) {
	if authority == nil || now == nil {
		return nil, fmt.Errorf("%w: authority provider and clock are required", ErrInvalidInput)
	}
	catalog := make(map[string]CommandDefinition, len(definitions))
	for _, definition := range definitions {
		if err := validateCommandDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := catalog[definition.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate command registration", ErrInvalidInput)
		}
		catalog[definition.ID] = definition
	}
	return &CommandService{now: now, authority: authority, definitions: catalog, intents: map[string]*commandIntent{}, idempotency: map[string]string{}}, nil
}

func validateCommandDefinition(value CommandDefinition) error {
	if !commandIdentifier.MatchString(value.ID) || !commandIdentifier.MatchString(value.Version) || !commandIdentifier.MatchString(value.TargetType) || !commandIdentifier.MatchString(value.AuthorityRequirement) || !commandIdentifier.MatchString(value.ConfirmationClass) || value.ReplayPolicy != "exactly-once-readback" || !commandIdentifier.MatchString(value.ResultType) || len(value.StableReasonCodes) == 0 || value.Timeout <= 0 || value.Timeout > 30*time.Second || value.MaxBodyBytes < 2 || value.MaxBodyBytes > CommandBodyBytesMax || value.Normalize == nil || value.ResolveTarget == nil || value.Commit == nil {
		return fmt.Errorf("%w: invalid command definition", ErrInvalidInput)
	}
	seenReasons := map[string]struct{}{}
	for _, reason := range value.StableReasonCodes {
		if !commandIdentifier.MatchString(reason) {
			return fmt.Errorf("%w: invalid command reason catalog", ErrInvalidInput)
		}
		if _, exists := seenReasons[reason]; exists {
			return fmt.Errorf("%w: duplicate command reason", ErrInvalidInput)
		}
		seenReasons[reason] = struct{}{}
	}
	return nil
}

func (s *CommandService) Preview(ctx context.Context, subject core.Subject, sessionID string, request CommandPreviewRequest) (CommandPreview, error) {
	if err := ctx.Err(); err != nil {
		return CommandPreview{}, err
	}
	definition, ok := s.definitions[request.CommandID]
	if !ok {
		return CommandPreview{}, ErrCommandUnknown
	}
	if request.SchemaVersion != CommandCatalogVersion || subject.ID == "" || sessionID == "" || !commandOpaqueIdentifier.MatchString(request.TargetID) || !validSHA256(request.ExpectedDigest) || !commandOpaqueIdentifier.MatchString(request.IdempotencyKey) || int64(len(request.Input)) > definition.MaxBodyBytes {
		return CommandPreview{}, ErrInvalidInput
	}
	if err := rejectBrowserAuthorityFields(request.Input); err != nil {
		return CommandPreview{}, ErrInvalidInput
	}
	normalized, err := definition.Normalize(append(json.RawMessage(nil), request.Input...))
	if err != nil || len(normalized) == 0 || int64(len(normalized)) > definition.MaxBodyBytes || !json.Valid(normalized) {
		return CommandPreview{}, ErrInvalidInput
	}
	inputDigest := digestBytes(normalized)
	requestDigest := digestStrings(definition.ID, definition.Version, request.TargetID, request.ExpectedDigest, inputDigest)
	idempotencyScope := digestStrings(subject.ID, sessionID, definition.ID, request.IdempotencyKey)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now())
	if intentID, exists := s.idempotency[idempotencyScope]; exists {
		intent := s.intents[intentID]
		if intent == nil || intent.requestDigest != requestDigest {
			return CommandPreview{}, ErrCommandConflict
		}
		return intent.preview, nil
	}

	target, err := definition.ResolveTarget(ctx, request.TargetID)
	if err != nil {
		return CommandPreview{}, err
	}
	if err = validateCommandTarget(target, definition.TargetType, request.TargetID, request.ExpectedDigest); err != nil {
		return CommandPreview{}, err
	}
	authority, err := s.authority.ResolveCommandAuthority(ctx, subject, sessionID, definition.AuthorityRequirement)
	if err != nil {
		return CommandPreview{}, err
	}
	now := s.now()
	if err = validateCommandAuthority(authority, subject, sessionID, now); err != nil {
		return CommandPreview{}, err
	}
	intentID, err := randomCommandID()
	if err != nil {
		return CommandPreview{}, err
	}
	expires := now.Add(CommandIntentTTLMax)
	if authority.ExpiresAt.Before(expires) {
		expires = authority.ExpiresAt
	}
	if subject.ExpiresAt.Before(expires) {
		expires = subject.ExpiresAt
	}
	preview := CommandPreview{SchemaVersion: CommandCatalogVersion, IntentID: intentID, CommandID: definition.ID, CommandVersion: definition.Version, Target: target, InputDigest: inputDigest, ConfirmationClass: definition.ConfirmationClass, AuthorityState: "admitted", AuthorityExpiresAt: authority.ExpiresAt, ExpiresAt: expires}
	s.intents[intentID] = &commandIntent{preview: preview, requestDigest: requestDigest, idempotency: idempotencyScope, subjectID: subject.ID, sessionID: sessionID, normalized: append([]byte(nil), normalized...), authority: authority}
	s.idempotency[idempotencyScope] = intentID
	return preview, nil
}

func (s *CommandService) Execute(ctx context.Context, subject core.Subject, sessionID string, request CommandExecuteRequest) (CommandReceipt, error) {
	if err := ctx.Err(); err != nil {
		return CommandReceipt{}, err
	}
	if request.SchemaVersion != CommandCatalogVersion || subject.ID == "" || sessionID == "" || !commandOpaqueIdentifier.MatchString(request.IntentID) {
		return CommandReceipt{}, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	intent := s.intents[request.IntentID]
	if intent == nil {
		return CommandReceipt{}, ErrCommandExpired
	}
	if intent.subjectID != subject.ID || intent.sessionID != sessionID {
		return CommandReceipt{}, ErrDenied
	}
	if intent.receipt != nil {
		return cloneCommandReceipt(*intent.receipt), nil
	}
	// A Commit invocation is terminal even when its caller loses cancellation,
	// repository, audit, or receipt-validation evidence. Retaining that outcome
	// prevents a retry from invoking a possibly consequential mutation again.
	if intent.commitStarted {
		if intent.commitErr != nil {
			return CommandReceipt{}, intent.commitErr
		}
		return CommandReceipt{}, ErrCommandFailed
	}
	now := s.now()
	if !now.Before(intent.preview.ExpiresAt) || !now.Before(subject.ExpiresAt) {
		return CommandReceipt{}, ErrCommandExpired
	}
	definition, ok := s.definitions[intent.preview.CommandID]
	if !ok || definition.Version != intent.preview.CommandVersion {
		return CommandReceipt{}, ErrCommandConflict
	}
	target, err := definition.ResolveTarget(ctx, intent.preview.Target.ID)
	if err != nil {
		return CommandReceipt{}, err
	}
	if target != intent.preview.Target {
		return CommandReceipt{}, ErrCommandConflict
	}
	authority, err := s.authority.ResolveCommandAuthority(ctx, subject, sessionID, definition.AuthorityRequirement)
	if err != nil {
		return CommandReceipt{}, err
	}
	if err = validateCommandAuthority(authority, subject, sessionID, now); err != nil {
		return CommandReceipt{}, err
	}
	if authority != intent.authority {
		return CommandReceipt{}, ErrDenied
	}
	intent.commitStarted = true
	commitCtx, cancel := context.WithTimeout(ctx, definition.Timeout)
	defer cancel()
	receipt, err := definition.Commit(commitCtx, CommandInvocation{IntentID: request.IntentID, CommandID: definition.ID, CommandVersion: definition.Version, Subject: subject, SessionID: sessionID, Target: target, NormalizedInput: append([]byte(nil), intent.normalized...), InputDigest: intent.preview.InputDigest, Authority: authority, RequestedAt: now})
	if err != nil {
		intent.commitErr = fmt.Errorf("%w: %v", ErrCommandFailed, err)
		return CommandReceipt{}, intent.commitErr
	}
	if err = validateCommandReceipt(receipt, definition, request.IntentID, target, now); err != nil {
		intent.commitErr = err
		return CommandReceipt{}, intent.commitErr
	}
	stored := cloneCommandReceipt(receipt)
	intent.receipt = &stored
	return cloneCommandReceipt(stored), nil
}

func (s *CommandService) pruneLocked(now time.Time) {
	for id, intent := range s.intents {
		if intent.receipt == nil && !now.Before(intent.preview.ExpiresAt) {
			delete(s.intents, id)
			delete(s.idempotency, intent.idempotency)
		}
	}
}

func validateCommandTarget(target CommandTargetState, targetType, targetID, expectedDigest string) error {
	if target.ID != targetID || target.Type != targetType || !validSHA256(target.Digest) {
		return ErrCommandConflict
	}
	if target.Digest != expectedDigest {
		return ErrCommandConflict
	}
	return nil
}

func validateCommandAuthority(value CommandAuthorityBinding, subject core.Subject, sessionID string, now time.Time) error {
	if value.SubjectID != subject.ID || value.SessionID != sessionID || !commandOpaqueIdentifier.MatchString(value.StanzaID) || !commandOpaqueIdentifier.MatchString(value.MandateID) || !commandOpaqueIdentifier.MatchString(value.AuthorityID) || !validSHA256(value.AuthorityDigest) || !commandOpaqueIdentifier.MatchString(value.Runtime) || !now.Before(value.ExpiresAt) || value.ExpiresAt.After(subject.ExpiresAt) {
		return ErrDenied
	}
	return nil
}

func validateCommandReceipt(value CommandReceipt, definition CommandDefinition, intentID string, target CommandTargetState, requestedAt time.Time) error {
	if value.SchemaVersion != CommandCatalogVersion || value.IntentID != intentID || value.CommandID != definition.ID || value.Target != target || value.Outcome != "committed" || value.CommittedAt.Before(requestedAt) || len(value.Readback) == 0 || len(value.Readback) > CommandResultBytesMax || !json.Valid(value.Readback) {
		return ErrCommandFailed
	}
	if value.ReasonCode != "" {
		known := false
		for _, reason := range definition.StableReasonCodes {
			known = known || reason == value.ReasonCode
		}
		if !known {
			return ErrCommandFailed
		}
	}
	return nil
}

func cloneCommandReceipt(value CommandReceipt) CommandReceipt {
	value.Readback = append(json.RawMessage(nil), value.Readback...)
	return value
}

func validSHA256(value string) bool {
	if len(value) != 71 || value[:7] != "sha256:" {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func digestStrings(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = io.WriteString(hash, fmt.Sprintf("%d:", len(value)))
		_, _ = io.WriteString(hash, value)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func randomCommandID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate command intent: %w", err)
	}
	return "intent-" + hex.EncodeToString(value[:]), nil
}

// DecodeCommandRequest strictly decodes one bounded JSON object. It rejects
// malformed UTF-8, duplicate keys at any object depth, unknown fields, and
// trailing data before returning either supported browser request type.
func DecodeCommandRequest(data []byte, destination any) error {
	if len(data) == 0 || len(data) > CommandBodyBytesMax || !json.Valid(data) {
		return ErrInvalidInput
	}
	if err := rejectDuplicateCommandKeys(data); err != nil {
		return ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidInput
	}
	return nil
}

// DecodeCommandForm gives native forms the same closed request models as JSON.
// CSRF is transported in the header by the HTTP adapter and is intentionally
// absent here so it cannot enter retained intent or audit data.
func DecodeCommandForm(data []byte, destination any) error {
	if len(data) == 0 || len(data) > CommandBodyBytesMax || !utf8.Valid(data) {
		return ErrInvalidInput
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return ErrInvalidInput
	}
	single := func(key string) (string, bool) {
		value, ok := values[key]
		return strings.Join(value, ""), ok && len(value) == 1
	}
	switch request := destination.(type) {
	case *CommandPreviewRequest:
		if len(values) != 6 {
			return ErrInvalidInput
		}
		var ok bool
		if request.SchemaVersion, ok = single("schema_version"); !ok {
			return ErrInvalidInput
		}
		if request.CommandID, ok = single("command_id"); !ok {
			return ErrInvalidInput
		}
		if request.TargetID, ok = single("target_id"); !ok {
			return ErrInvalidInput
		}
		if request.ExpectedDigest, ok = single("expected_digest"); !ok {
			return ErrInvalidInput
		}
		if request.IdempotencyKey, ok = single("idempotency_key"); !ok {
			return ErrInvalidInput
		}
		input, ok := single("input")
		if !ok || !json.Valid([]byte(input)) {
			return ErrInvalidInput
		}
		request.Input = json.RawMessage(input)
	case *CommandExecuteRequest:
		if len(values) != 2 {
			return ErrInvalidInput
		}
		var ok bool
		if request.SchemaVersion, ok = single("schema_version"); !ok {
			return ErrInvalidInput
		}
		if request.IntentID, ok = single("intent_id"); !ok {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}

func rejectBrowserAuthorityFields(data []byte) error {
	if len(data) == 0 || !json.Valid(data) || rejectDuplicateCommandKeys(data) != nil {
		return ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	forbidden := map[string]struct{}{
		"actor": {}, "authority": {}, "stanza": {}, "mandate": {}, "runtime": {},
		"audit": {}, "disposition": {}, "success": {},
	}
	var inspect func(any) error
	inspect = func(candidate any) error {
		switch typed := candidate.(type) {
		case map[string]any:
			for key, child := range typed {
				normalizedKey := strings.ReplaceAll(strings.ToLower(key), "-", "_")
				for deniedKey := range forbidden {
					if normalizedKey == deniedKey || strings.HasPrefix(normalizedKey, deniedKey+"_") || strings.HasSuffix(normalizedKey, "_"+deniedKey) {
						return ErrInvalidInput
					}
				}
				if err := inspect(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range typed {
				if err := inspect(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return inspect(value)
}

func rejectDuplicateCommandKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrInvalidInput
				}
				if _, exists := seen[key]; exists {
					return ErrInvalidInput
				}
				seen[key] = struct{}{}
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err = walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return ErrInvalidInput
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidInput
	}
	return nil
}
