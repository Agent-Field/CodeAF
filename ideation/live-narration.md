# The caption — one live line over the work, and the outline it leaves behind

Status: proposal, nothing built. Grounded in `internal/tui3/{hierarchy,workfold,
toolview,reveal}.go` and `internal/session/{loop,auxiliary,title}.go` as they
stand.

## The one-sentence version

A turn's machinery is already collapsed twice; what it lacks is a **sentence at
the top of each collapse saying what is being found out and why**. That sentence
does not have to be predicted, guessed, or written by a second model: the big
model already emits it, this surface already classifies it as narration, and it
already arrives *before* the tool calls it describes. We are throwing it away
into a muted paragraph instead of promoting it into the one live line the person
reads.

## What exists today (so we argue from the same facts)

**Five fold mechanisms, none of which is a sentence.**

| Layer | Head | What it says |
| --- | --- | --- |
| Tool cluster (`toolview.go` `clusterRows`) | `↳ 6 earlier tool calls · ctrl+o` | a count |
| Work chip (`workfold.go` `workfoldLabel`) | `▸ worked 12s · thought 6s · 10 tool calls · ctrl+e` | counts and clocks |
| Thinking (`thinking.go`) | `thought for 6s · 148 tok · ctrl+e` | a count |
| Tool row inline (`entry.open`) | `read internal/tui3/render.go  398 lines` | the literal call |
| Brief fold (`brieffold.go`) | task instruction | — |

Every head on this surface is **counted facts**. That is a deliberate and good
law — nothing paraphrases, nothing can lie. It is also why a person watching a
90-second `bash go test` has a spinner and a clock and no idea whether the thing
is on the right track.

**The narration/answer split is already built.** `hierarchy.go` states it as a
law and needs no cooperation from the model:

> PROSE FOLLOWED BY MORE WORK IN THE SAME TURN WAS NEVER THE ANSWER. It was
> narration — the surface saying what it was about to do — and the proof arrives
> the moment the next tool call opens under it.

That prose is stamped `demoted`, dropped into the work column at `hueNarr`,
drawn plain, and then **hidden entirely** when the work chip closes over it. We
compute exactly the thing this feature needs and then bury it.

**Deterministic glosses are good and free.** `toolstat.go`'s `targetField` +
`toolWords` already turn a call into `read internal/tui3/render.go` with no
model in the loop.

**A cheap-model ladder already exists.** `internal/roles` has five tiers
(`reflex`, `low`, `worker`, `high`, `mastermind`) and `Agent.callRole`
(`auxiliary.go:72`) is a one-shot non-streaming door that already books cost
through `addAuxiliaryUsage` and keeps off the phase clock. `title.go`,
`taskname.go`, `route_judge.go`, `guardian.go` all ride it. Adding a narrator
role is a day's plumbing, not a project.

**Two laws constrain the drawing.** `toolview.go:87` — *nothing else animates on
this surface* except the running tool spinner (forming uses a pulse). And
`reveal.go` — every unread remainder is walked onto the page on one clock, and
the screen-reader tier paces nothing.

## The insight the whole design turns on

**Text tokens precede tool-call tokens in the same completion.** When the model
decides to read four files, it writes any prose first and the tool calls after,
in one stream, over one connection, in one billed request. So the question "can
we know what it is about to do?" has an answer that needs no prediction: *ask
for the sentence, and the ordering delivers it early for free.*

Which inverts the cost intuition in the prompt that started this:

> **The big model is the cheap narrator. The small model is the expensive one.**

| | added output tok | added input tok | added round trips | added latency |
| --- | --- | --- | --- | --- |
| Big model writes one line per batch | ~12 | 0 | **0** | **0** (arrives before calls that must be waited for anyway) |
| Reflex model narrates a batch | ~15 | 300–800 (must re-send context) | 1 | 300–900 ms, and it is *behind* reality |

At four batches a turn, tier A is roughly **$0.00004 of output** on a turn that
costs cents. It is not a rounding error on the bill; it is a rounding error on
the rounding error. The small model costs more per line than the big one,
because input tokens dominate and the small model needs context the big one
already has.

That does not make the small model useless — it makes it the wrong tool for the
common case and the *right* tool for two specific ones, below.

## The shape

### Three densities, one object

The caption is the head of a fold. Same line, three states.

**Live, collapsed — the default.** One line. It shimmers while its step runs.

```
  ▸ read how a fold decides to collapse                        6 calls
  ▸ found where the chip is minted                             2 calls
  ▾ rewriting clusterRows so the head is a sentence                 3s
```

The finished captions above go still and dim; only the last one moves. This is
already a live outline — the person can read the shape of the work at a glance
and knows, mid-turn, whether the thing understood the question.

