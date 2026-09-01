# Embedding — an owned copy of swe-pro-go

This directory is the **swe subharness engine**: swe-pro-go, imported into
aforge at commit `af248e9` (see `UPSTREAM`). Read `docs/SUBHARNESSES.md`
first — it is the law this serves, and its section *"The engine copy is owned,
not borrowed"* is the rule below.

## Owned, not borrowed

From the moment this landed it is **aforge code**. Modify it freely, in place,
whenever tighter integration serves the product — richer events for the chat
surface, steering injection, per-call data for the ledger, performance work.
Ordinary commits, ordinary review, no fork-management ceremony.

Upstream swe-pro-go continues to evolve separately. Improvements worth having
are harvested by **occasional cherry-pick**, not by mechanical re-sync, and
divergence is the expected steady state rather than a debt. Two things keep
that honest:

- `UPSTREAM` records the import commit, so a harvest knows what it is diffing
  against.
- The **divergence log** below records every intentional departure, so a
  harvest knows what not to clobber. *Adding to it is part of changing this
  tree.*

One thing is not negotiable: the **outer seam**. However deep the integration
goes inside this directory, the engine is reached only through the swe
subharness's `Executor` implementation — one Task in, one Outcome out. That is
what lets the next subharness arrive the same way.

`BUGS-KEPT.md` is upstream's document and still worth reading: several
behaviors here look wrong and are deliberate ports of swe-pro's own quirks.
Know which is which before you "fix" one.

---

# Divergence log

Every intentional departure from upstream `af248e9`, newest last. Each is
marked in the source with an `// aforge-embed:` comment;
`grep -rn 'aforge-embed:' internal/swepro` is the same list from the code
side, and the two must agree.

### D1 — `codeaf/main.go`: `func main()` → `func Main(argv []string) int`

*Wave 2, the import itself.* The aforge binary must be able to **be** codeaf
in a child process, so the command needs one exported, callable entry point.
The body is the binary's `main()` verbatim with `os.Exit(n)` replaced by
`return n`. It passes a **nil** injected backend — which is exactly what makes
`runCLI` construct the real OpenRouter backend — so every startup semantic the
shipped binary had is untouched inside `runCLI`: the control-plane gate, the
`OPENROUTER_API_KEY` hard-require (checked after the gate so the gate's
refusal keeps precedence), the models.dev catalog fetch and its background
refresh, the terminal checkpoint, the auto-resume supervisor.

*Cherry-pick note:* upstream still has `func main()`. A harvest that touches
main.go's entry point must re-apply this shape.

### D2 — `codeaf/main.go`: `CODEAF_CP_URL=off` skips the control-plane gate

*Wave 2.* Upstream the gate is unconditional for the real backend — codeaf is
not a standalone product, every run is mirrored onto AgentField, and a run
without a reachable plane is refused. Embedded that is wrong: the run is
driven by aforge's subharness, which is not an AgentField reasoner, and a
developer running `aforge do` should not need `af dev` up.

The gate's condition is extracted into a predicate with one row added. Every
value other than the literal `off`, including the empty one, evaluates exactly
as upstream:

```go
func controlPlaneEnabled(baseURL string, injected backend) bool {
	if baseURL == "off" {
		return false
	}
	return injected == nil || baseURL != ""   // upstream, verbatim
}
```

*Cherry-pick note:* upstream's `if injected == nil || baseURL != ""` is the
line this replaced. Held from outside by
`cmd/aforge.TestSweproControlPlaneGateIsOffOnlyForOff`.

### D3 — mechanical, applied at import by `revendor.sh`

Not really divergences — the same code at a different address — but a harvest
has to reproduce them on anything it pulls across:

1. **Import paths.** `github.com/Agent-Field/swe-pro-go/internal/` →
   `github.com/Agent-Field/aforge-v2/internal/swepro/internal/`, in 453 files.
   The package layout below the module path is identical, so one substitution
   is the whole rewrite. String literals naming `swe-pro-go` (default
   control-plane node IDs, test fixtures) are data, not imports, and were
   deliberately left alone.
