#!/bin/sh
set -eu

root=$(mktemp -d "${TMPDIR:-/tmp}/aegis-release-test-XXXXXXXX")
cleanup() { rm -rf "$root"; }
trap cleanup EXIT HUP INT TERM

real_git=$(command -v git)
mkdir -p "$root/bin" "$root/repo/scripts"

cat >"$root/bin/git" <<'EOF'
#!/bin/sh
if [ "${1:-}" = tag ] && [ "${2:-}" = -s ]; then
    printf 'fixture signing failure\n' >&2
    exit 79
fi
exec "$REAL_GIT" "$@"
EOF
cat >"$root/bin/make" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0700 "$root/bin/git" "$root/bin/make"

cp scripts/release.sh "$root/repo/scripts/release.sh"
cat >"$root/repo/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

### Fixed

- Pending release change.
EOF

(
    cd "$root/repo"
    "$real_git" init -q -b main
    "$real_git" config user.name "Release Test"
    "$real_git" config user.email "release-test@example.invalid"
    "$real_git" add CHANGELOG.md scripts/release.sh
    "$real_git" commit -q -m initial
    "$real_git" init -q --bare "$root/origin.git"
    "$real_git" remote add origin "$root/origin.git"
    "$real_git" push -q -u origin main

    before=$("$real_git" rev-parse HEAD)
    if PATH="$root/bin:$PATH" REAL_GIT="$real_git" sh scripts/release.sh 9.8.7 >"$root/stdout" 2>"$root/stderr"; then
        printf 'release unexpectedly succeeded when signing preflight failed\n' >&2
        exit 1
    fi
    after=$("$real_git" rev-parse HEAD)
    if [ "$before" != "$after" ]; then
        printf 'signing preflight failure created a release commit\n' >&2
        exit 1
    fi
    if ! "$real_git" diff --quiet || ! "$real_git" diff --cached --quiet; then
        printf 'signing preflight failure left tracked changes\n' >&2
        exit 1
    fi
    if "$real_git" rev-parse -q --verify refs/tags/v9.8.7 >/dev/null; then
        printf 'signing preflight failure created the release tag\n' >&2
        exit 1
    fi
    if [ "$("$real_git" ls-remote --heads origin main | cut -f1)" != "$before" ]; then
        printf 'signing preflight failure changed remote main\n' >&2
        exit 1
    fi
    if [ -n "$("$real_git" ls-remote --tags origin refs/tags/v9.8.7)" ]; then
        printf 'signing preflight failure pushed the release tag\n' >&2
        exit 1
    fi
    if ! grep -q 'signed-tag preflight failed; no release commit, tag, or push was performed' "$root/stderr"; then
        printf 'signing preflight failure did not report safe rollback\n' >&2
        exit 1
    fi
)
