"""Controller-only diagnostic guard around the unchanged native gateway.

No client, resolver, tool, scheduler or credential implementation lives here.
The native SDK generates/sends its normal request. Permit at most one exact
Codex request, and retain only reviewed structured error literals, never text.
"""
import json
import os
import runpy
import sys
import threading

PROMPT = "Provider format diagnostic only. Reply with OK. Do not use tools or perform any task."
PARAMETERS = frozenset(("model", "temperature", "top_p", "max_output_tokens", "max_tokens", "reasoning", "reasoning.effort", "reasoning.summary", "text.verbosity", "tools", "tool_choice", "parallel_tool_calls", "store", "stream", "input", "instructions", "prompt_cache_key", "prompt_cache_retention", "service_tier", "include", "context_management"))
CODES = frozenset(("invalid_request_error", "unsupported_parameter", "unsupported_value", "invalid_value", "unknown_parameter", "missing_required_parameter", "model_not_found", "invalid_prompt"))


def closed_details(body):
    if not isinstance(body, dict) or not isinstance(body.get("error"), dict):
        return {}
    error = body["error"]
    out = {}
    for key, allowed in (("param", PARAMETERS), ("code", CODES)):
        value = error.get(key)
        if isinstance(value, str) and value in allowed:
            out["request_parameter" if key == "param" else "request_code"] = value
    return out


def closed_status(status):
    if status in (401, 403):
        return {"layer": "auth", "code": "auth"}
    if status == 429:
        return {"layer": "provider", "code": "rate_limit"}
    if status >= 500:
        return {"layer": "provider", "code": "server_error"}
    if status in (400, 422):
        return {"layer": "provider", "code": "format_error"}
    return {"layer": "provider", "code": "unknown"}


def install_guard(client_class, surface_module, model):
    original = client_class.send
    lock = threading.Lock()
    used = False
    details = {}

    def send(client, request, *args, **kwargs):
        nonlocal used, details
        # No redirects, alternate endpoints, refresh, auxiliary model or tools.
        if str(request.url) != "https://chatgpt.com/backend-api/codex/responses" or request.method != "POST":
            raise RuntimeError("aegis_provider_probe_destination_denied")
        with lock:
            if used:
                raise RuntimeError("aegis_provider_probe_request_limit")
            used = True
        body = json.loads(request.content)
        if body.get("model") != model or body.get("tools") or body.get("store") is not False:
            raise RuntimeError("aegis_provider_probe_contract_denied")
        inputs = body.get("input")
        if not isinstance(inputs, list) or len(inputs) != 1 or inputs[0].get("role") != "user":
            raise RuntimeError("aegis_provider_probe_input_denied")
        content = inputs[0].get("content")
        if content != PROMPT and content != [{"type": "input_text", "text": PROMPT}]:
            raise RuntimeError("aegis_provider_probe_input_denied")
        kwargs["follow_redirects"] = False
        response = original(client, request, *args, **kwargs)
        if response.status_code >= 400:
            # Native SDK will consume the same response. Never retain raw bytes.
            chunks, size = [], 0
            for chunk in response.iter_bytes():
                size += len(chunk)
                if size > 65536:
                    response.close()
                    raise RuntimeError("aegis_provider_probe_error_bound")
                chunks.append(chunk)
            response._content = b"".join(chunks)
            try:
                details = closed_details(response.json())
            except Exception:
                details = {}
            # Report the first bounded response before native automatic repair
            # retries. The controller then reaps this diagnostic process; the
            # one-request guard still blocks any race to a second send.
            from tui_gateway.server import _emit
            _emit("aegis.provider-format-error", "", {"error_surface": {
                **closed_status(response.status_code), **details}})
        return response

    client_class.send = send
    for name in ("build_error_surface_from_result", "build_error_surface_from_exception"):
        original_surface = getattr(surface_module, name)

        def surface(*args, _original=original_surface, **kwargs):
            result = _original(*args, **kwargs)
            if isinstance(result, dict):
                result.update(details)
            return result

        setattr(surface_module, name, surface)


def main():
    root = os.environ["HERMES_PYTHON_SRC_ROOT"]
    sys.path.insert(0, root)
    import httpx
    from agent import error_surface
    install_guard(httpx.Client, error_surface, os.environ["HERMES_MODEL"])
    runpy.run_module("tui_gateway.entry", run_name="__main__")


if __name__ == "__main__":
    main()
