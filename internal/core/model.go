package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = "aegis.dev/v1alpha1"

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

type RuntimeConstraint struct {
	Adapter           string `json:"adapter"`
	Runtime           string `json:"runtime"`
	VersionConstraint string `json:"version_constraint"`
	Target            string `json:"target"`
}
type IdentitySelector struct {
	Kinds        []string          `json:"kinds"`
	SubjectIDs   []string          `json:"subject_ids"`
	PrincipalIDs []string          `json:"principal_ids"`
	Issuers      []string          `json:"issuers"`
	Claims       map[string]string `json:"claims"`
	Environments []string          `json:"environments"`
}
type AuthenticationPolicy struct {
	Methods       []string           `json:"methods"`
	Selectors     []IdentitySelector `json:"selectors"`
	RequireFresh  bool               `json:"require_fresh"`
	MaxAuthAgeSec int64              `json:"max_auth_age_seconds"`
}
type Grant struct {
	Capabilities []string `json:"capabilities"`
	Tools        []string `json:"tools"`
}
type Scopes struct {
	Memory      []string `json:"memory"`
	Credentials []string `json:"credentials"`
}
type SessionPolicy struct {
	MaximumLifetimeSec int64 `json:"maximum_lifetime_seconds"`
	IdleTimeoutSec     int64 `json:"idle_timeout_seconds"`
	RequireReauth      bool  `json:"require_reauth"`
	Delegation         bool  `json:"delegation"`
}
type ApprovalPolicy struct {
	RequiredOperations []string `json:"required_operations"`
	MaximumLifetimeSec int64    `json:"maximum_lifetime_seconds"`
	SingleUse          bool     `json:"single_use"`
}
type InformationFlowPolicy struct {
	CrossStanza string `json:"cross_stanza"`
}
type HermesConfig struct {
	Profile        string   `json:"profile"`
	PersistentHome bool     `json:"persistent_home"`
	MCPServers     []string `json:"mcp_servers"`
	Plugins        []string `json:"plugins"`
	Toolsets       []string `json:"toolsets"`
	Model          string   `json:"model"`
	Provider       string   `json:"provider"`
}
type TrustStanza struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Enabled         bool                  `json:"enabled"`
	Authentication  AuthenticationPolicy  `json:"authentication"`
	Grant           Grant                 `json:"grant"`
	Scopes          Scopes                `json:"scopes"`
	Session         SessionPolicy         `json:"session"`
	Approval        ApprovalPolicy        `json:"approval"`
	InformationFlow InformationFlowPolicy `json:"information_flow"`
	Hermes          HermesConfig          `json:"hermes"`
}
type Charter struct {
	SchemaVersion string            `json:"schema_version"`
	AgentID       string            `json:"agent_id"`
	Name          string            `json:"name"`
	Revision      uint64            `json:"revision"`
	Runtime       RuntimeConstraint `json:"runtime"`
	Stanzas       []TrustStanza     `json:"stanzas"`
	CreatedBy     string            `json:"created_by"`
	CreatedAt     time.Time         `json:"created_at"`
}
type CanonicalCharter struct {
	Charter   Charter         `json:"charter"`
	Digest    string          `json:"digest"`
	Canonical json.RawMessage `json:"canonical"`
}

