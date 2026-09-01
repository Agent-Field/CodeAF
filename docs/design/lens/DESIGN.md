# The lens: the person participates in a chat and presides over a task, and one grammar serves both

2026-09-01, written against dev @ 483b4727. Follows a full recon of both render
paths (chat: `render.go`/`app.go`/`replay.go`; room: `room.go`/`roomorch.go`) and
of the steer machinery end to end. All four rulings were settled by the owner
on 2026-09-01 (see Rulings). Every `file:line` was verified at that commit.

## The problem, in the owner's framing

The chat and the task room were meant to be the same conversation surface
pointed at two different speakers, and mostly they are — one renderer paints
both. But they have drifted in ways nobody chose, and the differences that
*were* chosen were never derived from a stated philosophy, so there is nothing
to test a new difference against.

The unchosen drift, concretely:

- A room never learns a tool finished. `roomEvent`'s switch (`room.go:1115-1163`)
  omits `EventToolFinished`, `EventRetrying`, `EventNotice`, `EventConsentRequest`,
  `EventTaskProposal`, `EventTaskReplyTags` — all of which chat handles
  (`app.go:3859-4148`). A room's rows never show "took 4s"; a retry inside a task
  is invisible.
- Two live `bash` rows resolve differently in the two views: chat's `closeTool`
  takes the oldest row of that name (`app.go:4874`), the room's
  `roomClaimRunning` prefers an args match (`room.go:1432`). One of these is
  more correct, and both surfaces should have it.
- The room shaper (`shapeRoomJournal`, `room.go:877`) parses raw JSONL itself
  and knows only `user`/`assistant`/`thinking`/`reasoning` — a steered line, a
  note, a divider all reopen as ordinary user messages and bump the turn
  counter. It also hand-copies `journalArgsLimit`/`journalOutputLimit`
  (`room.go:1057-1060`) from `internal/session/loop.go`'s unexported caps, which
  the comment there already admits is a one-source-of-truth violation.
- Eleven functions in `room.go:1098-1485` are 1:1 mirrors of the chat's event
  ingestion (`formTool`→`roomFormTool`, `closeTool`→`roomCloseTool`, …). Every
  new event kind has to be wired twice, and the room is the copy that falls
  behind — see the first bullet.

And the deeper product question, which is the owner's actual framing: the
industry's agent surface is one long linear scroll — prose, tool spam and
status interleaved, with the human expected to read all of it, because the
premise of that surface is that the human validates every step. Aforge's
premise is the opposite: **the model executes well, and the person is needed at
specific moments** — to say what they want, to correct it mid-flight, to answer
what the work cannot decide alone, to judge whether it is going right, and to
see what it cost. A person goes to a task *to steer and to check*, not to read
a transcript; a person sits in a chat *to think alongside* the model. The two
pages must be derived from those two postures, or every future rendering
question gets answered by whim.

## The one-sentence design

One reducer and one renderer produce every transcript; a page differs from
another page only by a declared **lens** — the participant's (chat: a dialogue
at full fidelity, with finished work folded) or the overseer's (task: the
trajectory at a glance, with the machinery one gesture down) — and a
correction typed into running work is an **elbow** on both.

## The five human acts (why the lens has exactly these knobs)

Everything a person does on either surface is one of five acts, and the
surface's job is to keep each act's evidence at the salience that act needs —
not to replay the token stream:

1. **Intent** — the question, the brief. Flush left, accent `›`, never hidden.
   Already law: **A FOLD MAY NEVER HIDE THE PERSON'S WORDS**
   (`workfold.go:249-258`), with the task brief's three-lines-and-a-door as the
   single exception (`brieffold.go:22-30`).
2. **Correction** — the steer. The person bending work that is already moving.
   This is the overseer's *primary* act and today it renders worst exactly
   where it matters most (Decision 2).
3. **Consent** — a question the work cannot proceed without. Already the `ask`
   hue and one of the three things allowed to interrupt
   (`THREAD-UX.md:38-56`).
4. **Judgment** — the glance: healthy or not, on course or not. Served by
   failure-only signals (**NO SUCCESS GLYPH**, `styles.go:1499-1502`), by fold
   chips that summarize settled work, and — in a room — by the header
   instrument (Decision 3).
5. **Accountability** — receipts: what it cost, what it touched, how long it
   ran.

