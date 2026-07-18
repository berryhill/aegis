# Aegis host endpoint monitoring, enforcement, and response

- Status: Exploratory product and architecture report; not a current implementation specification
- Date: 2026-07-17
- Prepared for: Aegis
- Scope: Host-wide endpoint security, agent-runtime correlation, mitigation, gateway enforcement, and compliance evidence
- Current authority: `AGENTS.md` and `specs/` remain normative for implemented Aegis behavior

## Executive summary

Aegis can coherently expand from an identity, trust, and session-control layer for agents into an identity-aware endpoint monitoring, enforcement, and response system. In that broader system, the physical or virtual machine is a protected endpoint; human sessions, services, containers, conventional applications, and AI agents are workloads executing on that endpoint.

The long-term product thesis would be:

> Aegis establishes what identities, users, services, processes, and agents are allowed to do on a machine; observes the machine for compromise and policy violations; mediates protected actions at real enforcement points; and performs bounded, reviewable mitigations outside the monitored workload.

The differentiator is not merely host telemetry. Aegis can correlate operating-system observations with authenticated identity, approved policy, agent charter, trust stanza, short-lived mandate, runtime process, credential binding, destination, and exact protected operation. That creates an attributable chain from principal intent to machine-observed behavior.

This is a major expansion beyond the current MVP. The current implementation must not be described as endpoint detection and response (EDR), host intrusion prevention, antivirus, host sandboxing, or comprehensive machine confinement. The proposed system would require a hardened endpoint service, host and workload identity, signed machine policy, operating-system sensors, carefully scoped enforcement adapters, fleet control, protected evidence retention, incident workflows, and extensive adversarial and operational validation.

A defensible initial market position would be:

> Aegis is an identity-aware endpoint monitoring and response control plane with native protection for AI agent workloads. It complements existing EDR, SIEM, identity, credential, network, and orchestration systems while adding agent authority and session provenance those systems generally do not possess.

Aegis could support an organization's SOC 2 control environment and produce endpoint-security evidence. Installing Aegis would not itself make an organization or product “SOC 2 compliant.” SOC 2 examines a defined organizational system, the design of its controls, and, for a Type II examination, their operation over a review period.

## 1. Product boundary

### 1.1 Protected hierarchy

The broadened security model is hierarchical:

```text
organization
    -> environment or fleet
        -> enrolled deployment
            -> physical machine, VM, or host
                -> authenticated human session
                -> system service
                -> container or workload
                -> conventional application
                -> agent runtime
                    -> logical agent
                    -> one trust stanza
                    -> one mandate
                    -> one runtime session
                -> protected resource or downstream action
```

Each layer contributes identity and policy context. No display name, process name, prompt, profile, environment variable, container label, or self-reported workload assertion is sufficient authentication by itself.

### 1.2 Four product planes

Aegis should preserve clear architectural responsibilities:

1. **Aegis Authority** — identities, charters, machine policies, approvals, mandates, revocation, and provenance.
2. **Aegis Endpoint** — host sensors, workload identity, integrity monitoring, endpoint policy verification, and host enforcement adapters.
3. **Aegis Gateway** — inline mediation of credentials, APIs, network destinations, tools, data disclosure, and other protected actions.
4. **Aegis Response** — detections, incidents, containment playbooks, recovery, notifications, evidence, and integrations.

These may ship as one product, but they should not collapse into one unreviewable privileged process. Collection, policy decision, privileged enforcement, secret use, and audit retention have different trust and failure boundaries.

### 1.3 Relationship to current Aegis concepts

The existing model remains useful:

- Agent charters continue to define logical-agent and trust-stanza authority.
- Mandates continue to bind authenticated agent sessions to short-lived authority.
- The credential broker continues to demonstrate typed, destination-bound action mediation.
- Deployment identity and selective projections can become a basis for fleet policy distribution.
- Authoritative Aegis audit records can be correlated with host observations.

The host expansion requires new artifacts rather than overloading agent charters:

- **Machine policy:** expected host state, workloads, sensors, network, protected files, identities, and response rules.
- **Workload identity:** verified relationship among executable, package or image, service manager, OS identity, process ancestry, and deployment.
- **Endpoint event:** normalized observation with source and confidence.
- **Detection:** a rule or correlation result derived from observations and policy.
- **Incident:** a durable investigation and response object.
- **Response authorization:** authority to execute one typed mitigation against one bounded target.
- **Mitigation receipt:** authoritative evidence of requested, attempted, confirmed, failed, or rolled-back response.

