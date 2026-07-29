# ACP and Pi compatibility contract

Status: evidence baseline for the Kothar ACP server and Pi engine. `pi-acp` is reference-only and is not a runtime dependency.

## Pins and reproduced evidence

| Item | Exact reference | Reproduction |
|---|---|---|
| Buzz ACP client | block/buzz `7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc` | `git rev-parse HEAD`; source `crates/buzz-acp/src/acp.rs` |
| pi-acp | npm `0.0.32`, git `2f6e3c530819489bd09a84139b0b757df6895556` | tarball SHA-256 `0faee4e31d75e987166d17ebf73cd970d90315877dadd73a765d1b46f716bab6`; rebuilt and published `dist/index.js` both SHA-256 `fffbdc67ce361866082b0f2ad78d64de85bfac5e4c89a1b7662a6d2785502d4a`; 95/95 tests pass |
| Pi | installed `0.82.1` | resolved CLI `dist/cli.js` SHA-256 `af302f231437eaf6f37691bce4b34234fcb626bcb5eb3910d4fc3f6519bf78ca` |

The installed Pi equals the planned baseline. pi-acp does **not**: Buzz requests ACP protocol version 2, while pi-acp 0.0.32 returns version 1. Its startup also performs `npm view @earendil-works/pi-coding-agent version` (see `buildUpdateNotice`), and a disposable run created npm cache/log files. Those are disqualifying production properties. Kothar must launch reviewed absolute Pi and Buzz paths from its root-owned registry; never `npx`, a semver range, `latest`, or start-time package resolution. No later task may require pi-acp to be installed.

## Buzz-facing ACP wire contract (`internal/acp`)

Transport is UTF-8 JSON-RPC 2.0, exactly one JSON object plus LF per stdout record (NDJSON). Stderr is logs only. Empty/non-JSON stdout, banners, ANSI, or provider text on stdout are protocol violations. Bound a line before allocation (Buzz uses 10 MB), writes (Buzz uses 30 s), non-prompt requests (60 s), idle time and total turn time. Numeric client request IDs are monotonically increasing; replies match IDs. Agent requests may use string or numeric IDs.

| Lifecycle / behavior | Exact wire or rule |
|---|---|
| initialize | request `initialize` with `protocolVersion:2`, `clientInfo:{name:"buzz-acp",version}`, terminal-auth client capability; return deterministic selected version, agent info and honest capabilities. Kothar targets Buzz's requested v2 rather than copying pi-acp's v1 downgrade. |
| create | `session/new` with absolute `cwd`, required `mcpServers` array, optional `systemPrompt`, optional `_meta.sessionTitle`; return non-empty `sessionId`. Reject relative cwd as invalid params. |
| prompt | `session/prompt` with `sessionId` and ordered `prompt:[{type:"text",text}, ...]`. Stream notifications before the matching response. |
| stream | `session/update` notifications. Required normalized forms are `agent_message_chunk`, `agent_thought_chunk`, `tool_call`, `tool_call_update`, and useful session/config/command updates. Preserve order and tool-call ID; statuses never regress. |
| finish | response result has exactly a recognized ACP `stopReason`: `end_turn`, `cancelled`, `max_tokens`, `max_turn_requests`, or `refusal`. Missing/unknown is a protocol error. |
| cancel | `session/cancel` is a notification (no `id`). Resolve/drain the outstanding prompt as `cancelled`; if a permission request is pending, answer it with cancelled first. Cancellation has a short hard grace then kills/reaps the Pi process group; it cannot wait forever. Unknown session is an idempotent no-op. |
| permissions | `session/request_permission` is an agent request and must receive one response. Framework policy chooses an advertised option; no blanket Buzz-derived approval is hidden in ACP. Unknown agent requests receive JSON-RPC `-32601`. |
| errors | Preserve JSON-RPC numeric code/message. Use `-32602` for malformed/unknown session input and `-32603` for internal launch/runtime failure; distinguish framing/parse, process exit, timeout, cancellation timeout, engine/auth/provider, and policy errors internally. Never convert a failure into assistant text. |
| persistence | ACP maps opaque session IDs only. Load/list/delete are advertised only when implemented; Pi session files remain engine-owned. Multiple prompts for one session serialize. |

`testdata/buzz-acp-lifecycle.ndjson` is a redacted framing example, not evidence of an executable Kothar runtime. It covers v2, session creation, ordered text streaming, terminal stop reason, and notification-shaped cancellation. The baseline implements `initialize`, `session/new`, `session/prompt`, `session/update`, and `session/cancel`; it honestly advertises `loadSession:false`. Session load/list/delete remain future methods and must stay absent or false until implemented.

## Pi engine contract (`internal/engine/pi`)

Launch the reviewed absolute Pi CLI directly as an argv vector, never through a shell: `pi --mode rpc --no-themes --session-dir <isolated-dir>` plus reviewed model, tools, system-prompt and telemetry/update flags. Set an isolated Unix `HOME`, `PI_CODING_AGENT_DIR`, session directory and cwd. Stdout is strict LF-delimited JSON only; stderr is diagnostic. Pi 0.82.1 documentation explicitly warns that generic Node `readline` is not compliant because U+2028/U+2029 are valid JSON string content; the Go reader must split only on LF and strip one trailing CR.

