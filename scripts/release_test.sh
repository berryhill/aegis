#!/bin/sh
set -eu

root=$(mktemp -d "${TMPDIR:-/tmp}/aegis-release-test-XXXXXXXX")
cleanup() { rm -rf "$root"; }
trap cleanup EXIT HUP INT TERM

real_git=$(command -v git)
script_source=$(pwd -P)/scripts/release.sh
mkdir -p "$root/bin"

cat >"$root/bin/git" <<'EOF'
#!/bin/sh
if [ "${1:-}" = verify-tag ]; then
    if [ "${TEST_BAD_SIGNATURE:-0}" = 1 ]; then
        printf 'fixture signature verification failure\n' >&2
        exit 81
    fi
    exit 0
fi
if [ "${1:-}" = tag ] && [ "${2:-}" = -s ]; then
    if [ "${TEST_SIGN_FAIL:-0}" = 1 ]; then
        printf 'fixture signing failure\n' >&2
        exit 79
    fi
    shift 2
    exec "$REAL_GIT" tag -a "$@" -m '-----BEGIN PGP SIGNATURE-----
fixture-signature
-----END PGP SIGNATURE-----'
fi
if [ "${1:-}" = push ]; then
    printf '%s\n' "$*" >>"$TEST_PUSH_LOG"
    case " $* " in
        *' --force '*|*' --force-with-lease '*)
            printf 'fixture observed forbidden force push\n' >&2
            exit 82
            ;;
    esac
    if [ "${TEST_PUSH_FAIL_ONCE:-0}" = 1 ] && [ -f "$TEST_PUSH_FAIL_FILE" ]; then
        rm -f "$TEST_PUSH_FAIL_FILE"
        printf 'fixture push failure\n' >&2
        exit 83
    fi
fi
exec "$REAL_GIT" "$@"
EOF
cat >"$root/bin/make" <<'EOF'
#!/bin/sh
if [ "${TEST_VERIFY_FAIL:-0}" = 1 ]; then
    printf 'fixture verification failure\n' >&2
    exit 84
fi
exit 0
EOF
chmod 0700 "$root/bin/git" "$root/bin/make"

fail_test() {
    printf 'release test failed: %s\n' "$*" >&2
    exit 1
}

setup_repo() {
    name=$1
    repo="$root/$name/repo"
    origin="$root/$name/origin.git"
    mkdir -p "$repo/scripts"
    cp "$script_source" "$repo/scripts/release.sh"
    cat >"$repo/CHANGELOG.md" <<'EOF'
# Changelog

## Unreleased

### Fixed

- Pending release change.
EOF
    "$real_git" init -q -b main "$repo"
    "$real_git" -C "$repo" config user.name 'Release Test'
    "$real_git" -C "$repo" config user.email 'release-test@example.invalid'
    "$real_git" -C "$repo" add CHANGELOG.md scripts/release.sh
    "$real_git" -C "$repo" commit -q -m initial
    "$real_git" init -q --bare "$origin"
    "$real_git" -C "$repo" remote add origin "$origin"
    "$real_git" -C "$repo" push -q -u origin main
    push_log="$root/$name/push.log"
    : >"$push_log"
}

create_release_commit() {
    release_date=$(date +%F)
    RELEASE_DATE=$release_date python3 -c '
import os
from pathlib import Path
p = Path("CHANGELOG.md")
t = p.read_text()
p.write_text(t.replace("## Unreleased\n", "## Unreleased\n\n## [9.8.7] - " + os.environ["RELEASE_DATE"] + "\n", 1))
'
    "$real_git" add CHANGELOG.md
    "$real_git" commit -q -m 'Prepare v9.8.7 release'
}

create_signed_tag() {
    PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" \
        git tag -s v9.8.7 -m 'Aegis v9.8.7'
}

run_release() {
    PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" \
        RELEASE_SKIP_GITHUB_STATUS=1 sh scripts/release.sh 9.8.7
}

run_dry_release() {
    PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" \
        RELEASE_SKIP_GITHUB_STATUS=1 RELEASE_DRY_RUN=1 sh scripts/release.sh 9.8.7
}

snapshot() {
    {
        "$real_git" rev-parse HEAD
        "$real_git" status --porcelain=v1
        "$real_git" show-ref || true
        "$real_git" ls-remote --heads --tags origin
        sha256sum CHANGELOG.md scripts/release.sh
    } >"$1"
}

expect_failure() {
    pattern=$1
    shift
    if "$@" >"$root/stdout" 2>"$root/stderr"; then
        fail_test "command unexpectedly succeeded; expected $pattern"
    fi
    grep -q "$pattern" "$root/stderr" || {
        printf '%s\n' '--- stderr ---' >&2
        sed -n '1,160p' "$root/stderr" >&2
        fail_test "failure did not contain: $pattern"
    }
}

