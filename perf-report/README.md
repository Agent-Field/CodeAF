# perf/lightweight — what was measured, what was changed, what was left

A performance wave on the aforge binary: startup latency, binary size, steady
state CPU, memory footprint, allocation pressure. Two rules held throughout.
Nothing a person can see changed. Nothing shipped without a number.

Baseline is the merge base, `6b1c06b` (`chat-v3-task`, after the settings-UX and
subharness waves landed). Everything below was measured on the same machine
(linux/arm64, 20 cores, go1.26.5) with the machine otherwise idle.

Reproduce it with the three harnesses in this directory:

```
python3 perf-report/measure-startup.py    bin/aforge 60      # wall, RSS, init
python3 perf-report/measure-firstpaint.py bin/aforge 15      # exec -> first paint
python3 perf-report/measure-size.py       bin/aforge <unstripped>
go test ./internal/tui3/ -run XXX -bench . -benchmem -count=10 | benchstat
```

---

## The table

| | before (`6b1c06b`) | after | delta |
|---|---|---|---|
| **binary** (`make build`, `-s -w`) | 44.11 MB | 38.34 MB | **−13.1%** |
| **first paint** (`aforge chat`, median of 15) | 45.45 ms | 30.71 ms | **−32.4%** |
| **startup wall** (`--help`, median of 60) | 39.77 ms | 26.78 ms | **−32.7%** |
| startup wall (p10) | 33.67 ms | 20.78 ms | −38.3% |
| **idle RSS** (peak, init-only run) | 37.7 MiB | 30.8 MiB | **−18.3%** |
| init heap | 13.70 MB | 8.43 MB | −38.5% |
| init allocations | 128,365 | 72,943 | **−43.2%** |
| packages with an `init` | 330 | 274 | −17.0% |
| packages linked | 687 | 526 | −23.4% |
| **render** ns/frame (`FrameIdle`, 60 turns) | 638.8 µs | 250.3 µs | **−60.8%** |
| render allocs/frame (60 turns) | 1,465 | 1,405 | −4.1% |
| render bytes/frame (60 turns) | 275.9 KiB | 214.7 KiB | −22.2% |
| transcript layout (60 turns) | 610.1 µs | 232.5 µs | **−61.9%** |
| streaming frame (20 turns) | 262.4 µs | 131.3 µs | **−50.0%** |
| **streaming turn**: per-call sanitize (81 turns) | 14.16 µs / 25.1 KiB | 2.40 µs / 0 B | −83.1% / −100% |
| per-call request encode (81 turns) | 806.6 KiB | 775.7 KiB | −3.8% |
| **resume**: journal replay (2000 msgs) | 30.15 ms / 15.4 MiB | 26.48 ms / 7.6 MiB | −12.2% / −50.7% |
| per-message journal append (tool result) | 16.14 µs / 8.27 KiB | 14.47 µs / 4.24 KiB | −10.3% / −48.7% |

Binary sections, stripped:

| section | before | after | delta |
|---|---|---|---|
| `.text` | 18.07 MB | 15.86 MB | −12.2% |
| `.gopclntab` | 14.44 MB | 12.24 MB | −15.2% |
| `.rodata` | 8.36 MB | 7.50 MB | −10.3% |
| `.noptrdata` | 2.80 MB | 2.30 MB | −17.9% |

`.gopclntab` is the PC→line table the runtime needs for tracebacks. No flag
shrinks it; it shrinks only when fewer functions are linked, which is why
removing a dependency pays roughly twice.

---

## The fixes, in the order they were found

### 1. `perf(swepro/baked)` — parse the agent roster on first use

`internal/swepro/internal/baked` held `var agentDocuments = loadAgentDocuments()`:
thirty-four embedded Markdown files whose YAML frontmatter each became a
`map[string]any`, parsed before `main()` on every invocation of every
subcommand. Every reader of it is inside the swe pipeline. A chat, a plan, a
`--help` paid all of it and read none of it.

`sync.OnceValue`. Measured on its own, with `GODEBUG=inittrace=1`:

| | before | after |
|---|---|---|
| init clock | 36.54 ms | 21.21 ms |
| init heap | 13.67 MB | 9.65 MB |
| init allocs | 128,114 | 82,289 |
| `--help` wall (median) | 46.21 ms | 32.45 ms |

`baked` went from 12.00 ms / 4.02 MB / 45,840 allocs to zero.

### 2. `perf(connect)` — the amp provider catalog as a build-time snapshot

