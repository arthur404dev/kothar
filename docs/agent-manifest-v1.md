# Agent manifest v1

The canonical record is `${XDG_CONFIG_HOME:-$HOME/.config}/kothar/agents/<id>/agent.json`; directory and immutable `id` must match. Adjacent prompt, context, skill, and extension resources are record-owned, relative, traversal-free, symlink-safe, bounded resources.

The exact closed top-level object is `version`, `id`, `profile`, `inbound`, `engine`, `models`, `behavior`, `tools`, `workspace`, `permissions`, and `runtime`. No command, executable path, version, revision, environment variable, or literal secret is accepted.

`inbound.name` is `buzz`. Its typed options are WSS `relay`, named `identity_credential`, `respond_to` (`nobody` by default, `allowlist`, or `anyone` with bounded 64-hex pubkeys), and bounded `heartbeat_seconds`. Upstream buzz-acp is fixed at commit `7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc` by the inbound registry.

`engine.name` is `pi`. Typed options force `project_trust: never` and explicitly select telemetry and update checks. Credentials are named references with `inherit`, `custom`, or `none`; custom covers every authenticated configured provider and none permits only unauthenticated providers. Pi is fixed at `0.82.1` by the engine registry. Models, provider support, tools, bundles, failover limits, and executable metadata are compiled capabilities.

The Draft 2020-12 schema closes every object and validates local shape, types, enums, patterns, bounds, and uniqueness. The canonical Go decoder additionally rejects duplicate keys, secret-bearing keys, NUL/control data, trailing JSON, unsafe paths, unsupported capabilities, incomplete credential coverage, invalid Buzz response policy, and overlapping mount targets. Callers must run both schema and Go validation.

The complete Atlas record is in `examples/agents/atlas`. Runtime is one systemd worker/process tree and network mode is exactly `full` or `none`.