# Fresh classification is hermetic and dry-run changes neither local nor remote state.
setup_repo fresh
(
    cd "$repo"
    snapshot "$root/fresh-before"
    run_dry_release >"$root/fresh-output"
    snapshot "$root/fresh-after"
    cmp "$root/fresh-before" "$root/fresh-after" || fail_test 'fresh dry-run changed repository state'
    grep -q 'fresh release preparation' "$root/fresh-output" || fail_test 'fresh state was not classified'
    grep -q 'would create one release commit' "$root/fresh-output" || fail_test 'fresh dry-run omitted exact action'
)

# A valid existing signed tag is recoverable with origin/main at either allowed commit.
setup_repo recovery-parent
(
    cd "$repo"
    parent=$("$real_git" rev-parse HEAD)
    create_release_commit
    create_signed_tag
    release=$("$real_git" rev-parse HEAD)
    snapshot "$root/recovery-parent-before"
    run_dry_release >"$root/recovery-parent-output"
    snapshot "$root/recovery-parent-after"
    cmp "$root/recovery-parent-before" "$root/recovery-parent-after" || fail_test 'recovery dry-run changed state'
    grep -q 'resumable local release found' "$root/recovery-parent-output" || fail_test 'recovery was not classified'
    grep -q 'atomically push the existing local main and existing signed tag' "$root/recovery-parent-output" || fail_test 'parent recovery action was wrong'
    [ "$("$real_git" ls-remote --heads origin main | cut -f1)" = "$parent" ] || fail_test 'recovery dry-run moved remote main'
    [ -z "$("$real_git" ls-remote --tags origin refs/tags/v9.8.7)" ] || fail_test 'recovery dry-run pushed tag'
    [ "$("$real_git" rev-parse v9.8.7^{})" = "$release" ] || fail_test 'recovery moved local tag'
)

setup_repo recovery-tag-only
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    "$real_git" push -q origin main
    run_dry_release >"$root/recovery-tag-only-output"
    grep -q 'origin/main already contains the release commit' "$root/recovery-tag-only-output" || fail_test 'tag-only state was not distinguished'
    grep -q 'would push only the existing verified signed tag' "$root/recovery-tag-only-output" || fail_test 'tag-only action was wrong'
)

setup_repo recovery-tag-only-publish
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    release=$("$real_git" rev-parse HEAD)
    "$real_git" push -q origin main
    run_release >"$root/recovery-tag-only-publish-output"
    [ "$("$real_git" ls-remote --heads origin main | cut -f1)" = "$release" ] || fail_test 'tag-only publication changed remote main'
    [ "$("$real_git" ls-remote --tags origin 'refs/tags/v9.8.7^{}' | cut -f1)" = "$release" ] || fail_test 'tag-only publication used the wrong target'
)

# A matching remote immutable tag is completed; a different object fails closed.
setup_repo completed
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    "$real_git" push -q --atomic origin main refs/tags/v9.8.7
    snapshot "$root/completed-before"
    run_dry_release >"$root/completed-output"
    snapshot "$root/completed-after"
    cmp "$root/completed-before" "$root/completed-after" || fail_test 'completed dry-run changed state'
    grep -q 'remote tag already exists and matches exactly' "$root/completed-output" || fail_test 'completed state was not classified'
    grep -q 'would perform no publication action' "$root/completed-output" || fail_test 'completed action was wrong'
)

setup_repo conflict
(
    other="$root/conflict/other"
    "$real_git" clone -q --branch main "$origin" "$other"
    "$real_git" -C "$other" config user.name 'Other Release Test'
    "$real_git" -C "$other" config user.email 'other@example.invalid'
    "$real_git" -C "$other" tag -a v9.8.7 -m 'Aegis v9.8.7'
    "$real_git" -C "$other" push -q origin refs/tags/v9.8.7
    cd "$repo"
    create_release_commit
    create_signed_tag
    expect_failure 'origin v9.8.7 conflicts with the local immutable tag' run_dry_release
)

# Invalid or ambiguous local tags are rejected without repair.
setup_repo lightweight
(
    cd "$repo"
    create_release_commit
    "$real_git" tag v9.8.7
    expect_failure 'is lightweight' run_dry_release
)

setup_repo bad-signature
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    expect_failure 'failed signature or signer-policy verification' env TEST_BAD_SIGNATURE=1 \
        PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" RELEASE_SKIP_GITHUB_STATUS=1 RELEASE_DRY_RUN=1 sh scripts/release.sh 9.8.7
)

setup_repo wrong-annotation
(
    cd "$repo"
    create_release_commit
    PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" \
        git tag -s v9.8.7 -m 'wrong annotation'
    expect_failure 'annotation is invalid' run_dry_release
)

setup_repo wrong-target
(
    cd "$repo"
    parent=$("$real_git" rev-parse HEAD)
    create_release_commit
    PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" \
        git tag -s v9.8.7 -m 'Aegis v9.8.7' "$parent"
    expect_failure 'does not target local main exactly' run_dry_release
)

