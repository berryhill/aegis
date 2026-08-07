#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${1:-}
expected_revision=${2:-}
hermes_input=${3:-}
workspace_input=${4:-}
decision_input=${5:-}

usage() {
  printf '%s\n' 'usage: verify-release-candidate.sh VERSION SOURCE_REVISION HERMES_EXECUTABLE EMPTY_WORKSPACE DECISION.json' >&2
  exit 2
}
fail() {
  printf 'release candidate denied: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 5 ] || usage
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || usage
printf '%s\n' "$expected_revision" | grep -Eq '^[0-9a-f]{40}$' || usage

canonical_existing_file() {
  input=$1
  [ -f "$input" ] && [ ! -L "$input" ] || fail "required file is missing, not regular, or a symlink: $input"
  directory=$(CDPATH= cd -- "$(dirname -- "$input")" && pwd -P) || fail "cannot resolve parent: $input"
  printf '%s/%s\n' "$directory" "$(basename -- "$input")"
}

hermes=$(canonical_existing_file "$hermes_input")
[ -x "$hermes" ] || fail "Hermes executable is not executable: $hermes"
decision=$(canonical_existing_file "$decision_input")

case "$workspace_input" in
  /*) workspace=$workspace_input ;;
  *) workspace=$repo/$workspace_input ;;
esac
[ ! -L "$workspace" ] || fail "workspace must not be a symlink: $workspace"
mkdir -p "$workspace"
workspace=$(CDPATH= cd -- "$workspace" && pwd -P) || fail "cannot resolve workspace"
case "$workspace" in
  "$repo"/*) ;;
  *) fail "workspace must be repository-local: $workspace" ;;
esac
[ -z "$(find "$workspace" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail "workspace must be empty: $workspace"

actual_revision=$(git -C "$repo" rev-parse HEAD)
[ "$actual_revision" = "$expected_revision" ] || fail "source revision mismatch: expected $expected_revision actual $actual_revision"

# Parse the bounded, exact decision before building anything. This records an
# operator-supplied decision; it does not authenticate the operator or publish.
decision_canonical=$workspace/decision.json
DECISION_INPUT=$decision DECISION_OUTPUT=$decision_canonical VERSION=$version REVISION=$expected_revision python3 - <<'PY'
import json, os, pathlib, re, sys
source = pathlib.Path(os.environ["DECISION_INPUT"])
if source.stat().st_size > 16 * 1024:
    raise SystemExit("release candidate denied: decision manifest exceeds 16 KiB")
try:
    value = json.loads(source.read_text(encoding="utf-8"))
except (UnicodeDecodeError, json.JSONDecodeError) as exc:
    raise SystemExit(f"release candidate denied: invalid decision manifest: {exc}")
required = {"schema_version", "candidate_version", "source_revision", "decision", "decided_by", "rationale"}
if not isinstance(value, dict) or set(value) != required:
    raise SystemExit("release candidate denied: decision manifest fields are not exact")
if value["schema_version"] != 1:
    raise SystemExit("release candidate denied: unsupported decision manifest schema")
if value["candidate_version"] != os.environ["VERSION"] or value["source_revision"] != os.environ["REVISION"]:
    raise SystemExit("release candidate denied: decision manifest candidate identity mismatch")
if value["decision"] not in {"release", "hold", "withdraw"}:
    raise SystemExit("release candidate denied: decision must be release, hold, or withdraw")
for field in ("decided_by", "rationale"):
    text = value[field]
    if not isinstance(text, str) or not text.strip() or len(text.encode()) > 4096 or re.search(r"[\x00-\x08\x0b\x0c\x0e-\x1f]", text):
        raise SystemExit(f"release candidate denied: invalid {field}")
pathlib.Path(os.environ["DECISION_OUTPUT"]).write_text(
    json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8"
)
PY

[ -z "$(git -C "$repo" status --porcelain=v1 --untracked-files=no)" ] || fail 'tracked source is dirty'

git clone --quiet --no-hardlinks "$repo" "$workspace/source"
git -C "$workspace/source" checkout --quiet --detach "$expected_revision"
[ "$(git -C "$workspace/source" rev-parse HEAD)" = "$expected_revision" ] || fail 'detached source snapshot revision mismatch'
[ -z "$(git -C "$workspace/source" status --porcelain=v1)" ] || fail 'detached source snapshot is dirty'

# Build the four archives exactly once from the detached exact revision. Every
# later check uses the one extracted native candidate and retained evidence.
"$workspace/source/scripts/verify-installed-mvi.sh" "$version" "$workspace/dist"

native_os=$(go env GOOS)
native_arch=$(go env GOARCH)
native_archive=$workspace/dist/aegis_v${version}_${native_os}_${native_arch}.tar.gz
[ -f "$native_archive" ] && [ ! -L "$native_archive" ] || fail "native archive is unavailable: $native_archive"
mkdir -m 0700 "$workspace/candidate"
tar -xzf "$native_archive" -C "$workspace/candidate"
candidate=$workspace/candidate/aegis
[ -f "$candidate" ] && [ ! -L "$candidate" ] && [ -x "$candidate" ] || fail 'extracted candidate is not one regular executable'
[ "$("$candidate" --version)" = "aegis version $version" ] || fail 'exact candidate version mismatch'

candidate_sha=$(sha256sum "$candidate" | cut -d' ' -f1)
hermes_sha=$(sha256sum "$hermes" | cut -d' ' -f1)
decision_sha=$(sha256sum "$decision_canonical" | cut -d' ' -f1)
set +e
timeout 10 "$hermes" --version >"$workspace/hermes-version.out" 2>&1
hermes_status=$?
if [ "$hermes_status" -ne 0 ]; then
  timeout 10 "$hermes" version >"$workspace/hermes-version.out" 2>&1
  hermes_status=$?
fi
set -e
[ "$hermes_status" -eq 0 ] || fail 'Hermes version identity command failed or timed out'
[ "$(wc -c <"$workspace/hermes-version.out")" -le 4096 ] || fail 'Hermes version identity exceeds 4 KiB'
hermes_version=$(tr '\n' ' ' <"$workspace/hermes-version.out" | cut -c1-512)
[ -n "$hermes_version" ] || fail 'Hermes version identity is empty'
printf '%s\n' "$hermes_version" | grep -Eq '(^|[^0-9])0\.18\.[0-9]+([^0-9]|$)' || fail 'Hermes version is outside supported >=0.18.0,<0.19.0 range'
rm -f "$workspace/hermes-version.out"

# Rehearse replacement and exact rollback only in the task-owned workspace.
mkdir -m 0700 "$workspace/rehearsal" "$workspace/rehearsal/bin"
printf '#!/bin/sh\nprintf "previous-install\\n"\n' >"$workspace/rehearsal/bin/aegis"
chmod 0700 "$workspace/rehearsal/bin/aegis"
previous_sha=$(sha256sum "$workspace/rehearsal/bin/aegis" | cut -d' ' -f1)
cp "$candidate" "$workspace/rehearsal/bin/aegis.candidate"
chmod 0700 "$workspace/rehearsal/bin/aegis.candidate"
[ "$(sha256sum "$workspace/rehearsal/bin/aegis.candidate" | cut -d' ' -f1)" = "$candidate_sha" ] || fail 'staged candidate digest mismatch'
mv "$workspace/rehearsal/bin/aegis" "$workspace/rehearsal/bin/aegis.previous"
mv "$workspace/rehearsal/bin/aegis.candidate" "$workspace/rehearsal/bin/aegis"
[ "$("$workspace/rehearsal/bin/aegis" --version)" = "aegis version $version" ] || fail 'installed rehearsal candidate identity mismatch'
mv "$workspace/rehearsal/bin/aegis" "$workspace/rehearsal/bin/aegis.withdrawn"
mv "$workspace/rehearsal/bin/aegis.previous" "$workspace/rehearsal/bin/aegis"
[ "$(sha256sum "$workspace/rehearsal/bin/aegis" | cut -d' ' -f1)" = "$previous_sha" ] || fail 'rollback did not restore the exact previous executable'

# Rehearse withdrawal from a local publication staging area. Original candidate
# evidence is retained and no tag, release, remote, or production install changes.
mkdir -m 0700 "$workspace/rehearsal/publication"
cp "$workspace/dist"/*.tar.gz "$workspace/dist/SHA256SUMS" "$workspace/rehearsal/publication/"
rm -f "$workspace/rehearsal/publication"/*
[ -z "$(find "$workspace/rehearsal/publication" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail 'withdrawal rehearsal left staged publication assets'
(
  cd "$workspace/dist"
  sha256sum -c SHA256SUMS >/dev/null
)

manifest=$workspace/release-candidate-evidence.json
MANIFEST=$manifest VERSION=$version REVISION=$expected_revision CANDIDATE=$candidate CANDIDATE_SHA=$candidate_sha \
HERMES=$hermes HERMES_SHA=$hermes_sha HERMES_VERSION=$hermes_version DECISION=$decision_canonical DECISION_SHA=$decision_sha \
NATIVE_OS=$native_os NATIVE_ARCH=$native_arch python3 - <<'PY'
import json, os, pathlib
manifest = {
    "schema_version": 1,
    "candidate": {
        "version": os.environ["VERSION"],
        "source_revision": os.environ["REVISION"],
        "binary_path": os.environ["CANDIDATE"],
        "binary_sha256": os.environ["CANDIDATE_SHA"],
        "native_target": os.environ["NATIVE_OS"] + "/" + os.environ["NATIVE_ARCH"],
    },
    "hermes": {
        "executable_path": os.environ["HERMES"],
        "executable_sha256": os.environ["HERMES_SHA"],
        "version_output": os.environ["HERMES_VERSION"],
    },
    "decision": {
        "manifest_path": os.environ["DECISION"],
        "manifest_sha256": os.environ["DECISION_SHA"],
    },
    "proofs": {
        "archive_targets": 4,
        "checksums": "verified",
        "installed_first_run": "fail_closed_no_mutation",
        "exact_binary_reused": True,
        "rollback": "exact_previous_binary_restored",
        "withdrawal": "local_staging_cleared",
        "published": False,
        "production_install_mutated": False,
    },
}
pathlib.Path(os.environ["MANIFEST"]).write_text(
    json.dumps(manifest, sort_keys=True, separators=(",", ":")) + "\n", encoding="utf-8"
)
PY
chmod 0600 "$decision_canonical" "$manifest"

printf 'release candidate verified: version=%s revision=%s candidate_sha256=%s decision_recorded=true published=false evidence=%s\n' \
  "$version" "$expected_revision" "$candidate_sha" "$manifest"