| Pi behavior | Engine requirement |
|---|---|
| handshake/state | Correlated `get_state` and `get_available_models`; require successful responses. Capture opaque session ID/file only under the isolated root. No models/auth becomes an auth/config failure, not an empty healthy capability. |
| prompt | Send `{type:"prompt",id,message,images?}`. Its response means accepted, not completed. Stream `message_update` deltas and tool events; finish only on `agent_settled` (0.82.1's strongest full-run boundary), not `turn_end` and not merely the acceptance response. |
| cancel | Send correlated `abort`; bound acknowledgement and `agent_settled`, then TERM/KILL/reap. The real isolated active-turn probe reached `agent_settled` and then acknowledged abort in 12 ms. Pi emitted no separate `cancelled` event; the actual cancellation boundary was assistant `stopReason:"aborted"` followed by `agent_end`, `agent_settled`, and the successful correlated abort response. |
| persistence | `--session-dir` is mandatory. The disposable probe reported `get_state.sessionFile` only as `<ISOLATED_DIR>/sessions/<session>.jsonl`; the credential source was referenced without copying and was not recorded. Ordinary reapply must preserve engine-owned session files. |
| system prompt | Pi supports launch-time `--system-prompt` and repeatable `--append-system-prompt`; use argv/file content assembled by the Pi owner before launch. ACP `systemPrompt` is normalized by the framework but pi-acp 0.0.32 ignores it. Kothar must not silently ignore it. |
| tools | Pi's reviewed `--tools`/`--exclude-tools` flags select built-in/extension/custom tools. Normalize text/thought, tool start/update/end, location, result/error and extension UI requests. Unsupported interactive UI is cancelled explicitly. |
| errors | Classify spawn `ENOENT`/`EACCES`, malformed/non-JSON or oversized stdout, early exit/signal, correlated `success:false`, auth/no-model, provider failure, timeout, and cancellation escalation. Redact paths/provider/session identifiers at trust boundaries. |

The primary sanitized evidence is `testdata/pi-rpc-probe.json`, `testdata/pi-rpc-lifecycle.ndjson`, and `testdata/pi-rpc-active-cancel.ndjson`. A real disposable Pi 0.82.1 process used placeholder-recorded isolated `HOME`, `PI_CODING_AGENT_DIR`, and session paths, referenced the existing auth file without copying or recording it, disabled tools and project resources, and requested `KOTHAR_OK`. The prompt command was accepted; real deltas were `K`, `OTH`, `AR`, `_OK`; ordered events continued through `agent_end` and `agent_settled`; stdin EOF exited 0; stdout was NDJSON-only and stderr was empty. The active prompt was aborted immediately after `agent_start`; Pi emitted `stopReason:"aborted"`, `agent_end`, `agent_settled`, then acknowledged abort successfully in 12 ms and exited 0. No explicit `cancelled` event was emitted. Provider request/account IDs, real session IDs, credentials, tokens, and host paths were removed rather than copied into evidence.

## Framework normalization (`internal/framework`)

Framework request: opaque session ID, absolute cwd, ordered content blocks, effective system prompt, requested model/tool policy, cancellation context and optional declared MCP servers. Framework events: text delta, thought delta, tool start/update/end, permission request, terminal completion. Framework errors have one stable class (`invalid_request`, `unknown_session`, `auth`, `policy`, `engine_unavailable`, `provider`, `timeout`, `cancel_timeout`, `protocol`, `internal`) plus safe message/cause; transports map them but do not reinterpret them. One session allows one active turn; additional work follows the manifest's queue/steer policy.

`mcpServers` limitation is explicit: Pi 0.82.1 RPC has no MCP-server injection and pi-acp merely stores the array. Kothar must reject non-empty ACP `mcpServers` for the Pi engine as unsupported unless a later reviewed Pi capability implements them; never claim support or silently discard them.

## Direct Buzz CLI decision

Do not route Buzz tools through ACP `mcpServers`. Upstream Buzz builds one stdio MCP entry from `BUZZ_ACP_MCP_COMMAND` and injects relay identity into its environment, but the Pi boundary cannot consume it. Ownership is singular: `internal/inbound/buzz` owns the buzz-acp deployment executable and configuration metadata; `internal/framework` owns only the generic declared tool grant; `internal/engine/pi` resolves the root-registry-reviewed absolute Buzz multicall CLI path and exposes it in Pi's tool environment. Manifest data never chooses either executable, and relay identity never enters argv, prompts, or the repository.

## Single-owner matrix

| Requirement | Sole owner |
|---|---|
| JSON-RPC/NDJSON, v2 negotiation, method validation, ACP error mapping, session ID map, cancellation drain, stdout discipline | `internal/acp` |
| Effective prompt/system prompt, queue/steer policy, normalized request/event/error types, permission/capability policy, generic declared tool grant | `internal/framework` |
| Pi argv/env/isolation, RPC correlation, session files, stream/tool translation, abort/escalation, provider/auth failures, reviewed absolute Buzz multicall CLI resolution/exposure | `internal/engine/pi` |
| buzz-acp deployment executable and relay-side configuration metadata | `internal/inbound/buzz` |

No behavior is shared ambiguously: ACP translates wire to framework; framework decides policy; Pi translates framework to Pi RPC.

## Reproduction commands

```text
git -C <buzz> checkout 7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc
npm pack pi-acp@0.0.32 --ignore-scripts
npm ci --ignore-scripts && npm test && npm run build   # exact pi-acp checkout
pi --version
sha256sum <tarball> <published-dist> <rebuilt-dist> <resolved-pi-cli>
go test ./internal/acp
go test ./... && go build ./cmd/kothar && go vet ./...
```

The npm commands are reference reproduction only, in a disposable directory. They are not installation or production launch instructions.
