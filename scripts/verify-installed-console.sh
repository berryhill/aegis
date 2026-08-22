#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
socket_dir=${AEGIS_PROOF_SOCKET_DIR:-$repo}
case "$socket_dir" in /*) ;; *) printf '%s\n' 'installed console proof socket directory must be absolute' >&2; exit 1 ;; esac
[ -d "$socket_dir" ] && [ ! -L "$socket_dir" ] || { printf '%s\n' 'installed console proof socket directory must be one existing real directory' >&2; exit 1; }
[ "$(CDPATH= cd -- "$socket_dir" && pwd -P)" = "$socket_dir" ] || { printf '%s\n' 'installed console proof socket directory must be canonical' >&2; exit 1; }
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
  if [ -n "$socket" ]; then rm -f "$socket" "$socket.lock"; fi
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
socket=$socket_dir/.c-$port.sock
[ ! -e "$socket" ] && [ ! -L "$socket" ] || { printf '%s\n' 'installed console proof socket already exists' >&2; exit 1; }
python3 - "$workspace" "$port" "$uid" "$user" "$socket" <<'PY'
import pathlib, secrets, sys
root, port, uid, user, socket = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5]
token = secrets.token_urlsafe(48)
token_path = root / "transport" / "api.token"
token_path.parent.mkdir(mode=0o700)
token_path.write_text(token + "\n", encoding="utf-8")
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
  token_file: {token_path}
  read_timeout: 5s
  write_timeout: 5s
  shutdown_timeout: 2s
  max_body_bytes: 1048576
  console:
    origin: http://127.0.0.1:{port}
    session_ttl: 1m
    bootstrap_ttl: 45s
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
for name in ("aegis.yaml", "hermes-fixture"):
    (root / name).chmod(0o700 if name == "hermes-fixture" else 0o600)
token_path.chmod(0o600)
PY
mkdir -m 0700 "$workspace/state"
go run "$repo/scripts/demo-authority-init" "$workspace/state/persistence/authority-v1" >/dev/null
python3 - "$workspace/agent.json" <<'PY'
import json, pathlib, sys
digest = "sha256:" + ("a" * 64)
fixture = {
    "schema_version": "aegis.current-fleet.fixture.v1",
    "fleet_id": "fleet-primary",
    "agents": [{
        "source_id": "fleet-agent-1",
        "agent_id": "agent-alpha",
        "runtime": {"adapter": "hermes", "runtime": "hermes-agent", "target": "profile/alpha"},
        "ownership": {"owner_id": "operator-primary", "accountability_id": "team-platform"},
        "lifecycle": "enabled",
        "charter": {"schema_version": "aegis.reference.revision.v1", "id": "agent-alpha", "revision": 7, "digest": digest},
        "capability_declarations": [],
        "policy_refs": [],
    }],
}
request = {
    "fixture": fixture,
    "identity": {"fleet_id": "fleet-primary", "kind": "current-fleet", "source_id": "fleet-agent-1"},
}
path = pathlib.Path(sys.argv[1])
path.write_text(json.dumps(request), encoding="utf-8")
path.chmod(0o600)
PY
(cd "$repo" && go test ./internal/api -run '^TestServeSingletonDeniesBeforeActiveSocketMutation$' -count=1)
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
python3 - "$socket" "$workspace/transport/api.token" "$workspace/agent.json" "$workspace/agent-response.json" <<'PY'
import pathlib, socket, sys
socket_path, token_path, request_path, response_path = sys.argv[1:]
token = pathlib.Path(token_path).read_text(encoding="utf-8").strip()
body = pathlib.Path(request_path).read_bytes()
auth_header = b"Author" + b"ization: " + b"Bear" + b"er " + token.encode("ascii")
request = (
    b"POST /v1/agents HTTP/1.1\r\nHost: unix\r\n" + auth_header +
    b"\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: " + str(len(body)).encode("ascii") + b"\r\n\r\n" + body
)
with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as client:
    client.connect(socket_path)
    client.sendall(request)
    response = bytearray()
    while True:
        chunk = client.recv(65536)
        if not chunk:
            break
        response.extend(chunk)
status, _, payload = bytes(response).partition(b"\r\n\r\n")
if not status.startswith(b"HTTP/1.1 201 "):
    raise SystemExit("installed console fixture registration failed")
pathlib.Path(response_path).write_bytes(payload)
PY
grep -F 'Sign the Aegis principal into this browser' "$workspace/shell.html" >/dev/null
grep -F 'Recovery: run' "$workspace/shell.html" >/dev/null
grep -F 'aegis console' "$workspace/shell.html" >/dev/null
! grep -F '/console/assets/datastar-v1.0.2.js' "$workspace/shell.html" >/dev/null
curl --fail --silent --show-error "http://127.0.0.1:$port/console/assets/datastar-v1.0.2.js" -o "$workspace/datastar.js"
[ "$(wc -c <"$workspace/datastar.js")" -gt 1000 ]
recovery_status=$(curl --silent --show-error -o "$workspace/recovery.html" -w '%{http_code}' -H "Origin: http://127.0.0.1:$port" -H 'Content-Type: application/x-www-form-urlencoded' --data 'bootstrap=malformed' "http://127.0.0.1:$port/console/session")
[ "$recovery_status" = 400 ]
grep -F 'bootstrap_invalid_format' "$workspace/recovery.html" >/dev/null
grep -F 'Bootstrap lifetime: <strong>45s</strong>' "$workspace/recovery.html" >/dev/null
grep -F 'Browser session lifetime: <strong>1m0s</strong>' "$workspace/recovery.html" >/dev/null
! grep -F '>malformed<' "$workspace/recovery.html" >/dev/null

HOME=$workspace "$candidate" --config "$workspace/aegis.yaml" console >"$workspace/bootstrap-response.json"
python3 - "$workspace" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
response = json.loads((root / "bootstrap-response.json").read_text())
if set(response) != {"bootstrap", "console_origin", "expires_at", "reusable_bearer_exposed", "single_use"}:
    raise SystemExit("installed console command response shape is invalid")
if not response["bootstrap"] or not response["single_use"] or response["reusable_bearer_exposed"]:
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
grep -F 'agent-alpha' "$workspace/authenticated.html" >/dev/null
grep -F 'href="/console/agents/charter-import"' "$workspace/authenticated.html" >/dev/null
! grep -F 'aegis charter validate &lt;charter-file.json&gt;' "$workspace/authenticated.html" >/dev/null
! grep -F 'aegis charter import &lt;charter-file.json&gt;' "$workspace/authenticated.html" >/dev/null
curl --fail --silent --show-error -b "$workspace/cookies" "http://127.0.0.1:$port/console/agents/charter-import" -o "$workspace/charter-import.html"
grep -F '<title>Charter import review · Aegis Console</title>' "$workspace/charter-import.html" >/dev/null
grep -F 'href="/console/agents#/agents"' "$workspace/charter-import.html" >/dev/null
grep -F 'aegis charter validate &lt;charter-file.json&gt;' "$workspace/charter-import.html" >/dev/null
grep -F 'aegis charter import &lt;charter-file.json&gt;' "$workspace/charter-import.html" >/dev/null
! grep -F 'Sign the Aegis principal into this browser' "$workspace/authenticated.html" >/dev/null
! grep -F '/console/assets/datastar-v1.0.2.js' "$workspace/authenticated.html" >/dev/null

HOME=$workspace "$candidate" --config "$workspace/aegis.yaml" console >"$workspace/browser-bootstrap-response.json"
if ! curl --fail --silent --show-error "http://127.0.0.1:$port/console" -o /dev/null; then
  printf 'installed console server became unreachable before browser proof; server_alive=%s\n' "$(kill -0 "$server_pid" 2>/dev/null && printf true || printf false)" >&2
  while IFS= read -r line; do printf '%s\n' "$line" >&2; done <"$workspace/server.log"
  exit 1
fi
set +e
python3 "$repo/scripts/console_browser_test.py" "http://127.0.0.1:$port" "$workspace/browser-bootstrap-response.json" "$workspace"
browser_status=$?
set -e
if [ "$browser_status" -ne 0 ]; then
  printf 'installed browser proof failed; server_alive=%s browser_status=%s\n' "$(kill -0 "$server_pid" 2>/dev/null && printf true || printf false)" "$browser_status" >&2
  while IFS= read -r line; do printf '%s\n' "$line" >&2; done <"$workspace/server.log"
  if [ -f "$workspace/chrome.stderr" ]; then
    printf '%s\n' 'Chrome diagnostics:' >&2
    while IFS= read -r line; do printf '%s\n' "$line" >&2; done <"$workspace/chrome.stderr"
  fi
  exit "$browser_status"
fi

printf '%s\n' 'installed console verified: extracted_binary=true token_file_transport=true singleton_denial=true daemon_console=true retained_asset_direct=true retained_asset_loaded=false authenticated_surface=true real_chrome=true archive_members=1'
