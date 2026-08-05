#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${1:-0.0.0}
requested_dist=${2:-}

case "$version" in
  0.0.0) ;;
  0.*|[1-9]*) ;;
  *) printf 'version must be exact stable SemVer: %s\n' "$version" >&2; exit 2 ;;
esac
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' || {
  printf 'version must be exact stable SemVer: %s\n' "$version" >&2
  exit 2
}

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
  mkdir -p "$dist"
  if [ -n "$(find "$dist" -mindepth 1 -maxdepth 1 -print -quit)" ]; then
    printf 'release output must be empty: %s\n' "$dist" >&2
    exit 2
  fi
else
  dist=$proof/dist
  mkdir "$dist"
fi

cd "$repo"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  name="aegis_v${version}_${os}_${arch}"
  stage=$proof/$name
  mkdir "$stage"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags="-s -w -X github.com/berryhill/aegis/internal/buildinfo.Version=$version" \
    -o "$stage/aegis" ./cmd/aegis
  tar -C "$stage" -czf "$dist/$name.tar.gz" aegis
  rm -rf "$stage"
done

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
[ ! -e "$home/.argis" ] || {
  printf 'non-interactive installed first run mutated production state\n' >&2
  exit 1
}

printf 'installed MVI verified: version=%s targets=4 checksums=valid first_run=fail_closed_no_mutation\n' "$version"