Operational machine policy must remain distinct from agent charters. A machine policy governs the endpoint and its deployed workloads; a charter governs a logical agent and its stanza-specific runtime authority. The artifacts may reference each other by stable identity and digest.

## 2. Security objectives and non-goals

### 2.1 Objectives

A host-wide Aegis system should aim to:

1. Enroll every protected endpoint with a unique, revocable deployment identity.
2. Establish attributable identities for human sessions, services, workloads, processes, and agent sessions.
3. Collect sufficient operating-system telemetry to detect declared threat classes.
4. Correlate machine behavior with approved host policy and agent authority.
5. Mediate selected consequential actions synchronously at real chokepoints.
6. Deny missing, stale, unknown, expired, revoked, or ambiguous authority.
7. Execute only typed, bounded response actions under explicit policy.
8. Preserve evidence outside the potentially compromised endpoint.
9. Report sensor, policy, enforcement, and evidence-health gaps rather than silently implying coverage.
10. Integrate with existing EDR, SIEM, identity, credential, network, cloud, and orchestration controls.

### 2.2 Explicit non-goals for an initial endpoint release

An initial release should not claim:

- Complete prevention of arbitrary root- or kernel-level compromise.
- Complete malware detection.
- Memory-safe collection or guaranteed secret zeroization.
- Comprehensive host sandboxing.
- Perfect attribution after kernel compromise.
- Complete mediation while ungoverned shell, filesystem, credential, or network routes remain available.
- Replacement of mature cross-platform EDR suites.
- Automatic correctness of model-generated detections or remediations.
- Guaranteed deletion of data or credentials from all media.
- SOC 2 certification or compliance merely because the software is installed.
- Protection against physical attacks, malicious firmware, or compromised hardware without separately validated mechanisms.

### 2.3 Threat classes

The design should explicitly cover selected examples from these classes:

- Unauthorized interactive or remote access.
- Credential theft and unauthorized credential use.
- Persistence through accounts, SSH keys, services, scheduled jobs, startup files, or packages.
- Privilege escalation and privileged group changes.
- Unexpected process execution or process ancestry.
- Protected-file modification and executable replacement.
- Unauthorized network listeners or outbound destinations.
- Data access or disclosure outside an approved workload boundary.
- Defense evasion, including sensor, audit, or policy tampering.
- Agent prompt-driven misuse that produces host-level effects.
- Cross-stanza, cross-workload, or cross-deployment access.
- Compromised package, image, executable, plugin, MCP server, or runtime artifact.
- Fleet policy rollback or deployment identity cloning.

Each supported threat class needs declared telemetry prerequisites, detection logic, expected response, validation cases, and residual risks.

## 3. Endpoint architecture

### 3.1 Reference data flow

```text
OS and kernel sensors ---------+
service/container integrations |
identity and authentication ---|
Aegis session authority -------+--> endpoint event normalization
Aegis protected gateways ------|             |
existing EDR or host sensors --+             v
                                      local policy checks
                                              |
                                  authenticated event stream
                                              |
                                              v
                                   fleet detection/correlation
                                              |
                                  deterministic policy decision
                                              |
                       +----------------------+----------------------+
                       |                      |                      |
                     record                 deny              require approval
                       |                      |                      |
                       +--------------> response orchestrator <------+
                                              |
                     revoke / stop / block / isolate / rotate / notify
                                              |
                                              v
                           receipts and separately retained evidence
```

### 3.2 Endpoint sensor

The endpoint sensor collects and normalizes observations. A Linux-first implementation may integrate with:

- eBPF tracing and, where justified, eBPF LSM hooks.
- Linux Audit.
- procfs, cgroups, namespaces, and process start-time identity.
- systemd and its journal.
- Netlink and socket/network state.
- fanotify or inotify for deliberately selected paths.
- SELinux or AppArmor state and denials.
- nftables or another validated firewall interface.
- Container runtime and orchestrator APIs.
- Package manager and software inventory sources.
- osquery, Falco, or an installed EDR through adapters.
- Aegis authentication, charter, mandate, session, broker, and audit services.