**Observation is not a sixth act — it is the mode of attention the acts run
on.** A person observes *in order to* judge or correct; it produces no mark in
the transcript, so it gets no salience row. But it is why the overseer's page
exists at all, and it earns its own affordances at three depths, each one
gesture from the next: the **glance** (the header instrument — verb, elapsed,
spend, the live line), the **watch** (the wide live frontier — the one place
where raw machinery *is* the content, because the person watching now has
chosen to watch), and the **audit** (`ctrl+e` into the folded past). A design
change that removes one of these depths, or puts more than one gesture between
adjacent ones, breaks observation even if every act still renders.

Everything else on the wire — narration, progress, the individual tool call
that worked — is the model's business: ambient, indented, foldable,
*retrievable but not broadcast*. This is `THREAD-UX.md`'s interruption budget
(question, delivery, failure) generalized into a rendering law:

**THE TRANSCRIPT IS EVIDENCE, NOT THE PRODUCT. THE FIVE ACTS ARE THE PRODUCT.**

The participant and the overseer differ only in which acts are near:

| act | chat (participant) | room (overseer) |
| --- | --- | --- |
| intent | every question, full | the brief, folded to three lines and a door |
| correction | elbow, inline where said | elbow, inline where said (Decision 2) |
| consent | interrupts the stream | interrupts the room and the rail |
| judgment | fold chips per turn | header instrument + phase folds (Decision 3) |
| accountability | inline per-turn receipts | aggregated in the header, not inline |

## What already exists that this stands on (verified)

| piece | where |
| --- | --- |
| the one renderer — `deckRows`, exactly three callers | `render.go:307`; callers `render.go:249`, `room.go:3029`, `roomorch.go:1063` |
| the `deck` type, built to prevent divergence | `render.go:124-156` |
| all tool rendering, incl. every tool-name special case, shared | `toolview.go` (`toolLine:388`, `toolBlock:222`, `detailBody:1568`) |
| answer hierarchy applied to every deck | `hierarchy.go:93-116` |
| the chat elbow — glyph, inks, landing clause, lifecycle | `steerelbow.go` (picture at `:15-27`, law at `:64-87`) |
| elbow pinned by fourteen tests | `steerelbow_test.go:149-666` |
| the engine's steer law — splices the turn, three landings | `internal/session/steer.go:1-96, 205-263` |
| the journal's steer mark | `sessionfile.go:573-591, 1747-1793`; `DisplayEntry.Steer` at `agent.go:2944-2979` |
| the task steer door — delivery-only, no decoration | `internal/session/task_room.go:100-159`, `agent.go:842-861` |
| the room's no-fold law (to be re-ruled, Decision 3) | `workfold.go:26-52` |
| the room's whole-screenful tool tail | `room.go:3208-3210` |
| the room header and kin rows | `room.go:2458, 2628` |
| the room shaper and its copied caps | `room.go:877, 1030-1060` |
| the mirrored ingestion functions | `room.go:1098-1485` vs `app.go:3771-4968` |
| the disclosure ladder that must never dead-end | `THREAD-UX.md:108-124` |

## Rulings already made that this design treats as law

1. **FLUSH LEFT IS WHAT WAS SAID TO THE PERSON; TWO COLUMNS IN IS WHAT WAS DONE
   FOR THEM.** (`steerelbow.go:83-87`, `render.go:538-556`.) Every lens obeys it.
2. **THE INTERRUPTION BUDGET IS A QUESTION, A DELIVERY, A FAILURE.**
   (`THREAD-UX.md:38-56`.) A lens may lower salience below this line, never
   raise anything above it.
3. **ONLY FAILURE SPEAKS.** No success glyph, ever (`styles.go:1499-1502`,
   D11). A healthy room is a quiet room.
4. **THE EMPTINESS LAW** and **NO MACHINERY VOCABULARY**
   (`conversations-design.md:179-186`). The lens struct's names are code names;
   nothing on screen ever says "lens".
5. **A FOLD MAY NEVER HIDE THE PERSON'S WORDS** (`workfold.go:249-258`). Phase
   folds in the room break around elbows exactly as chat's work fold breaks
   around them today (`steerelbow.go:159-167`).
6. **DISCOVERABILITY BEFORE PURITY** (`DESIGN-LANGUAGE.md:583-597`). Every
   altitude change ships with its visible door (`ctrl+e` chip, scroll hint) —
   no gesture whose only documentation is documentation.
