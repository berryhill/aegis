#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
version=${1:-}
expected_revision=${2:-}
work_root=

cleanup() {
  if [ -n "$work_root" ]; then
    rm -rf "$work_root"
  fi
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'release readiness denied: %s\n' "$*" >&2
  exit 1
}

[ "$#" -eq 2 ] || {
  printf 'usage: verify-release-readiness.sh VERSION SOURCE_REVISION\n' >&2
  exit 2
}
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
  printf 'VERSION must be MAJOR.MINOR.PATCH\n' >&2
  exit 2
}
printf '%s\n' "$expected_revision" | grep -Eq '^[0-9a-f]{40}$' || fail 'source revision must be one full lowercase commit digest'

actual_revision=$(git -C "$repo" rev-parse --verify HEAD^{commit})
[ "$actual_revision" = "$expected_revision" ] || fail "source revision mismatch: expected $expected_revision actual $actual_revision"
[ -z "$(git -C "$repo" status --porcelain=v1 --untracked-files=no)" ] || fail 'tracked source is dirty'

work_root=$(mktemp -d "$repo/.aegis-release-readiness-XXXXXXXX")
git -c core.hooksPath=/dev/null clone --quiet --no-checkout --no-hardlinks "$repo" "$work_root/candidate"
git -C "$work_root/candidate" -c core.hooksPath=/dev/null \
  checkout --quiet --detach "$expected_revision"
[ "$(git -C "$work_root/candidate" rev-parse HEAD)" = "$expected_revision" ] || fail 'candidate source revision mismatch'
[ -z "$(git -C "$work_root/candidate" status --porcelain=v1)" ] || fail 'candidate source snapshot is dirty before release transformation'

release_date=$(date +%F)
python3 "$work_root/candidate/scripts/prepare-release-changelog.py" \
  "$work_root/candidate/CHANGELOG.md" "$work_root/candidate/CHANGELOG.prepared.md" \
  "$version" "$release_date"
mv "$work_root/candidate/CHANGELOG.prepared.md" "$work_root/candidate/CHANGELOG.md"
git -C "$work_root/candidate" add CHANGELOG.md
git -C "$work_root/candidate" \
  -c core.hooksPath=/dev/null \
  -c user.name='Aegis Release Readiness' \
  -c user.email='release-readiness@aegis.invalid' \
  commit --quiet -m "Prepare v$version release readiness candidate"
candidate_revision=$(git -C "$work_root/candidate" rev-parse HEAD)
[ -z "$(git -C "$work_root/candidate" status --porcelain=v1)" ] || fail 'release candidate snapshot is dirty'

(
  cd "$work_root/candidate"
  AEGIS_PROOF_SOCKET_DIR="$repo" make verify
  git diff --check
  [ -z "$(git status --porcelain=v1)" ] || fail 'verification mutated the release candidate snapshot'
)

printf 'release readiness verified: version=%s source_revision=%s candidate_revision=%s published=false\n' \
  "$version" "$expected_revision" "$candidate_revision"