**Live, expanded — `ctrl+o`, or click, or `enter` on the row.** The tool rows
this surface draws today, unchanged, underneath their sentence:

```
  ▾ rewriting clusterRows so the head is a sentence                 3s
      edit internal/tui3/toolview.go                           +14 −3
      bash go test ./internal/tui3                               ⠋ 3s
```

**Settled — the work chip, as today, plus one number.**

```
▸ worked 24s · thought 6s · 3 steps · 11 tool calls · ctrl+e
```

And `ctrl+e` opens it to **the outline, not the machinery**:

```
▾ worked 24s · thought 6s · 3 steps · 11 tool calls · ctrl+e
    read how a fold decides to collapse                        6 calls
    found where the chip is minted                             2 calls
    rewrote clusterRows and ran the suite                      3 calls
```

The strategic payoff is that **the live experience and the archaeology are the
same object at different densities**. The person watching and the person
scrolling back a week later read the same three lines. Nothing is authored
twice, and there is no separate "summary" that can disagree with the transcript.

The caption also **deletes** the `↳ N earlier tool calls · ctrl+o` fold line: a
sentence with a call count on its right is a strictly better fold head than a
call count alone. Net new machinery is smaller than it looks.

### Where the sentence comes from — a ladder, best first

A capability that cannot work is absent, not broken. So the caption must have a
floor that is always available and never blank.

**Tier A — the model's own line (default, ~95% of steps).** Take
`firstLine(demoted prose)` and promote it into the caption slot; the remainder
stays as body under the expanded caption. **No new syntax, no sigil, no
parsing** — the trigger is the existing `demoted` test, which is already "prose
followed by more work in the same turn." A model that writes nothing before its
tools simply falls through to tier B; a model that writes three paragraphs gets
its first line as the head and the rest as body. There is nothing to leak into
the answer because a block that is *not* followed by work is never a caption, by
construction.

The prompt cost is one paragraph in `internal/session/prompts/system.md`, which
today says the opposite (*"don't narrate obvious steps"*) and would need to say:
before a batch of tool calls, one short present-tense line naming what you are
trying to find out — not the commands you will run.

**Tier B — the deterministic composite (floor, free, instant).** From the
announced batch alone, before anything executes. `EventToolAnnounced` fires for
every call in the batch, so at the moment the first one begins we already know
all N — the foreknowledge is *free and exact* for the shape of the step, just
not for its purpose.

```
4× read                        → reading 4 files
grep, then read                → searching the tree
edit ×3, bash go test          → editing 3 files and running the suite
```

This is `toolWords`/`targetField` generalised from one call to a batch. It is
never wrong, never late, and never interesting — which is precisely what a floor
should be.

**Tier C — the reflex model, on dwell only.** Not per step. A narrator that is
one beat behind reality is worse than no narrator, and a cheap model call
(300–900 ms) loses a race against a 200 ms `read`. So it fires on **silence**,
which is the one condition where it cannot lag and the one place the person is
actually staring at nothing:

- **A step exceeds ~4 s with no new text.** One `callRole(RoleNarrator, …)` on
  `TierReflex` writes a line about what is happening and why. That is 1–3 calls
  on a long turn and zero on a fast one, and it lands exactly where a spinner
  and a clock are the whole of the UI today.
- **The turn settles.** One call converts the live present-tense captions into
  the past-tense outline the chip opens to, `title.go`'s exact shape, fully off
  the critical path.

Both book through `addAuxiliaryUsage` (session total honest, turn meter
unchanged — the existing law) and ride `lane.RoleAuxiliary` so they do not steal
the phase clock.

### The shimmer, and how it stays lawful

*Nothing else animates on this surface* is a real law and this feature must not
simply break it. The honest framing is that the caption does not **add**
animation, it **moves** it:

> **THE SHIMMER IS THE SPINNER, RELOCATED.** When a step is collapsed its tool
> rows are not on screen, so their spinners are not either. The caption inherits
> that budget. Expanding a caption stops its shimmer and hands the animation
> back to the rows. Exactly one thing moves, in exactly one place, at any moment.

And the motion carries meaning rather than decorating: **shimmering is present
tense; still is past tense.** A caption that stops moving has stopped being
true-right-now, which is a fact the person needs and which no glyph currently
states.

Mechanically: a one-lightness-step sweep travelling left→right across the
caption's cells, driven off the existing `a.paints` frame counter at roughly the
`pulseStep` cadence (~1.2 s period), so it cooperates with the 33 ms frame clock
rather than opening a second one. Frozen flat in the linear/screen-reader tier,
following `formingInk`'s precedent in `reveal.go:104`.

### The writing spec — where the quality actually lives