2. **Package clause.** `^package main$` → `package codeaf` across
   `codeaf/*.go`, anchored so a `package main` inside a string literal or a
   testdata fixture is untouched.
3. **One directory shallower.** `"../../internal/` → `"../internal/` in
   `codeaf/*.go`: upstream this package was `cmd/codeaf`, two levels under the
   repo root, and its fixtures reached the engine's tree as `../../internal/`.

### D4 — dropped at import

| Dropped | Why |
|---|---|
| `cmd/swedog` | A standalone watchdog binary. Nothing on the subharness path calls it. |
| `cmd/plandb-diff` | A developer diff tool for the plan database. |
| `.git` | The import exports a ref; provenance is `UPSTREAM`. |
| `AGENTFIELD.md`, `DOGFOOD.md`, `FIX-CANDIDATES.md`, `OBSERVER-WIRING.md` | Stay upstream. `BUGS-KEPT.md`, `ENGINE-DESIGN.md`, `EVENTS-CONTRACT.md` came across. |

### D5 — `codeaf/pipeline.go`: two stage events carry the numbers that grade the run

*Wave 4, the learning wave.* The embedding calibrates the boundary between the
generalist and this worker from what the engine already decided, and two of the
engine's own judgements were being made and then dropped on the floor:

- `root-cut` (`decompose` and `selected`) now carries `data.band` — the
  `sizeband` estimate the cut was made from. A `selected` root cut on a small
  band is the engine saying, in its own voice, that the whole goal fits one
  leaf; that is the clearest available evidence that the work may have been
  too small for this worker.
- `audit` (the verdict event) now carries `data.max_cycles` beside `cycle`. A
  cycle number alone says nothing about strain. The same number next to its
  ceiling is the difference between a comfortable run and one that used every
  cycle it had.

Both are additive keys in an existing event's `data` map, so the event contract
in `EVENTS-CONTRACT.md` still holds for every consumer that does not read them;
`internal/exec/swe.go` turns them into `Outcome.Calibration` notes.

*Cherry-pick note:* upstream passes `nil` and `{"cycle": cycle}`. A harvest
touching those three lines must re-apply the maps.

### D6 — the disk floor is capped to the volume, and a pause waits instead of paying

*Wave 5, the headless-regression wave.* One policy number and one loop shape
between them cost a measured run twelve orchestrator turns and 723 seconds of
wall time, all of it spent learning nothing.

`resourceguard.DefaultDiskFloorGB` is a flat `5`, and
`isolation.ReadingToStatus` pauses dispatch below `floor/2`. On a 5GB volume
that pause arms at 2.5GB free and **can never clear** — the volume cannot hold
enough free space to satisfy a floor of its own size. `resourceguard` is a
line-for-line port with fixture tests pinned to upstream's `Number()`
semantics, so the correction is made at the aforge seam that consumes it:
`scheduler/gatherEnvelopeReadings` now caps the floor at a tenth of the
measured volume (`scheduler/diskfloor.go`, `scheduler/volume_statfs.go`) and
recomputes the reading's `ok` against the capped floor. A large volume keeps
the ported 5GB exactly; an unmeasurable volume keeps it too; and a healthy
reading never measures at all, so the common path pays nothing.

`codeaf/root_orchestrator.go` then stops paying for the pause it does hit. A
paused cycle dispatches nothing, so returning its synthetic continuation to the
step loop spends a **paid model turn** to be told what the next cycle already
knows. `absorbResourcePause` waits out the pause inside the cycle that hit it,
re-pumping the delegate (free) every `pauseRecheckInterval` until the volume
recovers or the existing `pauseCycles`/`pauseWallTimeout` bounds expire — the
bounds are unchanged, the stall the caller declares is unchanged, and the whole
episode now costs one turn instead of twelve.

*Cherry-pick note:* upstream's `gatherDispatchEnvelope` passes the guard's
reading through verbatim and its root orchestrator has no pause wait. A harvest
touching either must re-apply this.

