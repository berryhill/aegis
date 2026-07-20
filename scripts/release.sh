#!/bin/sh
set -eu

version=${1:-}
tag=v$version
dry_run=${RELEASE_DRY_RUN:-0}
verify_root=
changelog_temp=
changelog_original=
prepared=false
committed=false

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

cleanup() {
    if [ -n "$verify_root" ]; then
        rm -rf "$verify_root"
    fi
    if [ "$prepared" = true ] && [ "$committed" = false ]; then
        cp "$changelog_original" CHANGELOG.md
    fi
    if [ -n "$changelog_temp" ]; then
        rm -f "$changelog_temp"
    fi
    if [ -n "$changelog_original" ]; then
        rm -f "$changelog_original"
    fi
}
trap cleanup EXIT HUP INT TERM

case "$version" in
    0|*[!0-9.]*) printf 'VERSION must be MAJOR.MINOR.PATCH\n' >&2; exit 2 ;;
esac
if ! printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
    printf 'VERSION must be MAJOR.MINOR.PATCH\n' >&2
    exit 2
fi
if [ "$dry_run" != 0 ] && [ "$dry_run" != 1 ]; then
    printf 'RELEASE_DRY_RUN must be 0 or 1\n' >&2
    exit 2
fi
if [ "$(git branch --show-current)" != main ]; then
    fail 'release must run from main'
fi
if ! git diff --cached --quiet; then
    fail 'release refuses pre-staged changes; unstage them and retry'
fi
head=$(git rev-parse HEAD)
remote_main=$(git ls-remote --heads origin refs/heads/main | cut -f1)
[ -n "$remote_main" ] || fail 'origin/main is missing or unreadable; release state is ambiguous'

remote_tag_object=
remote_tag_commit=
remote_tag_lines=$(git ls-remote --tags origin "refs/tags/$tag" "refs/tags/$tag^{}")
if [ -n "$remote_tag_lines" ]; then
    tab=$(printf '	')
    old_ifs=$IFS
    IFS='
'
    for line in $remote_tag_lines; do
        oid=${line%%"$tab"*}
        ref=${line#*"$tab"}
        case "$ref" in
            "refs/tags/$tag") remote_tag_object=$oid ;;
            "refs/tags/$tag^{}") remote_tag_commit=$oid ;;
        esac
    done
    IFS=$old_ifs
fi

local_tag=false
if git show-ref --verify --quiet "refs/tags/$tag"; then
    local_tag=true
fi

prepare_changelog() {
    source_file=$1
    destination_file=$2
    release_date=$3
    VERSION=$version RELEASE_DATE=$release_date SOURCE_FILE=$source_file DESTINATION_FILE=$destination_file python3 -c '
import os
from pathlib import Path

source = Path(os.environ["SOURCE_FILE"])
destination = Path(os.environ["DESTINATION_FILE"])
text = source.read_text()
version = os.environ["VERSION"]
date = os.environ["RELEASE_DATE"]
heading = f"## [{version}] - {date}"
marker = "## Unreleased"
if text.count(marker) != 1:
    raise SystemExit("CHANGELOG.md must contain exactly one Unreleased heading")
before, after = text.split(marker, 1)
next_release = after.find("\n## ")
pending = after if next_release < 0 else after[:next_release]
existing = [line for line in text.splitlines() if line.startswith(f"## [{version}] - ")]
if existing:
    raise SystemExit(f"CHANGELOG.md already contains a {version} release heading")
if not pending.strip():
    raise SystemExit("CHANGELOG.md has no unreleased entries")
destination.write_text(before + marker + "\n\n" + heading + after)
'
}