type Subject struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"`
	PrincipalID     string            `json:"principal_id,omitempty"`
	Issuer          string            `json:"issuer"`
	Method          string            `json:"method"`
	AuthenticatedAt time.Time         `json:"authenticated_at"`
	ExpiresAt       time.Time         `json:"expires_at"`
	Claims          map[string]string `json:"claims,omitempty"`
}
type RuntimeDescriptor struct {
	Name           string   `json:"name"`
	Runtime        string   `json:"runtime"`
	Version        string   `json:"version"`
	Executable     string   `json:"executable"`
	Installation   string   `json:"installation"`
	AdapterVersion string   `json:"adapter_version"`
	Capabilities   []string `json:"capabilities"`
}
type Environment struct {
	Name   string `json:"name"`
	Host   string `json:"host,omitempty"`
	Tenant string `json:"tenant,omitempty"`
}
type Decision struct {
	Allowed       bool           `json:"allowed"`
	Selected      *TrustStanza   `json:"selected,omitempty"`
	MatchingCount int            `json:"matching_count"`
	Reason        string         `json:"reason"`
	TrustedInputs map[string]any `json:"trusted_inputs"`
}
type Mandate struct {
	ID               string            `json:"id"`
	Subject          Subject           `json:"subject"`
	AgentID          string            `json:"agent_id"`
	StanzaID         string            `json:"stanza_id"`
	CharterRevision  uint64            `json:"charter_revision"`
	CharterDigest    string            `json:"charter_digest"`
	Runtime          RuntimeDescriptor `json:"runtime"`
	Target           string            `json:"target"`
	Capabilities     []string          `json:"capabilities"`
	Tools            []string          `json:"tools"`
	Scopes           Scopes            `json:"scopes"`
	Hermes           HermesConfig      `json:"hermes"`
	IssuedAt         time.Time         `json:"issued_at"`
	ExpiresAt        time.Time         `json:"expires_at"`
	RevokedAt        *time.Time        `json:"revoked_at,omitempty"`
	RevocationReason string            `json:"revocation_reason,omitempty"`
}
type Session struct {
	ID                  string     `json:"id"`
	Mandate             Mandate    `json:"mandate"`
	RuntimeSessionID    string     `json:"runtime_session_id"`
	RuntimePID          int        `json:"runtime_pid,omitempty"`
	ProcessStart        string     `json:"process_start,omitempty"`
	RuntimeHome         string     `json:"runtime_home"`
	VerifiedToolsets    []string   `json:"verified_toolsets"`
	ToolsetVerification string     `json:"toolset_verification"`
	Status              string     `json:"status"`
	StartedAt           time.Time  `json:"started_at"`
	EndedAt             *time.Time `json:"ended_at,omitempty"`
	EndReason           string     `json:"end_reason,omitempty"`
}
type Effect struct {
	Kind          string `json:"kind"`
	Target        string `json:"target"`
	Description   string `json:"description"`
	Consequential bool   `json:"consequential"`
	Digest        string `json:"digest"`
}

const (
	EffectCreateFile          = "create_file"
	EffectModifyFile          = "modify_file"
	EffectCreateHermesProfile = "create_hermes_profile"
	EffectConfigureMCP        = "configure_mcp"
	EffectConfigurePlugin     = "configure_plugin"
	EffectStartGateway        = "start_gateway"
	EffectInstallService      = "install_service"
	EffectCreateCron          = "create_cron"
	EffectExternalNetwork     = "external_network"
)

type Plan struct {
	ID            string            `json:"id"`
	AgentID       string            `json:"agent_id"`
	Revision      uint64            `json:"revision"`
	CharterDigest string            `json:"charter_digest"`
	Runtime       RuntimeDescriptor `json:"runtime"`
	Environment   Environment       `json:"environment"`
	Effects       []Effect          `json:"effects"`
	CreatedAt     time.Time         `json:"created_at"`
	Digest        string            `json:"digest"`
}

func PlanDigest(plan Plan) string {
	plan.Digest = ""
	return Digest(plan)
}