### D7 — `internal/util/gitexclude.go`: the exclusions reach the file git reads, and the list is declared once

*Wave 6, the state-boundary wave.* Two edits to one file, and they are the same
subject from either end: where the boundary between the engine's machinery and
somebody's repository is written down, and whether writing it does anything.

`EnsureCodeafExcluded` resolved the exclude file as `rev-parse --git-dir` +
`/info/exclude`. In an ordinary clone that is the file git reads. In a **linked
worktree** it is not: `--git-dir` answers with the worktree's private gitdir,
and git consults the **common** directory's `info/exclude` for exclusions. The
function created the directory, wrote the five patterns, and returned
`(true, nil)` — into a file nothing ever opens. aforge runs every isolated
coding leaf in a linked worktree (`internal/exec/sweview.go`), so the layout
the bug needs is the layout it always has, and the measured result was 490 and
257 engine files committed into user history across two benchmark cells. It now
resolves through `rev-parse --git-path info/exclude`, which is the file git
will read in either layout and an error outside a repository — the one case
where doing nothing is still right. aforge's own `excludeFromGit`
(`internal/exec/swe.go`) had already fixed this exact class for the same
reason.

`ExcludedPaths` is now `enginestate.ExcludePatterns()`. The five strings are
declared at `internal/swepro/enginestate`, a dependency-free package both sides
of the embedding can import: the engine to tell git what to ignore, aforge to
exclude the same paths at the repository root, to take them out of the index
before a landing commit, to keep them out of a delivered file list, and to
sweep them from a directory that is not ours. Before this there were three
lists, and one of aforge's had drifted — it named `.plandb.db` and its sqlite
sidecars and had never named the `.plandb/` directory the scheduler cuts its
worktrees into.

Held by `internal/swepro/internal/util.TestEnsureCodeafExcludedReachesTheFileGitReadsInAWorktree`,
which asserts on `git status` in a real linked worktree rather than on the
contents of a file — only git can say which file git reads.

*Cherry-pick note:* upstream (and the TS source at `src/util/git-exclude.ts`)
still resolves through `--git-dir` and still declares the five strings inline. A
harvest that touches this file must re-apply both.

**`codeaf/serve.go` was NOT dropped.** swe-pro-go pinned
`agentfield/sdk/go` at `v0.0.0-20260724201800-7ee31640a2f4` and aforge at
`v0.0.0-20260801225427-e6587ade0886`; MVS picks aforge's. serve.go — the only
file in the whole engine that touches the SDK — compiles against the newer one
unchanged, so **the SDK skew cost nothing and no compatibility edit exists.**

---

# The initial import

Reproducible, and scripted for exactly that reason:

```sh
internal/swepro/revendor.sh <path-to-swe-pro-go-checkout> [ref]

# what actually ran, once:
internal/swepro/revendor.sh ~/src/swe-pro-go af248e9
```

It exports the ref (not the working tree), copies `internal/` and
`cmd/codeaf/`, drops the unused binaries, applies the three mechanical
rewrites of D3, and stamps `UPSTREAM`. It refuses to finish if an upstream
import survived the rewrite.

**It is not a re-sync tool.** Re-running it against a newer ref would
overwrite every edit in the divergence log. Harvesting an upstream improvement
is a cherry-pick or a hand-port of the specific change, read against this log.

## Provenance

