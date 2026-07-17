# Runtime and Session Specification

## Explicit Hermes runtime

The MVP supports Hermes Agent `>=0.18.0,<0.19.0`. Discovery displays the executable, installation, runtime version, and Aegis adapter version. Unsupported versions fail closed, and the CLI never disguises Hermes behind a generic runtime label.

## Design sessions

Design uses Hermes safe mode, the structured TUI-gateway stdio protocol, `no_mcp`, and a disposable `HERMES_HOME`. It does not use one-shot mode and receives no provisioning, shell, arbitrary file-write, profile, plugin, MCP, cron, gateway, or ambient credential authority. Provider authentication is injected only when explicitly configured for design.

## Mandates

A short-lived mandate binds one authenticated subject, agent, stanza, charter revision/digest, resolved Hermes runtime, effective capabilities/toolsets, memory and credential scopes, environment, issue time, and expiry. The runtime cannot modify or extend it; delegation is forbidden.

## Operational launch

Each session starts a new Hermes process and disposable home with a minimal environment. Safe mode disables inherited project rules, user memories, plugins, and MCP. Aegis passes exactly the approved stanza toolset arguments and selected provider binding, verifies the resolved launch arguments, and records `toolset_verification: launch_arguments`.

This is process and Hermes-state isolation, not host filesystem, network, container, VM, or individual-tool runtime attestation.

## Lifecycle

Session records bind the mandate, process identity, clean runtime home, start time, status, and termination reason. Expired, revoked, invalid, missing, or PID-reused runtimes are terminated and fail closed through Aegis. Stanza changes and downshifts require a new clean session.
