# Dashboard Archive authentication and recovery

Loop Archive/Restore and terminal Queue Archive/Unarchive are bounded discovery or
presentation controls. They use the existing live dashboard action-admission
hook, including its configured action freshness, rather than accidentally applying
the separate generic principal-authentication age after browser admission passed.
No authentication TTL, origin/CSRF policy, owner check or execution authority is
changed. Foundational, native, credential and runtime operations do not acquire
presentation-action authority.

## Reported failure and reproduction

The serving gateway logged `operation requires fresh configured principal
authentication` on both POST handlers. An isolated real-browser reproduction
signed into the real password verifier, retained that browser login, advanced a
synchronized fixture clock beyond the generic principal window while remaining
inside the configured dashboard action window, and clicked the actual Archive
buttons. Both returned HTTP 403 before the repair. This was not a GET of a
POST-only URL and did not operate Matt's named records.

## Expired authentication does not authorize a mutation

When a valid original form reaches an expired dashboard action window, the
presentation-only adapter may retain its exact target, operation and CAS fields
for two minutes. Custody is bounded to 128 entries, one per browser identity,
with bounded paths and payloads. It carries no authority. An expired login cookie
may prove only its original cookie/CSRF binding during that brief recovery window;
ordinary identity and mutation admission still deny it. Logout, revocation,
password generation changes, expiry and process restart invalidate custody.

The browser moves to a safe GET review URL. Password verification creates a new
fixed-lifetime authenticated session and binds the same pending action to it;
it does not execute the action. The owner must explicitly confirm the exact
retained target and payload. Current ownership, target, freshness, origin, new
session CSRF and original publication/presentation CAS are checked again. A stale
record is not silently refreshed or retargeted. Cancellation discards custody
without mutation. Custody is consumed before the effect, so a lost or uncertain
completion cannot silently replay the retained action.

Malformed, denied, stale/conflicting and unavailable requests have distinct safe
HTML recovery destinations. API clients retain their ordinary rejection statuses.
An unavailable/unknown completion directs the operator to read the exact record,
not blindly retry. Failure to retain an expired form does not grant an action or
claim it succeeded.

Loop Archive still changes discovery for every revision of the logical Loop;
it preserves history and does not stop execution. Queue Archive still changes
terminal-record visibility only; it does not cancel, requeue or rerun work.

## Evidence boundary

`TestPresentationRealBrowser` is opt-in and uses disposable synthetic definitions,
canonical terminal Queue history, a real password verifier, browser forms, the
actual Serve/Manager/service and durable repositories. It covers both successful
paths beyond the generic principal window, expired-action reauthentication and
cancellation, explicit confirmation, consumed-review replay, changed CAS, stale
CSRF, forged owning-Agent input, a foreign-owned Queue, expired-login recovery,
retention expiry and unchanged canonical execution history. Real browser freshness
is controlled only by a private synchronized fixture clock, never a product API.

Run it under the shared candidate-port helper, with a fresh ignored repository-local
proof directory and a Python interpreter containing Playwright:

    AEGIS_BROWSER_PYTHON="$PWD/.browser-proof-runtime/bin/python" \
    AEGIS_PRESENTATION_PROOF="$PWD/.scratch/archive-repair/fresh-proof" \
    python3 scripts/acceptance_port.py --pool OPERATOR_EXISTING_POOL -- \
      env GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/api \
      -run '^TestPresentationRealBrowser$' -count=1 -v

Normal/race Console custody tests additionally prove no identity/freshness renewal,
wrong-cookie/CSRF/origin/target denial, cancellation, single-use consumption and
expiry. Existing Loop/Queue HTTP tests retain owner, origin, CSRF, strict-input,
CAS and immutable-history checks. Isolated proof is not the owner's browser
acceptance; the exact deployed target and any independent login gate must be
reported separately. No named owner Loop or execution is mutated for acceptance.