The single largest item in the binary. `internal/connect/catalog.go` reads six
fields off a 236-row table. Importing the package that publishes the table
linked 377 packages: AWS SDK v2 (twelve modules), the OpenTelemetry
trace/metric/log/baggage stack, `go-playground/validator` and its locale trees,
`antchfx/xpath`, `golang/groupcache`, `x/net/html` dragging in `x/text`'s CJK
encoding tables, and — through a test helper left in the graph — `testing` and
`flag`. A standalone program calling nothing but `AllNames` and `ReadInfo`
measures 11.21 MB against a 1.64 MB hello-world.

`internal/connect/ampcatalog` mirrors the six fields under the library's own
type, field and constant names — so `catalog.go` changed by exactly one import
line — and reads them from a committed, human-diffable `providers.json`. The
generator is a `main` package nothing imports, so the library stays a module
requirement and stops being a linked dependency. Refresh is one command:

```
go generate ./internal/connect/ampcatalog
```

Binary 44.11 → 38.34 MB, linked packages 687 → 526, RSS −12.4%, wall p10 −19.8%.
(The snapshot itself is 58 KB of embedded JSON, which is in that figure.)

### 3. `connect` — proof the snapshot took nothing away

A copy is only worth having if it can be shown to be true, so the library is
still imported — in tests, where it costs nothing that ships:

- `ampcatalog/parity_test.go` compares the two catalogs row by row and fails if
  regenerating right now would produce a different file.
- `catalog_snapshot_test.go` builds the **whole plug list twice**, once from the
  snapshot the binary ships and once from the library at its pinned commit, and
  requires `reflect.DeepEqual` on every plug including unexported fields — id,
  display name, category, blurb, address with its blank still in it, the blank's
  name and label, where the key rides and under what prefix or format, and the
  health check with its accepted status codes.
- `TestRegistryStillHoldsEveryService` takes the census of the whole registry:
  **99 keyed services**, and six browser services named one by one — `google`
  plus the five tool servers `atlassian`, `linear`, `notion`, `sentry`, `slack`.
  **105 in all.** That is the same census the merge base takes, checked by
  running it there: `byAuth=map[browser:6 key:99]`.

### 4. `perf(tui3)` — ask `outputBody`'s cheap question first

The paint clock runs at 30 fps for as long as anything is alive on the surface,
and every tick invalidates the whole transcript layout. Tool rows are not
cached, so `outputBody` ran once per tool row per frame — and it opened with two
`strings.LastIndex` calls, backward scans of the entire tool result, which for a
read or a grep is hundreds of kilobytes.

Both scans were already paired with a `strings.HasSuffix` test, in the wrong
order. Both trailers run to the end of the result, so a result that does not end
in `"more bytes)"` cannot carry the cap marker. Both operands are pure; only the
order changed.

A CPU profile of `BenchmarkFrameIdle/turns=60` had `strings.LastIndex` as the
hottest function in the process, 100% of it from `outputBody`. −31.6% to −39.5%
on every frame benchmark, allocations unchanged to the byte.

### 5. `perf(tui3)` — stop measuring the same strings twice on every tool row

With `outputBody` gone, `ansi.stringWidth` was 28% of the frame, more than half
from `toolLine`. Four repeats:

- `readLines` counted lines with `len(strings.Split(body, "\n"))` — a string
  header per line of a hundred-kilobyte read, to take `len()` of.
  `strings.Count` gives the same number and allocates nothing.
- `toolLine` measured the rail marker per row. All three rail forms are four
  cells wide by construction — that is *why* they are that shape. Now a
  constant, held there by `TestRailFormsAreOneWidth`.
- The stat slot was measured to decide whether it fits, then measured again to
  record how much of the line it took.
- `fit()` measures a string to decide whether to truncate; the caller then
  measured the answer. `fitWidth` returns both.

−24.7% to −36.4% on top of the previous commit, and −22.7% bytes/frame.

Across both render commits: `FrameIdle/turns=60` **−60.8%**,
`Layout/turns=60` **−61.9%**, `FrameStreaming/turns=20` **−50.0%**.

### 6. `perf(provider)` — sanitize the transcript without copying it first

`sanitizeMessages` always allocated a full copy of the message-header array and
threw it away when nothing changed; `dropEmptyTextParts` did the same per
assistant message. Copy-on-first-write. On a clean 81-turn transcript that is
25.1 KiB and 82 allocations per provider call reduced to zero, and the repair
path got 32% faster too, so it is not a trade.

`EncodeRequest` wall time does not move — it is dominated by the JSON marshal of
a ~450 KB body, and sanitize was never more than ~0.6% of it. The win is
garbage, and it is >5% of the metric the change is about.

### 7. `perf(session)` — replay the journal off the scanner's buffer

`replaySessionFile` did `json.Unmarshal([]byte(scanner.Text()), …)`: `Text()`
allocates a copy of the line and `[]byte(…)` allocates a second. Every use of
the line in the loop was audited — the two unmarshal targets are strings, ints
and bools, no `json.RawMessage`, no `[]byte` field — so nothing can alias the
buffer past the iteration. Resume of a 2000-message session: **−50.7% bytes**,
−12.2% wall.

