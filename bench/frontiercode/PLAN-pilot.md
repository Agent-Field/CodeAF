# PLAN-pilot.md — the first FrontierCode-style pilot (D4)

*A pilot plan for the owner to approve. Nothing here has run yet beyond the
D2 slice. The brief's cap: under $20 total on pilots; any campaign beyond the
pilot waits for approval (brief §5, §10).*

## What a pilot must answer

One question, the one the whole rig exists for: **does senior-dev produce
work a maintainer would merge more often than a plain loop agent on the same
model?** DeepSWE answered "do hidden tests pass"; FrontierCode's conditions
add test quality, scope, style and conventions — exactly where a
harness-tuned-for-hidden-tests may not transfer. The pilot measures that
transfer on a small, honest task set.

## Task set: 8 named tasks

The brief asks for 5–10 candidate tasks with draft rubrics. Eight are named
here: the calibration fixture plus seven mined candidates (candidates 2–8,
numbered after the fixture). Each came through the sourcing sketch below;
each entry records the repository, the merged PR, the base commit (the PR
head's parent, the commit the tree is checked out at), a one-paragraph task
sketch, a note on what is verified versus merely expected, and a draft
rubric.

| # | task id | repository | PR | language | merged |
|---|---|---|---|---|---|
| 1 | `jsonschema-log-warning` | sourcemeta/jsonschema | #521 | C++ | built, calibrated |
| 2 | `shell-pipe-empty-command` | goreleaser/goreleaser | [#5929](https://github.com/goreleaser/goreleaser/pull/5929) | Go | 2025-07-27 |
| 3 | `subcommand-single-char-alias-leak` | alecthomas/kong | [#637](https://github.com/alecthomas/kong/pull/637) | Go | 2026-08-05 |
| 4 | `completions-args-mutation` | spf13/cobra | [#2356](https://github.com/spf13/cobra/pull/2356) | Go | 2026-04-24 |
| 5 | `dns-extra-records-lowercase` | juanfont/headscale | [#3366](https://github.com/juanfont/headscale/pull/3366) | Go | 2026-09-09 |
| 6 | `progress-state-string-out-of-range` | charmbracelet/bubbletea | [#1748](https://github.com/charmbracelet/bubbletea/pull/1748) | Go | 2026-08-19 |
| 7 | `mangen-hidden-positionals` | clap-rs/clap | [#6482](https://github.com/clap-rs/clap/pull/6482) | Rust | 2026-08-12 |
| 8 | `conflicted-files-refname-crash` | pre-commit/pre-commit | [#3425](https://github.com/pre-commit/pre-commit/pull/3425) | Python | 2025-03-15 |

**The draft rubrics live inline in this plan, not as skeleton files under
`tasks/`.** A directory under `tasks/` is a BUILT task to this rig's reader
(`lib.sh` walks it; `task.toml` names images it expects); a half directory
with only a rubric would be read as a broken task. A draft that lives here is
plainly a draft. When a candidate is built its rubric moves into its own
directory beside a `task.toml`.

**On the training-cutoff rule** (brief §4.6: PRs merged after the candidate
models' cutoffs): the merge dates above are verified from GitHub. The pinned
pilot model (`deepseek/deepseek-v4-flash-0731`) publishes no cutoff date —
its `-0731` suffix is a build date, not a cutoff — so no candidate can be
*proven* unseen. The four candidates merged after that build date
(3, 5, 6, 7) are the safe half of the set; the three merged earlier
(2, 4, 8 — 2025-07-27, 2026-04-24, 2025-03-15) are kept for the language and
tooling spread the brief asks for, with the risk recorded here rather than
silently accepted.

### Candidate 2 — shell-pipe-empty-command (goreleaser/goreleaser)

- **PR**: goreleaser/goreleaser#5929, merged 2025-07-27. **Base commit**:
  `21eefb16cfc5346f9cbe8fb0513ee1840353c324` (the PR head's parent; single-
  commit PR). License MIT.
- **Task sketch**: goreleaser's shell-pipe helper (`internal/shell`) runs
  each pipe entry's command by indexing `command[0]` without a length check,
  so a configuration whose pipe has an empty command list panics the whole
  release pipeline instead of skipping the entry. The task: make the runner
  skip an empty command without crashing and without returning an error,
  leaving every other behaviour of the runner untouched. Small, single-file
  fix in a Go tree with strong conventions (golangci-lint config in tree).
- **Verified vs expected**: VERIFIED over the GitHub API and the PR's diff
  (2026-09-28): the PR is merged; the base SHA is served by the PR commits
  endpoint; the diff fixes exactly `internal/shell/shell.go` (adds the
  length guard) and adds an "empty command" regression test to
  `internal/shell/shell_test.go` whose body indexes the same empty list.
  EXPECTED, not verified by building: the tree compiles at base (a released
  goreleaser tree); that regression test panics at base and passes with the
  fix (its assertion shape makes that direct); the rest of the suite is
  green at base.
- **Draft rubric** (rubric-schema-v1; ≥2 blockers, both writable from the
  brief's behaviour alone — a pipe entry with no command is skipped, real
  commands behave as before — without quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "empty-command-skipped-not-crashed"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["internal/shell/shell.go"]
question = """The brief says a pipe entry whose command list is EMPTY must be
skipped without crashing and without failing the pipeline. Read the diff hunk
for this file. Does the change handle the empty case where the runner would
previously index into the empty list (the panic) by skipping it — silently or
with a warning — rather than panicking or returning an error? A change that
still panics, converts the skip into an error, or guards only part of the
run path fails."""

[[criterion]]
id = "run-contract-unchanged"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["internal/shell/shell.go"]
question = """Skipping an empty command must not change what the runner does
with commands that are not empty. Read the diff hunk. Does a failing
non-empty command still produce its error, and is the env/output handling
untouched? A change that swallows failures or silences output for non-empty
commands too fails."""

[[criterion]]
id = "regression-tests-pass"
kind = "classical"
blocker = false
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "go test ./internal/shell/ -run TestRunCommand"

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "go test ./internal/shell/ -run TestRunCommand"
revert_paths = ["internal/shell"]

[[criterion]]
id = "clean-build"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go build ./..."

[[criterion]]
id = "shell-package-tests-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go test ./internal/shell/"

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 4
max_changed_lines = 60
allowed_paths = ["internal/"]
forbidden_paths = [".github/", "www/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "go test ./internal/shell/ -run TestRunCommand"
```

### Candidate 3 — subcommand-single-char-alias-leak (alecthomas/kong)

- **PR**: alecthomas/kong#637, merged 2026-08-05. **Base commit**:
  `a5c96268805994e92822673b24553196694fdda1` (the PR head's parent;
  single-commit PR). License MIT.
- **Task sketch**: kong tracks which flags a sibling subcommand has already
  claimed so the same flag name may recur across subcommands. The bookkeeping
  deletes every alias under its LONG form (`--name`) only, so two sibling
  subcommands that each declare a single-character alias (`aliases:"f"`)
  appear to conflict: parsing `sub2 -f hello` errors as a duplicate alias
  even though the subcommands are separate. The task: make the bookkeeping
  delete an alias under the form it is actually invoked as — one character
  under `-x`, longer under `--x` — so sibling subcommands may reuse
  single-character aliases while duplicate detection still works.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  changes exactly `build.go`'s alias-deletion loop and adds
  `TestSubCommandSingleCharAliases` to `kong_test.go`, whose body parses
  `sub2 -f hello` across two sibling subcommands. EXPECTED, not built: the
  test errors at base (the duplicate-alias error the loop produces) and
  passes with the fix; `go build ./...` clean at base; rest of suite green.
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  sibling subcommands may reuse a one-character alias, and duplicate
  detection for long aliases still works — without quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "single-char-alias-parses"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "go test . -run TestSubCommandSingleCharAliases"

[[criterion]]
id = "alias-bookkeeping-by-invoked-form"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["build.go"]
question = """The bookkeeping that lets sibling subcommands share flag names
must delete an alias under the form it is invoked as: a one-character alias
under its short form (-x), a longer alias under its long form (--x). Read the
diff hunk. Does the change delete exactly the alias's own spelling, or does
it instead clear whole records, drop duplicate detection for long aliases,
or treat long aliases as short — any of which may stop THIS error while
changing the conflict contract for every other flag? A change that widens
the deletion beyond the alias's own form fails."""

[[criterion]]
id = "clean-build"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go build ./..."

[[criterion]]
id = "package-tests-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go test ."

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "go test . -run TestSubCommandSingleCharAliases"
revert_paths = ["build.go"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 3
max_changed_lines = 40
allowed_paths = ["./"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "go test . -run TestSubCommandSingleCharAliases"
```

### Candidate 4 — completions-args-mutation (spf13/cobra)

- **PR**: spf13/cobra#2356, merged 2026-04-24. **Base commit**:
  `61968e893eee2f27696c2fbc8e34fa5c4afaf7c4` (the PR's FIRST commit's parent
  — the PR has three commits; the head's parent is its own earlier commit).
  License Apache-2.0.
- **Task sketch**: cobra's completion machinery slices the caller's argument
  list (`args[:len(args)-1]`, ultimately a subslice of `os.Args`) and later
  appends `"--"` to that subslice. When the backing array has spare capacity
  the append writes into it IN PLACE, silently mutating `os.Args` — the
  program's own view of its arguments — for any program with
  `TraverseChildren`. The task: the arguments handed through completion
  lookup must be a copy the machinery owns, so no later append can write
  into the caller's array. One-file fix; the regression test asserts
  `os.Args` is byte-identical after a completion run.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  changes exactly `completions.go` (the slice becomes a `make`+`copy`) and
  adds `TestCompletionDoesNotMutateOsArgs` to `completions_test.go`, which
  overrides `os.Args` directly and asserts no mutation. EXPECTED, not built:
  that test fails at base (the append writes `"--"` into `os.Args[2]`) and
  passes with the fix — the test's own comments spell the mechanism; the
  tree compiles at base; rest of suite green.
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  completion must not mutate the caller's arguments — without quoting the
  PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "os-args-not-mutated"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "go test . -run TestCompletionDoesNotMutateOsArgs"

[[criterion]]
id = "trimmed-args-are-owned-copy"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["completions.go"]
question = """The bug is aliasing: a subslice of the caller's arguments is
later appended to, and an append with spare capacity writes into the caller's
backing array in place. Read the diff hunk. Is the slice handed through
completion lookup a FRESH COPY the machinery owns (an explicit copy of the
trimmed arguments), so that no later append anywhere in the path can write
into the caller's array? A change that only special-cases the one append
site, re-slices at a different length, or copies only when capacity happens
to be exactly full leaves the shape — the next append corrupts the caller
again — and fails."""

[[criterion]]
id = "clean-build"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go build ./..."

[[criterion]]
id = "completion-tests-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go test . -run 'TestCompletion'"

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "go test . -run TestCompletionDoesNotMutateOsArgs"
revert_paths = ["completions.go"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 3
max_changed_lines = 40
allowed_paths = ["./"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "go test . -run TestCompletionDoesNotMutateOsArgs"
```

### Candidate 5 — dns-extra-records-lowercase (juanfont/headscale)

- **PR**: juanfont/headscale#3366, merged 2026-09-09. **Base commit**:
  `f3f6c982209b08fe2734c38832edf74468221069` (the PR head's parent;
  single-commit PR). License BSD-3-Clause.
- **Task sketch**: headscale's MagicDNS clients match extra records by exact
  name, but nothing lowercases record names on the way into the tailcfg DNS
  config, so a user-configured `Printer.fritz.box` never resolves against
  `printer.fritz.box`. There are TWO paths that place extra records into
  that config — the static conversion of the configured DNS block, and a
  runtime setter used when records are updated live. The task: normalize
  record NAMES to lowercase on every path that produces extra records,
  leaving record types and values untouched.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  normalizes both named paths in `hscontrol/types/config.go` via one helper
  and adds `TestExtraRecordsAreLowercased` to `hscontrol/types/
  config_test.go` with mixed-case names. EXPECTED, not built: that test
  fails at base (mixed-case names pass through unchanged) and passes with
  the fix; the package compiles at base; rest of suite green. The PR also
  touches `CHANGELOG.md` — the built fixture's reference patch would take
  the code and test files, not the changelog.
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  mixed-case extra records must resolve wherever they are set, and nothing
  but the name may change — without quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "mixed-case-records-normalized"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "go test ./hscontrol/types/ -run TestExtraRecordsAreLowercased"

[[criterion]]
id = "every-entry-point-lowercases"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["hscontrol/types/config.go"]
question = """The brief says extra records reach the tailcfg DNS config from
MORE THAN ONE path — the conversion of the configured DNS block, and the
runtime setter used when records are updated while the server is up. Read
the diff hunk. Is the name normalization applied on EVERY path that writes
extra records into that config, or only on one of them? A fix that
lowercases in one place and leaves the other path passing mixed-case names
through is exactly the partial fix this criterion exists to catch."""

[[criterion]]
id = "only-names-normalized"
kind = "prompt"
blocker = false
weight = 1
[criterion.prompt]
paths = ["hscontrol/types/config.go"]
question = """Only the record NAME may be normalized. Read the diff hunk. Do
record types (A, AAAA, CNAME…) and record values pass through untouched, and
is the empty-record list handled without allocating or erroring? A change
that lowercases values, mangles types, or errors on an empty list fails."""

[[criterion]]
id = "clean-build"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go build ./..."

[[criterion]]
id = "types-package-tests-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go test ./hscontrol/types/"

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "go test ./hscontrol/types/ -run TestExtraRecordsAreLowercased"
revert_paths = ["hscontrol/types"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 4
max_changed_lines = 80
allowed_paths = ["hscontrol/"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "go test ./hscontrol/types/ -run TestExtraRecordsAreLowercased"
```

### Candidate 6 — progress-state-string-out-of-range (charmbracelet/bubbletea)

- **PR**: charmbracelet/bubbletea#1748, merged 2026-08-19. **Base commit**:
  `fc707bb7ea0161405bb6c653ec93f6a9c6a72fe1` (the PR's FIRST commit's parent
  — three commits; the head's parent is its own earlier commit). License MIT.
- **Task sketch**: bubbletea's progress-bar state type implements `String()`
  by indexing an anonymous five-string array, so any value outside the five
  known states — reachable directly, because the state field is exported —
  panics with index-out-of-range. The task: make `String()` total — return a
  human-readable name for every known state and a stable placeholder for
  anything else — without changing any of the known spellings.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  replaces the array index with a switch plus a default in `tea.go` and adds
  `TestProgressBarStateStringOutOfRange` to `tea_test.go` (values -1, 5, 100
  must return the placeholder; the five known states must not).
  EXPECTED, not built: that test panics at base (the index) and passes with
  the fix; `go build ./...` clean at base; rest of suite green.
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  out-of-range must not panic and known names must survive — without
  quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "out-of-range-returns-placeholder"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "go test . -run TestProgressBarStateStringOutOfRange"

[[criterion]]
id = "known-names-unchanged"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["tea.go"]
question = """Making the state name total must not change what the KNOWN
states return. Read the diff hunk. Does every previously-known state still
return exactly its former name — same words, same spelling — and does the
placeholder apply only to values outside the known range? A rewrite that
also respells a known name, reorders the mapping, or returns the placeholder
for a known state breaks every caller that matches on these strings and
fails."""

[[criterion]]
id = "clean-build"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go build ./..."

[[criterion]]
id = "package-tests-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "go test ."

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "go test . -run TestProgressBarStateStringOutOfRange"
revert_paths = ["tea.go"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 3
max_changed_lines = 50
allowed_paths = ["./"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "go test . -run TestProgressBarStateStringOutOfRange"
```

### Candidate 7 — mangen-hidden-positionals (clap-rs/clap, Rust)

- **PR**: clap-rs/clap#6482, merged 2026-08-12. **Base commit**:
  `8e50ec891e30cd2af4f9a62a405a5b7ee31aac4f` (the PR's FIRST commit's parent
  — two commits; the first commit is the test, the second the fix; the head's
  parent is the fix commit's own predecessor). License Apache-2.0.
- **Task sketch**: clap's man-page generator filters hidden OPTIONS out of
  the SYNOPSIS but renders every POSITIONAL, hidden or not, so an
  application's internal positional leaks into its published man page. The
  task: hidden positionals must be filtered from the SYNOPSIS exactly as
  hidden options already are, leaving the SYNOPSIS of a command with no
  hidden arguments byte-identical. The PR is test-first: the snapshot file
  and the test land before the one-line filter.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  adds `.filter(|arg| !arg.is_hide_set())` to the positional loop in
  `clap_mangen/src/render.rs`, plus the snapshot
  `clap_mangen/tests/snapshots/hidden_positional.bash.roff`, the command
  builder and the `hidden_positionals` test. EXPECTED, not built: with the
  test-side patch applied at base the snapshot test fails (the hidden
  positional still rendered) and passes with the fix; `cargo build` clean at
  base; rest of the suite green. Rust toolchain pinned in the built task's
  `task.toml` (stable at the fixture build date).
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  hidden positionals disappear from the SYNOPSIS, visible content is
  untouched — without quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "hidden-positional-absent-from-synopsis"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's snapshot + test, test-side
run = "cargo test -p clap_mangen hidden_positionals"

[[criterion]]
id = "visible-content-unchanged"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["clap_mangen/src/render.rs"]
question = """The SYNOPSIS of a command with NO hidden arguments must be
unchanged, and visible positionals must still render. Read the diff hunk.
Does the filter apply to the positional loop only, leaving the option and
flag rendering and the argument-group handling untouched? A change that
drops visible positionals too, hides everything after the first hidden arg,
or alters how groups are skipped is a wider change than the brief asks for
and fails."""

[[criterion]]
id = "mangen-suite-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "cargo test -p clap_mangen"

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "cargo test -p clap_mangen hidden_positionals"
revert_paths = ["clap_mangen/src"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 5
max_changed_lines = 60
allowed_paths = ["clap_mangen/"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "cargo test -p clap_mangen hidden_positionals"
```

### Candidate 8 — conflicted-files-refname-crash (pre-commit/pre-commit, Python)

- **PR**: pre-commit/pre-commit#3425, merged 2025-03-15. **Base commit**:
  `7b88c63ae691cb243c3137bce8fb870523e0a884` (the PR head's parent;
  single-commit PR). License MIT.
- **Task sketch**: pre-commit enumerates a merge's conflicted files by
  running `git diff --name-only -m <tree> HEAD MERGE_HEAD` on the user's
  worktree. If the worktree happens to contain a tracked FILE named `HEAD`
  (or `MERGE_HEAD`), git reads the refname ambiguously and the command
  crashes — `error: ambiguous argument 'HEAD'` — taking pre-commit down at
  the worst moment (mid-merge). The task: the enumeration must not crash
  when a tracked file's name collides with a refname, and must still return
  the complete set: merge-conflict files AND merge-diff files. One-line fix
  (a `--` pathspec separator) in a shell-heavy Python tool whose own test
  suite is built around driving real git in temp repositories.
- **Verified vs expected**: VERIFIED (2026-09-28) over the GitHub API and
  the PR's diff: merged; base SHA served by the commits endpoint; the diff
  appends `--` to the one git invocation in `pre_commit/git.py` and adds
  `test_get_conflicted_files_with_file_named_head` to `tests/git_test.py`,
  which creates a tracked file named `HEAD` inside the suite's existing
  merge-conflict fixture. EXPECTED, not built: that test errors at base (the
  ambiguous-argument crash) and passes with the fix; the suite passes at
  base; `pip install -e .` prewarmed in the built task image.
- **Draft rubric** (≥2 blockers; both derivable from the brief's behaviour —
  a refname collision must not crash, and the returned set must stay
  complete — without quoting the PR):

```toml
schema_version = 1
judge_model = "openrouter/anthropic/claude-sonnet-4.5"
judge_prompt_version = "fc-judge-1"

[[criterion]]
id = "refname-collision-no-crash"
kind = "classical"
blocker = true
weight = 1
[criterion.classical]
overlay = "/solution/test-overlay.patch"  # the PR's regression test, test-side
run = "python -m pytest tests/git_test.py -k conflicted_files"

[[criterion]]
id = "conflict-set-still-complete"
kind = "prompt"
blocker = true
weight = 1
[criterion.prompt]
paths = ["pre_commit/git.py"]
question = """The brief says the enumeration must return BOTH kinds of
files: the merge-conflict filenames and the merge-diff filenames, nothing
added, nothing lost. Read the diff hunk. Does the fix separate the refnames
from pathspecs without narrowing the diff — an empty-but-successful result
would be silent data loss, worse than the crash it replaced? A change that
silences the ambiguity by moving a separator to the wrong position, dropping
one of the two diff modes, or catching the error and returning a partial set
fails."""

[[criterion]]
id = "git-test-suite-pass"
kind = "command"
blocker = false
weight = 1
[criterion.command]
command = "python -m pytest tests/git_test.py"

[[criterion]]
id = "reverse-tests-fail"
kind = "reverse-classical"
blocker = false
weight = 1
[criterion.reverse_classical]
run = "python -m pytest tests/git_test.py -k conflicted_files"
revert_paths = ["pre_commit"]

[[criterion]]
id = "scope-focused"
kind = "scope"
blocker = false
weight = 1
[criterion.scope]
max_files = 3
max_changed_lines = 20
allowed_paths = ["pre_commit/", "tests/"]
forbidden_paths = [".github/"]

[[criterion]]
id = "adapted-tests-pass"
kind = "adaptive-classical"
blocker = false
weight = 1
[criterion.adaptive_classical]
overlay = "/solution/test-overlay.patch"
run = "python -m pytest tests/git_test.py -k conflicted_files"
```

### Considered and rejected

- **restic/restic#22007** (Swift backend password redacted from debug logs,
  merged 2026-08-29): VERIFIED merged, base SHA served by the commits
  endpoint, and the diff read. REJECTED: the PR adds no test of any kind —
  the pipeline's test-failure expectation (fails at base, passes with the
  PR) cannot be justified from the PR's diff or CI; the reference tests
  would be entirely ours. That is allowed but is the weaker evidence shape;
  held back rather than padded into the list. Revisit if the pilot needs an
  eighth task and a person is willing to author the reference tests.
- **alecthomas/kong#626** (counter panics on an explicit `--flag=N` against
  a uint or float field, merged 2026-07-19): VERIFIED merged and the diff
  read — a genuine fix with a regression test. NOT TAKEN only because kong
  is already taken by #637 and the plan runs one task per repository. First
  spare if a candidate above fails verification at fixture-build time.
- **charmbracelet/bubbletea#1806** (signal goroutine deadlock on shutdown,
  merged 2026-09-28): seen in search only, diff NOT verified. Deadlock and
  race fixes are poor grader material at draft time — a regression test
  hangs at base rather than failing, which turns the base-failure
  expectation into a timeout — so it was not pursued for v1.

### Sourcing sketch (the pipeline the candidates above came through; brief §4.6)

1. **Find merged PRs** in repositories with real linters and conventions
   (clang-format/ESLint/ruff configs present in the tree), merged AFTER the
   candidate models' training cutoffs, that touch code rather than docs or
   CI. Source of candidates: the repo's own git log on a full clone,
   filtered by files-touched and size. (In practice the seven candidates
   above were found by GitHub issue search over each repository's merged
   bug-fix PRs — `label:bug` or "fix" in the title, dated, then read through
   the PR's `.diff` — and every load-bearing claim re-checked against the
   API: merged state, the first commit's parent for the base SHA, and the
   diff's test-side shape for the base-failure expectation. The clone-based
   route stays in the pipeline for the next batch.)
2. **Write the brief** in the four FrontierCode sections, guideline text
   taken from the repository's own CONTRIBUTING files and CI config —
   never invented.
3. **Draft the rubric** from the reference PR: 6–12 criteria, kinds chosen
   from the six, blockers marked where the PR would not have merged without
   it. Have a model draft them, then a person review every one — Cognition's
   own account says their rubrics needed adversarial QC (75 overly strict
   blockers demoted in their 1.1 audit); ours will need the same scrutiny.
4. **Build the fixture**: environment image (pinned base commit, hygiene
   assertions), verifier image (reference patch + overlay + controls), gold
   control MUST score 1.00 and the negative control (where one can be
   generated) MUST score 0.0 with its blockers failed before the task is
   usable.
5. **Record the split**: `tasks/dev.txt`, `tasks/heldout.txt`, committed.

Steps 1 and 3 are DONE for the seven candidates above (mined, verified,
rubrics drafted); steps 2, 4 and 5 remain per task — the fixture build
(environment + verifier images, gold and negative controls) is still an
afternoon of hand work each, and a candidate's draft rubric may change when
that work touches the real tree. Nothing is built yet beyond the fixture.

### The dev/held-out split (drafted; files written when the tasks are built)

Eight tasks, split by language so both halves cover the spread:

| half | tasks |
|---|---|
| dev (the pilot tunes here) | `jsonschema-log-warning` (C++), `subcommand-single-char-alias-leak` (Go), `completions-args-mutation` (Go), `mangen-hidden-positionals` (Rust) |
| held-out (nobody tunes on it) | `shell-pipe-empty-command` (Go), `dns-extra-records-lowercase` (Go), `progress-state-string-out-of-range` (Go), `conflicted-files-refname-crash` (Python) |

The fixture anchors the dev half — it is the task the grader was calibrated
on. Five of the eight tasks are Go, so neither half can escape that; the
split still puts the only Python task in the held-out half and the only Rust
task in the dev half. The held-out half carries two of the three
pre-build-date merges (2, 8), so the transfer check leans on the riskier
candidates — deliberate: the dev half is where the headline is scored and it
should hold the safest set.

## Arms and model

- **Arms**: `codeaf-senior-dev`, and `mini-swe-agent` through Pier (the D3
  arm, same rig, same grader). `codex` optional when its OpenRouter routing
  is settled.
- **Model**: `deepseek/deepseek-v4-flash-0731` — the model the DeepSWE
  campaigns pinned, so pilot results cross-check against that history (brief
  §6). One model for the whole pilot; a second model (Kimi K3) only if the
  owner asks.
- **Seeds**: the official FrontierCode protocol is 5 trials per model per
  reasoning-effort level; the rig sweeps `seed_ids` × `reasoning_efforts` and
  averages per level. This pilot's written plan stays at 2 seeds per arm (the
  $20 cap the original brief set) — running the full 5-trial protocol at one
  effort is ~80 rollouts, ~$24–120 of model spend, and waits for the owner's
  budget approval. Effort `high` is pinned by listing one level; recorded per row.

## Protocol

- Every arm, every seed: same task, same model, same rubric, same grader,
  same 4 CPU / 8 GiB container budget, same wall (2 h) and cost cap ($5).
- Scanner on every agent run; a flagged run scores 0 and counts toward flag
  rate.
- Compare tasks in PAIRS across arms; report the per-task win/loss column,
  not just the mean. The table is `grade/report.py`'s.
- **Preregistration** (drafted below) before the first pilot run.

## Cost estimate

| item | figure | basis |
|---|---|---|
| gold + negative controls per task | $0 (CPU only) + ~$0.03 judge | measured on the fixture |
| senior-dev rollout | ~$0.3–1.5 | DeepSWE full113 history + this task's C++ build cost |
| mini-swe-agent rollout | ~$0.1–0.5 | fewer turns, smaller context |
| judge per grade | ~$0.02–0.05 | measured (~$0.006/criterion call) |
| **pilot total** | **8 named tasks × 2 arms × 2 seeds ≈ 32 rollouts ≈ $8–16** (the official 5-trial protocol at one effort is ≈ 80 rollouts ≈ $24–120) | within the $20 cap; the count is now pinned by the named task set above |

All spend goes through the credential guard (per-call usage rows) — no
account-balance arithmetic anywhere.

## Preregistration draft (write the full one before the first run)

- **Hypothesis**: senior-dev's pass rate (cleared every blocker, unflagged)
  on the dev tasks is at least mini-swe-agent's, at comparable or lower cost
  per rollout, on the same model.
- **Arms**: the two above, same model, 2 seeds each (5 when the owner
  approves the official protocol's budget).
- **Metric**: pass rate; secondary: mean score, flag rate, cost per rollout,
  wall seconds.
- **Analysis**: per-task paired comparison; no pooling across the dev/held-out
  line (the split is drafted above); wall-clock compared only within the same
  host architecture. The dev half is scored for the headline result; the
  held-out half is run once afterwards as the transfer check, not tuned on.
- **Stopping**: when every planned cell has a grade (or rig), or when the $20
  cap is reached — whichever first. A rig-failed cell is re-run once; twice is
  a rig bug to fix first.
- **Known threats**: the corpus is ours, so no leaderboard comparison is
  implied; judge quality is calibrated only on the fixture's controls; 2
  seeds cannot resolve small differences; 2 cannot either — the official
  protocol averages 5 trials per level for this reason.

## What blocks the pilot

1. **Owner questions** (brief §10): budget ceiling beyond $20, cloud for
   scale-out (GCP/RunPod/Modal) vs local Colima, model set confirmation, and
   whether to approach Cognition.
2. **Task sourcing is hand-work**: each task needs a person-reviewed rubric;
   the pipeline drafts, a person approves. Budget ~1 hour per task.
3. **The judge needs adversarial QC** like Cognition's: the fixture's
   controls calibrate it; every new task's rubric should run gold + a
   hand-made negative before use.