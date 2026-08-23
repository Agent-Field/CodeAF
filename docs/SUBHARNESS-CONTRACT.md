# The subharness contract, as landed

*Build map for the subharness v2 wave. `docs/SUBHARNESS-PRD.md` is the
authority on what is being built and why; this page is what the contract lane
actually put in the tree, where each other lane plugs in, and the three places
the build had to decide something the PRD left open.*

Everything below is in `internal/exec` unless it says otherwise. Nothing is in
`internal/subharness`: the old package is untouched, exactly as PRD §2 requires.

---

## 1. The manifest — `manifest.go`

`Manifest` **embeds** `SubharnessInfo` rather than restating it. That is the
PRD's "grow, don't parallel" taken literally: every existing reader still asks
the same struct for a name, a purpose, a ruler and a budget shape, and the
manifest is that struct plus the six things a typed program needs.

```go
type Manifest struct {
	SubharnessInfo                         // name, purpose, prior anchors, the deadline triple
	Cues       []string   `json:"cues,omitempty"`
	Input      Schema     `json:"input,omitempty"`
	Output     Schema     `json:"output,omitempty"`
	Whitelist  []string   `json:"whitelist,omitempty"`
	Guards     []Guard    `json:"guards,omitempty"`
	Provenance Provenance `json:"provenance,omitempty"`
}
```

The **cost shape** is the embedded `SubharnessInfo`: `PriorAnchors` plus
`DeadlineFloor`/`DeadlineStep`/`DeadlinePerTokens`, and `SubharnessInfo.Deadline`
is unchanged. There is no second spelling of a budget anywhere in the wave.

`Schema` is `json.RawMessage` under a name — the bytes a schema was written as,
round-tripping through a manifest untouched, which is what makes the contract
language-agnostic in practice and not only in intent.

```go
type Schema json.RawMessage
func (s Schema) Empty() bool
func (s Schema) Validate() error      // "this is not JSON", "has to be an object"
func (s Schema) Fields() []Field      // what the intake card draws
type Field struct { Name, Type, Title, Description string; Required bool; Default json.RawMessage; Enum []string }
```

`Fields` orders by the schema's own `x-order` where it states one and
alphabetically otherwise, so a card drawn twice is the same card.

```go
type Provenance string
const (
	FromBinary  Provenance = "built-in"
	FromYou     Provenance = "yours"
	FromProject Provenance = "from this project"
)

type GuardKind string
const (GuardFile, GuardTool, GuardField, GuardJudgement GuardKind = "file", "tool", "field", "judgement")

type Guard struct {
	Kind GuardKind
	File, Tool, Field, Pattern, Question string
	Because string   // the person-facing half of the fell-back sentence
}
```

`Manifest.Validate() error` and `Guard.Validate() error` answer in prose —
*"`weekly marketing` has a space in it — a name is one word, with dashes where
you want the gaps"* — because the two readers are a person writing a bundle by
hand and a model iterating against the error it got back.

Name rule, stated once: lowercase letters, digits, `-`, `_`, starting with a
letter, at most 64 characters. It is the intersection of what a slash command,
a spoken proposal and a directory can all carry.

## 2. The runner — `runner.go`

```go
type Runner interface {
	Manifest() Manifest
	Run(ctx context.Context, input json.RawMessage, env Env) (RunResult, error)
}

type RunResult struct {
	Output     json.RawMessage
	Report     string
	Artifacts  []Artifact
	Incomplete string   // why it did not finish, in a person's words
	FellBack   string   // it needed a closer look and was handled the long way
	Spend      Spend
}
func (r RunResult) Finished() bool

type Artifact struct { Name, Path, Note string }
```

**An error means the run could not be made to happen** — a broken bundle, a dead
context — and is what the deopt path catches. A run that *happened* and did not
finish returns `Incomplete` and no error. `Finished()` is the one reading of
"done" in the wave.

