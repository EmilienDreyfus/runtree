# runtree

> Run multiple realities of your codebase.

`runtree` is a local-first CLI for running several worktrees of the same repository side by side, with isolated ports, logs, and editor/browser entrypoints.

It is built for teams working with:
- Git worktrees
- AI coding agents
- parallel feature branches
- fast local review loops

`runtree` is source-available and distributed as static binaries for macOS and Linux.

## Install

Install the latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/EmilienDreyfus/runtree/main/install.sh | sh
```

Install a specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/EmilienDreyfus/runtree/main/install.sh | sh -s -- --version v0.1.0
```

Install into a custom directory:

```bash
curl -fsSL https://raw.githubusercontent.com/EmilienDreyfus/runtree/main/install.sh | sh -s -- --install-dir /usr/local/bin
```

Requirements:
- macOS or Linux
- `git`
- `curl`
- a runnable application command that accepts a `{port}` placeholder

Verify the install:

```bash
runtree version
```

## Quickstart

Initialize `runtree` in an existing Git repository:

```bash
runtree init \
  --name goodays-api \
  --run-command "npm run dev -- --port {port}"
```

Import the current worktrees into `runtree` state:

```bash
runtree scan
```

List available instances:

```bash
runtree ls
```

Start an instance:

```bash
runtree up main
```

Open it in the browser:

```bash
runtree web main
```

Open its worktree in your preferred editor:

```bash
runtree code main
```

Follow logs:

```bash
runtree logs main --follow
```

Stop the runtime:

```bash
runtree down main
```

## What `runtree` manages

For each imported worktree, `runtree` tracks:
- a stable instance name
- an allocated local port
- runtime status and PID
- a dedicated log file
- a browser URL template
- an editor entrypoint

Persistent local state lives under `~/.runtree/`.

## Command Reference

Core commands:
- `runtree init`
- `runtree scan`
- `runtree ls`
- `runtree up <instance>`
- `runtree down <instance>`
- `runtree restart <instance>`
- `runtree logs <instance>`
- `runtree web <instance>`
- `runtree code <instance>`
- `runtree editor`
- `runtree version`

Version shortcuts:
- `runtree version`
- `runtree --version`

## Upgrade

Re-run the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/EmilienDreyfus/runtree/main/install.sh | sh
```

Or pin a newer tag explicitly:

```bash
curl -fsSL https://raw.githubusercontent.com/EmilienDreyfus/runtree/main/install.sh | sh -s -- --version v0.1.0
```

## Uninstall

If you used the default location:

```bash
rm -f ~/.local/bin/runtree
```

`runtree` keeps local state in `~/.runtree/`. Remove it separately only if you want to delete project metadata and logs.

## Build From Source

Install from source with Go:

```bash
go install github.com/EmilienDreyfus/runtree/cmd/runtree@latest
```

Or build locally from a clone:

```bash
mkdir -p dist && go build -o dist/runtree ./cmd/runtree
```

Run the test suite:

```bash
go test ./...
```

## Releases

Releases are published on [GitHub Releases](https://github.com/EmilienDreyfus/runtree/releases).

Each release ships:
- `darwin/amd64`
- `darwin/arm64`
- `linux/amd64`
- `linux/arm64`
- SHA-256 checksums

## Roadmap

Planned, not available yet:
- `runtree expose`
- stable public URLs for shared previews
- managed sharing infrastructure around `expose`

The future `expose` capability and stable URLs are intended to be offered as a managed service outside the scope of this public repository.

## Contributing

External contributions are welcome, but this repository is maintainer-reviewed and CLA-gated.

Before opening a pull request:
- read [CONTRIBUTING.md](CONTRIBUTING.md)
- read [CLA.md](CLA.md)
- confirm the PR checklist

## License

This repository is source-available under [Elastic License 2.0](LICENSE).

In practice:
- you can use `runtree` locally, including inside your company
- you can modify it and redistribute it under the license terms
- you cannot provide `runtree` as a competing hosted or managed service
