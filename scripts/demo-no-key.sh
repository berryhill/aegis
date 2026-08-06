#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "$repo/.aegis-no-key-XXXXXXXX")
binary=$(mktemp "$repo/aegis-no-key-demo-XXXXXXXX")
cleanup() { rm -rf "$work"; rm -f "$binary"; }
trap cleanup EXIT HUP INT TERM

profile_home=$HOME
case "$repo/" in
  "$profile_home"/*) ;;
  *) profile_home=$(dirname "$repo") ;;
esac
aegis() { HOME=$profile_home "$binary" "$@"; }

cd "$repo"
go build -o "$binary" ./cmd/aegis
cp examples/aegis.yaml "$work/aegis.yaml"
cp examples/office-charter.json "$work/office-charter.json"
chmod 0600 "$work/aegis.yaml" "$work/office-charter.json"
uid=$(id -u)
user=$(id -un)
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/g; s/REPLACE_WITH_LOCAL_USERNAME/$user/g" "$work/aegis.yaml" "$work/office-charter.json"
cd "$work"
mkdir -m 0700 "$work/.aegis"
go run "$repo/scripts/demo-authority-init" "$work/.aegis/state/persistence/authority-v1"

sanitize() {
  sed \
    -e "s|$profile_home|<PROFILE_HOME>|g" \
    -e "s|$HOME|<HOME>|g" \
    -e "s|local-uid:$uid|local-uid:<LOCAL_UID>|g" \
    -e "s|\"uid\": \"$uid\"|\"uid\": \"<LOCAL_UID>\"|g" \
    -e "s|\"user\": \"$user\"|\"user\": \"<LOCAL_USER>\"|g"
}

run_sanitized() {
  output=$1
  shift
  if aegis "$@" >"$output" 2>&1; then
    sanitize <"$output"
  else
    status=$?
    sanitize <"$output"
    return "$status"
  fi
}

printf '%s\n' '== Explicit Hermes discovery =='
if ! run_sanitized "$work/runtime.out" --config aegis.yaml runtime; then
  printf '%s\n' 'Hermes discovery failed; install a supported Hermes version before running this demonstration.' >&2
  exit 1
fi
printf '%s\n' '== Checkout execution profile =='
run_sanitized "$work/version.out" version
printf '%s\n' '== Strict charter validation =='
aegis --config aegis.yaml charter validate office-charter.json >/dev/null
printf '%s\n' 'Strict validation passed.'
printf '%s\n' '== Redacted effective configuration =='
run_sanitized "$work/config.out" --config aegis.yaml config
printf '%s\n' '== Real no-key design boundary (failure is expected) =='
if aegis --config aegis.yaml design --smoke >"$work/design.out" 2>&1; then
  sanitize <"$work/design.out"
  printf '%s\n' 'Design succeeded because an explicit configured provider was available.'
else
  status=$?
  sanitize <"$work/design.out"
  printf 'Design stopped at an authentic unavailable-runtime/provider boundary (exit %s); no model success is claimed.\n' "$status"
fi
