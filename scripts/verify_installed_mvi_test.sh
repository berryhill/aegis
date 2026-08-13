#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root=$(mktemp -d "$repo/.aegis-installed-mvi-test-XXXXXXXX")
cleanup() { rm -rf "$root"; }
trap cleanup EXIT HUP INT TERM

fail_test() {
  printf 'installed MVI test failed: %s\n' "$*" >&2
  exit 1
}

expect_status_2() {
  expected=$1
  shift
  set +e
  "$@" >"$root/stdout" 2>"$root/stderr"
  status=$?
  set -e
  [ "$status" -eq 2 ] || fail_test "command exited $status instead of 2"
  grep -Fq "$expected" "$root/stderr" || fail_test "failure omitted: $expected"
}

# Release versions are exact stable SemVer. Invalid input must fail before creating output.
invalid_dist=$root/invalid-dist
expect_status_2 'version must be exact stable SemVer: 1.2.3-rc.1' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3-rc.1 "$invalid_dist"
[ ! -e "$invalid_dist" ] || fail_test 'invalid version created release output'

expect_status_2 'version must be exact stable SemVer: 01.2.3' \
  "$repo/scripts/verify-installed-mvi.sh" 01.2.3 "$invalid_dist"
[ ! -e "$invalid_dist" ] || fail_test 'leading-zero version created release output'

# Source provenance is local, exact, and fail-closed before build/output mutation.
expect_status_2 'expected revision is not one exact lowercase 40-hex value: not-a-revision' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3 "$invalid_dist" not-a-revision
expect_status_2 'revision mismatch: expected 0000000000000000000000000000000000000000' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3 "$invalid_dist" 0000000000000000000000000000000000000000

missing_vcs=$root/missing-vcs
mkdir -p "$missing_vcs/scripts"
cp "$repo/scripts/verify-installed-mvi.sh" "$missing_vcs/scripts/verify-installed-mvi.sh"
expect_status_2 'Git worktree metadata is unavailable' \
  env GIT_CEILING_DIRECTORIES="$root" "$missing_vcs/scripts/verify-installed-mvi.sh" 1.2.3 "$missing_vcs/dist"

source_fixture=$root/source-fixture
mkdir -p "$source_fixture/scripts"
cp "$repo/scripts/verify-installed-mvi.sh" "$source_fixture/scripts/verify-installed-mvi.sh"
printf 'fixture\n' >"$source_fixture/README.md"
GIT_CONFIG_GLOBAL=/dev/null git -C "$source_fixture" init --quiet
GIT_CONFIG_GLOBAL=/dev/null git -C "$source_fixture" -c user.name='Aegis Fixture' -c user.email='aegis@example.invalid' add .
GIT_CONFIG_GLOBAL=/dev/null git -C "$source_fixture" -c user.name='Aegis Fixture' -c user.email='aegis@example.invalid' commit --quiet -m fixture
printf '\n' >>"$source_fixture/README.md"
fixture_revision=$(git -C "$source_fixture" rev-parse HEAD)
expect_status_2 'tracked source worktree is dirty' \
  "$source_fixture/scripts/verify-installed-mvi.sh" 1.2.3 "$source_fixture/dist" "$fixture_revision"
[ -d "$source_fixture/dist" ] && [ -z "$(find "$source_fixture/dist" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail_test 'dirty source populated release output'

# Existing release output must be an empty real directory; never overwrite or follow a symlink.
nonempty=$root/nonempty
mkdir "$nonempty"
printf 'sentinel\n' >"$nonempty/sentinel"
expect_status_2 'release output must be empty:' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3 "$nonempty"
[ "$(cat "$nonempty/sentinel")" = sentinel ] || fail_test 'non-empty output was mutated'

real_dist=$root/real-dist
linked_dist=$root/linked-dist
mkdir "$real_dist"
ln -s "$real_dist" "$linked_dist"
expect_status_2 'release output must not be a symlink:' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3 "$linked_dist"
[ -z "$(find "$real_dist" -mindepth 1 -maxdepth 1 -print -quit)" ] || fail_test 'symlink target was mutated'

real_parent=$root/real-parent
linked_parent=$root/linked-parent
mkdir "$real_parent"
ln -s "$real_parent" "$linked_parent"
expect_status_2 'release output must not traverse symlinks:' \
  "$repo/scripts/verify-installed-mvi.sh" 1.2.3 "$linked_parent/dist"
[ ! -e "$real_parent/dist" ] || fail_test 'symlink-parent denial mutated its target'

printf 'installed MVI denial tests passed\n'
