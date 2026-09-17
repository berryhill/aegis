# Bounded verified-implementation kernel (integration pending)

This package is an additive, provider-free execution kernel. Loop v3 now binds
the exact action contract through authenticated draft publication/readback. A
typed, tool-free Hermes patch proposer exists, but **kernel execution and atomic
Queue completion are not yet wired**. Production Queue execution rejects v3
before claim; ordinary runtime adapters also reject implementation contracts.
Existing Loop v2 canonical bytes,
evidence claims and execution behavior are unchanged. Do not advertise this as an
available console workflow or activate it through an existing v2 Loop.

`loop.ImplementationDraft` creates a typed v1 action contract with an explicitly
unresolved workspace. `Validate` rejects unresolved workspace/source policy or missing immutable required package/test identities,
unknown policy kinds, nonlocal package patterns and budgets outside one or two
passes. Strict decoding rejects unknown and duplicate fields. Only explicitly
listed non-test Go files may be proposed for writes; the model cannot supply a
command, change tests or supply a PASS receipt.

`Executor.Run` requires a controller-owned Badger DB, an absolute operator-selected
Go executable, a proposal adapter and fresh admission callback. The callback must
bind the exact run, contract/workspace, authenticated operator and current authority
outside the model; a callback that always allows is appropriate only in tests.
Each proposal/write/check/completion repeats admission. The runner passes only an
allowlisted environment, disables module network lookup and uses disposable caches.
The workspace is limited to 4096 regular files and 8 MiB of source content; symlinks
and multiply-linked files are rejected. Source replacement uses exclusive temporary files and root-relative atomic rename, never truncating existing inodes. Snapshots read bounded content through root-relative descriptors. `.git` is excluded from content snapshots.

Actual `go test -json` output and complete workspace snapshots are content addressed in
Badger. Every selected immutable top-level test must emit run and pass for its exact package/name; missing, skipped, failed and zero-test runs cannot succeed. Completion rechecks persisted JSON evidence. Test files cannot be model edit targets. Persisted run/pass IDs consume the correction budget before effects. A
second invocation cannot reset it, including after interruption. Interrupted runs
become terminal `interrupted` on the next invocation with the same contract; they are never replayed. Initial authority rejection is durable and cannot replace a reserved run. Completion reloads stored evidence
and hashes the current workspace again. Failed, denied, revoked, cancelled and
expired outcomes remain distinct.

**Not a host sandbox:** Go source/tests execute native code with controller OS
rights. A trusted operator must explicitly authorize this code-execution scope.
Environment scrubbing is not filesystem credential confinement. A nonblocking advisory lock on the canonical workspace directory serializes cooperating kernel executors across databases/processes. **The operator must exclude all external/noncooperating writers and directory replacement throughout custody.** Unix process groups are killed on cancellation and after checker exit, including remaining descendants; unsupported platforms fail closed. Process groups are not a sandbox, cgroup, or protection against hostile processes escaping their group. Strong isolation and hostile native-code containment are not provided. Native malicious tests can forge test events and remain out of scope. Source proposals are intended as untrusted data, while the
proposal adapter and custody DB are trusted controller components.

Implemented integration: v3 Step binding, detached canonical copies, strict
validation, typed authoring builder, authenticated Unix HTTP publication with
real isolated persistence/readback and no Queue submission, immutable revision
transport in RuntimeRequest, and strict tool-free Hermes proposal adapter.

Remaining integration: wire proposer into kernel through controller-owned custody;
Graph/Queue execution routing; shared fleet transaction checker-receipt completion
revalidation (legacy ExpectedDigest verification must not be reused); explicit
workspace/native-check authority adapter; browser authoring; activation readiness;
first-pass/corrective/failure/tamper service-to-Queue execution tests; launch-asset
review. The current production refusal is intentional until those gates exist.
No general scheduler, credential broker, auto-commit/push or deployment is added.

Verification: `GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/implementation`.
The synthetic repository tests execute the real Go toolchain without a provider.