### 8. `perf(session)` — journal a single text part without copying it

The `strings.Builder` flatten is kept for every other shape; the one-text-part
case takes the string as it stands. −48.7% bytes and −25% allocations per
recorded tool result, on the path that holds `a.mu` across a `write(2)`.

---

## Measured and rejected

Each of these was measured, not guessed. Every one is below the ~5% bar this
wave held itself to, or would change something a person can see.

| candidate | measurement | why rejected |
|---|---|---|
| `-trimpath` | 38.273 → 38.208 MB, **0.17%** | Below the bar. (Still worth having for reproducible builds — that is a different argument.) |
| Two bubbletea and two lipgloss generations linked | **0.09 MB combined** | The linker's dead-code elimination already handled it. The "duplicate dependency" reading of `go.mod` is wrong. |
| Two JSON-schema validators (`santhosh-tekuri` + `google/jsonschema-go`) | 0.17 MB, **0.4% of the binary** | Below the bar, and consolidating them is an API change in two subsystems. |
| `golang.org/x/text/collate` | **1.26 MB** | It is `internal/swepro/internal/jscompat`'s `localeCompare`, a deliberate and heavily documented JS-parity implementation. Removing it changes sort order in the swe pipeline. Behaviour. |
| `chroma/lexers` selective import | 2.74 MB heap, 31,450 allocs, ~8 ms at init — the **largest remaining init item** | `internal/tui2/prose` calls `lexers.Analyse` for unlabelled code fences, which needs every lexer's analyser. A subset registry changes how an unlabelled fence is highlighted. Behaviour. Upstream's own load is already the minimal config-header parse (`fastUnmarshalConfig`), so there is nothing to reclaim short of dropping languages. |
| `modernc.org/sqlite` | 1.69 MB | The price of CGO-free sqlite. A deliberate standing tradeoff, not a regression. |
| `e.text += text` per delta (`tui3/app.go`) — the classic quadratic | **48.25 MiB and 13.5 ms per 4000-delta reply**, confirmed by `BenchmarkAppendText` | Kept as-is, deliberately. In context it is ~1.5% of a streaming turn's CPU and ~6% of its allocation, because the 30 Hz frame path dwarfs it. Every fix that removes it either needs `unsafe.String` over an append-only buffer or turns `entry.text` into an accessor at 41 call sites — a real risk of a dropped character on screen, for a win the profile does not justify **yet**. It becomes worth doing once the frame path is cheaper; see follow-ups. `strings.Builder` is specifically *not* an option: `entry` values are copied when `a.entries` grows, and a copied non-empty Builder panics on the next write. |
| `debug.SetGCPercent` tuning | — | `cmd/aforge/main.go:41` already sets `GOGC=400` when the environment does not, with a comment explaining the bargain. Nothing to add; the wave reduced allocation instead. |
| `-s -w` | already in the Makefile | Confirmed applied. |
| `upx` | not attempted | Explicitly out of scope: it changes startup. |

---

## Follow-ups this wave did not take

Ranked by what the profiles say they are worth.

1. **Tool rows are never cached at all** (`tui3/render.go:363-369`, `toolview.go`).
   After the two render fixes, `toolRows` is still the largest single item in the
   frame. Every row re-parses its arguments JSON (`toolstat.go` `argsOf`), re-runs
   three regexes over the whole tool output, and — for an edit row — recomputes an
   O(n×m) LCS with a freshly allocated table capped at 600×600. All of it is
   frozen once the call has ended. A memo on the entry, invalidated where the
   entry is, is the fix; it needs care because the spinner and count-up cells
   genuinely do animate.
2. **The live assistant block is re-parsed as markdown every frame**
   (`render.go` `assistantRows` → `prose.Render` → goldmark). The settled prefix
   at `e.mdCut` cannot change; only the tail does. Caching the rendered prefix
   turns a per-turn quadratic into a linear one.
3. **`detailBody` renders the whole tool output and then caps it to ~30 rows**
   (`toolview.go:1111` and `:1207`). A 5000-line log paints 5000 rows and
   discards 4970, every frame it is open. Cap before painting.
4. **Geometry is rendered two or three times per frame to be counted.**
   `inputHeight`, `statusHeight`, `consentHeight` and `welcomeHeight` each run
   their full renderer to take `len(rows)`. Memoise per (width, frame sequence).
5. **`e.text += text`** — see the rejection above. Worth revisiting once 1–3 have
   landed and the frame path no longer dominates.
