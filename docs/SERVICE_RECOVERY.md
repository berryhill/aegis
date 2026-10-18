# Recover a stopped foreign user gateway unit

Linux only. This is not a force-update or general repair command. Normal install, bootstrap, and update continue rejecting foreign units. Recovery changes only the exact current-account `aegis.service` unit and reloads its identity; it never stops a foreign process or replaces an executable.

For a source coordinator, build with `./scripts/build-source.sh ./aegis`. Explicitly bind the existing production release (not the source binary) and canonical configuration:

```sh
./aegis gateway recover --installed-executable /absolute/path/to/installed/aegis --config "$HOME/.aegis/aegis.yaml"
./aegis gateway recover --installed-executable /absolute/path/to/installed/aegis --config "$HOME/.aegis/aegis.yaml" --apply
```

The first command is read-only. Apply requires a real terminal, protected enrolled-principal password authentication, and typing the entire displayed `RECOVER AEGIS <digest>` phrase. Over interactive SSH, keep stdin and diagnostic stderr attached to the terminal and omit `--pinentry-executable`: Aegis requests the enrolled principal password directly there with echo disabled, restoring terminal modes on success, cancellation, or timeout. The two-minute protected-request deadline is unchanged. An explicit pinentry helper overrides this route; a helper timeout after `GETPIN` is final, not permission for a second prompt. Do not send passwords through chat, command arguments, environment variables, or shell startup files. The installed executable must pass release inspection; the foreign unit never supplies executable, configuration, or principal identity. Configuration must resolve to canonical production state.

Systemd must report inactive/failed and MainPID 0. Running foreign processes, unsafe links/modes/ancestry, changed bytes/inode, and evidence failures deny. Original bytes are synced to the owner-only reported `.aegis-recovery-<digest>-<inode>.unit` file beside the unit before atomic exchange. Preserve that file for operator review. On reload or postcondition failure, the restored unit and evidence may remain: do not assume rollback or readiness. No arbitrary restore operation is provided.

Success verifies only restored/loaded unit identity and stopped state. It does not establish protected transport, authenticated readiness, or current audit. Continue with the supported `update --installed` coordinator journey against the same production configuration; its existing release verification, principal authentication, journal and readiness checks still apply. If recovery reports a recognized unit, it performs no replacement: use ordinary status/update instead.

No reset, credential/authority changes, expiry widening, manual executable copy, service start, or release publication is part of recovery.

## Reviewed Linux access policy

Recovery uses descriptor-relative no-follow traversal for both the installed executable and unit directory. Every ancestor must be owned by root or the current UID. Accessible group/other-writable ancestors deny (root-owned sticky directories retain the existing sticky-directory exception). An owned ancestor with no group/other search bits and no POSIX ACL creates a restrictive search barrier: writable descendants such as `.config` mode 0775 beneath home mode 0700 are accepted because another unprivileged UID cannot traverse that barrier. Ownership and no-symlink checks continue below it. Recovery never changes permissions.

Any nonempty POSIX access or default ACL denies, even beneath a private barrier; ACL query errors deny except ENODATA (absent) and ENOTSUP (filesystem lacks POSIX ACL support). This is intentionally conservative, not a general ACL evaluator or host sandbox. Root and same-UID hostile processes are outside this access policy. The executable must additionally be a regular current-UID-owned single-link executable, without group/other write or access ACLs; its inode and digest are rechecked before apply. Loaded `DropInPaths` must be empty before any recovery mutation and after daemon-reload. Recovery does not remove overrides.
