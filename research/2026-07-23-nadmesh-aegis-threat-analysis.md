# NadMesh and Aegis: threat analysis and product implications

- Status: Complete supporting research; non-normative
- Date: 2026-07-23
- Prepared for: Aegis
- Trigger: The Hacker News report and X post published 2026-07-17
- Scope: NadMesh evidence, attack chain, credential-theft implications, current Aegis coverage, residual risk, and prioritized recommendations
- Evidence cutoff: 2026-07-23 UTC
- Current authority: `AGENTS.md`, `specs/`, and implemented/tested behavior remain authoritative

## Executive summary

NadMesh is credible evidence that attackers are treating exposed AI and automation infrastructure as an access path to higher-value cloud, cluster, registry, and model-provider authority. QiAnXin XLab's primary analysis describes a Go botnet that discovers AI services through Shodan, scans 30 ports, attempts more than 20 exploitation paths, establishes redundant persistence, and harvests environment credentials, AWS configuration, Kubernetes service-account tokens, Docker configuration, AI model access, and exploitable MCP tools [1].

The strongest Aegis-relevant conclusion is not merely that AI services need authentication. It is:

> Compromise of an AI runtime must not imply possession of the operator's reusable credentials or ambient infrastructure authority.

NadMesh validates Aegis's external-authority, exact-stanza, short-lived-mandate, narrow-tool, credential-broker, revocation, and audit thesis. It particularly validates the implemented GitHub broker's decision to apply a credential inside Aegis rather than return or inject it into Hermes. It also exposes important limits in the current MVP:

- Aegis does not prevent exploitation of an internet-exposed AI service.
- Aegis provides neither host nor network confinement.
- A compromised process running as the operator can still reach same-user files and other ambient host resources.
- Operational model-provider authentication remains environment-backed and therefore process-readable.
- The broker protects only one typed GitHub repository-metadata action; it does not protect arbitrary AWS, Kubernetes, Docker, model-provider, `.env`, or filesystem credentials.
- Aegis is not EDR, antimalware, or persistence detection.

Accordingly, NadMesh is strong validation of the product direction but not evidence that the present Aegis release would stop this campaign end to end. Aegis can reduce blast radius only for authority that is actually removed from the runtime and mediated at an unavoidable boundary. Identity metadata and prompt policy do not contain arbitrary code execution.

The highest-priority engineering direction is to make a compromised runtime an explicit acceptance-test adversary: remove reusable provider and downstream credentials from runtime environment/files, run the runtime under a distinct OS identity, constrain its egress, mediate protected actions through typed destination-bound brokers or proxies, and verify these properties with compromise-harness tests.

## 1. Evidence assessment

### 1.1 Primary source

QiAnXin XLab published the English NadMesh analysis on 2026-07-17 [1]. The report says XLab observed samples and exploit traffic through its sensors and analyzed controller and agent code. It includes architecture, target queries, scan ports, exploitation paths, harvested-data structures, persistence paths, and indicators.

High-confidence findings directly supported by XLab's technical analysis include:

- NadMesh is implemented as an autonomous scan, exploit, deploy, persist, and intelligence-harvesting system.
- Its Shodan harvester explicitly queries ComfyUI, Ollama, n8n, Open WebUI, Langflow, Stable Diffusion WebUI, and Gradio targets, then inserts results as high-priority scan tasks.
- It scans common web, Kubernetes, database, Docker, Elasticsearch, AI-service, and management ports.
- Its exploit inventory includes callable MCP command tools, Kubernetes pod creation with host mounts, Docker API container creation, Redis configuration abuse, weak SSH/Telnet credentials, Jenkins script execution, and other exposed administrative or vulnerable services.
- After compromise, it seeks environment credentials, AWS configuration, Kubernetes service-account tokens, Docker configuration, AI model access, and exploitable MCP services.
- It persists through an SSH authorized key, multiple hidden loader paths, and cron watchdogs.
- It uses Garble, UPX, and random padding, so a single sample hash is not a sufficient detector.

### 1.2 Secondary reporting and corroboration

The Hacker News report [2] accurately preserves an important distinction: the campaign deliberately prioritizes AI and MCP targets and loot, while much of XLab's observed exploit traffic still used conventional exposed infrastructure such as Docker APIs and Jenkins consoles. This is not a purely novel “AI exploit.” It is a cloud-intrusion and credential-theft operation that has incorporated AI infrastructure into an existing class of internet-scale attack workflow.

