# Embedding — where this tree came from and what was done to it

This directory is the **swe subharness engine**: swe-pro-go, copied into the
aforge binary so that the `swe` subharness can drive it. Read
`docs/SUBHARNESSES.md` first — it is the law this vendoring serves. Read
`BUGS-KEPT.md` next, because it explains the rule that governs every line
here: **the engine is held bug-for-bug at its upstream commit.** Do not fix,
refactor, reformat, or improve anything in this tree. A behavior you think is
wrong is upstream's to change, and then this tree is re-vendored.

## Provenance

| | |
|---|---|
| Source repo | `github.com/Agent-Field/swe-pro-go` |
| Commit | `af248e9` (*Merge pull request #27 from Agent-Field/model/v4-flash-0731-default*) |
| Vendored | 2026-08-09 |
| Files | 649 `.go` files, ~114k lines, 121 packages |
| Upstream module | `github.com/Agent-Field/swe-pro-go` |
| Upstream go directive | `go 1.25.0` |

### Licensing — unsettled, on purpose visible

**swe-pro-go carries no LICENSE file.** Both repositories are Agent-Field
property and this copy was made with the owner's explicit authorization, so
nothing here is in question internally. But an unlicensed source tree copied
into a second repository is a loose end, and it is worth naming rather than
inheriting silently: **a license should be settled upstream in swe-pro-go**,
and this file updated with it at the next re-vendor.

## Why a copy at all

`docs/SUBHARNESSES.md` explains the shape. In one paragraph: a subharness is
an alternative executor for a single leaf of the graph, and the `swe`
subharness is a full software-engineering pipeline — plan, parallel coder
leaves in git worktrees, per-leaf judge, merge, audit-fix loop, verified
terminal status. A coding issue that would decompose into eight linear leaves
is better taken whole by something that owns worktrees, merges, and a
verifier. That something already exists and works; it is this engine.

It is a copy rather than a module dependency because the aforge binary must be
able to *be* the engine (see the sentinel below), which means the engine's
code has to be linked into it, and because a vendored tree is a thing you can
read, bisect, and patch in place — a version-pinned dependency is not.

## Layout

```
internal/swepro/
  internal/      <- swe-pro-go/internal/*, verbatim
  codeaf/        <- swe-pro-go/cmd/codeaf/*, `package main` -> `package codeaf`
  EMBEDDING.md   <- this file
  BUGS-KEPT.md   ENGINE-DESIGN.md   EVENTS-CONTRACT.md   <- upstream docs
```

The nesting is the encapsulation. Go's internal rule scopes
`internal/swepro/internal/...` to the `internal/swepro/...` subtree, so no
package outside this directory can import a single one of the engine's 121
packages by accident. The only door is `codeaf.Main`.

## What was dropped

| Dropped | Why |
|---|---|
| `cmd/swedog` | A standalone watchdog binary. Nothing in the subharness path calls it. |
| `cmd/plandb-diff` | A developer diff tool for the plan database. Same. |
| `.git` | It is a copy, not a submodule. Provenance lives in this file. |
| Upstream docs other than the three kept | `AGENTFIELD.md`, `DOGFOOD.md`, `FIX-CANDIDATES.md`, `OBSERVER-WIRING.md` stayed upstream; the three kept are the ones a maintainer of *this* tree needs. |

**`serve` mode was NOT dropped.** `codeaf/serve.go` wires the engine up as an
AgentField node through `agentfield/sdk/go/agent`, and the plan allowed
dropping it if it failed to compile against aforge's newer SDK pin. It did
not fail: swe-pro pinned the SDK at `v0.0.0-20260724201800-7ee31640a2f4`,
aforge at `v0.0.0-20260801225427-e6587ade0886`, MVS picked aforge's, and
serve.go compiles against it with zero changes. The SDK skew cost nothing —
the only surfaces that moved between those two versions (`agent.go`,
`agent_did.go`, `discovery.go`, `note.go`) moved compatibly, and swe-pro-go
touches the SDK in exactly one file.

## The `// aforge-embed:` patches — the complete inventory

Three, and only three. Every one carries an `// aforge-embed:` comment in the
source; `grep -rn 'aforge-embed:' internal/swepro` is the authoritative list
and should always agree with this table.

### 1. `codeaf/main.go` — `func main()` becomes `func Main(argv []string) int`

The exported entry point, and the only exported symbol in the package.
The body is the binary's `main()` verbatim with `os.Exit(n)` replaced by
`return n`. It passes a **nil** injected backend, which is precisely what
makes `runCLI` construct the real OpenRouter backend — so every startup
semantic the shipped binary had survives untouched inside `runCLI`:

- the control-plane probe and gate (with patch 2 applied),
- the hard `OPENROUTER_API_KEY` requirement, checked after the gate so the
  gate's refusal keeps precedence,
- the models.dev catalog fetch and its background refresh,
- the terminal checkpoint write and the auto-resume supervisor.

### 2. `codeaf/main.go` — `controlPlaneEnabled`, i.e. `CODEAF_CP_URL=off`

Upstream, the control-plane gate is unconditional for the real backend:
codeaf is not a standalone product, every run is mirrored onto AgentField, and
a run without a reachable control plane is refused. Embedded, that is wrong —
the run is driven by aforge's subharness, which is not an AgentField reasoner,
and a developer running `aforge do` should not need `af dev` up.

The patch extracts the gate's condition into a named predicate and adds one
row to it: the literal value `off` in `CODEAF_CP_URL` refuses the gate. Every
other value, including the empty one, evaluates exactly as upstream did:

```go
func controlPlaneEnabled(baseURL string, injected backend) bool {
	if baseURL == "off" {
		return false
	}
	return injected == nil || baseURL != ""   // upstream, verbatim
}
```

`TestAforgeEmbedControlPlaneGateKeepsUpstreamShapeExceptOff` pins all six
rows, upstream's four and the patch's two.

### 3. `codeaf/catalog_test.go` — one `../` fewer

`CatalogPath: "../../internal/modelsdev/testdata/catalog.json"` was correct
when this package was `cmd/codeaf`, two directories under the repo root. It is
now `internal/swepro/codeaf`, one directory under the vendored root, and the
fixture moved with it: `"../internal/modelsdev/testdata/catalog.json"`. A
relocation consequence, not a behavior change.

### Added files (not patches)

- `codeaf/aforge_embed_test.go` — tests the two `main.go` patches and the
  supervisor argv contract. Named `TestAforgeEmbed*` so `make test` can run
  exactly these out of the vendored package.

Nothing else in 649 files was touched. The import rewrite and package rename
below were mechanical and applied to every file at once.

## The mechanical rewrite

```sh
# every import of the old module, in 453 files
sed -i '' 's|github.com/Agent-Field/swe-pro-go/internal/|github.com/Agent-Field/aforge-v2/internal/swepro/internal/|g'

# the package clause, in the 63 files of codeaf/
sed -i '' 's|^package main$|package codeaf|'
```

String literals that name `swe-pro-go` (default node IDs, test fixtures) were
deliberately left alone: they are data, not imports, and changing them would
be a behavior change.

## The sentinel — how the binary becomes the engine

The engine has process-global state: environment knobs it sets on itself, a
plandb singleton, a working directory it owns. A run of it is therefore a
process, not a goroutine. Rather than ship a second binary, `cmd/aforge`
agrees to *be* that process when told to (`cmd/aforge/swepro.go`):

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
re-execs `os.Executable()` with a codeaf argv of its own
(`codeaf/main.go`, `runAutoResume`) and hands it `os.Environ()` plus
`CODEAF_SUPERVISED=1`. Embedded, `os.Executable()` is the aforge binary — and
because `AFORGE_SWEPRO=1` is already in that environment, the grandchild is
codeaf too. **The supervisor required no modification.** Two tests hold this:

- `cmd/aforge.TestSweproSentinelTurnsTheBinaryIntoTheEngine` builds the
  binary, runs it with the sentinel and the supervisor's exact argv shape, and
  asserts codeaf's voice comes out; then clears the sentinel and asserts the
  same argv lands back in aforge's command switch.
- `codeaf.TestAforgeEmbedSupervisorResumeArgvIsAcceptedByMain` asserts the
  argv `runAutoResume` builds parses under `Main`'s own parser with every
  optional flag set.

Smoke test by hand:

```sh
make build
AFORGE_SWEPRO=1 ./bin/aforge          # codeaf's error, not aforge's chat
AFORGE_SWEPRO=1 ./bin/aforge help     # codeaf's usage
```

## go.mod

- `go` directive raised to `1.25.0` (upstream's floor).
- Already identical, no change: `modernc.org/sqlite v1.29.1`,
  `gopkg.in/yaml.v3 v3.0.1`.
- Raised by MVS: `golang.org/x/sys` 0.36.0 → 0.47.0,
  `golang.org/x/text` 0.3.8 → 0.40.0. The x/sys jump was the identified risk
  to the bubbletea/termenv TUI stack; the full suite is green across it.
- Added: `golang.org/x/net`, `github.com/santhosh-tekuri/jsonschema/v5`.
- Added, but **not linked into a default build**:
  `github.com/smacker/go-tree-sitter`. It is reachable only from
  `internal/tool/shell_treesitter.go`, behind the `treesitter` build tag, and
  it is cgo. `go mod tidy` considers all build configurations so it appears in
  `go.mod`; that is fine and expected. What matters is the invariant:
  **`CGO_ENABLED=0 go build ./...` must succeed.** It does. Keep it that way.
- `replace github.com/charmbracelet/bubbles => ./internal/tui/charmbubbles`
  preserved.

## The test ritual and this tree

`make check` does **not** run the engine's own test suite. In a clean
swe-pro-go clone at `af248e9` on macOS, fifteen of its tests already fail —
`/var` vs `/private/var` symlink resolution, a case-insensitive filesystem,
and JS float-rounding parity. Those are upstream's verdicts to change, and
under the bug-for-bug rule they are not ours to fix, so they are not part of
aforge's end-of-change ritual. This is the same line the repo already draws
around `internal/tui/charmbubbles`, which sits outside `./...` by being its
own module.

The exclusion is exactly this narrow, and lives in one Makefile variable:

- `go build ./...` and `go vet ./...` **do** cover `internal/swepro`, and both
  are green.
- `make test` runs everything outside `internal/swepro`, plus
  `go test -run AforgeEmbed ./internal/swepro/codeaf` — the tests that cover
  the embedding patches themselves.
- `make test-swepro` runs the engine's full suite. This is a re-vendoring
  tool, not a ritual step.

## Re-vendoring procedure

1. Clone swe-pro-go at the new commit somewhere outside this repo. Run
   `go test ./...` in that clone and **keep the output** — it is the baseline.
2. `rsync -a --exclude .git <clone>/internal/ internal/swepro/internal/`
   and `rsync -a <clone>/cmd/codeaf/ internal/swepro/codeaf/`. Do not copy
   `cmd/swedog` or `cmd/plandb-diff`. Refresh `BUGS-KEPT.md`,
   `ENGINE-DESIGN.md`, `EVENTS-CONTRACT.md` from the clone.
3. Re-run the two `sed` commands above over `internal/swepro`.
4. Re-apply the three patches. `git diff` against the previous vendored tree
   will show them as the only conflicts; each is a handful of lines and each
   is findable by `grep -rn 'aforge-embed:'`.
5. `go mod tidy`. Check the `replace` survived and that
   `CGO_ENABLED=0 go build ./...` still succeeds.
6. `make test-swepro` and diff its failure set against step 1's baseline.
   **Equal failure sets mean the embedding changed nothing.** A failure the
   clone does not have is a bug in the vendoring, not in the engine.
7. `make check`, then the sentinel smoke test above.
8. Update the provenance table at the top of this file: commit, date, file
   count, and the licensing line if it has finally been settled.
