# Architecture decision record: modular bootstrap contracts

Status: accepted, 2026-07-28.

Kothar is one Go binary and owns the ACP endpoint. The fixed process tree for each agent is:

`systemd -> buzz-acp -> kothar serve acp -> Pi`

The data path is Buzz event -> pinned upstream buzz-acp -> ACP stdin/stdout -> generic Kothar ACP server adapter -> framework -> engine interface -> Pi implementation -> model/provider/tools. `pi-acp` 0.0.32 at commit `2f6e3c530819489bd09a84139b0b757df6895556` is historical compatibility reference only and is never deployed.

## Package boundaries

- `internal/inbound`: minimal static inbound/deployment adapter registry.
- `internal/inbound/buzz`: fixed upstream buzz-acp deployment executable and configuration metadata only; no Nostr implementation.
- `internal/acp`: ACP framing, lifecycle, sessions, streaming, cancellation, errors, and stdout discipline; no Buzz or Pi imports.
- `internal/framework`: normalized engine-independent orchestration, policy, types, and generic declared tool grants; no ACP wire, Buzz, or Pi imports.
- `internal/engine`: minimal engine contract and static registry.
- `internal/engine/pi`: Pi process/RPC/settings/capabilities boundary, including resolution and exposure of the reviewed absolute Buzz multicall CLI path in Pi's tool environment; no ACP imports.
- `internal/manifest`, future `records`, `credentials`, `deploy/systemd`, and current `xdg` each own only their control-plane responsibility.
- `cmd/kothar/internal/cli` is the composition root and may wire concrete implementations.

There is no daemon, second binary, dynamic SDK, Python runtime, Nostr reimplementation, credential material, or host mutation in this bootstrap. `pi-acp` is not deployed. Static registries—not manifests—own commands, versions, and capabilities.

## Filesystem and deployment

Records use `${XDG_CONFIG_HOME:-$HOME/.config}/kothar/agents/<id>/agent.json`; state uses `${XDG_STATE_HOME:-$HOME/.local/state}/kothar`; cache uses `${XDG_CACHE_HOME:-$HOME/.cache}/kothar`. `KOTHAR_CONFIG_DIR` is the absolute test/operator override. Root deployment uses `/etc/kothar`, `/var/lib/kothar`, and `/usr/local/libexec/kothar`. Secrets remain named references in a protected credential backend.

Each applied agent is one root-owned systemd unit and process tree. Network policy is exactly `full` or `none`. Apply must validate ownership, regular-file and symlink safety, stage atomically, preserve isolated Pi state, and roll back generated artifacts and receipt on activation failure. Records and credentials are never implicitly deleted.

## Reviewed pins

- upstream buzz-acp commit: `7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc`
- Pi: `0.82.1`

The reproduced wire, engine, failure, limitation, and single-owner matrices are in [compatibility/acp-pi-contracts.md](compatibility/acp-pi-contracts.md).