Go runners implement this natively. The JS runner is **one** generic Go runner
parameterized by a bundle; it registers through `BundleSource` (§3).

## 3. The unified registry — `runner.go` + `executor.go`

`exec.Registry` grew two fields; nothing was duplicated and no existing method
changed.

```go
type Registry struct {
	executors map[string]Executor   // unchanged
	fallback  Executor              // unchanged
	runners   map[string]Runner     // layer 0: compiled in
	bundles   []bundleLayer         // stores, sorted by Layer
}

func (r *Registry) RegisterRunner(runner Runner) error
func (r *Registry) UseBundles(layer Layer, source BundleSource)
func (r *Registry) Subharness(name string) (Runner, error)   // the new lookup
func (r *Registry) Manifests() []Manifest                    // the one list
func (r *Registry) Generalist() Executor                     // the deopt's long way

type BundleSource interface {
	Runner(name string) (Runner, bool)
	Manifests() []Manifest
}

type Layer int
const (LayerBuiltIn Layer = iota; LayerPacked; LayerProject; LayerHome)
func (l Layer) Provenance() Provenance
```

**The two halves answer two different questions about the same names.**
`Registry.For` still degrades an unknown name to the generalist, because a *leaf*
that named a worker this build lacks should still get its work done.
`Registry.Subharness` refuses an unknown name with `ErrNoSubharness`, because a
*person* who typed one has made a typo and being handed something else is worse
than being told.

**Lookup order** lives in the `Layer` constants and nowhere else. Adding the
packed trailer in Phase 2 is a `UseBundles(LayerPacked, …)` and not an edit to
any lookup. Nothing packed-trailer-shaped was built.

**Provenance is stamped, never authored.** `manifest.json` on disk does not get
to claim it is built in; the registry writes the field from the layer the bundle
was found at, at both `Subharness` and `Manifests`.

### Registration story

| what | where it registers | when |
| --- | --- | --- |
| a description (name, purpose, ruler, budget) | `exec.RegisterSubharness` / `exec.RegisterManifest` — process-global | `installSubharnesses()`, once per process |
| a Go runner | `Registry.RegisterRunner` — per registry | `registerSubharnessRunners`, per run |
| a store of bundles | `Registry.UseBundles(layer, source)` — per registry | store lane, at launch |

The split is the one this package always drew: a **description** is a fact about
the process; a **worker** is built by a surface out of its own clients and
workspaces. The process-global table now holds `Manifest` instead of
`SubharnessInfo` — `Subharnesses()` and `SubharnessFor()` project the embedded
half out and every existing caller is byte-identical. New doors beside them:
`RegisterManifest`, `RegisteredManifests`, `ManifestFor`.

### linear and swe, re-fronted

`ExecutorRunner` fronts any `Executor` as a `Runner`, with `TaskInput`
(`brief` required, plus `goal`, `contract`, `title`) and `TaskOutput`
(`result`, `artifacts`) as the typed doors. It is generic and must stay so —
nothing in it may ask which worker it wraps.

```go
func LeafManifest(info SubharnessInfo) Manifest
func FrontExecutor(executor Executor, manifest Manifest) (*ExecutorRunner, error)
```

`RegisterSubharness` now routes through `LeafManifest`, so `linear`, `swe`,
`bare` and every test registration in the tree grew the typed front door without
one caller changing a line. `cmd/aforge`'s `registerSubharnessRunners` fronts
each of them from **the same constructor table** `executorFor` uses, which is
what makes the deopt honest: falling back to the long way means falling back to
the worker the person would otherwise have had.

The ending is read from `Outcome.Overran()`, never from `Stop` alone — a leaf
whose budget ran out lands truthfully on `StopDone`, and reading `Stop` would
report a truncated partial as a finished deliverable.

## 4. One Env — `env.go`

