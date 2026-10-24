# Contributing to codeaf

## The name

The product is `codeaf`, lowercase in prose, titles, help and release names.
`internal/namelaw` checks the spelling across source, scripts and documentation.
Historical records and testdata may preserve former names. Compatibility lines
that retain former identifiers must carry the `legacy-name` comment. Explain
former names only in a manual section whose heading contains “old name”.

## Repository map

- `internal/tui3`: the live chat surface, entered through `cmd/codeaf/chatv3.go`.
- `internal/session`: the chat engine, turn loop, tools and tasks.
- `internal/tui2`: shared components used by the current surface.
- `internal/head` and `internal/resident`: the separate resident surface.

Check which surface a change affects before editing. Chat and resident have
separate behavior and manual corpora.

## Branches and contributions

Branch from `dev` and open pull requests against `dev`. Keep branches short-lived.
Never push feature work directly to `dev`, `staging` or `main`, and never force-push
those branches. `staging` and `main` advance by fast-forward to tested history;
see [branching](docs/rules/branching.md) and [promotion](docs/rules/promotion.md).
Stage explicit paths rather than `git add .` or `git add -A`.

Each pull request carries a change entry in `docs/changes/unreleased/`:

```sh
make changelog-new PR=<number> KIND=<kind> SLUG=<slug>
make changelog-check
```

Read recent entries before relying on earlier behavior. Explain changed behavior
and obsolete assumptions in the entry; see [changelog rules](docs/rules/changelog.md).

## Build and test

Apply [Matt Pocock's engineering skills](https://github.com/mattpocock/skills)
when writing code: diagnose unexplained failures before changing code, use TDD
for fixes and features at agreed observable interfaces, and review the final
diff against both repository standards and the request. Pass these practices
to coding delegates. Claude can load them with
`claude plugin install mattpocock-skills@claude-plugins-official`.

Keep module interfaces small and concentrate related behavior behind them.
Prefer reusing an existing module over adding a pass-through abstraction.
Measure performance before and after; preserve behavior during cleanup.

Use `make build` for the runnable binary at `bin/codeaf`. Rebuild after merging.
Do not overwrite a running executable in place; use the installation/update path.

```sh
make build
make test-focus PKGS=./internal/pkg RUN='^TestRegression$$'
make test-quick
make pr-ready
```

Use focused tests while editing. Before pushing, run `make test-quick` and the
focused tests for your change. Before opening a pull request, `make pr-ready`
adds fresh tests for packages changed from `origin/dev` (or `BASE=<commit>`).
Commit Go and module edits first: the touched-package target tests the committed
diff. CI runs the same selector and blocks merging on the required `check` status.
See [CI rules](docs/rules/ci.md) for failure diagnosis.

Avoid full `internal/tui3` or `internal/session` runs as an editing loop. Complete
runs share a per-machine lock; use `make test-focus` while another suite is running.
Full checks belong to staging validation or an intentional full-tree check.
Use a 15-minute timeout per heavy test shard. `make test-report` writes fresh test
results and timings; `SHARDS=1` reproduces a serial run.

Live-provider tests skip when no provider credential is configured. Do not require
credentials for ordinary contributions. `make test-remote` uses containers and
needs no provider key; it skips when Docker is unavailable. The automation e2e
fixture uses a scripted endpoint. Build before tests that drive `bin/codeaf`.

`make demo-home` opens a disposable populated profile for inspecting the UI:

```sh
make demo-home DEMO_HOME=/tmp/codeaf-demo
make demo-home DEMO_HOME=/tmp/codeaf-demo KEEP=1
```

## Manual law

Update the manual in the same change as any feature, command, alias, tool, key,
default, limit or refusal. `internal/manual/chat/` is the chat's compiled manual;
`internal/manual/pages/` is the separate resident corpus. Also keep
`internal/session/prompts/system.md` consistent with tools actually available.

Use one `#` title per page and self-contained `##` sections under about 2,000
characters. Headings are the retrieval index: include the words people ask with.
Quote visible strings accurately, state limits, and remove obsolete denials when
a capability becomes available. When retrieval reaches the wrong page, fix the
page's headings rather than weakening the test. Keep resident vocabulary out of
the chat corpus.

Edit Markdown only; generated manual packs are ignored build artifacts.
Check both shipped and source paths with:

```sh
make test-packed-manual manual-gates
```

## Design and performance rules

Unknown or zero readings render as nothing. The live status row deliberately
keeps `$0.00` to avoid shifting; empty home panels keep a heading and a dim line
naming what appears there. Use plain user-facing task states: running, finishing,
done, incomplete or your call. Leave unavailable tools off the belt.

Resolve every icon through `internal/tui2/tokens` and the surface's glyph accessor;
never add a literal mark that bypasses terminal repertoire selection. Keep shared
values in one constant and derive schemas and text from it. Comments explain why
in full sentences; use capitals only for an explicit law.

Performance gates count work, bytes and allocations rather than elapsed time.
Keep measurements reproducible and update documented caps in the same commit as
changes to those caps. See [PERF.md](PERF.md).

## Defect reports

Use `.github/ISSUE_TEMPLATE/defect.md`. Supply a reproduction another contributor
can run, using a stub or fixture where possible. State acceptance through the real
entry point and name the exact visible string or receipt field to assert.
