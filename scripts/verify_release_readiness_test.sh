#!/bin/sh
set -eu

# Fixture commits must not inherit host hooks or identity.
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
root=$(mktemp -d "$repo/.aegis-release-readiness-test-XXXXXXXX")
cleanup() { rm -rf "$root"; }
trap cleanup EXIT HUP INT TERM

fail_test() {
  printf 'release readiness test failed: %s\n' "$*" >&2
  exit 1
}

expect_failure() {
  expected=$1
  shift
  set +e
  "$@" >"$root/stdout" 2>"$root/stderr"
  status=$?
  set -e
  [ "$status" -ne 0 ] || fail_test "command unexpectedly succeeded: $expected"
  grep -Fq "$expected" "$root/stderr" || {
    printf '%s\n' '--- stderr ---' >&2
    sed -n '1,120p' "$root/stderr" >&2
    fail_test "failure omitted: $expected"
  }
}

setup_repo() {
  name=$1
  fixture="$root/$name"
  mkdir -p "$fixture/scripts"
  cp "$repo/scripts/verify-release-readiness.sh" "$fixture/scripts/verify-release-readiness.sh"
  cp "$repo/scripts/prepare-release-changelog.py" "$fixture/scripts/prepare-release-changelog.py"
  cat >"$fixture/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

### Fixed

- Pending release change.

## [0.1.0] - 2026-01-01

- Previous release.
EOF
  cat >"$fixture/Makefile" <<'EOF'
verify:
	@test -z "$$(git status --porcelain=v1)"
	@grep -Eq '^## \[1.2.3\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$$' CHANGELOG.md
	@awk '/^## Unreleased$$/{getline; if ($$0 != "") exit 1; getline; if ($$0 !~ /^## \[1.2.3\] - /) exit 1}' CHANGELOG.md
	@printf 'fixture verification passed\n'
EOF
  git init -q -b main "$fixture"
  git -C "$fixture" -c user.name='Release Readiness Test' -c user.email='release-readiness@example.invalid' add .
  git -C "$fixture" -c user.name='Release Readiness Test' -c user.email='release-readiness@example.invalid' commit -q -m initial
}

setup_repo success
revision=$(git -C "$fixture" rev-parse HEAD)
before=$(sha256sum "$fixture/CHANGELOG.md" | cut -d' ' -f1)
(
  cd "$fixture"
  ./scripts/verify-release-readiness.sh 1.2.3 "$revision" >"$root/success-output"
)
after=$(sha256sum "$fixture/CHANGELOG.md" | cut -d' ' -f1)
[ "$before" = "$after" ] || fail_test 'readiness verification changed the source changelog'
grep -Fq 'fixture verification passed' "$root/success-output" || fail_test 'candidate verification did not run'
grep -Fq "release readiness verified: version=1.2.3 source_revision=$revision" "$root/success-output" || fail_test 'success identity was not reported'
[ -z "$(git -C "$fixture" status --porcelain=v1)" ] || fail_test 'readiness verification dirtied source'

setup_repo inherited-hook
revision=$(git -C "$fixture" rev-parse HEAD)
mkdir -p "$root/inherited-hooks"
cat >"$root/inherited-hooks/pre-commit" <<EOF
#!/bin/sh
printf 'pre-commit\n' >>"$root/inherited-hook-executed"
exit 91
EOF
cat >"$root/inherited-hooks/post-checkout" <<EOF
#!/bin/sh
printf 'post-checkout\n' >>"$root/inherited-hook-executed"
exit 92
EOF
chmod +x "$root/inherited-hooks/pre-commit" "$root/inherited-hooks/post-checkout"
git config --file "$root/inherited-gitconfig" core.hooksPath "$root/inherited-hooks"
(
  cd "$fixture"
  GIT_CONFIG_GLOBAL="$root/inherited-gitconfig" GIT_CONFIG_SYSTEM=/dev/null \
    ./scripts/verify-release-readiness.sh 1.2.3 "$revision" >"$root/inherited-hook-output"
)
[ ! -e "$root/inherited-hook-executed" ] || fail_test 'candidate construction executed an inherited Git hook'
grep -Fq "release readiness verified: version=1.2.3 source_revision=$revision" "$root/inherited-hook-output" || \
  fail_test 'hook-isolated readiness success identity was not reported'

printf '%s\n' dirty >>"$fixture/CHANGELOG.md"
expect_failure 'tracked source is dirty' \
  sh -c 'cd "$1" && ./scripts/verify-release-readiness.sh 1.2.3 "$2"' sh "$fixture" "$revision"
git -C "$fixture" checkout -q -- CHANGELOG.md

expect_failure 'source revision mismatch' \
  sh -c 'cd "$1" && ./scripts/verify-release-readiness.sh 1.2.3 0000000000000000000000000000000000000000' sh "$fixture"
expect_failure 'VERSION must be MAJOR.MINOR.PATCH' \
  sh -c 'cd "$1" && ./scripts/verify-release-readiness.sh latest "$2"' sh "$fixture" "$revision"

setup_repo empty-unreleased
revision=$(git -C "$fixture" rev-parse HEAD)
python3 - "$fixture/CHANGELOG.md" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text()
start = text.index("## Unreleased")
end = text.index("\n## [0.1.0]", start)
path.write_text(text[:start] + "## Unreleased\n" + text[end:])
PY
git -C "$fixture" -c user.name='Release Readiness Test' -c user.email='release-readiness@example.invalid' add CHANGELOG.md
git -C "$fixture" -c user.name='Release Readiness Test' -c user.email='release-readiness@example.invalid' commit -q -m 'empty unreleased'
revision=$(git -C "$fixture" rev-parse HEAD)
expect_failure 'CHANGELOG.md has no unreleased entries' \
  sh -c 'cd "$1" && ./scripts/verify-release-readiness.sh 1.2.3 "$2"' sh "$fixture" "$revision"

printf 'release readiness tests passed\n'
