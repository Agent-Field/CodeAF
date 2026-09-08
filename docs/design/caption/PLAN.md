# The caption — implementation plan

Status: plan, nothing built. The UX argument and the alternatives it beat are in
[`ideation/live-narration.md`](../../../ideation/live-narration.md); this file is
the build.

Settled by the owner on 2026-09-03:

- **Name:** `caption`. The collapsed stack of them is **the outline**.
- **All three tiers**, in two waves.
- **`ctrl+e` on the chip opens the outline**, machinery one expand further.
- **Present tense is kept in the archive.** A caption keeps the words it was
  born with.

That last one is load-bearing and it **deletes half of tier C**. The settle-time
rewrite (live present tense → archived past tense) was tier C's second job; a
record that changes under the reader is worse than a slightly odd tense, so it
goes. Tier C is now exactly one thing: **the dwell narrator**, which was always
the stronger half.

---

## 0. The four things being built

| | What | Where | Needs a model? |
| --- | --- | --- | --- |
| **A** | The model's own line, promoted out of demoted prose | `tui3` + one prompt paragraph | the big one, already running |
| **B** | The batch composite, the floor that is never blank | `tui3`, pure function | no |
| **C** | The dwell narrator, on silence only | `session` + `roles` | `RoleCaption`, low tier |
| **D** | The drawing: fold head, shimmer, outline, keys | `tui3` | no |

A + B + D is the whole visible feature and is **wave 1**. C is **wave 2** and can
be judged on its own once the surface is already better.

---

## 1. The type, and where it is derived

New file `internal/tui3/caption.go`. The caption is **derived at render time from
the entry list**, exactly as `workfold` is (`workfold.go` `deriveWorkfolds`) and
for the same reason: it is a fact about a *run* of entries, not a thing any one
entry owns, and journalling it would be a second source of truth for something
the list already states.

```go
// caption is the sentence at the head of one STEP of a turn's work.
//
// A step is a maximal run of entries holding at least one tool call, headed by
// the assistant prose that preceded it. THE STEP IS THE UNIT BECAUSE THE MODEL
// DECIDES IN STEPS: it writes, it calls a batch, it reads the results, it writes
// again. That rhythm is already in the entry list and this type only names it.
type caption struct {
	text   string
	source captionSource
	// start is the first entry of the step; head is the assistant block whose
	// first line was lifted, or -1 when the words are not the model's own.
	start, head, end int
	calls            int
	began, ended     time.Time
}

// captionSource is WHICH RUNG OF THE LADDER SPOKE, and it is kept because the
// three are not equally trustworthy and one of them is worth drawing
// differently if we ever decide it is.
type captionSource uint8

const (
	captionMade captionSource = iota // tier B: composed here from the batch
	captionSaid                      // tier A: the model's own first line
	captionTold                      // tier C: the dwell narrator
)
```

Derivation mirrors `deriveWorkfolds`:

```go
func deriveCaptions(es []entry, runningTurn int) map[int]caption
```

keyed by `start`, so a renderer holding an index can ask one map lookup.

`deck` (`render.go`) gains two fields beside `unfolded` and `workOpen`:

```go
captions map[int]caption
capOpen  map[int]bool
```

### The rules of derivation, each one a refusal

1. **A step with fewer than two calls gets no caption**, unless it has passed
   the dwell threshold. `read internal/tui3/render.go` is already a perfect
   one-liner and a sentence over it is chrome for nothing. This is the emptiness
   law applied to prose.
2. **A caption exists only where `demoted` is already true** — prose followed by
   more work in the same turn. This is `hierarchy.go`'s existing test, reused
   rather than restated, and it is what makes it *structurally impossible* for
   the answer's first line to be stolen into a caption.
3. **Consecutive captions with no calls between them merge.** A model that
   writes three paragraphs before one batch produced three heads and one step.
4. **The live frontier is a caption like any other**, but it is the only one
   that moves. `ended.IsZero()` is the whole test.

---

## 2. Tier A — promoting the model's own line