verify_tag_annotation() {
    annotation=$(git cat-file tag "refs/tags/$tag" | python3 -c '
import sys
data = sys.stdin.read()
try:
    message = data.split("\n\n", 1)[1]
except IndexError:
    raise SystemExit(1)
markers = ("-----BEGIN PGP SIGNATURE-----", "-----BEGIN SSH SIGNATURE-----")
positions = [message.rfind(marker) for marker in markers]
position = max(positions)
if position < 0:
    raise SystemExit(1)
print(message[:position].rstrip("\n"))
') || fail "existing local tag $tag has an unreadable annotation; it was not changed"
    [ "$annotation" = "Aegis $tag" ] || fail "existing local tag $tag annotation is invalid: expected exactly 'Aegis $tag'; it was not changed"
}

verify_release_commit() {
    release_commit=$1
    parents=$(git show -s --format=%P "$release_commit")
    case "$parents" in
        ''|*' '*) fail "existing local tag $tag does not target a single-parent release commit; it was not changed" ;;
    esac
    release_parent=$parents
    [ "$(git show -s --format=%s "$release_commit")" = "Prepare $tag release" ] ||
        fail "existing local tag $tag targets a commit with the wrong release subject; it was not changed"
    changed_paths=$(git diff-tree --no-commit-id --name-only -r "$release_commit")
    [ "$changed_paths" = CHANGELOG.md ] ||
        fail "existing release commit for $tag contains unexpected files; expected only CHANGELOG.md; it was not changed"
    [ -z "$(git diff --summary "$release_parent" "$release_commit")" ] ||
        fail "existing release commit for $tag contains an unexpected file-mode change; it was not changed"
    release_date=$(git show -s --format=%cs "$release_commit")
    expected_changelog="$verify_root/expected-CHANGELOG.md"
    git show "$release_parent:CHANGELOG.md" >"$verify_root/parent-CHANGELOG.md"
    git show "$release_commit:CHANGELOG.md" >"$verify_root/tagged-CHANGELOG.md"
    VERSION=$version RELEASE_DATE=$release_date PARENT_FILE="$verify_root/parent-CHANGELOG.md" TAGGED_FILE="$verify_root/tagged-CHANGELOG.md" SOURCE_FILE="$verify_root/source-CHANGELOG.md" python3 -c '
import os
from pathlib import Path

parent = Path(os.environ["PARENT_FILE"]).read_text()
tagged = Path(os.environ["TAGGED_FILE"]).read_text()
version = os.environ["VERSION"]
release_date = os.environ["RELEASE_DATE"]
heading = f"## [{version}] - {release_date}"
needle = "\n\n" + heading
if tagged.count(needle) != 1:
    raise SystemExit("release heading is not unique")
source = tagged.replace(needle, "", 1)

def sections(text):
    marker = "## Unreleased"
    before, after = text.split(marker, 1)
    boundary = after.find("\n## ")
    if boundary < 0:
        return before, after, ""
    return before, after[:boundary], after[boundary:]

parent_before, parent_pending, parent_tail = sections(parent)
source_before, source_pending, source_tail = sections(source)
if parent_before != source_before or parent_tail != source_tail:
    raise SystemExit("changes outside Unreleased")
remaining = iter(source_pending.splitlines())
if not all(any(candidate == line for candidate in remaining) for line in parent_pending.splitlines()):
    raise SystemExit("parent Unreleased entries were changed or removed")
Path(os.environ["SOURCE_FILE"]).write_text(source)
' || fail "existing release commit for $tag does not preserve its parent changelog outside additive Unreleased entries; it was not changed"
    if ! prepare_changelog "$verify_root/source-CHANGELOG.md" "$expected_changelog" "$release_date"; then
        fail "existing release commit for $tag cannot reproduce its changelog preparation; it was not changed"
    fi
    cmp -s "$expected_changelog" "$verify_root/tagged-CHANGELOG.md" ||
        fail "existing release commit for $tag has an incorrect changelog; it was not changed"
    heading_count=$(git show "$release_commit:CHANGELOG.md" | grep -Ec "^## \[$version\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$" || true)
    [ "$heading_count" = 1 ] ||
        fail "existing release commit for $tag must contain exactly one valid $version changelog heading; it was not changed"
}

run_verification() {
    commit=$1
    mode=$2
    if [ -n "$verify_root" ]; then
        rm -rf "$verify_root"
    fi
    verify_root=$(mktemp -d "${TMPDIR:-/tmp}/aegis-release-verify-XXXXXXXX")
    git clone --quiet --no-hardlinks "$(pwd -P)" "$verify_root/repo"
    if [ "$mode" = fresh ]; then
        cp CHANGELOG.md "$verify_root/repo/CHANGELOG.md"
    else
        git -C "$verify_root/repo" checkout --quiet --detach "$commit"
    fi
    (
        cd "$verify_root/repo"
        sh -n scripts/release.sh
        if [ "$mode" = fresh ] && [ "$dry_run" != 1 ]; then
            preflight_tag="aegis-signing-preflight-$$"
            if ! git tag -s "$preflight_tag" -m 'Aegis release signing preflight'; then
                printf 'signed-tag preflight failed; no release commit, tag, or push was performed\n' >&2
                exit 1
            fi
            git tag -d "$preflight_tag" >/dev/null
        fi
        RELEASE_DRY_RUN=0 make verify
        make release-review VERSION="$version"
        git diff --check
    )
}

