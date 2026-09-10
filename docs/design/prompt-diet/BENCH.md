# The prompt diet — proof

2026-09-10. The instrument, the baseline, and what it cannot yet settle.

[DESIGN.md](DESIGN.md) §6 ends on one line: **"Proof is the bench, not the byte
count."** This is that bench. The owner's acceptance for the wave is one
sentence and two claims — *"real e2e with dev and our branch, same other
changes, just this diff alone, and see if we are on parity but efficient"* — and
the two are decided separately because they can disagree.

| claim | what decides it | what sinks it |
| --- | --- | --- |
| **parity** | every subtest and every cell ends equal or better | one subtest that passed on `dev` and fails on the diet |
| **efficiency** | median prompt tokens per **turn** | a page that got shorter while the conversation got longer |

Efficiency is per TURN and not per run on purpose. A build that answers in three
rounds where the baseline took six has a smaller bill and a *bigger* per-turn
figure, and it is the per-turn figure that says whether the prefix got lighter.
`compare.py` prints the round count beside it so the other reading is never
hidden.

The scripts are `bench/prompt-diet/`; its README says what each file is. This
page says what was measured, and what the measurement is worth.

## 0. The instrument, in four layers

| layer | what it is | costs | proves |
| --- | --- | --- | --- |
| **A** prefix | `TestTheFixedPrefixStaysUnderItsBudget` with `-v`, its one logged line read back | nothing | the static bill, exactly |
| **B** suites | `TestTUIE2E`, `TestQuestionsE2E`, `TestStandingE2E` — the real binary, a real terminal, a real model | ~20 min, cents | **parity** |
| **C** cells | `bench/conversation` scenarios and `bench/e2e` cells, unchanged, handed this build's binary | minutes, cents | outcome and cost on ordinary work |
| **D** wire | a per-request ledger rolled up out of what B and C already wrote | nothing extra | **efficiency** |

**The rig is the checkout, the subject is only the binary.** `run.sh` builds
`<branch>` in a worktree of its own and points every bench script *from the
checkout it itself lives in* at that binary. This is not a convenience: `dev` at
`6aa6a946e` has never heard of `bench/prompt-diet`, so a run that used the
subject's own rig would have nothing at all to run on the baseline side, and a
rig that moved with the subject would be comparing two harnesses rather than two
prompts.

**Layer D stands up no new proxy, and none needed writing.** The wave brief
allowed for a sixty-line Go forwarder behind `AFORGE_BASE_URL`. Two records
already existed and between them they are better than one:

- `bench/conversation/lib/guard.py` is a loopback forwarder every conversation
  cell already runs in front of OpenRouter, handed to the harness as
  `AFORGE_BASE_URL` with a sentinel key while the real one stays in the guard's
  own process. It writes an `admitted` row carrying the request's SHAPE — bytes
  on the wire, message count, tool count — and a `settled` row carrying what the
  provider said it CHARGED: `prompt_tokens`, `completion_tokens`,
  `prompt_tokens_details.cached_tokens`, `completion_tokens_details.reasoning_tokens`,
  `cost_usd`. It is arm-neutral and it deliberately keeps no bodies.
- `internal/calllog` is aforge's own always-on record, pinned anywhere with
  `AFORGE_CALL_LOG`. Under `AFORGE_CALL_LOG_BODIES=1` it keeps the whole request
  body, which is the ONLY place a request's **tool-block bytes** can be counted
  rather than inferred.

`lib/wire.py` normalises both into one JSON Lines file and keeps `source` on
every row, so the two are never added together. The memory note saying a proxy
would have to be written was describing a session that never saved one.

## 1. The baseline — `dev` at `6aa6a946e`

Measured on the Spark, 2026-09-10.

### The static prefix (layer A)

| piece | bytes | ~tokens |
| --- | ---: | ---: |
| page | 23,391 | 5,848 |
| tool block | 24,044 | 6,011 |
| **total** | **47,435** | **11,858** |

Against the cap of 48,000 that is 565 bytes of headroom. The figures agree to
the byte with DESIGN.md §0's audit table, which is the first thing this bench
proves: the audit was measuring the same build the wave is about to change.

The diet's own targets, from DESIGN.md §6, are **≤ 40,000 for the full profile
and ≤ 10,000 for lean**, and the cap only ever ratchets down.

### The model pin is not the one this wave started with

The brief named `deepseek/deepseek-v4.1-flash`. That id is real — a bare curl to
it answered three times out of three, from GMICloud and DeepInfra — but pinned
to it, cells died mid-turn with the provider's own sentence:

> 0 endpoints out of 1 requested are available matching your guardrail
> restrictions and data policy … Paid model training violation (account
> settings): 1 endpoint excluded

Two things make that fatal to a comparison rather than merely annoying. This
account's privacy settings exclude at least one endpoint serving that model, and
**aforge asks for ONE endpoint per request and takes no fallback**, so drawing
the excluded lane is a dead turn rather than a hop. It killed `research-brief`,
which had passed twice that afternoon, and `code-fix`, on the same build within
minutes — and it lands on whichever side happens to draw it, which is exactly
the shape of noise a parity ruling cannot survive.

