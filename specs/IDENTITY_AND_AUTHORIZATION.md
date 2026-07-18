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

Aegis never unions grants across stanzas. Trust stanzas are security contexts, not personalities, and stanza names are metadata. Changing stanza or materially changing effective authority requires a newly issued mandate and a clean new session. Delegation, inheritance, transitive trust, and model-selected policy are excluded from the MVP.

Enabled-selector overlap is rejected when a charter is validated. Selection still implements defense-in-depth for legacy input: zero matches returns `zero_authorized_matches`, multiple matches returns `multiple_authorized_matches`, and an unauthorized narrowing request returns `requested_stanza_unauthorized`. Expired, stale, malformed, disabled, wrong-method, wrong-issuer, and wrong-environment input never selects a stanza.

## Effective authority

The selected stanza independently determines capabilities, Hermes toolsets, memory scopes, credential scopes, session lifetime, and approval requirements. Effective-authority inspection itself requires authenticated selection and projects only those fields and the selected stanza ID; it does not expose or combine another stanza. An operational launch receives only the explicitly selected provider credential binding. Ambient provider credentials are removed from the child environment.

Authorization decisions and denials are emitted by Aegis with machine-readable reasons.
