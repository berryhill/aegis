# Issue 370 launch-asset impact review

The only product change is the successful Console Loop Archive 303 destination:
`/console/loops?availability=archived`, without `record_key`. Restore retains
`/console/loops?availability=published&record_key=<exact revision key>`.
No new navigation input, filter, authority, lifecycle or execution behavior is added.

- Updated `CHANGELOG.md` with the bounded navigation correction.
- Reviewed root README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT,
  `docs/THREAT_MODEL.md`, `docs/ARCHITECTURE.md` and its diagrams: unaffected;
  authentication, origin/CSRF, stable ownership, immutable digest and availability
  compare-and-swap admission remain unchanged.
- Reviewed `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`, terminal recordings and
  `docs/launch/LAUNCH_COPY.md`: these CLI/build workflows do not assert the old
  Loop Archive detail destination; no edits or new recordings required.
- Reviewed release workflow/binary-checksum packaging and
  `docs/contributing/ISSUE_BACKLOG.md`: unaffected. No release or external issue
  action requested or performed; existing published binaries/checksums were not
  republished or claimed to contain this fix.

Regression reuses the authenticated mixed-availability browser fixture, actual
built candidate and private durable stores. Archive from a collection card must
remain a collection with no selected Loop workspace, survive reload/restart and
remain absent from Published and discoverable in Archived. Restore retains exact
existing detail navigation. Handler tests assert exact 303 Location values;
existing denial/CAS and non-vacuous lifecycle-history checks remain in place.
Full hosted mandatory CI and independent acceptance belong to the parent delivery
conductor; full local `make verify` is intentionally excluded by the owner budget.
