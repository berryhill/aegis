"""Offline qualification of Aegis's access-only Codex transport.
Run with installed Hermes Python, source root and a DISPOSABLE home as args.
No real credentials, imports before home binding, or network are permitted.
"""
import base64
import json
import os
import pathlib
import socket
import sys
import time

root, home = map(pathlib.Path, sys.argv[1:])
assert home.is_dir()
os.environ.clear()
os.environ.update(HOME=str(home), HERMES_HOME=str(home),
                  HERMES_SKIP_VERSION_CHECK="1", HERMES_ENABLE_PROJECT_PLUGINS="false",
                  PYTHONDONTWRITEBYTECODE="1")
sys.path.insert(0, str(root))
def no_network(*args, **kwargs):
    raise AssertionError("network forbidden in synthetic qualification")
socket.socket.connect = no_network
socket.create_connection = no_network
from agent.credential_pool import load_pool
from hermes_cli.auth import resolve_codex_runtime_credentials
from hermes_cli.auth_constants import CODEX_ACCESS_TOKEN_REFRESH_SKEW_SECONDS
assert CODEX_ACCESS_TOKEN_REFRESH_SKEW_SECONDS == 120

def material(seconds, source="manual:aegis-controller"):
    claims = {"exp": int(time.time()) + seconds,
              "https://api.openai.com/auth": {"chatgpt_account_id": "synthetic-account"}}
    payload = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
    token = "e30." + payload + ".synthetic-signature"
    data = {"version": 1, "providers": {}, "credential_pool": {"openai-codex": [
        {"id": "aegis-controller", "auth_type": "oauth", "source": source,
         "access_token": token}]}}
    (home / "auth.json").write_text(json.dumps(data))
    (home / "auth.json").chmod(0o600)
    return token

from hermes_cli.auth import _auth_file_path
assert _auth_file_path() == home / "auth.json"
if (home / "auth.json").exists():
    token = json.loads((home / "auth.json").read_text())["credential_pool"]["openai-codex"][0]["access_token"]
else:
    token = material(3600)
pool = load_pool("openai-codex")
entry = pool.select()
assert entry is not None and entry.access_token == token and not entry.refresh_token
resolved = resolve_codex_runtime_credentials()
assert resolved["api_key"] == token
assert resolved["source"] == "credential_pool"
assert "refresh_token" not in (home / "auth.json").read_text()
print("qualified: actual pool loads/selects access-only JWT and resolver uses credential_pool; no network")
material(60)
pool = load_pool("openai-codex")
assert pool.select() is None
print("regression reproduced: 60s JWT cannot be selected without refresh token (120s skew)")
material(3600, source="aegis-controller")
assert load_pool("openai-codex").select() is None
print("regression reproduced: unknown source aegis-controller is pruned; manual:aegis-controller is supported")
