"""Aegis controller-only native Hermes Codex store protocol (embedded).
Input: one already protected/pinned read-only source descriptor, not a path.
Output: access-only token or candidate protocol to PRIVATE controller pipe only.
Public metadata is validated/redacted by the controller. Never run from tools.
No refresh, writes, network, fallback, ambient credentials or diagnostics.
"""
import hashlib
import json
import re
import logging
import os
import socket
import sys


def main():
    logging.disable(logging.CRITICAL)
    sys.path.insert(0, sys.argv[1])

    def deny(*args, **kwargs):
        raise RuntimeError("native connection denies side effects")

    socket.socket.connect = deny
    socket.create_connection = deny
    try:
        from hermes_cli.auth import _load_auth_store, _provider_state_in
    except BaseException:
        sys.exit(20)

    class PinnedStore:
        # Hermes owns JSON decoding/legacy format interpretation. Its corruption
        # backup path is deliberately unavailable: this connection is read-only.
        def exists(self):
            return True

        def read_text(self, encoding):
            with os.fdopen(os.dup(3), "r", encoding=encoding) as source:
                value = source.read((1 << 20) + 1)
            if len(value) > 1 << 20:
                raise ValueError("oversize")
            return value

        def with_suffix(self, suffix):
            raise ValueError("read-only native source")

    store = _load_auth_store(PinnedStore())
    # Native Hermes is pool-first. A singleton is neither a prerequisite nor
    # authority to override the local pool. Do not call load_pool/read_credential_pool:
    # those may hydrate shared-root/external sources, seed, refresh, or persist.
    # Decode only this pinned descriptor with native APIs, then rehydrate entries
    # with the qualified native PooledCredential class below.
    state = _provider_state_in(store, "openai-codex")
    pool = store.get("credential_pool", {})
    if not isinstance(pool, dict):
        sys.exit(22)
    entries = pool.get("openai-codex", [])
    if not isinstance(entries, list):
        sys.exit(22)
    if not entries:
        if state is None:
            # Only the approved local store was inspected. This says nothing
            # about root/shared login availability and never reads that source.
            return
        sys.exit(22)
    # Any local suppression changes effective selection. Do not infer a usable
    # login from an excluded singleton or consult a different store.
    suppressed = store.get("suppressed_sources", {})
    if not isinstance(suppressed, dict) or suppressed.get("openai-codex"):
        sys.exit(23)
    try:
        from agent.credential_pool import PooledCredential
    except BaseException:
        sys.exit(20)
    if len(entries) > 64:
        sys.exit(22)
    mode = sys.argv[2] if len(sys.argv) > 2 else "resolve"
    selector = sys.argv[3] if len(sys.argv) > 3 else ""

    def identifier(entry):
        # Do not expose native labels or even raw IDs: either may contain secrets.
        # Missing native IDs must not acquire from_dict's random generated ID.
        value = entry.get("id")
        if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", value):
            return ""
        return "nc-" + hashlib.sha256(b"aegis-native-codex-id-v1\x00" + value.encode()).hexdigest()

    def reason(native):
        if (native.source not in ("device_code", "manual:device_code")
                or native.extra.get("secret_source") is not None):
            return "source_approval_required"
        if native.auth_type != "oauth":
            return "unsupported_auth_type"
        if native.last_status not in (None, "", "ok"):
            return "unusable_status"
        if not isinstance(native.access_token, str) or not native.access_token:
            return "token_absent"
        if len(native.access_token) > 32 << 10:
            return "token_invalid"
        return "eligible"

    if not all(isinstance(entry, dict) for entry in entries):
        sys.exit(22)
    ids = [identifier(entry) for entry in entries]
    if mode == "candidates":
        rows = []
        for entry, cid in zip(entries, ids):
            native = PooledCredential.from_dict("openai-codex", entry)
            why = reason(native)
            if not cid:
                why = "id_missing_or_invalid"
            elif ids.count(cid) != 1:
                why = "id_duplicate"
            row = {"credential_id": cid, "reason": why}
            # This is a PRIVATE controller pipe, not the metadata API. Only the
            # controller validates JWT claims; raw JSON/labels never cross it.
            if why == "eligible":
                row["token"] = native.access_token
            rows.append(row)
        sys.stdout.write(json.dumps(rows))
        return
    if mode != "resolve":
        sys.exit(26)
    if selector:
        if not re.fullmatch(r"nc-[a-f0-9]{64}", selector) or ids.count(selector) != 1:
            sys.exit(28)
        entries = [entries[ids.index(selector)]]
    identities = set()
    for entry in entries:
        native = PooledCredential.from_dict("openai-codex", entry)
        why = reason(native)
        if why == "source_approval_required":
            sys.exit(27)
        if why == "unsupported_auth_type":
            sys.exit(24)
        if why != "eligible":
            sys.exit(25)
        identities.add(native.access_token)
    # Exact aliases are one identity. Never use priority, labels, random choice,
    # singleton metadata, or an account pin to silently choose between grants.
    if len(identities) != 1:
        sys.exit(24)
    sys.stdout.write(identities.pop())


try:
    main()
except SystemExit:
    raise
except BaseException:
    # No raw native exception, body, credentials or diagnostics cross the pipe.
    sys.exit(26)
