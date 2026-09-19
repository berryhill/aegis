# PR 268 command race memory diagnosis

Base: d14997a88ba530ce7bf7fc35ba38eed0b530ede9.

The complete internal/command race run and isolated TestGatewayIntakePTY both hit the unchanged 6 GiB cgroup limit at the first `typed` case. A 10-second diagnostic timeout captured `os.ReadFile` in the final plaintext-canary WalkDir, with a file length of 0x7ffffffe. The test was materializing a preallocated Badger value log, not demonstrating retained race goroutine metadata. No production storage change is warranted.

The test now streams all regular-file bytes using 64 KiB chunks and canary-length overlap. It retains plaintext detection across chunk boundaries and read-error propagation. No file or test is excluded. Production code, storage settings, concurrency, and hard budget are unchanged.

Evidence retained outside Git in `/home/silas/.hermes/.scratch/`:
- `aegis-command-race-before.log`: complete package OOM, 6.0G peak.
- `aegis-command-race-isolated.log`: isolated first fixture OOM, 6.0G peak.
- `aegis-command-race-stack.log`: diagnostic allocation/read stack.
- `aegis-command-race-after.log`: complete package PASS, 124.064 seconds, `go test -race -v -p=1 -timeout=6m ./internal/command`.
- `aegis-command-race-rss.log`: uncached isolated fixture and scanner regressions PASS, 111.555 seconds; GNU time max RSS 443956 KiB. Successful systemd MemoryPeak readouts were implausibly small and are not accepted as aggregate peak evidence.

All runs used scripts/verify-budget.py, GOMAXPROCS=2, GOMEMLIMIT=512MiB; hard 6 GiB, zero swap, task and CPU limits remain unchanged.

Launch-asset impact review: CHANGELOG updated. README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, threat model, architecture diagram document, quickstart, no-key script, recording documentation, release script/workflow, and contributor issue backlog inspected and unaffected by this test-only repair. Historical recording and release binaries/checksums were not regenerated or certified; no release/laptop work was authorized. Full repository readiness, exact-head CI, merge, and release artifact validation remain the parent's separate gates. This focused package proof is not a full readiness verdict.