`hierarchy.go` already stamps `demoted` in one pass over the deck. The lift is a
second stamp in the same walk, so nothing is derived twice:

```go
// entry gains two fields (app.go, beside demoted)
capHead bool // this block's first line was lifted into a caption
capCut  int  // byte offset in text where the body under the caption begins
```

`stampCaptions` follows `stampHierarchy` and uses its identical stale
discipline — *a block whose tier changed is holding rows it drew in the other
tier* — so a block that gains or loses its head is invalidated and every other
block on the frame is untouched.

`workingProse` then draws `e.text[e.capCut:]` instead of `e.text`. **A block
whose whole text was the caption draws no body at all**, which is the common and
desired case: one line in, one line out, no orphan paragraph under it.

### The prompt

`internal/session/prompts/system.md`, a new section after `# Planning` (which
already mandates the visible plan note and already holds calls past a dozen
tool replies with no visible text — the caption is the same instinct at batch
scale, and the two must read as one idea rather than two):

```markdown
# The line before the work
Before a batch of tool calls, write ONE short line saying what you are trying to
find out or settle — not the commands you will run. Present tense, lowercase, no
first person, under 60 characters. It is the heading the person reads while the
batch runs; the calls list themselves underneath it.
- Good: `working out where the fold is minted`, `checking who else calls clusterRows`
- Bad: `Let me explore the codebase!`, `Reading files`, `Running grep -rn 'foo'`
One line per BATCH, never per call. A single obvious call needs no line.
```

And `# Tone`'s third bullet needs the exception spelled, or the two sections
contradict each other:

```diff
-- Technical reader; don't narrate obvious steps or explain basics.
+- Technical reader; don't narrate obvious steps or explain basics — the one
+  line before a batch of tool calls is the exception, and it is not narration
+  of the steps but of the question (see The line before the work).
```

**Cost:** ~12 output tokens per batch, zero extra input, zero round trips, zero
added latency. It rides a request that was happening anyway and it arrives
*before* the calls it describes because text tokens precede tool-call tokens in
the same completion. That ordering is the entire reason this feature does not
need to predict anything.

---

## 3. Tier B — the composite, and why it is the floor

Pure function, no model, no I/O, in `caption.go`. It runs on the **announced
batch**, which is available at `EventToolAnnounced` — every call of a batch is
announced before the first one begins, so the shape of the step is known exactly
and for free at the moment it opens.

```go
// captionMade composes the floor sentence from a batch of calls.
//
// IT IS NEVER INTERESTING AND IT IS NEVER WRONG, which is what a floor is for.
// A capability that cannot work is absent, not broken — so the caption slot may
// never be blank and may never say "thinking", and this is what stands in it
// when the model said nothing.
func composeCaption(es []entry, from, to int) string
```

The rules, cheapest signal first:

| Batch | Sentence |
| --- | --- |
| `read ×4` | `reading 4 files` |
| `read ×4`, all under one dir | `reading 4 files in internal/tui3` |
| `grep`, then `read` | `searching the tree` |
| `edit ×3`, `bash go test ./…` | `editing 3 files and running the suite` |
| four or more kinds | `<dominant kind> and 6 more calls` |

The common-prefix case is worth the twenty lines it costs: it is the one place
where a free, deterministic sentence is genuinely more informative than the
count it replaces. Verbs come from a table beside `targetField`
(`toolstat.go`) — `read`→reading, `grep`/`find`→searching, `edit`/`write`→editing,
`bash`→running, `web_fetch`/`web_search`→looking up, `ls`→listing — and an
unknown tool falls back to its own name, which is what `toolWords` already does.

---

## 4. Tier C — the dwell narrator (wave 2)

**It fires on silence, not on events.** A narrator one beat behind reality is
worse than none, and a 300–900 ms model call loses a race against a 200 ms
`read`. Silence is the one condition where it cannot lag, and it is also the one
place the current UI is a spinner and a clock and nothing else.

### The role

`internal/roles/roles.go`, beside `RoleTaskName` and registered from the file
that owns the call:

```go
// RoleCaption writes the line over work that has gone quiet: a batch that has
// run past captionDwell with nothing new on screen, and a person watching a
// spinner with no way to tell a wedged call from a slow one. LOW, for the
// title's reason — a wrong caption costs a glance, the rows beneath it are the
// truth, and nothing downstream is decided from it. Registered from
// internal/session/caption.go, which owns the call.
RoleCaption Role = "caption"
```

`DefaultAssignment[RoleCaption] = TierLow`. **Not `TierReflex`** — that tier's
own doc says *no role that has to REASON belongs on it*, and reading a
transcript tail to say what is going on is a digest, which is exactly what
`TierLow` is described as being for.

`errandRole` needs no case: the default `lane.RoleAuxiliary` is right, and it is
what keeps the narrator off the phase clock so it cannot take the status line
away from the turn the person is actually waiting on.

### The call

`internal/session/caption.go`, `title.go`'s shape almost exactly — including its
hard-won lesson that **the instruction goes last**, because a cheap model reads
the system message as character and the end of the user message as the thing to
do:

```go
const captionSystem = "You say what a working session is doing right now."

const captionPrompt = "In one line under 60 characters, present tense, lowercase, " +
	"no first person: what is this work trying to find out? Answer with the line only."

// captionDwell is how long a batch may run in silence before the narrator is
// worth paying for. Four seconds is past the point where a spinner stops
// reassuring and starts worrying, and far past a cheap call's own latency, so
// the line lands while the silence it explains is still going on.
const captionDwell = 4 * time.Second

// captionCalls bounds the narrator to three per turn, for [RoleMarkReader]'s
// reason said again: a turn that goes quiet six times is a turn where the sixth
// line is worth less than the first and the bill is real either way.
const captionCalls = 3
```

Hooked in the turn loop where the batch is already tracked (`runToolsWarm`): arm
a timer at batch start, disarm on the first `EventToolEnd` that completes the
batch or on any text delta. On fire, send the transcript tail plus the batch's
glosses, take one line, emit:

```go
// EventCaption carries one line ABOUT WORK THAT HAS GONE QUIET.
EventCaption
```

`Event` gains `Text` (already there) and reuses `Turn`; the UI keys it to the
open step. This is **the only new event kind** the whole feature needs, because
tiers A and B are computed entirely in `tui3` from events that already exist.

### The books

```go
a.addAuxiliaryUsageAs(response, named, 1, auxRoleCaption)
```

Session total honest, turn meter unchanged — the existing law that a turn which
happened to cross a threshold must not read as three times the cost of its
neighbours. A new `auxRoleCaption` constant sits beside `auxRoleTitle`.

**Refuse a non-answer.** `cleanTitle`'s machinery already exists and already
handles the failure this will hit — a small model handing its instruction back
— so the narrator reuses `stripOpener`/`namesTheInstruction` and adds
`captionPrompt` to the `instructionEchoDivisor` sweep in `namesTheInstruction`.
A refused line leaves the tier B floor standing, which is exactly right.

---

## 5. The drawing

### The fold head, and a deletion

`clusterRows` (`toolview.go`) currently emits `↳ 6 earlier tool calls · ctrl+o`
when a run exceeds the window. **The caption replaces that line.** `foldWord`,
`foldKeyWord` and `foldScrollWord` retire; a sentence with a call count on its
right is strictly a better fold head than a count alone.

```
  ▸ read how a fold decides to collapse                        6 calls
  ▸ found where the chip is minted                             2 calls
  ▾ rewriting clusterRows so the head is a sentence                 3s
      edit internal/tui3/toolview.go                           +14 −3
      bash go test ./internal/tui3                               ⠋ 3s
```

The right column follows the existing law verbatim — **drops whole segments
rather than clipping characters**, and below the floor it is absent entirely.
The live caption shows its own age (the count-up the running tool row already
draws); a closed one shows its call count.

### The shimmer

