# Charter Specification

## Purpose

A charter is the canonical, immutable, versioned source of truth for one logical agent. Conversation transcripts and model narration are not authority.

## Required document

A charter contains:

- schema version;
- stable agent ID, display name, and monotonically increasing revision;
- explicit Hermes adapter, runtime, supported version constraint, and target;
- one or more independently scoped trust stanzas;
- authenticated creator identity and creation timestamp.

Each stanza contains a stable ID, metadata name, enabled state, authentication policy and selectors, capabilities, Hermes toolsets, memory scopes, credential scopes, session limits, approval policy, information-flow policy, and Hermes model/provider mapping.

## Validation

Aegis MUST:

- decode strict JSON and reject unknown fields or trailing input;
- reject empty, duplicate, wildcard, unsupported, inherited, delegated, or ambiguous authority;
- require explicit authentication selectors and a supported authentication method;
- require `grant.tools` to match the Hermes toolset mapping exactly;
- deny cross-stanza information flow in the MVP;
- reject persistent homes, ambient MCP/plugins, unsupported runtime extensions, and unsafe implicit defaults;
- serialize the typed charter deterministically and display its SHA-256 digest.

A charter revision is immutable. Reusing an agent/revision with different canonical content MUST fail.

## Design boundary

Hermes may propose charter bytes through the isolated design protocol. Only Aegis validates, canonicalizes, digests, and persists them. Model output cannot approve or provision a charter.
