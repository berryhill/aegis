# Five-Minute Quickstart

## Prerequisites

- Linux
- Go 1.26.5+
- Hermes Agent `>=0.18.0,<0.19.0` on `PATH`

Install the latest tagged Aegis release with `go install github.com/berryhill/aegis/cmd/aegis@latest`, or continue below to build a checkout. Release builds can subsequently use `aegis update --check` and `aegis update`.

## Build and configure

```sh
go build -o aegis ./cmd/aegis
cp examples/aegis.yaml .aegis.yaml
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

## Provider boundary

```sh
./aegis --config .aegis.yaml design --smoke
```

Without `credentials.design_provider` and its source credential, this must not be presented as a successful model turn. The command may reach Hermes and report its authentic provider-configuration failure. It uses disposable state and does not modify the normal Hermes profile.

Clean up with `rm -f aegis .aegis.yaml .office-charter.json && rm -rf .aegis`.