**The pin is `deepseek/deepseek-v4-flash-0731`**: the same family, the id every
historical row in `bench/conversation` and `bench/e2e` was measured on, and
endpoints this account allows. Nothing failed that way again.

That aforge turns an endpoint exclusion into a dead turn with no hop, while the
failover ladder exists and works for other causes, looks like a defect worth its
own issue. It is not this lane's to file blind: it needs the `--host`/lane owner
to say whether the one-endpoint pin is deliberate here.

## 1a. dev → diet, measured

`dev` at `6aa6a946e` against `prompt-diet/integrate` at `2a90be7e8` (its tip when
the run fetched it; the branch has moved since). Same rig, same pin, same cells,
back to back on the Spark so the provider's weather fell on both. Layers A, C
and D at first; layer B followed and is folded in below.

### The prefix

| piece | dev | diet | Δ | |
| --- | ---: | ---: | ---: | ---: |
| page | 23,391 | 17,761 | −5,630 | −24.1% |
| tool block | 24,044 | 22,171 | −1,873 | −7.8% |
| **total** | **47,435** | **39,932** | **−7,503** | **−15.8%** |

39,932 is already inside DESIGN.md §6's ≤ 40,000 target for the full profile.

### Outcomes

| cell | dev | diet | |
| --- | --- | --- | --- |
| `conversation/research-brief` | pass | pass | same |
| `conversation/code-fix` | pass | pass | same |
| `conversation/followup-while-working` | timeout | fail | same |
| `conversation/work-result-recalled` | timeout | **pass** | better |
| `e2e/lookup` | fail | fail | same |

`work-result-recalled` is the one that moved: it ran out its 480-second cap on
`dev` and passed in 78 seconds on the diet. `followup-while-working` stopped
timing out and started failing an assertion instead — faster and a fifth of the
tokens, but **not** an improvement, and the comparison says `same` for it on
purpose (`compare.py`'s `RANK`).

`e2e/lookup` fails on both sides for a reason that is neither branch's: its
quality checks all pass and its **shape** check cannot run, because the run left
no store to autopsy. That is a `bench/e2e` calibration gap in this environment,
identical on both sides, and it is owed in §7.

### Tokens per turn

Over the requests that carry the belt — see §1b, which is why that qualifier is
load-bearing.

| | dev | diet | Δ | |
| --- | ---: | ---: | ---: | ---: |
| turn requests | 69 | 19 | −50 | −72.5% |
| **median prompt tokens** | **17,815** | **13,962** | **−3,853** | **−21.6%** |
| median cached tokens | 16,212 | 9,627 | −6,585 | −40.6% |
| median completion tokens | 132 | 126 | −6 | −4.5% |
| median request bytes | 79,946 | 58,774 | −21,172 | −26.5% |
| median tools on the belt | 26 | 26 | 0 | — |
| median tool block bytes | 40,660 | 38,769 | −1,891 | −4.7% |
| asides (no belt) | 26 | 21 | −5 | −19.2% |
| median aside prompt tokens | 689 | 595 | −94 | −13.6% |

Recorded and not ruled on: total spend $0.0372 → $0.0085, summed cell wall 947s
→ 236s, median call 9,473ms → 8,144ms.

### Suite outcomes (layer B) — and this is where parity actually failed

44 subtests a side, run afterwards on the same two builds:

| suite | dev | diet |
| --- | --- | --- |
| `TestTUIE2E` | 15 pass | 14 pass · **1 fail** |
| `TestQuestionsE2E` | 16 pass · 1 fail | 15 pass · 2 fail |
| `TestStandingE2E` | 11 pass | 11 pass |

`TestQuestionsE2E/ASentenceWithHolesIsFilledIn` fails on **both** sides — a
pre-existing red on `dev`, not this wave's, and not a regression.

Two are regressions, and **neither was visible in layer C**:

- **`TestTUIE2E/space_in_the_task_room_pages_the_card`** — passed on `dev` in
  71.7s, failed on the diet in 249.3s. The trace says why and it is not the
  feature the subtest is named for: the task landed
  `your call · landed 1m ago · ran 2m 8s` where on `dev` it landed
  `done · landed moments ago · ran 10s`, so two 30-second waits ahead of the
  paging assertion blew before the thing under test was ever reached. Twelve
  times slower and escalating to `your call` where it used to finish is a
  behaviour difference; it may equally be the model taking a different road on a
  nondeterministic flash model.
- **`TestQuestionsE2E/TheOrdinaryRoadCarriesAQuestionAndItsAnswer`** — passed on
  `dev`, failed on the diet asserting `the screen never said " · you · "`. The
  captured screen says `· another window ·` in that position — and contains
  `· you ·` elsewhere in the same dump — while the road itself plainly worked:
  `They picked "delete it" (key 1). The build directory will be deleted.` This
  is a **person-facing string**, which makes it lane I's neighbourhood (the
  attribution row) rather than a byte-cut anywhere, and it may be a frame race
  rather than a respelling.

