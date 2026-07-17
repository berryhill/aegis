# Identity and Authorization Specification

## Principal authentication

The MVP has one explicitly configured principal. Authority is established outside the model from the configured local OS identity or Linux Unix-socket peer credentials.

Prompts, display names, model conclusions, bearer tokens, requested stanza names, and CLI flags are never identity evidence. Missing, expired, incomplete, or ambiguous authentication fails closed.

## Trust-stanza selection

Every operational session binds to exactly one stanza. Selection uses only authenticated subject data, trusted environment data, the canonical charter, and an optional requested stanza treated as a restriction—not as authorization.

The selector result is deterministic:

- zero authorized matches: deny;
- exactly one authorized match: select it;
- more than one match: deny as ambiguous.

Aegis never unions grants across stanzas. Stanza names are metadata. Changing stanza requires a clean new session. Delegation, inheritance, transitive trust, and model-selected policy are excluded from the MVP.

## Effective authority

The selected stanza independently determines capabilities, Hermes toolsets, memory scopes, credential scopes, session lifetime, and approval requirements. An operational launch receives only the explicitly selected provider credential binding. Ambient provider credentials are removed from the child environment.

Authorization decisions and denials are emitted by Aegis with machine-readable reasons.
