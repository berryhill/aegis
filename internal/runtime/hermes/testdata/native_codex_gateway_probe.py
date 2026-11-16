"""Opt-in networkless installed-Hermes gateway diagnostic, never qualification.

The outer process mounts only the explicit source/interpreters read-only, system
libraries, and a new private fixture directory. No host profile, PM state, native
credentials or network interface is available. Do not replace AIAgent.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import selectors
import shlex
import signal
import subprocess
import tempfile
import time


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def namespace(root, work, command):
    args = ["bwrap", "--unshare-all", "--clearenv", "--die-with-parent", "--new-session"]
    for name in ("/usr", "/bin", "/lib", "/lib64"):
        if Path(name).exists():
            args += ["--ro-bind", name, name]
    args += ["--proc", "/proc", "--dev", "/dev", "--dir", "/etc",
             "--ro-bind", str(root), str(root), "--bind", str(work), "/probe"]
    # The public launcher owns an explicit interpreter; mount its distribution,
    # not the surrounding native home or PM installation state.
    launcher = root / ".hermes/bin/hermes"
    python = Path(shlex.split(launcher.read_text(), comments=True)[1])
    distribution = python.resolve().parent.parent
    if not distribution.is_relative_to(root):
        args += ["--ro-bind", str(distribution), str(distribution)]
    env = {
        "HOME": "/probe/home", "HERMES_HOME": "/probe/home", "TMPDIR": "/probe/tmp",
        "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "PYTHONDONTWRITEBYTECODE": "1",
        "HERMES_ENABLE_PROJECT_PLUGINS": "false", "HERMES_YOLO_MODE": "0",
        "HERMES_SKIP_VERSION_CHECK": "1", "HERMES_DISABLE_LAZY_INSTALLS": "1",
        "HERMES_SAFE_MODE": "1", "HERMES_IGNORE_USER_CONFIG": "1", "HERMES_IGNORE_RULES": "1",
        "HERMES_PYTHON_SRC_ROOT": str(root), "HERMES_TUI_TOOLSETS": "context_engine",
        "HERMES_TUI_SKILLS": "", "HERMES_DISABLE_AUTO_SKILLS": "1",
        "HERMES_TUI_PROVIDER": "openai-codex", "HERMES_MODEL": "gpt-5.3-codex",
        "HERMES_TUI_MODEL": "gpt-5.3-codex",
    }
    for key, value in env.items():
        args += ["--setenv", key, value]
    return args + ["--chdir", "/probe/home", "--", *command]


def startup(root, evidence, label, command):
    with tempfile.TemporaryDirectory(prefix=label + "-", dir=evidence) as directory:
        work = Path(directory)
        (work / "home").mkdir(mode=0o700)
        (work / "tmp").mkdir(mode=0o700)
        proc = subprocess.Popen(namespace(root, work, command), stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        out, err = b"", b""
        ready = False
        selector = selectors.DefaultSelector()
        selector.register(proc.stdout, selectors.EVENT_READ, "out")
        selector.register(proc.stderr, selectors.EVENT_READ, "err")
        deadline = time.monotonic() + 20
        try:
            while selector.get_map() and time.monotonic() < deadline:
                for key, _ in selector.select(0.1):
                    data = os.read(key.fileobj.fileno(), 65536)
                    if not data:
                        selector.unregister(key.fileobj)
                    elif key.data == "out":
                        out += data
                    else:
                        err += data
                if b'"gateway.ready"' in out:
                    ready = True
                    break
                if len(out) + len(err) > 262144:
                    break
        finally:
            proc.stdin.close()
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait(timeout=3)
            selector.close()
        # No credentials have been introduced in this startup-only diagnostic.
        (evidence / (label + ".stdout")).write_bytes(out)
        (evidence / (label + ".stderr")).write_bytes(err)
        result = {"case": label, "gateway_ready": ready, "exit": proc.returncode,
                  "no_committed_environment": b"no dependency environment is committed" in err,
                  "stdout_sha256": hashlib.sha256(out).hexdigest(),
                  "stderr_sha256": hashlib.sha256(err).hexdigest(), "child_reaped": proc.poll() is not None}
    result["home_removed"] = not work.exists()
    return result


def turn(root, evidence, case, bounded_config=False):
    with tempfile.TemporaryDirectory(prefix="turn-", dir=evidence) as directory:
        work = Path(directory)
        home = work / "home"
        home.mkdir(mode=0o700)
        (work / "tmp").mkdir(mode=0o700)
        claims = {"exp": int(time.time()) + 3600, "https://api.openai.com/auth": {"chatgpt_account_id": "synthetic-account"}}
        token = "e30." + base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=") + ".synthetic"
        auth = json.dumps({"version": 1, "providers": {}, "credential_pool": {"openai-codex": [{
            "id": "aegis-controller", "auth_type": "oauth", "source": "manual:aegis-controller", "access_token": token}]}}).encode()
        (home / "auth.json").write_bytes(auth)
        (home / "auth.json").chmod(0o600)
        if bounded_config:
            # Supported settings only, controller-authored in an empty home. This
            # tests whether these knobs suffice; it is not a production policy.
            config = {"model": {"provider": "openai-codex", "default": "gpt-5.3-codex"},
                      "agent": {"max_turns": 1, "api_max_retries": 1, "auto_recovery_cycles": 0},
                      "auxiliary": {"title_generation": {"enabled": False, "model_upgrade_enabled": False}},
                      "updates": {"check": False},
                      "model_catalog": {"enabled": False, "excluded_providers": ["openai-codex"]},
                      "plugins": {"enabled": []}, "mcp_servers": {},
                      "auth": {"adopt_external_logins": False},
                      "memory": {"memory_enabled": False, "user_profile_enabled": False},
                      "compression": {"enabled": False}}
            (home / "config.yaml").write_text(json.dumps(config))
            (home / "config.yaml").chmod(0o600)
        (work / "fixture.py").write_bytes(Path(__file__).with_name("native_codex_wire_fixture.py").read_bytes())
        command = [str(root / "venv/bin/python"), "-I", "-B", "/probe/fixture.py", str(root)]
        args = namespace(root, work, command)
        index = args.index("--chdir")
        args[index:index] = ["--setenv", "AEGIS_FIXTURE_CASE", case]
        proc = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        assert proc.stdin and proc.stdout and proc.stderr
        selector = selectors.DefaultSelector()
        selector.register(proc.stdout, selectors.EVENT_READ, "out")
        selector.register(proc.stderr, selectors.EVENT_READ, "err")
        stdout, stderr, buffer = b"", b"", b""
        summary = {"case": case, "gateway_ready": False, "tools_empty": False, "session_created": False, "completed": False}
        terminal = False
        def send(identifier, method, params):
            proc.stdin.write((json.dumps({"jsonrpc": "2.0", "id": identifier, "method": method, "params": params}) + "\n").encode())
            proc.stdin.flush()
        try:
            deadline = time.monotonic() + 35
            while selector.get_map() and time.monotonic() < deadline and not terminal:
                for key, _ in selector.select(0.1):
                    data = os.read(key.fd, 65536)
                    if not data:
                        selector.unregister(key.fileobj)
                        continue
                    if key.data == "err":
                        stderr += data
                        continue
                    stdout += data
                    buffer += data
                    while b"\n" in buffer:
                        line, buffer = buffer.split(b"\n", 1)
                        try:
                            message = json.loads(line)
                        except ValueError:
                            continue
                        event = message.get("params", {}).get("type")
                        if event == "gateway.ready":
                            summary["gateway_ready"] = True
                            send("tools", "tools.show", {})
                        if message.get("id") == "tools":
                            result = message.get("result", {})
                            summary["tools_empty"] = result.get("total") == 0 and result.get("sections") == []
                            if not summary["tools_empty"]:
                                terminal = True
                                break
                            send("create", "session.create", {"source": "aegis-attempt", "cols": 100})
                        if message.get("id") == "create":
                            result = message.get("result", {})
                            sid = result.get("session_id") or result.get("id")
                            summary["session_created"] = bool(sid)
                            if not sid:
                                terminal = True
                                break
                            send("prompt", "prompt.submit", {"session_id": sid, "text": "Synthetic gateway transport check. Reply SYNTHETIC_OK without tools."})
                        if event in ("message.complete", "error"):
                            summary["completed"] = event == "message.complete"
                            summary["terminal_payload"] = message.get("params", {}).get("payload", {})
                            terminal = True
                    if len(stdout) + len(stderr) > 262144:
                        terminal = True
            summary["deadline_reached"] = time.monotonic() >= deadline
        finally:
            proc.stdin.close()
            try:
                proc.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait(timeout=3)
            selector.close()
        summary["exit"] = proc.returncode
        summary["child_reaped"] = proc.poll() is not None
        summary["auth_unchanged"] = (home / "auth.json").read_bytes() == auth
        summary["forbidden_effect_absent"] = not (home / "forbidden-effect").exists()
        log_bytes = b"".join(p.read_bytes() for p in home.rglob("*.log") if p.is_file())
        summary["token_absent_from_protocol_and_logs"] = all(token.encode() not in blob for blob in (stdout, stderr, log_bytes))
        wire = work / "wire.json"
        summary["wire"] = json.loads(wire.read_text()) if wire.exists() else None
        identity_file = work / "identity.json"
        if identity_file.exists():
            (evidence / (case + ".identity.json")).write_bytes(identity_file.read_bytes())
        payload = summary.get("terminal_payload", {})
        summary["successful"] = summary["completed"] and payload.get("status") == "complete"
        wire_report = summary["wire"] or {}
        native_turn = wire_report.get("agent_init", 0) > 0 and wire_report.get("agent_run", 0) > 0
        outcome = (summary["successful"] and payload.get("text") == "SYNTHETIC_OK") if case == "complete" else (
            summary["completed"] and payload.get("status") == "error")
        summary["expected_outcome_pass"] = bool(outcome and native_turn and summary["tools_empty"]
            and summary["session_created"] and not summary["deadline_reached"]
            and summary["token_absent_from_protocol_and_logs"] and summary["forbidden_effect_absent"])
        summary["fixed_destination_pass"] = bool(summary["wire"]) and all(r["exact_destination"] for r in summary["wire"]["requests"])
        # Never retain a token value, even if the native diagnostic leaks it.
        for suffix, blob in (("stdout", stdout), ("stderr", stderr), ("logs", log_bytes)):
            (evidence / (case + "." + suffix)).write_bytes(blob.replace(token.encode(), b"[REDACTED_SYNTHETIC_TOKEN]"))
        safe = json.dumps(summary).replace(token, "[REDACTED_SYNTHETIC_TOKEN]")
        summary = json.loads(safe)
    summary["home_removed"] = not work.exists()
    return summary


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("evidence", type=Path, help="new nonexistent private evidence directory")
    parser.add_argument("--cases", nargs="+", choices=["complete", "malformed", "truncated", "tool", "tool-text", "401", "403", "429", "500"], default=["complete"])
    parser.add_argument("--bounded-config", action="store_true")
    parser.add_argument("--require-terminal", action="store_true", help="fail when selected native outcome checks fail; not full qualification")
    options = parser.parse_args()
    root = options.source.resolve(strict=True)
    evidence = options.evidence.resolve()
    evidence.mkdir(mode=0o700, parents=True, exist_ok=False)
    cases = [
        ("public-launcher", [str(root / ".hermes/bin/hermes"), "--run-module", "tui_gateway.entry"]),
        ("adapter-gateway", [str(root / "venv/bin/python"), "-m", "tui_gateway.entry"]),
    ]
    results = [startup(root, evidence, label, command) for label, command in cases]
    results.extend(turn(root, evidence, case, options.bounded_config) for case in options.cases)
    result = {"scope": "native gateway diagnostic; not transport qualification", "bounded_config": options.bounded_config, "results": results,
              "python": str((root / "venv/bin/python").resolve()),
              "python_sha256": digest(root / "venv/bin/python"),
              "sources": {p: digest(root / p) for p in ["hermes_bootstrap.py", "pm/environments.py",
                          "hermes_cli/venv_sync.py", "tui_gateway/entry.py"]}}
    (evidence / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({"evidence": str(evidence), "scope": result["scope"], "results": [
        {k: v for k, v in row.items() if k not in ("wire", "terminal_payload")} for row in results]}, indent=2))
    if options.require_terminal and any(not row.get("expected_outcome_pass", True) for row in results):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
