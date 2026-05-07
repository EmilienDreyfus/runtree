# Contributing to runtree

Thanks for contributing.

## Before you open a pull request

- Read [CLA.md](CLA.md) and confirm it in the PR template.
- Check whether an issue already exists for the problem or proposal.
- Keep changes scoped. Large rewrites without prior discussion are likely to be declined.

## Development setup

```bash
go test ./...
mkdir -p dist && go build -o dist/runtree ./cmd/runtree
```

For installer validation:

```bash
VERSION=v0.1.0 ./scripts/smoke-install.sh
```

## Contribution rules

- Preserve the local-first scope of the project.
- Avoid adding features that require hosted infrastructure in the core CLI.
- Keep the public CLI explicit and scriptable.
- Update docs when user-facing behavior changes.
- Add or update tests when behavior changes.

## Review policy

- All changes require maintainer review.
- CODEOWNERS is configured so the maintainer is requested automatically.
- A CLA acknowledgement is required before merge.
- The maintainer may close or decline contributions that do not fit the product direction.

## Good first contributions

- bug fixes with tests
- documentation improvements
- install and packaging hardening
- UX polish for existing commands