type Review struct {
	Summary              string                    `json:"summary"`
	Diff                 string                    `json:"diff"`
	Warnings             []string                  `json:"warnings"`
	CharterDigest        string                    `json:"charter_digest"`
	PlanDigest           string                    `json:"plan_digest"`
	RequestedToolsets    map[string][]string       `json:"requested_toolsets"`
	CredentialScopes     map[string][]string       `json:"credential_scopes"`
	MemoryScopes         map[string][]string       `json:"memory_scopes"`
	ApprovalRequirements map[string]ApprovalPolicy `json:"approval_requirements"`
	Plan                 Plan                      `json:"plan"`
}
type Approval struct {
	ID             string      `json:"id"`
	PlanDigest     string      `json:"plan_digest"`
	CharterDigest  string      `json:"charter_digest"`
	Runtime        string      `json:"runtime"`
	RuntimeVersion string      `json:"runtime_version"`
	Environment    Environment `json:"environment"`
	Status         string      `json:"status"`
	RequestedBy    string      `json:"requested_by"`
	ApprovedBy     string      `json:"approved_by,omitempty"`
	RequestedAt    time.Time   `json:"requested_at"`
	DecidedAt      *time.Time  `json:"decided_at,omitempty"`
	ExpiresAt      time.Time   `json:"expires_at"`
	ConsumedAt     *time.Time  `json:"consumed_at,omitempty"`
}
type Artifact struct {
	Path     string `json:"path"`
	Action   string `json:"action"`
	Digest   string `json:"digest"`
	Verified bool   `json:"verified"`
}
type Receipt struct {
	ID            string     `json:"id"`
	PlanID        string     `json:"plan_id"`
	ApprovalID    string     `json:"approval_id"`
	CharterDigest string     `json:"charter_digest"`
	Status        string     `json:"status"`
	Artifacts     []Artifact `json:"artifacts"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    time.Time  `json:"finished_at"`
	Failure       string     `json:"failure,omitempty"`
}
type AuditEvent struct {
	ID              string            `json:"id"`
	Type            string            `json:"type"`
	OccurredAt      time.Time         `json:"occurred_at"`
	SubjectID       string            `json:"subject_id,omitempty"`
	PrincipalID     string            `json:"principal_id,omitempty"`
	AgentID         string            `json:"agent_id,omitempty"`
	StanzaID        string            `json:"stanza_id,omitempty"`
	SessionID       string            `json:"session_id,omitempty"`
	MandateID       string            `json:"mandate_id,omitempty"`
	Runtime         string            `json:"runtime,omitempty"`
	CharterRevision uint64            `json:"charter_revision,omitempty"`
	CharterDigest   string            `json:"charter_digest,omitempty"`
	ApprovalID      string            `json:"approval_id,omitempty"`
	ProvisioningID  string            `json:"provisioning_id,omitempty"`
	Outcome         string            `json:"outcome"`
	Reason          string            `json:"reason"`
	PreviousDigest  string            `json:"previous_digest"`
	EventDigest     string            `json:"event_digest"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func DecodeCharter(r io.Reader) (Charter, error) {
	var c Charter
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, fmt.Errorf("decode charter: %w", err)
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return c, errors.New("decode charter: trailing data")
	}
	return c, ValidateCharter(c)
}
func Canonicalize(c Charter) (CanonicalCharter, error) {
	if err := ValidateCharter(c); err != nil {
		return CanonicalCharter{}, err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return CanonicalCharter{}, err
	}
	h := sha256.Sum256(b)
	return CanonicalCharter{Charter: c, Digest: "sha256:" + hex.EncodeToString(h[:]), Canonical: b}, nil
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func ValidateCharter(c Charter) error {
	var es []string
	add := func(s string) { es = append(es, s) }
	if c.SchemaVersion != SchemaVersion {
		add("schema_version must be " + SchemaVersion)
	}
	if !idPattern.MatchString(c.AgentID) {
		add("agent_id is invalid")
	}
	if strings.TrimSpace(c.Name) == "" {
		add("name is required")
	}
	if c.Revision == 0 {
		add("revision must be positive")
	}
	if c.CreatedBy == "" || c.CreatedAt.IsZero() {
		add("creation identity and timestamp are required")
	}
	if c.Runtime.Adapter != "hermes" || c.Runtime.Runtime != "hermes-agent" || c.Runtime.VersionConstraint == "" || c.Runtime.Target == "" {
		add("explicit Hermes runtime, version constraint, and target are required")
	}
	if len(c.Stanzas) == 0 {
		add("at least one stanza is required")
	}
	seen := map[string]bool{}
	type ownedSelector struct {
		stanza  string
		methods []string
		value   IdentitySelector
	}
	var enabledSelectors []ownedSelector
	for i, s := range c.Stanzas {
		p := fmt.Sprintf("stanzas[%d]", i)
		if !idPattern.MatchString(s.ID) || seen[s.ID] {
			add(p + " has invalid or duplicate id")
		}
		seen[s.ID] = true
		if strings.TrimSpace(s.Name) == "" {
			add(p + " name required")
		}
		if len(s.Authentication.Methods) == 0 || len(s.Authentication.Selectors) == 0 {
			add(p + " authentication must be explicit")
		}
		for _, selector := range s.Authentication.Selectors {
			if len(selector.Kinds) == 0 && len(selector.SubjectIDs) == 0 && len(selector.PrincipalIDs) == 0 && len(selector.Issuers) == 0 && len(selector.Claims) == 0 && len(selector.Environments) == 0 {
				add(p + " contains an implicit wildcard identity selector")
			}
			if s.Enabled {
				for _, previous := range enabledSelectors {
					if previous.stanza != s.ID && selectorsOverlap(previous.methods, previous.value, s.Authentication.Methods, selector) {
						add(p + " has a selector that overlaps enabled stanza " + previous.stanza)
					}
				}
				enabledSelectors = append(enabledSelectors, ownedSelector{stanza: s.ID, methods: append([]string(nil), s.Authentication.Methods...), value: selector})
			}
		}
		if s.Authentication.RequireFresh && s.Authentication.MaxAuthAgeSec <= 0 {
			add(p + " fresh authentication age required")
		}
		if s.Session.MaximumLifetimeSec <= 0 || s.Session.MaximumLifetimeSec > 86400 || s.Session.Delegation {
			add(p + " session lifetime invalid or delegation enabled")
		}
		if s.InformationFlow.CrossStanza != "deny" {
			add(p + " cross-stanza flow must be deny")
		}
		if !s.Approval.SingleUse || s.Approval.MaximumLifetimeSec <= 0 {
			add(p + " approval must be finite and single-use")
		}
		if strings.TrimSpace(s.Hermes.Provider) == "" || strings.TrimSpace(s.Hermes.Model) == "" {
			add(p + " Hermes provider and model must be explicit")
		}
		expectedCredential := "provider:" + s.Hermes.Provider
		if len(s.Scopes.Credentials) != 1 || s.Scopes.Credentials[0] != expectedCredential {
			add(p + " credential scope must contain only " + expectedCredential + " in the MVP")
		}
		for _, x := range append(append([]string{}, s.Grant.Tools...), s.Grant.Capabilities...) {
			if strings.TrimSpace(x) == "" || x == "*" || strings.EqualFold(x, "all") {
				add(p + " contains empty or wildcard authority")
			}
		}
		grantTools, runtimeTools := append([]string(nil), s.Grant.Tools...), append([]string(nil), s.Hermes.Toolsets...)
		sort.Strings(grantTools)
		sort.Strings(runtimeTools)
		if strings.Join(grantTools, "\x00") != strings.Join(runtimeTools, "\x00") {
			add(p + " Hermes toolsets must exactly equal granted tools")
		}
		for _, x := range append(append([]string{}, s.Scopes.Memory...), s.Scopes.Credentials...) {
			if strings.TrimSpace(x) == "" || x == "*" || strings.EqualFold(x, "all") {
				add(p + " contains empty or wildcard scope")
			}
		}
		for _, x := range append(append([]string{}, s.Hermes.MCPServers...), s.Hermes.Plugins...) {
			if strings.TrimSpace(x) == "" || strings.Contains(x, "*") {
				add(p + " contains invalid runtime extension")
			}
		}
	}
	if len(es) > 0 {
		sort.Strings(es)
		return errors.New(strings.Join(es, "; "))
	}
	return nil
}
func EqualCanonical(a, b CanonicalCharter) bool {
	return a.Digest == b.Digest && bytes.Equal(a.Canonical, b.Canonical)
}

// selectorsOverlap reports overlaps determinable from finite equality
// selectors. Empty dimensions are unconstrained. Runtime selection still
// fails closed if a future selector form cannot be proven disjoint here.
func selectorsOverlap(aMethods []string, a IdentitySelector, bMethods []string, b IdentitySelector) bool {
	if !stringSetsIntersect(aMethods, bMethods) ||
		!stringSetsCompatible(a.Kinds, b.Kinds) ||
		!stringSetsCompatible(a.SubjectIDs, b.SubjectIDs) ||
		!stringSetsCompatible(a.PrincipalIDs, b.PrincipalIDs) ||
		!stringSetsCompatible(a.Issuers, b.Issuers) ||
		!stringSetsCompatible(a.Environments, b.Environments) {
		return false
	}
	for key, av := range a.Claims {
		if bv, ok := b.Claims[key]; ok && av != bv {
			return false
		}
	}
	return true
}

func stringSetsCompatible(a, b []string) bool {
	return len(a) == 0 || len(b) == 0 || stringSetsIntersect(a, b)
}

func stringSetsIntersect(a, b []string) bool {
	for _, av := range a {
		for _, bv := range b {
			if av == bv {
				return true
			}
		}
	}
	return false
}
