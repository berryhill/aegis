# Frozen installed-client release contract

`candidate.go`, `update.go`, `prepared.go`, `journey.go`, and `upgrade.go`
are byte-for-byte production sources from v0.2.29, commit
`e9f68106c504088998d1988d9f7a6a82798a968e`. Do not modernize these copies.
The separate module uses only the Go standard library. The surrounding test
serves actual release archives/checksums from loopback HTTP to unchanged
`Prepare`, then checks unchanged `Identity` against expected version, source,
and candidate SHA-256, and verifies preparation did not replace the isolated
installation. All four platforms pass the old candidate validator; wrong
versions must still be rejected. No service, authentication or real installation
is touched. Native Prepare uses current version 0.2.29 except the non-release
0.0.0 CI fixture, where a development version permits candidate preparation.

The installed-MVI publication gate invokes this test before installed vertical
proofs. Frozen sources avoid a network fetch or dependence on historical tags
being present in shallow release checkouts.