The report does not select one kernel collection technology. Coverage, portability, performance, required privilege, kernel compatibility, bypass resistance, event loss, and operational burden must be evaluated before a normative choice.

The sensor should not interpret all observations as equally authoritative. Every event requires source, collection method, source health, and confidence metadata.

### 3.3 Endpoint enforcer

The endpoint enforcer performs narrow privileged operations through a small interface. Candidate actions include:

- Freeze, terminate, or prevent restart of a process tree.
- Stop or disable a service.
- Revoke an Aegis mandate or broker capability.
- Block one executable digest.
- Quarantine one artifact.
- Add a bounded firewall rule.
- Remove a workload from service.
- Lock one local identity under approved policy.
- Request machine isolation through an external EDR or network controller.

Collection and enforcement should be separate interfaces even if one daemon initially hosts both. The enforcer must not accept free-form model-generated commands.

### 3.4 Host authority and policy service

The host authority validates canonical machine policy, binds policy to enrolled deployments, distributes signed generations, tracks activation, and rejects stale or mismatched policy.

A machine policy should be:

- Strictly typed and schema-versioned.
- Canonically serialized and digestible.
- Signed by an authorized policy issuer.
- Bound to a deployment or explicit deployment class.
- Monotonic in generation.
- Diffable and reviewable.
- Explicit about monitor-only versus enforcing controls.
- Explicit about offline, degraded, and controller-unreachable behavior.
- Accompanied by activation and effective-state receipts.

### 3.5 Detection and correlation service

Detection should support three distinct classes:

1. **Deterministic violations:** direct mismatch with authenticated policy or approved state.
2. **Known-indicator and rule detections:** signatures, indicators, or known attack sequences.
3. **Behavioral detections:** deviations from a declared or learned baseline.

Deterministic violations are best suited for automatic denial. Behavioral detections generally require investigation or conservative containment until validated. A model may summarize evidence or recommend a response, but it must not be the sole authority for a destructive decision.

### 3.6 Response orchestrator

The response orchestrator accepts a typed incident and response request, rechecks authority and target state, executes one approved playbook, and emits a receipt. It must handle idempotency, partial failure, retries, timeout, rollback, and target disappearance.

### 3.7 Controller and fleet services

A central controller may provide:

- Endpoint enrollment and revocation.
- Signed policy publication.
- Fleet inventory and policy-generation status.
- Sensor and enforcement health.
- Event intake and correlation.
- Incident lifecycle.
- Response approval and orchestration.
- Evidence export and retention verification.
- SIEM, EDR, ticketing, identity, cloud, and notification integrations.

Fleet control must not require every endpoint to hold global fleet policy or credentials. Selective per-deployment projections remain the preferred direction.

## 4. Identity and attestation model

### 4.1 Deployment identity

Each physical computer, VM, or independently managed endpoint should have one enrolled deployment identity. Enrollment should establish:

- Stable deployment ID.
- Deployment class, owner, and environment.
- Per-deployment asymmetric key or equivalent identity.
- Controller trust roots.
- Enrollment provenance and approver.
- Rotation, revocation, and re-enrollment procedures.
- Cloning and duplicate-identity detection.

Hardware-backed key storage and measured boot may strengthen this identity, but their absence or presence must be explicit. A file-held key is not hardware attestation.

### 4.2 Human and service identity

OS UID, username, service name, container label, and process name are attributes, not sufficient global identity. Aegis should bind them to stronger context where available:

- Authenticated local or remote login.
- Unix peer credentials.
- Service manager unit and executable identity.
- Container image digest and orchestrator identity.
- Workload certificate or service-account token.
- Process start token and ancestry.
- Aegis-issued short-lived capability.

### 4.3 Process identity

PID alone is unsafe because it is reusable. A process reference should include at least deployment identity, PID, process start token, executable identity, OS identity, and observed ancestry. Response actions must revalidate the target immediately before execution.

### 4.4 Agent-session correlation

For Aegis-managed runtime sessions, endpoint events should carry or correlate to:

- Logical-agent ID.
- Charter revision and digest.
- Trust-stanza ID.
- Mandate ID and expiry.
- Runtime name, version, adapter version, and process identity.
- Effective tool, memory, and credential scopes.
- Deployment identity and active machine-policy generation.

