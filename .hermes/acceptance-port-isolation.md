# Candidate acceptance port isolation

Repository-owned engineering workflow, not an Aegis runtime, scheduler, service,
network boundary or authority grant. Read this before launching acceptance
candidates. It carries the owner-directed workstation reservation into fresh
source sessions; it does not change product acceptance criteria.

## Targets and authority

- `https://silas-workstation.taild7c550.ts.net:8448/console` and its **actual
  backend `127.0.0.1:8098`** are reserved together for Matt/Xander clean-main
  integration. Main-only means a clean revision contained on main, not necessarily
  newest main until the owner explicitly updates it. Builders must not replace,
  restart, reconfigure or use this deployment for candidate acceptance.
- Builders dynamically choose **ANY free port 8450–8469 inclusive**. No issue,
  builder or agent receives a fixed assignment. Default and supported helper
  probe address is **127.0.0.1**. No new public, Funnel or tailnet exposure.
- On this host there is exactly one cooperative pool:
  `/home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks/`.
  The existing operator policy in its parent `README.md` remains the operational
  reservation. Do not establish another claim root for real candidates, including
  another checkout, `/tmp`, per-issue or per-agent lock directory. Distinct lock
  roots do not coordinate before listeners bind. A different host requires its
  owner's explicit shared-pool selection, not an inferred workstation default.
- Existing files `8450.lock` through `8469.lock` are standard Linux advisory
  flock inodes. **Never delete, replace, truncate or write them**, even when a
  previous owner crashes. Lock-file bytes are not liveness/ownership metadata.
  Do not change permissions to make a denied pool work.
- Keep each builder's worktree, configuration, state/database, workspace,
  process and test identity isolated. A port claim is not state isolation,
  authentication or a sandbox. Never open main's stores as another writer, copy
  main configuration/credentials, move execution history, reset consumed budgets,
  inherit authority or create a substitute Agent/Loop/run to escape a failure.
  Required candidate authority uses supported approved setup only; no approval
  means that stage remains BLOCKED. The helper performs no setup or provisioning.

## Foreground lifetime helper

