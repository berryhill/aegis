# Human-to-Aegis Manager Operator Acceptance POC

This is one narrow, opt-in acceptance lane for the current shipped `aegis manager` terminal interface. It is not multi-agent testing, profile orchestration, a scenario language, semantic grading, or a promise that the manager is sandboxed.

## Journey

The driver opens the real manager under a Linux PTY with the accessible/plain presentation profile and conducts exactly this journey:

1. Ask an ordinary manager-capability question and require a completed, visibly untrusted Hermes turn. The model's exact prose is not graded.
2. Ask naturally to create a credential named `test`.
3. Supply and confirm a fresh generated canary only after Aegis enters protected no-echo intake.
4. Ask naturally for the credential count and require the authoritative inventory result.
5. Say `Show me the one I just created.` without naming `test`, and capture the completed, visibly untrusted conversational response for operator review. The model's exact prose is not graded.
6. Type `exit`, require zero exit and visible cleanup completion, verify the manager process group is absent, require successful `credential_created` and cleanup-complete `manager_session_closed` audit events, then run `aegis audit verify`.

Use a disposable Aegis installation whose credential authority does not already contain `test`. Create is insert-only, so an existing record intentionally fails the lane rather than mutating it.

## Prerequisites

- Linux and Python 3.10+ from the standard library only.
- A source-built `./aegis` executable in this checkout, or an explicit executable supplied with `--aegis`.
- A fully initialized manager installation for that executable/configuration: configured principal, unlockable credential authority, supported Hermes Agent, exact local Ollama artifact, and valid current manager certification.
- An authority that needs no unlock or a compatible desktop `pinentry` that the operator can complete before the manager composer appears. The POC does not accept a passphrase or credential value in argv, environment, or a file, and it cannot safely automate terminal-fallback authority-passphrase entry.
- No existing credential reference `test`. Use a disposable development installation; do not run against valuable production authority merely to exercise this POC.

The driver sets `AEGIS_ACCESSIBLE=1`, `TERM=dumb`, and `NO_COLOR=1` for stable line-oriented capture. It does not change the configured model, pull an artifact, certify a model, initialize/reset an installation, modify a Hermes profile, or start an operator-managed Ollama service.

## Run

From the repository root:

```sh
go build -o aegis ./cmd/aegis
python3 scripts/operator_acceptance_poc.py \
  --aegis ./aegis \
  --evidence ./operator-acceptance-evidence-$(date -u +%Y%m%dT%H%M%SZ)
```

For an explicit configuration, add `--config /absolute/path/to/aegis.yaml`. The path must be appropriate for the selected executable profile. Each turn is bounded to six minutes by default; use `--turn-timeout SECONDS` only when local inference needs a different bound, up to 15 minutes.

The evidence directory must not already exist. It is created mode `0700`, and `journey.jsonl` is created mode `0600`. Keep it outside Git or delete it after review. Do not publish it without inspecting every line; ordinary model prose and credential metadata can themselves be sensitive.

## Evidence format and checks

`journey.jsonl` contains one `aegis.operator-acceptance.v1` JSON object per line:

- run header: journey and explicit `runtime: hermes`;
- turn records: turn name, human intent, submitted non-secret input, sanitized visible result, UTC start, elapsed milliseconds, outcome, and deterministic checks;
- failure records: bounded sanitized friction/failure evidence;
- final summary.

Protected-input plaintext is never represented in a record. The create turn says `[protected input omitted]`. A fresh 256-bit canary is held only in process memory, entered twice at the protected prompt, and checked against every PTY chunk, audit command output, encoded record, and final retained evidence file. A detected leak fails immediately. The evidence renderer removes terminal escape/control sequences and bounds retained lines and widths; this is safe-to-inspect support evidence, not a forensic transcript.

The current deterministic pass criteria are:

- ordinary conversation reaches a typed `Hermes model / untrusted` result and authoritative turn completion, without matching exact prose;
- protected intake is reached, no canary appears on the PTY, and typed authoritative output reports creation of reference `test`;
- authoritative count output is present;
- the exact non-explicit referent turn `Show me the one I just created.` reaches a typed `Hermes model / untrusted` result and authoritative turn completion, without exact-prose or second-model grading;
- manager exits zero, reports cleanup complete, and its process group is absent;
- audit listing contains successful `credential_created` and cleanup-complete `manager_session_closed` events, and `aegis audit verify` exits zero;
- the canary is absent from retained evidence.

The credential database effect is checked through the typed create result, the subsequent authoritative count, and the metadata-only create audit event. The later pronoun-only turn deliberately exercises conversational referent resolution rather than repeating the credential name; its sanitized response is retained for human acceptance review, not semantic grading by another model. Audit checks also require the current session-cleanup event, then verify chain/checkpoint validity. They do not claim external audit anchoring or that the read-only count operation emits a dedicated event.

## Hermetic support checks

Normal CI does not run a live model. It runs the PTY recorder against a deterministic fake terminal peer, including a forced protected-canary echo regression:

```sh
python3 -m unittest -v scripts/operator_acceptance_poc_test.py
```

The fixture proves driver synchronization, evidence shape/modes, failure capture, and canary non-retention. It is not reported as a live manager transcript or model success.

## Safety limits and deferred work

- PTY no-echo protects this capture path; it does not protect against root, the kernel, a compromised terminal, same-account process inspection, swap/core dumps, or Go/Python runtime copies.
- Manager terminal scrollback and this evidence file are outside Aegis session cleanup.
- Process-group absence covers the Aegis manager and children in its session. An external operator-owned Ollama daemon is intentionally outside that cleanup boundary.
- This lane tests one operator, one manager session, and one credential. It does not test workers, delegation, multiple profiles, agent orchestration, scheduled CI, model matrices, GitHub operations, generalized scenarios, or exact response prose.
- Rich-terminal rendering, non-Linux PTYs, cancellation campaigns, and broader model quality evaluation remain in their existing focused test/research lanes.
- A live run remains manual and gated because it requires operator-owned local authority, Hermes, Ollama, an installed exact artifact, and valid certification. Never synthesize a transcript when one of those prerequisites is unavailable.
