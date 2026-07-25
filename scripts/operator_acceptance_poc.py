#!/usr/bin/env python3
"""Opt-in, narrow human-to-Aegis manager acceptance journey (Linux only)."""

from __future__ import annotations

import argparse
import datetime as dt
import errno
import json
import os
import pty
import re
import secrets
import select
import signal
import subprocess
import sys
import time
from dataclasses import asdict, dataclass, field
from pathlib import Path

COMPOSER = b"[composer] > "
ANSI = re.compile(
    rb"(?:\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1bP.*?\x1b\\|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_])",
    re.DOTALL,
)
CONTROL = re.compile(rb"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]")


@dataclass
class Evidence:
    schema: str = "aegis.operator-acceptance.v1"
    kind: str = "turn"
    outcome: str = "passed"
    journey: str | None = None
    runtime: str | None = None
    intent: str | None = None
    submitted_non_secret_input: str | None = None
    sanitized_visible_result: str | None = None
    started_at: str | None = None
    duration_ms: int | None = None
    checks: list[str] = field(default_factory=list)
    friction: list[str] = field(default_factory=list)


class AcceptanceFailure(RuntimeError):
    pass


class Recorder:
    def __init__(self, evidence_dir: Path, canary: bytes):
        evidence_dir.mkdir(mode=0o700, parents=False, exist_ok=False)
        self.path = evidence_dir / "journey.jsonl"
        self.canary = canary
        self.file = self.path.open("x", encoding="utf-8")
        os.chmod(self.path, 0o600)

    def write(self, item: Evidence) -> None:
        data = {key: value for key, value in asdict(item).items() if value not in (None, [], "")}
        encoded = json.dumps(data, ensure_ascii=True, separators=(",", ":"))
        if self.canary.decode() in encoded:
            raise AcceptanceFailure("protected canary reached the evidence encoder")
        self.file.write(encoded + "\n")
        self.file.flush()

    def turn(self, name: str, intent: str, submitted: str, visible: bytes, started: float, checks: list[str]) -> None:
        if self.canary in visible or self.canary.decode() in submitted:
            raise AcceptanceFailure("protected canary was offered to retained evidence")
        self.write(
            Evidence(
                journey=name,
                intent=intent,
                submitted_non_secret_input=submitted,
                sanitized_visible_result=sanitize(visible),
                started_at=timestamp(started),
                duration_ms=int((time.time() - started) * 1000),
                checks=checks,
            )
        )

    def failure(self, name: str, intent: str, error: Exception) -> None:
        message = sanitize(str(error).encode(errors="replace"))
        self.write(Evidence(journey=name, intent=intent, outcome="failed", friction=[message]))
        self.write(Evidence(kind="summary", outcome="failed", friction=[f"journey stopped at {name}"]))

    def close(self) -> None:
        self.file.close()
        if self.canary in self.path.read_bytes():
            self.path.unlink(missing_ok=True)
            raise AcceptanceFailure("protected canary reached retained evidence; evidence file removed")


