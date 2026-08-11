#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
candidate=${1:-}
workspace=${2:-}
[ "$#" -eq 2 ] || { printf '%s\n' 'usage: verify-installed-console.sh EXTRACTED_AEGIS DURABLE_WORKSPACE' >&2; exit 2; }
case "$workspace" in "$repo"/*) ;; *) printf '%s\n' 'installed console proof workspace must be repository-local' >&2; exit 1 ;; esac
[ -x "$candidate" ] && [ ! -L "$candidate" ] || { printf '%s\n' 'installed console candidate must be one executable' >&2; exit 1; }
[ ! -e "$workspace" ] || { printf '%s\n' 'installed console proof workspace must not exist' >&2; exit 1; }
mkdir -m 0700 "$workspace"
server_pid=
socket=
cleanup() {
  if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  if [ -n "$socket" ]; then rm -f "$socket"; fi
  rm -rf "$workspace"
}
trap cleanup EXIT HUP INT TERM

uid=$(id -u)
user=$(id -un)
port=$(python3 - <<'PY'
import socket
with socket.socket() as server:
    server.bind(("127.0.0.1", 0))
    print(server.getsockname()[1])
PY
)
socket=$repo/.c-$port.sock
python3 - "$workspace" "$port" "$uid" "$user" "$socket" <<'PY'
import pathlib, secrets, sys
root, port, uid, user, socket = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
token = secrets.token_urlsafe(48)
(root / "token").write_text(token, encoding="utf-8")
(root / "aegis.yaml").write_text(f'''state_dir: {root}/state
runtime_default: hermes
hermes_executable: {root}/hermes-fixture
principal:
  id: principal-1
  name: Installed Proof Principal
  uid: "{uid}"
  user: {user}
  auth_ttl: 2m
api:
  listen: 127.0.0.1:{port}
  unix_socket: {socket}
  token: "{token}"
  read_timeout: 5s
  write_timeout: 5s
  shutdown_timeout: 2s
  max_body_bytes: 1048576
  console:
    origin: http://127.0.0.1:{port}
    session_ttl: 1m
    bootstrap_ttl: 15s
    max_page_size: 25
audit:
  checkpoint_dir: {root}/checkpoints
manager:
  enabled: false
  runtime: hermes
  security_context: secrets-manager
  cleanup_timeout: 2s
  hermes:
    context_length: 65536
    gateway_start_timeout: 2s
    turn_timeout: 2s
    maximum_response_bytes: 1048576
  inference:
    runtime: ollama
    mode: external-local
    executable: ollama
    keep_alive: 1m
    start_timeout: 2s
    request_timeout: 2s
    maximum_request_bytes: 1048576
    maximum_response_bytes: 1048576
  ingress:
    maximum_message_bytes: 262144
    maximum_message_runes: 262144
    scan_timeout: 250ms
    bounded_decode_depth: 2
  transcript:
    retention: session
credentials:
  references: {{}}
  provider_auth: {{}}
''', encoding="utf-8")
(root / "hermes-fixture").write_text("#!/bin/sh\nprintf 'Hermes Agent v0.18.2\\nInstall directory: /isolated/installed-console-proof\\n'\n", encoding="utf-8")
(root / "curl.conf").write_text('header = "Authorization: Bearer ' + token + '"\n', encoding="utf-8")
for name in ("token", "aegis.yaml", "hermes-fixture", "curl.conf"):
    (root / name).chmod(0o700 if name == "hermes-fixture" else 0o600)
PY
mkdir -m 0700 "$workspace/state"
go run "$repo/scripts/demo-authority-init" "$workspace/state/persistence/authority-v1" >/dev/null
HOME=$workspace "$candidate" --config "$workspace/aegis.yaml" serve >"$workspace/server.log" 2>&1 &
server_pid=$!

ready=false
for _ in $(seq 1 100); do
  if curl --fail --silent --show-error "http://127.0.0.1:$port/console" -o "$workspace/shell.html"; then ready=true; break; fi
  kill -0 "$server_pid" 2>/dev/null || {
    printf '%s\n' 'installed console server exited before readiness' >&2
    while IFS= read -r line; do printf '%s\n' "$line" >&2; done <"$workspace/server.log"
    exit 1
  }
  sleep 0.05
done
[ "$ready" = true ] || {
  printf '%s\n' 'installed console server did not become ready' >&2
  while IFS= read -r line; do printf '%s\n' "$line" >&2; done <"$workspace/server.log"
  exit 1
}
grep -F 'Authenticate this browser' "$workspace/shell.html" >/dev/null
grep -F '/console/assets/datastar-v1.0.2.js' "$workspace/shell.html" >/dev/null
curl --fail --silent --show-error "http://127.0.0.1:$port/console/assets/datastar-v1.0.2.js" -o "$workspace/datastar.js"
[ "$(wc -c <"$workspace/datastar.js")" -gt 1000 ]

curl --fail --silent --show-error --unix-socket "$socket" --config "$workspace/curl.conf" -X POST -H 'Content-Type: application/json' --data '{}' http://localhost/v1/console/bootstrap -o "$workspace/bootstrap-response.json"
python3 - "$workspace" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
response = json.loads((root / "bootstrap-response.json").read_text())
if set(response) != {"bootstrap", "expires"} or not response["bootstrap"]:
    raise SystemExit("installed console bootstrap response is invalid")
(root / "exchange.json").write_text(json.dumps({"bootstrap": response["bootstrap"]}), encoding="utf-8")
(root / "exchange.json").chmod(0o600)
PY
curl --fail --silent --show-error -c "$workspace/cookies" -H "Origin: http://127.0.0.1:$port" -H 'Content-Type: application/json' --data-binary "@$workspace/exchange.json" "http://127.0.0.1:$port/console/session" -o "$workspace/session-response.json"
python3 - "$workspace/session-response.json" <<'PY'
import json, pathlib, sys
response = json.loads(pathlib.Path(sys.argv[1]).read_text())
if set(response) != {"csrf", "expires"} or not all(response.values()):
    raise SystemExit("installed console session response is invalid")
PY
curl --fail --silent --show-error -b "$workspace/cookies" "http://127.0.0.1:$port/console" -o "$workspace/authenticated.html"
grep -F 'Agent Registry' "$workspace/authenticated.html" >/dev/null
grep -F 'authoritative collection is empty' "$workspace/authenticated.html" >/dev/null
! grep -F 'Authenticate this browser' "$workspace/authenticated.html" >/dev/null

printf '%s\n' 'installed console verified: extracted_binary=true self_hosted_asset=true authenticated_surface=true archive_members=1'
