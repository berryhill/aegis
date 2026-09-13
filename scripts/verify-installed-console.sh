#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
# Reserve space for the per-port ".installed-console-PORT.sock" suffix used
# by the integrated proof. The host Unix-domain socket path limit is 108
# bytes; a default of the source repository fails to bind on a long checkout.
proof_socket_budget=80
# Default short-path socket roots, in preference order. The first entry is the
# controller-side scratch directory; CI runners (which lack /home/silas) fall
# back to GitHub Actions' $RUNNER_TEMP, then to a writable system scratch.
default_short_proof_socket_roots="/home/silas/.hermes/.scratch/aegis-installed-sockets ${RUNNER_TEMP:-} ${TMPDIR:-} /tmp"
socket_dir=${AEGIS_PROOF_SOCKET_DIR:-}
socket_dir_created=false
if [ -z "$socket_dir" ]; then
  if [ "${#repo}" -lt "$proof_socket_budget" ] && [ -d "$repo" ] && [ ! -L "$repo" ]; then
    socket_dir=$repo
  else
    short_socket_root=
    for candidate_root in $default_short_proof_socket_roots; do
      [ -n "$candidate_root" ] || continue
      # Skip a candidate that is not an existing, writable directory; otherwise
      # the caller has already chosen an explicit root we must not override.
      if [ -d "$candidate_root" ] && [ ! -L "$candidate_root" ] && [ -w "$candidate_root" ]; then
        short_socket_root=$candidate_root
        break
      fi
    done
    if [ -z "$short_socket_root" ]; then
      printf '%s\n' "installed console proof short socket root is not writable; set AEGIS_PROOF_SOCKET_DIR to a short canonical directory (candidates inspected: $default_short_proof_socket_roots)" >&2
      exit 1
    fi
    socket_dir=$(mktemp -d "$short_socket_root/aegis.XXXXXXXX")
    socket_dir_created=true
  fi
fi
case "$socket_dir" in /*) ;; *) printf '%s\n' 'installed console proof socket directory must be absolute' >&2; exit 1 ;; esac
[ -d "$socket_dir" ] && [ ! -L "$socket_dir" ] || { printf '%s\n' 'installed console proof socket directory must be one existing real directory' >&2; exit 1; }
[ "$(CDPATH= cd -- "$socket_dir" && pwd -P)" = "$socket_dir" ] || { printf '%s\n' 'installed console proof socket directory must be canonical' >&2; exit 1; }
candidate=${1:-}
workspace=${2:-}
[ "$#" -eq 2 ] || { printf '%s\n' 'usage: verify-installed-console.sh CANDIDATE_AEGIS DURABLE_WORKSPACE' >&2; exit 2; }
case "$workspace" in "$repo"/*) ;; *) printf '%s\n' 'installed console proof workspace must be repository-local' >&2; exit 1 ;; esac
[ -x "$candidate" ] && [ ! -L "$candidate" ] || { printf '%s\n' 'installed console candidate must be one executable' >&2; exit 1; }
[ ! -e "$workspace" ] || { printf '%s\n' 'installed console proof workspace must not exist' >&2; exit 1; }
mkdir -m 0700 "$workspace"
cleanup() {
  status=$?
  if [ "$status" -eq 0 ] || [ "${AEGIS_KEEP_FAILED_PROOF:-0}" != 1 ]; then
    rm -rf "$workspace"
  else
    printf 'installed console proof workspace retained after failure: %s\n' "$workspace" >&2
  fi
  if [ "${socket_dir_created:-false}" = true ] && [ -d "$socket_dir" ]; then
    rm -rf "$socket_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -m 0700 "$workspace/state" "$workspace/state/persistence"
go run "$repo/scripts/demo-authority-init" "$workspace/state/persistence/authority-v1" >/dev/null
(cd "$repo" && go test ./internal/api -run '^TestServeSingletonDeniesBeforeActiveSocketMutation$' -count=1)

AEGIS_INSTALLED_CONSOLE_BROWSER="$repo/scripts/console_browser_test.py" \
AEGIS_INSTALLED_CONSOLE_REPO="$repo" \
AEGIS_PROOF_SOCKET_DIR="$socket_dir" \
python3 "$repo/scripts/verify-installed-fleet-vertical.py" "$candidate" "$workspace"

archive_extracted=false
[ "${AEGIS_CANDIDATE_ARCHIVE_EXTRACTED:-0}" = 1 ] && archive_extracted=true
printf 'installed console verified: candidate_single_binary=true archive_extracted=%s charter_validate_import=true browser_authenticated_agent_registration=true loop_publish_activate=true graph_publish=true typed_submission=true queue_process=true evidence_receipt_disposition=true server_restart=true durable_reconstruction=true later_agent_loop_revisions=true durable_rejection=true credential_journey_separate=true filters=true pagination=true responsive=true csp=true real_chrome=true\n' "$archive_extracted"