class ManagerPTY:
    def __init__(self, command: list[str], environment: dict[str, str]):
        master, slave = pty.openpty()
        self.master = master
        self.buffer = bytearray()
        self.offset = 0
        self.process = subprocess.Popen(
            command,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            env=environment,
            start_new_session=True,
            close_fds=True,
        )
        os.close(slave)

    def write_line(self, value: bytes) -> None:
        os.write(self.master, value + b"\n")

    def read_until(self, marker: bytes, timeout: float, canary: bytes) -> bytes:
        started = self.offset
        deadline = time.monotonic() + timeout
        while marker not in self.buffer[started:]:
            current = bytes(self.buffer[started:])
            if canary in current:
                raise AcceptanceFailure("protected canary appeared on the manager PTY")
            if time.monotonic() >= deadline:
                raise AcceptanceFailure(
                    f"timed out waiting for {marker!r}; sanitized partial result: {sanitize(current)}"
                )
            ready, _, _ = select.select([self.master], [], [], 0.1)
            if not ready:
                continue
            try:
                chunk = os.read(self.master, 4096)
            except OSError as error:
                if error.errno == errno.EIO:
                    chunk = b""
                else:
                    raise
            if not chunk:
                raise AcceptanceFailure(f"manager PTY closed before {marker!r}")
            self.buffer.extend(chunk)
        result = bytes(self.buffer[started:])
        self.offset = len(self.buffer)
        if canary in result:
            raise AcceptanceFailure("protected canary appeared on the manager PTY")
        return result

    def read_to_exit(self, timeout: float, canary: bytes) -> bytes:
        started = self.offset
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            ready, _, _ = select.select([self.master], [], [], 0.1)
            if ready:
                try:
                    chunk = os.read(self.master, 4096)
                except OSError as error:
                    if error.errno == errno.EIO:
                        break
                    raise
                if not chunk:
                    break
                self.buffer.extend(chunk)
                if canary in self.buffer[started:]:
                    raise AcceptanceFailure("protected canary appeared during manager exit")
            if self.process.poll() is not None and not ready:
                break
        else:
            raise AcceptanceFailure("timed out waiting for manager exit")
        self.offset = len(self.buffer)
        return bytes(self.buffer[started:])

    def close(self) -> None:
        try:
            os.close(self.master)
        except OSError:
            pass
        if self.process.poll() is None:
            try:
                os.killpg(self.process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            self.process.wait(timeout=5)


def sanitize(value: bytes) -> str:
    value = ANSI.sub(b"", value)
    value = CONTROL.sub(b"", value.replace(b"\r", b""))
    text = value.decode("utf-8", errors="replace")
    lines = text.splitlines()[:2000]
    return "\n".join(line[:4096] for line in lines).strip()


def timestamp(epoch: float | None = None) -> str:
    moment = dt.datetime.fromtimestamp(epoch or time.time(), tz=dt.timezone.utc)
    return moment.isoformat().replace("+00:00", "Z")


def require_markers(value: bytes, *markers: bytes) -> None:
    for marker in markers:
        if marker not in value:
            raise AcceptanceFailure(f"visible result omitted required typed marker {marker!r}")


def process_group_absent(pid: int) -> bool:
    try:
        os.killpg(pid, 0)
    except ProcessLookupError:
        return True
    except PermissionError:
        return False
    return False


def conduct_turn(
    recorder: Recorder,
    manager: ManagerPTY,
    timeout: float,
    name: str,
    intent: str,
    submitted: str,
    required: tuple[bytes, ...],
) -> None:
    started = time.time()
    manager.write_line(submitted.encode())
    visible = manager.read_until(COMPOSER, timeout, recorder.canary)
    require_markers(visible, *required)
    recorder.turn(name, intent, submitted, visible, started, ["expected typed outcome markers present"])


def command_args(binary: Path, config: Path | None, *tail: str) -> list[str]:
    result = [str(binary)]
    if config:
        result += ["--config", str(config)]
    return result + list(tail)


def run(args: argparse.Namespace) -> Path:
    if sys.platform != "linux":
        raise AcceptanceFailure("the POC PTY driver currently supports Linux only")
    binary = args.aegis.resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        raise AcceptanceFailure(f"Aegis executable is unavailable or not executable: {binary}")
    config = args.config.resolve() if args.config else None
    canary = ("aegis-poc-canary-" + secrets.token_hex(32)).encode()
    recorder = Recorder(args.evidence.resolve(), canary)
    environment = dict(os.environ, AEGIS_ACCESSIBLE="1", TERM="dumb", NO_COLOR="1")
    manager: ManagerPTY | None = None
    failed = False
    try:
        recorder.write(
            Evidence(
                kind="run",
                journey="human-to-aegis-manager-credential",
                runtime="hermes",
                outcome="started",
                started_at=timestamp(),
                checks=["real manager PTY", "generated protected canary", "metadata-only JSONL evidence"],
            )
        )
        manager = ManagerPTY(command_args(binary, config, "manager"), environment)
        try:
            manager.read_until(COMPOSER, args.turn_timeout, canary)
        except Exception as error:
            recorder.failure("startup", "reach the authenticated manager composer", error)
            failed = True
            raise

        conduct_turn(
            recorder,
            manager,
            args.turn_timeout,
            "ordinary-conversation",
            "hold an ordinary manager conversation",
            "Hello. In one short sentence, what can you help me do here?",
            (b"Hermes model / untrusted", b"guarded turn complete"),
        )

        started = time.time()
        manager.write_line(b"Please create a credential named test.")
        visible = manager.read_until(b"Secret value: ", args.turn_timeout, canary)
        manager.write_line(canary)
        visible += manager.read_until(b"Confirm secret value: ", args.turn_timeout, canary)
        manager.write_line(canary)
        visible += manager.read_until(COMPOSER, args.turn_timeout, canary)
        require_markers(visible, b"Credential created", b"reference  test", b"protected intake completed")
        recorder.turn(
            "create-credential-test",
            "create credential test through protected no-echo intake",
            "Please create a credential named test. [protected input omitted]",
            visible,
            started,
            ["protected value not retained", "authoritative create result names test"],
        )

        conduct_turn(
            recorder,
            manager,
            args.turn_timeout,
            "credential-count",
            "ask for the authoritative credential count",
            "How many credentials do I have?",
            (b"Credential inventory", b"total", b"active"),
        )
        conduct_turn(
            recorder,
            manager,
            args.turn_timeout,
            "natural-reference",
            "refer naturally to the credential just created",
            "Show me all test credentials.",
            (b'Credentials matching "test"', b"1. test"),
        )

        started = time.time()
        manager.write_line(b"exit")
        visible = manager.read_to_exit(args.turn_timeout, canary)
        status = manager.process.wait(timeout=args.turn_timeout)
        require_markers(visible, b"Aegis manager stopped; cleanup complete.")
        if status != 0:
            raise AcceptanceFailure(f"manager exited with status {status}")
        if not process_group_absent(manager.process.pid):
            raise AcceptanceFailure("manager process group still exists after exit")
        recorder.turn(
            "exit",
            "exit and complete bounded manager cleanup",
            "exit",
            visible,
            started,
            ["zero process exit", "manager process group absent", "cleanup completion visible"],
        )

        audit_list = subprocess.run(
            command_args(binary, config, "audit", "list"),
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=args.turn_timeout,
            check=False,
        )
        if canary in audit_list.stdout:
            raise AcceptanceFailure("protected canary reached audit list output")
        if audit_list.returncode != 0:
            raise AcceptanceFailure(
                f"audit list exited {audit_list.returncode}: {sanitize(audit_list.stdout)}"
            )
        try:
            audit_events = json.loads(audit_list.stdout)
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise AcceptanceFailure("audit list did not return valid JSON") from error
        if not isinstance(audit_events, list):
            raise AcceptanceFailure("audit list did not return an event list")
        if not any(
            event.get("type") == "credential_created" and event.get("outcome") == "ok"
            for event in audit_events
            if isinstance(event, dict)
        ):
            raise AcceptanceFailure("audit omitted successful credential_created evidence")
        if not any(
            event.get("type") == "manager_session_closed"
            and event.get("outcome") == "ok"
            and event.get("metadata", {}).get("cleanup") == "complete"
            for event in audit_events
            if isinstance(event, dict) and isinstance(event.get("metadata", {}), dict)
        ):
            raise AcceptanceFailure("audit omitted successful manager cleanup evidence")

        started = time.time()
        audit = subprocess.run(
            command_args(binary, config, "audit", "verify"),
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=args.turn_timeout,
            check=False,
        )
        if canary in audit.stdout:
            raise AcceptanceFailure("protected canary reached audit command output")
        if audit.returncode != 0:
            raise AcceptanceFailure(
                f"audit verify exited {audit.returncode}: {sanitize(audit.stdout)}"
            )
        recorder.turn(
            "audit-verification",
            "verify authoritative audit chain",
            "aegis audit verify",
            audit.stdout,
            started,
            [
                "credential_created audit event present",
                "manager_session_closed cleanup event present",
                "audit verify exited zero",
            ],
        )
        recorder.write(
            Evidence(
                kind="summary",
                outcome="passed",
                checks=[
                    "ordinary conversation completed without prose matching",
                    "credential test created through protected intake",
                    "authoritative count and exact metadata search completed",
                    "manager exited and cleaned up",
                    "credential create and manager cleanup audit evidence present",
                    "audit chain verified",
                    "protected canary absent from retained evidence",
                ],
            )
        )
        return recorder.path
    except Exception as error:
        if not failed:
            recorder.failure("journey", "complete the bounded acceptance journey", error)
        raise
    finally:
        if manager:
            manager.close()
        recorder.close()


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aegis", type=Path, default=Path("./aegis"))
    parser.add_argument("--config", type=Path)
    parser.add_argument("--evidence", type=Path, required=True)
    parser.add_argument("--turn-timeout", type=float, default=360.0)
    result = parser.parse_args(argv)
    if not 0 < result.turn_timeout <= 900:
        parser.error("--turn-timeout must be positive and at most 900 seconds")
    return result


def main(argv: list[str] | None = None) -> int:
    try:
        path = run(parse_args(argv or sys.argv[1:]))
    except Exception as error:
        print(f"operator acceptance: {error}", file=sys.stderr)
        return 1
    print(f"operator acceptance passed; safe evidence: {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