report_github_state() {
    [ "${RELEASE_SKIP_GITHUB_STATUS:-0}" != 1 ] || return 0
    command -v curl >/dev/null 2>&1 || {
        printf 'remote tag matches; GitHub release/workflow state unavailable (curl not installed).\n'
        return 0
    }
    origin_url=$(git remote get-url origin)
    case "$origin_url" in
        git@github.com:*) repository=${origin_url#git@github.com:}; repository=${repository%.git} ;;
        https://github.com/*) repository=${origin_url#https://github.com/}; repository=${repository%.git} ;;
        *) printf 'remote tag matches; GitHub release/workflow state unavailable for non-GitHub origin.\n'; return 0 ;;
    esac
    status_file="$verify_root/github-status.json"
    release_code=$(curl --max-time 10 -sS -o "$status_file" -w '%{http_code}' -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$repository/releases/tags/$tag" || true)
    case "$release_code" in
        200) printf 'GitHub release is published for %s.\n' "$tag" ;;
        404) printf 'GitHub release not yet published for %s.\n' "$tag" ;;
        *) printf 'GitHub release state unavailable for %s (HTTP %s).\n' "$tag" "${release_code:-unavailable}" ;;
    esac
    runs_code=$(curl --max-time 10 -sS -o "$status_file" -w '%{http_code}' -H 'Accept: application/vnd.github+json' "https://api.github.com/repos/$repository/actions/workflows/release.yml/runs?head_sha=$local_tag_commit&per_page=10" || true)
    if [ "$runs_code" = 200 ]; then
        workflow_state=$(python3 -c '
import json, sys
runs = json.load(open(sys.argv[1])).get("workflow_runs", [])
if not runs:
    print("not found")
else:
    run = runs[0]
    status = run.get("status") or "unknown"
    conclusion = run.get("conclusion")
    print(conclusion if status == "completed" and conclusion else status)
' "$status_file" 2>/dev/null || printf 'unavailable')
        case "$workflow_state" in
            success) printf 'release workflow complete for %s.\n' "$tag" ;;
            failure|cancelled|timed_out|action_required|startup_failure|stale) printf 'release workflow failed for %s (%s); inspect it manually; it was not rerun.\n' "$tag" "$workflow_state" ;;
            queued|in_progress|pending|requested|waiting) printf 'release workflow pending for %s (%s).\n' "$tag" "$workflow_state" ;;
            'not found') printf 'release workflow not found yet for %s.\n' "$tag" ;;
            *) printf 'release workflow state unavailable for %s.\n' "$tag" ;;
        esac
    else
        printf 'release workflow state unavailable for %s (HTTP %s).\n' "$tag" "${runs_code:-unavailable}"
    fi
}

if [ "$local_tag" = false ]; then
    if [ -n "$remote_tag_object" ] || [ -n "$remote_tag_commit" ]; then
        fail "$tag exists on origin but not locally; fetch and verify the immutable tag before retrying"
    fi
    [ "$head" = "$remote_main" ] || fail 'HEAD must exactly match origin/main before fresh release preparation'
    existing_commit=$(git log --all --format='%H %s' --fixed-strings --grep="Prepare $tag release" | grep " Prepare $tag release$" || true)
    [ -z "$existing_commit" ] || fail "a release commit for $tag already exists without the local tag; inspect $existing_commit and recover manually"
    printf 'fresh release preparation for %s found.\n' "$tag"
    release_date=$(date +%F)
    # Preserve an existing unstaged changelog and restore it exactly on any
    # pre-commit failure or dry run.
    changelog_original=$(mktemp "${TMPDIR:-/tmp}/aegis-changelog-original-XXXXXXXX")
    cp CHANGELOG.md "$changelog_original"
    # Write through a temporary file so failed validation cannot truncate CHANGELOG.md.
    changelog_temp=$(mktemp "${TMPDIR:-/tmp}/aegis-changelog-XXXXXXXX")
    if ! prepare_changelog CHANGELOG.md "$changelog_temp" "$release_date"; then
        rm -f "$changelog_temp"
        exit 1
    fi
    prepared=true
    mv "$changelog_temp" CHANGELOG.md
    changelog_temp=
    run_verification "$head" fresh
    git diff -- CHANGELOG.md
    if [ "$dry_run" = 1 ]; then
        printf '%s dry run: would create one release commit, create one signed tag, and atomically push main plus the tag; no refs or remote state changed.\n' "$tag"
        exit 0
    fi
    git commit --only CHANGELOG.md -m "Prepare $tag release"
    committed=true
    release_commit=$(git rev-parse HEAD)
    git tag -s "$tag" -m "Aegis $tag"
    if ! git push --atomic origin refs/heads/main:refs/heads/main "refs/tags/$tag:refs/tags/$tag"; then
        fail "atomic publication failed; local signed tag $tag and release commit $release_commit were preserved for verified retry"
    fi
    printf '%s pushed atomically; GitHub Actions will publish the release after verification.\n' "$tag"
    exit 0