This does not make every descendant process authorized. Child-process rules must be explicit.

## 5. Machine-policy model

A canonical machine policy should describe at least the following domains.

### 5.1 Machine and environment

- Deployment selectors and explicit exclusions.
- Environment classification.
- Operating-system and kernel constraints.
- Machine owner and operational contacts.
- Data classification and business role.
- Expected controller and evidence endpoints.

### 5.2 Expected workloads

- Service or workload ID.
- Executable path and digest or package/image provenance.
- Expected OS identity.
- Parent process or service manager.
- Allowed arguments and material environment restrictions.
- Required cgroup, container, namespace, or sandbox attributes.
- Allowed child processes.
- Restart and maintenance rules.

### 5.3 Identity and access

- Authorized local accounts and groups.
- Permitted login mechanisms.
- Privileged identities and elevation rules.
- Expected service accounts.
- Break-glass procedure and expiry.
- Required authentication evidence.

### 5.4 Network

- Expected inbound listeners.
- Allowed outbound destinations and protocols.
- DNS and proxy requirements.
- Management and evidence channels.
- Rate or data-volume constraints where enforceable.
- Isolation behavior and exceptions.

### 5.5 Filesystem and integrity

- Protected files and directories.
- Expected ownership and permissions.
- Authorized writers.
- Executable and configuration integrity baselines.
- Secret-bearing paths.
- Quarantine storage and access rules.

### 5.6 Required controls

- Required sensor sources.
- Expected heartbeat interval.
- Maximum tolerated event loss or delay.
- Local buffering requirements.
- Required OS security-control state.
- Update channel and minimum endpoint version.
- Evidence-delivery requirements.

### 5.7 Response rules

- Automatic actions.
- Approval-gated actions.
- Prohibited actions.
- Target scope.
- Evidence prerequisites.
- Maximum action duration.
- Rollback and recovery requirements.
- Authorized responders and separation of duties.

## 6. Endpoint event and evidence model

### 6.1 Normalized event fields

A normalized event should include:

- Event schema and source versions.
- Globally unique event ID.
- Source-local sequence number where available.
- Observed time, received time, and clock-quality metadata.
- Deployment and machine-policy generation.
- Sensor identity, health, and collection method.
- Human, service, workload, and process identities where known.
- Agent, stanza, mandate, charter, and runtime context where applicable.
- Action, object, destination, and result.
- Data classification without secret values.
- Detection, incident, and response correlation IDs.
- Evidence confidence and known gaps.

### 6.2 Evidence integrity

Evidence should be authenticated in transit, ordered where practical, and retained outside the endpoint. Local hash chains and signed checkpoints help detect some forms of replacement or truncation but do not provide independent protection when the endpoint, log, and checkpoints share one trust boundary.

The system should report:

- Event gaps.
- Sequence discontinuities.
- Clock anomalies.
- Buffer overflow.
- Sensor restart.
- Policy-generation mismatch.
- Controller delivery lag.
- Evidence-retention acknowledgement.

An endpoint acknowledgement is not proof that all historical copies or secrets were erased.

### 6.3 Sensitive data

Endpoint telemetry can expose paths, usernames, commands, destinations, source code, document names, and security architecture. Collection must be minimized and classified. Secrets, full prompts, credential values, and arbitrary file content should be excluded by construction unless a separately approved forensic workflow requires exact content.

## 7. Detection design

### 7.1 Deterministic examples

High-confidence detections include:

- Protected file modified by an unauthorized process.
- Unexpected executable launched as a privileged identity.
- Service executable or configuration digest differs from approved state.
- Required sensor or audit facility stops.
- Machine policy is stale, invalid, or rolled back.
- Unauthorized identity enters a privileged group.
- Unexpected SSH key or persistence mechanism appears.
- Unknown kernel module loads.
- New listener appears outside policy.
- Workload contacts a prohibited destination.
- Workload reads another workload's protected secret path.
- Runtime process no longer matches its recorded process identity.
- Revoked agent session continues to request broker actions.
- Agent attempts a tool, credential, destination, or stanza outside its mandate.

### 7.2 Correlated attack sequences

The engine should support stateful correlations such as:

```text
external execution
    -> credential-path access
    -> privilege change
    -> security-control modification
    -> outbound connection
```

