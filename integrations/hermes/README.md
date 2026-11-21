# Plain Linux CLI model-review candidate

The Aegis-owned `aegis-model-review` Hermes plugin declares one zero-argument tool. Its only successful result identifies a **request**, not delivery, a decision, or an applied change. It has no terminal, password, or apply method. An unmodified client does nothing with that request. The matching foreground host source is separately based on Hermes `5a52faf1aed965b8692ec890a7d8af6e37b5e59e`. Plugin loading alone does not install this handler.

See the packaging and activation gates below. The installed profile is **not** this checkout-backed deployment. No lookup or permission fallback is permitted. After companion exit the host keeps echo disabled and chat detached; enter the freshly displayed return code within 60 seconds or the CLI exits. The return code is only a terminal handback, never an approval. Text entered after explicit handback is chat input. A CLI exit cannot control a parent shell or terminal recorder.

Only the attached CLI's exact successful tool completion can schedule this interaction. Prompt-toolkit detaches its reader; Aegis gets terminal fds directly. The launcher passes no user-controlled argv, captures no stdout, and returns no review outcome. Aegis checks foreground process group and the controlling terminal against logind's session tty, then uses the manager's password login, exact review, decision, and checked Agent result. Tool PTYs, redirects, absent logind session, tmux panes distinct from login tty, missing companion, and detached clients cannot substitute chat. The single request slot is spent for the process even on denial; do not blindly replay an unknown effect. Terminal identity is not approval. This design assumes trusted installed frontend, OS and operator terminal, not a sandbox against hostile same-user software or terminal recorders.

## Package, without activating

Use a clean standalone checkout of the reviewed Aegis commit, pinned Go from the Makefile, Python 3 and a trusted local Git object store containing the Hermes commit above. Under an operator-controlled private directory, prepare a mode-0600 JSON object with exactly `config`, `socket`, `origin`, `agent`, `provider`, `model`. Verify the exact approved checkout config, effective protected socket, HTTPS origin (port 8448), serving executable identity and requested lane independently. Do not use credentials as build inputs. The packager, environment, build cache and ancestor directories must be trusted; inputs and outputs are private and must not be committed or uploaded.

```sh
python3 integrations/hermes/package.py "$target_json" "$hermes_repo" "$new_private_output"
(cd "$new_private_output" && sha256sum -c SHA256SUMS)
```

The Linux/amd64 companion has a scoped non-release version and fixed linker bindings; ordinary builds have empty pins and deny review. The Hermes tar is an overlay, not a standalone runtime. Verify `HERMES_BASE` against a new clean private Hermes checkout, install the four `client/` files at the matching relative paths and use that checkout's matching dependencies. Stage `plugins/aegis-model-review` in only the intended profile, explicitly enable the plugin and toolset, then verify **fresh** client and gateway executable/import paths and registration. Do not patch a running human CLI or turn this on fleet-wide.

A separately authorized administrator must install the verified companion at `/usr/local/libexec/aegis/8448/aegis`, root-owned and executable, no set-ID bits, symlinks, untrusted ACLs or group/other-writable ancestry. No fallback locator. The password and exact decision require an independently enrolled operator's foreground logind tty with trusted software, no competing readers or recording. Packaging neither installs nor approves anything. Reconcile uncertain results before any retry.
