# Issue 249 registration default contract

Objective: authorized new registrations default to canonical `enabled` before preview, validation and sealing. Active Registry eligibility does not mean running, ready, provisioned, delegated or runtime-authorized.

Gap: fixture lifecycle omission currently fails validation; explicit default-profile import hardcodes disabled.

Scope: registration input decoding and explicit import proposals/confirmation/readback only. Canonical revision decoding stays strict. Preserve explicit disabled/retired input, prior lifecycle decisions, provenance, exact approvals and fresh authority admission. No discovery-side writes or runtime activation.

Acceptance: omitted fixture lifecycle yields enabled; null, empty, unknown, duplicate and unknown-field input deny; explicit disabled/retired survive. Import preview and persisted initial revision agree; existing imports remain unchanged. Focused registry/app/command tests guard these boundaries. Broad/race, installed proof and launch-asset verification remain separate conductor gates.