setup_repo main-ahead
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    printf 'later\n' >later.txt
    "$real_git" add later.txt
    "$real_git" commit -q -m later
    expect_failure 'does not target local main exactly' run_dry_release
)

setup_repo main-diverged
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    parent=$("$real_git" rev-parse 'HEAD^')
    "$real_git" checkout -q -B main "$parent"
    printf 'diverged\n' >diverged.txt
    "$real_git" add diverged.txt
    "$real_git" commit -q -m diverged
    expect_failure 'does not target local main exactly' run_dry_release
)

setup_repo bad-changelog
(
    cd "$repo"
    printf '\n## [9.8.7] - invalid\n' >>CHANGELOG.md
    "$real_git" add CHANGELOG.md
    "$real_git" commit -q -m 'Prepare v9.8.7 release'
    create_signed_tag
    expect_failure 'has an incorrect changelog' run_dry_release
)

setup_repo unexpected-file
(
    cd "$repo"
    create_release_commit
    printf 'unexpected\n' >unexpected.txt
    "$real_git" add unexpected.txt
    "$real_git" commit -q --amend --no-edit
    create_signed_tag
    expect_failure 'contains unexpected files' run_dry_release
)

setup_repo staged
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    printf 'staged\n' >staged.txt
    "$real_git" add staged.txt
    expect_failure 'refuses pre-staged changes' run_dry_release
)

setup_repo divergent-origin
(
    other="$root/divergent-origin/other"
    "$real_git" clone -q --branch main "$origin" "$other"
    "$real_git" -C "$other" config user.name 'Other Release Test'
    "$real_git" -C "$other" config user.email 'other@example.invalid'
    printf 'remote divergence\n' >"$other/remote.txt"
    "$real_git" -C "$other" add remote.txt
    "$real_git" -C "$other" commit -q -m 'remote divergence'
    "$real_git" -C "$other" push -q origin main
    cd "$repo"
    create_release_commit
    create_signed_tag
    expect_failure 'origin/main is neither the verified release parent' run_dry_release
)

# Signing and verification failures occur before publication and preserve refs.
setup_repo signing-preflight
(
    cd "$repo"
    before=$("$real_git" rev-parse HEAD)
    expect_failure 'signed-tag preflight failed; no release commit, tag, or push was performed' env TEST_SIGN_FAIL=1 \
        PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" RELEASE_SKIP_GITHUB_STATUS=1 sh scripts/release.sh 9.8.7
    [ "$("$real_git" rev-parse HEAD)" = "$before" ] || fail_test 'signing preflight created a commit'
    "$real_git" diff --quiet && "$real_git" diff --cached --quiet || fail_test 'signing preflight left changes'
    ! "$real_git" show-ref --verify --quiet refs/tags/v9.8.7 || fail_test 'signing preflight created a tag'
)

setup_repo recovery-verification
(
    cd "$repo"
    create_release_commit
    create_signed_tag
    snapshot "$root/recovery-verification-before"
    expect_failure 'fixture verification failure' env TEST_VERIFY_FAIL=1 \
        PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" RELEASE_SKIP_GITHUB_STATUS=1 RELEASE_DRY_RUN=1 sh scripts/release.sh 9.8.7
    snapshot "$root/recovery-verification-after"
    cmp "$root/recovery-verification-before" "$root/recovery-verification-after" || fail_test 'verification failure changed recovery state'
)

# A failed atomic push leaves a signed commit/tag pair; retry publishes those exact refs.
setup_repo retry
(
    cd "$repo"
    touch "$root/retry/fail-once"
    expect_failure 'atomic publication failed; local signed tag v9.8.7 and release commit' env \
        TEST_PUSH_FAIL_ONCE=1 TEST_PUSH_FAIL_FILE="$root/retry/fail-once" \
        PATH="$root/bin:$PATH" REAL_GIT="$real_git" TEST_PUSH_LOG="$push_log" RELEASE_SKIP_GITHUB_STATUS=1 sh scripts/release.sh 9.8.7
    local_object=$("$real_git" rev-parse refs/tags/v9.8.7)
    local_commit=$("$real_git" rev-parse 'refs/tags/v9.8.7^{}')
    [ -z "$("$real_git" ls-remote --tags origin refs/tags/v9.8.7)" ] || fail_test 'failed atomic push published tag'
    run_release >"$root/retry-output"
    [ "$("$real_git" ls-remote --tags origin refs/tags/v9.8.7 | cut -f1)" = "$local_object" ] || fail_test 'retry did not publish exact tag object'
    [ "$("$real_git" ls-remote --tags origin 'refs/tags/v9.8.7^{}' | cut -f1)" = "$local_commit" ] || fail_test 'retry changed tag target'
    [ "$("$real_git" ls-remote --heads origin main | cut -f1)" = "$local_commit" ] || fail_test 'retry did not publish release commit'
    if grep -E -- '--force($| )|--force-with-lease' "$push_log" >/dev/null; then
        fail_test 'release attempted a force push'
    fi
)

printf 'release recovery tests passed\n'