**Both are candidates, not verdicts.** One red on a suite that drives a real
model against a real provider is not yet a regression, and this bench's own rule
is #176's: reproduce it, do not rerun it in isolation and move on. Two more runs
a side of each are queued (`~/bench-diet-repeat.sh`, results at
`~/bench-diet-out/repeat-<name>-<side>-<n>.log`).

### The ruling

**Parity: NO.** Two subtests that pass on `dev` fail on the diet — both in layer
B, neither reachable by any cell in layer C. That is the whole reason layer B is
in this bench, and it is the reason a wave cannot be signed off on the cells
alone.

**Efficiency: yes.** Median prompt tokens per turn 17,815 → 13,962, −21.6%, with
every layer-C outcome equal or better.

Two honest qualifications. The prefix fell 15.8% and the per-turn prompt fell
21.6%, so the diet is doing slightly better on the wire than on the scale — the
dynamic fixes in DESIGN.md §5 showing up beside the page. And **the cached
tokens fell 40.6%**, which reads like the frontier-bill risk DESIGN.md §0 names.

**That reading is wrong and §1c is the autopsy**, kept here rather than corrected
in place because the row above is what the run measured. Read as a SHARE by
dividing these two medians it says the cached fraction fell from 91% to 69%;
asked per request and then taken at the median it ROSE, 83.0% to 95.2%, and the
prefix did not move on a single request of either capture. A ratio of medians is
not the median of a ratio, which is §1b's lesson arriving a second time by
another road.

## 1b. The number this bench nearly reported instead

The first comparison it ever ran announced median prompt tokens falling
**16,473 → 938, a 94% cut**. That was not a diet. It was a change of MIX.

A conversation does not only send turns. A title call, a memory reflex, a judge
and a router all go out on the same wire with a few hundred prompt tokens and no
tool block at all. The candidate made 19 turn calls where the baseline made 67 —
because it finished work the baseline timed out on — so its median landed among
the asides that both runs make in roughly equal number. Over the requests that
actually carry the prefix the same two runs read 17,815 → 13,962.

The corrected figure is a quarter of the size of the one the average told, and
the wrong one was the flattering one. `compare.py`'s `carries_the_belt` is the
fix — a request with at least one tool definition is a turn, and nothing else on
the wire carries a belt — and this section is here because the failure is not
obvious from a table and would be made again by the next person to average
something.

Two more measurement errors were caught the same way and are written into the
code that fixes them: a parent test counted beside its own children turned one
broken subtest into two failures, and a `wall_s` that travelled as a JSON string
was silently dropped by a numeric filter and printed as an em dash.

## 1c. Cache — does the prefix hold still, and did the diet move it

Two questions were open here and they are answered together, because the first
one's evidence is the second one's instrument.

§1a would not rule on the cached share and was right not to: a charge is an
outcome. And byte-stability is a different question from size — DESIGN.md §0
opens on it, because on a frontier model a prefix one byte different from the
last request's re-prices the whole conversation cold.

### The first measurement, and the question it left open

`lib/wire.py` fingerprints each request's system message and tool block out of
the bodies the call log already keeps. Over the **turn** calls of §1a's two runs:

| | dev | diet |
| --- | --- | --- |
| turn calls fingerprinted | 69 | 19 |
| distinct system prompts | 5 | 4 |
| distinct tool blocks | 3 | 2 |
| system bytes seen | 20,406 · 20,412 · 23,027 · 23,029 · 38,696 | 17,229 · 17,235 · 17,787 · 17,789 |
| tool-block bytes seen | 30,938 · 40,660 · 53,028 | 29,047 · 38,769 |

The sizes that differ by thousands are the honest ones — a shelf loaded, a
project instruction file folded in, a task's own page. **The pairs two and six
bytes apart look like a prefix that wobbles**, and `23,027` against `23,029`
covers 51 of dev's 69 turn calls. A prefix that moved by two bytes between
requests inside one conversation would be a cold re-price every time it moved
and would be invisible in every size column this bench prints, so it had to be
run down before anything else here was worth reading.

**It is not a wobble.** A fingerprint pooled over a whole RUN cannot answer a
question about one CONVERSATION, and that is the whole of it. Counted per
conversation, every cell on both branches sent exactly one system prompt, by
digest, for its entire life:

| conversation | dev: distinct system prompts | diet: distinct system prompts |
| --- | ---: | ---: |
| `47a6a473` | 1 (×5 calls) | 1 (×5) |
| `60c39ef4` | 1 (×20) | 1 (×4) |
| `b97e23ca` | 1 (×31) | 1 (×7) |
| `bc3edd99` | 1 (×3) | 1 (×3) |
| `44015c4b` task | 1 (×10) | — |

