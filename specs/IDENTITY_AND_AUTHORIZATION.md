# Identity and Authorization Specification

## Principal authentication

The MVP has one explicitly configured principal. Authority is established outside the model from the configured local OS identity or Linux Unix-socket peer credentials.

Prompts, display names, profile names, model conclusions, bearer tokens, requested stanza names, and CLI flags are never identity evidence. Missing, expired, incomplete, or ambiguous authentication fails closed.

## Trust-stanza selection

Every operational session binds to exactly one stanza. Selection uses only authenticated subject data, trusted environment data, the canonical charter, and an optional requested stanza treated as a restriction—not as authorization.

The MVP's only trusted operational environment is `local`, established by the control plane. CLI flags and API request bodies can narrow by supplying `local` or cause denial, but cannot establish another trusted environment or satisfy a non-local selector.

The selector result is deterministic:

- zero authorized matches: deny;
- exactly one authorized match: select it;
- more than one match: deny as ambiguous.

Aegis never unions grants across stanzas. Trust stanzas are security contexts, not personalities, and stanza names are metadata. Changing stanza or materially changing effective authority requires a newly issued mandate and a clean new session. Runtime-authority delegation, inheritance, transitive trust, and model-selected policy are excluded from the MVP. The bounded registered-Agent workspace delegation below is a separate controller-issued control-plane capability and never delegates stanza or runtime authority.

Enabled-selector overlap is rejected when a charter is validated. Selection still implements defense-in-depth for legacy input: zero matches returns `zero_authorized_matches`, multiple matches returns `multiple_authorized_matches`, and an unauthorized narrowing request returns `requested_stanza_unauthorized`. Expired, stale, malformed, disabled, wrong-method, wrong-issuer, and wrong-environment input never selects a stanza.

## Effective authority

The selected stanza independently determines capabilities, Hermes toolsets, memory scopes, credential scopes, session lifetime, and approval requirements. Effective-authority inspection itself requires authenticated selection and projects only those fields and the selected stanza ID; it does not expose or combine another stanza. An operational launch receives only the explicitly selected provider credential binding. Ambient provider credentials are removed from the child environment.

Authorization decisions and denials are emitted by Aegis with machine-readable reasons.

## Registered-Agent workspace authority

After fresh principal authentication, Aegis may issue `aegis.workspace-authority.v1` for one exact latest enabled Agent revision. The sealed record binds principal ID, Agent ID/revision/digest, stable owner ID, the fixed workspace capability set, and its digest. It is valid only while all bindings still match authoritative Registry state; it cannot follow `latest`, survive disablement or ownership drift, or be inferred from a profile, prompt, display name, receipt, session, or model output.

This control-plane delegation permits own-Loop/Graph definition and lifecycle management, exact-participant Graph submission, own-Queue management, and fleet-wide shared definition reads. It does not select a stanza, issue a mandate, create a session, prove provisioning, admit a claim or runtime effect, or grant credential access. Processing requires a separately issued fresh runtime-authority binding and normal admission. Credential authority remains exclusively with the authenticated Aegis controller.
