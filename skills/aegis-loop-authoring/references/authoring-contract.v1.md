# Portable Loop authoring contract v1

This reference and `hello-draft.v1.json` require only the installed bundle and
an Aegis executable advertising `loops hello`. They apply to the bundle's exact
source revision; installation does not upgrade an older executable. All example
identities are synthetic: replace the Agent selector with an intended existing
enabled registered Agent, and choose the exact owned Loop/publication identity
before authoring. Do not submit a fixture unchanged or invent an authority.

From this installed skill directory, the local, non-authorizing builder is:

```text
aegis loops hello references/hello-draft.v1.json --output NEW_PUBLICATION_FILE
```

The output path must not exist. The builder strictly decodes `agent_id`,
`loop_id`, positive `revision`, `idempotency_key` and optional `previous_digest`.
For revision 1 omit the predecessor; for revision N use the exact N-1 digest.
It writes a complete `PublishLoopInput` with a canonical v2 revision, typed
`task` and `acceptance_criteria` string inputs, a `report` string output, and
precommitted exact UTF-8 `hello` (no newline) artifact evidence. The generated
publication contains server-resolved `agent_id`, not caller-authored runtime
`authority` or `publisher`. The local builder does not resolve that selector,
authenticate, publish, activate, run, or produce a verification receipt.

For explicitly requested publication, use the existing owning service:

```text
aegis --config OWNER_CONFIG --target CONSOLE_URL loops validate NEW_PUBLICATION_FILE
aegis --config OWNER_CONFIG --target CONSOLE_URL loops publish NEW_PUBLICATION_FILE
aegis --config OWNER_CONFIG --target CONSOLE_URL loops show LOOP_ID REVISION
```

`OWNER_CONFIG` and `CONSOLE_URL` are operator-supplied instance selectors, not
credentials. Do not create configuration, copy authentication material, stop the
daemon, open another writer or construct a client. Without the installed online
adapter, report it unavailable. Workspace publication needs no runtime session,
provisioning receipt or credentials; the authenticated service derives the exact
latest enabled Agent/stable-owner authority. Runtime publication instead requires
current authenticated `authority` and `publisher` references and omits `agent_id`;
the local workspace recipe never issues them.

Publish returns `revision`, `validation`, and `decision`; this is
an envelope description, not a complete wire response. Exact `loops show` returns
the immutable revision with validation, provenance and lifecycle. Match the
returned revision and validation digests and publisher provenance. Publication
is inactive and not execution. Activation is a separate authorized lifecycle
operation; use the primary skill's exact CAS procedure, never infer activation
from a generated digest or a successful validation.

The paired Graph publication example in the Graph skill uses this builder's
port interface, but its Agent/Loop digests are placeholders. Replace every exact
reference using authenticated readback **after** explicit publication/activation.
A Graph is not required for direct Loop Run. For creation-and-execution use the
primary skill's retained `task-execute` path instead of manually orchestrating
these authoring-only examples. Unknown outcomes require exact readback with the
same publication identity, not new IDs or repeated setup.

`TestPortableBundledAuthoringContracts` installs the verified archive into an
isolated home, consumes this input through the public local builder/decoder and
compares its ports with the bundled Graph candidate. This is executable
schema/builder/packaging evidence, not service admission, actual agent behavior,
live-provider qualification or public release/install evidence.
