# Issue-derived repository engineering (source candidate)

A v6 contribution profile may opt into `engineering` with directory
`writable_prefixes` and `max_writable_files` (2–64), managed kind
`go-repository-engineering.v1`, and final policy `aegis.contribution.final.v1`.
This changes neither the one-URL manager invocation nor exact published-revision
host authorization. Legacy fixed-file profiles keep their existing semantics.

## URL-only invocation and definition-time policy

The engineering successor declares one required input, `github_issue_url`.
`aegis loops contribution FILE --output NEW_FILE` builds a local publication
proposal; authenticated validation, publication and activation remain separate.
Its authoring input fixes the repository, exact participants, implementation
template and approved engineering policy. Those settings are not per-run inputs.
The published Console detail accepts the issue URL, while the authenticated
manager retains `/contribute select` and `/contribute run` support.

Ordinary issue prose and complete bounded discussion are untrusted task data.
The owning service prepares and seals a bounded proposal internally; no fenced
issue JSON, file list, test names, workspace, session or receipt is required from
the invoker. Unknown, duplicate, escaping, overlapping or out-of-policy paths deny.
Existing Go production files, exact new/existing regression files and the closed
approved documentation/template/CSS asset classes are supported. New regressions
require absent preimages in inspected existing package directories; concurrent
creation denies rather than overwrites. Trusted checks, module inputs, roles,
base, budgets and authority remain independently fixed. Builder-authored tests
and model verdicts cannot replace immutable acceptance.

Admission reloads explicit repository bytes, seals the issue-specific contract
and derives a unique sibling worktree from the original run identity. Binding,
role input serialization, native execution and accepted evidence readback use that
sealed contract, not the template task/files. Same-key recovery reuses the sealed
snapshot. Native fresh-base preparation rejects observed preimage drift rather
than silently resealing. Existing sticky stop, delivery narrowing, separate role
sessions, source custody, cumulative candidate limits and protected delivery gates
remain required. No new runtime authority is granted by the schema.

## Retained managed preparation

`HermesPatchProposer.PrepareEngineering` uses admitted tool-free `AttemptTurn`
operations. Small inputs retain bounded read selection and scope derivation;
large inventories first select directories, then files, and inspect bounded
chunks with complete source commitments. Every actual input remains within the
runtime limit. Existing writes must be inspected; new regression basenames must
belong to inspected package directories. No regex filename guessing, silent
truncation or indiscriminate writable-repository fallback is used.

The closed proposal cannot alter checks, commands, roles, dependencies, budgets,
base or authority. The owning service retains the original intent/deadline,
actual dispatch lifecycle, full private input/output evidence and counters before
sealing. Role payloads reference controller custody rather than carrying all
private transcripts. Preparation consumes the same cumulative runtime budget as
builder/tester execution and leaves a bounded execution reserve. A new session,
candidate or request replay cannot reset it. Known returned failures can recover
only after exact quiescence; unknown interrupted effects remain fenced.

Stop is sticky; narrowing retains a distinct validated delivery restriction.
Missing foundational authority uses the existing independent provisioning
decision journey. Console receipts disclose its purpose, exact role/plan and
delivery status; unavailable transport is not a pending human approval or denial.
The page cannot approve or apply authority. These retained task-data records are
neither trusted acceptance nor evidence of whole-contribution success.

## Offline dependencies

Engineering managed policy may name `dependencies: {"path": ABSOLUTE_TAR_PATH,
"digest": "sha256:..."}`. The independently approved bounded tar contains only
regular Go file-proxy `.mod`, `.info`, `.zip` and `@v/list` entries. No links,
traversal, duplicates or network/ambient-cache resolution are admitted. Its exact
bytes are reloaded before/after native execution; native Go runs with an isolated
module cache, `GOSUMDB=off`, local file proxy, `-mod=readonly`, pinned toolchain and
immutable `go.mod`/`go.sum`. Dependencies require both locked sums and that approved
bundle. Definition-time `aegis-offline-bundle` preparation checks the actual locked
module closure; normal execution does not borrow an ambient module cache.
Local replacements and excludes are unsupported. Tool declarations require their
module to be pinned in the immutable dependency graph. Native
repository code is **not sandboxed**; bundle approval is a host trust decision.

