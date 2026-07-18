# Five-Minute Quickstart

## Prerequisites

- Linux
- Go 1.26.5+
- Hermes Agent `>=0.18.0,<0.19.0` on `PATH`

Install the latest tagged Aegis source with `go install github.com/berryhill/aegis/cmd/aegis@latest`, or continue below to build a checkout. Self-update requires a published, non-draft stable GitHub release with assets, not merely a local or remote Git tag; until publication completes it correctly reports the previous published stable version. `aegis --update` is the strict root-only alias for `aegis update`; both use the same checksum-verifying service.

## Build and configure

```sh
go build -o aegis ./cmd/aegis
cp examples/aegis.yaml .aegis.yaml
chmod 600 .aegis.yaml
uid=$(id -u)
user=$(id -un)
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/; s/REPLACE_WITH_LOCAL_USERNAME/$user/" .aegis.yaml
cp examples/office-charter.json .office-charter.json
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/g; s/REPLACE_WITH_LOCAL_USERNAME/$user/g" .office-charter.json
```

The copied files are local working files and should not be committed.

## Verify the no-key path

```sh
./aegis --config .aegis.yaml runtime
./aegis --config .aegis.yaml charter validate .office-charter.json
./aegis --config .aegis.yaml config
```

Success means Hermes is named and versioned explicitly, charter validation returns a canonical digest, and the API token is shown as `[REDACTED]`.

Alternatively, a genuinely new installation can run `./aegis init` in a terminal. Review the displayed configuration/state paths and exact configuration, then type `yes`; this does not start Hermes/Ollama, download a model, create credentials, modify profiles, or provision an agent.

Verify the initialized non-interactive manager boundary without starting Hermes or Ollama:

```sh
printf 'not chat' | ./aegis --config .aegis.yaml
# exits with manager_requires_tty and names deterministic subcommands
```

Without configuration, the same non-TTY invocation instead emits structured `manager_not_initialized` output naming `aegis init` and exits 2 without prompting.

Run `./aegis --config .aegis.yaml` in a real terminal to inspect the built-in manager shell. It does not download a model. Until an exact local artifact has passed the opt-in conformance suite, ordinary prose reports that no cloud fallback was attempted; `/status`, `/audit verify`, `/help`, and `/quit` remain local.

Do not fill the manager model fields merely to suppress that denial. For an already-installed official candidate, first record the exact Ollama artifact digest and a mode-`0600` certification destination, then explicitly invoke `./aegis --config .aegis.yaml manager certify CANDIDATE_ID`. This live command loads and tests the artifact through disposable Hermes and the authenticated loopback proxy; it writes no certification unless all security-critical and operational cases pass. Model installation/downloading remains an operator action outside this quickstart.

## Provider boundary

```sh
./aegis --config .aegis.yaml design --smoke
```

Without `credentials.design_provider` and its source credential, this must not be presented as a successful model turn. The command may reach Hermes and report its authentic provider-configuration failure. It uses disposable state and does not modify the normal Hermes profile.

Clean up with `rm -f aegis .aegis.yaml .office-charter.json && rm -rf .aegis`.
