// Package specs defines the normative Go contracts for the Aegis MVP.
//
// Aegis binds an authenticated subject to exactly one trust stanza and one
// explicit agent runtime for the lifetime of a session. The package contains
// domain types, service interfaces, and executable structural invariants; it
// intentionally contains no runtime, storage, authentication, or transport
// implementation.
//
// Feature mapping to MVP_FEATURE_SET.md:
//
//   - Principal authentication: Authenticator, PrincipalAuthorizer
//   - Explicit runtime selection: RuntimeRegistry, RuntimeAdapter
//   - Dedicated design session: Designer, DesignSession
//   - Canonical agent charter: CharterCodec, CharterRepository, ValidateCharter
//   - One-to-many trust stanzas: TrustStanza
//   - Deterministic stanza selection: StanzaSelector
//   - Authenticated session mandate: MandateIssuer, ValidateMandate
//   - Clean per-stanza runtime launch: RuntimeAdapter.Launch
//   - Capability restriction: CapabilityResolver, EffectiveCapabilities
//   - Exact charter approval: ApprovalService
//   - Deterministic provisioning: Provisioner
//   - Session startup and visibility: SessionService, SessionPreview
//   - Basic audit trail: AuditSink, AuditReader
//   - Inspection and validation: Inspector
//   - Go application foundation: Services and transport-neutral contracts
//   - Security invariants: invariants.go and invariants_test.go
package specs