and:

```text
untrusted input reaches agent
    -> broad shell capability invoked
    -> executable downloaded
    -> unexpected child process launched
    -> new destination contacted
```

The second sequence illustrates the value of combining Aegis authority data with host observations.

### 7.3 Behavioral examples

- Service begins spawning shells.
- Internal-only workload begins making external connections.
- User accesses an unusual machine class.
- Workload reads an unusual number of protected files.
- Process ancestry changes from its stable baseline.
- Agent produces a burst of denied requests.
- Multiple endpoints contact a previously unseen destination.

Behavioral systems need baseline provenance, drift controls, false-positive measurement, explainability, and conservative automatic-response policy.

### 7.4 Detection lifecycle

Every detection should have:

- Stable rule ID and version.
- Threat and control rationale.
- Required sensors and minimum versions.
- Severity and confidence.
- Suppression and exception semantics.
- Test fixtures and expected events.
- Response recommendation.
- Known bypasses and residual risk.
- Release and rollback history.

Model-generated detection content must undergo the same review, testing, versioning, and approval as human-authored detection content.

## 8. Gateway and prevention semantics

### 8.1 Monitoring is not prevention

A userspace sensor may observe an action only after it occurs. Aegis should use precise enforcement language:

- **Observed:** telemetry indicates an action occurred.
- **Detected:** a rule matched after or during the event.
- **Denied by Aegis gateway:** Aegis refused a synchronously mediated request.
- **Blocked by OS control:** a validated operating-system mechanism prevented the action.
- **Blocked by external control:** an integrated EDR, network, identity, or cloud system confirmed prevention.
- **Mitigation requested:** Aegis requested a response but has no confirmation.
- **Mitigation confirmed:** target state was rechecked and the requested outcome was observed.

### 8.2 Real enforcement points

Prevention requires control over the relevant chokepoint. Candidate mechanisms include:

- Typed credential and API brokers.
- Authenticated network proxies.
- Linux Security Module policy.
- eBPF LSM where supported and validated.
- seccomp.
- cgroups and namespaces.
- SELinux or AppArmor.
- nftables or equivalent host firewall controls.
- Container runtime admission and policy.
- Service-manager controls.
- Identity-provider and credential-authority APIs.
- Existing EDR enforcement APIs.

Aegis should prefer configuring, verifying, and coordinating established mechanisms over building unnecessary kernel enforcement from scratch.

### 8.3 Complete-mediation limitation

Aegis may claim mediation only for resource classes whose paths it actually controls. If the same workload retains unmediated shell, filesystem, reusable credential, or arbitrary network access, gateway enforcement for one API does not imply confinement of equivalent actions through other routes.

## 9. Mitigation and recovery

### 9.1 Graduated response ladder

**Level 0 — Record**

- Preserve bounded event metadata.
- Correlate related activity.
- Capture process, file, policy, and authority identity.
- Create or update an incident.

**Level 1 — Alert**

- Notify the operator or security team.
- Export to a SIEM.
- Open a ticket.
- Require acknowledgement under organizational policy.

**Level 2 — Contain the immediate action**

- Deny one protected request.
- Block one destination.
- Freeze one suspicious process.
- Disable one runtime capability.
- Revoke one agent session or broker capability.

**Level 3 — Contain the workload**

- Terminate the process tree.
- Stop or disable the service.
- Disable the workload identity.
- Quarantine one executable or artifact.
- Prevent restart.
- Remove the workload from a load balancer or scheduler.

**Level 4 — Contain the endpoint**

- Restrict network access to approved management and evidence channels.
- Revoke machine credentials where appropriate.
- Isolate through an EDR or network controller.
- Deny new workload and agent sessions.
- Mark the endpoint unavailable for placement or service.

**Level 5 — Recover**

- Rotate affected credentials.
- Restore or redeploy approved artifacts.
- Rebuild from a trusted image.
- Re-enroll the endpoint.
- Verify effective policy, software, and sensor health.
- Require approval before returning to service.

### 9.2 Response authorization

Every response action should declare:

- Typed action and schema version.
- Incident and evidence prerequisites.
- Authorized target selector.
- Maximum blast radius.
- Automatic or approval-gated status.
- Required approver identity and freshness.
- Idempotency key.
- Timeout and retry limits.
- Rollback procedure.
- Expected postcondition.
- Receipt and verification requirements.