```go
// ── THE SHIMMER IS THE SPINNER, RELOCATED ───────────────────────────────────
//
// NOTHING ELSE ANIMATES ON THIS SURFACE (toolview.go) is a real law and this
// does not break it. When a step is COLLAPSED its tool rows are not on screen,
// so their spinners are not either; the caption inherits that budget and hands
// it straight back the moment the step is expanded. Exactly one thing moves, in
// exactly one place, at any moment — which is a tighter guarantee than the
// surface had before, because a nine-call cluster used to run nine clocks.
//
// AND THE MOTION CARRIES THE TENSE. Shimmering is present tense: this sentence
// is still true. Still is past tense: it stopped being true and the count on
// the right is final. That is a fact the person needs and no glyph on this
// surface currently states.
func (a *app) shimmer(text string) string
```

A band one lightness step above `hueNarr`, ~8 cells wide, travelling left→right,
period `shimmerPeriod = 36` frames (≈1.2 s at the existing 33 ms
`frameInterval`). Driven off `a.paints` like `formingInk`'s pulse, so it
cooperates with the one frame clock rather than opening a second.

**Frozen flat in the linear/screen-reader tier**, following `formingInk`'s
precedent (`reveal.go`) — and the caption is emitted once as plain text there,
never rewritten in place.

### The outline, under the chip

`workfoldLabel` (`workfold.go`) gains one segment:

```
▸ worked 24s · thought 6s · 3 steps · 11 tool calls · ctrl+e
```

`N steps` is omitted when there are no captions or only one — counted facts,
emptiness law, unchanged. Then `ctrl+e` opens to the outline rather than the
machinery, which is the second expand:

```
▾ worked 24s · thought 6s · 3 steps · 11 tool calls · ctrl+e
    read how a fold decides to collapse                        6 calls
    found where the chip is minted                             2 calls
    rewriting clusterRows so the head is a sentence            3 calls
```

Note the third line is still present tense. **That is the decision, and it needs
a law comment where a reader will trip on it:**

```go
// THE RECORD DOES NOT CHANGE UNDER THE READER. A caption keeps the words it was
// born with, including its tense. Rewriting the live "rewriting clusterRows" to
// a tidier "rewrote clusterRows" at settle would mean the sentence a person
// watched and the sentence they scroll back to are different sentences, and the
// surface would be quietly editing its own history to read better. The mild
// oddness of a present-tense archive is the honest cost of that, and it is
// cheap: an outline reads as a list of headings, where tense barely registers.
```

In `deckRows`, chip-open no longer falls through to the machinery. It emits the
caption rows and skips to `f.answer` unless a *caption* is open:

- `deck.workOpen[key]` → the outline
- `deck.capOpen[start]` → that caption's machinery

### Keys and hits

| Key | Where | What |
| --- | --- | --- |
| `ctrl+o` | live turn, empty draft | toggle the live caption's rows — the same gesture it has today, since the caption *is* the fold head |
| `ctrl+e` | empty draft | newest chip → outline (was: → machinery) |
| `enter` | caption selected | toggle that caption |
| `↑`/`↓` | empty draft | `selectTool` now walks caption rows too |
| click | anywhere | new `hitCaption` in `hitKind`, handled beside `hitFold` |

`hitFold` retires with `foldWord`.

---

## 6. Failure modes and what absorbs them

| Risk | What holds |
| --- | --- |
| The caption lies — says "reading tests", greps something else | It is a *head over what happened*, never a replacement. No tool row is ever deleted, the count on the right is honest, and one expand shows the truth. |
| Caption spam, one per call | Prompted for batch level; rule 1 refuses a caption covering fewer than two calls; rule 3 merges consecutive heads. |
| The answer's first line stolen into a caption | Impossible by construction — rule 2 requires `demoted`, which requires work to follow in the same turn. |
| A cheap model hands back its instruction | `cleanTitle`'s existing refusal machinery, extended with `captionPrompt`. Floor stands. |
| Shimmer read as a second spinner | It is the same budget, relocated; expanded steps do not shimmer. Pinned by a test. |
| Screen reader | Plain text, once, no shimmer, no live rewriting. |
| Cost dishonesty | Tier A tokens are ordinary turn tokens, already counted. Tier C through `addAuxiliaryUsage`, like the title. |