## Large assets and generated templates

Large approved text assets accept bounded exact byte hunks against a complete
preimage digest and mode. The controller preserves untouched bytes and denies
overlap, drift, traversal and out-of-contract edits. Candidate inventories include
all source and referenced generated inputs; an optional immutable bounded source
inventory policy supports repository-scale custody. Exact reviewed synthetic
fixture exceptions never authorize real private-key material.

Selected `.templ`/`_templ.go` pairs require the independently approved
`templ_consistency` policy and an offline locked module tool. The compatibility
identifier is `templ-module-stdout.v1`; despite that historical spelling, the
checker uses exact read-only `generate -check`, not stdout comparison.

Controller generation additionally requires `templ_generated_writes: true` in
that same managed policy. The binder derives only each exact existing
`_templ.go` pair inside the approved engineering prefixes/assets and cumulative
file bound, preserving full context preimages. It cannot add trusted checks to
writable scope. No invoker supplies per-run bindings or generated Go bytes.
After builder source patches, the controller retains a generation intent with
exact output paths, complete source preimages/modes, module/toolchain/dependency
identities and candidate pass. It runs only `go run github.com/a-h/templ/cmd/templ
generate -f INPUT -include-timestamp=false -include-version=true` in a private
native source copy with the approved offline proxy. Only those paired output
bytes may change; extra/missing files and mode/input drift deny. Builder proposals
cannot write controller-owned generated outputs. Fresh exact-path write admission
and preimage checks precede application to the owned worktree, preserving modes.
A content-addressed generation result joins the refrozen candidate before build
and fresh independent testing. Unknown generation outcomes retain custody rather
than replaying effects.

Every native build/check independently repeats the exact pinned read-only
`generate -f INPUT -check -include-timestamp=false -include-version=true` and
verifies unchanged source. No arbitrary generator, shell, repository script or
network command is admitted. Stale generated source must fail; actual compiled
and rendered behavior still requires independent trusted issue-specific
acceptance. Complete source/toolchain/dependency and executable identities are
refrozen after all edits; model verdicts and asset hashes alone are insufficient.

## Candidate-bound review

Set managed `review_mode: issue-journeys.v1`. Each actual security/launch asset
pins its baseline digest and names a matching immutable criterion with
`criterion_id`; `approved_digest` must be empty. Changed/unaffected/missing assets
are checked against the frozen candidate. A passing ledger additionally binds the
actual required native security/launch journey output from the independent tester
operation. Model PASS text and advance approval of future edited bytes cannot mint
review evidence. These typed checks do not claim a general semantic human review.

## Evidence and limits

Controlled native regression `TestManagedContributionEngineeringRegressionRepair`
creates a deliberately bad candidate, retains failure feedback for the same
builder, authors a separate regression, rebuilds/refreezes and runs a fresh tester
within the original two-candidate budget. Application regression
`TestContributionIssueDerivedAdmissionAndBinding` covers publication, activation,
issue admission, exact role payload and same-key replay. Dependency archive and
context substitution negatives live in `engineering_context_test.go`.

Controlled tests exercise managed protocol, native compilation, bad-candidate
feedback/repair, fresh tester sessions, large asset custody and local joined
disposition. Actual built-executable Console/authoring acceptance is a separate
fixture tier. Neither establishes live-provider execution, permitted real GitHub
protected merge or installed 8448 behavior; those require exact target evidence.
No new provider, credentials, daemon, TaskQueue or PlanStore is installed by this
change. Historical placeholder revisions remain immutable and are not repaired
by restoring discovery or activation.
