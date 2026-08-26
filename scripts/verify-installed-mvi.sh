#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${1:-0.0.0}
requested_dist=${2:-}
expected_revision=${3:-}

case "$version" in
  0.0.0) ;;
  0.*|[1-9]*) ;;
  *) printf 'version must be exact stable SemVer: %s\n' "$version" >&2; exit 2 ;;
esac
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
  printf 'version must be exact stable SemVer: %s\n' "$version" >&2
  exit 2
}

deny_provenance() {
  printf 'release source provenance denied: %s\n' "$*" >&2
  exit 2
}
git -C "$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1 || deny_provenance 'Git worktree metadata is unavailable'
actual_revision=$(git -C "$repo" rev-parse --verify HEAD^{commit} 2>/dev/null) || deny_provenance 'HEAD does not resolve to exactly one commit'
printf '%s\n' "$actual_revision" | grep -Eq '^[0-9a-f]{40}$' || deny_provenance "resolved HEAD is not one exact lowercase 40-hex revision: $actual_revision"
if [ -z "$expected_revision" ]; then
  expected_revision=$actual_revision
else
  printf '%s\n' "$expected_revision" | grep -Eq '^[0-9a-f]{40}$' || deny_provenance "expected revision is not one exact lowercase 40-hex value: $expected_revision"
fi
[ "$actual_revision" = "$expected_revision" ] || deny_provenance "revision mismatch: expected $expected_revision actual $actual_revision"

proof=$(mktemp -d "$repo/.aegis-installed-mvi-XXXXXXXX")
cleanup() { rm -rf "$proof"; }
trap cleanup EXIT HUP INT TERM

if [ -n "$requested_dist" ]; then
  case "$requested_dist" in
    /*) dist=$requested_dist ;;
    *) dist=$repo/$requested_dist ;;
  esac
  if [ -L "$dist" ]; then
    printf 'release output must not be a symlink: %s\n' "$dist" >&2
    exit 2
  fi
  if DIST=$dist python3 -c 'import os,pathlib,sys; p=pathlib.Path(os.path.abspath(os.environ["DIST"])); sys.exit(1 if any(part.is_symlink() for part in (p, *p.parents)) else 0)'; then
    :
  else
    printf 'release output must not traverse symlinks: %s\n' "$dist" >&2
    exit 2
  fi
  mkdir -p "$dist"
  lexical_dist=$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$dist")
  physical_dist=$(CDPATH= cd -- "$dist" && pwd -P)
  if [ "$lexical_dist" != "$physical_dist" ]; then
    printf 'release output must not traverse symlinks: %s\n' "$dist" >&2
    exit 2
  fi
  if [ -n "$(find "$dist" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    printf 'release output must be empty: %s\n' "$dist" >&2
    exit 2
  fi
else
  dist=$proof/dist
  mkdir "$dist"
fi

[ -z "$(git -C "$repo" status --porcelain=v1 --untracked-files=no)" ] || deny_provenance 'tracked source worktree is dirty'

cd "$repo"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="aegis_v${version}_${os}_${arch}"
  stage=$proof/$name
  mkdir "$stage"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags="-s -w -X github.com/berryhill/aegis/internal/buildinfo.Version=$version -X github.com/berryhill/aegis/internal/buildinfo.SourceRevision=$expected_revision" \
    -o "$stage/aegis" ./cmd/aegis
  build_revision=$(go version -m "$stage/aegis" 2>/dev/null | sed -n 's/^[[:space:]]*build[[:space:]]*vcs.revision=//p')
  [ -n "$build_revision" ] || deny_provenance "built $os/$arch binary omitted Go VCS revision metadata"
  [ "$build_revision" = "$expected_revision" ] || deny_provenance "built $os/$arch binary revision mismatch: expected $expected_revision actual $build_revision"
  build_modified=$(go version -m "$stage/aegis" 2>/dev/null | sed -n 's/^[[:space:]]*build[[:space:]]*vcs.modified=//p')
  [ "$build_modified" = false ] || deny_provenance "built $os/$arch binary has missing or dirty VCS state: ${build_modified:-missing}"
  chmod 0755 "$stage/aegis"
  tar -C "$stage" -czf "$dist/$name.tar.gz" aegis
  python3 "$repo/scripts/verify-release-archive.py" "$dist/$name.tar.gz"
  rm -rf "$stage"
done

go run ./internal/skillbundle/cmd validate .
go run ./internal/skillbundle/cmd evaluate .
go run ./internal/skillbundle/cmd build . "$dist" "$version" "$expected_revision"
go run ./internal/skillbundle/cmd verify "$dist/aegis-skills_v${version}.tar.gz" "$expected_revision"

(
  cd "$dist"
  sha256sum *.tar.gz >SHA256SUMS
  sha256sum -c SHA256SUMS
)

native_os=$(go env GOOS)
native_arch=$(go env GOARCH)
native_archive=$dist/aegis_v${version}_${native_os}_${native_arch}.tar.gz
if [ ! -f "$native_archive" ]; then
  printf 'declared release targets do not include native %s/%s\n' "$native_os" "$native_arch" >&2
  exit 1
fi

install=$proof/install
home=$proof/home
mkdir -m 0700 "$install" "$home"
tar -xzf "$native_archive" -C "$install"
[ "$($install/aegis --version)" = "aegis version $version" ] || {
  printf 'installed binary did not report injected version %s\n' "$version" >&2
  exit 1
}
provenance=$($install/aegis version --provenance)
PROVENANCE=$provenance VERSION=$version REVISION=$expected_revision python3 -c 'import json,os,sys; value=json.loads(os.environ["PROVENANCE"]); sys.exit(0 if value == {"version":os.environ["VERSION"],"source_revision":os.environ["REVISION"]} else 1)' || deny_provenance 'installed binary provenance does not exactly match release version and source revision'

set +e
printf 'non-interactive installed proof\n' | HOME=$home "$install/aegis" >"$proof/first-run.out" 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || {
  printf 'uninitialized installed binary exited %s instead of 2\n' "$status" >&2
  exit 1
}
for expected in \
  '"state": "uninitialized"' \
  '"initialized": false' \
  '"reason": "manager_not_initialized"' \
  '"next_command": "aegis init"' \
  '"exit_status": 2'
do
  grep -Fq "$expected" "$proof/first-run.out" || {
    printf 'installed first-run output omitted %s\n' "$expected" >&2
    exit 1
  }
done
[ ! -e "$home/.aegis" ] || {
  printf 'non-interactive installed first run mutated production state\n' >&2
  exit 1
}

vertical=$proof/vertical
mkdir -m 0700 "$vertical" "$vertical/state" "$vertical/state/persistence"
go run ./scripts/demo-authority-init "$vertical/state/persistence/authority-v1"
python3 "$repo/scripts/verify-installed-fleet-vertical.py" "$install/aegis" "$proof/vertical"
AEGIS_CANDIDATE_ARCHIVE_EXTRACTED=1 \
  "$repo/scripts/verify-installed-console.sh" "$install/aegis" "$proof/console"

printf 'installed MVI verified: version=%s source_revision=%s targets=4 skill_bundle=validated_evaluated_reproducible checksums=valid first_run=fail_closed_no_mutation fleet_vertical=registry_loop_graph_queue_evidence_disposition console=authenticated_native_forms retained_asset_direct=true retained_asset_loaded=false real_chrome=true\n' "$version" "$expected_revision"
