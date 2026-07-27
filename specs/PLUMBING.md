# Legacy Plumbing Specification

This filename is retained temporarily so existing repository links fail visibly rather than preserving the removed architecture.

The former participant-centric plumbing aggregate, universal validator, POC orchestrator, GraphRun readback, CLI command, and API routes are removed. They are not production models or compatibility surfaces.

The canonical replacement is [Canonical Domain Boundaries](CANONICAL_DOMAINS.md): bounded identity/authority, execution, evidence, session, and provisioning responsibilities linked by immutable IDs, digests, and fresh authoritative admission decisions.

Do not add new production imports of `internal/plumbing` or `internal/poc`, and do not reintroduce a cross-domain mutation aggregate under another name.