The Cloud Security Alliance synthesis [3] reaches the same strategic conclusion and correctly warns that the claimed `3,811` unique AWS keys came from the attacker's dashboard, not an independently audited victim count.

NVD and vendor advisories independently confirm the two recent vulnerabilities highlighted in secondary coverage:

- CVE-2026-39987 is a pre-authentication RCE in marimo before 0.23.0 through an unauthenticated terminal WebSocket. CISA added it to the Known Exploited Vulnerabilities catalog on 2026-04-23 [4][5].
- CVE-2026-41176 affects reachable rclone RC servers from 1.45.0 before 1.73.5 when started without global HTTP authentication; an unauthenticated caller can alter RC configuration and disable authorization for other methods [6]. It was not present in the CISA KEV feed at the evidence cutoff.

The MCP authorization specification dated 2025-11-25 says authorization is optional for MCP implementations. It recommends the defined authorization flow for HTTP transports and says stdio implementations should instead retrieve credentials from the environment [7]. This does not make exposed unauthenticated command tools safe. It reinforces the need for deployment-level authentication, narrow tool schemas, runtime isolation, and credential non-delivery.

### 1.3 Claims that require caution

The following should not be repeated as independently established facts:

- **`3,811` unique AWS keys:** attacker-dashboard telemetry reported by XLab; indicative, not independently verified.
- **Bot/deployment totals:** The Hacker News notes internal inconsistencies among the panel's bot, deployment, and 24-hour funnel counts.
- **Campaign-wide exploit percentages:** XLab's chart reflects its observed sensor traffic, not a complete census of every NadMesh attempt or successful compromise.
- **“Industrial-grade” or commercial intent:** XLab's characterization is plausible from the productized panel and development workflow, but monetization and operator identity are not established.
- **A general MCP vulnerability:** the reported MCP vector calls exposed tools such as `execute_command`. XLab does not attach one universal MCP CVE to that behavior. The immediate problem is unsafe tool exposure and missing authorization, not proof that the MCP protocol itself was universally broken.
- **One hash as campaign coverage:** polymorphic build behavior makes the published SHA-1 useful only for one observed sample.

## 2. Reconstructed attack chain

### Stage 1: target discovery

The controller continuously receives targets from Shodan searches and autonomous network scans. AI-service ports and previously productive targets receive higher priority. Empty queues do not stop scanning; bots generate additional random address ranges.

Security implication: a public listener should be assumed discoverable. “Nobody knows this IP” is not an access-control mechanism.

### Stage 2: initial access

NadMesh combines several classes of entry:

1. unauthenticated or weakly authenticated administrative functionality;
2. unsafe callable tools, including MCP-exposed command execution;
3. weak SSH/Telnet credentials;
4. known vulnerabilities in AI-adjacent and conventional infrastructure;
5. exposed Docker, Kubernetes, Redis, Jenkins, and similar control surfaces.

Security implication: this campaign does not depend on prompt injection or model cooperation. Arbitrary code execution bypasses prompt-level instructions and application-layer identity claims.

### Stage 3: persistence and resilience

The reported persistence set includes:

- an added SSH key in `~/.ssh/authorized_keys`;
- `/dev/shm/.a`;
- `/var/tmp/.a`;
- `/tmp/.a`;
- `/etc/cron.d/.sys_monitor`;
- `/etc/cron.d/.s`.

Multiple mechanisms restore one another. Obfuscation, packing, random padding, concurrent versions, and canary rollouts reduce the value of single-hash detection.

Security implication: killing the originally exploited process is not sufficient incident response. Revocation, containment, persistence removal, credential rotation, and downstream-use review are all required.

### Stage 4: authority harvesting

The malware's reported collection targets include:

- AWS keys in environment variables and AWS configuration;
- Kubernetes service-account tokens, potentially with cluster-level authority;
- Docker registry credentials;
- `.env` files;
- model-provider and AI-service access;
- SSH sessions and internal ranges;
- exploitable MCP tools.

Security implication: the host is an access broker whether or not it was designed as one. Every credential or callable administrative capability reachable from the compromised process becomes part of the effective blast radius.

### Stage 5: lateral and downstream impact

The stolen artifacts can outlive the runtime process and can authorize access to cloud APIs, clusters, registries, paid model accounts, internal networks, and additional services. Runtime cleanup does not revoke copied credentials.

