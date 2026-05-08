# runtree

<p align="center">
  <img src="./assets/logo.png" alt="runtree logo" width="420">
</p>

> Make git worktrees actually usable.

`runtree` is a local-first CLI that lets you run multiple branches of the same project side by side without fighting your local setup.

Git worktrees are powerful. In practice, they are annoying:
- ports collide
- run commands break
- editor configs get weird
- logs get mixed up
- opening the right branch in the right browser tab is manual every time

`runtree` fixes that.

You install it once in a project, it scans your worktrees, asks how to run the app, remembers your editor, and gives you a clean workflow:

```bash
runtree up main
runtree up auth-refactor
runtree web auth-refactor
runtree code auth-refactor
runtree logs auth-refactor
```

Each worktree gets:
- its own port
- its own runtime
- its own logs
- its own browser entrypoint
- its own editor entrypoint

So you can actually work in parallel:
- one branch for a refactor
- one branch for a fix
- one branch being handled by an AI agent
- one branch being reviewed locally

No containers. No preview deployment. No manual local orchestration.

`runtree` is especially useful if you:
- dream about using Git worktrees without hating your local setup
- need multiple branches running at the same time
- delegate work to Codex, Cursor, Claude Code, or other coding agents
- want faster PM and QA feedback without waiting for preview environments

Today, `runtree` focuses on local orchestration.  
Soon, `runtree expose` will make a local branch shareable through a stable public URL for review and QA.

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

## Why This Exists

Modern development is no longer:

```text
1 developer = 1 branch = 1 running app
```

It is increasingly:

```text
1 developer = multiple branches = multiple agents = multiple running instances
```

That is where the default local tooling falls apart.

`runtree` is built for the moment where:
- you launch one agent on branch A
- you work yourself on branch B
- you want both apps running at the same time
- you do not want to waste time reconfiguring ports, terminals, editors, and logs

It works just as well for:
- a monolith with several parallel branches
- a repo with many long-lived feature worktrees
- a local stack where multiple services or libraries need to be launched independently

## Quickstart

Initialize `runtree` in an existing Git repository:

```bash
runtree init \
  --name newProject \
  --run-command "npm run dev -- --port {port}"
```

List available instances and import current worktrees:

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

## What It Feels Like

Without `runtree`, Git worktrees are technically available but awkward in practice.

With `runtree`:
- `runtree up <instance>` starts the right branch on the right port
- `runtree web <instance>` opens the right browser target
- `runtree code <instance>` opens the right worktree in your editor
- `runtree logs <instance>` gives you the logs for that branch only

That is the real value: making parallel local development usable, not just theoretically possible.

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

The future `expose` capability is the natural next step:
- run a branch locally
- share it instantly with a PM or teammate
- avoid waiting for a feature environment or preview deployment

Stable public URLs and the managed infrastructure behind `expose` are intended to be offered as a service outside the scope of this public repository.

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