One Go type is both the host API handed to JS bundles and the interface handed
to Go runners. The two drifting `Env`s named at
`cmd/aforge/chatv3_harness.go:41-53` get no third sibling.

```go
type Env interface {
	AI(ctx context.Context, promptRef string, input any, opts AIOptions) (Answer, error)
	Tool(ctx context.Context, name string, args map[string]any) (ToolResult, error)
	Ask(ctx context.Context, question string, opts AskOptions) (AskAnswer, error)
	Remember(ctx context.Context, note string) error
	Recall(ctx context.Context, query string) ([]Note, error)
	Log(ctx context.Context, status string) error
}

type AIOptions struct { Schema Schema; Effort provider.Effort }
type Answer     struct { Text string; JSON json.RawMessage; Spend Spend }
type ToolResult struct { Text string; JSON json.RawMessage; Spend Spend }
type AskOptions struct { Options []string; Default string }
type AskAnswer  struct { Text string; TakingOver bool; Unanswered bool }
type Note       struct { Text, At string }
```

`Effort` is `provider.Effort`, not a second spelling of it. `AskAnswer` keeps the
third answer the old gate got right — *taking over* — and adds `Unanswered`,
which is what an unattended run gets when the gate declared no default: the
program stops, it does not guess.

`UnwiredEnv` implements every door with a typed `*NotWired` error wrapping
`ErrNotWired`. It is scaffolding for the lanes, not the "absent, not broken"
pattern reaching a person — a surface with no host has no subharnesses to offer,
so nothing about them is drawn at all.

### The journaling seam — `journal.go`

```go
type JournalEntry struct {
	Seq int; At time.Time
	Call string        // CallAI | CallTool | CallAsk | CallRemember | CallRecall | CallLog
	Ref string
	Input, Output json.RawMessage
	Spend Spend
	Note, Err string
	Elapsed time.Duration
}

type Journal interface { Write(entry JournalEntry) error }
func Record(journal Journal, entry JournalEntry) error   // nil-safe; use this at every call site
```

Every host call journals inputs, outputs and cost **before its answer returns**.
A nil journal is a run nobody is watching, which is a real case (a guard check, a
headless call that wants only the answer) and not an error.

```go
type Spend struct { Model string; Calls, Input, Output, CacheRead, CacheWrite int; CostUSD float64 }
func (s Spend) Reported() bool
func (s *Spend) Add(other Spend)
func (s Spend) Mixed() bool
```

Summed across a run's journal entries, `Spend` *is* the run's ledger — which is
the only reason the journal and the ledger can never disagree. It does not lock,
and that is the same law `internal/subharness/usage.go:24-28` states about
itself: a runtime that ever ran host calls concurrently owns this type's safety
along with everything else it changed.

## 5. The frozen stub doors — `internal/session/subharness_contract.go`

Written in the shape `standing_contract.go` and `standing_orders.go` proved. The
signatures are the contract; the surface lanes code against them as they stand.

```go
type SubharnessRow   struct { Manifest exec.Manifest; LastRun string }
type SubharnessField struct { Field exec.Field; Value json.RawMessage; Filled bool }
type SubharnessCard  struct { Manifest exec.Manifest; Fields []SubharnessField; Missing []string; Why string }

func (a *Agent) SubharnessList() []SubharnessRow
func (a *Agent) SubharnessIntake(name string) (SubharnessCard, error)
func (a *Agent) SubharnessRun(ctx context.Context, name string, input json.RawMessage) (uint64, string, error)
```

The seam is `Config.Subharnesses *exec.Registry`. **Nil is subharnesses off**, on
exactly the terms `RunHarness` nil is detection off: the doors answer nothing,
calmly.

