# Rail, rooms, focus, homes — grooming from the third hands-on (2026-08-11)

USER REPORT, compressed: (1) the right bar has no separation, no hierarchy, and
no answer for hundreds of history items — v1's had both; (2) entering a task
shows nothing — no tree, no DAG, no tools, no sense that each task is an
orchestrator you can talk to; (3) clicks should land the keyboard in the
composer, typing should be the default, homes feels like a weird rail bolt-on
that belongs near the composer, and "untitled room" rows make it unclear where
one is supposed to talk at all.

Three sources were read against the report before this doc: v1's rail anatomy
(`internal/tui`), the Part-5/8/JOURNEY law, and the current `internal/tui2`
build. The headline: **complaints 2 and 3 are mostly LAW ALREADY WRITTEN that
the build diverges from; complaint 1 is the one place the law itself is
missing a mechanism, and v1 had the mechanism.**

---

## A. What is a bug against existing law (no design debate needed)

**A1. Task entry must re-scope the rail to the task's DAG.** 5.15: "enter on a
task card → the rail re-scopes to that task's DAG — row 0 = the task
orchestrator (its chat), then the plan steps and workers as an indented tree."
The user's screenshot shows the home list still standing while the breadcrumb
says they are inside the task. 13.17 photographed this working via
`run_trace.py`, so the live gesture (entering by pointer from inside an
untitled room, via the delivery card) is missing the `SubtreeNodes` + trace
read some other path takes. Root-cause lane is running; whatever it finds, the
acceptance is the user's gesture, not the harness's.

**A2. Untitled rooms are a P0 already on file.** 13.3 bug 4: wire the head-side
scribe that names sessions (8.2.13). "untitled room" is the honest placeholder
by design (`chat/scope.go:334`), but five of them at once is not a naming
problem — it is a MINTING problem: JOURNEY law says "session resumes by
default; `/new` starts fresh," so a launch must reopen the last room, never
mint a fresh one. An empty room with no draft and no messages should be
reaped or reused on the next `+ new`, not accumulated.

**A3. Composer-first custody.** v1's law, worth adopting verbatim: initial
focus is the composer (`internal/tui/model.go:959`), esc from any zone walks
back to the composer, clicks focus what was clicked. v2 inverted this — the
map holds the keyboard at rest and `App.railFocus` routes keys before the
shell sees them. 2f12b1c opened the click-back door; the remaining change is
the DEFAULT: typing is the rest state, the map is a place you visit. Digits
keep their 5.22 dual meaning, but only while the map holds the keyboard, and
the footer already says which meaning is live.

**A4. Empty states teach (5.22 rule 6)** — already in flight in this session's
fix wave (chat lens agent), listed here only so the doc is complete.

---

## B. The rail — where v1 had mechanisms the v2 law never wrote down

v2's only overflow answer is the line-budget fold ("the rail has no viewport,"
13.14) plus hard caps that silently drop history (`scopeLimits`,
`maxTaskRows = 24`). v1 had four mechanisms, all portable under the 7.1
doctrine (problems-solved, never visual language):

**B1. Live floats, settled sinks, settled dims.** v1 `orderRoots` partitioned
top-level jobs live-then-settled, each newest-first, and dimmed settled
subtrees instead of moving them to a "done" section. v2 sorts newest-first
regardless, so a dead `✕ $0.12` receipt sits above live work with equal ink.
The 7.2 stable-order law ("cards never re-sort while visible") still holds:
partition applies to NEW rows; a row that settles in place dims in place and
sinks only on the next scope build. This also answers 13.17's open finding
that a fresh commission lands below the homes group — live work claims the top
by definition.

**B2. One history row, not twenty-four receipts.** v1: all live jobs + the 5
freshest settled ones, then one selectable row `▸ history (37)` that expands
in place. This is the hundreds-scale mechanism, and the doctrine already
points at it from three directions: 5.18's `@` grammar shows live-first then a
dim `history` group; the ctrl+k palette already groups settled under
`history` (`overlay.go:289`); 8.1.7's fold line already carries a breakdown.
The rail is the one surface still pretending history is navigation. Needs the
disclosure seam 12.10 §6.4/6.5 already named (`rail.Row` has no disclosure
field — "the mapping here is one line in scope.go").

**B3. Section words, not boxes.** v1 separated sections with whitespace plus a
lowercase faint header word (`standing`, `services`, `tasks`) — exactly 5.13's
"whitespace not boxes; hairlines only at room boundaries." v2's home scope is
one undifferentiated column: rooms, task cards, homes rows interleaved. Three
faint words — `rooms`, `work`, `history` — and the blank line before each
would give the user's "clean separation" without one new visual element.
(5.24's no-new-nouns filter: all three words already exist in the product's
mouth.)

