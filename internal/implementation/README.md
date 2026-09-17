# Bounded verified-implementation kernel (integration pending)

This package is an additive, provider-free execution kernel, **not yet wired into
Loop publication, Hermes, or the Queue worker**. Existing Loop v2 canonical bytes,
evidence claims and execution behavior are unchanged. Do not advertise this as an
available console workflow or activate it through an existing v2 Loop.

`loop.ImplementationDraft` creates a typed v1 action contract with an explicitly
unresolved workspace. `Validate` rejects unresolved workspace/source policy,
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
are rejected. `.git` is excluded from content snapshots.

Actual `go test` output and complete workspace snapshots are content addressed in
Badger. Persisted run/pass IDs consume the correction budget before effects. A
second invocation cannot reset it, including after interruption. Interrupted runs
are fail-closed, not automatically resumed. Completion reloads stored evidence
and hashes the current workspace again. Failed, denied, revoked, cancelled and
expired outcomes remain distinct.

**Not a host sandbox:** Go source/tests execute native code with controller OS
rights. A trusted operator must explicitly authorize this code-execution scope.
Environment scrubbing is not filesystem credential confinement. Strong isolation,
hard-link defenses, competing workspace writers and hostile native-code containment
are not provided. Source proposals are intended as untrusted data, while the
proposal adapter and custody DB are trusted controller components.

Remaining integration: versioned Step binding and canonicalization; runtime typed
proposal transport; Graph/Queue routing; shared fleet transaction completion
revalidation; authority adapter; crash reconciliation; publication/activation
readiness; real service/storage draft publish/readback fixture; launch-asset review.
No general scheduler, credential broker, auto-commit/push or deployment is added.

Verification: `GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/implementation`.
The synthetic repository tests execute the real Go toolchain without a provider.