7. **ONE SOURCE OF TRUTH.** The copied journal caps and the mirrored ingestion
   are both existing violations this design removes; it may not add new ones.

## Decision 1 — One reducer, one shaper; every divergence is a line in a lens literal

**Decision.** Three moves, in dependency order:

*The feed.* A `feed` type in `internal/tui3` owns event → `[]entry`: forming,
announcing, beginning, claiming, closing tools; thought settling; live-block
closing; unfinished resolution; compaction settling. Chat's family
(`app.go:4373-4968`, `thinking.go:101-125`) becomes its methods; the eleven
`room*` mirrors (`room.go:1098-1485`) are deleted. The room inherits every
event it is missing today for free, and a future event is wired once.
Chat-only behaviors (the spawn card at `app.go:4600`) become reducer hooks the
lens enables, not code the room copies around.

*The shaper.* The room consumes `session.DisplayEntry` like chat does. If node
journals are not exposed that way yet, the shaping moves into
`internal/session` beside `sessionfile.go`'s existing rebuild — the engine
owns journal interpretation, the view never touches JSONL. `shapeRoomJournal`
is deleted; `replayBlocks` (`replay.go:486`) becomes the only entry-shaping
path, parameterized by tail. The copied caps (`room.go:1057-1060`) die with it.

*The lens.* The deck's scattered knobs — `clock` (`render.go:182`),
`showsWork` (`workfold.go:47-52`), `toolTail` (`room.go:3208`) — become one
struct, defined in one file with two named literals so the whole difference
between the surfaces is readable in a dozen lines and diffable in review:

```go
// A LENS IS A POSTURE, NOT A PAGE. participantLens is a person inside the
// conversation; overseerLens is a person checking on work another agent is
// doing. A third surface picks one of these or argues, in writing, for a third.
type lens struct {
        clock      bool       // per-turn timestamps and receipts inline
        receipts   receiptsAt // inline | header
        foldPast   foldStyle  // turns (chat) | phases (room) | none
        toolTail   func(a *app) int
        spawnCards bool
        // …
}
```

`deckRows` itself does not change; it already does the right thing.

**Rejected: keeping the mirrors and back-filling the missing events by hand.**
That is the third time the same wiring would be written (chat, room, orch), and
the room is already the proof of what happens next: the copy that nobody
remembers to extend. Rejected: a second renderer for the room's different
altitude — the altitude differences in Decision 3 are all expressible as fold
policy and header content over the same rows, and `deck` exists precisely so
that a second renderer never gets written (`render.go:124-155`).

## Decision 2 — A steer is an elbow everywhere, and a task is a trunk with elbows

Chat already has the right physics. A steer there is **ONE CORRECTION TYPED
INTO A RUNNING TURN, DRAWN AT THE POINT IN THE TRANSCRIPT WHERE IT WAS SAID**
(`app.go:101-111`): the `└ ` glyph in dim, the words one reading step below the
question's own ink, a transient landing clause, the same turn number — because
the person did not ask a new question, they bent the one in flight
(`steerelbow.go`, `replay.go:520-528`).

The room has the same act with none of the physics. A room steer renders as an
ordinary `entryUser` that **opens a new turn** (`room.go:1637-1650`), and on
replay it is indistinguishable from any question — `shapeRoomJournal` writes no
steer mark at all (`room.go:910`), so yesterday's correction reads as a second
brief. This is backwards precisely where it matters most: a task *is* one
question — the brief — and everything the person says after it is, by the
nature of the page, a correction to running work. The overseer's primary act
is the one the room renders as something else.

**Decision.** A task room's transcript is a trunk with elbows:

- The node's journal gains a steer mark on steered lines, written where
  `enqueueSteeredLine` lands them (`agent.go:842-861`), shaped like the chat's
  `journalSteer` (`sessionfile.go:573-591`) minus the fields that do not apply
  — `SteerTask` promises delivery, not consumption (`steer.go:74-96`), so the
  mark carries `At` and a landing of at most one fact: whether the node was
  parked and the line woke it.