High-confidence, narrow, reversible actions are the best initial candidates for automation. Host isolation, broad identity lockout, credential rotation, destructive file removal, and rebuild normally require stronger evidence or approval.

### 9.3 Model role

A security model may:

- Summarize evidence.
- Correlate related incidents.
- Recommend an existing playbook.
- Explain likely impact.
- Draft a reviewable investigation query.

It must not:

- Authenticate an operator.
- Grant itself response authority.
- Modify machine policy.
- Invent and execute arbitrary shell remediation.
- Mark its own mitigation successful.
- Suppress authoritative audit events.

## 10. Tamper resistance and failure behavior

### 10.1 Privilege separation

The endpoint design should minimize privileged code and separate:

- Event collection.
- Policy validation.
- Privileged enforcement.
- Credential decryption and downstream application.
- Fleet communication.
- Audit authority.
- Model-assisted analysis.

A model should receive none of these privileged interfaces directly.

### 10.2 Endpoint hardening

Candidate requirements include:

- Dedicated service identities.
- Root-owned configuration and executable paths.
- Signed release artifacts and updates.
- Signed, monotonic policy generations.
- External supervision and watchdogs.
- Sensor and enforcer heartbeats.
- Bounded protected local event buffering.
- Remote evidence streaming.
- Detection of service or audit disablement.
- Secure rollback and recovery procedure.
- Separation between policy authors, approvers, and responders where required.

### 10.3 Fail-open and fail-closed policy

One global failure mode is unsafe. Each control must specify behavior when the sensor, controller, network, policy engine, evidence sink, or enforcer is unavailable.

Examples:

- Expired agent mandate: fail closed for new broker operations.
- Evidence sink unavailable: buffer within a strict bound, alert, and apply declared degraded-mode policy.
- Behavioral detector unavailable: continue deterministic host policy enforcement and report degraded detection.
- Controller unavailable: continue the last valid unexpired machine-policy generation only under explicit offline policy.
- Isolation integration unavailable: record failed mitigation; do not claim containment.

### 10.4 Residual trust

Root or kernel compromise may disable sensors, falsify local observations, inspect memory, or alter enforcement. Hardware-backed identity, measured boot, remote attestation, immutable infrastructure, and external network controls may reduce that risk, but each guarantee must be separately implemented and validated.

## 11. Relationship to endpoint protection and EDR

### 11.1 Capabilities Aegis could provide

A fully implemented endpoint direction could provide:

- Endpoint inventory and enrollment.
- Identity-aware process and workload attribution.
- Expected-state and integrity monitoring.
- Agent and conventional workload correlation.
- Session and credential-use mediation.
- Protected-action authorization.
- Detection and incident generation.
- Process, workload, session, and endpoint containment.
- Tamper-evident evidence under documented retention assumptions.
- Integration with existing endpoint and security operations systems.

### 11.2 Capabilities not implied by the current project

Mature endpoint platforms may also provide:

- Malware signatures and reputation services.
- Exploit and ransomware prevention.
- Memory and module scanning.
- Broad kernel telemetry.
- Device and removable-media control.
- Host firewall management.
- Vulnerability and patch management.
- Forensic acquisition and threat-hunting stores.
- Large cross-platform fleet operations.
- Tamper-resistant offline operation.
- Extensive threat-intelligence and detection content.

Aegis must not imply these capabilities until they exist and are tested.

### 11.3 Product positioning

Near-term:

> Aegis complements existing EDR and SIEM controls by correlating machine behavior with authenticated agent identity, approved authority, credential scope, and runtime provenance.

Long-term, after implementing and validating the required endpoint capabilities:

> Aegis is an identity-aware endpoint monitoring, enforcement, and response platform with native security controls for AI agent workloads.

The project should avoid “replaces CrowdStrike,” “complete endpoint protection,” “zero trust,” “fully autonomous SOC,” or equivalent claims without corresponding implementation and comparative evidence.

## 12. SOC 2 and broader compliance use

### 12.1 Correct claim boundary

SOC 2 assesses an organization's defined system and controls. A product is not automatically “SOC 2 compliant” because it implements endpoint monitoring. Aegis may provide controls and evidence used by an organization in its SOC 2 control environment.