The 20 and the 31 that make up "51 of 69" are two different cells. The two bytes
between them are in the footer's own `- Working directory:` line —
`work-result-recalled-aforge` against `followup-while-working-aforge` — and the
six-byte pair is `code-fix-aforge` against `research-brief-aforge`. They are the
names of the bench's scratch directories, and they differ between conversations
exactly as they should.

That is worth writing down rather than deleting, because the shape of the error
is one this bench has now made twice: §1b averaged over a mix of request kinds,
and this pooled over a mix of conversations. **A statistic about a prefix has to
be taken inside the thing that has a prefix.**

### The instrument that settles it

`bench/prompt-diet/prefixdiff.py` asks the question directly rather than
inferring it from sizes. For each request it serialises `[tools][system]
[messages]`, groups requests into conversations by their opening human message,
and reports how many leading bytes each shared with the one before it, where the
sharing stopped, which block and field that offset lands in, sixty bytes of old
against new, and a one-word cause.

The serialisation order is the Anthropic assembly order and the conservative
reading of the other: whatever a server does with the tool block, a client that
keeps tools AND system AND the head of the transcript byte-stable is stable
under either. It sorts nothing — `canonical()` says why — because a schema whose
keys came out in a different order is one of the things it exists to convict.

```sh
bench/prompt-diet/prefixdiff.py ~/bench-diet-out/dev ~/bench-diet-out/diet
```

### What it found: nothing moved, on either side

Run against §1a's own two captures, unchanged.

| | dev | diet |
| --- | ---: | ---: |
| belt-carrying requests | 69 | 19 |
| conversations | 5 | 4 |
| openers (nothing to share with) | 5 | 4 |
| **append-only — the whole last request re-sent unchanged** | **64** | **15** |
| **requests that moved the prefix** | **0** | **0** |
| median stable share of the request | 98.6% | 98.3% |

Every non-opening request on both branches was an APPEND: everything the request
before it sent was still there, byte for byte, in the same order, at the head.
The worst single row in either run shares 76.3% of its bytes with the request
before it — a task node that appended a very large tool result, which is a big
append and not a small break.

Two mechanisms that could have shown up here and did not. **No history was
rewritten**: the stubbing pass (`stub.go`, `stubKeepTurns` = 4) replaces old tool
outputs in place, which re-bills everything after the rewrite point, and it fired
**zero times** across both captures — 0 of 69 dev requests and 0 of 19 diet
requests carry a `[tool:` stub line, and nothing broke at a `messages[i]` either.
**No tool block moved**: nothing was armed, re-armed or reordered mid-conversation
on either side, which is `armFamily`'s append-and-dedupe law holding in the wild.

### The number that said otherwise was a ratio of two medians

§1a reported median cached tokens `16,212 → 9,627` and read a share out of it by
dividing by the median prompt. That is a **ratio of medians standing in for the
median of a ratio**, and over these rows the two are not close, because the
request with the median prompt is not the request with the median cache. Asked
per request and then taken at the median — which is the only form of the question
a bill can answer — the cached share went the other way:

| median of the per-request cached share | dev | diet |
| --- | ---: | ---: |
| all belt-carrying requests | 83.0% | **95.2%** |
| openers | 64.8% | 58.8% |
| continuations | 83.9% | **95.6%** |
| openers as a fraction of the requests | 7% | 21% |

The absolute cached-token median fell for the reason the absolute prompt-token
median fell — there is less prompt to cache — and because the diet's
conversations finished in three to seven rounds where the baseline's ran to
twenty and thirty-one, so the deep requests that drag an absolute cache count
upwards are simply not there. Neither is a prefix that moved.

**The ruling on cache: the diet did not make it worse, and on the same cells it
made it better.** Nothing on either branch broke a cached prefix; the median
request's own cached share rose 83.0% → 95.2%.

### What the same instrument then convicted

Zero breaks in these captures is a true statement about these captures, and they
were made on fresh homes: no memories, no standing orders, no answered
questions, so three of the four blocks that ride in `message[0]` were empty
strings the whole time. The code says what happens when they are not, and one of
them was a real defect.

`refreshSystemLocked` (memory.go) built `message[0]` as
`system + places + standing + memory + record`. The first, third and fourth of
those move only when a person does something — a folder attached, an order
stood up, a question answered — which is the documented rule for a block that
sits in front of every message there is. **The `<memory>` block was not like
them**: it is re-routed against the person's own words at the start of every turn
(`refreshMemory`), and `renderMemoryBlock` re-stamps every line it keeps with an
age label whose granularity is hourly for anything learned today, so it moved on
turns where the router had chosen identically. A working session with memories on
therefore re-priced its whole conversation at the uncached rate — about five
times the cached one — on any turn the subject moved.

