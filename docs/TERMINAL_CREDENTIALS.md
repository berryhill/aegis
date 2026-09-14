# Create a credential from the conversational terminal

In the authenticated, gateway-backed Linux Aegis terminal, ask either:

- `can we add a secret for a test?`
- `can we add a secret as a test`

Do not include a credential value in the request. Aegis bypasses the model and opens its own protected dialog without starting a new conversation or invoking a standalone credential command.

1. Enter a non-secret reference (for example, `disposable-test`). Enter a non-secret kind, or press Enter for `opaque`.
2. Review the exact reference, kind and authenticated principal. Input in this dialog is not echoed or added to chat history; the validated metadata is displayed for review.
3. Type exactly `yes` at the fresh approval prompt. Empty input or any other answer declines. Type-ahead from metadata entry is discarded; a pasted batch cannot serve as pre-approval.
4. At `Secret value (no echo)`, type a disposable single-line value and press Enter, or use terminal-framed bracketed paste for a multiline value and press Enter after the paste. Repeat the exact value at the separate protected confirmation prompt. The maximum value is 1 MiB. Do not use an unframed multiline paste.
5. A successful operation prints only the record ID and reference, verifies metadata through canonical encrypted custody and returns to the same conversation. It grants no credential binding or registered-Agent rights.

Ctrl-C or Ctrl-D outside a framed paste cancels without submitting a value. Decline, empty input and confirmation mismatch create nothing. An interrupted, incomplete or oversized paste/read without a known input boundary stops the conversation rather than risking late secret bytes in chat. Terminal modes are restored when the dialog exits; Aegis does not control a parent shell or external terminal recorder.

The operation expires after at most five minutes, bounded by the current authenticated session. Closing/revoking the session or restarting the gateway invalidates outstanding intake. Invalid operation/session substitution, expired authority and replay deny. Duplicate references remain subject to the canonical insert-only custody policy.

If creation is **not confirmed** after submission, do not replay the value or immediately create another operation: a lost response or audit/readback failure can occur after persistence. Inspect credential metadata first, using `what secrets do I have?` in the authenticated terminal. A failure message is not proof that nothing was stored.

Both input and review output must be terminals. Redirected/piped clients, browser requests and platforms without cancellation-safe protected intake retain the non-mutating unsupported-client guidance. Gateway turn endpoints still do not accept credential values; this feature uses a separate protected byte endpoint. The protocol is specified in [CONTROL_PLANE_API.md](../specs/CONTROL_PLANE_API.md).

This boundary prevents intentionally collected values from entering Aegis chat/model traffic; it is not a host sandbox, protection from the same OS account/root, guaranteed heap erasure, or a way to remove values already typed into ordinary terminal scrollback.