The exact control mapping depends on the organization's system description, risks, commitments, policies, auditor, and selected Trust Services Criteria. This report is architecture guidance, not legal or audit advice.

### 12.2 Potential control-support areas

Aegis could support organizational controls for:

- Endpoint inventory and ownership.
- Logical and privileged access.
- Authentication and authorization.
- Authorized software and workload state.
- Configuration and change approval.
- Security monitoring and anomaly detection.
- Incident identification, containment, and recovery.
- Credential lifecycle and revocation.
- Audit generation, integrity, and retention.
- Control health and exception management.
- Vulnerability and security-update response.

### 12.3 Evidence products

Useful evidence exports include:

- Enrolled in-scope endpoint inventory.
- Endpoint owner, role, and environment.
- Active machine-policy generation and digest.
- Effective control status: enforcing, monitor-only, degraded, or absent.
- Sensor and enforcer heartbeat history.
- Event delivery gaps and retention acknowledgements.
- Software, workload, and protected-file baselines.
- Policy changes, approvals, and activation receipts.
- Authentication and privileged-access events.
- Detections, incidents, acknowledgements, and response timelines.
- Mitigation requests, approvals, outcomes, and failures.
- Expiring exceptions and their approvers.
- Agent charter, stanza, mandate, runtime, and machine correlation.

### 12.4 Operational questions an auditor or assessor may ask

- Are all in-scope endpoints enrolled?
- How are unenrolled or unhealthy endpoints detected?
- Which controls prevent activity and which only observe it?
- Are alerts reviewed within the organization's declared response times?
- Are exceptions approved, bounded, and periodically reviewed?
- Can administrators silently disable or rewrite the evidence?
- Are policy and software changes reviewed and attributable?
- Are response actions tested, and are failures visible?
- Is evidence retained across a boundary separate from the monitored endpoint?
- Does the documented control match actual operation over the review period?

A compliance feature must answer these with real records, not generated narratives.

## 13. Delivery roadmap

### Phase 0 — Scope and threat validation

- Define Linux distributions, kernels, deployment types, and threat classes.
- Inventory required telemetry and enforcement mechanisms.
- Establish performance, event-loss, and false-positive budgets.
- Produce an explicit capability matrix: enforced, detected, observed, planned, and non-goal.
- Threat-model endpoint privilege, controller compromise, evidence compromise, and update compromise.

### Phase 1 — Agent-correlated host observation

- Define endpoint-event schemas.
- Add deployment and process identity correlation.
- Ingest existing Aegis session, mandate, broker, and audit events.
- Integrate selected low-risk host observations.
- Add sensor health and event-gap reporting.
- Export to one existing SIEM or event sink.

This phase remains monitoring; it must not claim host prevention.

### Phase 2 — Signed machine policy and fleet health

- Define a separate canonical machine-policy artifact.
- Add enrollment, signed generations, activation receipts, expiry, and revocation.
- Report expected versus effective endpoint state.
- Add selective per-deployment policy projection.
- Add controller-unavailable and offline-expiry behavior.

### Phase 3 — Safe containment

- Revoke Aegis sessions and broker capabilities.
- Revalidate and terminate identified process groups.
- Stop selected service units through a typed adapter.
- Add incident records and approval-gated playbooks.
- Verify postconditions and emit mitigation receipts.
- Exercise rollback and partial-failure paths.

### Phase 4 — Gateway expansion

- Add additional typed downstream brokers.
- Mediate high-value credential and API operations.
- Add destination, operation, and data-scope restrictions.
- Remove ambient credentials and equivalent bypass routes where feasible.
- Clearly report resources that remain unmediated.

### Phase 5 — Endpoint and security-platform integrations

- Integrate one validated Linux sensor stack.
- Integrate one EDR isolation path.
- Integrate identity and credential revocation.
- Integrate ticketing and notification workflows.
- Add multi-endpoint correlation and threat-intelligence input.

### Phase 6 — Stronger OS enforcement

- Evaluate SELinux/AppArmor, seccomp, eBPF LSM, firewall, and container controls.
- Compile selected machine policy into validated enforcement artifacts.
- Verify effective control state after activation.
- Measure bypasses, kernel compatibility, and operational failure modes.

### Phase 7 — Compliance evidence package