`scripts/acceptance_port.py` uses only Python's standard library on Linux, as an
unprivileged invoking user. The pool must already exist, belong to that user,
and have a private owned ancestor protecting any group-writable inner paths
(the workstation's existing profile does). Descriptor-relative traversal rejects
symlinks, foreign owners, unprotected writable ancestry and access ACLs. Lock
files must be owned, single-link regular files without other-write or access ACLs.
No implicit path, privilege escalation, directory/lock creation or fallback pool.

Example **fixture**, not an Aegis listener flag or product acceptance:

```sh
python3 scripts/acceptance_port.py \
  --pool /home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks \
  -- python3 -m http.server '{AEGIS_ACCEPTANCE_PORT}' --bind 127.0.0.1
```

This command remains foreground until interrupted. Use an isolated test harness
instead of `http.server` for actual acceptance. First inspect the candidate's
supported `--help`, prepare its separately authorized isolated configuration,
and pass its **actual documented** foreground command after `--`. A harness
may start the server, run tests and stop/reap its own server before exiting; the
claim then covers the entire sequence. Do not launch a detached server and exit
the harness. Interactive human approval must use its normal protected surface;
this helper does not turn unattended commands into approval-capable sessions.

The helper exports **`AEGIS_ACCEPTANCE_PORT`** to its child and replaces only the
literal **`{AEGIS_ACCEPTANCE_PORT}`** marker in argv. These are helper mechanisms,
**not Aegis flags/environment configuration**. A harness can read the variable to
produce its own supported isolated config. No shell evaluation or general format
expansion occurs. `AEGIS_ACCEPTANCE_PORT_POOL` can supply the explicit pool path;
`--pool` wins. `--ports` restricts diagnostic/test search to a nonempty subset of
8450–8469; normal candidates omit it and search the entire pool. Reserved 8448,
8098 and all out-of-range values deny before launch.

For each candidate the helper acquires nonblocking `fcntl.flock(LOCK_EX)` on an
existing inode, verifies that pathname still names that inode, and **inside the
held claim** tries an actual exclusive loopback bind. Busy locks and unregistered
listeners are skipped, never killed. Exhaustion fails closed with exit 125.
The probe closes before the child's real bind: uncooperative software can still
race it. The child's OS bind remains final; a bind failure is failure, not license
to kill/reuse another listener or fall back to main.

A wrapper and small guardian share the same locked open-file description through
spawn, execution and complete owned process-group shutdown. Both are subreapers.
SIGINT/SIGTERM/SIGHUP request owned-group shutdown;
normal/failed child exit also cleans ordinary descendants. After three seconds
remaining owned group members receive SIGKILL. The lock stays held until the
group is gone; an uninterruptible child can therefore block cleanup rather than
release falsely. Failed launch releases custody without changing pool files.
Wrapper SIGKILL/abrupt death closes a private pipe; the guardian then shuts down
and reaps the owned group before release. Before exec, the foreground child
registers its newly created group over a private pipe that is closed before the
command runs. If the guardian crashes, the surviving wrapper retains the lock,
adopts ordinary descendants and shuts down that exact registered group before
release. Linux parent-death signaling also protects the direct child. The child
inherits the claim descriptor as additional lifetime protection. These are cooperative
foreground process controls, **not confinement**: commands must not daemonize,
close inherited custody and escape into another session, or arrange external
service ownership. Simultaneous forced death of guardian and deliberately escaped
descendants is not promised safe supervision. After any abnormal infrastructure
interruption, inspect actual listener availability; a released lock with a
surviving listener is occupied, not reclaimable. Never kill other owners.

The wrapper propagates normal child exit status, uses 128+signal for handled
interruption/child signal death, and 125 for setup/spawn denial. Its own SIGKILL
is reported by the caller as signal death. Argument and setup/spawn errors use
closed diagnostics, never raw OS exception filenames or command arguments.
The sole helper metadata event contains
chosen port and owner-child/supervisor PIDs, not argv, environment, secrets or
configuration contents. Child output is passed through unchanged: keep secrets
out of fixture output. The helper inherits the invoking environment; use a
reviewed minimal environment and disposable HOME as appropriate, never ambient
main credentials or copied profile configuration.

## Evidence and release

Record issue, exact owner session, selected port, worktree, isolated config/state/
workspace locations, executable/source identity and scope in private coordination
evidence. Do not retain credential values, raw private config/prompts, tokens or
approval material. Verify the running candidate identity before/after acceptance.
Stop only your own server through its normal lifecycle, verify listener absence,
preserve evidence and let custody exit. Keep all existing locks and history.
Main integration is separate owner-controlled work after candidate acceptance and
permitted source merge. Never repoint a candidate listener to main/shared state.

Loop, Graph, Trigger and canonical contribution contracts retain **all** exact
publication/Run, immutable identity, real provider/event, negative, output-byte,
budget, independent verification and installed-target requirements. Historical
8446 installed URLs are separate targets, not builder defaults. Older candidate
8448 wording is superseded only for location; 8448/8098 now names reserved main.
A helper fixture PASS is not product Loop/Graph/Trigger acceptance. Engineering
port-tool changes do not themselves require spending live-provider budgets;
report product journeys NOT EXERCISED rather than waived or passed.

## Verification and fresh adoption

```sh
python3 -m unittest scripts/acceptance_port_test.py scripts/acceptance_context_test.py -v
GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/architecture ./internal/skillbundle/... -count=1
GOMAXPROCS=2 GOFLAGS=-p=1 go run ./internal/skillbundle/cmd validate .
GOMAXPROCS=2 GOFLAGS=-p=1 go run ./internal/skillbundle/cmd evaluate .
```

The process tests create private repository-local fixture pools under ignored
`.aegis-e2e/port-isolation-proof/`, never modify the real operator pool, bind only
free candidate ports, and retain lock inode/byte history assertions. Fixture pool
separation is a negative test, **not** permission for parallel real claim roots.

The essential reservation is directly near the start of `AGENTS.md` because
Hermes discovers that file but does **not** automatically inline linked policy.
A higher-priority `.hermes.md`/`HERMES.md` or truncation can mask project rules.
Test the **actual installed loader in a fresh Python process**, with the selected
Hermes interpreter/dependencies and source path, for both root and nested cwd:

```sh
AEGIS_HERMES_SOURCE=/absolute/installed/hermes-agent \
  /absolute/hermes/python -m unittest scripts/acceptance_context_test.py -v
```

The opt-in test imports `agent.prompt_builder.build_context_files_prompt`, asserts
its module provenance, uses explicit cwd and `skip_soul=True`, and requires the
port reservations/routing in the effective context (including a bounded-context
load). Missing opt-in is reported SKIPPED, not fresh-loader proof. This proves
fresh project loading only; it does not prove old sessions adopted changes.
Have the parent independently repeat against the frozen candidate, and start new
sessions or obtain explicit read/adoption from each active owner. `/reload-skills`
alone refreshes neither cached project context nor running workers.

The source router skill has a minimal repository engineering pointer; the fifteen
shipped skills remain product skills, not a new engineering skill suite. After
changing a skill, use the canonical maintenance path and revalidate:

```sh
AEGIS_REFRESH_SKILL_MANIFEST=1 GOMAXPROCS=2 GOFLAGS=-p=1 \
  go test ./internal/skillbundle -run '^TestRefreshManifest$' -count=1
```

#348 retains final distribution ownership. Do not edit its central operation
matrix or `docs/HERMES_SKILLS.md` merely to add this engineering rule. Bundle
validation is source/package evidence, not profile installation or fresh skill
loading; those are separate parent-owned, approved-custody steps. Do not modify
installed skills, services, credentials or another profile as part of this tool.