---

## 7. Tests

Behavioural, named as sentences, in `internal/tui3` unless noted:

- `TestACaptionIsTheFirstLineOfTheProseThatWorkFollows`
- `TestTheAnswersFirstLineIsNeverACaption`
- `TestASingleCallGetsNoCaption`
- `TestTheCompositeStandsWhenTheModelSaidNothing`
- `TestFourReadsUnderOneDirectoryNameThatDirectory`
- `TestConsecutiveHeadsWithNoWorkBetweenThemMerge`
- `TestExpandingACaptionStopsItsShimmerAndStartsTheRowSpinners`
- `TestTheLinearTierDrawsNoShimmer`
- `TestTheChipOpensToTheOutlineAndNotTheMachinery`
- `TestACaptionKeepsItsWordsAndItsTenseAfterSettle`
- `TestAnInterruptedTurnLeavesItsLastCaptionStill`
- `TestATurnWithNoWorkHasNoCaptions` (the sibling of
  `TestATurnWithNoWorkIsUntouchedByTheHierarchy`)
- `internal/session`: `TestTheNarratorDoesNotFireOnABatchThatFinishesFast`,
  `TestTheNarratorStopsAfterThreeCallsInOneTurn`,
  `TestANarratorAnswerThatIsTheInstructionIsRefused`

Budget `go test ./internal/tui3/ -timeout 15m`, never `8m`.

---

## 8. Gates — none of these are follow-ups

- **The manual law.** `internal/manual/chat/` in the same change: a `## ` heading
  in the asker's own vocabulary (people will search "what is that line", "why did
  it collapse", "how do I see what it did"), the changed `ctrl+e` behaviour, the
  changed `ctrl+o` head, and — stated plainly — **that a caption can be wrong and
  the rows beneath it are the truth**. Then grep the corpus for the old denial:
  any page quoting `N earlier tool calls` or describing the chip as opening to
  the machinery is now lying, and the gates check that a name is *mentioned*,
  never that the claim around it is true.
- **`internal/e2e/tuiwords_test.go`.** `foldWord`'s strings are almost certainly
  in the needle table and retire with it. The untagged reader in that package
  fails the moment the surface stops spelling a sentence the suite waits for —
  that is the gate that caught #184 and it will name us.
- **`docs/changes/unreleased/`.** `make changelog-new`. What was true: the fold
  head was a count and `ctrl+e` opened the machinery. What is true now: the head
  is a sentence and `ctrl+e` opens the outline.
- **`make test-laws`** picks up any structural test automatically via its
  `go/ast` import.
- **`.github/known-red.txt` only shrinks.** Never add a line.

---

## 9. Build order

Two waves, and per the owner's standing order the lanes are **split across
models — Codex (`gpt-5.6-sol`) alongside Opus**, never all on one.

**Wave 1 — the whole visible feature, no second model.** One branch off `dev`,
because A, B and D are one change: the caption cannot land without its floor and
the floor has nowhere to draw without the fold head.

1. `caption.go` — type, `deriveCaptions`, `composeCaption`, `shimmer`.
2. `hierarchy.go` — `stampCaptions`, `capHead`/`capCut`, `workingProse` offset.
3. `toolview.go` — `clusterRows` head; retire `foldWord`.
4. `workfold.go` — `N steps`; outline render.
5. `render.go` / `input.go` — `hitCaption`, keys, `deck` fields.
6. `system.md` — the section and the Tone diff.
7. Manual pages, tuiwords, change entry.

**Wave 2 — the dwell narrator.** Separate branch, judged on its own.

8. `roles.go` — `RoleCaption`, `DefaultAssignment`.
9. `session/caption.go` — the call, the dwell timer, the refusal.
10. `session.go` / `loop.go` — `EventCaption`, `auxRoleCaption`.
11. `tui3/feed.go` — fold `EventCaption` onto the open step.
12. Manual page gains the narrator; change entry.

Both waves: branch off `dev`, pull request against `dev`, watch to green, merge,
and clean local — the worktree, the branch and any review binary in the same
breath.