- Publish a control-capability and residual-risk matrix.
- Provide bounded evidence exports and retention verification.
- Add control-health, exception, and response-time reports.
- Document deployment and operating procedures.
- Run real controls over a representative period.
- Obtain independent security and compliance review before stronger claims.

## 14. Acceptance gates before endpoint-protection claims

Aegis should not market an endpoint capability until tests demonstrate, for its declared scope:

1. Every event is attributable to one enrolled deployment or explicitly marked unauthenticated.
2. PID reuse cannot redirect a response action to an unrelated process.
3. Stale, unsigned, wrong-target, and rolled-back machine policies fail according to declared policy.
4. Sensor loss and event gaps are visible within a bounded time.
5. Endpoint and agent-session events correlate without treating runtime narration as fact.
6. Automatic mitigations are typed, bounded, idempotent, and independently audited.
7. Failed and partially completed mitigations are distinguishable from successful containment.
8. Protected evidence survives endpoint log replacement under the documented external-retention assumptions.
9. Controller loss, offline operation, full buffers, clock skew, and restart behavior are tested.
10. Resource and latency overhead remain within published bounds.
11. False positives and false negatives are measured for the released detection set.
12. Update, rollback, enrollment, key rotation, and endpoint decommissioning are tested.
13. Equivalent unmediated routes are disclosed for every claimed gateway control.
14. An independent reviewer can reconstruct identity, policy, observation, decision, action, and receipt provenance.

## 15. Strategic recommendation

The machine-wide direction is coherent, but Aegis should enter it through its strongest unique advantage rather than by recreating an entire commodity endpoint stack immediately.

The recommended wedge is:

1. Preserve Aegis's external identity and authority model.
2. Add machine and workload identity.
3. Correlate Aegis-managed sessions with trustworthy host telemetry.
4. Mediate selected high-value actions through typed gateways.
5. Automate only narrow, high-confidence containment.
6. Integrate with established EDR and SIEM products for broad endpoint coverage.
7. Expand OS-level enforcement only where Aegis can verify the complete path.

The resulting differentiator is an end-to-end explanation such as:

```text
machine: build-worker-17
machine policy: generation 142, digest sha256:...
OS identity: aegis-runtime
logical agent: release-assistant
trust stanza: teamwide
charter: revision 7, digest sha256:...
mandate: M-..., active at observation time
runtime: Hermes 0.18.x, process identity P-...
requested authority: github/read for repository A
observed behavior: unexpected shell child contacted destination B
decision: outside approved machine and agent authority
response: mandate revoked; process tree terminated
verification: process identity absent; new sessions denied
incident: I-...
evidence retention: externally acknowledged checkpoint C-...
```

Conventional endpoint telemetry may see the process and destination. Aegis can additionally explain the authenticated identity, approved intent, exact agent security context, authority mismatch, and independently authorized response.

That is the durable product opportunity:

> Identity and authority are established outside every workload; machine and runtime behavior are observed at trustworthy boundaries; consequential actions are mediated at real enforcement points; and every decision and mitigation is attributable, bounded, and reviewable.

## 16. Open decisions

Before converting this report into normative specifications, the project must decide:

1. Is the first protected endpoint a physical Linux machine, VM, container host, or a narrower deployment class?
2. Which threat classes and Linux versions define the first support contract?
3. Which sensor stack supplies process, file, identity, and network events?
4. Which controls are observe-only, detect-after-action, or preventive?
5. What privileges are acceptable for the endpoint sensor and enforcer?
6. How are machine keys protected, rotated, cloned, and revoked?
7. What is the canonical machine-policy schema and approval model?
8. Which events are retained locally, centrally, and across an independent boundary?
9. What are the offline and controller-unavailable rules?
10. Which mitigations may run automatically, and which require fresh approval?
11. Which existing EDR, SIEM, identity, and orchestration products are first integration targets?
12. What privacy and data-minimization rules apply to command, path, network, and user telemetry?
13. What measurable performance, coverage, event-loss, and response-time commitments can the project sustain?
14. What product name distinguishes current agent authority from future endpoint capabilities without overstating either?
15. Which independent security and compliance reviews are required before release claims change?

Until those decisions become approved specifications and tested implementation, this document remains a product and architecture exploration rather than a statement of shipped behavior.
