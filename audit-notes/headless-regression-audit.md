# Headless regression audit — `chat-v2` vs trunk

> Superseded by #227: there is one worker; `--subharness`, `swe` and `bare` no
> longer exist.

Investigation only. No fixes landed. Working tree untouched.

- Window audited: `subharness-m1` (5d64d88) → `chat-v2` (61923d7), 209 commits.
- Spot-checked: `resident-m1` (d9fc533), `master` (8e24b7f), `origin/exec-harness-mode` (064ae9e).
- Live A/B on RunPod, both binaries cross-compiled from the two commits above.

## TL;DR

| question | answer |
|---|---|
| Is there a regression? | **No capability regression — chat-v2 delivered the answer trunk failed to produce.** But there is a real **efficiency** regression: `abcc5a5` grew the leaf's observation window 10.5× (25 KB → 256 KB) and left the 150 000-token spend ceiling unchanged, so the same task cost **4.5× the tokens, 4.0× the spend and 3.2× the wall time**, burned through **7 extension rounds vs 1**, and crossed its wrap-up threshold on the first call. §1.0, fix F0. |
| Task parallelization regressed? | **No.** Every scheduling file is byte-identical; both arms measured **3 concurrent leaves**, leaves starting within **8 ms** of each other. §4, §6. |
| swe allocated wrongly? | **Inverted.** Both arms routed the *coding* task to `swe` identically. On the *non-coding* analysis task, **trunk wasted `swe`** and failed; **chat-v2 correctly chose `linear`** and succeeded. Separately: **swe failed on every leaf in the scored battery (16/16)** — but an isolated probe on `deepseek/deepseek-chat-v3.1` ran the pipeline end to end (54 s, real compiled code), so the engine works and the blocker is the model-id rewrite you are already half-fixing. P0. §1.5, fix F1. |
| `exec` vs `do`? | `exec` was never on the trunk lineage. It existed only on unmerged `origin/exec-harness-mode` @`064ae9e` as a raw one-shot harness loop. §2. |
| Do chat and `do` share the task algo? | **Yes**, one `buildBrain`; chat-v2 is a rendering fork only. §3. |
| Is worker choice per-leaf? | **No — one choice per job, in practice.** `linear` is spelled `""`, which already means "no verdict", so a per-node sizing verdict can promote to a specialist but never pin back to the generalist. Measured: 12 leaves sized generalist all ran `swe`. §1.6, fix F1b. |
| Are task specs under-specified? | **No.** Coding briefs name module, file, types, constraints and a `go build` acceptance check; contracts add done-means, verify step, common mistakes. But the shape is **one-size-fits-all prose** — no per-harness template, no structured scope or acceptance — and the **retry path drops specificity**. §6. |
| Is the spec per-harness? | **No — one `exec.Task` for both.** And `swe` **never reads `Contract`** (`internal/exec/swe.go` flattens everything to one CLI arg), so the per-leaf working method is written, paid for, and discarded on every coding leaf. §8.1. |
| Are selection rules hardcoded? | **No — 100% prompt-driven, zero keyword/regex heuristics** across all four selection sites; the descriptor is `SubharnessInfo.Purpose` + a measured cost/success line. That registry is the natural seam for richer descriptors. §8.3. |
| What do the prompts optimize? | **Wall-clock only.** `plan/plan.go:50-53` tells every planning prompt agents are *"instant, free, and unlimited in number"* — token cost is declared zero and enforced only by hard rails; quality is a binary bar. **Nothing anywhere trades two axes.** §8.4. |
| Anything else? | Two features declared and never wired: `Terrain` (F2 — the patch is already written and unapplied) and `FileShaped` (F3). §5. |

---

## 1. Verdict

**The suspected regression is refuted. A different, real one — an efficiency
regression — was found instead.**

On the decomposable task P, run on both binaries with a byte-identical prompt:

| | trunk | chat-v2 |
|---|---|---|
| **outcome** | **failed** — *"still mid-flight when time ran out… Nothing here is the answer"* | **succeeded** — all four briefs delivered in full |
| wall | 264.2 s | **834.5 s (3.2×)** |
| spend | $0.0746 | **$0.2976 (4.0×)** |
| prompt tokens | 1 031 922 | **4 653 833 (4.5×)** |
| nodes | 6 | 12 |
| extension rounds | 1 | **7** |
| max concurrent leaves | **3** | **3 (identical)** |

So chat-v2 is **better on the thing that matters** — it produced the deliverable
trunk abandoned — and materially **worse on cost and latency**. Whoever remembers
headless "getting worse" is most likely remembering the wall clock, which is real.
The cause is `abcc5a5` and it is a **tuning defect, not a capability loss** (§1.0):
the leaf's memory grew 10.5× while its wallet stayed fixed, so leaves cannot finish
inside one budget and must be extended over and over. Fixing F0 should keep
chat-v2's better outcome while removing most of the 3-4× cost.

**chat-v2 won every head-to-head task in the battery.**
- **N** (non-coding analysis): trunk wasted `swe` on a prose task and failed in
  82 s; chat-v2 chose `linear` and delivered in 115 s.
- **P** (four independent briefs): as above — trunk abandoned, chat-v2 delivered.
- **C** (coding, default model): a draw — both allocated `swe` correctly, both hit
  the same pre-existing engine bug (§1.5).
- **C′** (coding, alternate model): trunk compiled it as one `task`, swe failed,
  run over in 34 s with nothing. chat-v2 compiled it as a `project` of ~10 parts,
  swe failed on all of them, and the revision pass re-issued the work as new nodes
  that did not inherit the `swe` choice — so it **kept building on `linear`**,
  three leaves at a time, several already complete (§6).

On the specific things suspected:

- **Parallel scheduling: not changed.** Every scheduling file is byte-identical
  across the window, and both arms measured *identical* concurrency on the same
  task (3 leaves in flight, the governor's floor).
- **swe routing: not regressed — improved.** Both arms routed the hard *coding*
  task to `swe` identically. On the *non-coding* analysis task trunk wasted `swe`
  (and crashed); chat-v2 correctly chose `linear` (and delivered). The "wasted swe
  allocation" the user suspected is real and lives on trunk.
- Every shape-affecting prompt change in the window pushes toward *more*
  parallelism, not less.

### 1.0 The efficiency regression — `abcc5a5`: the leaf's memory grew 10.5×, its wallet did not

`abcc5a5` ("the leaf remembers in the model's room, not in its wallet…") resized
the linear leaf's observation window. It used to be `maxTokens/6`; it is now
derived from the model's context length (`internal/exec/linear.go:461-470` →
`observationWindow`, `internal/exec/context.go:86-102`).

On the configured model this is a **10.5× increase**:

| | trunk | chat-v2 |
|---|---|---|
| observation window | `maxTokens/6` = 150 000/6 = **25 000 B** (~24 KB) | `min(ctx/2 − 36 000, …)×4 B` → **262 144 B** (256 KB cap) |
| leaf spend ceiling `chatLeafTokens` | **150 000** | **150 000 — unchanged** |

(`deepseek/deepseek-v4-flash` context = **1 048 576** tokens, read live from the
OpenRouter models API, so `usable = 1048576/2 − 4000 − 32000 = 488 288` tokens →
`×4 B` = 1.95 MB → clamped to `maxObservationBudget = 256 << 10`,
`internal/exec/context.go:21`. `chatLeafTokens = 150_000` at `cmd/aforge/chat.go:1907`
and `subharness-m1:cmd/aforge/chat.go:2832` — identical.)

The window is *per-request memory*. The ceiling is *cumulative spend*:

```go
// internal/exec/linear.go:1000-1002
func spent(outcome *Outcome) int {
	return outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
}
```

Every turn re-sends the whole window, and every turn's prompt tokens are added to
`spent()`. The stop test is `spent(outcome) >= l.maxTokens`
(`internal/exec/linear.go:698`). So a 10× larger transcript is billed against an
unchanged 150 000-token wallet **on every turn**, and the leaf exhausts its budget
in a small number of turns instead of many. It then overruns and the reconciler
issues an extension node (`-x1`), which overruns again (`-x2`, `-x3`)…

The wrap-up machinery makes this sharper, and it is **unchanged** in the window:
`wrapUpAt = 0.7` (`internal/exec/linear.go:982`) warns the leaf once it has spent
70 % of its ceiling — 105 000 tokens — and `landingTurns = 4`
(`internal/exec/linear.go:988`) reserves the tail. A chat-v2 leaf measured at
152 k tokens on its *first* call is past the wrap-up threshold before it has done
any work, so it spends its whole existence in landing mode. The thresholds were
calibrated for a 6 k-token turn and are now crossed by a single turn.

The commit's own comment states the intent — *"maxTokens stays what it is — the
spend ceiling the landing reserve and the wrap-up warning are measured against —
and stops sizing memory"* (`linear.go:466-468`). Decoupling the two was correct;
not re-scaling the ceiling to match a 10× memory is the defect. Prefix caching
reduces the *cost* of the re-sent bytes but not the *token count*, and `spent()`
counts tokens, not dollars.

**Measured, identical prompt, task P:**

| arm | leaves | extension nodes | wall | spend | outcome |
|---|---|---|---|---|---|
| trunk | 4 | **1** (`n2-x1`) | 264.2 s | $0.0746 | **failed** |
| chat-v2 | 4 | **7** (`n3-x1..x3`, `n4-x1,x2`, `task-2-x1,x2`) | **834.5 s** | **$0.2976** | **succeeded** |

Same graph shape, same 4 independent leaves, same concurrency — chat-v2 simply
cannot finish a leaf inside one budget, so it buys completion through repeated
extensions. Note this cuts both ways: the extensions are what let chat-v2 finish
where trunk gave up. The defect is that they should not have been necessary.
See §6 for the full timelines.

**Decisive evidence — per-node token usage, same prompt, task P** (`usage` table,
summed per node; `calls` is rows):

| node | trunk prompt tok | trunk calls | tok/call | chat-v2 prompt tok | chat-v2 calls | tok/call | amplification |
|---|---|---|---|---|---|---|---|
| `task-2-n1` | 48 093 | 2 | 24 k | 299 598 | 3 | 100 k | **4.2×** |
| `task-2-n2` | 397 097 | 3 | 132 k | 478 031 | 3 | 159 k | 1.2× |
| `task-2-n3` | 127 851 | 1 | 128 k | 492 538 | 3 | 164 k | 1.3× |
| `task-2-n4` | 27 139 | 1 | 27 k | 456 169 | 3 | 152 k | **5.6×** |
| extensions | 412 105 (1 node) | 3 | — | 2 099 043+ (7 nodes) | 14+ | — | **7× nodes** |
| **whole run** | **1 031 922** | 14 | — | **4 653 833** | 42 | — | **4.5×** |

**How firmly to read this.** What is *proven*: chat-v2 spent 4.5× the tokens and
needed 7 extension nodes where trunk needed 1, on a byte-identical prompt; the
observation window is arithmetically 10.5× larger on chat-v2; the leaf ceiling is
unchanged; parallelism is identical. What is *inferred*: that the window is the
dominant cause. The inference is strong but not airtight — trunk's `n2`/`n3` also
show 128-132 k tokens/call, so per-call size is not purely a function of the
window, and some of chat-v2's excess likely comes from leaves doing more retrieval
(see the contract-prompt note below). The clean signal is `n1` and `n4`, the two
nodes trunk finished cheaply (24 k and 27 k tokens/call) and chat-v2 did not
(100 k and 152 k): a 4-6× amplification on work trunk completed in one or two
calls. Treat the mechanism as established and the exact split between the two
contributing changes as unquantified.

Note that `tok/call` on chat-v2 (152-164 k) is
**above the entire 150 000-token leaf ceiling** — so such a leaf is into its
landing reserve after roughly one productive turn, which is precisely why five
extension nodes were needed where trunk needed one.

A second in-window change plausibly amplifies this rather than causing it:
`aeeb141` rewrote the contract prompt's toolbox sentence from *"a shell, file
writing, file editing, and web search"* to a list that also advertises background
jobs, **page fetching**, recall, sibling messaging and media tools
(`internal/plan/contract.go:25-52`). A method routed through more retrieval
produces more observation bytes — which the enlarged window then retains. The
window arithmetic is the primary cause; this is a likely multiplier.

**Honest caveat on the P measurement.** The two arms did not fail the same way,
and one of them did not fail at all. Trunk "settled" at 264 s with *"Nothing here
is the answer"*; chat-v2 spent 834 s and delivered all four briefs. So chat-v2's
extra wall time is not pure waste — the extensions are what carried the work to
completion. What is defensibly wrong is that they were *needed*: a leaf should be
able to write a 600-word brief inside one 150 000-token budget, and on trunk's
window three of the four did. The efficiency claim (4.5× tokens, 4.0× spend, 3.2×
wall) is solid; a claim that chat-v2 is *worse overall* on this task would not be.

One run is one run. This is a single task on a single model; the arithmetic in
§1.0 is what generalises, not the 3.2×.

---

### 1.1 Why parallelism is ruled out — the scheduling layer is byte-identical

Identical git blob hashes on both refs:

| file | blob (both branches) |
|---|---|
| `internal/exec/schedule.go` | `3003facd425cb99ced1e547ba5368a9126799d29` |
| `internal/exec/executor.go` | `4731f22cdecc2a05970f865fc2eca157bf6e8201` |
| `internal/exec/governor.go` | `3ff5a864a0f1c7222e7334fdcfb0e53a5b38aeab` |
| `internal/exec/jobs.go` | `3c23f36074db6607eed52b46349364424aa7ab0f` |
| `internal/exec/capped.go` | `93c54f9737fbdd19d5ab2a19c080d37c2e69a562` |
| `internal/exec/subharness.go` | `f309ade6f7fcc91b0f88b1bab229a87aca0b83d3` |
| `internal/plan/fanout.go`, `bind.go`, `bundle.go` | identical |
| `cmd/aforge/do.go` | identical |

`git log --oneline subharness-m1..chat-v2 -- internal/exec/schedule.go
internal/exec/executor.go internal/exec/governor.go internal/exec/jobs.go
internal/exec/capped.go internal/exec/subharness.go` returns **zero commits**.

`cmd/aforge/subharness_swe.go` — the swe registration, its purpose text, its
capacity anchors and its deadline shape — is also byte-identical. Nothing in the
window changed *what swe is for* or *how it is offered*.

Nor how it is *chosen*. There are two sources for a leaf's worker and both are
unchanged: the compiler's whole-job judgement (`Compiled.Subharness` →
`Reconciler.chosenSubharness`, `internal/resident/resident.go:402-407` → the
splice's `Subharness`, `resident.go:1275`) and the plan sizing pass's per-node
judgement (`internal/plan/size.go:441-442`). The settled answer is the node's own
with the splice's as fallback (`internal/store/store.go:372-376`,
`internal/store/splice.go:336-338`). Filtering the `internal/resident/resident.go`
diff for `subharness|worker|concurrentCommands` returns **nothing**.

One nuance that matters when reading the experiment results: the swe menu text
itself tells the model *not* to pick swe for well-specified work —
*"Do not choose it when the change is already located and specified — a
well-described edit in a handful of files is the default worker's job even when
it is a whole feature"* (`cmd/aforge/subharness_swe.go:37-42`). A tightly
specified coding task landing on `linear` is therefore the documented **correct**
behaviour, not a missed allocation.

### 1.2 `resident-m1` spot-check

`git diff --stat resident-m1..subharness-m1 -- internal/exec/ cmd/aforge/do.go`
is **empty** — the execution engine and the headless entry point are identical
across both trunks. The two differ only in `internal/head/`, `internal/plan/revise.go`
and `internal/resident/` (resident-m1 carries plan-sight, the confirm gate and
cancel-rethink that subharness-m1 lacks; they are parallel trunks, not a line).
`resident-m1` also carries the trunk `compileReplyTokens` floor of `1000`
(`internal/head/compiler.go:483`) and the same `GovernorMinInFlight = 3`
(`internal/exec/governor.go:48`). Nothing in §1 changes for resident-m1.

### 1.3 What actually changed in the window, and in which direction

Three commits touch the shape of headless work. All three widen it.

| commit | change | direction |
|---|---|---|
| `51287ca` head: the workforce is wide… | `builds_on` tightened from "continues, improves, or **refers to** earlier work" to "**NEEDS** an earlier job's result to start", plus a new rule: *"builds_on is a WAIT… If two workers who never spoke to each other could do both at once… builds_on is []."* | **more parallel** |
| `d2e02d7` head: the reading decides the shape… | adds `structure` as the first JSON field of the compile reply and `reconcileScale` (`internal/head/compiler.go:572-583`), which promotes `task`→`project` when structure is `enumerates`/`stratifies`. Only ever widens. | **more parallel** |
| `d2e02d7` (same commit) | `compileReplyTokens` floor raised `1000` → `8000` (`internal/head/compiler.go:535`) | **fixes total compile failure — see §1.4** |

`51287ca`'s own commit message documents the serialization bug it fixes: a second
job sat pending **8.67 s** and was claimed **1 ms** after the first completed, via
an invented `task-8 -> task-22 (feeds_into)` edge tripped by the word *"also"*.
**That bug is present on trunk and fixed only on chat-v2.**

### 1.4 A third defect — and this one is on TRUNK, not chat-v2

`internal/head/compiler.go:483` on `subharness-m1`:

```go
func compileReplyTokens(instruction string) int {
	const floor = 1000
	echo := 2 * len(instruction) / 3
	return floor + echo
}
```

`max_tokens` is the whole completion budget and reasoning is spent out of it
first. On `~deepseek/deepseek-v4-flash-latest` — the user's configured default
(`internal/config/config.go:25`, and `chat_model`/`task_model` in
`~/.aforge/settings.json`) — three ordinary multi-part asks came back
`finish_reason:"length"`, `content:null`, with `completion_tokens` equal to the
cap **to the digit** (1106/1106, 1100/1100, 1124/1124). The retry at double room
did the same. The compile is the one call whose loss forfeits the entire job.

chat-v2 raises the floor to 8000 (`internal/head/compiler.go:535`). Measured
successful compiles land between 827 and 5565 completion tokens.

**Implication for the user's perception:** if the comparison was run on this
model, trunk is the arm that cannot reliably compile multi-part work at all.

### 1.5 A fourth defect — the swe worker cannot start on the default model (BOTH branches)

Found by the live battery, and it is the most actionable thing in this audit.

The hard coding task was allocated `swe` on **both** arms — the allocation logic
works exactly as designed — and then the swe worker died at startup in 5-7 s
having spent $0.0007, on both binaries, with a byte-identical error:

```
chat-v2  "· understood · 1 task (swe) 5s" → ✗ 5.5s   {"subharness":"swe", "spend":0.00071386}
trunk    "· understood · 1 task (swe) 6s" → ✗ 7.1s   {"subharness":"swe", "spend":0.00033048}

deliverable: "…the coding pipeline crashed: models.dev: model
              \"deepseek/deepseek-v4-flash-20260423\" not found for provider \"openrouter\""
```

**Root cause: `engineModelID` over-resolves the model id.**

`cmd/aforge/subharness.go:114-120` hands the swe engine
`catalog.Concrete(model)`, and `internal/catalog/catalog.go:216-226` rewrites the
id to OpenRouter's `canonical_slug`. The swepro engine then prices its calls from
a *different* catalog — models.dev (`internal/swepro/internal/modelsdev/models.go:84`
raises this exact error).

Queried live from the pod, models.dev's `openrouter` provider (342 models) carries:

```
deepseek/deepseek-v4-flash            ← present
deepseek/deepseek-v4-flash-0731       ← present
~deepseek/deepseek-v4-flash-latest    ← present, WITH the tilde
deepseek/deepseek-v4-flash-20260423   ← ABSENT
```

So the id aforge starts with is one models.dev knows, and `Concrete()` converts it
into one models.dev does not. The resolution *causes* the failure it was written
to prevent.

**Note: you are already fixing half of this.** The uncommitted working tree adds
`Model.AliasTarget` to `internal/catalog/catalog.go` and resolves through it before
`CanonicalSlug`. That lands the *default* model on `deepseek/deepseek-v4-flash-0731`,
which models.dev does carry — so the alias case is solved. The `CanonicalSlug`
fallback still breaks explicitly-named concrete models. Details and the remaining
change in **F1**.

The prior bug is instructive: `engineModelID`'s own doc comment
(`cmd/aforge/subharness.go:98-102`) cites `models.dev: model
"deepseek/deepseek-v4-flash-latest" not found`. That failed because
`catalog.normalizeID` **strips the leading `~`**, and models.dev indexes the alias
*with* the tilde. The tilde-strip was the bug; `Concrete()` was the wrong fix for
it, and it swapped one unknown id for another.

**And the model id is not the only thing wrong with the swe engine.** A follow-up
run of task C on a *different* model, `deepseek/deepseek-chat-v3.1` — chosen
precisely to get past the models.dev lookup — cleared that error and hit a second
one on trunk:

```
{"deliverable":"…the coding pipeline crashed: entry agent returned no result",
 "spend":0.0101, "seconds":33.7, "subharness":"swe"}
```

Two cautions on that second failure. `internal/swepro/` is **byte-identical across
the window** (`git diff --stat subharness-m1..chat-v2 -- internal/swepro/` is
empty), so nothing in the vendored engine changed. And the error itself is raised
at `internal/swepro/codeaf/pipeline.go:777-779` when the entry turn returns empty
text *and* no parts — which is the same empty-completion shape as §1.4, and is
plausibly nondeterministic. On the same alternate model **chat-v2's swe leaves
failed too — twelve of them, consecutively** (§6) — so this must not be read as
"chat-v2 fixed the engine". What differed is that chat-v2 had somewhere to fall
back to.

**Corrected scope of the swe defect.** An isolated probe on trunk with
`deepseek/deepseek-chat-v3.1` ran the swe pipeline **end to end in 54 s, writing
and compiling real code**. So swe is not fundamentally broken, and the defect is
precisely bounded: `Concrete()` breaks exactly those models that have a **dated
snapshot** in aforge's catalog. `deepseek/deepseek-v4-flash` → `-20260423` ✗;
`-0731` → `-20260731` ✗; `v3.2` → `-20251201` ✗; `deepseek-chat-v3.1` has no dated
canonical, passes through unrewritten, and **works** ✓. That is a clean,
deterministic rule and it is what F1 must fix. The remaining C′ failures on v3.1
are a separate and apparently flaky issue.

`cmd/aforge/subharness.go`'s `engineModelID` is identical on both branches (the
window's only hunks there are `WithContextLength` and the trace path), which the
identical failure on both arms confirms empirically. **Not a regression — a live
P0.** But it is very likely a large part of what reads as "swe allocation
regressed": swe *is* allocated, dies in five seconds, and returns an error string
as the deliverable.

---

### 1.6 A fifth defect — "linear" and "no opinion" are the same value, so the sizer can never override a job-level `swe`

Found by inspecting what the C′ run actually recorded, and it explains why twelve
leaves ran on a worker nothing had chosen for them.

Measured, chat-v2's C′ store:

```
plan_graph events, every node's sizing verdict:   subharness = ''   (distinct set is {''} — all of them)
nodes table, the twelve leaves that ran:          subharness='swe', splice_subharness='swe'
```

**The per-node sizing pass judged every node generalist, and every node ran on
`swe` anyway.** The mechanism, three links:

1. `internal/plan/size.go:441-444` records a verdict **only** if it passes
   `KnownSubharness`:
   ```go
   if KnownSubharness(verdict.Subharness) {
       node.Subharness = strings.TrimSpace(verdict.Subharness)
   ```