This is the part that decides whether the feature reads as a senior engineer
thinking out loud or as chatbot filler, and it is worth more care than the
rendering. The caption is **the question being answered, not the action being
taken**.

| Bad | Why | Good |
| --- | --- | --- |
| `Let me explore the codebase!` | pleasantry, first person, no content | `working out where the fold is minted` |
| `Reading files` | restates tier B for free | *(no caption — let tier B draw it)* |
| `Running grep -rn 'clusterRows'` | the row below already says this | `checking who else calls clusterRows` |
| `Analyzing the results` | says nothing | `seeing whether the chip owns the indent` |
| `I'll now update the renderer` | future tense, first person | `rewriting clusterRows` |

Rules: present participle, lowercase, no first person, ≤ 60 cells, states the
uncertainty being resolved. And the banned-vocabulary law applies unchanged.

**No caption for a single-call step.** `read internal/tui3/render.go` is already
a perfect one-liner and a sentence over it is chrome for nothing. A caption
forms when a batch has ≥ 2 calls, or when a single call passes the dwell
threshold. This is the emptiness law applied to prose.

## Failure modes, and what absorbs them

| Risk | What holds |
| --- | --- |
| The caption lies (says "reading tests", greps something else) | It is a *head over what happened*, never a replacement. The rows beneath are the truth, the call count on the right is the honest counter, and no tool row is ever deleted. |
| Caption spam — one per call | Tier A is prompted for batch level; a floor rule refuses a new caption that covers zero calls, and merges consecutive captions with no work between them. |
| The model steals the answer into a caption | Impossible by construction: a caption exists only where `demoted` is already true, i.e. work follows in the same turn. |
| Rewriting captions at settle feels like the record changing | Real risk. Either keep present tense in the archive (free, honest, slightly odd) or make the past-tense pass opt-in. Flagged as an open decision. |
| Screen reader | Caption emitted once as plain text, no shimmer, no live rewriting. Same tier discipline as `reveal.go`. |
| Cost dishonesty | Tier A tokens are ordinary turn tokens and already counted. Tier C goes through `addAuxiliaryUsage` like the title and the memory reflex. |

## What it touches

- `internal/tui3/caption.go` (new) — the type, derivation from `entry` runs, the
  ladder, the shimmer.
- `internal/tui3/hierarchy.go` — promote `firstLine` of a demoted block; the
  remainder stays demoted body.
- `internal/tui3/toolview.go` — `clusterRows` fold head becomes the caption;
  `foldWord` retires.
- `internal/tui3/workfold.go` — chip label gains `N steps`; open state renders
  the outline before the machinery.
- `internal/tui3/input.go` — `ctrl+o` semantics, `enter` on a caption row, a new
  `hitCaption`.
- `internal/session/prompts/system.md` — the one paragraph, replacing the line
  that currently forbids it.
- `internal/session/narrator.go` (new, tier C only) + a `RoleNarrator` on
  `TierReflex` in `internal/roles`.

**Gates it must clear.** `internal/manual/chat/` in the same change (the manual
law — new key behaviour and a changed default). `internal/e2e/tuiwords_test.go`
for every new person-facing string. `docs/changes/unreleased/` entry saying the
fold head stopped being a count. And `go test ./internal/tui3/ -timeout 15m`.

## The smallest thing that ships

Tier A and tier B only — no second model at all. That is: promote the first line
of demoted prose, add the batch composite as the floor, make the fold head the
caption, add the shimmer, teach the chip to open to the outline. **It is the
whole feature the person actually sees, it costs one paragraph of prompt and
~$0.00004 a turn, and it can land before a single line of narrator plumbing
exists.** Tier C is a second wave whose value is confined to long silences and
the past-tense archive, and it can be judged on its own once the surface is
already better.

## Decisions — settled 2026-09-03

The build is [`docs/design/caption/PLAN.md`](../docs/design/caption/PLAN.md).

1. **Name:** `caption`; the collapsed stack is **the outline**.
2. **Present tense is kept in the archive.** A caption keeps the words it was
   born with. This deletes half of tier C — the settle-time rewrite is gone,
   leaving the dwell narrator, which was always the stronger half. The law it
   costs: THE RECORD DOES NOT CHANGE UNDER THE READER.
3. **`ctrl+e` on the chip opens the outline**, machinery one expand further.
4. **All three tiers**, in two waves: A+B+D (no second model at all) first, the
   dwell narrator second and judged on its own.

Still open, and cheap to answer during the build:

- **Does the caption survive into a task room?** `A ROOM FOLDS NOTHING`
  (`workfold.go`) but a room still demotes its narration — so captions probably
  draw there without folding, same as the hierarchy.