It rides at the tail now, in an appended note of its own
(`agent.go`'s `memoryNoteOpening`), beside the state card and the other windows'
work that an earlier wave moved there for the same reason. It is a second note
rather than a paragraph of the first because the two move on different beats: one
note would re-send up to `memoryBlockRunes` of memory every time a goal changed.
What it costs is a superseded memory left standing in the transcript where it was
said, which is why the note's opening says the last one holds.

The law is pinned rather than described.
`TestARoutedMemoryChangeLeavesMessageZeroByteIdentical` renders two consecutive
turns with a routed memory change and asserts `message[0]` is byte-identical; it
fails on the arrangement above with exactly the diagnosis this section gives.
`TestTheToolBlockMarshalsToTheSameBytesEveryTime` is the other half — every
schema in this program reaches the wire as a `map[string]any`, `encoding/json`
sorts map keys so it is stable today, and the test says that this is load-bearing
rather than incidental.

The fixed prefix is unchanged by the move: **38,742 bytes (prompt 18,807 + tools
19,935)** before and after. Nothing left the page or the belt; one block left the
front of the conversation.

### What was checked and found already correct

- **Arming appends and never reorders.** `armFamily` (connect.go) dedupes by
  name, keeps the existing order and appends the arrivals into fresh arrays;
  `armPrearmed` is one of its callers, so a lean belt's handed-over groups ride
  the same door. A load costs the tool block's tail once and nothing after that.
- **Nothing in the `# Project` footer moves on its own except the clock**, and it
  moves only past `clockRefresh` (10 minutes), which is longer than any of these
  providers keeps an untouched entry — so the re-render is free by construction.
  Inside the threshold `refreshClockLocked` returns without touching `a.system`
  at all. The footer quotes `AGENTS.md` and `CLAUDE.md`, so an edit to one of
  those does move the page — on the next clock refresh, which is to say at the
  moment the prefix was going cold anyway. The attached-folder block is the same
  shape: `publishAttached` compares the composed text and does not touch
  `message[0]` when a re-resolve produced the same bytes.
- **The caps and the profile settle once per agent.** `bare.CapsFor(a.window())`
  and the prompt profile are both read out of `Config` when the belt and the page
  are built, and the belt is rebuilt exactly twice in a conversation's life: at
  construction, and on `AnchorWorkspace` — a project-less conversation acquiring
  its project, once, on a deliberate act, which also re-renders the page.

### Where the breakpoints are, and what is owed there

There is one wire dialect in this build: the OpenAI-shaped `chat/completions`
body, with the system prompt as `messages[0]` and `tools` in the body
(`internal/provider/wire.go`). `cache_control` is written **only** for
Anthropic-family slugs on `openrouter.ai` or `anthropic.com`
(`caching.go`'s `dialectFor`); everything else — DeepSeek and the rest of what
OpenRouter fronts — is `cacheDialectAutomatic`, gets no markers at all, and its
only levers are byte stability and `prompt_cache_key`. `quirks.go`'s
`noCacheControl` is learned at runtime and never configured: an endpoint that
rejects a breakpoint downgrades that model to automatic for good, and the refused
call is re-sent free.

Three of Anthropic's four breakpoints are used: the last message of the leading
system run, the last tool definition, and the last user-or-tool message. After
the move above, the system breakpoint sits behind a `message[0]` that only a
deliberate act can move, which is where DESIGN.md §0 wants it.

**Owed: none of that is measured.** Every capture this bench holds is DeepSeek,
which sends no markers, so the placement is argued from the code and from the
provider's documentation and not from a bill. §4's frontier arm is where it gets
tested, and `prefixdiff.py` runs on a frontier capture unchanged — it reads
whichever dialect the body is in.

## 2. What is not covered, and why that is stated rather than fixed

The wave brief asked for cells exercising a conversation turn, a task handoff, a
question, a standing item and a media refusal. Three of those exist as cells and
two do not. Saying so is the point: a bench whose gaps are undocumented is a
bench that will be quoted as though it had none.

| shape | covered by | how |
| --- | --- | --- |
| a conversation turn | `bench/conversation/research-brief` | print door, `aforge chat --once`, facts that exist only in the fixture |
| a task handoff | `bench/e2e/bundle3`, `bench/conversation/code-fix` | three parts, three leaves and a sink; and a turn that becomes work |
| work coming back | `bench/conversation/work-result-recalled` | the interactive door — work handed off, then recalled |
| a person typing mid-work | `bench/conversation/followup-while-working` | the interactive door, tmux, a real screen |
| **a question asked of the person** | `TestQuestionsE2E` only (layer B) | **no bench cell exists** |
| **a standing item** | `TestStandingE2E` only (layer B) | **no bench cell exists** |
| **a media refusal** | **nothing** | no cell, no suite, nowhere |

**Both interactive cells were dead when this lane found them, and are not now.**
`bench/conversation`'s tmux door waits for `ARM_READY_RE` before it types
anything, and for aforge that needle was `· idle`. After the seven-panel home
landed, a fresh screen draws the state word at the right edge of the status row
with nothing in front of it — the failed cell's own saved scrollback ends in a
line reading `idle`, no separator — so the needle matched nothing, the door gave
up at the ready wait, and both interactive scenarios recorded `unsupported`
after 91 seconds. Two of the four cells in the first baseline proved nothing at
all, and said so in a word that reads like a limitation of the suite rather than
a broken calibration.

This is the rot #184 named, in this suite rather than the e2e one: a calibration
regex is a claim about a person-facing string, and a respelling ends the
measurement without ending the run. Both spellings are accepted now, so no
historical row changes meaning and a screen that goes back to the dot still
reads. With it fixed the same two cells run for real — and one of them is the
only outcome that moved between `dev` and the diet.

**There is no question cell and there was never going to be one by accident.**
Every arm of every battery under `bench/` runs unattended — `--yolo`, `pi -p`,
`opencode run` — so consent is bypassed by construction and there is no
assertion vocabulary for an agent→person `ask` at all. Adding one means a new
witness in `bench/conversation/lib/door_tmux.sh`. Until then the question road
is proved by `TestQuestionsE2E`'s sixteen subtests and by nothing else, which is
real proof but a different instrument, on a different model pin.

**There is no standing cell either.** The only mention of standing anywhere
under `bench/` is defensive: `bench/oneroad/marathon/cell.sh` *excludes* the
standing ticker's wake log from a fingerprint because it is a heartbeat and not
work. `TestStandingE2E`'s seven subtests are the whole coverage.

**A media refusal is covered by nothing at all**, and it is the gap that matters
most to this wave. DESIGN.md §1 says the 853-byte media-prompting essay "ships
unconditionally on a belt where every media verb is shelved", and lane E moves
it off the page. Nothing in the tree would notice if moving it changed what the
chat says when somebody asks for an image. The cell that ought to exist is a
`bench/conversation` scenario in the print door asking for a picture, asserting
that the reply either loads the capability or says plainly that it cannot — and
in particular that it does not invent a file path. It is not written; lane E
should say in its own report whether it wants it before the essay moves.

**Layer B runs on a different model from layer C.** `internal/e2e`'s `e2eModel`
is the constant `deepseek/deepseek-v4-flash`, not this bench's
`deepseek/deepseek-v4.1-flash`. Both sides of a comparison run the same
constant, so the comparison is fair; the two layers are simply not on one
model, and every table says which is which. Changing that constant is a change
to `internal/e2e`, which this lane does not own.

## 3. The recipes

Everything runs on the Spark. Go is at `~/.local/bin/go` and is not on the
non-interactive PATH; the key is in `~/.config/fleet/secrets.env`; `tmux`,
`sqlite3`, `timeout` and `python3` are all present.

The rig is a worktree of the branch carrying `bench/prompt-diet`:

```sh
ssh spark 'export PATH=$HOME/.local/bin:$PATH
  git -C ~/src/aforge-v2 fetch -q origin prompt-diet/h
  git -C ~/src/aforge-v2 worktree add -f --detach ~/bench-diet/rig origin/prompt-diet/h'
```

**The evidence and the build live outside every checkout** — `~/bench-diet-out/<label>`
and `~/bench-diet-build/<label>`, movable with `DIET_OUT_ROOT` and
`DIET_BUILD_ROOT`. That is a correctness rule and it was learned the expensive
way. The first baseline run put the evidence under `bench/prompt-diet/out/`, so
a cell's scratch workspace sat inside the rig's own git checkout; the `code-fix`
cell handed the model a two-file Go module to repair, and the model walked up
out of it, found the aforge repository around it, and ran `cd <rig> && go test
./...` — a full-tree build of aforge on a shared box, inside a cell whose wall
clock was supposed to be measuring a two-file fix. That run was killed and
thrown away. `run.sh` now refuses an `--out` inside a checkout.

Then, per label:

```sh
# free, seconds — the static bill only
ssh spark 'export PATH=$HOME/.local/bin:$PATH
  ~/bench-diet/rig/bench/prompt-diet/run.sh 6aa6a946e dev --layers a'

# the default sweep: prefix, cells, wire
ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a
  ~/bench-diet/rig/bench/prompt-diet/run.sh 6aa6a946e dev'

# everything, ~40 minutes
ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a
  ~/bench-diet/rig/bench/prompt-diet/run.sh prompt-diet/integrate diet --layers a,b,c,d'

ssh spark '~/bench-diet/rig/bench/prompt-diet/compare.py dev diet'
```

`compare.py` exits non-zero when an outcome got worse, so it is usable as a
gate. Use `run_in_background` for anything with layer B in it and poll the log;
the suite logs are far too wide to read through a pipe and `run.sh` redirects
each of them whole to its own file for that reason.

## 4. The frontier arm

The owner's requirement is that parity holds for normal systems too, not only
for the open-weight pin. The same cells run once more with the crew's default
frontier seat and are recorded **separately** — never averaged into the flash
rows, which would produce a number describing no run that ever happened:

```sh
ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a
  ~/bench-diet/rig/bench/prompt-diet/run.sh prompt-diet/integrate diet-frontier \
    --layers a,c,d --model <frontier id> --allowlist <frontier id>'
```

The frontier arm is where the **cached** column earns its place. DESIGN.md §0's
first bill is the frontier cached one, where a moved byte in `message[0]`
re-prices the whole conversation cold and stability matters more than size — so
a diet that shortened the page while making it less stable would show up here
as a collapsed cached share and nowhere else. `compare.py` prints median cached
tokens per turn beside median prompt tokens for exactly that reason.

Its cost is recorded in the run's own `summary.md` and belongs in the wave's
report. `bench/conversation` bills at its own guard, which is the same
measurement for every model, and never at an account-level credit delta on a
shared key.

## 4a. The lean cell — planned, and blocked on lane G

DESIGN.md §3's verdict on the lean profile is explicit: it "exists only if
`prefixbudget_test` weighs it and a local-model bench cell runs it". This is
that cell. Lane G has landed `internal/session/promptprofile.go`, so the switch
now exists: `AFORGE_PROMPT_PROFILE`, taking `full` or `lean`. The cell has not
been run — it is next after the two candidate regressions in §1a are settled,
because a lean profile measured against an unsettled baseline proves nothing.

```sh
ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a
  AFORGE_PROMPT_PROFILE=lean CONV_PASS_ENV=AFORGE_PROMPT_PROFILE \
    ~/bench-diet/rig/bench/prompt-diet/run.sh prompt-diet/integrate diet-lean \
      --layers a,c,d'
ssh spark '~/bench-diet/rig/bench/prompt-diet/compare.py diet diet-lean --out-root ~/bench-diet-out'
```

`CONV_PASS_ENV` is not optional: `bench/conversation` hands a harness only the
variables it is told to carry, so a profile set in the caller's shell and not
named there reaches nothing and the cell silently measures the full profile
instead — which would read as a lean profile that saved nothing.

**It is recorded separately and never averaged with the full-profile rows.** The
two are different products with different belts, and one number over both would
describe no run that ever happened. The comparison to make is `diet` against
`diet-lean` on the same branch and the same cells, not `dev` against
`diet-lean`: the question the lean profile has to answer is whether it keeps
parity while paying Pi-sized, and that is a claim about the profile, not about
the diet.

The open-weight pin is already this bench's default
(`deepseek/deepseek-v4-flash-0731`), so no `--model` is needed; a genuinely
small local model is the harder follow-up and needs a window that
`ContextWindowFor` actually reads as small.

## 5. The ablation — prepared, not run

DESIGN.md §3 calls ablation "the arbiter", and it is the only instrument that
can tell a law that helps from a law somebody believed helped. Exactly one
paragraph on the page has ever earned its place that way: the working
discipline, on twelve unattended runs, 3/3 against 0/5. Every other law is
there on judgement.

`bench/prompt-diet/ablate.sh <branch> <unit-id>` renders the page with one unit
removed and runs the cell set without it. `bench/prompt-diet/units.tsv` is the
list — thirteen units, largest first, each with a key sentence verified verbatim
against `internal/session/prompts/system.md`, plus where it lives and whether a
substring test pins it.

Two roads, and the script says in its own output which one it took:

- **the env hook**, `AFORGE_PROMPT_ABLATE=<id>`, which lane C or lane G may add
  beside the law registry. This is the honest road: the unit is removed by the
  same code that renders it, so no neighbouring byte moves.
- **the patch**, which cuts the unit's whole blank-line-delimited block out of a
  throwaway worktree's `system.md` and rebuilds. Cruder, and it can only reach
  units that live on that page.

**Two of the ten largest units cannot be ablated by the patch road at all.**
Standing (2.5 KB) and accounts (1.1 KB) are rendered from `beltfacts.go` — Go
strings chosen by a predicate — and there is no honest way to cut one out of a
Markdown file. `ablate.sh` refuses those rather than removing nothing and
reporting a null effect, which would read exactly like a law that turned out not
to matter. **This is the one thing this lane needs from another: lane C or lane
G adding `AFORGE_PROMPT_ABLATE` to the law registry unlocks them.**

What the ablation could and could not settle, before anybody spends on it:

- **It cannot settle a pinned law.** `one-breath` and the working discipline are
  held in place by substring tests, and DESIGN.md §6 and the wave brief both say
  a pinned law does not move on bytes — or on one run's evidence. Ablating one is
  still worth doing (knowing what a law is worth is useful even when it is
  staying) but the result is a note here, never a deletion.
- **It cannot separate a real effect from the model's variance in one run.** The
  cell set is four scenarios and two task cells; the discipline paragraph needed
  twelve runs to say 3/3 against 0/5. Treat a moved outcome as a reason to run
  that unit again with more seeds.
- **What it can do** is the cheap half: a unit whose removal moves nothing over
  the whole cell set is a unit with no evidence for its place on the page, which
  is precisely the case for refiling it as ON DEMAND. That is the diet's thesis
  and this is the instrument for it.

Budget, at the baseline's measured cost: one unit is one cell-set run, and
eleven runnable units plus a control is twelve of them.

## 6. What this bench does not rule on

Wall clock and dollars are **recorded, never ruled on**. `bench/e2e/README.md`
sets the thresholds this follows and the reason: two runs of *identical code*
came in at 15s and 24s, a 60% swing, because wall clock carries the provider's
queue as well as the work. A ruling on wall would report a regression every
other run, and a battery that fails on a healthy run stops being read. Dollars
move with the cached share, which moves with how the provider felt about the
prefix that minute.

An outcome that is `incomplete` on either side is never called equal. A run cut
off by its own timeout, or by an ssh pipe going away, measured nothing about the
tests it never reached; `compare.py` reports those as **not proven** and neither
as a pass nor as a regression.

## 7. What is owed

- **The two candidate regressions in §1a need their repeats read.** Queued on
  the Spark as `~/bench-diet-repeat.sh`, two runs a side of each, logs at
  `~/bench-diet-out/repeat-{taskroom,askroad}-{dev,diet}-{1,2}.log`. Two greens
  a side clears one; a second red confirms it. Nothing merges past a confirmed
  one.

- **The diet side was measured at `2a90be7e8`,** which is behind the integration
  branch — deliberately, so that layers A, C and D and layer B are all one
  revision pair. A run on the current tip is labelled `diet-tip` and queued
  behind layer B (`~/bench-diet-tip.sh`); compare it with
  `compare.py diet diet-tip` to see what the later lanes moved.

- **The frontier arm (§4) has not been run.** The cached share fell 40.6% on the
  open-weight pin, and that is precisely the number DESIGN.md §0's first bill is
  made of. It costs nothing on flash and everything on a frontier model.

- **`bench/e2e`'s shape assertions do not work in this rig.** `lookup` passes
  every quality check and fails `no journal — the run left no store to autopsy`
  on both sides: `aforge do --keep` did not leave the `store kept at …` line the
  autopsy greps for. Both sides fail identically so no comparison is harmed, but
  the cell's real value — route, node count, edges — is unavailable, and
  `bundle3`, the handoff-shape cell, is worth nothing without it. `bundle3` is
  also 32 minutes a side, so it is off the default `--cells` and reachable with
  the flag.

- **A media-refusal cell.** §2 says why it matters more to this wave than the
  other two gaps: lane E moves the media essay off the page and nothing in the
  tree would notice if that changed what the chat says when somebody asks for a
  picture. The shape is a `bench/conversation` print-door scenario asking for an
  image, asserting the reply either loads the capability or says plainly it
  cannot — and in particular does not invent a file path.

- **A question cell and a standing cell**, which need a witness that does not
  exist: every arm of every battery under `bench/` runs unattended, so there is
  no assertion vocabulary for an agent→person `ask`.

- **`AFORGE_PROMPT_ABLATE`**, from lane C or lane G. Without it, two of the ten
  largest law units — standing and accounts — cannot be ablated at all (§5).

- **The lean cell in §4a has not been run.** `AFORGE_PROMPT_PROFILE` exists now
  that lane G has landed, so nothing blocks it but time — and the order matters:
  settle §1a's two candidate regressions first, because a lean profile measured
  against an unsettled baseline proves nothing. DESIGN.md §3 says the profile
  should not ship without this cell.

- **The raw wire evidence** is on the Spark and needs no re-capture. This is
  what §1c was run against and what a re-run of `prefixdiff.py` reads.
  Per label — `dev` and `diet`:

  | what | path |
  | --- | --- |
  | normalised, one row per request | `~/bench-diet-out/<label>/wire.jsonl` |
  | whole request bodies | `~/bench-diet-out/<label>/cells/conversation.calllog.jsonl` |
  | the guard's own token and cost ledger | `~/bench-diet-out/<label>/cells/conversation/<scenario>-aforge/guard-usage.jsonl` |

  The call logs are 14 MB (dev, 190 rows) and 2.4 MB (diet, 80 rows), and EVERY
  row carries its whole `request_body` — system, tools and messages — because
  the cells run under `AFORGE_CALL_LOG_BODIES=1`. `wire.jsonl` deliberately does
  not copy those bytes; it carries the fingerprints computed from them
  (`system_bytes`, `system_sha`, `tool_block_bytes`, `tools_sha`, `prefix_sha`)
  and a `body_source` naming the log beside it. **The thread has been pulled**:
  the two-byte pair across 51 of dev's 69 turn calls is two different cells whose
  scratch directories are named two characters apart, not one conversation whose
  prefix moved, and §1c has the per-conversation digests that settle it. What is
  still owed there is a FRONTIER capture: every body here is DeepSeek, which
  sends no `cache_control` at all.

- **An issue about the one-endpoint pin.** §1's model-pin note has the evidence:
  an endpoint excluded by account policy ends a turn instead of hopping, while
  the failover ladder handles other causes. It needs the lane that owns routing
  to say whether that pin is deliberate before it is filed as a defect.