| door | what it promises | who fills the rest |
| --- | --- | --- |
| `SubharnessList` | every subharness visible here, all layers, precedence already applied, `linear` excluded (it is what you get when you pick nothing) | **tui lane** draws it; **store lane** fills `LastRun` from the run journals it keeps beside each bundle. Empty until then, which is what a subharness nobody has run should draw. |
| `SubharnessIntake` | every input field with its schema account, and the required blanks in `Missing`. An unknown name is an error, not an empty card. | **session lane** fills the fields from the conversation (infer-then-confirm). Today every field is blank and every required one is missing — an honest card for `/subharness <name>` typed cold. |
| `SubharnessRun` | the node's id and title, the same pair `StartTask` answers | **door lane**: reserve the node, spin the run against the registry's runner with the session's `Env`, wire the journal to the room and the cancel route to the run's context. Answers `errSubharnessUnwired` until then. |

`SubharnessRun` returns the `StartTask` pair deliberately, so a surface that
already knows how to open a room on a started task needs no second call site.

## 6. Lane boundaries

| lane | owns | plugs into |
| --- | --- | --- |
| **runtime** | goja, the bundle-parameterized `Runner`, the six host-call bodies, fuel, guards and the deopt, the incremental `Journal` writer | implements `exec.Runner` and `exec.Env`; writes `exec.JournalEntry`; folds `exec.Spend` |
| **store** | `~/.aforge/subharnesses/` and `.aforge/subharnesses/`, version minting, run journals on disk | implements `exec.BundleSource`; calls `Registry.UseBundles(LayerHome/LayerProject, …)`; fills `SubharnessRow.LastRun` |
| **tui** | the `/subharness` list, the intake card, the run view | reads `SubharnessList`, `SubharnessIntake`; calls `SubharnessRun` |
| **doors** | run-as-task-node, auto-propose behind the consent card, headless `aforge run subharness` | fills `SubharnessRun`; wires `Config.Subharnesses` |

Not built, on purpose: goja, the store, anything in tui3, the catalog/index
(Phase 2), the packed trailer (Phase 2 — only the `LayerPacked` constant).

## 7. Deviations from the PRD, and why

Three, all small, all flagged rather than made quietly.

**1. `exec.Spend` exists instead of reusing `subharness.Usage` directly.**
PRD §5 says to reuse `internal/subharness/usage.go` and its fold. Its *figures*
are reused field for field — model, calls, input, output, cache read, cache
write, cost — and the session's fold takes the same numbers. What could not be
reused is the type: `Usage.add` and `Usage.addLoop` are **unexported**, so
nothing outside `internal/subharness` can fold a call into one, and the goja
host and every Go runner live outside it. Importing that package into
`internal/exec` would also make the executor depend on the package the PRD
supersedes. So the fields are the ledger's and the doors are exported. The
no-lock law is carried over verbatim.

**2. "Go-native subharnesses are layer 0 implicitly" is read as *layer 0 wins*.**
PRD §7 numbers the stores 1–3 and calls compiled-in Go "layer 0, always
present" without saying where it sits in the first-hit-wins order. This build
puts it **first**: a bundle on disk may not shadow a name the binary ships,
because those names are what the manual, the system prompt and every menu
describe, and a store that could silently replace one would turn all three into
documents about a program that did not run.

**3. `linear` is a registered runner but is on no list.**
PRD §3 says `linear` and `swe` are re-fronted through the registry, and §9 says
`/subharness` opens a list. Both are true here without contradiction: `linear`
resolves by name (the deopt path and the headless runner both need it) and
appears on nothing a person picks from. That is the same law
`internal/exec/subharness.go` already states — the baseline is what every node
is judged against, not an entry on a menu — and it is why `Manifest.Validate`
exempts exactly one name from needing a purpose.

## 8. Gates

`gofmt -l` clean on every touched file. `go build ./...` clean.
`go test ./internal/exec/ ./internal/session/ ./internal/subharness/ -count=1`
green, as are `./cmd/aforge/` and `./internal/resident/`. No manual page yet:
this lane landed no slash command, no belt verb and no person-facing surface —
the pages come with the surfaces, per the manual law.