| | |
|---|---|
| Source repo | `github.com/Agent-Field/swe-pro-go` |
| Import commit | `af248e9` (*Merge pull request #27 from Agent-Field/model/v4-flash-0731-default*) |
| Imported | 2026-08-09 |
| Size | 649 `.go` files, ~114k lines, 121 packages |
| Upstream go directive | `go 1.25.0` |

### Licensing — unsettled, and named rather than inherited

**swe-pro-go carries no LICENSE file.** Both repositories are Agent-Field
property and this copy was made with the owner's explicit authorization, so
nothing here is in question internally. But an unlicensed source tree copied
into a second repository is a loose end worth naming: **a license should be
settled upstream in swe-pro-go**, and this file updated when it is.

## Layout

```
internal/swepro/
  revendor.sh    <- the scripted initial import (not a re-sync tool)
  UPSTREAM       <- the import commit
  EMBEDDING.md   <- this file, incl. the divergence log
  internal/      <- the engine: swe-pro-go/internal/*
  codeaf/        <- the orchestrator: swe-pro-go/cmd/codeaf/*, as `package codeaf`
  BUGS-KEPT.md   ENGINE-DESIGN.md   EVENTS-CONTRACT.md   <- upstream docs
```

The nesting is the encapsulation. Go's internal rule scopes
`internal/swepro/internal/...` to the `internal/swepro/...` subtree, so no
package outside this directory can reach one of the engine's 121 packages by
accident. Today the only door is `codeaf.Main`.

## The boundary today, and where it is going

v1 of the swe subharness drives the engine through its **process boundary**,
because the engine's process-global state (env knobs it sets on itself, the
plandb singleton, a working directory it owns) makes that the safe seam:

- **In:** argv and environment — the codeaf CLI's flags plus `CODEAF_CP_URL`,
  `OPENROUTER_API_KEY`, `PLANDB_DB`, `AFORGE_SWEPRO`.
- **Out:** the stdout NDJSON event stream specified in `EVENTS-CONTRACT.md`,
  with the `terminal` event as the result.

That is a starting point, not a permanent contract. As this copy is
domesticated — globals threaded, hooks added — the boundary tightens:
in-process event callbacks instead of NDJSON parsing, mid-run steering,
aforge's router behind the engine's backend interface. Each tightening is an
ordinary aforge change and belongs in the divergence log above.

## The sentinel — how the binary becomes the engine

A run of the engine is a process, not a goroutine (see the globals above).
Rather than ship a second binary, `cmd/aforge` agrees to *be* that process
when told to. `cmd/aforge/swepro.go` is aforge's own file, outside this tree:

```go
func main() {
	if sweproSentinel(os.Getenv) {          // AFORGE_SWEPRO == "1", exactly
		os.Exit(dispatchSwepro(os.Args[1:]))
	}
	...
}
```

This is the first statement in `main()` — before the GC tuning, before a flag
is read. Only the exact value `1` counts, so an operator who exports the
variable to something else still gets aforge.

The sentinel pays for itself twice. The engine's auto-resume supervisor
re-execs `os.Executable()` with a codeaf argv of its own (`codeaf/main.go`,
`runAutoResume`) and hands it `os.Environ()` plus `CODEAF_SUPERVISED=1`.
Embedded, `os.Executable()` is the aforge binary — and because
`AFORGE_SWEPRO=1` is already in that environment, the grandchild is codeaf
too. **The supervisor needed no change at all**, and that is not an accident
of this design, it is the reason for it.
`cmd/aforge.TestSweproSentinelTurnsTheBinaryIntoTheEngine` holds it: build the
binary, run it with the sentinel and the supervisor's exact argv, hear
codeaf's voice; clear the sentinel and the same argv lands back in aforge's
command switch.

Smoke test by hand:

```sh
make build
AFORGE_SWEPRO=1 ./bin/aforge
#   codeaf: missing message; pass a prompt as a positional argument
AFORGE_SWEPRO=1 CODEAF_CP_URL=off ./bin/aforge run "x" --dir /tmp/ws --high vendor/high
#   codeaf: OPENROUTER_API_KEY is not set in the environment      (gate skipped, D2)
AFORGE_SWEPRO=1 CODEAF_CP_URL=http://127.0.0.1:1 ./bin/aforge run "x" --dir /tmp/ws --high vendor/high
#   codeaf: codeaf requires a running AgentField control plane    (gate intact)
```

## go.mod

