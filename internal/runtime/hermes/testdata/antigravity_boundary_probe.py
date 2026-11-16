"""Offline inspection of an explicit installed Antigravity plugin.

This is a DISQUALIFICATION probe, not provider qualification or real agy proof.
The actual plugin consumes a synthetic child process. The child performs a
scratch-only effect before publishing a native tool event, demonstrating that
post-event termination cannot establish pre-execution denial. No native OAuth
material, real agy process, provider call, profile config or host keyring is used.
Usage: python -B antigravity_boundary_probe.py PLUGIN_SOURCE NEW_PRIVATE_DIRECTORY
"""
import json
import os
from pathlib import Path
import sys

plugin = Path(sys.argv[1]).resolve(strict=True)
root = Path(sys.argv[2]).absolute()
root.mkdir(mode=0o700, parents=False, exist_ok=False)
root = root.resolve(strict=True)
sys.dont_write_bytecode = True
os.environ.clear()
os.environ.update(
    HOME=str(root), HERMES_HOME=str(root / "hermes"),
    XDG_CONFIG_HOME=str(root / "config"), XDG_CACHE_HOME=str(root / "cache"),
    TMPDIR=str(root), PATH="/usr/bin:/bin", PYTHONDONTWRITEBYTECODE="1",
    ANTIGRAVITY_CONFIG_DIR=str(root / "synthetic-auth"),
    AEGIS_SYNTHETIC_AMBIENT="ambient-canary",
)
(root / "hermes").mkdir(mode=0o700)
(root / "synthetic-auth").mkdir(mode=0o700)
source = root / "synthetic-auth" / "jetski-standalone-oauth-token"
source.write_text("SYNTHETIC_NOT_A_CREDENTIAL", encoding="utf-8")
source.chmod(0o600)
child = root / "synthetic-agy"
child.write_text("#!" + sys.executable + "\n" + '''import json, os, pathlib, sys
sys.stdin.readline()
pathlib.Path('effect-before-event').write_text('synthetic effect')
pathlib.Path('environment-observation').write_text(json.dumps({
    'ambient_inherited': os.environ.get('AEGIS_SYNTHETIC_AMBIENT') == 'ambient-canary',
    'settings_present': '--settings' in sys.argv,
}))
print(json.dumps({'event':'init', 'conversation_id':'synthetic'}), flush=True)
print(json.dumps({'event':'step_update', 'step_update':{
    'step_type':'tool', 'tool_name':'run_command'}}), flush=True)
for line in sys.stdin: pass
''', encoding="utf-8")
child.chmod(0o700)


def guard(event, args):
    if event in ("socket.connect", "socket.getaddrinfo"):
        raise RuntimeError("network denied in boundary probe")
    if event == "subprocess.Popen" and Path(args[0]) != child:
        raise RuntimeError("only synthetic child permitted")
    if event == "open" and isinstance(args[0], (str, bytes)):
        path = Path(os.fsdecode(args[0])).absolute()
        sensitive = path.name in ("auth.json", ".env", "config.yaml", "jetski-standalone-oauth-token", "antigravity-oauth-token")
        if sensitive and not path.is_relative_to(root):
            raise RuntimeError("native configuration/credential access denied")


sys.addaudithook(guard)
sys.path.insert(0, str(plugin))
import importlib
client_module = importlib.import_module("client")
assert client_module.__file__ is not None
assert Path(client_module.__file__).resolve() == plugin / "client.py"
AntigravityClient = client_module.AntigravityClient

workspace = root / "workspace"
workspace.mkdir(mode=0o700)
with AntigravityClient(command=str(child), cwd=str(workspace)) as client:
    linked = client._isolated_gemini_dir / source.name
    link_exposes_source = linked.is_symlink() and linked.resolve() == source
    chunks = list(client.chat.completions.create(
        model="gemini-3.8-flash", messages=[{"role": "user", "content": "synthetic probe"}],
        tools=[], stream=True, timeout=5,
    ))
observation = json.loads((workspace / "environment-observation").read_text())
observation.update(
    native_token_linked=link_exposes_source,
    effect_preceded_stream_neutralization=(workspace / "effect-before-event").exists(),
    qualification="not_qualified",
    evidence_tier="actual_plugin_synthetic_child_no_network",
)
assert observation["native_token_linked"]
assert observation["ambient_inherited"]
assert not observation["settings_present"]
assert observation["effect_preceded_stream_neutralization"]
print(json.dumps(observation, sort_keys=True))
