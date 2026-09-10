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
and D; **layer B is owed** — see §7.

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

### The ruling

**Parity: yes** — every outcome equal or better, on the cells that ran.
**Efficiency: yes** — median prompt tokens per turn 17,815 → 13,962, −21.6%.

Two honest qualifications. The prefix fell 15.8% and the per-turn prompt fell
21.6%, so the diet is doing slightly better on the wire than on the scale — the
dynamic fixes in DESIGN.md §5 showing up beside the page. And **the cached share
fell 40.6%**, which is the frontier-bill risk DESIGN.md §0 names: on this
open-weight pin it costs nothing, but it is the number to watch on the frontier
arm in §4 before anybody calls this settled.

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

- **Layer B has not been run on either side.** It is the layer that decides
  parity properly — `TestTUIE2E`'s fifteen subtests, `TestQuestionsE2E`'s
  sixteen, `TestStandingE2E`'s seven — and it is the only coverage the question
  and standing roads have at all. Budget about forty minutes a side plus a few
  cents, sequentially and never beside another suite:

  ```sh
  ssh spark 'export PATH=$HOME/.local/bin:$PATH; set -a; . ~/.config/fleet/secrets.env; set +a
    ~/bench-diet/rig/bench/prompt-diet/run.sh 6aa6a946e dev --layers b --reuse-worktree
    ~/bench-diet/rig/bench/prompt-diet/run.sh prompt-diet/integrate diet --layers b --reuse-worktree'
  ```

  Until it has run, the ruling in §1a is parity **on the cells that ran** and not
  parity on the wave.

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

- **An issue about the one-endpoint pin.** §1's model-pin note has the evidence:
  an endpoint excluded by account policy ends a turn instead of hopping, while
  the failover ladder handles other causes. It needs the lane that owns routing
  to say whether that pin is deliberate before it is filed as a defect.