2. `KnownSubharness` is **false for `linear`** — by design and by doc comment
   (`internal/plan/size.go:177-184`): *"Empty and \"linear\" are the baseline rather
   than a specialist, so both answer no."* Same in `internal/exec/subharness.go:150-159`.
   So a node the sizer explicitly judged `linear` keeps `Subharness = ""`.
3. `internal/store/splice.go:336-338` then reads `""` as **inherit the job-level
   choice**:
   ```go
   subharness := strings.TrimSpace(node.Subharness)
   if subharness == "" {
       subharness = strings.TrimSpace(payload.Provenance.Subharness)
   }
   ```

The job-level choice came from the compiler (`Compiled.Subharness` →
`resident.go:402-407` → `resident.go:1275`), which said `swe` for the whole
coding job. So every leaf inherited `swe`.

**`linear` is encoded as the empty string, and the empty string already means "no
opinion".** The two collide, and the collision is a one-way ratchet: a per-node
verdict can promote a leaf to a specialist but can never pin one back to the
generalist. There is no way for the sizing pass to say "this particular node is
ordinary" once the job carries a specialist.

This is not a regression — the collision is identical on `subharness-m1` — but it
is the single most consequential thing found about harness selection, and it is
directly load-bearing for any redesign that wants per-atom worker choice. It also
compounded the swe engine failure: all twelve leaves were routed to a worker that
could not start, and only the retry path (which re-authors nodes with an explicit
empty inheritance) reached `linear` and succeeded.

**Fix (F1b):** give the baseline a distinct spelling at the verdict layer — either
a sentinel (`"linear"` recorded verbatim, with `KnownSubharness` kept as the
"is a specialist" predicate but a separate `IsVerdict` used at `size.go:441`), or a
`SubharnessSet bool` beside the name so "set to linear" is distinguishable from
"unset". Then `splice.go:336` inherits only when genuinely unset. Add a test that a
node sized `linear` inside a job spliced as `swe` runs on `linear`.

---

## 2. `exec` vs `do`

**There is no `exec` subcommand on any trunk branch, and there never was one on
the trunk lineage.** `internal/exec` is the engine *package* only.

`exec` existed as a CLI verb on exactly one branch — `origin/exec-harness-mode`
@ `064ae9e` ("exec: one-shot harness mode with honest exit codes, plus version
subcommand", Aug 6), with `cmd/aforge/exec.go` and this dispatch:

```
git show origin/exec-harness-mode:cmd/aforge/main.go
  case "plan" / "revise" / "run" / "exec" / "show" / "version"
  aforge exec ["<prompt>"] [-w dir] [--system text] [-turns N] [-budget N] [--json] [-o file]
```

`git merge-base --is-ancestor origin/exec-harness-mode <ref>` is **false** for
`subharness-m1`, `resident-m1`, `chat-v2` and `master`. The branch was never
merged.

Semantically the two are different things, which is probably what is being
remembered: `aforge exec` was a **raw one-shot harness loop** — a prompt straight
into the linear agent, no planning, no graph. `aforge do` is the **full task
algorithm** — compile → plan graph → splice → parallel leaf execution → gate.

Subcommand list is identical on both experiment binaries: `chat, do, plan,
revise, run, show, models, notebook, competence, services, wake, doctor,
rebuild, why, help`. Graph execution is spelled `run` (`cmd/aforge/main.go:106`).
The only help-text difference in the whole window is that chat-v2's `plan` gained
`[-w dir]`.

---

## 3. Do chat and `do` share the same task algorithm?

**Yes. Same engine, one constructor, no fork.** The chat-v2 rebuild is a
*rendering* fork only.

Both paths converge on `buildBrain` (`cmd/aforge/chat.go:172`):

```
do:        main.go:100 case "do" -> runDo (do.go:109) -> doErrand (do.go:165)
             do.go:182  openChatWindow(path, path, session)
             do.go:195  graph.RequestCommand(store.Command{Kind: store.CommandSplice, ...})
             do.go:220  headlessBrain(...) -> do.go:276 buildBrain(window, session,
                                              brainOptions{headless:true, ...})
             do.go:228  brain.start() (chat.go:1451) -> reconciler.Serve + runner.Serve
             do.go:238  settlementWatch.wait(ctx) — polls the journal for settlement

chat --v2: main.go:96  -> runChatV2 (chatv2.go:98)
             chatv2.go:151 openChatWindow(...)                [same call]
             chatv2.go:157 newChatResidency(window)           [residency.go:113]
               residency.go:140 -> buildChatBrain (chat.go:160) -> buildBrain(...)
               residency.go:145 -> brain.start()              [SAME chat.go:1451]
             chatv2.go:192 chat.Run(...)                      [tui2 surface only]
```

`cmd/aforge/do.go:38` states it outright: *"from that command onward every
mechanism is the one a chat window drives, because it is literally the same
construction."* And `chat.go:1281-1283`: *"A headless run has no surface to reach
anything… The brain above is complete and identical either way."*

Everything execution-relevant is identical:

| concern | `do` | chat / chat --v2 | site |
|---|---|---|---|
| max parallel leaves | `chatWorkerCeiling = 32` | identical | `chat.go:476`, applied `chat.go:1268` |
| load governor | `executor.HostGovernor()` | identical (process-global) | `resident/runner.go:100` |
| subharness registry | same table | same table | `main.go:82 installSubharnesses()` runs **before** dispatch; `subharness.go:40` |
| leaf envelope | `chatLeafTurns=200`, `chatLeafTokens=150_000` | identical | `chat.go:1906-1907` ("mirror the headless run defaults exactly") |
| scale gate | `chat.go:2907` | same line | trunk `chat.go:5003` — unchanged |
| daily budget rail | same | same | `chat.go:1268` |

`headless: true` removes only the conversational half — head, voice, narrator,
arrival brief, standing watch, TUI commander (`chat.go:1284` early return) — plus
`compiler.WithOneShotErrands()`, which changes *surface* classification and
explicitly not the judgement of the work (`internal/head/compiler.go:228-237`).

This was the chat rebuild's stated premise, not an accident. Its own design doc,
`audit-notes/chat-rebuild.md`:

- line 6: *"The headless task path (`aforge do`) is **healthy and stays**; the chat
  surface and the head's split brain are what get rebuilt."*
- line 33-37: *"Chat vs headless converge below the head (`brainOptions`): same
  reconciler, runner, compile→plan→splice, consent desk. `aforge do` writes the
  identical splice command the head would… **Keep this too.**"*
- line 117 (Decision 7, the lens law): *"chat and headless differ only in where the
  task comes from. A feature that behaves differently in one surface is a bug in
  the seam."*

And the product law it points at, `docs/ARCHITECTURE.md:162-175`:

> **Decision 7 — The conversation is a lens on the brain, not the brain.**
> There is one brain… `aforge chat` is that brain with a head and a terminal;
> `aforge do` is the same construction with the conversation removed, and the
> seam between them is exactly one thing: where the task comes from. …From that
> command onward nothing downstream can tell which surface produced it, because
> it is literally the same code.

The byte-identical `do.go` and the shared `buildBrain` are that law honoured. Note
the same passage names the *other* engine explicitly — `plan`/`run`, "compiles a
graph to a file and executes what the file says" — which is the one path that does
**not** share this brain, and the one `aforge exec` most resembled.

### Three genuine asymmetries (not regressions, but worth knowing)

1. **`do` bypasses the head entirely.** `do` writes exactly one `CommandSplice`
   (`do.go:195`) → one command → one subtree. Chat's head can split one message
   into up to `fanOutLimit = 6` independent work orders
   (`internal/head/spawn.go:78`, `internal/head/head.go:983`), each its own
   subtree. So chat has one extra top-level parallelism tier that `do` structurally
   cannot have. Past 6 orders the message collapses to a single order and the
   compiler is expected to call it `enumerates`.
2. **`--subharness` is `do`-only.** `do.go:121` → `reconciler.WithSubharness(forced)`
   (`chat.go:443-445`). Chat always lets the compiler choose. This is the
   benchmarking door and the cleanest way to A/B workers.
3. **`do` cannot be driven through the head.** There is no production flag, env
   var or stdin protocol that drives the chat task path headlessly.
   `runChatV2` accepts `--db --session --linear --color --nerd-font` only and
   errors on any positional arg (`chatv2.go:131`). `chat.Options.Input/Output`
   exist (`internal/tui2/chat/app.go:102-104`) but `runChatV2` never sets them
   (`chatv2.go:192-202`) — a test seam, not a CLI door. Runtime verification of
   the chat path was therefore not feasible; §3 is proven statically.

---

## 4. Where parallelism actually comes from (and where it can be lost)

Correcting a common assumption: **headless `do` does not use `exec.Scheduler`.**
`exec.Scheduler` is reachable only from `aforge run <graph.json>`
(`cmd/aforge/run.go:237`). `do` and chat both use `resident.Runner`.

- Fan-out point: `internal/resident/runner.go:394` (goroutine per claimed node).
- Bound: buffered channel `r.slots` (declared `runner.go:65`, sized `runner.go:97`), 32 wide.
- Governor gate: `runner.go:365` `if !r.governor.Admit(len(r.slots) - 1)`.
- Readiness: `store.Ready(0)` — `internal/store/query.go:445`.
- Independent splices planned 4-wide: `internal/resident/resident.go:784`
  `concurrentCommands = 4` (trunk `resident.go:771`, unchanged).

**There is no environment variable that limits leaf concurrency.** Sweeping every
`AFORGE_*` name read anywhere in `internal/` and `cmd/` yields no worker/parallelism
knob at all — the nearest are `AFORGE_MAX_DEPTH`, `AFORGE_NODE_BUDGET` and
`AFORGE_SPINE_SAMPLES`, which shape the graph rather than throttle the runner, and
`AFORGE_DAILY_BUDGET`, which halts rather than serializes. So "someone set a
variable" is ruled out as an explanation: the ceiling is the compiled-in 32.

**The governor cannot serialize.** `internal/exec/governor.go:92`, first check:
`if inFlight < GovernorMinInFlight { return true }`, with
`GovernorMinInFlight = 3` (`governor.go:48`). Unconditional. The comment at
`:33-47` records that the floor used to be 1 and that exactly this bug — three
independent single-leaf jobs running strictly one after another — is why it was
raised. On Linux it reads `/proc/loadavg` field 0 with no cgroup awareness
(`loadavg_linux.go`), divided by `runtime.NumCPU()`; ceiling 1.5, resume 1.2. An
unreadable `/proc/loadavg` admits (`governor.go:106-109`). **A pod that reads
high load holds at 3 leaves in flight, never at 1.**

So the only structural serializers are, in order of likelihood:

1. **The scale gate** — `cmd/aforge/chat.go:2907`:
   `if compiled.Scale != head.ScaleProject { ... return one-node subtree }`.
   Non-project scale means *one leaf*, hence zero parallelism and no sizing pass.
   Present verbatim on trunk at `chat.go:5003`. `d2e02d7`'s `reconcileScale` is
   the mitigation, and it exists only on chat-v2.
2. **`builds_on` edges invented by the compiler** — fixed by `51287ca`, on
   chat-v2 only.
3. **Plan-layer `bind`/`fanout` prompt disobedience** — `internal/plan/bind.go:74-77`
   (gatherer exception) and `internal/plan/fanout.go:72-78` (no merge/summary
   part). Both files byte-identical between branches; this is a **pre-existing**
   weakness, not a window regression. `audit-notes/chat-rebuild.md:4528-4551`
   records a measured instance: a 71 s + 22 s serial synthesis tail against a
   ~70 s parallel section.

---

## 5. Half-wired features found (not regressions; incomplete landings)

Both are inert-by-default and preserve historical prompt bytes, so neither can
have caused a regression — but both are load-bearing features that are declared
and never switched on.

### 5.1 `Terrain` — the planner still plans blind in `do` and chat

`internal/plan/terrain.go` (857 lines, new), `plan.Options.Terrain`
(`plan/plan.go:191-203`), `Graph.Terrain` (`plan/graph.go:207`), threaded into
spine (`plan/spine.go:112`), ground (`plan/ground.go:210-238`), panel and expand.

**Wired at exactly one call site:** `cmd/aforge/main.go:285` — `aforge plan -w`.

The two `plan.Build` calls that `do` and chat actually use carry a placeholder
comment instead:

```
cmd/aforge/chat.go:2955   // terrain wiring lands here (world-grounded-planning handoff)
cmd/aforge/chat.go:3142   // terrain wiring lands here (world-grounded-planning handoff)
```

(placeholder introduced by `6c96172`). A sweep of every local and remote ref
(`git show <ref>:cmd/aforge/chat.go | grep plan.RenderTerrain`) finds the wiring
on **no branch at all** — `git log --all -S RenderTerrain -- cmd/` returns only
`d321d14`, which wired `aforge plan` and nothing else. `terrainBlock("")` returns `""`
(`plan/ground.go:225-227`), so every non-`aforge plan` prompt is byte-identical to
trunk. Net effect: **`aforge do -w <dir>` never shows the planner the workspace it
is about to work in.** The spine's own doc comment says why that matters:
*"a goal whose first stage is 'gather the responses' is one stage shorter when the
responses are sitting in the workspace already"* (`plan/spine.go:105-110`) — i.e.
missing terrain buys **extra serial stages**.

### 5.2 `FileShaped` — the delivery-law carve-out is never selected

`Graph.FileShaped` (`plan/graph.go:185-200`), `plan.Options.FileShaped`
(`plan/plan.go:226-229`), `plan.DeliveryLaw` / `DeliverToNamedFile`
(`plan/delivery.go`), consumed by `deliverableLineFor` (`plan/brief.go:244`) and
`contractDeliverableLineFor` (`plan/contract.go:239`).

`grep -rn FileShaped cmd/ internal/ | grep -v _test.go` shows **no site anywhere
sets it**. It is `false` on every run, so `DeliverInMessage` always applies. The
declared intent (`plan/graph.go:195-199`) is that the chat session sets it from
the same judgement its delivery gate already makes.

Consequence: on a file-shaped ask ("write X to `report.md`", "change this repo"),
the deliverable owner is still told to put the finished thing *in its reply* and
never to file it — the exact gap `plan/delivery.go:10-16` documents as having
killed a task over four gate rounds.

---

## 6. Experiment results

Pod: RunPod Ubuntu 20.04, 16 cores, 503 GiB RAM, x86_64, no GPU.
Binaries cross-compiled from `61923d7` (chat-v2) and `5d64d88` (subharness-m1).
Model, verbatim: `deepseek/deepseek-v4-flash` via OpenRouter.
Per-arm `AFORGE_HOME`, `AFORGE_DAILY_BUDGET=60`, `AFORGE_PRACTICE_BUDGET=0`,
fresh store + workspace per run, at most one run per arm at a time.
Evidence per run: `nodes.subharness`, `nodes.started_at`, `nodes.finished_at`
from the durable store, plus `--json` stdout and full stderr.

**A discarded first round, disclosed.** The initial task-C launch put both arms'
stores in one directory. aforge writes `resident.lock`, `cas/`, `craft/` and
`scratch/` *next to the .db*, so the two arms shared a resident lease: chat-v2 hung
six minutes with two events, no network sockets and a pending splice, while trunk
ran normally. That looked like a dramatic chat-v2 regression and was **a harness
fault, not a finding**. Every run reported here uses `/root/stores/<arm>-<task>/`.
The bad round is archived at `/root/round1/` and is excluded from all numbers
above. Worth noting as a live hazard in its own right: two aforge processes pointed
at one store directory silently contend on the lease.

Environment hygiene, verified: the pod sets only `OPENROUTER_API_KEY`,
`AFORGE_MODEL`, `AFORGE_HOME`, `AFORGE_DAILY_BUDGET`, `AFORGE_PREAUTHORIZE_SPEND`,
`PATH`, `GOTOOLCHAIN`. None of `AFORGE_MAX_DEPTH`, `AFORGE_NODE_BUDGET`,
`AFORGE_SPINE_SAMPLES` or `AFORGE_FAKE_SWE` is set, so both arms run on compiled-in
defaults and neither graph shape nor the swe worker is stubbed.

### Summary table

| task | binary | harness chosen | nodes | max concurrent leaves | parallelism observed | wall | spend | outcome |
|---|---|---|---|---|---|---|---|---|
| **C** coding (RB-tree + tests) | trunk | **swe** ✓ | 1 | 1 | n/a (single leaf) | 7.1 s | $0.0003 | ✗ swe engine crash |
| **C** coding | chat-v2 | **swe** ✓ | 1 | 1 | n/a (single leaf) | 5.5 s | $0.0007 | ✗ swe engine crash |
| **N** analysis (Paxos/Raft/VR) | trunk | **swe** ✗ *wasted* | 1 | 1 | n/a (single leaf) | 82.3 s | $0.0146 | ✗ failed |
| **N** analysis | chat-v2 | **linear** ✓ | 1 | 1 | n/a (single leaf) | 114.7 s | $0.0185 | ✓ delivered |
| **P** 4 independent briefs | trunk | **linear** ✓ | 6 | **3** | ✓ 3 leaves within **8 ms** | 264.2 s | $0.0746 | ✗ abandoned |
| **P** 4 independent briefs | chat-v2 | **linear** ✓ | 12 | **3** | ✓ 3 leaves within **8 ms** | 834.5 s | $0.2976 | ✓ delivered |
| **C′** coding, alt model `deepseek-chat-v3.1` | trunk | **swe** (scale=`task`) | 1 | 1 | n/a (single leaf) | 33.8 s | $0.0101 | ✗ swe crash, run over |
| **C′** coding, alt model | chat-v2 | **swe** → failed → **linear** ✓ | ~20 (scale=`project`) | **3** | ✓ 3 leaves within **9 ms** | **592.4 s and still working** when the audit stopped it (`rc=143`, SIGTERM) | — | ✓ leaves completing steadily |

Parallelism ratio (sum of leaf seconds ÷ wall): **trunk-P 1.32, chat-v2-P 1.58** —
both comfortably above 1.0, i.e. genuine concurrency in both arms. chat-v2's C′ run
reached $1.1677 spend across 26 nodes before it was stopped.

Parallelism is identical between arms wherever it could be observed. Harness
choice is identical on the coding task and *better on chat-v2* on the analysis
task. The cost/latency gap on P is §1.0.

### Task C — hard coding task (red-black tree in Go, property tests)

| arm | nodes | scale | harness | outcome | wall | spend |
|---|---|---|---|---|---|---|
| chat-v2 | 1 | task | **swe** | swe engine crashed at startup | 5.49 s | $0.00071 |
| trunk | 1 | task | **swe** | swe engine crashed at startup | 7.08 s | $0.00033 |

Identical on both arms, including the error string. See §1.5 — swe allocation is
correct and unchanged; the swe *engine* cannot start on this model on either
branch. No parallelism signal available from this pair (one node each).

**H2 (swe allocation) answered here: no regression.** A hard coding task is
routed to `swe` identically on trunk and chat-v2.

### Task P — decomposes into 4 independent briefs (the parallelism test)

Node timelines, verbatim from `nodes` (id, stage, status, subharness, start, finish):

**trunk** — wall 264.18 s, spend $0.0746, 6 nodes, `subharness: linear`
```
task-2-n1  1 done ''  14:44:28.735 → 14:44:56.998   (28.3s)
task-2-n2  1 done ''  14:44:28.739 → 14:46:23.520  (114.8s)
task-2-n3  1 done ''  14:44:28.743 → 14:45:12.949   (44.2s)
task-2-n4  1 done ''  14:44:57.003 → 14:45:24.245   (27.2s)
task-2-n2-x1 1 done '' 14:46:23.524 → 14:48:38.272 (134.7s)   ← 1 extension
task-2     2 done ''  14:48:38.280 → 14:48:40.174    (1.9s)   ← Deliver together
```

**chat-v2** — wall 834.57 s, spend $0.2976, 12 nodes, `subharness: linear`, **succeeded**
```
task-2-n1  1 done ''  14:44:31.734 -> 14:45:42.810   (71.1s)
task-2-n2  1 done ''  14:44:31.737 -> 14:46:38.744  (127.0s)
task-2-n3  1 done ''  14:44:31.742 -> 14:47:16.167  (164.4s)
task-2-n4  1 done ''  14:45:42.815 -> 14:47:52.299  (129.5s)
task-2-n3-x1 1 done '' 14:47:16.171 -> 14:49:14.049 (117.9s)   <- 7 extensions
task-2-n4-x1 1 done '' 14:47:52.304 -> 14:50:02.414 (130.1s)
task-2-n3-x2 1 done '' 14:49:14.057 -> 14:51:26.491 (132.4s)
task-2-n4-x2 1 done '' 14:50:02.423 -> 14:51:39.079  (96.7s)
task-2-n3-x3 1 done '' 14:51:26.499 -> 14:52:16.514  (50.0s)
task-2     2 done ''  14:52:16.521 -> 14:53:07.352   (50.8s)   <- Deliver together
task-2-x1  1 done ''  14:53:07.358 -> 14:56:30.415  (203.1s)   <- delivery itself extended twice
task-2-x2  1 done ''  14:56:30.426 -> 14:58:10.595  (100.2s)
```

Deliverable, chat-v2: *"All four briefs are complete and within the acceptable
range... My final message delivers the full content of all four briefs."* followed
by all four briefs in full. Deliverable, trunk: *"The work was still mid-flight
when time ran out: it had split into further pieces that never finished. Nothing
here is the answer."*

**H1 — parallelism: identical, no regression.** Both arms started `n1`, `n2`, `n3`
within **8 milliseconds** of each other, and in both arms `n4` started ~5 ms after
`n1` finished (trunk: n1 done .998, n4 start 57.003; chat-v2: n1 done 42.810, n4
start 42.815). That is **max 3 concurrent leaves in both arms** — exactly
`GovernorMinInFlight = 3` holding under pod load, precisely as §4 predicted, with
the 4th leaf admitted the instant a slot freed. Independent leaves overlap; nothing
is queued that could run.

**H2 — no swe waste:** every node in both arms is `subharness = ''` (linear). The
non-coding task correctly never touched the coding pipeline, on either branch.

**H3 — the difference is extension churn**, 1 vs 7 extension nodes and 4.5× total
tokens — and, decisively, chat-v2 delivered the answer while trunk abandoned the
task. See §1.0.

### Task N — hard non-coding analysis (Paxos vs Raft vs Viewstamped Replication)

**This is the run that inverts the swe suspicion.**

| | trunk | chat-v2 |
|---|---|---|
| harness chosen | **`swe`** — wasted on a prose/analysis task | **`linear`** — correct |
| outcome | **failed** (`rc=1`) — swe engine crashed (§1.5) | **succeeded** (`rc=0`) |
| wall | 82.3 s | 114.7 s |
| spend | $0.0146 | $0.0185 |
| nodes | 1 | 1 |

trunk stderr: `· understood · 1 task 7s` → `✗ Consensus algorithm compari… 1m22s`,
`"subharness": "swe"`, deliverable = the models.dev crash string.

chat-v2 `"subharness": "linear"`, and its deliverable records the delivery gate
doing its job: *"The gap identified by the reviewer was that the deliverable
message contained only a summary and a pointer…"* — i.e. `DeliverInMessage`
(`internal/plan/delivery.go`, `aeeb141`) caught a pointer-only answer and forced
the full content into the message.

The swe menu explicitly forbids exactly what trunk did — *"nor when the
deliverable is prose or analysis about code rather than a change to it"*
(`cmd/aforge/subharness_swe.go:37-42`). **So the "unnecessary swe allocation" the
user suspected of chat-v2 is real, and it is on trunk.** chat-v2 got it right.

**Attribution — it is the compiler's judgement, not the sizing pass.** Node rows:

```
trunk  N:  ('task-2', subharness='swe', splice_subharness='', stage=1, status='failed')
chatv2 N:  ('task-2', subharness='',    splice_subharness='', stage=1, status='done')
```

Task N compiled at `task` scale, so there is no plan graph and no sizing pass —
`cmd/aforge/chat.go:2907` returns a one-node subtree. The only thing that can name
a worker on that path is `Compiled.Subharness`, the compiler's whole-job answer,
carried by `Reconciler.chosenSubharness` (`internal/resident/resident.go:402-407`)
onto the subtree (`resident.go:1275`). So the difference is entirely attributable
to the **compiler prompt**, which is exactly what `51287ca` and `d2e02d7` changed
and the only thing that differs here.

Caveat: n=1 per arm and the choice is an LLM judgement, so this is a strong signal
rather than proof.

### Task C rerun on an alternate model (`deepseek/deepseek-chat-v3.1`)

Re-run to get past the models.dev bug. It became the most informative run of the
battery, because the two arms produced **completely different graph shapes**.

**trunk** — one node, `scale=task`, failed, done in 33.8 s:
```
task-2  1 failed swe  15:01:32.062 -> 15:01:56.139   (24.1s)   ← that is the entire run
```

**chat-v2** — a full **project** graph, ~20 nodes over 3 stages:
```
task-2-n1   1 failed swe  15:02:04.552 -> 15:02:42.808   Tree structure
task-2-n2   1 failed swe  15:02:42.812 -> 15:02:48.425   Get operation
task-2-n5   1 failed swe  15:02:42.817 -> 15:02:53.942   Iterator implementation
task-2-n9   1 failed swe  15:02:42.821 -> 15:02:50.928   InsertMethod
task-2-n10  1 failed swe  15:02:48.429 -> 15:02:56.974   RebalanceLogic
task-2-n11  1 failed swe  15:02:50.931 -> 15:02:59.726   ColorAdjustment
task-2-n12  1 failed swe  15:02:53.947 -> 15:03:02.418   DeleteMethod
task-2-n13  1 failed swe  15:02:56.978 -> 15:03:06.620   SuccessorLogic
task-2-n14  1 failed swe  15:02:59.730 -> 15:03:11.729   RebalanceDelete
task-2-n6   2 failed swe  15:03:11.734 -> 15:03:19.156   InvariantChecker
task-2-n7   2 failed swe  15:03:19.161 -> 15:03:25.384   VersionImmutabilityTest
task-2-n8   2 failed swe  15:03:25.389 -> 15:03:33.217   TestOrchestrator
task-2-n16  1 done   ''   15:03:02.422 -> 15:04:17.249   Tree structure — Define th…
task-2-n17  2 done   ''   15:03:06.624 -> 15:08:17.916   InsertMethod — Implement t…
task-2-n19  2 done   ''   15:04:17.258 -> 15:06:40.845   RebalanceLogic — Implement…
task-2-n18  2 running ''  15:03:33.222 -> …              Iterator implementation
task-2-n21  2 running ''  15:06:40.852 -> …              DeleteMethod
task-2-n22  2 running ''  15:08:17.924 -> …              SuccessorLogic
```

Four things this shows, all of them load-bearing:

1. **`reconcileScale` widening, observed.** trunk compiled the coding task as
   `task` → one leaf. chat-v2 compiled it as `project` → ~10 named parts across
   three stages. This is `d2e02d7` doing exactly what it says (§1.3), on a real
   task, and it is the difference between "one worker grinds at the whole thing"
   and "the work is laid out".
2. **The swe engine failed on every leaf it touched** — 12 consecutive `failed`
   rows with `subharness='swe'` on chat-v2, plus 1 on trunk, plus the 2 default-model
   crashes earlier. **Across the scored battery: 16 swe attempts, 0 successes** — 12
   here, 1 on trunk's C′, 1 on trunk's N, 1 on each arm of the default-model C.
   **But swe is not fundamentally broken**: an isolated probe on
   `deepseek/deepseek-chat-v3.1` ran the pipeline end to end in 54 s and wrote and
   compiled real code. So the engine works when it is handed a model id the
   foreign catalog accepts — see the corrected scope in §1.5.
3. **Recovery onto `linear` happened — but by accident, not by design.** The
   repeated titles tell the story: `Tree structure` failed on swe as `n1`, then
   succeeded on `linear` as `n16`; `RebalanceLogic` failed as `n10`, succeeded as
   `n19`. But the replacement nodes are **not retries**: `retry_of=''`,
   `origin='self'`, and `intent='revision: Node 1 failed to produce any result due
   to a crash…'`. The sentinel/revision pass wrote **brand-new nodes** from a
   model-authored description of the failure, and spliced them with
   `splice_subharness=''` — so they simply did not inherit the job's `swe` choice
   and landed on the generalist by default. There is no explicit "swe failed, try
   linear" policy anywhere; `executorFor` only degrades for a worker this build
   *cannot construct*, and swe was perfectly constructible. Two consequences: the
   recovery is luck rather than a guarantee, and it is what loses the spec detail
   (see §6, "Task specs"). trunk's single-node graph had no such path at all —
   one swe failure ended the run.
4. **Concurrency is 3 again, on both.** `n2`, `n5`, `n9` all start at
   `15:02:42.81x` — within **9 ms**. Same `GovernorMinInFlight = 3` floor as task P.

This run is the clearest illustration of the audit's overall finding: chat-v2's
window changes make the system lay work out and keep going, while trunk's
narrower compile plus a single failure ends the job with nothing.

**Status at close of audit:** chat-v2's C′ run was **terminated by this audit**, not
by the harness — `rc=143` (SIGTERM) at **592.4 s**, during cleanup. At that point it
read `still waiting: 6 tasks pending, 3 running` with `✓ Iterator implementation`
just landed, so it was still making progress and would have run longer. It is the
one run in the battery without a natural finish, and its wall time is therefore a
floor, not a result. Its graph shape, harness sequence and concurrency — the things
it was run for — were already unambiguous. For comparison, the trunk arm had been
over for nine minutes with a single failed node and nothing to show.

### Task specs as the harnesses actually received them

Pulled verbatim from the run stores. Briefs are `nodes.brief`.

**Contracts are barely durable, and at `task` scale not durable at all.**
`store.Node` has **no `Contract` field** and the `nodes` table has no such column;
the working method lives in an in-memory map (`jobPlans.contracts`,
`cmd/aforge/chat.go:2131`). At `project` scale it is recoverable only by replaying
`plan_graph` event payloads. At `task` scale **no `plan_graph` event is ever
written** — verified: both arms' task-N stores contain `plan_graph: 0` events — so
the method that governed the single leaf is gone the moment the process exits.
For a redesign built on per-harness task templates, this is a prerequisite gap:
you cannot currently audit, replay, or A/B a spec you did not capture.

**Non-coding leaf (task P, chat-v2, `task-2-n1`, 574 chars, ran on `linear`):**
```
Produce a standalone reference brief of roughly 600 words on the CAP theorem and its
common misreadings. Cover the original formulation (Consistency, Availability,
Partition tolerance), the common misinterpretation that it is a choose-two trade-off
in all cases, the distinction between CP and AP systems in practice, and the role of
partition tolerance as a necessity rather than a choice. Be technically precise and
cite key sources (e.g., Brewer's original conjecture, Gilbert and Lynch's proof). The
brief must be self-contained with no cross-references to other topics.
```

**Coding leaf (task C′, chat-v2, `task-2-n1`, 566 chars, routed to `swe`):**
```
Create a new Go module in directory `rbtree` with `go mod init rbtree`. Define the
core tree structure in `rbtree.go`: a `Color` type with `Red` and `Black` constants,
and a `Node` struct with fields for `key`, `value`, `color`, `left`, and `right` (all
exported). The tree is represented by its root `*Node`. Use `interface{}` for values
to support any type initially. Include a constructor for empty trees. Ensure the code
compiles with `go build` and follows standard Go formatting. This defines the
fundamental data structure that all operations will build upon.
```

**Its contract / working method (648 chars):**
```
Start by creating the directory and initializing the module with `go mod init rbtree`.
Define the Color type as an int with Red=0 and Black=1 constants. The Node struct must
have exported fields: Key, Value interface{}, Color Color, Left, Right *Node. The tree
is represented by a root *Node. Write a constructor function NewTree() returning an
empty tree (nil root). Compile immediately with `go build` to catch syntax errors. Use
gofmt. Common mistakes: unexported fields preventing access, incorrect color constants,
or nil pointer issues in constructors. If interface{} causes vet warnings, note it as a
known trade-off for initial flexibility.
```

Another contract, showing the acceptance/verify structure the prompt asks for
(`Get operation`, 892 chars): *"…Done when the method correctly retrieves existing
keys and returns false for absent ones, without altering any node. Verify by testing
with known keys in a small tree and checking return values match expectations. Common
mistakes: not handling nil, incorrect comparison logic… If stuck on key comparison,
ensure keys are comparable types…"*

**Task-scale spec (task N, chat-v2, the single node, 2 523 chars — a different
author entirely):**
```
Deliver a rigorous comparative analysis of Paxos, Raft, and Viewstamped Replication
that a distributed-systems engineer can use to choose a consensus algorithm for a
concrete deployment. The analysis must cover failure models, exact liveness-vs-safety
conditions, leader-election differences, and network assumptions, and must conclude
with a decision table mapping three named scenarios (…) to the recommended algorithm
with the reasoning that justifies each choice. Success means the engineer, after
reading the analysis, can confidently pick an algorithm for each scenario and explain
why the other two are less suitable. The deliverable is a single written document,
self-contained, with no missing sections or placeholders. Verbatim request: …
```

Note the **two different spec authors**, chosen by scale rather than by worker:
- `task`/`lookup` scale → the spec is the **compiler's goal** (`Compiled.Goal`),
  which carries an explicit success criterion and the verbatim request appended.
  There is no plan brief and no plan contract; the method is `Compiled.Contract`,
  written in the same compile call (`cmd/aforge/chat.go:2916-2922`).
- `project` scale → the spec is a **plan-authored brief** (`internal/plan/brief.go`)
  plus a **plan-authored contract** (`internal/plan/contract.go`), one per leaf.

So spec shape varies by *scale* but never by *harness*.

#### What this says about under-specification

- **Specs are not vague.** Coding briefs name the **module, the file, the exact type
  and field names, the immutability constraint, and a build-level acceptance check**
  (`go build`). Contracts add done-means, a verification step, common mistakes and a
  stuck-hint — matching the `contractPrompt` bullet list. The failure mode in this
  battery was never an under-specified spec.
- **But the shape is one-size-fits-all.** The `swe` leaf and the `linear` leaf receive
  the *same* prose brief + prose contract. There is no per-harness template, no
  structured field for files-in-scope, no machine-readable acceptance criteria or todo
  list. Everything is prose the worker must parse.
- **The retry path silently loses specificity.** Compare the original swe brief above
  with its linear replacement, `task-2-n16` (319 chars):
  ```
  Tree structure — Define the Node struct and tree type with color, key, value, and
  left/right pointers.
  Replacement for failed node 1. Define the fundamental structure for the red-black
  tree, including node color (red or black), key, value, and left/right child pointers.
  ```
  The module name, the filename `rbtree.go`, `interface{}`, the constructor, and the
  `go build` acceptance check are **all gone**. And this is structural, not a
  truncation bug: the replacement is a **brand-new node** (`retry_of=''`,
  `origin='self'`, `intent='revision: Node 1 failed to produce any result due to a
  crash…'`) authored by the revision pass from the *failure context*, never from the
  original brief. Nothing re-targets the original spec at a different worker. For a redesign that wants per-harness templates,
  this is the clearest seam: the spec should be an object that survives re-targeting,
  not prose that gets rewritten each time a worker changes.
- **Scope is implicit.** No brief carries an explicit file-scope list or a "do not
  touch" boundary; the workspace root is the only scope, and it is passed out-of-band.

### How to reproduce

Everything is on the pod (`root@216.81.151.50 -p 12708`, key `~/.ssh/runpod_ed25519`).
Binaries `/root/aforge-{chatv2,trunk}`; stores `/root/stores/<arm>-<task>/<task>.db`;
logs `/root/logs/<arm>-<task>.{stdout.json,stderr.log,wall,rc}`; task prompts
`/root/task{C,N,P}.txt`; runner `/root/runone2.sh`.

One run:
```bash
AFORGE_HOME=/root/.aforge-<arm> AFORGE_DAILY_BUDGET=60 AFORGE_PRACTICE_BUDGET=0 \
/root/aforge-<arm> do "$(cat /root/taskP.txt)" \
  --db /root/stores/<arm>-P/P.db --keep -w /root/work/<arm>-P \
  --model deepseek/deepseek-v4-flash --timeout 1500 --json --yes-spend
```

The two queries that produced every number above:
```sql
SELECT id,stage,status,subharness,splice_subharness,started_at,finished_at FROM nodes ORDER BY started_at;
SELECT node_id, SUM(prompt_tokens), SUM(completion_tokens), COUNT(*) FROM usage GROUP BY node_id;
```
Overlapping `[started_at, finished_at]` intervals across sibling leaves is the
parallelism evidence; `subharness` is the harness evidence; the `usage` sums are
the §1.0 evidence.

---

## 7. Proposed fixes (described, not applied)

### F0 — Re-couple the leaf spend ceiling to the observation window (THE regression)

`abcc5a5` correctly stopped sizing memory from the wallet, then left the wallet at
its pre-change value. Three options, not mutually exclusive:

**Recommended combination: 2 + 3.** Option 1 alone is honest but expensive — it
implies a ~1.57 M-token ceiling. Capping the window at 64-96 KB (2) and
discounting cached prompt tokens in `spent()` (3) restores the old *turns
affordable* without a 10× budget, and 3 is independently correct regardless.

1. **Scale the ceiling with the window.** `chatLeafTokens`
   (`cmd/aforge/chat.go:1907`) is a constant chosen when a turn cost ~6 k prompt
   tokens. Make the leaf's token grant a function of `observationWindow(contextTokens)`
   — e.g. `ceiling = k × windowTokens`. The old behaviour fixes `k` exactly: a
   25 000-byte window is ~6 250 tokens (`observationBytesPerToken = 4`), and
   150 000 / 6 250 = **24 full-window turns**. The new window is ~65 536 tokens, so
   preserving 24 turns implies a ceiling near **1.57 M tokens** — which is the
   honest price of the larger memory, and shows how far the two drifted apart.
   Keeping *turns affordable* constant is the invariant that matters, since that is
   what governs whether a leaf finishes.
2. **Lower `maxObservationBudget`.** `256 << 10` (`internal/exec/context.go:21`) is
   reached by any model with ≥ ~600 k context, so every long-context model gets the
   maximum. A 64-96 KB cap would keep most of the benefit (the 26 KB file that
   motivated the change fits easily) at a quarter of the per-turn cost.
3. **Bill the window honestly against the ceiling.** `spent()`
   (`internal/exec/linear.go:1000-1002`) counts every re-sent cached prompt token at
   full weight. Since the provider bills cached prefix tokens at a fraction,
   weighting `CachedTokens` down in `spent()` would make the ceiling track real
   cost instead of raw tokens. `ExecResult.CachedTokens` already exists
   (`internal/resident/runner.go:36-41`, added in this same window), so the number
   is in hand. This is the smallest change and arguably the most correct: the
   ceiling is meant to bound spend, and it currently bounds something else.

Whichever is chosen, add a regression test that asserts a leaf's affordable turn
count does not fall when the observation window grows — that is the invariant that
silently broke.

### F1 — Finish the `Concrete()` fix already in flight (P0, both branches)

**You are already mid-fix on this**, in the uncommitted working tree:
`internal/catalog/catalog.go` gains `Model.AliasTarget` and `Concrete()` now tries
it before `CanonicalSlug`. That change is **correct and it fixes the default
model.** Measured against the live OpenRouter catalog:

```
'~deepseek/deepseek-v4-flash-latest'  canonical='~deepseek/deepseek-v4-flash-latest'
                                      alias_target.slug='deepseek/deepseek-v4-flash-0731'
'deepseek/deepseek-v4-flash'          canonical='deepseek/deepseek-v4-flash-20260423'
                                      alias_target=None
```

and models.dev **has** `deepseek/deepseek-v4-flash-0731`. So the alias path now
lands on a row the swe engine can price. Good.

**The remaining hole is the `canonical_slug` fallback**, and its justifying
comment is empirically wrong:

> *"canonical_slug is the fallback because on non-floating rows it is the dated
> spelling of the same model, which is still a better thing to hand a foreign
> catalog than a bare id"*

For models.dev it is strictly **worse**. models.dev's `openrouter` provider (342
rows) carries `deepseek/deepseek-v4-flash`, `deepseek/deepseek-v4-flash-0731` and
`~deepseek/deepseek-v4-flash-latest` — and **not** `deepseek/deepseek-v4-flash-20260423`.
So on a non-alias row the fallback converts an id the foreign catalog knows into
one it does not. That is exactly the crash reproduced in §1.5, and it will still
fire for anyone who names a concrete model explicitly (`--model deepseek/deepseek-v4-flash`,
which is how this battery ran).