- `go` directive raised to `1.25.0` (the engine's floor).
- Already identical, no change: `modernc.org/sqlite v1.29.1`,
  `gopkg.in/yaml.v3 v3.0.1`.
- Raised by MVS: `golang.org/x/sys` 0.36.0 → 0.47.0, `golang.org/x/text`
  0.3.8 → 0.40.0. The x/sys jump was the identified risk to the
  bubbletea/termenv TUI stack; the suite is green across it.
- Added: `golang.org/x/net`, `github.com/santhosh-tekuri/jsonschema/v5`.
- Added but **not linked into a default build**:
  `github.com/smacker/go-tree-sitter`. It is reachable only from
  `internal/tool/shell_treesitter.go`, behind the `treesitter` build tag, and
  it is cgo. `go mod tidy` considers all build configurations, so it appears
  in `go.mod`; that is expected. The invariant that matters is
  **`CGO_ENABLED=0 go build ./...` must succeed.** It does. Keep it that way.
- `replace github.com/charmbracelet/bubbles => ./internal/tui/charmbubbles`
  preserved.

## The test ritual and this tree

`make check` does not run the engine's own suite. At the import commit, in a
clean upstream checkout on macOS, fifteen of its tests already fail — `/var`
vs `/private/var` symlink resolution, a case-insensitive filesystem, and JS
float-rounding parity. They fail here for the same reasons, and they were not
worth fixing on the way in.

The exclusion is one Makefile variable and it is exactly this narrow:

- `go build ./...` and `go vet ./...` **do** cover `internal/swepro`, and both
  are green.
- `make test` runs everything outside `internal/swepro`. The embedding's own
  divergences are covered from outside by `cmd/aforge/swepro_test.go`, which
  drives the real binary.
- `make test-swepro` runs the engine's full suite.

Now that this is owned code, that fifteen is a to-do rather than a fact of
life: as the copy is domesticated, the environment-dependent tests can be
fixed and the exclusion narrowed, until `internal/swepro` is simply part of
`make test` like everything else.

### D9 — `internal/swepro/internal/tool`: `apply_patch` is ungated, and the coder is fenced out of the `.codeaf/` tree

*Wave 4, the bench-cost wave.* Two tool-layer fixes that each cost the
vs-pi cells a measured run.

**`apply_patch` is no longer model-gated.** Upstream `registry.ts:339-393`
gated the multi-file `apply_patch` tool to gpt-family models (`usePatch`:
`gpt-` and not `oss` and not `gpt-4`) and, for those same models, hid
single-file `edit`/`write`. The benchmark runs `deepseek/deepseek-v4-flash`,
so every coder leaf was limited to single-file edits while the pi harness
edited many files at once. `apply_patch` is a harness-side unified-diff
editor that needs no provider support, so the gate served only to deny
non-gpt coders the multi-file tool. `FilterDefinitions` now offers `edit`,
`write`, and `apply_patch` to every coder regardless of model; the `usePatch`
branch and the `strings` import are gone.

**The coder is fenced out of the `.codeaf/` machinery tree.** The FEATURE
cell regressed because a coder leaf read and then EDITED
`.codeaf/contract.json` — the harness's registered acceptance contract —
corrupting the run. `resolveMutationPath`, the single choke point that
`write`, `edit`, and `apply_patch` (per-hunk path and move target) all flow
through, now refuses any coder write whose resolved path lands in the
workspace's top-level `.codeaf/` directory: `harness machinery; not part of
the task`. Reads keep using `resolvePath` and stay allowed — the root-cut
flow instructs reading `.codeaf/contract.json`.

The guard is agent-scoped, not blanket. The harness's own agents legitimately
write `.codeaf/` through these same tools — the auditor writes its verdict to
`.codeaf/auditor-verdict.json`, the architect to `.codeaf/plan/architecture.md`
— so a blanket block would break the audit pipeline (and did, on the first
pass). Only the `coder` worker is refused; `call.Agent` is populated from the
leaf's message agent, so a coder leaf's tool call carries `Agent == "coder"`.
The engine's own writes to `.codeaf/contract.json` are Go code in
`internal/session/contract` and never pass through the tool path, so they are
unaffected either way.

*Cherry-pick note:* upstream's `FilterDefinitions` still has the `usePatch`
gate, and its `resolvePath` has no `.codeaf/` write guard. A harvest touching
`registry.ts:339-393` or the edit/write/apply_patch path resolution must
re-apply both.

### D10 — `internal/swepro/orientation`: a shared repo orientation digest, and the grep tool was already present

*Wave 5, the orientation-cost wave.* Two structural changes that each cut the
measured gap between aforge and the pi harness: the pi harness orients in
zero turns because its first prompt carries the repo structure, and it
searches code rather than reading files serially (measured: 51 reads on a
14-file repo where pi needed 8 calls total).

**Orientation digest.** A new aforge-owned package at
`internal/swepro/orientation` (NOT under `internal/swepro/internal/`, so Go's
internal rule lets both `internal/exec` and `internal/swepro/codeaf` import
it) builds a bounded Markdown digest of the workspace: directory tree
(bounded to 300 entries, BFS so entry points appear before leaves), code-file
declaration/signature outlines (NOT bodies — `func`/`type`/`const`/`var` for
Go, `export`/`function`/`class`/`interface`/`const` for JS/TS, `def`/`class`
for Python, etc.), test-file names, and the failing-test summary from the
pre-run baseline. The whole digest is bounded to 8000 bytes and degrades
gracefully: huge repos fall back to top-level tree + README head. The result
is cached per workspace path + failing-test key (a `sync.Map`) so repeated
calls return byte-identical output and the provider prefix cache stays intact.

**Injection points.** The linear worker (`internal/exec/linear.go`) prepends
the digest to the brief just before "Your work:", omitting it when
`briefIsWhole` says the directory holds nothing to discover. The swe coder
gets it two ways: `buildRootCutPrompt` in `pipeline.go` appends it alongside
the existing factsheet (carrying the baseline's failing tests), and
`composeTurnSystem` in `engine_backend.go` adds it to the system prompt for
coder agents. The root-orchestrator gets its digest through the root-cut
prompt path; non-coder agents get none.

**Grep tool.** The swe coder's tool belt already included `grep`
(`internal/swepro/internal/tool/grep.go`, vendored from swe-pro-go) — it runs
`rg --json --hidden --glob=!.git/*` and returns matches grouped by file with
`file:line: text` format, sorted by modification time, capped at 100 matches.
`FilterDefinitions` already offers it to every agent including coders (the
D9 gate removal kept it in the list). The existing tests
(`TestRunLeafToolListMatchesAgentAndModelContract`,
`TestRunLeafFiltersDefinitionsForActualModel`) pin `grep` in the coder's
belt. No new tool was needed; this entry records the audit.

*Cherry-pick note:* the orientation package is aforge-owned with no upstream
counterpart. A harvest has nothing to re-apply here. The `aforge-embed: D10`
markers in `pipeline.go:buildRootCutPrompt` and
`engine_backend.go:composeTurnSystem` mark the injection seams; a harvest
that touches either function must preserve the digest injection.

### D11 — `internal/swepro/internal/engine/steploop` + `internal/swepro/internal/tool`: no-progress guard and diff-aware repeat-read detection

*Wave 5, the no-progress wave.* Two structural mechanisms that each address a
measured tail-risk pathology: one coder replicate spent 1.3M tokens and 184
seconds with no forward progress, re-reading unchanged files and repeating
the same tool calls.

**No-progress guard (`steploop/noprogress.go` + `loop.go`).** The swe coder's
step loop had no progress signal — its only convergence bound was `MaxSteps`
(agent.steps), a hard ceiling rather than a progress indicator. A new
`stepGuard` tracks three general signals after each step: (1) repeated
identical tool calls (same tool + same input + same output, 4 consecutive
times); (2) a stagnant window (6 consecutive steps with no write tool and no
new tool output); (3) a step floor (60 steps, well above the measured
longest honest leaf of 25). On any signal, the guard injects a conclude-now
directive (one chance, with a `landingTurns`-sized grace period) and then
terminates the loop with the partial result. The guard observes completed
`ToolPart`s from the persisted parts, so it works through the existing
message-store seam without new tool-side hooks. The thresholds are deliberately
generous — this is a tail-risk bound, not a budget — and err on the side of
firing late.

**Diff-aware repeat-read detection (`tool/read_history.go` + `read.go` +
`registry.go`).** The `read` tool now tracks file-path + offset + limit →
(mtime, size) fingerprints per session. A re-read of an unchanged file with
the same range is answered with a one-line notice instead of the full
content, making the repeat cost-visible to the model. A file that has changed
since the last read is always allowed (the diff-aware allowance). The check
is skipped when nested instructions were resolved for the read, because
instruction-claim clearing makes a re-read legitimate even though the file
itself is unchanged. The `readHistoryState` is a pointer field on `Registry`,
shared across `forContext` clones, matching the `testMemo`/`guardInFlight`
pattern.

The same no-progress guard exists in aforge's own linear loop
(`internal/exec/noprogress.go`), with the same signals and thresholds, so
both turn loops are bounded consistently.

*Cherry-pick note:* upstream's step loop has no progress guard and its `read`
tool has no read-history tracking. A harvest touching `loop.go`'s `Run`
method or `read.go`'s `executeRead` must re-apply the `aforge-embed: D11`
markers. The `stepGuard` and `readHistoryState` types are aforge-owned with no
upstream counterpart.

### D12 — `internal/baked` + `internal/assets`: the embedded corpora ship compressed

*Binary-size work.* The baked agent roster (`baked/agents/`, half a megabyte of
Markdown) and the engine's prompt and tool-description assets
(`assets/src/`, 166 KB of text) are the two most compressible things this tree
puts in the aforge binary, and go:embed puts them in verbatim. Both now embed a
generated archive instead: one gzip stream per folder, unpacked whole on the
first read, through `internal/packed`. Together they take 443 KB off the
shipped binary.

Nothing a caller sees moves. A packed folder keeps the names go:embed gave it,
so `assets.Get("src/session/prompt/gpt.txt")` resolves exactly as before and
`baked` still reads `agents/<name>.md`; the bytes that come back are the bytes
on disk, which `assets_test.go` and `registry_packed_test.go` assert file by
file against the folders. The folders remain the source of truth — the archives
are generated from them by the `//go:generate` line beside each embed and by
`make build`, and a folder edited without regenerating fails those tests.

The unpack is lazy in both places. `baked` already deferred its parse to first
use, so the roster costs a chat or a `--help` nothing, exactly as before;
`assets.Get` is reached only from a live session.

*Cherry-pick note:* upstream embeds both folders raw. A harvest that touches
`baked/registry.go`'s embed or `assets/assets.go` must re-apply the
`aforge-embed:` markers there, and must regenerate the archives rather than
hand-edit them.

### D13 — `internal/tool`: Firecrawl is the keyless web-search default

*Web-search availability and selection.* Upstream gates `websearch` by provider
or experimental flags, then splits unflagged sessions between Exa and Parallel.
The embedded engine always has Firecrawl's keyless MCP endpoint behind the tool,
so the gate and its frozen `webSearchEnabled` fixtures are deleted. Every coder
belt now carries `websearch`; `CODEAF_WEBSEARCH_PROVIDER` is trimmed and folded
to accept `exa`, `parallel`, or `firecrawl`, the Parallel and Exa enable flags
keep their precedence, and Firecrawl answers every remaining session. The
session hash and its FNV-1a implementation are gone.

*Wire and result shape.* Firecrawl uses one stateless `firecrawl_search`
`tools/call`. Every search requests main-content Markdown, whose per-result
content is capped before the readable numbered result block is returned.
`FIRECRAWL_API_KEY`, read directly rather
than added to the frozen environment inventory, becomes an optional bearer
header. The existing two-endpoint test seam is widened to include Firecrawl;
Exa and Parallel keep their request and response paths unchanged. The schema
names which provider honors each knob and derives Firecrawl's content bounds
from the same constants the renderer applies.

*Cherry-pick note:* upstream still has the provider gate and session split. A
harvest touching `registry.go`, `websearch.go`, or `web_common.go` must preserve
the `aforge-embed: D13` availability, selection, rendering, and endpoint seam.
