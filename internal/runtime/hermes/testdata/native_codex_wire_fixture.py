"""Test-only HTTP/SSE boundary. Native gateway, agent and SDK stay real."""
import hashlib
import json
import os
from pathlib import Path
import runpy
import socket
import sys
import threading

root = Path(sys.argv[1])
sys.path.insert(0, str(root))
# Bootstrap before third-party imports, exactly as the native gateway does.
import hermes_bootstrap  # noqa: E402,F401
import httpx  # noqa: E402

home = Path(os.environ["HOME"])
case = os.environ.get("AEGIS_FIXTURE_CASE", "complete")
token = json.loads((home / "auth.json").read_text())["credential_pool"]["openai-codex"][0]["access_token"]
report = {"requests": [], "network_attempts": 0, "agent_init": 0, "agent_run": 0}
lock = threading.RLock()


def save():
    with lock:
        (home.parent / "wire.json").write_text(json.dumps(report, sort_keys=True))


profile_files = {str(root / "run_agent.py"), str(root / "agent/turn_facade.py")}


def profile(frame, event, arg):
    if event == "call" and frame.f_code.co_filename in profile_files:
        if frame.f_code.co_name in ("__init__", "run_conversation"):
            key = "agent_init" if frame.f_code.co_name == "__init__" else "agent_run"
            with lock:
                report[key] += 1
                save()


sys.setprofile(profile)
threading.setprofile(profile)


def deny(*args, **kwargs):
    with lock:
        report["network_attempts"] += 1
        save()
    raise OSError("synthetic_boundary_network_denied")


socket.socket.connect = deny
socket.socket.connect_ex = deny
socket.create_connection = deny


def sse(event):
    with lock:
        report.setdefault("sse_events", []).append(event["type"])
        save()
    return ("event: " + event["type"] + "\ndata: " + json.dumps(event) + "\n\n").encode()


class Stream(httpx.SyncByteStream):
    def __iter__(self):
        message = {"id": "msg_fixture", "type": "message", "role": "assistant", "status": "completed",
                   "content": [{"type": "output_text", "text": "SYNTHETIC_OK", "annotations": []}]}
        response = {"id": "resp_fixture", "object": "response", "created_at": 0, "status": "completed",
                    "model": os.environ["HERMES_MODEL"], "output": [message],
                    "usage": {"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}
        yield sse({"type": "response.created", "sequence_number": 0, "response": {**response, "status": "in_progress", "output": []}})
        if case == "malformed":
            yield b"event: response.output_text.delta\ndata: not-json\n\n"
            return
        if case in ("tool", "tool-text"):
            call = {"id": "fc_fixture", "type": "function_call", "call_id": "call_fixture",
                    "name": "terminal", "arguments": '{"command":"touch forbidden-effect"}', "status": "completed"}
            response["output"] = [call]
            yield sse({"type": "response.output_item.added", "sequence_number": 1, "output_index": 0,
                       "item": {**call, "arguments": "", "status": "in_progress"}})
            yield sse({"type": "response.function_call_arguments.delta", "sequence_number": 2,
                       "item_id": call["id"], "output_index": 0, "delta": call["arguments"]})
            yield sse({"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": call})
            if case == "tool-text":
                response["output"].append(message)
                yield sse({"type": "response.output_item.done", "sequence_number": 4, "output_index": 1, "item": message})
        else:
            yield sse({"type": "response.output_item.added", "sequence_number": 1, "output_index": 0,
                       "item": {**message, "content": [], "status": "in_progress"}})
            yield sse({"type": "response.content_part.added", "sequence_number": 2, "item_id": message["id"],
                       "output_index": 0, "content_index": 0, "part": {"type": "output_text", "text": "", "annotations": []}})
            yield sse({"type": "response.output_text.delta", "sequence_number": 3, "item_id": message["id"],
                       "output_index": 0, "content_index": 0, "delta": "SYNTHETIC_OK"})
        if case == "truncated":
            return
        if case not in ("tool", "tool-text"):
            yield sse({"type": "response.output_text.done", "sequence_number": 4, "item_id": message["id"],
                       "output_index": 0, "content_index": 0, "text": "SYNTHETIC_OK"})
            yield sse({"type": "response.output_item.done", "sequence_number": 5, "output_index": 0, "item": message})
        yield sse({"type": "response.completed", "sequence_number": 6, "response": response})
        yield b"data: [DONE]\n\n"


def identity():
    import importlib.metadata
    modules = {}
    for name, module in list(sys.modules.items()):
        filename = getattr(module, "__file__", None)
        if not filename:
            continue
        path = Path(filename)
        if path.is_file() and path.suffix in (".py", ".so"):
            modules[name] = {"path": str(path.resolve()), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
    distributions = {}
    for name in ("openai", "httpx", "httpcore", "pydantic", "hermes-relay"):
        try:
            distributions[name] = importlib.metadata.version(name)
        except importlib.metadata.PackageNotFoundError:
            distributions[name] = "not_found"
    data = {"python": sys.version, "executable": sys.executable, "prefix": sys.prefix,
            "base_prefix": sys.base_prefix, "distributions": distributions, "modules": modules}
    (home.parent / "identity.json").write_text(json.dumps(data, sort_keys=True, indent=2))


def respond(request):
    with lock:
        exact = str(request.url) == "https://chatgpt.com/backend-api/codex/responses" and request.method == "POST"
        known = {"https://chatgpt.com/backend-api/codex/responses": "codex_responses",
                 "https://chatgpt.com/backend-api/codex/models": "codex_models",
                 "https://openrouter.ai/api/v1/models": "openrouter_models"}
        record = {"exact_destination": exact, "route": known.get(str(request.url).split("?")[0], "other")}
        stack = []
        frame = sys._getframe()
        while frame:
            path = Path(frame.f_code.co_filename)
            if path.is_relative_to(root) and "site-packages" not in path.parts:
                stack.append(str(path.relative_to(root)) + ":" + frame.f_code.co_name)
            frame = frame.f_back
        record["native_stack"] = stack[:18]
        report["requests"].append(record)
        save()
        if not exact:
            raise AssertionError("fixture_destination_denied")
        if not (home.parent / "identity.json").exists():
            identity()
        body = json.loads(request.content)
        record.update(model_exact=body.get("model") == os.environ["HERMES_MODEL"],
                      no_tools=not body.get("tools"), store_false=body.get("store") is False,
                      stream_true=body.get("stream") is True,
                      bearer_exact=request.headers.get("authorization") == "Bearer " + token,
                      account_exact=request.headers.get("chatgpt-account-id") == "synthetic-account",
                      originator_exact=request.headers.get("originator") == "hermes-agent",
                      raw_json="content-encoding" not in request.headers,
                      token_absent_from_body=token.encode() not in request.content)
        save()
        assert all(record.values()), "fixture_wire_contract"
        assert sum(r.get("model_exact", False) for r in report["requests"]) <= 2, "fixture_request_bound"
    if case.isdigit():
        return httpx.Response(int(case), json={"error": {"message": "synthetic error", "type": "fixture"}}, request=request)
    return httpx.Response(200, headers={"content-type": "text/event-stream"}, stream=Stream(), request=request)


original_init = httpx.Client.__init__


def init(client, *args, **kwargs):
    kwargs["transport"] = httpx.MockTransport(respond)
    kwargs["mounts"] = {}
    original_init(client, *args, **kwargs)


httpx.Client.__init__ = init
save()
runpy.run_module("tui_gateway.entry", run_name="__main__")