6. **`encodeMessages` re-marshals the whole transcript per provider call**
   (`provider/caching.go:193`). Measured: 1.27 ms and 483 KB per call at 81
   turns, so total marshal work across a run is quadratic in transcript bytes.
   Not taken because messages are values in a slice and any cache needs an
   identity key that is as expensive to compute as the marshal itself; doing it
   safely means making transcript messages explicitly immutable first. That is a
   design change, not a perf patch.
7. **`estimateTokensLocked` walks the whole transcript two or three times per
   step** (`session/loop.go:1375`) for a number that is far below threshold
   almost every time. An incrementally maintained byte total on the Agent,
   invalidated by `record`/`compact`/`stub`, removes it.
8. **`firstLine`/`clip` pin whole tool results** (`session/loop.go:1010-1020`).
   `firstLine` returns a substring, and `clip` returns it unchanged when it is
   under 80 bytes, so an `Event.Hint` can hold a multi-megabyte result alive for
   as long as any surface keeps the event. One `strings.Clone`. Not measured
   here because it is a retention bug rather than a throughput one, but it is
   cheap and correct.
9. **`stubOldOutputs` holds `a.mu` across sha256 + `MkdirAll` + `WriteFile` in a
   loop** (`session/stub.go:99-114`), and `a.mu` is the lock `Interrupt` needs.
   Self-documented as an exception; still worth narrowing.
10. **`task_run.go:565` leaks a self-rearming `time.AfterFunc`** whose handle is
    discarded — nothing cancels it at session close.
11. **~50 `regexp.MustCompile` calls inside functions**, worst in
    `swepro/internal/session/plannertranslate/architecture.go` (fourteen, four of
    them inside loops) and `swepro/internal/session/scheduler/dispatch.go`.
    `swepro/internal/session/policyline/policyline.go:57` already has the right
    pattern for the genuinely dynamic ones.
12. **Uncached config reads on the launch path.**
    `config.LoadProjectConfig` and `readProfileConfig` memoise nothing, and
    `cmd/aforge/chatv3.go` calls through them ~24 times before the first frame
    (plus three in `newApp`). Roughly 30–50 `os.ReadFile` + `json.Unmarshal`
    pairs against two small files. Measured as a small share of a 27 ms startup,
    so below the bar on its own — but it is also what makes opening `/settings`
    ~46 uncached file reads, which is the better reason to fix it.
13. **`replaySessionFile` scans from byte 0 regardless of compaction markers**
    (`session/sessionfile.go:544`). A session compacted twenty times parses all
    twenty generations to produce the tail. A compaction offset in the header
    would make resume O(live transcript) instead of O(bytes ever written).
14. **The transcript is shaped twice, in full, to draw forty rows**
    (`tui3/replay.go:45` → `session/agent.go:1469`), and `Transcript()` does it
    under `a.mu`.

---

## Test status

`go test ./...` at the merge base and on the final tree produce the **identical
set of failing packages and the identical set of failing test names** — only the
timings differ. Both are recorded here: `testfailures-baseline.txt` and
`testfailures-after.txt`.

The eleven packages that fail do so on both, for reasons that predate this wave
(hard-coded `/home/abir` paths in a fixture, macOS-derived JS float-rounding
parity, a missing `bun`, ripgrep not on PATH, a query-string in an error
string).

- `gofmt -l` clean on every file touched.
- `go build ./...` and `go vet` on the touched packages: clean.
- `go test -race ./internal/session/ ./internal/tui3/`: green.
- `make build`: green.

One pre-existing flake was identified and is *not* from this wave:
`TestLearnedQuirksSurviveTheProcessThatLearnedThem` in `internal/provider` fails
deterministically at `-count=25`, and does so identically with the pre-change
`wire.go` restored — `quirks.persist()` and `quirks.save()` can interleave on the
same file.

---

## Files

| file | what it is |
|---|---|
| `measure-startup.py` | wall clock, peak RSS, and `GODEBUG=inittrace=1` totals for a binary |
| `measure-firstpaint.py` | exec → first painted byte of `aforge chat`, over a pty |
| `measure-size.py` | section breakdown and per-package symbol attribution |
| `startup-{base,before,after-baked,postmerge,after-ampsnap,final}.json` | startup harness output at each step |
| `size-{base,after-ampsnap,final}.json` | size attribution at each step |
| `inittrace-{baseline,after-baked}.txt` | raw `inittrace` output |
| `tui3-render-{before,after-outputbody,after-widths}.txt` | raw `-count=10` benchmark output |
| `tui3-{outputbody,widths,render-total}-benchstat.txt` | the benchstat tables |
| `{wire,replay,append}-{before,after,benchstat}.txt` | provider and session benchmarks |
| `testfailures-{baseline,after}.txt` | the two failure sets that must match |