Recommended, smallest change on top of what you have:

1. **Drop the `CanonicalSlug` fallback**, or gate it behind a check that the
   resolved id exists in the target catalog. `Concrete()`'s own stated principle
   already argues for this — *"an id this catalog cannot vouch for is forwarded
   verbatim and the far side's own error is allowed to be the thing the operator
   reads"* — it simply is not applied to the *output*. A bare id that the foreign
   catalog knows beats a dated id it does not.
2. **Verify before substituting (most robust).** Try candidates in order —
   alias target → id as written → canonical slug — and pass the first that
   resolves in `internal/swepro/internal/modelsdev`; forward the id as written if
   none do. The catalog is already fetched and cached with 5-minute freshness, a
   flock and a 60-minute refresh (`internal/swepro/ENGINE-DESIGN.md:182`; cache dir
   `modelsdev/models.go:189`), so the probe is free at steady state.
3. **Add a `doctor` check**: "can the swe worker resolve the configured work
   model?" `cmd/aforge/doctor.go` reports store, resident, standing-watch and
   budget state only (`collectDoctorSnapshot`, `doctor.go:104`) — it has no
   model-resolution probe. This failure costs $0 and 5 s and is invisible until
   someone reads a deliverable that is an error string.

**Separately: the second swe failure is not a model-id problem.** On
`deepseek/deepseek-chat-v3.1` the engine got past models.dev and still died with
*"entry agent returned no result"* (§1.5). Fixing `Concrete()` is necessary and
not sufficient; the swe engine needs its own investigation before coding tasks
work end to end.

**Workaround today**, since there is no env toggle (`AFORGE_SWEPRO` is an internal
re-exec sentinel — `cmd/aforge/swepro.go:17`, `internal/config/settings.go:116-121`):
force the generalist with `aforge do --subharness linear "<coding task>"`.

### F1b — Let a per-node verdict pin a leaf to `linear` (P1, both branches)

`linear` is spelled as the empty string, and the empty string already means "no
verdict", so the sizing pass can promote a node to a specialist but can never hold
one back to the generalist against a job-level `swe` (§1.6). Measured: 12 leaves
sized generalist all ran on `swe`.

Give the baseline a distinct spelling at the verdict layer — either record
`"linear"` verbatim at `internal/plan/size.go:441` behind a new `IsVerdict`
predicate (keeping `KnownSubharness` as the "is a specialist" question it is
documented to be, `size.go:177-184`), or carry a `SubharnessSet bool` beside the
name. Then `internal/store/splice.go:336-338` inherits the job choice only when the
node genuinely has no verdict. Test: a node sized `linear` inside a job spliced as
`swe` must run on `linear`.

This matters most for a per-atom-selection redesign: today, selection is
effectively **one choice per job**, not per leaf, regardless of what the sizing
pass concludes.

### F2 — Apply `terrain-chat-wiring.patch` (highest value, lowest risk, already written)

**The fix already exists, fully authored, and was deliberately not applied.**
`terrain-chat-wiring.patch` sits untracked at the repo root (6.3 KB, added by
`d321d14`). Its own header: *"The campaign never edits cmd/aforge/chat.go while
the chat rebuild owns it, so the two `plan.Build` call sites are written down here
instead of applied."* `d321d14`'s commit message says the same.

It is not literally two lines — there is no workspace variable in scope at either
site — so it threads one `terrainRoot string` parameter down two call chains:

| step | site (patch's own line numbers, pre-god-file-split) | current site |
|---|---|---|
| 1 | `chat.go:~305` — derive `terrainRoot` in `buildBrain` | `chat.go:302-320` |
| 2 | `chat.go:374` — hand it to `newResidentReconciler` | — |
| 3 | `chat.go:980`, `:1141` — both `replanRemainder` constructions | — |
| 4 | `chat.go:5058` — `planSubtree` gains the param | `chat.go:2896` |
| 5 | `chat.go:5117` — `Terrain: plan.RenderTerrain(terrainRoot, compiled.Goal)` | **`chat.go:2956`** |
| 6 | `chat.go:5297` — `replanRemainder` gains the param | `chat.go:3138` |
| 7 | `chat.go:5303` — the second `Terrain:` line | **`chat.go:3143`** |

Plus companions in `cmd/aforge/resident_build.go:27,43,70`, `cmd/aforge/wake.go:58`
and four `*_test.go` call sites. **The line numbers in the patch are stale** — it
was written against the pre-`6c96172` chat.go, and the two `plan.Build` sites are
now at `chat.go:2956` and `chat.go:3143`, marked by the `// terrain wiring lands
here` comments. Re-target before applying.

The gating detail is the one the patch author already resolved:

```go
terrainRoot := ""
if opts.sharedWorkspace {
    terrainRoot = workspaceRoot
}
```

Only a shared workspace has anything true to show — in the per-job layout the
directory does not exist yet at plan time. **`do` sets `sharedWorkspace: true`
(`do.go:278`)**, so `aforge do -w <dir>` is precisely the case this buys, and
interactive chat (per-job dirs) correctly renders nothing and keeps byte-identical
prompts. Expected effect on headless: fewer invented "gather the inputs" stages —
i.e. shorter serial chains — which is the exact axis under suspicion.

### F3 — Wire `FileShaped` from the delivery gate

The judgement already exists — `leafOutputHint` / the delivery gate decides
whether a leaf is offered a workspace path at all (`plan/graph.go:195-199` names
it as the intended source). Thread that bit into `plan.Options.FileShaped` at both
`plan.Build` sites, and set `graph.FileShaped` before `plan.Briefs`/`plan.Contracts`
on the paths that write briefs outside the build. For `do`, the honest source is
whether `-w` was given and the ask names a file.

### F4 — Backport the two compiler fixes if any trunk branch is still shipped

`resident-m1` and `subharness-m1` both carry `compileReplyTokens` floor `1000`
(`internal/head/compiler.go:483`) and the loose `builds_on` rule. On
`~deepseek/deepseek-v4-flash-latest` the first is a hard job-forfeit. Cherry-pick
`d2e02d7` and `51287ca`, or at minimum raise the floor.

### F5 — Make the scale gate observable

`cmd/aforge/chat.go:2907` silently collapses a job to one leaf, and it is the
single biggest determinant of whether a run has any parallelism. Nothing is
journaled about *why*. Journal `brief.Structure` and `brief.Scale` alongside the
splice so a run's shape can be explained after the fact without re-running it.
This is diagnosis infrastructure, not a behaviour change.

### F6 — The pre-existing plan-layer serial tail

`internal/plan/fanout.go:72-78` (no merge/summary part) and
`internal/plan/bind.go:74-77` (gatherer exception) are prompt rules that are
measurably disobeyed. These are byte-identical on both branches, so this is
backlog rather than regression, but it is the largest remaining source of real
serial time in a wide graph. A deterministic post-pass that drops a leaf whose
brief is purely "combine/summarize the above" — or binds it to all its siblings
rather than to none — would be more reliable than another prompt paragraph.

---

## 8. Instrumentation for the redesign

Collected alongside the regression work, for the contract *"chat submits one task;
headless owns JIT multi-level decomposition into linear/swe atoms; joint objective =
wall time × token cost × quality; per-harness task templates + selection descriptors
instead of hardcoded heuristics."* Runtime evidence for specs is in §6; this is the
static half.

### 8.1 Where the spec is authored — one payload, no per-harness shape

**Verdict: one-size-fits-all. A single `exec.Task` is built at one call site and
handed to whichever executor `executorFor` returns. The swe worker silently ignores
roughly half of it.**

- Struct: `internal/exec/executor.go:46-123`. Work-content fields: `Goal` ("the whole
  plan's goal, for orientation"), `Brief` ("the self-contained instruction: what the
  job is"), `Contract` ("the working method: how this kind of job is done well"),
  `Inputs`, `OutputHint`, `Intermediate`, `ImagePaths`, `DocumentPaths`, `Reflex`,
  `Subharness`, plus live channels `Steer`/`Share`/`Board`/`Control`/`Progress`.
  There is **no workspace field** (baked into the executor via `leafBuild.workspace`)
  and **no notebook field** (smuggled in as an `Input`).
- Populated once, at `cmd/aforge/chat.go:703-744`. Sources: `Brief` ← `node.Brief`
  wrapped by `residentDeliveryBrief` (`chat.go:1629`) and
  `withDocumentAttachmentBrief` (`:1688`); `Contract` ← `leafContract` (`:1592`);
  `Inputs` ← `leafNotebookInputs` (`:1566`) + `graph.DependencyInputs` (`:617-631`);
  `OutputHint`/`Intermediate` ← `leafOutputHint` (`:1653`); `Subharness` ← read, never
  re-decided (`chat.go:538-541`: *"Reading it here rather than deciding it here is the
  whole point of journaling it"*).
- Dispatch: `executorFor(subharness, build)` (`chat.go:592`) over `leafExecutors`
  (`cmd/aforge/subharness.go:74-96`). **`leafBuild` differs per harness; the `Task`
  does not.**

**The costly half of the spec is discarded for every swe leaf.** Fields each executor
actually reads:

| field | `linear.go` | `swe.go` |
|---|---|---|
| `Brief`, `Goal`, `Inputs`, `OutputHint`, `Steer`, `Control` | ✅ | ✅ |
| **`Contract`** | ✅ `:345` (system message) | ❌ **never read** |
| `Reflex`, `ImagePaths`, `DocumentPaths`, `Board`, `StoreNodeID` | ✅ | ❌ |
| `Progress` | ✗ | ✅ `:1019` |
| `Title` | ❌ | ❌ (dead both) |

`sweGoal` (`internal/exec/swe.go:688-710`) is a near-clone of `Linear.brief`
(`linear.go:892-925`) minus the share paragraph, and everything is flattened into a
single CLI positional at `swe.go:526-544`
(`argv = append(argv, "--max-cost", …, "--", goal)`). **There is no channel by which
the working method, attachments, or reflex framing could reach the engine at all.**
So a per-leaf model call writes a contract (`internal/plan/contract.go`), it is paid
for, and for coding leaves it is thrown away — while `linear.go:332-336` describes
that same contract as *"what a specialised harness would have hand-written for this
domain"*.

Spec composition is **byte-identical to trunk**: `internal/exec/executor.go` and
`internal/exec/subharness.go` are not in the window diff at all, and
`git show subharness-m1:cmd/aforge/chat.go | sed -n '686,725p'` matches the current
`chat.go:703-744` character for character. Only line numbers moved.

One asymmetry worth noting for the "one engine" claim: the **`aforge run <graph.json>`**
path composes a strictly poorer Task — `internal/exec/schedule.go:506-545` `taskFor`
sets only `NodeID, Title, Goal, Brief, Contract, Subharness, OutputHint, Inputs`, with
no images, documents, board, steer or progress. (`do` and chat are unaffected: they go
through `resident.Runner`, not `exec.Scheduler` — see §4.)

### 8.2 What briefs and contracts are required to contain

**Verdict: neither prompt requires file paths, and neither requires an acceptance
checklist. The brief prompt actively *forbids* scoping by file. Acceptance is required
as prose, under a hard word cap.**

- `internal/plan/brief.go:43-49` asks for context, deliverable, *"what finished looks
  like in the terms of whoever will use the result — what they do with it, what they
  must see — and where to stop"*.
- `brief.go:59-66` forbids file scope outright: *"Say nothing about which files,
  sections, components or parts of the work it may or may not change. It must be free
  to do whatever its own piece requires…"*
- Cap: **90-160 words** (`brief.go:74-75`), which works against enumerating criteria.
- `internal/plan/contract.go:61-80` requires five things — what to understand first,
  what "done" means, **how to verify** (*"the whole path exercised the way that user
  reaches it"*, and since `aeeb141`, *"Where the agent's instruction already states the
  bar for done, the check you write exercises that bar itself"*), the two-or-three
  common mistakes, and where the work gets stuck plus the route around it. Cap:
  **120-200 words** (`contract.go:93-94`).
- The **only** channel for a path is `node.Sources`, injected as one user-message line
  (`brief.go:277-278`, `contract.go:258-259`: `"It is expected to touch: %s\n"`).
  `Sources` (`internal/plan/graph.go:74-79`) is model-authored, optional, and nothing
  requires it to hold paths rather than *"the pricing page"*.

This reconciles with §6: the C′ coding briefs *did* name `rbtree.go` and `go build` —
not because any prompt required it, but because the goal and `Sources` happened to
carry it. **That specificity is discretionary, which is exactly why the retry path
lost it.**

### 8.3 Selection inputs — 100% prompt-driven, zero heuristics

**Verdict: definitively no keyword, word-list or regex matching on goal text anywhere
in the selection path. Every choice is a model reading a registry-generated menu and
returning an enum-constrained name, validated against the registry.**

Four selection sites, all fed from one registry:

1. **Compiler, whole-job** — `WithSubharnessMenu(exec.MenuText, exec.KnownSubharness)`
   (`cmd/aforge/resident_build.go:34`). Two functions deliberately
   (`compiler.go:253-256`: *"a menu is a claim and a name coming back is a claim to
   check"*). Menu appended to the **user** message, last (`compiler.go:342-344`), for
   cache stability. Body generated at `internal/exec/subharness.go:203-233`, closing
   with *"Choose a specialist subharness only when the job's essence matches its
   purpose. When in doubt, or for mixed or non-matching work, leave it unset."*
2. **Plan sizing, per-node** — `internal/plan/size.go:273-294` `subharnessSection`,
   with the specialist's own replacement ruler (`swePriorAnchors`,
   `subharness_swe.go:46-72`). Degradation is structural: `sizeSchemaFor`
   (`size.go:321-355`) emits `enum: ["", <registered names>]`, so *"a subharness the
   process does not have cannot be named at all"*.
3. **Retry-worker judge** — `internal/revision/judge.go:602-674` via `chat.go:808`.
   *"The failure itself is not a reason to change the kind of worker."* The failed
   worker is struck off structurally by `exec.MenuTextExcept` (`subharness.go:189-202`:
   *"a model cannot pick what it was never shown"*).
4. **Overrun-remainder judge** — `chat.go:990-998`.

Plus the non-model `--subharness` override (`do.go:121`, `run.go:41` →
`subharness.go:329-347`).

The descriptor the model actually reads is the registered `Purpose`
(`subharness_swe.go:30-42`) plus a measured line
(`cmd/aforge/selfknow.go:86-106`), rendered like:
`measured here so far: median 40000 tokens, 12 turns over 31 runs; 90% succeeded; avg cost $0.0412`
— suppressed entirely until `profile.MinSamples` records exist.

Heuristics check, definitive: grepping the whole selection path for
`Contains|HasPrefix|HasSuffix|regexp|match|Fields(|ToLower` yields **one** hit, and it
is prose inside the menu string (`subharness.go:229`). The only by-name `swe`
references outside tests are the executor, the constructor table and the
registration. `subharness.go:199-203` states this as law: *"the grep law is not
decoration, it is what keeps the next specialist a registration instead of a
rewrite."*

**Seam:** `SubharnessInfo` (`internal/exec/subharness.go:34-52`) is already the
selection descriptor — `Purpose`, `PriorAnchors`, budget shape — and
`RegisterSubharness` (`:98-108`) is the single door feeding all four sites. A richer
descriptor (per-harness Task template, input-capability declaration, expected
cost/latency profile) slots in there and reaches every selection site without touching
any of them.

### 8.4 Objective framing — three axes, never traded

**Verdict: every prompt optimizes one axis in isolation. Wall-clock is the only
currency any prompt reasons in; token cost is explicitly declared to be zero; quality
is a binary bar. There is no joint objective anywhere.**

| axis | where | form |
|---|---|---|
| wall-clock | `head/compiler.go:45,50,51`; `plan/size.go:239-242` | the only currency |
| token/$ cost | `plan/plan.go:50-53` declares it **zero**; real control is rails (`exec/governor.go`, `chat.go:564-582`, `--max-cost` `swe.go:540`) | governors, not objectives |
| quality | `compiler.go:31,55`; `brief.go:46`; `contract.go:68-73` | binary bar, not a weight |

The load-bearing string is `agentPremise` (`internal/plan/plan.go:50-53`), shared
verbatim by every planning prompt:

> "The work is done by AI agents. **They are instant, free, and unlimited in number.**
> They start together, never talk to each other, and never see each other's work."

The compiler's width rule (`compiler.go:50`) reasons purely in latency: *"an extra
worker costs almost nothing to organise and **the time the person waits is the longest
chain, never the total**"*, debiting only lateness — *"several of those land later than
one worker would have"*. Its neighbours agree: *"builds_on is a WAIT, and that is what
it costs"* (`:45`), *"paid for in full by the person waiting"* (`:51`). The one line
gesturing at spend is *"A duplicate is paid for twice and answers once"* (`:46`), still
framed as redundancy.

`plan/size.go:239-242` is the only place two costs sit side by side — and both are
latency: *"Splitting a node costs a round of planning and an extra result to
reassemble… Leaving a node too big costs an agent grinding serially."* The subharness
section closes *"it is never wrong, only slower"* (`size.go:292`).

The only counterweight to the free-agents premise is `proportionRule`
(`plan.go:118-120`): *"A large plan for a small goal is not thoroughness; it is delay
and expense the person asking pays for"* — a burden-of-proof rule, not a quantity.

**Cheapest place to introduce a real trade:** `agentPremise` is one string reaching
every planning prompt, so a single edit stops it declaring cost zero. And the measured
line already carries per-worker cost and success rate into all four selection prompts
(`selfknow.go:103`) as **decoration the model is never told what to do with** — turning
that into an instruction is a one-paragraph change with evidence already in hand. Note
the compiler prompt is golden-tested
(`internal/head/testdata/compiler_prompt_baseline.golden`), so that golden moves with it.

---

## 9. A/B rerun vs origin/master @3d692a7 (wider task battery)

Second live campaign, run the same day, on the same pod. Baseline changed at the
user's request: `origin/master` @ **`3d692a7`** ("BENCHMARKS: record the valid
post-fix PR-review re-run", 2026-08-05) instead of `subharness-m1`. `3d692a7` is
**737 commits behind** `chat-v2` and 0 ahead — a genuine ancestor.

### 9.0 The finding that reframes the question: `3d692a7` has no `aforge do`

Before any measurement: **the program the user remembers is not the program that
exists.** At `3d692a7` the headless task path does not exist yet.

```
git show 3d692a7:cmd/aforge/main.go
  // Command aforge builds and revises task graphs. It executes nothing:
  // the graph is the product.
  case "plan" / "revise" / "run" / "show" / "help"
```

`cmd/aforge/` at `3d692a7` contains exactly three files — `main.go`, `render.go`,
`run.go`. There is **no `do`, no `chat`**. `cmd/aforge/do.go` is first added by
`c63c290` ("aforge do: one errand, the whole living brain, nobody watching"),
after the baseline. `internal/` holds **5 packages / 42 Go files**
(`config`, `exec`, `plan`, `profile`, `provider`); `chat-v2` holds **288 packages
/ 1428 Go files**. There is no `store`, no `head`, no `resident`, no `catalog`,
no `swepro`, no subharness registry, no durable sqlite store, no reconciler, no
sentinel/revision pass, and no extension mechanism.

So the literal experiment asked for — "run each task on both arms via `aforge do`"
— is **impossible on the old arm**. What was run instead is the closest true
equivalent and the only headless pipeline that existed then:

```
aforge plan "<task>" --brief -o graph.json     # then
aforge run graph.json -w <dir> -o done.json
```

That is the *other* engine — the one `docs/ARCHITECTURE.md:162-175` names
explicitly as not sharing the chat brain (§3). Both arms therefore ran the same
task text, on the same model, against the same 200-turn / 150 000-token leaf
envelope; they differ in everything above the leaf. Read every number below as
**"old headless pipeline vs current headless pipeline"**, not as a controlled
single-variable diff.

Three constants make the comparison meaningful despite that:

| controlled | `3d692a7` | `chat-v2` |
|---|---|---|
| per-leaf turn backstop | `-turns 200` (`cmd/aforge/run.go:26`) | `chatLeafTurns = 200` (`cmd/aforge/chat.go:1906`) |
| per-leaf token ceiling | `-budget 150000` (`cmd/aforge/run.go:27`) | `chatLeafTokens = 150_000` (`cmd/aforge/chat.go:1907`) |
| model | `deepseek/deepseek-v4-flash` | same |

And `abcc5a5` — the §1.0 window regression — is **not an ancestor of `3d692a7`**
(`git merge-base --is-ancestor abcc5a5 3d692a7` → false; `abcc5a5` is dated
2026-08-10, the baseline 2026-08-05). The baseline still sizes the leaf window the
old way: `obsBudget := l.maxTokens / 6` → **25 000 B**
(`3d692a7:internal/exec/linear.go:175`), against `chat-v2`'s context-derived
**262 144 B**. So §1.0's mechanism is expected to reproduce here, and it does —
harder.

### 9.1 Task battery and rationale

Seven tasks, none reused verbatim from the C/N/P battery. Prompts on the pod at
`/root/ab2/tasks/task{K1,K2,K3,A1,A2,X1,X2}.txt`.

| id | task | shape | why it is in the battery |
|---|---|---|---|
| **K1** | Thompson-NFA regex engine in Go, no stdlib `regexp`, 5000-pair differential test against `regexp`, `go test ./...` green | coding, **monolithic** — one coherent artifact, splitting hurts | the anti-parallel coding case; also objectively gradeable by compiler |
| **K2** | Go module `toolkit` with **four independent packages** (`ratelimit`, `csvmap`, `toposort`, `ttlcache`), each with tests | coding, **naturally 4-way parallel** | the parallel coding case — the shape where fan-out should pay |
| **K3** | Seeded buggy `jobq` repo in the workspace: fix every defect, `go test -race ./...` green, **do not modify any test file**, explain each defect | coding, **brownfield / debug** | the only task grounded in pre-existing material; exactly `swe`'s stated purpose; 4 planted bugs give an objective score |
| **A1** | Derive memory-ordering requirements for a lock-free MPMC ring buffer; x86-64 TSO vs ARM64; ABA and sequence-counter wrap | analysis, **deep and serial** — one continuous derivation | non-coding work that resists decomposition |
| **A2** | **Five** independent ~700-word reference briefs (Bloom filters, join-order optimisation, CFS→EEVDF, TLS 1.3, column-store compression) | analysis, **naturally 5-way parallel** | the direct successor to task P; the parallelism test |
| **X1** | Settle whether `sync.Map` beats a sharded `RWMutex` map at 95% reads, across 1/8/64 goroutines and 10³/10⁵/10⁶ keys, **with evidence** | **deliberately ambiguous** | answerable as an essay *or* by writing and running a benchmark — stresses swe-vs-linear |
| **X2** | Complete `database/sql`+`lib/pq` → `pgx/v5` native migration guide, before/after code for every call-site pattern | **deliberately ambiguous** | reads as documentation, demands code — the inverse ambiguity of X1 |

K3's seed (`/root/ab2/seedK3/`) plants four defects — an unsynchronised
`append` to a shared slice, a non-idempotent `Stop()` that double-closes a
channel, a ring-buffer off-by-one (`r.n >= len(r.buf)-1`), and a `Retry` that
runs `attempts-1` times — and was verified failing before the battery.

### 9.2 Method

Pod: RunPod Ubuntu, 16 cores, x86_64, no GPU, host load average 18-32 throughout.
`chat-v2` binary is `/root/aforge-chatv2`, **verified byte-identical**
(`sha256 b31b6c8c…`) to a fresh `GOOS=linux GOARCH=amd64` build of committed HEAD
`61923d7` — no working-tree changes, so the in-flight `AliasTarget` fix is **not**
in this arm. Old binary `/root/aforge-old3d69` (`sha256 5b405ac1…`) cross-compiled
from `3d692a7`. Go 1.23.4 installed on the pod so coding deliverables are
*compiled and tested*, not merely read.

Isolation per arm **and per task** (stricter than §6, which shared one home per
arm): own `AFORGE_HOME` / `AFORGE_PROFILE_DIR`, own store dir, own workspace, own
git repo. This matters for the old arm specifically, whose `recordAndCalibrate`
(`3d692a7:cmd/aforge/run.go:190-230`) rewrites the sizing ruler from every run —
sharing a profile dir would have let task N contaminate task N+1.

Walls: new arm `--timeout 900` inside `timeout -k 120 1200`; old arm `plan` inside
`timeout 700`, `run` inside `timeout -k 180 900` (SIGTERM, which `run.go:150-158`
catches and lands gracefully). Quality graded by **running the deliverable**
(`go build`, `go vet`, `go test -count=1`, `go test -race -count=1`) and by
reading it, not by exit code.

**Disclosed confound.** K1-X1 ran two-at-a-time (one per arm), matching §6. On the
user's instruction to finish faster, **X2 and the whole v3.1 control ran 8-way
concurrent**, and unrelated `aforge-agot` processes belonging to another workload
were also live on the pod during that window. Higher load throttles the *new* arm
only — `chat-v2`'s governor reads `/proc/loadavg` (`internal/exec/governor.go:92-109`)
while `3d692a7` has **no governor at all** — so that window is biased *against*
chat-v2, and the v3.1 control's two timeouts must be read with that in mind.

### 9.3 Results

`ptok`/`ctok` are summed `usage` rows (new arm) and `graph.Usage` (old arm).
"conc" is max overlapping `[started_at, finished_at]` intervals — from the `nodes`
table on the new arm, from timestamped scheduler events on the old.

| task | arm | harness chosen | nodes | conc | prompt tok | compl tok | cost | wall | outcome |
|---|---|---|---|---|---|---|---|---|---|
| **K1** regex engine | new | **swe** | 1 | 1 | 4 107 | 657 | $0.0005 | **8.6 s** | ✗ swe engine crash, nothing |
| | old | linear (only worker) | 9 | 3 | 1 652 341 | 60 786 | $0.1212 | 569.9 s | ✓ **`go build` + `go vet` clean, `go test -count=1` PASS** |
| **K2** 4 packages | new | **swe** | 5 | 4 | 12 623 | 2 453 | $0.0015 | **16.2 s** | ✗ swe crash on all 4 leaves |
| | old | linear | 10 | 4 | 276 461 | 21 548 | $0.0274 | **112.6 s** | ✓ **all 4 packages PASS** |
| **K3** fix seeded bugs | new | **swe** | 1 | 1 | 4 029 | 465 | $0.0004 | **6.0 s** | ✗ swe crash, nothing |
| | old | linear | 4 | 2 | 196 042 | 11 126 | $0.0155 | 154.1 s | ✓ **all 4 bugs fixed, `-race` PASS, 0 test files touched, correct file:line writeup** |
| **A1** MPMC ordering | new | linear | 2 leaves (+1 ext) | 1 | 561 115 | 44 852 | **$0.0380** | **421.8 s** | ✓ 2 699-word document **returned in full** |
| | old | linear | 10 | 2 | 538 532 | 42 679 | $0.0529 | 444.0 s | ✓ 13 929 words filed — but **returns a 174-word pointer** |
| **A2** 5 briefs | new | linear | 10 (+3 ext) | **5** | **3 163 127** | 111 809 | **$0.1830** | **900.2 s (wall hit)** | ✗ **not settled; 4 of 5 briefs are wrong topics** |
| | old | linear | 11 | **5** | 342 428 | 32 904 | $0.0348 | **214.4 s** | ✓ all 5 briefs, 716-893 words each |
| **X1** sync.Map (ambiguous) | new | **swe** | 1 | 1 | 215 304 | 9 179 | $0.0108 | 512.1 s | ✗ swe crash after 8.4 min; one stray `bench_test.go` |
| | old | linear | 10 | 4 | 851 720 | 36 255 | $0.0561 | 504.7 s | ✓ **compiled and ran an 18-config benchmark**, real throughput table |
| **X2** pgx migration (ambiguous) | new | **linear** | 2 (+1 ext) | 1 | 240 909 | 20 725 | **$0.0195** | **195.6 s** | ✓ 2 372-word guide **returned in full** |
| | old | linear | 11 | 4 | 510 238 | 54 832 | $0.0559 | 340.1 s | ✓ 18 523 words filed — **returns a 198-word pointer** |

**Score: old 7/7 delivered, new 3/7.** (New: A1, X2 clean; A2 failed on content;
K1/K2/K3/X1 killed by the swe engine.)

Totals over the seven tasks — new $0.2537 / 2 060 s / 4.20 M prompt tokens; old
$0.3638 / 2 340 s / 4.37 M. **Those totals are meaningless as a cost comparison**,
because four of the new arm's seven runs aborted in under 20 seconds having done
no work. The honest comparison is the three tasks where **both arms ran to a real
finish**:

| A1 + A2 + X2 only | new (`chat-v2`) | old (`3d692a7`) | ratio |
|---|---|---|---|
| prompt tokens | **3 965 151** | 1 391 198 | **2.85×** |
| cost | **$0.2405** | $0.1436 | **1.67×** |
| wall | **1 517 s** | 999 s | **1.52×** |
| delivered | 2 of 3 | **3 of 3** | — |

### 9.4 Task-by-task, what actually happened

**K1/K2/K3/X1 — the new arm never started work.** All four were compiled to `swe`
and died on the *same* pre-existing P0 this report already documents in §1.5:

```
"the coding pipeline crashed: models.dev: model
 \"deepseek/deepseek-v4-flash-20260423\" not found for provider \"openrouter\""
```

`catalog.Concrete()` (`internal/catalog/catalog.go:216-226`) rewrites
`deepseek/deepseek-v4-flash` into its dated `canonical_slug`, which models.dev does
not carry. **This is not a regression against `3d692a7`** — `3d692a7` has no
subharness registry and no `internal/swepro`, so it cannot hit the bug. But it is
what a user running today's binary on the default model experiences on **every
coding task**, and it is why the old arm swept the coding half of the battery.

K2 surfaced a **second, previously unreported swe failure mode** beside the model
id — the `learned` array in `/root/ab2/logs/new-K2.stdout.json`:

```
"…the swe worker could not stage the workspace: exit status 128"
"…the swe worker could not commit a baseline: exit status 1"
"…the swe worker could not commit a baseline: exit status 128"
```

Three of the four leaves failed on **git plumbing**, not on models.dev. Worth its
own look; F1 will not fix these.

Note K2 also shows the new arm's *planning* was correct: it decomposed into
exactly the four named packages and started four leaves concurrently. Only the
worker was broken.

**K3 is the sharpest quality datum in the battery.** The old arm fixed all four
planted defects, touched zero test files, passed `go test -race -count=1`, and
returned an accurate writeup:

```
1. pool.go:27  — data race on p.results …          fixed with a sync.Mutex
2. pool.go:40  — Stop() could close a closed channel … fixed by checking p.stopped
3. queue.go:14 — r.n >= len(r.buf)-1 instead of len(r.buf) … fixed by removing -1
4. retry.go:10 — never made the final attempt …    fixed by a final fn() call
```

Every file:line and every diagnosis is correct. The new arm produced nothing in
6.0 seconds.

**A1 is the one genuine draw, and it inverts on delivery.** Nearly identical
tokens (606 k vs 581 k) and wall (422 s vs 444 s); the new arm was **cheaper**
($0.0380 vs $0.0529) despite marginally more tokens, because 9 long calls cache a
shared prefix better than 108 short ones. Quality diverges in shape, not in
competence: the new arm returned a tight, correct 2 699-word derivation
**in the message**; the old arm wrote 13 929 words across nine files and returned
*"The deliverable is complete at `10-synthesis.md`"* plus a table of contents —
**174 words, no answer.** For a headless caller reading stdout, the old arm
delivered nothing without going to dig in a workspace. That gap is precisely what
`DeliverInMessage` (`internal/plan/delivery.go:24-35`, absent at `3d692a7`) was
written to close, and its own comment records the task that died over it. The old
arm's extra volume also carries error: its A1 pseudocode publishes
`seq[slot].store(tail+3, release)` where the Vyukov encoding requires `tail+N`,
and it justifies the producer CAS's release half by appealing to RMW atomicity,
which is not a memory-ordering property.

**A2 is the decisive loss, and it reproduces §1.0 at a larger multiplier.**

```
new: 3 163 127 prompt tok, $0.1830,  900 s (wall hit), settled=false
old:   342 428 prompt tok, $0.0348,  214 s,            all 11 nodes done
     → 9.2× tokens, 5.3× cost, 4.2× wall — and the new arm lost
```

Parallelism is **not** the cause and is not regressed: the new arm started
`task-2-n1` … `task-2-n5` at `16:05:25.457`-`.473`, a **16 ms spread, five
concurrent leaves** — above `GovernorMinInFlight = 3`. The old arm also reached 5.
The cost is per-leaf: three extension nodes (`n3-x1`, `n3-x2`, `n4-x1`) plus a
delivery node that itself failed (`task-2-x1`, status `failed`), exactly the
extension churn §1.0 attributes to the 10.5× observation window that `3d692a7`
does not have (`3d692a7:internal/exec/linear.go:175`).

**A2 also exposes a defect this report has not previously recorded: sibling leaves
clobber one shared scratch file.** The new arm's entire A2 workspace is a single
20 635-byte file, and its headings are:

```
# Bloom Filters      ← correct
# Submesh            ← wrong topic
# Subnet             ← wrong topic
# Subquery           ← wrong topic
# Subroutine         ← wrong topic
```

Four of the five briefs were replaced by unrelated encyclopaedia entries for words
beginning "Sub". The run's own `learned` array names the mechanism:

```
"The shared scratch path 14-write-a-self-contained-700-word-technical.md is being
 overwritten by other workers (currently holds the CFS/EEVDF brief)."
"…got overwritten by the Linux-scheduling brief. Use a topic-specific filename…"
```

The proximate cause is a **title collision**. All five leaves carry the *identical*
title in the `nodes` table — `"Write a self-contained ~700-word technical"` —
because the title is the brief's first line clipped to 48 characters
(`internal/resident/resident.go:1103`, `clipLabel(firstLine(...), 48)`) and all
five briefs open with the same clause. `deliverableLineFor` then names the
deliverable owner **by that title** to every non-owner leaf
(`internal/plan/brief.go:269-271`: *"is produced by %s, not here"*), so all five
agents were pointed at one indistinguishable label, converged on one filename, and
overwrote each other; the delivery leaf then found one brief and confabulated four.
`internal/plan/brief.go:52-57` already warns the brief-writer about *"each write
the same file over the top of the others"* — the warning exists, the mechanical
guarantee does not, and the same warning text is present verbatim at `3d692a7`.
**Fix direction:** make the label identifying a node unique by construction (node
id, or title disambiguated among siblings) rather than a clip of shared prose.

**X1 — the ambiguity test, and the new arm's harness choice is defensible; the
engine is not.** The new arm read "settle with evidence, across 18 configurations"
as coding work and chose `swe`. That is arguably the *right* call — and it is the
opposite of trunk's error in §6, where `swe` was wasted on pure prose. It then
burned 8.4 minutes and died on models.dev, leaving one orphan `bench_test.go`. The
old arm, which has no choice to make, wrote `bench.go`, `main.go`,
`params/params.go` and `shardedmap/shardedmap.go`, **compiled a 2.2 MB static ELF
binary, ran it**, and returned an 18-cell throughput table (sharded map winning 17
of 18 configurations, up to 11.3× at 1 M keys × 64 goroutines) with a correct
account of `sync.Map`'s read-mostly path and dirty-map promotion. That is the best
single deliverable in the battery, from either arm.

**X2 — the new arm's clearest win.** Same ambiguity, opposite classification: the
compiler chose `linear`, and the run settled in 195.6 s for $0.0195 with a
2 372-word guide returned in full. The old arm spent 340.1 s and $0.0559, produced
7.8× the prose (18 523 words across eleven files), and returned a **198-word
pointer**. On raw scholarship the old arm produced more; on *delivering an answer*
the new arm won outright, at 2.2× less cost and 1.7× less wall.

### 9.5 Control: same tasks, model the swe engine can actually resolve

Because four coding tasks were decided by a model-id bug rather than by anything
in the 737-commit window, K1/K2/K3 were re-run on both arms on
`deepseek/deepseek-chat-v3.1` — the model §1.5 established passes through
`Concrete()` unrewritten. **This is the arm that answers the real question.**

| task | arm | nodes | prompt tok | cost | wall | outcome |
|---|---|---|---|---|---|---|
| **K3** | new (v3.1) | 1 (`swe`, succeeded) | 186 831 | $0.4791 | 244.1 s | ✓ **`go test -race` PASS**, seed test files unmodified |
| | old (v3.1) | 4 | 706 751 | $0.4451 | **154.2 s** | ✓ `-race` PASS, precise 4-defect writeup |
| **K1** | new (v3.1) | 35 (20 `swe`, 11 failed) | **3 235 233** | **$6.0964** | 900.2 s (wall hit) | ✗ incomplete |
| | old (v3.1) | — | 2 382 959 | $0.9681 | 906.9 s (wall hit) | ✗ incomplete |
| **K2** | new (v3.1) | 6 (5 `swe`, 3 failed) | 677 266 | $0.1735 | 900.1 s (wall hit) | ✗ build fails: stray `极` glyph in `ratelimit.go:92` |
| | old (v3.1) | — | 1 222 947 | $0.4405 | 900.0 s (wall hit) | ✗ `ttlcache` LRU eviction test FAILS |

Three things this settles:

1. **The swe engine is not broken and the coding wipeout is entirely the models.dev
   P0.** On K3, `chat-v2` ran `swe` end to end (`aforge-chatv2 run --dir … --format
   json --high` was observed live) and passed the race detector. Fix **F1** and the
   4-0 coding sweep in §9.3 becomes a real contest.
2. **Even when swe works, the old arm is cheaper and faster.** K3: 154 s / $0.445
   old vs 244 s / $0.479 new. K1: **$0.97 old vs $6.10 new — 6.3×** on the same
   unfinished task.
3. **Neither arm is good at K1/K2 on this model.** Both hit the 900 s wall; both
   left broken code. This is not a regression story, it is a capability ceiling —
   and it is the one place where the "old was better" thesis gets no support at all.
   Caveat: this control ran 8-way concurrent under foreign load, which penalises the
   governed arm only (§9.2).

### 9.6 Why the numbers came out this way — the code

Four mechanisms, all of them structural, all citable.

**(a) The old arm cannot overrun, because overrun is recorded as success.**
`3d692a7` has no reconciler, no sentinel, no revision pass and no extension node.
A leaf that exhausts its 150 000-token budget is marked **done** anyway:

```go
// 3d692a7:internal/exec/schedule.go:398-405
node.State = plan.StateDone
detail := fmt.Sprintf("%d turns, %dk tok", outcome.Turns, node.Tokens/1000)
switch outcome.Stop {
case StopBudget:
    detail += ", exhausted its token budget — the leaf was too large"
```

This is the single biggest reason the old arm looks cheap and fast. Its cost is
**hard-capped by construction** at roughly `leaves × 150 000`. Five of K1's nine
nodes stopped on `budget` and were counted done; K1's synthesis result is literally
truncated mid-sentence — *"All tests pass. Let me run `go vet` and confirm the full
test suite once more."* — and was still recorded as a success. The old arm bought
its speed by declaring victory at the budget line. That it *also* passed
`go test -count=1` on K1 means the gamble paid off there; it is not a guarantee,
and K2 under the v3.1 control is where the same gamble lost.

`chat-v2` instead extends (`internal/resident/overrun.go`, `-x1/-x2/-x3`), which is
what let it finish task P in §6 — and what made A2 cost 9.2× and still fail.

**(b) The leaf's per-request preamble more than doubled.** Independent of §1.0's
observation window, every single turn on `chat-v2` re-sends a larger fixed prefix:

| | `3d692a7` | `chat-v2` | ratio |
|---|---|---|---|
| leaf `systemPrompt` | **20 379 chars** | **45 940 chars** | **2.25×** |
| tools offered to a leaf | **4** (`sh`, `write`, `edit`, `web`) | **15** (adds `job`, `recall`, `share`, media, vision, `read_document`, `capabilities`, `promote`) | **3.75×** |
| `internal/exec/tools.go` | 13 933 B | 43 522 B | 3.1× |

(`internal/exec/linear.go` `const systemPrompt`; `internal/exec/tools.go`
`define(` sites.) This is a per-turn tax that no caching of *content* removes,
and it compounds §1.0 rather than replacing it.

**(c) Concurrency: the old arm is ungoverned and wider by default.** `3d692a7`
bounds leaves with one integer and nothing else — `-j 8`
(`3d692a7:cmd/aforge/run.go:28`), enforced at
`3d692a7:internal/exec/schedule.go:112` (`if len(inFlight) >= s.concurrency`),
defaulting to 8 (`schedule.go:64-68`). There is **no `governor.go` at all** at the
baseline. `chat-v2` has 32 slots but admits through a `/proc/loadavg` governor with
a floor of 3 (`internal/exec/governor.go:48,92-109`). On a pod at load 18-32 that
floor is what binds. Measured, both arms reached the same peak concurrency on the
same task (A2: **5 and 5**; K2: 4 and 4), so **parallelism is not where the time
went** — consistent with §4 and §6. But the old arm's ceiling is load-blind, which
is why the 8-way window in §9.2 penalises only one arm.

**(d) The old arm always decomposes; `chat-v2` decides.** `3d692a7` has no scale
gate: `plan.Build` runs the spine, expands every level up to `MaxDepth`, and
`graph.addSynthesis()` (`3d692a7:internal/plan/graph.go:726`) appends a synthesis
node unconditionally. A smoke test confirmed the cost of that: *"Write a
one-paragraph explanation of what a bloom filter is"* compiled to **9 nodes, 38
calls, $0.0098**. `chat-v2` gates on scale (`cmd/aforge/chat.go:2907`: non-project
→ one leaf) and widens back via `reconcileScale` (§1.3). That is why the new arm
ran A1 and X2 as 1-2 leaves and the old arm ran them as 10-11 — and why the new arm
won X2 on both axes while losing A2.

**(e) Corollary worth recording — the benchmarking door only opens one way.**
`--subharness` cannot force the generalist:

```go
// cmd/aforge/subharness.go:329-333
func resolveSubharnessFlag(name string, stderr io.Writer) string {
	name = strings.TrimSpace(name)
	if name == "" || name == exec.LinearSubharness {
		return ""            // ← "linear" resolves to "no forcing"
	}
```

So on committed `chat-v2` HEAD there is **no invocation** that runs a coding task
on `linear` with `deepseek/deepseek-v4-flash`, which is why the control had to
change the model instead. This is §1.6's `linear == ""` collision surfacing at the
CLI, and it is a direct obstacle to A/B-ing workers — the thing the flag exists for.

### 9.7 Verdict

**Was `3d692a7` better? On cost and wall time, yes and by a lot. On quality, no —
it is better at *doing* the work and worse at *delivering* it. And the comparison
does not license a revert, because the two are not the same program.**

| axis | verdict |
|---|---|
| **token cost** | **`3d692a7` is materially cheaper.** On the three tasks both arms finished, `chat-v2` spent **2.85× the prompt tokens and 1.67× the dollars**. On A2 alone, **9.2× tokens / 5.3× cost**. The v3.1 control agrees on the arm where swe works (K1: $0.97 vs $6.10, **6.3×**). This is §1.0's regression, confirmed against a second, older baseline, at a larger multiplier — plus a newly measured **2.25× growth in the leaf's fixed per-turn preamble** (§9.6b) that §1.0 did not account for. |
| **wall time** | **`3d692a7` is faster on wide work, `chat-v2` is faster on narrow work.** Old wins A2 (214 s vs 900 s) and K3-v3.1 (154 s vs 244 s); new wins X2 (196 s vs 340 s) and ties A1 (422 s vs 444 s). Aggregate over comparable tasks: **1.52× in the old arm's favour**. Not caused by parallelism — peak concurrency was identical on every task where both arms ran (§9.6c). |
| **quality** | **Split, and the split is the whole story.** The old arm delivered **7/7** and the new **3/7** — but four of those losses are one pre-existing bug (§1.5 / F1), not a regression, and the v3.1 control shows the engine works once it is past. Where both arms genuinely ran: the old arm produced **more and better material** (X1's compiled 18-config benchmark; A1's 13 929 words) but **returns a pointer instead of an answer** on every multi-node task (A1 174 words, A2, X2 198 words) — the exact defect `DeliverInMessage` was written to fix and which does not exist at the baseline. The new arm returns finished answers (A1, X2) but **lost A2 to a content-corruption bug** (§9.4) that has nothing to do with the baseline at all. |
| **is the user's memory right?** | **Partly, and for a reason worth knowing.** Headless *was* cheaper and often faster at `3d692a7`. But `aforge do` did not exist then: what they are remembering is `aforge plan` + `aforge run`, a 42-file program with one worker, no store, no head, no extensions and no delivery law. It was cheap because a leaf that ran out of budget was **recorded as done** (§9.6a) and because its leaf preamble was less than half the size (§9.6b). Two of those three cost mechanisms are defects worth fixing in the current tree; the third is a feature the current tree deliberately bought. |

**What to do with this, in order.**

1. **F1 (already scoped, §7)** — finish `Concrete()`. It decides 4 of 7 tasks in
   this battery and every coding task a user runs today on the default model.
   Add the two new K2 git failure modes to its investigation: *"the swe worker
   could not stage the workspace / could not commit a baseline"* is a separate bug.
2. **F0 (already scoped, §7)** — re-couple the leaf ceiling to the window. Confirmed
   against a second baseline; A2 is a cleaner reproduction than task P was.
3. **New — F4: make a node's label unique by construction.** The A2 content
   corruption (§9.4) is a correctness bug, not an efficiency one, and it fires
   exactly when a plan fans out into siblings that share a brief preamble — i.e.
   on the shape the system is being pushed *toward*. `resident.go:1103` and
   `brief.go:269-271` are the two sites.
4. **New — F5: measure the leaf preamble.** 20 k → 46 k chars of system prompt and
   4 → 15 tools is a per-turn tax nobody has priced. Some of it (media, vision,
   `read_document`) is dead weight on a text leaf and could be offered
   conditionally, as `recall` and `share` already are.
5. **F1b (§1.6)** — and note the CLI corollary in §9.6e: until `--subharness linear`
   works, worker A/Bs cannot be run at all without changing the model underneath
   them, which is not a controlled experiment.

**Confidence.** n=1 per cell; the harness choice and every deliverable are LLM
judgements. What is *proven*: the token and wall ratios on the three comparable
tasks, the seven objective build/test verdicts, the identical peak concurrency, the
byte counts in §9.6b, and the absence of `do`/`swe`/`store` at `3d692a7`. What is
*inferred*: that the observation window is the dominant term in A2's 9.2× (the
preamble growth and extension churn are co-contributors, unsplit), and the exact
filename mechanism behind the A2 collision — the title collision and the agents'
own reports of it are measured, the derivation from `deliverableLineFor` is a
reading of the code rather than an instrumented trace. The X2 and v3.1 numbers
carry the 8-way-load caveat in §9.2.

### 9.8 How to reproduce (campaign 2)

Everything is on the same pod (`root@216.81.151.50 -p 12708`, key
`~/.ssh/runpod_ed25519`), under `/root/ab2/`:

```
/root/aforge-chatv2      61923d7  (sha256 b31b6c8c…, == fresh build of committed HEAD)
/root/aforge-old3d69     3d692a7  (sha256 5b405ac1…)
/root/ab2/tasks/task{K1,K2,K3,A1,A2,X1,X2}.txt   the seven prompts
/root/ab2/seedK3/                                 the four-defect seed repo
/root/ab2/run_new.sh  run_old.sh  run_new_v31.sh  run_old_v31.sh   runners
/root/ab2/logs/<arm>-<task>.{stdout.json,stderr.log,wall,rc}       new arm
/root/ab2/logs/old-<task>.{plan.log,run.log,plan.wall,run.wall}    old arm, run.log timestamped
/root/ab2/stores/<arm>-<task>/{<task>.db | graph.json,done.json}
/root/ab2/work/<arm>-<task>/                      workspaces, gradeable with go test
/root/ab2/analyze2.py  deliv.py  verify.sh        analysis
/root/ab2/results.json                            the table in §9.3
```

One run, each arm:

```bash
# new arm
AFORGE_HOME=/root/ab2/home/new-A2 AFORGE_DAILY_BUDGET=60 AFORGE_PRACTICE_BUDGET=0 \
/root/aforge-chatv2 do "$(cat /root/ab2/tasks/taskA2.txt)" \
  --db /root/ab2/stores/new-A2/A2.db --keep -w /root/ab2/work/new-A2 \
  --model deepseek/deepseek-v4-flash --timeout 900 --json --yes-spend

# old arm — two steps, because `do` does not exist at 3d692a7
AFORGE_PROFILE_DIR=/root/ab2/home/old-A2 AFORGE_MODEL=deepseek/deepseek-v4-flash \
/root/aforge-old3d69 plan "$(cat /root/ab2/tasks/taskA2.txt)" --brief -o /root/ab2/stores/old-A2/graph.json
AFORGE_PROFILE_DIR=/root/ab2/home/old-A2 AFORGE_MODEL=deepseek/deepseek-v4-flash \
/root/aforge-old3d69 run /root/ab2/stores/old-A2/graph.json \
  -w /root/ab2/work/old-A2 -o /root/ab2/stores/old-A2/done.json | python3 /root/ab2/ts.py
```

New-arm evidence is the same SQL as §6. Old-arm evidence is different — there is no
store — so it comes from `done.json` and the timestamped event log:

```sql
-- new arm only
SELECT id,stage,status,subharness,splice_subharness,started_at,finished_at FROM nodes ORDER BY started_at;
SELECT node_id, SUM(prompt_tokens), SUM(completion_tokens), COUNT(*) FROM usage GROUP BY node_id;
```
```python
# old arm: totals and per-node accounting
g = json.load(open("done.json")); g["usage"]          # calls, prompt/completion/cached tokens, cost
[(n["id"], n["state"], n["turns"], n["tokens"], n["stop"]) for n in g["nodes"]]
# old arm: parallelism — overlap the ▶ / ✓ pairs in the epoch-prefixed run.log
```

Quality is graded by running it, not by reading `rc`:

```bash
cd /root/ab2/work/old-K3 && go test -race -count=1 ./... && git diff --name-only HEAD -- '*_test.go'
cd /root/ab2/work/old-K2 && go test -count=1 ./...
```

## 10. Post-fix behavioral validation

Same pod as §6/§9 (RunPod, 16 cores, 503 GiB, x86_64, no GPU), same model
verbatim: `deepseek/deepseek-v4-flash` via OpenRouter. **The binary under test was
cross-compiled from the dirty working tree**, not from a commit — the uncommitted
changes are the fixes. Baseline arm is `/root/aforge-chatv2`, the pre-fix chat-v2
binary already on the pod.

Evidence lives on the pod: stores `/root/v10/stores/<RUN>/`, logs
`/root/v10/logs/`, workspaces `/root/v10/work/<RUN>/`, prompts `/root/v10/*.txt`,
runner `/root/v10/runv.sh`, analyzer `/root/v10/an.py`. Pre-fix comparison runs
`agot1-SVC`, `agot3-LDG` are under `/root/stores/` with logs in `/root/logs/`;
their prompts were recovered byte-identically from their own
`command_requested` payloads and replayed, so V2 and V6 are true replays.

**Co-residency, disclosed.** All eight runs of this battery were launched into one
window (17:28–17:48 UTC) and ran concurrently, each with its own `AFORGE_HOME`,
`--db`, `-w` and log files. Nothing shared a store directory (the §6 hazard).
Both A/B pairs where wall time is the point — V3 fixed vs V3base, V4 fixed vs
V4base — were launched into the *same* loaded window, so those comparisons are
fair. V1/V2/V6's pre-fix baselines (`agot*`, 16:32–16:45) ran in a quieter window,
but every one of them died in under 45 s, so there is no wall-time comparison to
skew.

### The question-per-run table

| run | question | binary | wall | rc | nodes | peak conc. | ratio | prompt tok | compl tok | spend | delivered |
|---|---|---|---|---|---|---|---|---|---|---|---|
| **V1-swe** | does swe live? | fixed | 900.2 s *(wall hit)* | 2 | 1 leaf | 1 | 1.00 | 238,602 | 38,985 | $0.0007 | 15 KB `rbtree.go` + tests, **compiles, tests FAIL**, never converged |
| *baseline* `chatv2-C` §6 | | chatv2 | 5.5 s | — | 1 | 1 | — | — | — | $0.00071 | **instant crash**, `model … -20260423 not found` |
| **V2-parswe** | parallel swe + git race | fixed | 543.6 s | 1 | 8 (7 done, 1 failed) | 2 | 1.08 | 1,057,369 | 43,921 | $0.0874 | 16 files; **`go build ./...` clean, `go test ./...` passes all 6 packages** |
| *baseline* `agot1-SVC` | | chatv2 | 41.0 s | 1 | 12, **all 12 failed** | 3 | 1.75 | 135,744 | 9,232 | $0.0118 | nothing; 12/12 model-not-found |
| **V3-mixed** | per-node harness (F1b) | fixed | 98.0 s | 0 | 4 | 3 | 1.95 | 211,000 | 16,572 | $0.0139 | 4 artifacts; **`go build` clean, `go test ./limiter` ok** |
| **V3base-mixed** | | chatv2 | 15.4 s | 1 | 4 | — | — | — | — | $0.0012 | **nothing**; job-level `swe` → crash |
| **V4-briefs** | fan-out integrity (F4) | fixed | 382.0 s | 0 | 8 | 3 | 2.01 | 2,164,045 | 73,299 | $0.0978 | **5 distinct files, right topic each**; 2 serial overrun rounds |
| **V4base-briefs** | | chatv2 | 246.2 s | 0 | 6 | 3 | 2.06 | 1,396,435 | 44,628 | $0.0627 | **5 distinct files, right topic each** |
| **V5-adapt** | mid-run adaptation | fixed | 900.2 s *(wall hit)* | 2 | 9 | 3 | **0.77** | 1,649,065 | 72,382 | $0.1157 | `eval.md` + `dedup` pkg, **`go test ./dedup` ok**; README correctly cites the winner |
| **V6-ldg** | plan depth/shape | fixed | 1200.2 s *(wall hit)* | 2 | 24 (8 done, 3 failed, 13 pending) | 3 | 2.27 | 3,144,222 | 195,118 | $0.8007 | 24 real source files; **import cycle, does not build** (13 nodes never ran) |
| *baseline* `agot3-LDG` | | chatv2 | 78.4 s | 1 | 24, **all 24 failed** | 3 | 2.43 | 241,302 | 19,157 | $0.0200 | nothing; 24/24 model-not-found |

`ratio` = sum of leaf seconds ÷ leaf span. `peak conc.` = maximum overlapping
leaf intervals from `started_at`/`finished_at`.

### Fix-by-fix verdict

**F1 model resolution — fixed, and it is the difference between nothing and
something.** `aforge doctor` on the fixed binary prints the new row and the
baseline prints no such row:

```
coding model     deepseek/deepseek-v4-flash · the coding worker can price it
```

Live: every pre-fix swe leaf died in 0.3–2.9 s with `models.dev: model
"deepseek/deepseek-v4-flash-20260423" not found for provider "openrouter"` —
12/12 in `agot1-SVC`, 24/24 in `agot3-LDG`, 1/1 in `chatv2-C`. Post-fix that string
appears **zero times** across all six fixed runs. V1's single leaf ran 893.5 s;
V2's seven leaves ran to completion; V6 produced 24 real source files.

**F1b linear verdict — fixed and visible in the nodes table.** This is the
per-node diversity that was impossible before. `V2-parswe`, inside a job whose
`splice_subharness` is `swe` throughout:

```
task-2-n1  done  subharness=linear  splice_subharness=swe   model-types
task-2-n5  done  subharness=swe     splice_subharness=swe   inmem-impl
task-2-n6  done  subharness=linear  splice_subharness=swe   table-tests
task-2-n7  done  subharness=swe     splice_subharness=swe   billing-service
```
Distribution — V2: `linear`×5, `swe`×3. V6: `linear`×16, `swe`×8. V5 shows the
mirror case, a `swe` specialist (`tests_benchmark`) inside a job with no
job-level worker at all. Pre-fix `agot1-SVC` was uniform `swe`×12 and
`agot3-LDG` uniform `swe`×24 — job-level inheritance, exactly as diagnosed.
Note `subharness` stays `''` rather than `'linear'` when the sizing pass named no
specialist at all (V3, V4) — `recordGeneralist` only stamps when
`len(specialists) > 0` (`internal/plan/size.go:501-507`), which is consistent with
the fix's contract but means "" still carries two meanings in all-generalist jobs.

**F4 sibling collision — the fix is right, but the headless path never had the
bug.** The pre-fix corruption is real and is on disk at
`/root/ab2/work/new-A2/165-complete-briefs.md`: **one** 3,200-word file where the
trunk arm produced eleven (`/root/ab2/work/old-A2/01-…md` … `11-synthesis.md`),
containing `# Bloom Filters` followed by `# Submesh` — different briefs
concatenated into one file, and the `165-` prefix is the shared `CreatedSeq`
that `leafOutputHint` keyed on. That is `cmd/aforge/chat.go:1673`, the chat path.
The headless scheduler already keyed on the unique plan node id
(`internal/exec/schedule.go:514`), and V4 confirms it empirically **on both
arms**: fixed and pre-fix each wrote five distinct correctly-named files with the
right topic in each. **F4 is a chat-path fix; `aforge do` was never affected.**

**swe git-race — fixed, no counter-example.** `git exit 128` / `.git/index.lock`
appears nowhere in this battery. V2 bootstrapped 7 leaves in one job workspace and
V6 bootstrapped 11, all cleanly.

### Verdict per axis

**Parallelism — still capped at 3, and that is now the dominant wall-time cost.**
Peak concurrent leaves was **3 in seven of the eight runs** and 2 in V2. Never 4.
V6 is the proof: 13 nodes pending, 3 running, 16 cores idle, for twenty minutes.
`GovernorMinInFlight = 3` (`internal/exec/governor.go:48`) is a compile-time
constant, and the only dynamic input — runnable threads per core, `Admit` at
`internal/exec/governor.go:92-118` — can only ever *lower* admission. A leaf
parked on an HTTP socket contributes ~zero load, so the governor can never
justify exceeding its own floor; the floor is the ceiling. Two amplifiers:
a single refusal ends the entire claim pass rather than skipping one candidate
(`internal/resident/runner.go:365-368` returning into `runner.go:327-329`; same
shape at `internal/exec/schedule.go:176-178`), and overrun rounds are serial by
construction (`internal/resident/overrun.go:36-38`) — V4's `task-2-n1 → -x1 → -x2`
added 171 s of tail with nothing else running, and V5's ratio fell to **0.77**,
below 1, because a 271 s delivery node and a 45 s extension ran alone at the end.
Plan shape serializes too: V2's DAG was effectively a chain (in-degree
`{1:5, 2:1, 3:1}`, stages 1→5) for work whose model/fleet/rider parts are
independent.

**Adaptation — the mechanism exists, is reachable, and fired zero times.** The
revision sentinel *is* wired into headless: `cmd/aforge/do.go:256` `headlessBrain`
→ `buildBrain` (`cmd/aforge/chat.go:172`) → `NewRunner` (`chat.go:482`) →
`reviseAfter` on every landed leaf (`chat.go:906`) → `reviseOn`
(`chat.go:2594-2631`). Across eight runs it produced **no `add`, `remove`,
`rewire` or `retitle` operation**. Every mid-run `plan_graph`/`subtree_spliced`
event in the battery was an *overrun* replan — V4 at t+208.1 (`task-2-n1-x1`),
V5 at t+854.9 (`task-2-x1`) — i.e. triggered by a leaf running out of budget, never
by what a sibling learned. What *does* adapt mid-run is the **worker**:
`node_worker_changed`, "escalated from linear after a failed attempt", 8 times
(V2×2, V5×1, V6×5). And semantic adaptation through edges works: V5's evaluation
picked Approach A and the downstream implementation's README says *"The chosen
approach is Approach A … selected by the evaluation in ./eval.md"* — but those
implementation nodes were already in the t=0 splice; only the *content* flowed,
never the *shape*. **No sibling-result-driven structural change exists in
practice.** That is the negative finding for the AGoT phase.

**Plan quality, coverage, depth — coverage recovered, depth did not.** V6's graph
is a genuine DAG: 24 nodes, **69 `feeds_into` edges**, in-degree distribution
`{1:3, 2:8, 3:9, 6:1, 8:1, 9:1}` — real fan-in, not a chain. Capability coverage is
broad: schema + migration runner, CSV/OFX import, rule CRUD + categorisation, FX
engine, double-entry accounts, reconciliation + match + confirm + adjust, monthly/
quarterly/year-end reports, CLI root/config, plus six test nodes — the nine
capability areas are all represented, against the pre-fix 4-of-9 defect. **But the
parent-depth histogram was `{0:1, 1:1, 2:N}` in every single run of the battery,
fixed and baseline alike: one spine root, one task node, and every work node a
flat sibling. Max depth 2, no nesting anywhere, ever.** A 24-wide flat sibling set
is precisely the shape a 3-wide governor throttles worst.

**Harness allocation — correct per node now, but arrived at by paying for a
failure.** F1b works. What it exposes is that the *initial* verdict is wrong often:
8 escalations of the form `{"subharness":"swe","previous":"linear","reason":
"escalated from linear after a failed attempt"}`, each costing a full failed leaf
run — V6's `migration_runner` burned 221 s before escalating, `report_command` 522 s
before escalating and then died anyway. The discriminator is weak in *both*
directions: V3's explicitly-code deliverable was sized entirely generalist and the
generalist delivered a token-bucket limiter that builds and passes its tests. So
the sizing pass neither reliably recognises code nor reliably needs the specialist.

**Token cost — the fixed arm buys real output, but two levers are untouched.**
The honest comparisons: V3 $0.0139 for four working artifacts against V3base
$0.0012 for nothing; V6 $0.8007 for 24 real source files against `agot3-LDG`'s
$0.0200 for zero. Where output is *identical* the fixed arm is worse: **V4 $0.0978
/ 382 s vs V4base $0.0627 / 246 s** — +56 % spend, +55 % wall, for the same five
correct briefs, caused entirely by two serial overrun rounds on one leaf. Two
structural facts dominate the bill: prompt-to-completion ratios of **29:1** (V4:
2.16 M prompt, 73 K completion) and **16:1** (V6: 3.14 M / 195 K), and
**`cached_tokens = 0` in every run of the battery** — no prompt caching is in play
at all, which is the single largest untouched token lever.

**Wall time — nothing that finished got faster; three of six runs hit the wall.**
V1 (900 s), V5 (900 s) and V6 (1200 s) all timed out. The fixed binary converts
instant failures into long real runs, which is the point, but the wall is now
governed by 3-wide concurrency, serial overrun chains, and failed-attempt-then-
escalate.

### New defects found post-fix

1. **swe delivery-node crash.** V2 finished `rc=1` with
   `the coding pipeline crashed: No user message found in stream. This should
   never happen.` on the delivery node — while the actual deliverable builds clean
   and passes tests in all six packages. A working result reported as a failure.
2. **swe internal scheduler stall.** V6 `report_command` and `match_engine` died
   with `root graph drain stalled: root scheduler stalled: resource pause
   persisted for 12 cycles`, burning 513 s and 210 s respectively.
3. **Outcome narration contradicts the artifacts.** V4 returned `rc=0` with five
   correct files on disk *and* the text *"The work was still mid-flight when time
   ran out … Nothing here is the answer."* V1 reported *"The time limit was
   reached before anything finished"* with `artifacts: []` while 15 KB of
   compiling `rbtree.go`, a test file and a dozen `wip(edit)` commits sit in the
   workspace.
4. **swe spend is under-reported.** V1's node summary reads `swe: deadline after
   0 cycles, $0.0000` after 893 s of real work; its `usage` rows show 238,602
   prompt tokens for $0.0007, against V2's 1,057,369 for $0.0874 — a ~35×
   discrepancy in $/token between two runs of the same worker on the same model.
5. **swe gives every session its own module cache.** `/tmp/codeaf-scratch/ses_*/
   go-mod` — one V6 session pulled 329 MB of `modernc.org/sqlite` on its own,
   ignoring the machine `GOMODCACHE`. Concurrent swe sessions each re-download the
   same dependencies; this filled the pod's 5 GB root filesystem to 100 % mid-
   battery and had to be cleared by hand.

### Top 3 remaining bottlenecks, ranked by expected impact on wall × cost × quality

1. **`GovernorMinInFlight = 3` is the de-facto global concurrency ceiling.**
   `internal/exec/governor.go:48`, with `Admit` at `internal/exec/governor.go:92-118`;
   amplified by the pass-aborting refusal at `internal/resident/runner.go:365-368`
   → `internal/resident/runner.go:327-329` and `internal/exec/schedule.go:176-178`.
   Measured peak of 3 in seven of eight runs, with V6 holding 13 ready leaves
   behind it on 16 idle cores. V6's completed leaves summed 2,604 s of work into a
   1,147 s span; a fan limited by the longest leaf (~513 s) rather than by 3 is
   worth roughly half the wall on every wide job. The governor measures host load,
   which a socket-parked leaf does not generate — the admission signal should come
   from the provider limiter's own AIMD (`internal/provider/limiter.go:21`, already
   climbing to 64), and a refusal should skip a candidate, not end the pass.

2. **Harness is chosen at sizing and corrected only by paying for a failed leaf.**
   `internal/plan/size.go:459-478` (`sizeApply`) and `internal/plan/size.go:501-507`
   (`recordGeneralist`). Eight `node_worker_changed` escalations in this battery,
   each preceded by a wasted full-length attempt (V6 `migration_runner` 221 s,
   `report_command` 522 s then dead). It costs wall time and tokens on every
   coding job, and V3 shows the reverse error is equally live — the generalist
   shipped a building, passing Go package the sizer never routed to swe.

3. **Plans are flat (max depth 2) and structurally frozen at t=0.**
   `internal/plan/plan.go:274-400` builds once. The result-driven mutator is
   reachable — `cmd/aforge/do.go:256` → `cmd/aforge/chat.go:172` → `chat.go:482` →
   `chat.go:906` → `chat.go:2594-2631` — and fired zero structural operations in
   eight runs; the only mutators that ever ran were failure-driven
   (`internal/resident/overrun.go:125-217`, serial by construction per
   `internal/resident/overrun.go:36-38`, capped at 3 rounds by
   `internal/resident/overrun.go:35-49`; and `internal/revision/judge.go:503-531`).
   So a too-big leaf can only be decomposed by first exhausting its budget, one
   round at a time — and `locks.pass` held across the sentinel round-trip
   (`cmd/aforge/chat.go:2620-2621`) serialises revision per job exactly when a wide
   fan-out is landing results fastest. This is the bottleneck the AGoT redesign is
   aimed at, and the battery says it is untouched.

---

## 11. Perf-wave acceptance

Same pod, same model (`deepseek/deepseek-v4-flash` via OpenRouter), same
prompts — the §10 prompt files were reused byte-for-byte from `/root/v10/*.txt`,
which are themselves the payloads recovered from the original runs'
`command_requested` events. The binary under test was cross-compiled from the
**dirty working tree** at `chat-v2` @`61923d7`+321 modified files; nothing was
committed. Note the tree kept moving during the battery — 143 files, including
`internal/exec/linear.go`, `swe.go`, `tools.go` and `cmd/aforge/chat.go`, have
mtimes after the 14:25 EDT build — so the binary measured here is a **snapshot**
of the wave, and line numbers cited below are given for both that snapshot and
the tree as of writing where they have moved.

Evidence on the pod: stores `/root/v11/stores/<RUN>/`, logs `/root/v11/logs/`,
workspaces `/root/v11/work/<RUN>/`, runner `/root/v11/runv.sh`, analyzer
`/root/v11/an.py`, 15-second concurrency sampler `/root/v11/logs/sample.log`.
Baselines stay where §10 left them, under `/root/v10/`.

Four runs, each with its own `AFORGE_HOME`, `--db`, `-w` and logs. P1/P2/P3
launched together at 18:26:28 UTC; P4 (a second sample of the P2 prompt, added
because P2's result turned out to hinge on one node) launched 18:35:52. Total
wall for the battery was P1's 30 minutes.

**The host was loaded from outside.** `/proc/loadavg` read 38–51 on 16 cores for
most of the window, from other tenants — only one aforge-owned process (`cc1`)
ever appeared in `top`. This matters: 45/16 ≈ 2.8 runnable threads per core is
far above the old `GovernorLoadCeiling` of 1.5, so under the pre-wave governor
the load gate was **shut for the entire run** and admission could never exceed
`GovernorMinInFlight = 3`. That is the mechanical explanation for §10's "peak 3
in seven of eight runs", and it means these runs test the new governor under
exactly the condition that pinned the old one.

### Before/after, per run

**P1 — large ledger (§10 `V6-ldg`). The headline result.**

| | §10 V6 (pre-wave) | P1 (post-wave) | |
|---|---|---|---|
| peak concurrent leaves | **3** | **7** | ▲ |
| parallelism ratio | 2.27 | 2.36 | ▲ |
| nodes | 24 — 8 done, **3 failed, 13 never ran** | 17 — **15 done, 0 failed**, 1 pending (delivery only) | ▲ |
| wall | 1200.2 s *(wall hit)* | 1800.2 s *(wall hit)* | censored |
| prompt tokens | 3,144,222 | 11,611,747 | — |
| completion tokens | 195,118 | 286,107 | — |
| cached tokens (journal) | 0 | 18,688 | ▲ (but see D1) |
| spend | $0.8007 | $0.8221 | ≈ |
| **$ per M prompt tokens** | **0.255** | **0.071** | ▲ 3.6× cheaper |
| quality | 24 source files, **import cycle, does not build** | 34 source files, **`go build ./...` clean, `go test ./...` passes 10/10 packages** | ▲ |

Both runs hit their wall, so wall time is censored and not comparable. What is
comparable is what was inside it: V6 left **13 of 24 nodes never started** and
three swe leaves dead of `resource pause persisted for 12 cycles`; P1 finished
**every leaf it planned, none failed**, and the only unrun node is the final
delivery node. The seven-wide stage is the proof the governor moved —
`task-2-n10` … `task-2-n16` all claimed within 25 ms at t+546.2, five of them
`swe` running real compilers concurrently under the new `LocalLeafCap`.

The long pole is now a single leaf, not the gate: `task-2-n15`
(`balance_subcommand`) ran 1253.9 s alone at the end. `sum(leaf seconds)` 4148.2
over a 1760.0 s span.

**P2 / P4 — five parallel briefs (§10 `V4-briefs`). Two samples, wide spread.**

| | §10 V4 (pre-wave) | P2 | P4 |
|---|---|---|---|
| peak concurrent leaves | **3** | **5** | **5** |
| leaf start spread | staged: 3, then 1, then 1 | all 5 within **16 ms** | all 5 within **23 ms** |
| parallelism ratio | 2.01 | 1.31 | 2.21 |
| wall | 382.0 s | 485.2 s (+27 %) | **211.6 s (−45 %)** |
| prompt tokens | 2,164,045 | 3,752,010 | 1,719,357 |
| cached tokens (journal) | 0 | 4,608 | 5,888 |
| **true cache reads (trace)** | **1,957,632 / 2,127,407 = 92.0 %** | 3,469,568 / 3,670,785 = **94.5 %** | 1,603,328 / 1,703,158 = **94.1 %** |
| spend | $0.0978 | $0.1464 (+50 %) | **$0.0693 (−29 %)** |
| quality | 5 distinct correct files; delivery gate **failed, unrepaired** | 5 distinct correct files; gate failed → **repaired** by `task-2-x1` | 5 distinct correct files; gate **passed first time** |

No F4 regression in either sample: five files, five topics, one topic per file,
560–1027 words each.

The two samples of the same prompt on the same binary differ by 2.3× in wall and
2.1× in cost, and the whole difference is one node. The parallel fan is
consistently faster — V4's leaf phase spanned 367 s (three at a time, plus a
serial `n1 → x1 → x2` overrun chain), P2's five-wide fan finished in 188 s and
P4's in 198 s. What P2 then spent 284 s on was a **delivery node that melted
down**: 816,347 prompt tokens and 213 s to compose a final message (V4's
delivery node used 3,023 tokens and 2.2 s), emitted only three of the five
briefs, tripped the gate, and cost a further 70 s of serial repair. P4's
delivery node did the same job in 1.0 s. See D2.

**P3 — mixed harness (§10 `V3-mixed`).**

| | §10 V3 (pre-wave) | P3 (post-wave) |
|---|---|---|
| peak concurrent leaves | 3 | 3 *(plan was only 3 leaves wide — width-bound, not gate-bound)* |
| parallelism ratio | 1.95 | 2.07 |
| leaf start spread | — | all 3 within **16 ms** at t+12.6 |
| wall | 98.0 s | 131.4 s (+34 %) |
| prompt tokens | 211,000 | 879,986 (+317 %) |
| model turns (trace) | 30 | 81 |
| cached tokens (journal) | 0 | 3,072 |
| **true cache reads (trace)** | 168,448 / 197,956 = **85.1 %** | 791,808 / 866,478 = **91.4 %** |
| spend | $0.0139 | $0.0376 (+170 %) |
| harness diversity | all-generalist (`subharness=''` ×5) | all-generalist (`subharness=''` ×5) |
| quality | **4 artifacts**: `limiter.go`, `limiter_test.go`, `docs/decision-memo.md`, `docs/runbook.md`; builds, tests pass | **3 artifacts**: builds clean, `go test ./limiter` ok, `docs/runbook.md` present — **`docs/decision-memo.md` never written to the workspace** |

P3 gave no harness-diversity signal: as in §10, the sizing pass named no
specialist, so every node is `subharness=''`. The per-node diversity claim (F1b)
is instead re-confirmed by **P1**, which mixes `linear`×10 and `swe`×6 inside one
job whose `splice_subharness` is `swe` throughout, with five
`node_worker_changed` escalations at t+658 … t+734.

### Verdict per axis

**Concurrency — unlocked. This is the wave's unambiguous win.** Peak concurrent
leaves went 3 → **7** (P1) and 3 → **5** (P2, P4), and in every run the whole
ready set claimed within tens of milliseconds instead of being handed out three
at a time. It happened while the host's load average sat near 45 — the exact
condition that shut the old gate — which is direct evidence that API-bound
leaves no longer consult host load. The swe-class CPU cap behaved: five swe
leaves compiled concurrently in P1 without pinning the box or failing. Plan
width, not the governor, is now the binding constraint (P3 could only ever reach
3 because its plan had three leaves), and the residual tail is one long leaf
(P1's 1253.9 s `balance_subcommand`) — "the interesting bug is upstream of here",
as `GovernorInFlightCeiling`'s own comment predicts.

**Token cost — improved where it is measurable, and §10's premise was wrong.**
The single most important correction this section makes: **`cached_tokens = 0` in
§10 was a journalling defect, not an absence of caching.** The per-turn traces
for the §10 runs themselves show cache hit rates of **85.1 % (V3), 92.0 % (V4),
90.5 % (V6)** — prompt caching was already carrying nine tenths of every prompt
before this wave. §10's conclusion that caching was "the single largest untouched
token lever" is retracted; the lever was already pulled, and only the meter was
broken.

Against that corrected baseline the wave's caching work is a real but modest
gain: hit rate 92.0 % → 94.1–94.5 % on the briefs task and 85.1 % → 91.4 % on
the mixed task, consistent with a system message that is now byte-identical
across leaves and a tool block that only ever appends. Where it shows up
loudest is P1's unit price: **$0.255 → $0.071 per million prompt tokens**, a
3.6× drop, for a job that cost the same money and delivered a working
application instead of a broken one.

Raw spend per run is up on three of four samples (P3 +170 %, P2 +50 %, P4
−29 %), but the raw number is measuring different amounts of work: P3 spent 81
model turns where V3 spent 30, and P2 spent a delivery meltdown V4 never had.
Where output is genuinely identical — five correct briefs — the honest read is
the two-sample spread $0.069 / $0.146 against a one-sample baseline of $0.098.
**Cost neither clearly improved nor clearly regressed; its variance grew.**

**Wall time — better inside the fan, worse in the tail, net unproven.** The
parallel phase is decisively faster (briefs: 367 s → 188/198 s). Whole-run wall
went 382 → 211.6 s (P4, −45 %) and 382 → 485.2 s (P2, +27 %) on the same prompt
and binary; P3 went 98 → 131 s; P1 and V6 both hit their walls and cannot be
compared. The concurrency win is real and is being spent, in the bad samples, on
serial post-fan work — delivery nodes and repair extensions — which the wave did
not touch. §10's finding that "overrun rounds are serial by construction"
survives intact.

**Quality — improved on the hard job, regressed on one prose deliverable.**
P1 is a large improvement: a 34-file Go application that builds and passes ten
test packages, against V6's 24 files with an import cycle that did not build, at
the same price. Every §10 swe defect is gone across all four runs — **zero node
errors**, and zero occurrences of `resource pause persisted`, `.git/index.lock`,
`exit 128`, `No user message found in stream`, `not found for provider`, or
`no space left`. The briefs task held (5/5 correct in both samples). P3 lost a
requested deliverable (D3).

**swe accounting — fixed.** §10 defect 4 was swe leaves recording ~$0.0000 after
real work (V1: 238,602 prompt tokens for $0.0007 = $0.003/Mtok, against V2's
$0.083/Mtok — a ~30× spread between two runs of the same worker on the same
model). P1's five swe leaves each recorded real money — $0.0744, $0.0947,
$0.0856, $0.0945, $0.2967 — at **$0.088/M prompt tokens**, within 6 % of V2's
healthy rate, and every exit path produced a row. V6's swe rows priced at
$0.309/Mtok, 3.5× the healthy rate, because they were billing cache reads as
fresh tokens. The spread across swe leaves is now ~2×, not ~100×.

**swe shared toolchain cache — fixed, and it is what kept the box alive.** Five
concurrent swe sessions in P1 shared **one 159 MB** cache at
`/root/v11/homes/P1-ldg/cache/toolchain`, and `/tmp/codeaf-scratch/ses_*` stayed
at **zero entries** for the whole battery. Root filesystem never went above 42 %,
against §10 where per-session module caches (329 MB of `modernc.org/sqlite`
apiece) filled a 5 GB root to 100 % mid-battery and put the scheduler into the
resource pause that killed `report_command` and `match_engine`. Those two
failure modes did not recur.

### New defects surfaced

**D1. The cached-token fix reaches the structuring path and not the leaf path,
so 99 % of cached tokens are still journalled as zero.** Every node in P1 has two
usage rows: a small structuring row (~3,000 prompt tokens) that now carries a
cache count, and the large leaf-execution row (up to 1,877,154 tokens) that
carries `cached_tokens = 0`. The trace for the same leaves shows 91–95 % cache
hits. The break is exact and located: `internal/exec/linear.go:1111` accumulates
`usage.CachedTokens` correctly, `internal/resident/runner.go:785` writes
`result.CachedTokens` to the journal, and `ExecResult.CachedTokens`
(`internal/resident/runner.go:42`) is **never assigned** — all three construction
sites — `cmd/aforge/chat.go:879`, `:964`, `:1258` as built, `:897`, `:982`,
`:1275` in the tree at the time of writing — copy `spent.PromptTokens`,
`spent.CompletionTokens` and `spent.Cost` and silently drop `spent.CachedTokens`.
The acceptance criterion "SUM(cached_tokens) > 0" passes only on the structuring
rows. The one-line consequence: cache discipline remains unfalsifiable from the
`usage` table, which is the table anybody audits.

**D2. Delivery nodes on wide fan-outs can melt down, and it is the dominant
source of wall/cost variance.** P2's `task-2` spent 816,347 prompt tokens and
213 s composing a final message, dropped two of the five briefs from it, tripped
the delivery gate (`{"pass":false,"gap":"The deliverable contains only three
briefs …"}`) and forced a serial 70 s `write-missing-briefs` extension. P4's
delivery node did the identical job in 1.0 s and 3,000 tokens; V4's took 2.2 s.
Correlated evidence for the mechanism: decay spills doubled, 66 → 132 files under
`scratch/.obs/`, consistent with `maxObservationBudget` dropping 256 KB → 64 KB
and a delivery node needing all five briefs resident at once. **Not proven** —
isolating it needs a build with the window restored — but it is the one change
in the wave that would plausibly cause a node whose whole job is holding N
results in context to start losing them. This defect alone accounts for P2 being
the only sample where the wave looks like a regression.

**D3. A leaf can satisfy its output hint and never write the file the user asked
for.** P3's `task-2-n2` was told to write `./docs/decision-memo.md`. It wrote
1,146 words of correct, on-topic content to
`/root/v11/stores/P3-mixed/scratch/task-2-n2-write-docs-decision-memo-md-a-rigorous.md/decision-memo.md`
— creating the leaf's single-file output hint as a **directory** and putting the
deliverable inside it — and never wrote `./docs/`. It reported *"The file is
written and verified"*, the node settled `done`, and the run finished `rc=0` with
the memo absent from both the workspace and the artifact list. §10's V3 wrote
both the hint file and `docs/decision-memo.md`. Attribution to the wave is
unproven; the candidate is the contract moving out of the system message into the
head of the brief (`internal/exec/linear.go`, `system` → `brief`), which is the
text that tells a leaf where its deliverable goes.

**D4. A run's own spend summary can under-report its journal by a third.** P1's
`--json` reports `spend: 0.5255` and `nodes: 16`; its `usage` table sums to
**$0.8221** across 17 nodes. The gap is the last swe leaf, which landed at the
wall. A receipt that is 36 % low is worse than no receipt.

**Carried over, unfixed: §10 defect 3, narration contradicting artifacts.** P1
returned *"The time limit was reached before anything finished"* over a 34-file
Go application that builds clean and passes ten test packages. P3 returned a
success narration for a run missing one of its three requested deliverables.
Judging these runs by their own narration would have inverted every quality
verdict in this section — the §10 warning holds, and the wave did not address it.

### Summary

| axis | verdict |
|---|---|
| concurrency | **improved, decisively** — 3 → 7 (P1) and 3 → 5 (P2/P4), under the host load that used to pin it |
| wall | **held** — parallel phase −49 %, but serial delivery/repair tail eats it in the bad sample; −45 % / +27 % on two samples of one prompt |
| token cost | **held, unit price improved** — $/Mtok 0.255 → 0.071 on P1; raw spend up on 3 of 4 samples because more work happened; §10's "no caching at all" premise retracted |
| quality | **improved** — P1 builds and passes all tests where V6 did not build; all §10 swe failure modes eliminated; one prose deliverable lost (D3) |
| swe accounting | **fixed** — honest, consistent $/Mtok on every exit path |
| swe disk hygiene | **fixed** — one shared 159 MB cache for five sessions, zero `ses_*` dirs, disk flat at 42 % |

The wave did what it set out to do on the governor, on swe hygiene and on swe
spend honesty. Its caching work is real but smaller than advertised, because the
gap it was aimed at was a broken meter rather than a cold cache — and the meter
is still broken on the path that carries the tokens (D1). The next bottleneck is
no longer admission: it is the serial post-fan tail (D2) and the single
long-running leaf.

### D2 diagnosis — the window is exonerated; the leaf's wallet was silently multiplied by ~8

Controlled A/B on the same pod, same prompt (`/root/v12/V4-briefs.txt`, byte-identical to
`/root/v11/V4-briefs.txt`), same model, four runs launched together at 19:21 UTC under host
load 12–28/16 cores, each with its own `AFORGE_HOME`, `--db`, `-w`. Two binaries
cross-compiled from the same dirty tree differing in exactly one constant,
`internal/exec/context.go:maxObservationBudget` (`64 << 10` vs `256 << 10`; the constant was
restored and `git diff` re-verified afterwards). Evidence: `/root/v12/`.

**Mechanism (static, then confirmed in-trace).** The window is not what a melting node is
spending. `internal/exec/linear.go:spent()` was `PromptTokens + CompletionTokens` at
`61923d7`; the perf wave rewrote it to weight cache reads at `cachedTokenWeightPercent = 10`,
and `defaultLeafTokens` stayed at **150 000**. The trace of `A2/task-2-n3` shows the
consequence directly — `turn 25 in=17193 out=72 cached=16896`. That turn charges
`(17193−16896) + 16896×0.10 + 72 = 2 059` against the ceiling instead of 17 265. At the 92–98 %
hit rates these runs actually see, a leaf's real allowance is **~1M raw prompt tokens, ~8× the
pre-wave 150k** — and `defaultLeafTokens`' own comment says why that matters: *"the binding
limit is not a safety net, it is what makes the loop converge. Set it loosely and the model
will spend all of it."* Every observed "meltdown" is a node running to its budget; the budget
moved and the number did not. Per-turn `in` never exceeds ~17k in any trace, i.e. the 64 KB
window is working — the node melts by running very many cheap turns, not by carrying a large
context. Which node melts is a lottery; in P2 it happened to be the delivery node, which is
why that sample also lost briefs and tripped the gate.

| run | window | wall | total prompt | cost | decay spills | `task-2` (delivery) prompt / wall | worst node | gate | briefs |
|---|---|---|---|---|---|---|---|---|---|
| A1 | 64 KB | 420.4 s | 1 258 102 | $0.0557 | 34 | 173 837 / 145.0 s | n2 370 564 | fail → repaired | 5/5 |
| A2 | 64 KB | 406.0 s | 3 759 930 | $0.1364 | 42 | 57 293 / 73.1 s | **n3 1 419 426** | pass | 5/5 |
| B1 | 256 KB | 900.2 s *(wall)* | 1 685 316 | $0.0628 | 66 | **3 321 / 48.6 s** | n1 637 154 | fail, repair never ran | 5/5 |
| B2 | 256 KB | 470.8 s | 2 667 639 | $0.1070 | 84 | 52 292 / 83.0 s | **n3 1 641 030** | fail | 5/5 |
| *P2 (§11)* | 64 KB | 485.2 s | 3 752 010 | $0.1464 | 138 | **828 452 / 213.0 s** | n3 1 054 876 | fail → repaired | 5/5 |
| *P4 (§11)* | 64 KB | 211.6 s | 1 719 357 | $0.0693 | 50 | 3 375 / 1.0 s | n5 1 029 450 | pass | 5/5 |

At `256 << 10` the cap no longer binds for this model and the formula returns ~184 KB, so B is
~2.8× A's window, not 4×.

**Verdict: the window size does not cause the meltdown, and the correlation in D2 was
confounded.** Both arms produced runaway nodes; the two largest in the whole battery
(1 641 030 and 1 419 426) are one per arm. The delivery node ranged 3 321 → 828 452 tokens
across six samples of the same prompt, with its cheapest instance on the **large**-window arm
(B1) and its most expensive on a 64 KB run. Decay spills went the wrong way for the
hypothesis: 34/42 at 64 KB against 66/84 at 256 KB. Spills track how many turns a leaf ran,
and turn count is set by the runaway lottery — the §11 "66 → 132" doubling is a symptom of one
long node, not of a small window. (In P2, all 138 spills belong to a single leaf; the delivery
node wrote none.) The delivery gate failed in 4 of 6 samples across both arms, always with the
same gap — the message points at files instead of carrying the briefs — so that is a chronic
delivery-law defect, not window-related. All six runs wrote 5/5 correct briefs.

**Recommended fix (F7, not applied — it lives in `linear.go`, not `context.go`).** Re-couple
the ceiling to something the discount cannot inflate. Minimal: keep the cache discount (its
premise is right — a warm prefix is genuinely cheaper) but add a second, raw bound next to it,
`usage.PromptTokens > rawPromptCeiling` (≈ 3 × `maxTokens`) also granting the landing reserve,
so a 98 %-cached leaf converges at ~450k raw tokens instead of ~1.2M. Durable: bound what
actually correlates with progress rather than with billing — `maxTurns` is currently a 200-turn
"backstop" set far above real work, and 30–40 would have ended every runaway here at roughly
the pre-wave cost. Recalibrating `defaultLeafTokens` downward is *not* recommended: the
effective multiplier is the provider's hit rate, so the number would have to be re-tuned per
endpoint.

**Secondary, latent, and not observed in any of the six runs.** `observations.admit` /
`pointerTo` (`internal/exec/context.go:548-606`) can form a retrieval cycle: once a result is
stubbed, a re-read returning byte-identical content hashes to the same key, so the model is
handed *"…whose bytes are in `<spill>` — read the part you need with sh"* — a pointer at the
file it just read, forever. The escape is reading a slice (different bytes), which is what the
size-triggered spill's `sed -n '1,80p'` hint teaches; whole-file `cat` has no escape. Zero
occurrences of `identical to the result of` in any trace here, so this is a hazard to close on
its own merits, not the cause of D2. Nothing was landed in `context.go`.

---

## 12. Width-2 incident and the grain fix

**The receipts.** Live store `~/.aforge/graph.db`, job `task-11339`, spliced 2026-08-11T19:30:09Z
against `task-11071`. Read-only copy taken; nothing written to the user's store.

`EventScaleGate` (seq 11349) is the whole reading in one line:

```
structure:""  scale:"project"  route:"bundle"  parts:2  leaves:3
```

The **compiler**, not the planner, chose the width. `route:"bundle"` means the compile returned
`parts:[…]` with two entries, and `parts` filled is spliced directly — the planning pass that
would have divided the units never ran. `structure` came back empty, so the field that was
supposed to force the scale reading contributed nothing.

The two parts were **byte-identical**, not an 8+8 batch:

```
task-11339-n1.brief == task-11339-n2.brief   (466 bytes each, SQL equality true)
"Find the CEO and background for each of the 16 … The startups are: <all 16 named> …"
```

Both were handed the complete enumeration, which was already sitting in the goal (the compiler
had restated all 16 names from `task-11060`'s landed result under "Working decisions"). Only the
generated `contract` differed between them — two paraphrases of one method. A third node,
`Deliver together` (`task-11339`, `kind:synthesis`), was then invented to reconcile two copies of
one answer against each other.

**What it cost.** `usage_recorded`, one job:

| node | prompt tok | completion | cached | cost |
|---|---|---|---|---|
| `-n1` | 516,410 | 5,924 | 294,912 | $0.0511 |
| `-n2` | 141,874 | 2,385 | 20,736 | $0.0179 |
| `task-11339` (synthesis) | 336,616 | 4,943 | 197,376 | $0.0333 |

~995k prompt tokens and $0.102 buying, at best, one answer twice. Prompt:completion ≈ 66:1 — the
bill is context, exactly as W6 predicted, and the premise in force at the time told every planner
that context was free.

**Why they failed at ~2m — not a timeout, not a tool fault, not an overrun.** Both leaves died on
the same millisecond, and the event immediately before is the tell:

```
19:32:34.251442  seen_touched  {"surface":"tui","state":"detached"}
19:32:34.256158  node_failed   -n2: Post ".../chat/completions": context canceled
19:32:34.258505  node_failed   -n1: … context canceled
19:32:38.215024  seen_touched  {"surface":"tui","state":"attached"}
```

and the synthesis repeated it verbatim 90s later (`19:34:07.331691` detached → `19:34:07.333396`
failed → `19:34:09.767671` attached). The TUI detaching cancels the context the chat-runner's
in-flight provider requests are hung off, so a detach/reattach kills whatever is mid-POST. The
"2m" is only how long the leaves had been running, not a deadline. This is a
`cmd/aforge/chat.go` lifetime bug (runner context tied to surface attachment) and was **not**
fixed here — that file is owned by another lane. The resident's own post-mortem misread it as a
flaky external API and filed a lesson recommending retries (`fact_learned`, seq 11408).

**"grok" is the plan model, not a worker.** `~/.aforge/settings.json` sets
`plan_model:"~x-ai/grok-latest"` beside `task_model:"~deepseek/deepseek-v4-flash-latest"`, and the
nodes carry both (`run_model`/`plan_model`). The worker line "deepseek-v4-flash · grok" is
work-model · plan-model. No grok subharness exists; `subharness` is empty on all three nodes.

**What changed (W6 + width-follows-grain).**

- `internal/plan/plan.go` — `agentPremise` no longer says workers are "instant, free, and unlimited
  in number". It states the trade: the cost is the context each branch re-pays, a split that
  shares almost everything is nearly free, and the person waits for the longest chain and never the
  total. The human-overhead denial (no owners/roles/sign-off) is orthogonal to cost and stays.
- `internal/plan/fanout.go` — a general grain paragraph: where the material already enumerates its
  units (goal, a landed dependency's result, sources), that enumeration *is* the split; never write
  two parts that each cover the same set; and the counterweight in the same breath — units the
  material does not present as standing apart are not made independent by being separated.
- `internal/plan/size.go` — `split_into` reads the grain off the node's own words and the things it
  must touch; and the two-cost paragraph now prices context, not only latency.
- `internal/head/compiler.go` — the WIDTH rule debits context as well as lateness; a new rule
  derives pieces from the material's enumeration; and the `parts` rule gains the disqualifier this
  incident needed: two parts that would return the same thing were never two requests, and one
  request covering many units is one answer in `parts:[]` for the planning pass to divide.
- `internal/exec/subharness.go` — W6's measured-line instruction. The per-worker measured line was
  decoration; the menu now tells the model to read it as evidence about this machine rather than a
  target, and only when figures were actually printed (below the gate the menu is unchanged).
- Goldens re-captured: `internal/head/testdata/compiler_prompt_baseline.golden`,
  `internal/plan/testdata/size_prompt_baseline.golden`. Tests:
  `internal/plan/premise_test.go`, `internal/head/grain_test.go`,
  `internal/exec/measuredline_test.go` — including a no-domain/no-authored-example assertion over
  every string W6 wrote.

No hardcoded examples and no domain words entered any prompt; the only evidence-bearing text is
the measured line, which is generated from `cmd/aforge/selfknow.go` at runtime.

## 13. Real-TUI end-to-end validation

Everything above was measured through `aforge do`. This section is the same question asked of the
product: **does the chat surface a person actually types into split dynamically and parallelize?**
The battery ran through the v2 chat TUI in a tmux pty (220x50) on the RunPod box, driven by
`send-keys` and read by `capture-pane`, against a fresh isolated brain (`AFORGE_HOME=/root/tui-val/home`,
its own `graph.db`) so nothing here is contaminated by earlier campaigns. Work model
`~deepseek/deepseek-v4-flash-latest`, then `deepseek/deepseek-v4-flash` from T2 on (see 13.2).
Every verdict below is read from the store, not from the screen; screenshots are corroboration only.

Totals for the whole battery: **$0.4398, 7.00M prompt / 135.5k completion / 7.64M cached tokens,
106 provider calls, six jobs.**

### 13.1 Per-task results

| task | split shape (`scale_gate`) | vs expected | peak conc. | harness | wall | cost | tokens (p/c/cached) | quality |
|---|---|---|---|---|---|---|---|---|
| **T1** 12 vector DBs (`task-12`) | `project` / `bundle` / leaves 13 / parts 12 | **PASS** — 12 atoms + 1 reconciler, exactly the enumeration | **12** | linear ×12 | 116s | $0.1559 | 1.85M / 40.0k / 1.42M | **FAIL on delivery** (13.3) |
| **T2a** Go CLI (`task-220`) | `task` / `single_leaf` / 1 | PASS (swe owns its own decomposition) | 1 | `swe` | 1s | $0 | — | **crash** (13.2) |
| **T2b** Go CLI (`task-256`) | `task` / `single_leaf` / 1 | PASS | 1 | `swe` | 276s | $0.0612 | 66.4k / 9.98k / 1.72M | **PASS** — builds, vets, tests, works |
| **T3** essay (`task-180`) | `task` / `single_leaf` / 1 | **PASS** — no split, no children | 1 | linear | 22s | $0.0018 | 17.0k / 2.46k / 12.7k | PASS (520 words) |
| **T4** T1+T3 in one message (`task-341`) | `project` / `bundle` / leaves 13 / parts 12 | **PASS** — one task submitted, the other recognised as already done | **12** | linear ×12 | 214s | $0.1288 | 3.26M / 42.4k / 2.97M | deliverable **perfect**, gate wrong (13.3) |
| **T5** 4 reverse proxies (`task-519`) | `project` / `bundle` / leaves 5 / parts 4 | PASS | **4** | linear ×4 | 228s† | $0.0519 | 1.33M / 16.9k / 1.23M | PASS |

† T5 was the crash-recovery probe (13.5); 76s of its wall is the window in which no TUI was running.

**The splitting question is answered, and the answer is yes.** T1 and T4 each produced twelve leaves
whose briefs are twelve distinct 143–146-character instructions — one database each, no two covering
the same set — and all twelve entered `running` inside 57 milliseconds of each other
(`01:12:18.126` → `01:12:18.183`), held 12-wide until the first landed 11s later. The width-2
duplicate-halves failure of §12 does not reproduce anywhere in this battery. T3, the negative case,
took `single_leaf` with zero children and passed its gate in 22 seconds: no node was made for the
sake of making a node. T2 went whole to `swe` as designed.

### 13.2 T2's first attempt: a cost table killed the coding pipeline

`task-220` failed one second after starting, `subharness=swe`:

```
the coding pipeline crashed: models.dev: model
  "deepseek/deepseek-v4-flash-latest" not found for provider "openrouter"
```

The model is real — the same slug had just executed all twelve T1 leaves against OpenRouter. What is
missing is its row in the **models.dev cost table**, and `internal/swepro/internal/modelsdev/models.go:84`
makes that a hard error, which the engine turns into a crashed run. A price lookup is allowed to fail;
it is not allowed to destroy the work.

The guard for exactly this already exists and is dead code: `ModelResolves` and `ModelResolver` in
`internal/swepro/codeaf/modelprobe.go` — a file whose own header says it is "the one question aforge
is allowed to ask the engine without starting it: *would you find this model?*" — have **zero callers
anywhere in the tree**. The route to `swe` is chosen without ever asking. Re-running on
`deepseek/deepseek-v4-flash` (a slug models.dev carries) produced `task-256`, which passed.

T2b's artifacts were judged by building them, not by reading them: `go build ./...`, `go vet ./...`
and `go test ./...` all clean (`ok mdtable`), and the compiled `mdtable` correctly aligned two tables
of differing widths in place in a scratch file. `swe: pass after 1 cycles, $0.0608`, gate `pass:true`.

### 13.3 The delivery gate judges something other than the deliverable

This is the headline defect, and it reproduced on both runs of the same shape.

**T4 is the clean proof.** `task-341`'s summary is a flawless deliverable: twelve profiles,
`**Milvus** — …` through `**FAISS** — …`, in the requested order, one per line, 1,089–1,890
characters each, no preamble and no commentary. The gate's verdict on that text:

```json
{"pass":false,"gap":"The deliverable does not contain the 12 profiles together in one final
 message. Instead, it contains a series of separate messages, each describing or pointing to
 individual profiles written to files, with the actual profile text…"}
```

Not one clause of that describes the message it was judging. Three of the six gates in this battery
returned `pass:false`, and two of them (`task-12`, `task-341`) are demonstrable false negatives on
correct work. The same gate returned `pass:true` on T2b, whose user-facing text was *"The coding run
ended without saying how it went."* followed by a file list — a false positive on the identical
judge. The gate is not strict or lenient; it is reading the wrong material.

**T1 shows what the false negative costs when the repair path fires.** The gate failed `task-12`,
which produced `job_growth {"reason":"gap","lineage":"task-12","adding":1,"round":1,"allowed":true}`
and a repair node `task-12-x1`. That node was spliced **under `root`, not under the failed job**, so
its twelve finished siblings were unreachable from it. It said so plainly — *"the workspace is empty,
the recall index is empty, and the trace log holds no profile content"* — refused, correctly, to
fabricate ten profiles, and delivered two. The gate then failed *that* too. The user's screen ends on
the repair's confusion about missing files; the correct twelve-profile answer, already bought and
already in the store, is never surfaced. **A working answer was replaced by a broken one, for
$0.0087 and 55 seconds.** T4 was spared only because no repair fired.

**And the wrong verdicts are being learned.** Fact 495 was distilled from T4's false negative:

> When assembling a multi-item deliverable that must be delivered in one final message, do not write
> individual items to separate files and report progress… **The gate requires one contiguous output**,
> not a series of messages or files.

It was then injected into all four leaves of T5 (`fact_injected {"fact_seqs":[210,495]}` ×4). The
same happened after T1: facts 211 and 167 — both artefacts of the same misjudgement — reached the
prompt of `task-256`, a *coding* task, alongside fact 239. A broken judge is writing the notebook,
and the notebook reaches every later run.

### 13.4 The head re-authors the deliverable instead of relaying it

After T4 landed, the head wrote the user two agent messages (seq 501, 3,076 B and seq 506, 3,184 B),
both opening *"Here are the 12 technical profiles:"*. Neither is the 16,316-byte researched
deliverable sitting in the card above them. They are re-writes from the model's own knowledge, and
they contradict each other:

| | seq 501 | seq 506 |
|---|---|---|
| Chroma storage | "optional SQLite-backed metadata storage" | "stores collections as Parquet files" |
| Weaviate API | "a GraphQL API" | "GraphQL, REST, and gRPC APIs" |

Two generations disagreeing on fact is proof they were generated rather than relayed. The store's own
verified text says something different again ("shared-storage architecture with fully disaggregated…"
for Milvus, against the head's "Faiss, HNSW, DiskANN, and SCANN"). The research the twelve leaves paid
$0.12 to verify against live documentation does not reach the person; a cheaper, uncited paraphrase
does. T5 repeated it — seq 617 and 621, both *"Here are the four technical profiles:"* — and there
`answers_seq=0` on both rows, so nothing supersedes anything and **both are rendered on screen**
(`capture-pane | grep -c` returns 2). T4's pair at least shared `answers_seq=334`.

### 13.5 Detach and reattach: both halves pass

**Clean quit mid-run (the landed lifetime fix).** `ctrl+c` at `01:24:06` with the `swe` leaf 3 minutes
into `task-256`. stderr:

```
finishing the work already running before closing (up to 2m0s) — ctrl+C leaves it to be picked up next time
```

The process stayed up, the `aforge run` subprocess kept working, and the leaf **completed at
`01:25:36`** — 90 seconds after the window was told to close — passed its gate, and only then did the
process exit 0. Restarting against the same store resumed the same session with full history and the
finished result. The §12 incident (three nodes dying on the millisecond the surface detached) does not
reproduce; the asymmetry `chat.go` now documents is real.

**Hard kill (no grace).** `SIGKILL` at `01:36:26` with three T5 leaves `running`, leaving them stranded
as `running/chat-runner`. A TUI started at `01:37:42` reclaimed all three within one second —
`node_released {"token":1,"next_token":2}` ×3 — re-claimed at token 3, re-ran them (`attempt=2`), and
finished the job at `01:39:37`. The one leaf that had already landed was not redone. Work survives both
a polite close and a crash.

### 13.6 Smaller defects, with their producers

- **Twelve identical rows in the rail.** All twelve T1/T4 leaf titles are the byte-identical string
  `Write a one-paragraph technical profile of`. `clipTitle` (`internal/plan/bundle.go:62`) takes a
  48-character prefix on a word boundary, so parts sharing a long stem — precisely what an enumerated
  bundle produces — collapse to the same name and the discriminating word falls past the cut. The
  function's own comment asks for a name "recognisable in a rail beside its siblings".
- **Working preamble leaks into the deliverable.** T3 opens *"487 words — within reasonable tolerance
  of 500…"*; T1's reconciler opens *"Files n3, n7, n8 are empty…"*; T5's opens *"I now have
  comprehensive nginx information. Let me compile the deliverable."* This is very likely what the gate
  is actually reading in 13.3.
- **Inconsistent leaf output contract.** Nine of T1's twelve leaves wrote
  `task-12-n*-….md` into the workspace; three did not, keeping their profile only in `summary`. The
  reconciler read files, found three missing, and started narrating instead of delivering.
- **Opaque head error.** T4's first submission returned *"hit a provider error answering that — try
  again"* after a call that had in fact succeeded (`usage_recorded`: 20,707 prompt / 145 completion).
  An identical resubmission worked.
- **A contract that could not be parsed** was logged and swallowed: `could not write contracts:
  contract "Write a one-paragraph technical profile of": response contains no JSON object`.

### 13.7 Verdict

**Yes — the real chat surface splits dynamically and parallelizes properly.** Grain-following is
exact on the user's own failure case: twelve named databases became twelve leaves running twelve-wide,
not two duplicate halves. The atomic negative case stayed atomic. The coding case went whole to `swe`,
which built working, tested software. Detach-lifetime holds under both a clean quit and a kill. The
W6 grain-and-burden work and today's lifetime fix are validated on the product surface.

**What is broken is everything downstream of execution.** The engine now reliably buys the right
answer and the delivery path then loses, re-writes, or misjudges it. Queue for W7/W8/W9, in priority
order:

1. **P0 — the delivery gate reads the wrong material.** Two false negatives on flawless deliverables,
   one false positive on a non-answer, same judge, same battery. Find what is handed to it (the sink is
   `internal/store/gate.go`); the working hypothesis is that it sees a prefix or a progress trail rather
   than the final message, which the preamble leak in 13.6 would explain.
2. **P0 — the repair path loses lineage.** `task-12-x1` was spliced under `root`, orphaning the twelve
   finished siblings whose work it existed to assemble. A repair must be born inside the job it repairs.
3. **P0 — the head must relay the deliverable, not re-author it.** Two contradictory paraphrases
   replaced $0.12 of documentation-verified research.
4. **P1 — a false gate verdict must not become a lesson.** Facts 211, 167 and 495 were all distilled
   from misjudgements and injected into later, unrelated runs.
5. **P1 — wire the model probe.** `codeaf.ModelResolves` / `ModelResolver` have no callers; a missing
   models.dev *price row* currently crashes the whole coding pipeline for a model the provider serves.
6. **P1 — one answer per ask.** `answers_seq=0` lets two head answers render side by side.
7. **P2 — `clipTitle` must discriminate siblings**, not share their stem.
8. **P2 — strip the working preamble** from what is delivered, and settle the leaf output contract
   (message, file, or both — but the same one for every sibling).

## 14. Real-issue coding benchmark: aforge vs pi

Run 2026-08-12 on the RunPod CPU box (16 cores), both harnesses on
`deepseek/deepseek-v4-flash-0731` through the same OpenRouter key, aforge built
from `main@61923d7`, pi = `@earendil-works/pi-coding-agent@0.84.1`
(`pi -p --provider openrouter --mode json --no-session --no-context-files`).

Two batteries: four **fresh public GitHub issues** never used here before, and a
rerun of the **bambara acceptance battery** from BENCHMARKS.md §4.

**pi now self-reports cost.** `--mode json` emits `usage.cost.total` per
assistant message. Every "not measured" pi cell in BENCHMARKS.md §1/§4 and the
shared-key caveat in `bench/README.md` are obsolete for pi ≥ 0.84: both arms in
every table below carry a real, self-reported dollar figure. `bench/run.sh`
should add `--mode json` and a cost parser.

### 14.1 Fresh GitHub issues

Closed issues with known merged fixes; each clone pinned to the commit before
the fix; the harness got the issue title+body verbatim (no link to the fix) plus
one line: "Fix this issue in the repository at <path>; the repository's tests
must pass." Grading: the repo's own suite, plus the upstream fix's regression
test dropped in with every top-level identifier renamed (`TestZZB…`) so it
cannot collide with a test the agent wrote.

| # | issue | base commit | fix commit | shape |
|---|---|---|---|---|
| A | [spf13/cobra#2257](https://github.com/spf13/cobra/issues/2257) | `f2878ba` | `746ef07` | one-file bug (`os.Args` aliasing) |
| B | [urfave/cli#2217](https://github.com/urfave/cli/issues/2217) | `57943ed` | `700c663` | cross-file bug (persistent flag / env source) |
| C | [urfave/cli#2324](https://github.com/urfave/cli/issues/2324) | `c66ca03` | `84d0da5` | small feature (`Command.Path`/`Walk`) |
| D | [spf13/afero#576](https://github.com/spf13/afero/issues/576) | `ba8d668` | `295a660` | perf/interface (BasePathFile `WriterTo`/`ReaderFrom`) |

| issue | harness | wall | cost | suite | upstream regression test | verdict |
|---|---|---|---|---|---|---|
| A cobra#2257 | pi | 174s | $0.0274 | green¹ | **pass** | fixed correctly |
| | aforge | 867s | $0.2772 | green¹ | **pass** | **fixed correctly — but self-reported FAILURE** |
| B cli#2217 | pi | 521s | $0.0747 | green | **pass** | fixed correctly |
| | aforge | 1500s (cap) | $0.1952 | green | **pass** | **fixed correctly — but self-reported timeout, `settled:false`** |
| C cli#2324 | pi | 74s | $0.0110 | green | 4/5 (fails `Walk_NilFn`) | correct except unspecified edge case |
| | aforge | 829s | $0.1947 | green | 4/5 (**segfault** in `Walk_NilFn`) | correct except unspecified edge case; worse failure mode |
| D afero#576 | pi | 206s | $0.0237 | green | **pass** | fixed correctly |
| | aforge | 984s | $0.2690 | green | **pass** | fixed correctly |
| **total** | **pi** | **974s** | **$0.1368** | | 3.8/4 | |
| | **aforge** | **4181s** | **$0.9360** | | 3.8/4 | |

¹ cobra's suite has one pre-existing failure at the pin, `TestFailGenFishCompletionFile`,
which asserts a permission error and cannot fail when the process runs as root.
It is unrelated to the issue and identical in both arms; excluded from grading.

**Quality is a tie** (both 3.8/4; both miss only `Walk(nil)`, which the issue
never specifies). **aforge is 4.3x slower and 6.8x more expensive.**

Both harnesses reproduced upstream test names verbatim
(`TestDuplicatePersistentFlagUsesEnvSource`, `TestBasePathFileForwardsWriterTo`)
— these 2026 fixes are almost certainly in the model's training data. It is the
same model on both arms, so the comparison holds, but absolute quality here is
inflated and this battery should not be quoted as a difficulty measurement.

**Decomposition receipts (all four aforge cells):**
`scale_gate` = `{scale: task, route: single_leaf, leaves: 1}`, one node, worker
`swe`, no `job_growth`, no splits, no concurrency. **Grade: correct.** Each
issue is one coherent change; staying atomic is the wall-time- and
cost-optimal call, and there was no missed parallelism. The cost gap is not
decomposition waste.

### 14.2 Bambara acceptance battery (BENCHMARKS.md §4 rerun)

Same repo, same pin `6c978ff`, same prompt template as `bench/run.sh`, baseline
**317 passed**. Every cell was rebuilt from a write-protected pristine tarball
and asserted at 317-passing/clean-tree before the harness started.

| issue | pi (fresh, today) | aforge select | aforge swe-forced | pi (recorded §4) | select (recorded) | swe (recorded) |
|---|---|---|---|---|---|---|
| #20 CI workflow | 39s · $0.0068 · 317 | 526s · $0.1335 · 322 · *swe* | 269s · $0.0898 · 322 | 3m00s | 4m35s · $0.15 | 3m14s · $0.08 |
| #21 arithmetic | 227s · $0.0509 · **565** | **2400s DNF · 317** | 1907s · $0.3316 · **564** | 12m15s · 548 | 30m · $1.23 · 535 | 9m04s · $0.28 · 526 |
| #22 currency | 244s · $0.0356 · 540 **+5 failing** | **2400s DNF · 317** | 1066s · $0.0989 · **317, node failed** | 10m56s · 580 | 36m · $1.29 · 564 | 5m46s · $0.21 · 543 |
| #23 optional CLI | 114s · $0.0164 · 317 | 1100s · $0.0891 · 320 · **linear** | 1121s · $0.1473 · 321 | 2m37s · 324 | 12m24s · $0.81 | 2m57s · $0.11 |
| **total** | **624s · $0.1097** | 6426s · $0.2226 (2 DNF) | **4364s · $0.6676** | | | |

**The recorded pi row has drifted and the old target is stale.** pi 0.84.1 on
this box is 1.4-3.2x faster than the recorded pi on every row (39s vs 3m00s,
227s vs 12m15s, 244s vs 10m56s, 114s vs 2m37s). Its quality moved both ways:
#21 improved to 565 (recorded 548), but **#22 collapsed from 580 to 540 with
five failing tests** — today's pi leaves the suite red on #22, and #23 dropped
from 324 to 317 (no tests added). The headline goal "beat pi's 580 on #22" is
no longer the bar; the bar is 540-with-a-red-suite, which swe's 317-and-failed
still does not clear.

### 14.3 Acceptance-gate verdict

The §4 gate requires cheaper **and** faster **and** higher quality than pi. Read
against fresh same-box pi (the honest comparison):

| axis | verdict | evidence |
|---|---|---|
| cheaper | **FAIL** | swe $0.0898-$0.3316 vs pi $0.0068-$0.0509; 2-13x more per issue, 6.1x on the battery. Fresh-issue battery 6.8x. |
| faster | **FAIL** | swe 269-1907s vs pi 39-244s; 3-8x slower, 7.0x on the battery. Fresh-issue 4.3x. |
| higher quality | **MIXED** | swe wins #20 (322 vs 317) and #23 (321 vs 317); ties #21 (564 vs 565); loses #22 (nothing vs 540). Fresh issues: exact tie 3.8/4. |

aforge does not pass the gate. What it does establish is that **quality parity
is real** — on four never-before-seen public issues it matched pi patch for
patch — and that the deficit is entirely price and latency, concentrated in one
component (14.4.2).

### 14.4 Structural findings

#### 14.4.1 P0 — the delivery verdict is miscalibrated against pre-existing red tests

aforge produced four correct patches on the fresh issues and reported success on
**one**. The two most instructive failures:

- **cobra#2257**: the fix is correct and the upstream regression test passes.
  aforge failed the run on `project build verification failed: make all exited
  2` — because `make all` runs the suite, and the suite contains
  `TestFailGenFishCompletionFile`, which fails *only because the process is
  root* and was already failing before the harness arrived. aforge attributed a
  pre-existing environmental red to its own change. pi ran the same suite, saw
  the same failure, and correctly shipped anyway.
- **cli#2217**: the patch is nearly identical to the upstream fix (+19/+3/+42
  against upstream's +16/+3/+42), passes the regression test and the full suite
  — and was reported `settled:false`, "The time limit was reached before
  anything finished," because the 25-minute wall expired before the run could
  settle. The engine had already committed the work.

Both are the same defect: **the gate judges the suite's absolute state, not the
delta the run caused.** The fix is to snapshot the suite before the leaf runs
and fail only on tests the change turned red. Without it, aforge's own verdict
is unusable as a success signal on any repository with a pre-existing red test —
and it is worse than unusable, because it discards correct work.

#### 14.4.2 P0 — the cost and latency gap is entirely inside the swe leaf

The receipts localise it exactly. Per-run spend splits (fresh issues):

| cell | root / orchestration | swe leaf | orchestration share |
|---|---|---|---|
| cobra#2257 | $0.0006 (1 call, 4.8K tok) | $0.2766 | 0.2% |
| cli#2217 | $0.0010 (1 call, 5.6K tok) | $0.1942 | 0.5% |
| cli#2324 | $0.0007 (1 call, 5.1K tok) | $0.1939 | 0.4% |
| afero#576 | $0.0009 (1 call, 5.5K tok) | $0.2681 | 0.3% |

**aforge's own planning, gating and bookkeeping cost about a tenth of a cent per
run.** `spend_overhead` agrees ($0.0006-$0.0042). Every dollar of the gap is the
subharness engine's inner loop. Token comparison for identical tasks:

| cell | aforge swe leaf (prompt / completion / cached) | pi (input / output / cacheRead) |
|---|---|---|
| cobra#2257 | 670,536 / 27,657 / 2,589,824 | 95,761 / 15,285 / 888,523 |
| cli#2217 | 1,295,313 / 53,627 / 4,593,152 | 167,239 / 49,819 / 2,815,565 |
| cli#2324 | 578,006 / 29,572 / 1,617,920 | 74,020 / 5,216 / 188,947 |
| afero#576 | 844,644 / 42,698 / 1,940,352 | 105,094 / 18,514 / 607,396 |

Completion tokens are comparable — **the engine does not write more, it reads
far more**: 7-8x the fresh prompt tokens for the same patch. On bambara the swe
leaf spends 47-123 API calls per single leaf (#21: 123 calls, 708K prompt, 3.38M
cached, $0.33) against pi's 22-84 for the same issue. This is a context-assembly
problem in `internal/swepro`, not an aforge-architecture problem, and it is
where any cost or latency work must go. Nothing will be won by touching the
planner — there is nothing there to win.

#### 14.4.3 P0 — `aforge do` serialises on a resident lock keyed to the store's *directory* (W7)

`lease` places `resident.lock` in the directory containing the store
(`internal/lease/resident.go`, `filepath.Join(dir, residentLockName)`). N
concurrent `aforge do` runs whose `--db` paths share a directory elect **one**
resident; the losers post `command_requested` + `spine_created` and then wait
forever for a resident that is only serving someone else's store.

Observed signature, twice, on 3-of-4 cells each time: 25-40 minutes of wall,
`nodes: 0`, `spend: 0.00`, zero `usage` rows, no `scale_gate`, and the single
progress line "still waiting: the task is being turned into work" repeated to
the wall. There is no diagnostic anywhere that names the lock. Cost of finding
it: two full grids.

Fixes, in order: (a) key the lock to the store *file*, not its directory; (b)
when a resident holds the lock but is serving a different store, say so on
stderr rather than waiting; (c) time-bound the "turned into work" wait and fail
loudly. `bench/run.sh` dodges this only because its `select` mode uses `-keep`
(private store per cell) — any user pointing two `do` runs at one store
directory hits it.

#### 14.4.4 P1 — the swe leaf hangs on larger tasks

Three cells burned their entire wall inside a *started* swe leaf: select #21 and
#22 (2400s each, node `task-2` still `running`, 1 usage row, 5 and 3 insertions
produced) and fresh-issue cli#2217 (1500s). swe-forced #22 ran 1066s and
returned `nodes_failed=1` with an empty diff. The recorded §4 grid completed #21
and #22 in 9m04s and 5m46s forced, so this is either a regression or high
variance; either way the shipping `do` path DNF'd on both large bambara issues
today.

#### 14.4.5 P2 — the practice loop bills itself to benchmark runs

Every bambara store contains a `practice-loop-2026-08-12` node that ran to
`done` inside the measured cell. It is the curiosity loop using idle time, but
it lands inside a metered run and inside the receipts. Benchmarks (and any
priced errand) should suppress it, or its spend should be reported separately.

### 14.5 Decomposition judgment

Every one of the twelve aforge cells across both batteries ran as
`route: single_leaf, leaves: 1`. No `job_growth`, no JIT expansion, no split
refusals, no concurrent nodes anywhere. One revision node (`task-2-x1`) on
select #23.

| task | chosen | optimal | grade |
|---|---|---|---|
| cobra#2257, cli#2217, cli#2324, afero#576 | 1 leaf, swe | 1 leaf | **correct** — single coherent changes, no separable width, nothing lost |
| bambara #21, #22 | 1 leaf, swe | 1 leaf | **correct shape**, execution failed |
| bambara #23 | 1 leaf, **linear** | linear | **correct** — and it beat pi on tests (320 vs 317) for $0.089 |
| bambara #20 | 1 leaf, **swe** | **linear** | **wrong** — adding one CI YAML file is what linear settles in 32s/$0.008 (recorded §4); swe charged $0.13 and 526s for it |

**There is no node-making-for-its-own-sake and no missed parallelism** — the
decomposition judgment is sound, which is the good news buried under the cost
numbers. The one open defect is the linear/swe boundary: §4 recorded "selection
chose swe 4/4"; today it is **3/4**, with #23 correctly routed to linear. The
boundary is moving in the right direction and is not yet there for #20.

### 14.6 Queue

- **W7**: resident-lock directory keying + a diagnostic for the wait (14.4.3).
- **W7**: delivery gate judges the *delta*, not the suite's absolute state
  (14.4.1). Highest value per unit effort in this list — it is currently
  throwing away correct patches.
- **W8**: `internal/swepro` context assembly — 7-8x pi's prompt tokens for the
  same patch is the whole cost and latency story (14.4.2).
- **W8**: swe leaf hang / watchdog on larger tasks (14.4.4).
- **W9**: push the linear/swe boundary far enough to catch #20-class work
  (14.5); suppress the practice loop inside metered runs (14.4.5).
- **bench**: add `--mode json` + cost parsing for pi to `bench/run.sh`, and
  re-record the §1/§4 pi rows — the recorded walls are 1.4-3.2x pessimistic and
  the #22 quality target no longer reproduces (14.2).

**Caveats.** One box, one model, one attempt per cell. The fresh issues are
likely in the model's training data. Bambara #20/#22 are still open upstream;
#21/#23 have merged fixes, so the pin matters and cross-table comparison against
§4 is weaker than within-table comparison. aforge ran in its shipping default
configuration; no knobs were swept.

### select-arm DNF forensics

The two DNFs in 14.2 (#21, #22 at the 2400s cap) are **not** new-machinery
overhead, not a deadlock, and not load. aforge's own side of both runs cost
**one model call, $0.00083, finished 4.4s in**: `usage` holds exactly one row
(`root`, 4757/803 tok), `scale_gate` = single leaf, `node_started` at
`16:17:00.27`, and every subsequent event to the cap is a `practice-loop`
heartbeat. No growth gate, no `plan.Satisfied`, no JIT expansion, no judge, no
repair round ever ran — the leaf never returned. Per-invocation cost of the new
machinery on these cells is zero calls and ~0.2s of wall.

The wall was eaten inside the swe leaf, on a path the forced arm never took.
The engine's `root-cut` event divides the grid exactly:

| cell | band | root-cut | outcome |
|---|---|---|---|
| select #20 | `m` | `selected` (one coder leaf) | 526s, done |
| select #21 | `l` | `decompose` | 2400s DNF |
| select #22 | `l` | `decompose` | 2400s DNF |
| swe-forced #21/#22 | — | `selected` | 1907s / 1066s, done |

`cutpolicy.ShouldRootCut` returns false once the band reaches `l`, so the leaf
re-planned the whole issue from scratch: #21 spent architecture 445s + planner
146s + issue-writer 309s = **921s of planning before one line of code**, emitted
7 tasks / 12 edges, and was still in scheduler cycle 2 at the cap (#22: 691s,
8 tasks, still in cycle 1). Both produced 3-5 insertions total.

The band is inflated by prompt duplication at our boundary, not by the issue.
`internal/exec/swe.go:774` (`sweGoal`) prepends `"This work is part of a larger
goal:\n"+task.Goal` — but on a `single_leaf` splice the head sets `Brief ==
Goal`, so the same 3.1KB (head expansion + "Verbatim request:" + 7 working
decisions) is sent **twice**, 6.2KB total. Fed to the real strings,
`sizeband.EstimateSizeBand` scores it:

| description | len | band |
|---|---|---|
| select arm as sent (goal + identical brief) | 6221 | **l** |
| same brief, preamble dropped | 3097 | m |
| forced arm (one-line title + raw issue) | 1486 | m |
| raw issue alone | 1378 | m |

Dropping the duplicate preamble is the whole difference between `l` and `m`,
i.e. between the 921s decompose path and the fast path that finished in 1066s.
This is also why the recorded §4 select rows were 30m/$1.23 and 36m/$1.29 while
forced was 9m/5m — the same band-`l` path, just inside the old cap. Today's cap
is 40m; #21 and #22 landed the wrong side of it. Marginal wall, structural cause.

Not fixed here (forensics scope, and `sweGoal` is the shared prompt path, not
the new growth/JIT code). The fix is one guard in `sweGoal`: skip the "larger
goal" paragraph when `task.Brief` already contains `task.Goal` — orientation
toward a goal the brief *is* buys nothing and costs a band. Note also that the
select arm's reported `$0.2226` is a large undercount: a leaf killed at the cap
never reports its usage, so two of four cells contributed only their root call.
