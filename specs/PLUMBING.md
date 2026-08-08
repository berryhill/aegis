# Legacy Plumbing Specification

This filename is retained temporarily so existing repository links fail visibly rather than preserving the removed architecture.

The former participant-centric plumbing aggregate, universal validator, POC orchestrator, GraphRun readback, CLI command, and API routes are removed. They are not production models or compatibility surfaces.

The canonical replacement is [Canonical Domain Boundaries](CANONICAL_DOMAINS.md): bounded Registry, Loop, Graph, Queue, identity/authority, execution, evidence, disposition, session, and provisioning responsibilities linked by immutable IDs, digests, and fresh authoritative admission decisions.

Do not add new production imports of `internal/plumbing` or `internal/poc`, and do not reintroduce a cross-domain mutation aggregate under another name.

The removed GraphRun surface does not mean Graph execution is deferred. The MVI requires a new bounded Graph domain and separate Graph-run, Loop-execution, attempt, and queue facts. They reference exact immutable revisions and authority digests; they do not restore the former universal aggregate or its compatibility routes.