**B4. The collapsed dock.** v1's closed rail reduced to one line above the
composer (`⠙ 2 working · 1 failed — ctrl+t tasks`) and rendered NOTHING when
idle. v2's HUD mode (8.2.8) is the narrow-terminal answer; the wide-terminal
"I want the room, not the map" answer is this dock. Cheap, and it makes the
rail honestly optional, which the footer redesign (C2) wants.

---

## C. Homes, footer, and the one-mouth contradiction

**C1. The contradiction to settle.** JOURNEY.md: "The thread is the only
mouth; every other surface is eyes. No navigating into the graph to speak to a
task." 5.15: the composer re-binds to the selected row — task rooms are
speakable. Both laws are load-bearing and they disagree, and the user's "I
don't know if I chat here or inside the task" is the cost of shipping half of
each. Two coherent worlds:

- **One mouth (JOURNEY wins).** Row 0's thread is the only composer. Task
  rooms are records: tree, trace, receipts, steer verbs as CLICKS (cancel /
  steer / boost render as action chips, which 4.3 already asks for). Speaking
  to a task = `@task` from the one composer (5.18, built). Untitled rooms
  stop existing as a concept — there is the thread, and there is work.
- **Rooms all the way down (5.15 wins).** Every room speakable, main thread is
  just row 0. Then the scribe (A2) is not polish, it is the product, and the
  place line + composer binding must make "who hears this" unmissable.

RECOMMENDATION: one mouth. It matches the memory doctrine ("one mouth, 19
journeys"), it deletes the user's confusion rather than explaining it, it
makes A3 trivial (the composer never re-binds, so it can always hold the
keyboard), and steer-by-chip is closer to 4.3's card affordances than
steer-by-secret-second-composer. `@task` and `read thread://` (8.2.10) already
cover the speech acts a per-task composer would.

**C2. Homes move down, not out.** The user's instinct matches v1: home access
belongs in the bottom hug, near the composer. Keep the homes ROWS (5.24's
re-scoping rooms are right); move the DOOR. The footer today spends its line
on key legends; it should be the place strip: `home · notebook · standing ·
services` as click targets (registry-rendered, 5.22's six-surface law), with
the current room's name where it already is. The rail's collapsed homes group
then stops being the only door and can sink below history or disappear from
the home scope entirely.

**C3. The footer generally.** It is the least-designed surface on screen and
the user said so. One pass under the registry law: left = place (breadcrumb),
middle = the live meaning of ambiguous keys (digits, esc), right = doors
(homes, model chip, `?`). Everything a word, everything clickable, nothing
that is only a legend.

---

## D. The task room (once A1 is fixed)

The 5.15 shape is right and the user re-derived it independently: record
full-width in the lens, the task's tree alone in the rail. What v1 adds on
top, portable:

- **Collapse at hierarchy levels** — v1's trace blocks capped at 6/3 lines
  ending in `⋯`, click to expand, content-hashed identity so expansion
  survives streaming. 13.16 made `▸` rows doors; the tree needs the same at
  every depth (the 12.10 §6.5 disclosure seam again).
- **The legend taught once inside the scroll** (`✳ model · $ shell · ⌕ web ·
  › you · ⋯ expands`) — v1 line, 5.17-compatible, answers "what am I looking
  at" without a tutorial.
- **Named seam** `── execution ──` between the delivery and the trace, v1's
  `feedRule` idiom — the user's "very clear what the task is and what tools
  are used" is this one hairline.
- Per-node money stays 13.11's filed read gap; the narrator-chatter flood
  ("setting working standards · N of 5" × 5 as the room's first screen) stays
  filed as a producer question (13.17) — both belong in this room's next lane.

---

## E. Groomed order

1. **A1** — root-cause lane running; fix on its report.
2. **A2** — resume-by-default + scribe naming + empty-room reaping.
3. **A3** — composer-first custody flip.
4. **C1 decision** — user call required. Everything in C/D layers on it.
5. **B1+B3** — rail partition, dim-settled, section words (one lane).
6. **B2** — `▸ history (N)` row + disclosure seam.
7. **C2+C3** — footer as place strip, homes door moves down.
8. **D** — task-room collapse + legend + seam (after A1 proves the reads).
9. **B4** — collapsed dock, last; it depends on the footer having a door.