Security implication: short session lifetime is valuable only if downstream authority is also short-lived, non-exportable, or reauthorized per operation.

## 3. Why this matters to Aegis

### 3.1 Thesis validated

NadMesh supports six central Aegis design choices:

1. **Authority outside the model.** Malware does not need to persuade a model if useful authority already exists in the process environment or filesystem.
2. **One exact security context per session.** A compromised teamwide session should not inherit principal, unrelated deployment, or cross-stanza credentials.
3. **No authority union.** Aggregating credentials and tools from multiple contexts turns one foothold into broad compromise.
4. **Short-lived mandates and revocation.** Durable bearer credentials are more valuable to an attacker than expiring, operation-bound capabilities.
5. **Typed mediation rather than secret delivery.** A broker that performs one constrained action without releasing the reusable credential directly reduces theft and replay opportunities.
6. **Authoritative audit outside model narration.** Incident reconstruction must bind identity, stanza, mandate, operation, destination, runtime, and outcome independently of compromised runtime output.

### 3.2 The crucial distinction: authorization versus containment

Aegis can decide which authenticated subject should receive which authority. NadMesh demonstrates a different but adjacent problem: what arbitrary code can access after it has compromised the runtime or host.

A trust stanza is not a sandbox. Once an attacker obtains code execution, policy objects help only where a separate enforcement point still controls the resource. For example:

- A GitHub token absent from Hermes and applied only inside the Aegis broker remains meaningfully protected from direct runtime file/environment theft.
- An AWS key in `~/.aws`, an API token in `.env`, or a provider key in the Hermes environment does not become protected because the charter omitted it.
- A mandate cannot prevent direct network use of a stolen credential if the runtime has an unrestricted alternate egress path.
- Revoking an Aegis session does not revoke a copied external bearer token.

Aegis therefore needs both planes:

- **authority plane:** authenticated identity, exact stanza, mandate, approval, revocation, and audit;
- **enforcement plane:** credential non-delivery, distinct process identity, filesystem isolation, typed brokers/proxies, destination controls, and network confinement.

## 4. Current Aegis coverage and gaps

The table below describes repository behavior at the evidence cutoff. It does not claim controls that are only proposed in research.

| NadMesh-relevant concern | Current Aegis control | What it helps | Residual gap |
|---|---|---|---|
| Prompt or display name claims principal authority | OS identity/`SO_PEERCRED`; prompt and stanza names are not authentication | Prevents model text from granting principal authority | Does not prevent service exploitation or same-account compromise |
| Cross-context authority aggregation | Exactly one stanza per session; zero/multiple matches deny; no union | Limits logical authority assigned by Aegis | Ambient host files and same-UID resources remain outside stanza enforcement |
| Ambient Hermes configuration, plugins, rules, or state | Disposable `HOME`/`HERMES_HOME`; generated exact bridge configuration | Reduces inherited runtime state and extension supply-chain exposure | A disposable home is not host filesystem or process confinement |
| Generic downstream credential theft | Encrypted authority plus exact bindings | Protects stored values at rest and structures authorization | Storage encryption alone does not make arbitrary credentials safely usable by a compromised runtime |
| GitHub credential theft | Implemented `github.get_repository.v1` broker applies the credential inside Aegis and returns a bounded allowlist | Reusable GitHub credential is absent from Hermes environment, arguments, and results | Only one read-only GitHub metadata operation is covered; production requires distinct identities |
| Provider-key theft | Only the selected provider binding is injected; unrelated ambient keys are removed | Narrows environment exposure relative to wholesale inheritance | The selected provider credential remains in the Hermes process environment and is readable after runtime compromise |
| Unrestricted exfiltration or direct credential reuse | Broker fixes destination, method, schema, and response for its one action | Prevents generic SSRF/secret retrieval through that broker | No runtime network confinement; alternate direct network paths remain |
| Runtime authority after expiry/revocation | Session supervision, process identity checks, capability cleanup, process-group termination | Stops the supervised runtime and broker capability | Does not detect or remove independent malware persistence or revoke already copied external credentials |
| Malware/persistence detection | No claimed EDR or host-threat coverage | Avoids a false security claim | NadMesh files, cron entries, SSH keys, scanning, and C2 require external host controls or a future endpoint plane |
| Incident reconstruction | Aegis audit binds control-plane identities and decisions | Reconstructs Aegis-authorized actions | Unmediated filesystem/network activity by malware is not comprehensively observed |
| Public AI-service exposure | Aegis design uses a structured local stdio Hermes gateway rather than provisioning a public agent service | Avoids one public network listener in the intended flow | Aegis does not inventory or firewall unrelated Ollama, ComfyUI, n8n, Gradio, MCP, Docker, or Jenkins deployments |