fi

verify_root=$(mktemp -d "${TMPDIR:-/tmp}/aegis-release-verify-XXXXXXXX")
[ "$(git cat-file -t "refs/tags/$tag")" = tag ] ||
    fail "existing local tag $tag is lightweight; recovery requires an annotated signed tag; it was not changed"
local_tag_object=$(git rev-parse "refs/tags/$tag")
local_tag_commit=$(git rev-parse "refs/tags/$tag^{}")
if ! git verify-tag --verbose "$tag"; then
    fail "existing local tag $tag failed signature or signer-policy verification; it was not changed"
fi
verify_tag_annotation
[ "$head" = "$local_tag_commit" ] ||
    fail "existing local tag $tag does not target local main exactly; local main may be ahead or divergent; no refs were changed"
verify_release_commit "$local_tag_commit"

if [ -n "$remote_tag_object" ] || [ -n "$remote_tag_commit" ]; then
    [ -n "$remote_tag_object" ] && [ -n "$remote_tag_commit" ] ||
        fail "origin $tag is not an annotated tag with a readable peeled commit; remote state is ambiguous and was not changed"
    if [ "$remote_tag_object" != "$local_tag_object" ] || [ "$remote_tag_commit" != "$local_tag_commit" ]; then
        fail "origin $tag conflicts with the local immutable tag (local object $local_tag_object commit $local_tag_commit; remote object $remote_tag_object commit $remote_tag_commit); no refs were changed"
    fi
    [ "$remote_main" = "$local_tag_commit" ] ||
        fail "origin $tag matches locally but origin/main is not the release commit; remote state is ambiguous and was not changed"
    printf 'remote tag already exists and matches exactly (object %s, commit %s); nothing will be republished.\n' "$local_tag_object" "$local_tag_commit"
    run_verification "$local_tag_commit" recovery
    report_github_state
    if [ "$dry_run" = 1 ]; then
        printf '%s dry run: completed release state; would perform no publication action.\n' "$tag"
    fi
    exit 0
fi

case "$remote_main" in
    "$release_parent") recovery_action=atomic ;;
    "$local_tag_commit") recovery_action=tag-only ;;
    *) fail "origin/main is neither the verified release parent $release_parent nor release commit $local_tag_commit; recovery is unsafe and no refs were changed" ;;
esac

printf 'resumable local release found for %s (signed tag object %s, commit %s).\n' "$tag" "$local_tag_object" "$local_tag_commit"
run_verification "$local_tag_commit" recovery
if [ "$recovery_action" = atomic ]; then
    printf 'local release commit awaits publication; origin/main is its verified parent.\n'
    if [ "$dry_run" = 1 ]; then
        printf '%s dry run: would atomically push the existing local main and existing signed tag; no refs or remote state changed.\n' "$tag"
        exit 0
    fi
    if ! git verify-tag --verbose "$tag"; then
        fail "existing local tag $tag failed signature verification immediately before publication; no refs were changed"
    fi
    if ! git push --atomic origin refs/heads/main:refs/heads/main "refs/tags/$tag:refs/tags/$tag"; then
        fail "atomic publication failed; local signed tag $tag and release commit $local_tag_commit remain recoverable"
    fi
else
    printf 'origin/main already contains the release commit; the existing signed tag awaits publication.\n'
    if [ "$dry_run" = 1 ]; then
        printf '%s dry run: would push only the existing verified signed tag; no refs or remote state changed.\n' "$tag"
        exit 0
    fi
    if ! git verify-tag --verbose "$tag"; then
        fail "existing local tag $tag failed signature verification immediately before publication; no refs were changed"
    fi
    # Include the already-equal main ref in an atomic request so a concurrent
    # main change rejects the tag publication without moving either ref.
    if ! git push --atomic origin refs/heads/main:refs/heads/main "refs/tags/$tag:refs/tags/$tag"; then
        fail "tag-only publication failed; existing signed tag $tag remains recoverable and was not moved"
    fi
fi
printf '%s recovery publication succeeded; GitHub Actions will publish the release after verification.\n' "$tag"