# Issue 325 bounded candidate launch review

Changed README, CHANGELOG, SECURITY, threat model and architecture documentation,
and added the exact-run debugger operator/testing guide. The boundary is a
read-only authenticated Console visualization, not new scheduling or authority.

Reviewed LICENSE and CODE_OF_CONDUCT: unaffected legal/community terms.
Reviewed CONTRIBUTING, QUICKSTART, DEMO_NO_KEY and RECORDING: existing commands
and historical terminal proof do not demonstrate this browser debugger; no new
provider/installed acceptance is claimed. No-key demo and historical recordings
were not rerun in this bounded pass. Reviewed RELEASE_PIPELINE and contributor
ISSUE_BACKLOG: unchanged packaging and external-publication gates; multi-node
execution remains separate work. No release binaries/checksums, remote issues or
recordings were published. Release/live acceptance remains outside this task.

Controlled browser fixtures prove the live interpreter/checkpoint/service/browser
path for direct Loop and a separately published single-node Graph, not provider
execution or successful Queue disposition. The source-root candidate executable
also reopens isolated seeded history across two actual serve processes; public
CLI and authenticated browser reads preserve exact durable history, excluding
only request freshness (`observed_at`). Multi-node behavior remains unavailable
under #317. Complete negative HTTP-origin/cursor coverage, actual managed-provider
acceptance and fresh independent acceptance of the final frozen candidate remain
pending; the integration report records these boundaries. Updated the canonical
operation matrix with both new GET registrations and recomputed the existing
skill file-set/bundle digests; skillbundle verification passes. No release or
installed production changes were made.