- Live and on replay, a steered line renders as the same `entrySteer` elbow
  chat draws — dim `└ `, narr words, flush left — and **does not bump the
  room's turn counter**. ~~Plain delivery gets no clause: **ONLY FAILURE
  SPEAKS**, and an elbow sitting in the transcript is itself the receipt.~~
  **RULED the other way (owner, 2026-09-01): every task steer confirms
  delivery.** The elbow carries a short clause on the chat elbow's grammar
  and fade ramp — `· delivered` on the plain case, and the room's existing
  sentence when the line woke a parked node: `it was waiting on its pieces —
  your line wakes it` (`room.go:1662-1668`). The clause fades; the elbow
  stays. The distinction from ONLY FAILURE SPEAKS: a steer crosses to
  *another agent*, and the person deserves the same certainty chat's landing
  clause gives them that the words did not vanish in the crossing.
- The steer guard (`room.go:1670-1722`) is untouched: a refusal still raises
  the guard and never silently reroutes, because a message the person believes
  a worker read must have been read by a worker.
- The engine's two doors stay two doors. `Steer` splices a turn; `SteerTask`
  delivers to a node (`steer.go:74-96`); their machinery must not merge. What
  merges is the *grammar*: the person did the same thing, so the page draws
  the same mark. **NO MACHINERY VOCABULARY** applies to shapes as much as to
  words — rendering the same human act two ways because two engine doors
  carried it is the machinery leaking through the glass.
- The run page's foot echo (`orchSteerLead`, `roomorch.go:296, 1785-1798`) is
  out of scope here — that page is a graph, not a transcript — but the moment
  a run page grows a transcript view, its steers are elbows too.

**Rejected: keeping the room steer as a new-turn `entryUser`.** It miscounts
turns the person never opened, it makes replay lie (a correction reads as a
brief), and it renders the overseer's primary act as the participant's. Also
rejected: gathering elbows back up under the brief. Chat already tried the
equivalent and recorded the scar — on work long enough to scroll, a correction
attached above the work is a correction above the screen, and "the message
just seems to disappear" (`steerelbow.go:37-46`).

## Decision 3 — The overseer's altitude: compressed past, wide present, an instrumented header

Today the room draws the opposite of what a check-in needs: no folds ever
(`deckFolds`, `workfold.go:26-52` — a written law, see the open ruling) and a
whole screenful of raw tool calls (`room.go:3208-3210`). That is the right page
for an *audit* visit and the wrong page for the visit people actually make: a
glance and possibly a steer. The result is the codex failure mode reproduced
one level down — a long linear machinery scroll the person must read to answer
"is this okay?".

**Decision.** The room serves the check-in by default and keeps the audit one
gesture away:

- **Compressed past.** Settled work folds into phase chips on the chat fold's
  existing grammar (`workfoldLabel`, `workfold.go:214-249`):
  `▸ explored the repo · 14 tool calls · 2m · ctrl+e`. What never folds:
  the brief's three-line door, every elbow, every failure, every ask, every
  delivery — the five acts stay on the surface, per the laws above. The fold
  is derived, never journaled, exactly as `THREAD-UX.md:22-36` already
  requires so that replay and live obey one rule.
- **Wide present.** The live frontier keeps the room's whole-screenful tool
  tail. The person watching *now* is the one reader for whom the machinery is
  the content.
- **The header is the instrument.** `roomHead` (`room.go:2458`) carries the
  judgment and accountability acts so the transcript does not have to: the
  current verb, elapsed, spend, call count, and the live tool line — the
  vaguest true sentence when nothing better is known (`stillWorking`,
  `render.go:1046-1070`). Per-turn receipts and the clock stay chat-only
  (`deck.clock`); a room's numbers aggregate in one place instead of dribbling
  down the scroll. **THE EMPTINESS LAW** holds per segment: a node that has
  spent nothing shows no spend.
- **The audit is one keypress, not a mode.** `ctrl+e` unfolds — the same key,
  the same chip, the same muscle chat already teaches — and the room's
  existing scroll-up-at-top unfold gesture (`room.go:3258`) keeps working.
  The ladder never dead-ends (`THREAD-UX.md:108-124`).

**Rejected: never folding (the status quo).** It optimizes the rare visit at
the cost of the common one, and it contradicts the product's own premise —
if the model executes well, the default page cannot be built on the assumption
that every call must be read. Rejected: a model-generated summary of the past
instead of derived fold chips — a second account of the work that can drift
from the work, costs tokens to maintain, and violates one-source-of-truth;
the fold chip is computed from the entries and cannot lie. Rejected: hiding
folded work behind navigation (a separate "details" page) — inline disclosure,
never a modal, never a dead end.

## Decision 4 — Semantics are identical, and the lens may only touch salience

Everything below is one implementation with no per-view fork, most of it
falling out of Decision 1's feed:

- **Pairing.** Args-match-then-oldest (`roomClaimRunning`, `room.go:1432`)
  becomes the law in both views; chat's oldest-of-name goes. It is strictly
  more correct when two same-name calls run concurrently.
- **Events.** `EventToolFinished` (so a room's rows carry "took 4s"),
  `EventRetrying`, `EventNotice`, `EventNudge`, consent requests, proposals,
  reply tags — handled by the feed, rendered by the shared renderer,
  everywhere.
- **Roles.** Steer marks, notes (`entryNote`), dividers (`entryDivider`)
  shape correctly in a room and stop inflating its turn counter.
- **Spawn.** The room keeps kin rows (`room.go:2628`) as the roster, but the
  transcript marks *where* a spawn happened — a one-line row at the birth
  position that doors into the kin row, on the reply-tag pattern
  (`render.go:1014-1035`): cause above consequence. The chat-only spawn card
  stays chat-only via the lens; the *event* is never dropped on the floor.
- **Accounting.** A room's closed tools feed the same HUD/spend walk chat's do
  (`app.go:7240-7350`), so the header instrument and the ambient counters are
  one set of numbers.
- **Timestamps.** Exist as entry data everywhere; `deck.clock` (a lens field
  now) decides whether they paint.

**THE LENS MAY LOWER SALIENCE; IT MAY NOT DROP A FACT.** That is the whole
contract, and it is testable: a table test walks every event kind and every
journal role through both lenses and asserts the entry exists in both decks.

## What this deliberately does not do

- **No new marks, hues, or spacing.** The elbow, the fold chip, the rails, the
  note lane, the reply-tag row — everything here is drawn with glyphs and
  tiers that already exist. A design that needs a new color is a different
  design.
- **No summarizer.** Every compression is derived from the entries.
- **No merge of `Steer` and `SteerTask`.** Two doors, one grammar.
- **No changes to the rail, the tasks page, or the record card** — they are
  status surfaces, not transcripts, and correctly share nothing with the deck.
- **Nothing touches v1 or the resident.**

## Rulings (all four settled by the owner, 2026-09-01)

1. **The room fold default — RULED: fold the past.** The task page folds
   settled work into phase chips by default; the machinery stays one keypress
   away (`ctrl+e` / scroll-up). This reverses the written law at
   `workfold.go:26-52` ("the page somebody opened BECAUSE they want to read
   the machinery") and the three tests pinning it
   (`roomwork_test.go:50,102,120`); the lane that lands it strikes and boxes
   the old law's comment with this ruling quoted, per the house style
   (`FIDELITY.md:14-33`), and rewrites those tests *with* the design.
2. **A room steer is an elbow — RULED: yes.** Steers draw as `└ ` where they
   were said and stop opening turns. Replayed rooms will read differently than
   they did — corrections become elbows, turn counts drop — and that is the
   fix, not the regression: the current replay is the lie.
3. **Chat adopts args-match pairing — RULED: yes.** One pairing rule on both
   surfaces, landed in the feed extraction, with a change entry naming the
   user-visible edge (two same-name tools concurrent).
4. **The steer receipt — RULED: always confirm delivery.** The owner overruled
   the silent-unless-woke recommendation: every task steer shows a short
   fading `· delivered` clause (the woke-a-parked-node sentence when that is
   the truer fact). See Decision 2 for the wording and the reasoning the
   ruling encodes.

## Lanes, if approved

| lane | scope | ships alone? |
| --- | --- | --- |
| L1 feed extraction | move chat ingestion onto `feed`; no behavior change; tests stay green | yes |
| L2 room adopts feed | delete `room.go:1098-1485` mirrors; room gains missing events, durations, pairing; ruling 3 lands here | yes, after L1 |
| L3 one shaper + elbow | journal steer mark, shaper into `internal/session`, delete `shapeRoomJournal`, room elbows; rulings 2 and 4 land here | yes, after L1 |
| L4 lens + altitude | the `lens` struct, phase folds, header instrument, spend into HUD; ruling 1 lands here; rewrites `roomwork_test.go` *with* the ruling | after L2+L3 |

Each lane owes its `docs/changes/unreleased/` entry and its manual pages in
the same change — the corpus currently describes the room's no-fold behavior
and chat-only steering, and both claims stop being true mid-way through this
plan. The salience contract test (Decision 4) lands in L2 and guards every
lane after it.
