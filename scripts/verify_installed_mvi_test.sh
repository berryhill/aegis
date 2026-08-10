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
