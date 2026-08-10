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