Repository evidence:

- `docs/ARCHITECTURE.md:66-70` documents exact selection, disposable runtime state, the implemented broker, environment-backed provider authentication, and the absence of host/network confinement.
- `docs/CREDENTIAL_BROKER.md:3-31` documents the one typed operation, complete reauthorization tuple, capability handling, and distinct-identity production requirement.
- `docs/CREDENTIAL_BROKER.md:56-85` documents fixed GitHub egress, exact bridge registration, continued provider environment binding, and explicit non-goals.
- `internal/app/service.go:67-80` resolves the selected model-provider value from an environment source and returns it for Hermes injection.
- `docs/THREAT_MODEL.md:43-86` states that kernel, filesystem, and network confinement are outside the MVP and identifies same-UID, provider-environment, and broker bypass residual risks.

## 5. Product conclusions

### Conclusion 1: NadMesh validates Aegis's credential-broker direction more than its prompt-security direction

The campaign wants exportable credentials and callable infrastructure authority. The best defense is not a better instruction telling the model to keep secrets. It is to keep reusable secrets out of the runtime and expose only typed, independently authorized operations.

### Conclusion 2: exact stanza authority is necessary but insufficient

Stanzas prevent Aegis from intentionally assigning principal authority to a teamwide session. They do not constrain arbitrary code running with the same Linux UID from reading resources that Linux allows. Security claims must continue to separate logical authorization from OS containment.

### Conclusion 3: model-provider credentials are a first-order remaining exposure

Current operational provider credentials are selected rather than ambient, which is an improvement, but they still enter the runtime environment. NadMesh explicitly targets model and cloud access artifacts. A reusable provider key should eventually terminate at an Aegis-owned inference proxy or equivalent protected service, not at Hermes.

### Conclusion 4: mediation must be unavoidable

A destination-bound broker is useful only if the runtime cannot bypass it through a generic shell, direct network access, inherited credential, alternate MCP server, or same-user file access. Network and OS boundaries are not optional hardening around the final architecture; they complete the mediation claim.

### Conclusion 5: exposure management belongs in deployment prerequisites

Aegis need not become an internet-exposure scanner to state and verify its own deployment assumptions. Aegis-managed runtime paths should avoid public listeners, bind locally, reject inherited remote-control configuration, and report any enforcement precondition it cannot verify.

### Conclusion 6: EDR remains complementary

NadMesh uses conventional persistence and polymorphism. Aegis should integrate with endpoint, SIEM, cloud, Kubernetes, and credential systems rather than claiming its identity/session control detects all malware. A future Aegis Endpoint plane may correlate host events with mandates, but it is not current MVP behavior.

## 6. Prioritized recommendations

### P0 — make runtime compromise an explicit security acceptance case

Add a retained adversarial test model:

> The attacker obtains arbitrary code execution as the Hermes runtime identity during an active session and attempts to enumerate files, inspect process environments, access Aegis state, call protected services directly, replay capabilities, survive session cleanup, and exfiltrate credentials.

Acceptance criteria should eventually prove:

1. no reusable downstream credential appears in runtime environment, argv, home, tool input/output, logs, audit, or transcript;
2. runtime UID cannot read the authority database, KEK material, broker state, audit authority, operator home, or another stanza's state;
3. `/proc` policy prevents runtime inspection of Aegis and peer processes;
4. runtime egress permits only required local proxies/brokers and approved destinations through those mediators;
5. terminating or revoking the session invalidates every session capability immediately;
6. a copied request cannot be replayed by a sibling or replacement process;
7. runtime-created persistence outside its disposable boundary is denied or detected;
8. canary credentials remain unobserved across the full compromise harness.

Until these pass on a documented production profile, retain the current no-sandbox and no-network-confinement language.

### P0 — remove reusable provider credentials from Hermes

Implement an Aegis-owned provider mediation boundary that:

- authenticates the exact session and mandate;
- fixes provider, account, model, route, and allowed API surface outside model control;
- stores or obtains the upstream credential outside the runtime identity;
- injects only a short-lived, audience-bound local capability into Hermes;
- prevents direct provider egress from the runtime;
- supports streaming and tool-call semantics without logging request/response secrets;
- revokes on session stop, mandate expiry, route change, or process identity loss.

A local bearer capability without egress confinement reduces exposure duration but does not by itself prevent another same-UID process from stealing and using it. Distinct identity and restrictive `/proc` are part of this item.

### P0 — publish and test a hardened production deployment profile

The profile should use separate Aegis service and runtime users and should define:

- ownership and modes for authority, state, socket, capability, runtime-home, and audit paths;
- restrictive process visibility;
- systemd hardening evaluated with `systemd-analyze security`;
- writable-path allowlists and read-only or inaccessible operator homes;
- no public runtime/control listener;
- explicit network ingress and egress policy;
- resource limits and process-group/cgroup cleanup;
- core-dump and swap policy;
- external checkpoint/evidence retention;
- failure behavior when a required boundary is unavailable.

Do not label the profile a sandbox until concrete confinement and bypass tests justify the term.

### P0 — add an incident runbook for exposed AI-runtime compromise

The runbook should require:

1. isolate the host or workload;
2. preserve volatile and external evidence where authorized;
3. stop exposed services and revoke Aegis mandates/capabilities;
4. inspect the XLab-reported SSH, loader, and cron persistence locations;
5. identify every credential, token, config, socket, tool, and downstream identity reachable by the compromised UID;
6. revoke credentials before issuing replacements;
7. remove persistence before replacement credentials become available;
8. inspect AWS, Kubernetes, registry, model-provider, GitHub, and other downstream usage while old credentials were active;
9. rebuild from trusted artifacts where integrity cannot be established;
10. verify exposure, identity, and egress controls before reactivation.

Single-hash scanning is insufficient because XLab reports per-build randomization.

### P1 — extend typed mediation by operation, not by generic secret retrieval

After the GitHub proof, prioritize high-value operations whose credentials NadMesh seeks:

- short-lived cloud role sessions with exact audience and policy;
- Kubernetes operations bound to exact cluster, namespace, verb, and resource;
- registry operations bound to exact registry/repository/action;
- model-provider inference bound to exact provider/model/account/budget;
- secret-manager actions that return bounded derived results rather than reusable secret bytes.

Reject generic `get_secret`, arbitrary HTTP proxy, arbitrary shell, caller-selected URL/header, or profile-wide credential lease designs as the default security boundary.

### P1 — make bypass paths visible in authority review

For each stanza, report:

- broad terminal/file/process tools;
- direct network availability;
- environment-injected credentials;
- readable host paths;
- inherited sockets or service identities;
- unmediated MCP/plugin surfaces;
- whether distinct runtime identity and network confinement are active;
- which grants are logical policy only versus externally enforced.

A reviewer should be able to see when a narrow broker coexists with a broad bypass.

### P1 — verify Aegis-managed listener posture

For every Aegis-started component:

- prefer inherited stdio or pathname Unix sockets;
- bind TCP only when explicitly required;
- reject wildcard/public binds by default;
- authenticate before accepting privileged requests;
- report actual bound endpoints after launch;
- fail closed if the verified endpoint differs from the approved plan.

This does not inventory third-party AI services on the host, but it prevents Aegis from adding the exposure pattern NadMesh searches for.

### P2 — integrate host and cloud detections without overstating coverage

A future endpoint/integration layer can correlate:

- new SSH authorized keys;
- cron/service persistence;
- hidden executable creation in temporary or shared-memory paths;
- unexpected public listeners on AI and control-plane ports;
- high-rate scanning;
- access to known credential paths;
- unauthorized cloud, Kubernetes, registry, or model-provider usage;
- runtime behavior after mandate expiry.

Every result should declare its sensor source, scope, health, and gaps. Existing EDR, cloud audit, Kubernetes audit, and SIEM systems remain the preferred mature sources.

## 7. Defensive actions for current Aegis users

Before the recommended architecture is complete:

- Do not expose Aegis, Hermes, Ollama, MCP, or related control interfaces directly to the public internet.
- Keep AI development services loopback-only or behind authenticated, allowlisted access paths.
- Treat any stanza with terminal or broad file tools as host-facing authority.
- Use the implemented broker for its exact GitHub operation rather than injecting that GitHub token into Hermes.
- Use distinct, low-privilege credentials per deployment and stanza; do not reuse operator-wide keys.
- Prefer short-lived cloud/workload identities over static keys.
- Keep AWS, Kubernetes, Docker, `.env`, and other unrelated credentials inaccessible to the runtime UID; a disposable `HOME` alone is not sufficient.
- Run Aegis and the runtime under distinct OS identities where the broker production contract requires it.
- Restrict `/proc`, core dumps, writable paths, and outbound network access with OS/deployment controls.
- Monitor provider and infrastructure usage independently of Aegis audit.
- If an exposed service may have been compromised, assume every credential readable by that process was copied and follow the incident runbook rather than merely killing the process.

## 8. Suggested threat-model addition

The next normative threat-model update should add this explicit abuse case after implementation scope is agreed:

| Abuse case | Required control direction | Residual risk to state explicitly |
|---|---|---|
| Internet-exposed or vulnerable AI/MCP service gives an attacker arbitrary code execution as the runtime identity; attacker reads credentials, calls alternate egress, establishes persistence, and survives session cleanup | No public Aegis-managed listener by default; distinct runtime identity; credential non-delivery; typed broker/proxy; restrictive filesystem/process/network policy; session/cgroup teardown; external host/cloud detection and credential revocation | Current MVP lacks comprehensive host/network confinement and malware detection; any ambient same-UID file, environment credential, direct egress path, or externally exposed third-party service remains reachable according to OS policy |

This must not be phrased as an implemented prevention claim until the production deployment and compromise tests exist.

## 9. Bottom line

NadMesh is not proof that Aegis already solves AI-runtime compromise. It is evidence that Aegis is aimed at the right authority problem.

The campaign operationalizes a simple fact: AI hosts frequently combine exposed control surfaces with valuable ambient credentials. The durable Aegis response is to ensure that an agent runtime receives an authenticated, narrow, expiring ability to perform approved operations—not possession of the reusable credentials and host authority behind those operations.

The product promise should therefore remain precise:

> Aegis establishes and mediates agent authority outside the model so that compromise of one runtime can be constrained to one explicit security context and a bounded set of enforceable operations.

That promise becomes defensible only to the extent that credentials are absent from the runtime, broker/proxy paths are unavoidable, and OS/network controls prevent bypass. NadMesh makes closing those remaining enforcement gaps a product priority.

## References

1. QiAnXin XLab, “NadMesh Botnet Analysis: A Product-Grade Threat for the AI Service Era,” 2026-07-17. https://blog.xlab.qianxin.com/nadmesh-botnet-analysis-a-product-grade-threat-for-the-ai-service-era-en/
2. The Hacker News, “New NadMesh Botnet Hunts Exposed AI Services for Cloud Keys and Kubernetes Tokens,” 2026-07-17. https://thehackernews.com/2026/07/new-nadmesh-botnet-hunts-exposed-ai.html
3. Cloud Security Alliance AI Safety Initiative, “NadMesh: A Botnet Built to Hunt Exposed AI Infrastructure for Cloud Keys,” 2026-07-20. https://labs.cloudsecurityalliance.org/research/csa-research-note-nadmesh-ai-infrastructure-botnet-20260720/
4. NVD, CVE-2026-39987. https://nvd.nist.gov/vuln/detail/CVE-2026-39987
5. CISA, Known Exploited Vulnerabilities Catalog, CVE-2026-39987 entry; live JSON feed checked 2026-07-23. https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json
6. GitHub Security Advisory, “rclone unauthenticated RC server authorization bypass,” GHSA-25qr-6mpr-f7qx; NVD CVE-2026-41176. https://github.com/rclone/rclone/security/advisories/GHSA-25qr-6mpr-f7qx and https://nvd.nist.gov/vuln/detail/CVE-2026-41176
7. Model Context Protocol, “Authorization,” specification 2025-11-25. https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
8. Aegis architecture. `docs/ARCHITECTURE.md`
9. Aegis local session credential broker. `docs/CREDENTIAL_BROKER.md`
10. Aegis MVP threat model. `docs/THREAT_MODEL.md`
11. Aegis host endpoint monitoring, enforcement, and response research. `research/2026-07-17-host-endpoint-monitoring-enforcement-response.md`
