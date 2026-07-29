# Kothar

A strict, modular Go framework for deploying Buzz-connected agents through ACP and Pi.

```sh
go build ./cmd/kothar
./kothar --help
./kothar serve acp --help
./kothar completion zsh
```

The bootstrap freezes CLI, manifest, package-boundary, filesystem, and deployment contracts without implementing runtime actions or mutating a host. See [`docs/architecture.md`](docs/architecture.md), [`docs/agent-manifest-v1.md`](docs/agent-manifest-v1.md), and the complete [`examples/agents/atlas`](examples/agents/atlas) record.

License: MIT.
