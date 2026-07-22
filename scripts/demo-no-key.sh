#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/aegis-no-key-XXXXXXXX")
cleanup() { rm -rf "$work"; }
trap cleanup EXIT HUP INT TERM

cd "$repo"
go build -ldflags='-X github.com/berryhill/aegis/internal/buildinfo.Version=test' -o "$work/aegis" ./cmd/aegis
cp examples/aegis.yaml "$work/aegis.yaml"
cp examples/office-charter.json "$work/office-charter.json"
chmod 0600 "$work/aegis.yaml" "$work/office-charter.json"
uid=$(id -u)
user=$(id -un)
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/g; s/REPLACE_WITH_LOCAL_USERNAME/$user/g" "$work/aegis.yaml" "$work/office-charter.json"
cd "$work"

sanitize() {
  sed \
    -e "s|$HOME|<HOME>|g" \
    -e "s|local-uid:$uid|local-uid:<LOCAL_UID>|g" \
    -e "s|\"uid\": \"$uid\"|\"uid\": \"<LOCAL_UID>\"|g" \
    -e "s|\"user\": \"$user\"|\"user\": \"<LOCAL_USER>\"|g"
}

printf '%s\n' '== Explicit Hermes discovery =='
./aegis --config aegis.yaml runtime | sanitize
printf '%s\n' '== Strict charter validation =='
./aegis --config aegis.yaml charter validate office-charter.json >/dev/null
printf '%s\n' 'Strict validation passed.'
printf '%s\n' '== Redacted effective configuration =='
./aegis --config aegis.yaml config | sanitize
printf '%s\n' '== Real no-key design boundary (failure is expected) =='
if ./aegis --config aegis.yaml design --smoke >"$work/design.out" 2>&1; then
  sanitize <"$work/design.out"
  printf '%s\n' 'Design succeeded because an explicit configured provider was available.'
else
  status=$?
  sanitize <"$work/design.out"
  printf 'Design stopped at an authentic unavailable-runtime/provider boundary (exit %s); no model success is claimed.\n' "$status"
fi
