# Contributing to codeaf

Start from `dev` and open a pull request back to `dev`. Read [AGENTS.md](AGENTS.md)
for repository-wide engineering rules; it shares its contents with `CLAUDE.md`.

## Choose a starting point

| Work | Read first |
| --- | --- |
| Implement or fix a feature | [Developer architecture](docs/ARCHITECTURE.md), recent [change entries](docs/changes/README.md), and the relevant source tests |
| Explain product behavior | [Manual authoring](internal/manual/README.md) |
| Improve developer documentation | [Documentation ownership](docs/DOCUMENTATION.md) and [documentation map](docs/README.md) |
| Report a defect | [Defect template](.github/ISSUE_TEMPLATE/defect.md) with a reproducible example |

## Prepare a contribution

1. Create a contribution branch from current `dev`; follow the [branch rules](docs/rules/branching.md).
2. Use the Go version declared in [go.mod](go.mod). Run `make build` for `bin/codeaf`; build preparation is described in the [Makefile](Makefile).
3. Make a focused change with observable acceptance. Follow the testing, naming, manual and design rules in [AGENTS.md](AGENTS.md).
4. Update the matching documentation and add a [change entry](docs/rules/changelog.md).
5. Run the required [local checks](docs/rules/ci.md#local-verification), review your diff, and open the PR against `dev` with validation and honest limitations.

## Validate the affected behavior

Use `make test-focus PKGS=./internal/pkg RUN='^TestRegression$$'` while editing,
substituting the package and test you changed. Run `make test-quick`, then
`make pr-ready` before opening the PR. The latter selects touched packages from
the committed diff; commit Go/module edits before relying on that selection.
Manual changes also need the [manual checks](internal/manual/README.md#validate-a-manual-change).

A skipped live-provider or Docker test does not prove that path works. State what
ran, what skipped and what remains unverified. See [CI guidance](docs/rules/ci.md)
for the exact gate and failure diagnosis; this guide does not define another one.

## Review and release

Stage explicit paths and keep unrelated work out of the PR. Never push feature
work directly to `dev`, `staging` or `main`. Reviewers need the concrete problem,
resulting behavior and validation. Maintainers follow the [promotion rules](docs/rules/promotion.md)
for staging and release; opening a contribution does not authorize a merge.
