# Background jobs, a task room, and work parked on a person — an audit

Captured against `bin/aforge` at `da5d1a37d` on a demo home built by the extended
seeder (`cmd/aforge-demo-home/seed_room.go`), at 160x50, 120x40, 80x24 and 60x30.
Every row closes on a frame in `docs/design/polish/frames/`, named at the end of
the row. All frames here are prefixed `seed-`.

## What this audit is, and what the fixture had to grow first

`audit-tasks.md` opens by saying that the jobs section, the job page and a task
ROOM had no frame in this wave because the demo home seeded no background jobs
and no live work. The seeder now writes both, through the records the product
itself writes: a checkpointed task graph in the conversation's own folder
(`internal/session`'s `taskDocument`) and three background jobs as the rows a
job keeps beside it (`jobrow.go`). `docs/design/home-rethink/HANDOFF.md` says
what is in it.

**The room is now capturable and it is captured — rows 4, 5, 9 and 10 are the
first look anybody in this wave has had at one.** The jobs section and the job
page are still not, and row 1 is why: the surface throws the jobs away before it
draws anything. That is a defect and not a gap in the fixture, and it is the
first thing these frames found.

## What cannot be seeded, and it is the product being right

**No running job and no running task node can be written to disk, because the
product does not keep one.** A job is a child of the aforge that forked it
(`jobrow.go`); a node names a worker in a working copy. So a job's row comes back
STOPPED (`task_store.go`'s `runRowNotice`) and a node the checkpoint calls running
comes back queued and paused (`interrupt`). A fixture that wrote either would be
faking a shape the product cannot produce, and every frame taken on it would be a
frame of something that cannot happen. What the fixture writes instead is every
state that IS keepable — done, failed, stopped, needing a person, and waiting
behind the one that needs a person — and the frames below are of those.

**A live room therefore needs a live engine.** Anyone who wants a frame of work
actually in flight has to drive a real turn against a real model; `internal/e2e`
is where that already happens. It is not something `make demo-home` can hand you.

---

1. A conversation reopened draws none of the background jobs it ran, because the surface has no case for them on the lane the engine replays them on — `internal/tui3/task.go:722` (`taskEvent`'s switch: `EventTaskUpdate`, `EventTaskPhase`, `EventTaskProposal`, `EventStandingProposal`, `EventStandingUpdate`, `EventTakeover` — and no `EventJobUpdate`), against `internal/session/task_run.go:3061`, which sends exactly that event onto exactly that lane for every restored job row — the whole of `jobrow.go` and half of `task_store.go` exist so that "a conversation reopened tomorrow redraws what it ran rather than an empty column beside a transcript full of jobs", and the engine keeps its half: the demo home's conversation holds three jobs with three logs on disk, the roster lane replays all three (a test asserts it: `TestTheFixtureHasBackgroundJobsWithLogsToRead`), and the column beside the transcript draws `tasks` and `standing` and nothing else — so a developer who left a dev server and a `make check` running yesterday comes back to a screen that says they never ran, and because the section is the ONLY door to a job's page (there is no `/jobs` command), the page and its log are unreachable from a resumed conversation, which is precisely when the log is the only thing left; `fix shape`: add `case session.EventJobUpdate: if ev.Job != nil && a.jobUpdate(*ev.Job) { a.touch() }` to `taskEvent`, which is the same two lines `apply` already has at `app.go:4316` — the reducer, the state and the drawing all exist and are tested, and only the routing is missing — sev: high — frames: docs/design/polish/frames/seed-talk.120x40.txt, seed-talk.160x50.txt, seed-talk.80x24.txt

2. A task restored from a checkpoint is dated by when the WINDOW opened plus how long the work ran, which is a landing time in the future — `internal/tui3/place_tasks.go:303` (`taskNodeEnded` answers `node.began` or, failing that, `node.met`, plus `node.elapsed`) with `internal/tui3/task.go:5504` (`met: a.now()` — "when this surface first met it") — a node that ran for twelve minutes and settled twenty minutes ago comes back with no start (the checkpoint keeps `elapsed_ms`, never a start), so the page dates it twelve minutes from NOW, and the date-window filter at `internal/tui3/tasksplace.go:213` (`r.win.Holds(tasksEntryAt(...))`) then drops it off the page altogether whenever that lands past midnight — the frames were taken at 23:52 and the task that needs the person is missing from the tasks place while its two siblings, which ran for four minutes and one, are on it saying `now`; a developer who opens `tasks` to find the thing waiting on them is shown a page that does not contain it, and the page is honest at noon and wrong at midnight, which is the worst kind of wrong to be told about; `fix shape`: a node whose start nobody knows has no landing time to compute — answer the zero time and let the emptiness law draw nothing, or carry a start on the checkpoint (`taskRecord` already carries `elapsed_ms`; `EndedAt` is the field the index writes and it is the one the page wants) — sev: high — frames: docs/design/polish/frames/seed-tasks.120x40.txt, seed-home.160x50.txt

3. Work parked on a person's decision is filed under `running` and dated `now` — `internal/tui3/tasksplace.go:316` (`case item.runs: return tasksRunning`, where `runs` is true for anything `Live()`, i.e. running OR queued) and `:844` (`tasksEntryAt` answers `now` for the same rows) — the two nodes waiting behind the piece that needs a look are drawn as `running · ○ Write the change entry and open the pull request against dev … now`, and the foot counts them `2 running`, on a machine where nothing at all is executing; a developer reading that page believes two workers are burning tokens right now and goes looking for the window they are in, and the vocabulary is outside the sanctioned set as well — CLAUDE.md's words for work are running, finishing, done, incomplete, needs your look, and a node blocked on a person is none of them; `fix shape`: split `runs` into "a worker is in it" and "it is admitted and waiting", give the second its own section word (`waiting`, which the rail already uses — `2 parked` on the column) and let the age come from nothing rather than from `now` — sev: high — frames: docs/design/polish/frames/seed-tasks.120x40.txt, seed-tasks.60x30.txt, seed-talk.120x40.txt

4. A task's identity is cut to three words before any width is known, so a 160-column room header names the work no better than a 24-column rail row — `internal/tui3/taskident.go:149` (`taskTitleOf` returns `firstWords(label, taskTitleWords)`) — the node is called "Cut every list on the task surface over to the shared row fitter so a name is never cut to nine cells" and every surface in the room calls it `Cut every list`: the header at 160 columns, the composer prompt, the status line, the kin lines, the landed card in the transcript; three of the six pieces of work in this family begin with a verb and a plural noun, so `Cut every list` and `Fold the settled` and `Move the tab` are what a person has to tell apart, and the room they opened to find out more tells them exactly what the column already did — this is `rowfit.go`'s law inverted at the source rather than at the row: the identity is truncated by a constant instead of by the space in front of it; `fix shape`: keep the whole title on the node and let each drawing site fit it — the rail passes 24, the room header passes its own width, and `roomHeadWord` at `room.go:2031` is already the worked example of doing that correctly — sev: high — frames: docs/design/polish/frames/seed-room.160x50.txt, seed-room.120x40.txt, seed-talk.60x30.txt

5. The landed card joins the state to the clock with a bare space while joining every other fact with ` · ` — `internal/tui3/taskdone.go:389` (`tail += " " + word` for the span; every other arm of the same function appends `" · " + …`) — the card reads `? □ Cut every list · needs your look 12m00s · 4 files · merged`, in which `needs your look` and `12m00s` are two separate facts fused into one phrase while `4 files` and `merged` are properly separated, so the one separator on the row means two different things and the state word — the reason the card is asking for a hand at all — reads as part of a duration; this is `audit-tasks.md` row 4 said about the card instead of the list, and the fix is the same one line; `fix shape`: `tail += " · " + word` — sev: med — frames: docs/design/polish/frames/seed-talk.120x40.txt, seed-talk.80x24.txt, seed-talk.60x30.txt

6. The word on a landed card for when the work began is `spawned` — `internal/tui3/taskdone.go:141` (`doneSpawnWord = "spawned "`) — the card reads `"Eleven packages measured. Four are over the …" · spawned 23:48 · ctrl+o output`, and `spawned` is the machinery's own word for starting a process, which this house bans in anything a person reads; the same file removed `worktree` for exactly this reason on 2026-09-01 and says so in a comment eight lines below this constant, so the rule is already accepted here — and the figure is wrong as well as the word: `23:48` is when this WINDOW first met the node (`task.go:408` says so), not when the work started, so a card for work that ran twenty minutes ago stamps it with the moment the terminal opened; `fix shape`: `started ` for the word, and the stamp from the node's own start where there is one and nothing at all where there is not — sev: med — frames: docs/design/polish/frames/seed-talk.120x40.txt, seed-talk.80x24.txt

7. A task's price is drawn twice on the same home card, four cells apart, in two different inks — `internal/tui3/place_home.go:752` (`cost = dollars(entry.Cost)` on the name row) and `internal/tui3/homeband_work.go:143` (`parts = append(parts, dollars(entry.Cost))` in the rows underneath it) — the card draws `? Cut every list on the task … $0.52` / `? needs your look · The four list…` / `4 files · $0.52`, and does it for every task that is not simply done, because the under-block is appended only in that case (`place_home.go:766`) and the under-block re-states what the name row already right-aligned; a figure a person sees twice is a figure they have to check against itself, and the second copy is what pushes `4 files` and the outcome sentence into an ellipsis on a card this narrow; `fix shape`: the name row owns the money — drop the cost from `homeWorkUnder`'s parts, or pass it a flag saying the row above already said it — one source of truth, and the file count then has the room the second `$0.52` was taking — sev: med — frames: docs/design/polish/frames/seed-home.160x50.txt, seed-home.120x40.txt

8. A conversation whose name carries a combining accent is drawn with the accent silently removed — `internal/tui3/home.go:4769` (`humanName`, the one composer of a row's name) — the seeded conversation is named `国際化とレイアウト幅 🌏 the café pricing page`, with `e` + U+0301 as the product stores it (`meta.json` on the seeded home carries the mark; a test pins it), and every frame of home draws `the Cafe Pricing Page` with no mark in the bytes at all — so the surface renames somebody's conversation, which is the one thing `titleCase`'s own comment promises it never does ("a title-caser that normalized would be a surface correcting somebody's spelling of their own subject"); the loss is BELOW the words layer — `titleCase`, `raiseFirst`, `fit` and `ansi.Truncate` all preserve the mark when handed the same string, and tmux's capture preserves it when a shell prints it — so the drop is in the cell buffer the frame is written through, and it will take a bisect from `homeRow` down to the renderer to name the line; the wide CJK and the emoji on the same row survive, so nothing about width is wrong — only the zero-width rune is gone; `fix shape`: find the writer that walks runes and drops the zero-width ones (the same rule `dragselect.go:249` states deliberately for hit-testing, applied where it must not be) and make it carry them with the glyph they belong to — sev: med — frames: docs/design/polish/frames/seed-home.160x50.txt, seed-home.60x30.txt, seed-home.120x40.txt

9. The room's kin line names a child's state with a word that is neither true nor in the vocabulary — `internal/tui3/room.go:396` (`roomKinSpawnedWord = "spawned: "`) and `:397` (the state joined after it) — the room of the parked task opens `part of: Measure the frame` / `spawned: Fold the settled — queued`, in which `spawned` is row 6's word again and `queued` says a scheduler will get to that child, when what is actually true is that it is waiting on the person reading this very page to answer the question at the bottom of it; a developer told "queued" waits, and nothing is coming; `fix shape`: `handed out:` for the lead, and the child's state through the same function the rail uses (the column says `2 parked` for these two nodes, which is the honest word and is already written) — sev: med — frames: docs/design/polish/frames/seed-room.160x50.txt, seed-room.120x40.txt, seed-room.80x24.txt, seed-room.60x30.txt

10. At 60 columns the room's foot drops every key that answers the decision the room is asking, and one of the four answers goes with it — `internal/tui3/room.go` (the foot's right-hand hints) and `internal/tui3/taskdone.go` (the answers row) — at 160, 120 and 80 the foot reads `a accept · l look again · n not right · esc` and the body offers `[a] accept · [l] look again · [n] not right · [d] decide these for me`; at 60 the foot's right side is empty and the body has silently lost `[d]`, which still works — so the narrowest terminal is the one where a person parked on a decision is told least about how to answer it, and is not told that anything was dropped (every other fold on this surface counts what it hid: `▸ +1`, `holds 3 more`, `▸ N earlier`); `fix shape`: the answer keys are the highest-ranked facts on that frame — put the foot's hints on the fitter with the answers first, and let the body's row end in a count (`· +1`) rather than in nothing — sev: low — frames: docs/design/polish/frames/seed-room.60x30.txt, seed-room.80x24.txt, seed-room.160x50.txt

11. The tasks place offers `stop it` and `open its room` on a row that has nothing to stop and no room to open — `internal/tui3/tasksplace.go` (the foot: `enter open its room · → verbs: stop it · type to filter · tab next place`) — the selected row on the captured frame is `○ Write the change entry and open the pull request against dev`, a node that has never started: there is no worker to stop and no journal for a room to replay, so `enter` opens a page with a header and nothing under it and `→ stop it` acts on nobody; a key hint has to be true for where a person is standing, and both of these are true only for the rows in the section this row does not really belong to (row 3); `fix shape`: the foot's verbs come from the selected row's state — a node with no journal offers no room, and a node with no worker offers no stop; the honest verb for a parked node is the one that unparks it — sev: med — frames: docs/design/polish/frames/seed-tasks.120x40.txt, seed-tasks.60x30.txt

12. The status line under a room wraps onto two lines and leaves a hole under the left half — `internal/tui3/render.go` / `phase.go` (the status line's two-segment layout) — at 80 and 60 the foot reads `? Cut every list · task claude-opus-4.1` on one line and `crew balanced · ◦ keeping an eye on 3 · $1.07 · 5.5k/1.3M · idle` on the next, right-aligned, so the frame ends on two ragged half-rows where every other width ends on one; the segments are already ranked (the model, the crew word and the token figure all drop out at 60), so the wrap is the fitter running out of ranks rather than a missing law — but two lines is the one outcome it must not reach, because the composer above it then moves; and `crew balanced` is machinery a person reads on the way past; `fix shape`: give the line a last rank that is the left segment alone, and rank `crew balanced` below the money and the tokens — sev: low — frames: docs/design/polish/frames/seed-room.80x24.txt, seed-room.60x30.txt, seed-talk.80x24.txt

---

## Not defects, recorded so the next reader does not re-file them

- **The room's header drops facts as it narrows** (`$0.52 · 7 tool calls · anthropic/claude-opus-4.1` at 160, nothing but `12m 0s` at 60). That is `roomHeadWord` on the fitter, doing exactly what `rowfit.go` says: identity whole, facts as a ranked prefix. It is the best-behaved header in the package and `audit-tasks.md` already calls it the worked example.
- **The wide-character row loses its project name at 60 columns** (`○ 国際化とレイアウト幅 🌏 the Cafe Pricing Page  1h`, where every neighbour keeps `pricing-site`). The title is 46 cells of a 60-cell row; the name is whole and the lowest-ranked fact is what went. That is the law working. The accent is row 8; the width is not a row.
- **The rail folds the family correctly** — `✓ Measure the frame` with `├─`/`└─` children three deep and `▸ +1` counting what it hid. Four levels of handing-out draw as a tree at every width down to 60.

---

## fixed

Frames prefixed `jobs-` were captured after the change, against the same seeded
demo home, with `scripts/frame.sh`. The `seed-` frames each row already names are
the before.

**Row 1 — a reopened conversation drew none of the jobs it ran.**
Files: `internal/tui3/task.go` (`taskEvent` gains `case session.EventJobUpdate`).
Tests: `TestAReopenedConversationStillShowsItsJobs`,
`TestAJobReplayedOnTheStandingLaneOpensItsPage` (`internal/tui3/jobsreopen_test.go`).
Before: `docs/design/polish/frames/seed-talk.120x40.txt` — the column draws `tasks`
and `standing` and nothing else.
After: `docs/design/polish/frames/jobs-talk-after.120x40.txt` — `▸ jobs · 3 ran`.
And the two frames nobody in this wave had ever taken, because the section is the
only door and it was not there:
`docs/design/polish/frames/jobs-section-after.120x40.txt` (the section open, three
jobs, `exited 0` / `done` / `stopped`) and
`docs/design/polish/frames/jobs-page-after.120x40.txt` (a job page with a log long
enough to scroll).

**Row 2 — a restored task was dated in the future.** PARTIAL, and the rest of it
is not the surface's to fix.
Files: `internal/tui3/task.go` (`taskNode.restored`, set where a node's first news
is already settled; `taskNode.spawnedAt` answers the zero time for one),
`internal/tui3/place_tasks.go` (`taskNodeEnded` no longer falls back to `met`).
Tests: `TestARestoredTaskIsNeverDatedInTheFuture`,
`TestAWatchedTaskKeepsItsOwnLandingTime` (`internal/tui3/taskstamp_test.go`).
Before: `docs/design/polish/frames/seed-tasks.120x40.txt` — the task that needs the
person is missing from the page.
After: `docs/design/polish/frames/jobs-tasks-after.120x40.txt` — it is the first row
on the page, under `needs your look`, with no age on it.
The surface now adds no arithmetic to a stamp it does not have: where the record
kept no start, nothing is drawn. **It cannot yet draw the RIGHT time, because the
record does not keep one** — see the section below.
The same change takes the invented `spawned 23:48` off a landed card for a restored
node (the card draws no stamp where `spawnedAt` is zero, `taskdone.go:459`). The
WORD `spawned` is row 6 and is untouched.

**Row 3 — work parked on a person was filed under `running` and counted `2 running`.**
The word chosen is **`parked`**, read out of `railGroupWords[railParked]` rather
than spelled a second time, so the column and the place cannot drift apart again.
Files: `internal/tui3/tasksplace.go` (a fifth section `tasksParked`;
`tasksSectionOrder` and `tasksSectionCount` so the headings and the tally are one
list; `tasksWorking` splits "a worker is in it" out of `tasksItem.runs`;
`tasksEntryStamp` splits the DRAWING's question off `tasksEntryAt`'s window
question, so a parked row draws no age).
Manual: `internal/manual/chat/tasks.md` — the place has five sections now, in three
places that named four.
Tests: `TestWorkParkedOnAPersonIsNotFiledUnderRunning`,
`TestTheTasksPlaceAndTheColumnCallParkedWorkOneWord`
(`internal/tui3/tasksparked_test.go`).
Before: `docs/design/polish/frames/seed-tasks.120x40.txt` — `running`, two rows
saying `now`, foot `2 running`.
After: `docs/design/polish/frames/jobs-tasks-after.120x40.txt` and
`jobs-tasks-after.60x30.txt` — a `parked` section, no age on either row, foot
`1 needs your look · 2 parked · 13 earlier`.

**Row 9 — the room's kin line said `spawned:` and called a blocked child `queued`.**
Files: `internal/tui3/room.go` (`roomKinSpawnedWord = "handed out: "`;
`roomKinWord` returns `railGroupWords[railParked]` for a child held behind a
prerequisite).
Manual: `internal/manual/chat/tasks.md` — the kin block's example and its two
bullets.
Test: `TestAHandedOutPieceWaitingOnAnotherSaysParkedInTheColumnsWord`
(`internal/tui3/roomkin_test.go`, renamed from
`TestASpawnedPieceWaitingOnAnotherSaysOnlyQueued`).
Before: `docs/design/polish/frames/seed-room.120x40.txt` —
`spawned: Fold the settled — queued`.
After: `docs/design/polish/frames/jobs-room-after.120x40.txt` —
`handed out: Fold the settled — parked`.

### What row 2 still needs, and it is in `internal/session`

The landing time a person reads has to be a fact of the RECORD carried across the
restore. Today it is not, and here is exactly where it stops:

- **`TaskNode.started`** (`internal/session/task_run.go:391`) is the only real
  start, set at `task_run.go:1089` when the node actually begins running. It is
  **not written to the checkpoint**: `taskRecord` (`task_store.go`, ~line 292)
  carries `ElapsedMS` and no stamp, and the field's own comment says a rehydrated
  node "has no started to measure from".
- **`TaskNode.runStart()`** (`taskground.go:216`) already makes the same guess the
  surface was making — `time.Now().Add(-n.elapsed)` — and refuses when both are
  zero. It is used only for the ground-shift window.
- **`TaskIndexEntry.EndedAt`** (`task_index.go:275`) IS the durable landing time,
  and it survives a restart **only for a node whose row reached the project's index
  FILE**. `taskIndexEntryOf` (`task_index.go:790`) stamps a rebuilt row
  `entry.EndedAt = time.Now()` for any settled node, and its own comment concedes
  the point: a row rebuilt later "keeps the file's row instead, which is where the
  original stamp is". So a settled node restored into the graph with no file row
  is dated the moment of the restore.
- **`TaskNotice`** (`task_contract.go:335`) carries `Elapsed` and no stamp at all,
  so the surface has nothing to READ even when the engine knows.
- The demo home reproduces exactly this: `cmd/aforge-demo-home/seed_room.go` writes
  the session's `tasks.json` with `elapsed_ms` and no index row, which is why all
  four of that family's settled rows now draw with no age.

What would have to be written: a stamp on `taskRecord` (start, landing, or both),
restored onto the node; `taskIndexEntryOf` preferring the node's own landing stamp
over `time.Now()`; and `EndedAt`/`StartedAt` on `TaskNotice` so the surface can read
it. Then `taskNodeEnded` becomes a field read with no arithmetic and no `restored`
flag, and the emptiness law covers only the genuinely undated.

### Not done, and why

- **Rows 4, 5, 6, 7, 8, 12** need `taskident.go`, `taskdone.go`, `place_home.go`,
  `homeband_work.go`, `home.go`, `render.go` / `phase.go` — files other lanes hold.
- **Row 10** needs `tasksettle.go` (`roomSettleHint`, the answers row) as well as
  the room's foot, and half of it would be worse than none.
- **Row eleven** is left. The foot is ALREADY conditional on the selected row
  (`place_tasks.go`'s `hint` and `verbs`): `enter open its room` is drawn only for a
  node in this window's graph, and `stop it` only where `stopTaskTarget` and
  `stopDoors` both answer — and cancelling a queued node is a real door the engine
  has. The room of a parked node is not empty either; it opens on the guard line
  `<title> is parked — `. What the row actually asks for is a verb that UNPARKS,
  and there is no seam behind one: a capability that cannot work is absent, not
  broken, so it is not invented here.

**Row 4 — a task's identity was cut to three words before any width was known.**
The cut is gone from the source and lives at every drawing site, which is where
the width is.
Files: `internal/tui3/taskident.go` (`taskTitleOf` answers the engine's own title
WHOLE — groomed onto one line, without the sentence's closing punctuation — and
`wholeName` is `firstWords` with no cap; `taskTitleWords` stays as the engine's
figure and is no longer a ruler this surface applies), `internal/tui3/room.go`
(the header states law 1 outright: a name that cannot fit the line ALONE takes
the whole line and no fact is drawn beside it; `roomChip` caps the status row's
cluster at `roomChipCap` = the composer segment's own `roomLeadCap`; the kin
block fits each relative's name to an equal share of the row it is charged to,
`roomKinName`).
Tests: `TestAWideRoomNamesTheWorkBetterThanANarrowRail`,
`TestTwoTasksSharingTheirFirstWordsAreToldApartInTheirRooms`,
`TestARoomTooNarrowForTheNameDrawsNoFactsBesideIt`,
`TestTheKinBlockKeepsEveryRelativesStateInsideItsRowBudget`
(`internal/tui3/roomname_test.go`), and
`TestTheNameCapIsTheEnginesOwnFigure` /
`TestARowDrawnUnderASentenceTakesTheShortNameWhenItArrives`
(`internal/tui3/taskview_test.go`) were rewritten onto the new law rather than
around it.
Manual: `internal/manual/chat/tasks.md` — the focus-header section.
Before: `docs/design/polish/frames/seed-room.160x50.txt` —
`? main ▸ Cut every list · needs your look · 12m 0s · $0.52 · 7 tool calls ·
anthropic/claude-opus-4.1`.
After: `docs/design/polish/frames/room-after.160x50.txt` —
`? main ▸ Cut every list on the task surface over to the shared row fitter so a
name is never cut to nine cells · needs your look · 12m 0s · $0.52 · 7 calls`,
with `room-after.120x40.txt`, `room-after.80x24.txt` and `room-after.60x30.txt`
showing the name taking more of the line as the facts give way, and the column
beside it still drawing `? Cut every list on the task surface over to the sh… #3`
— the cut moved, it did not vanish.
**Two neighbours it broke and closed in the same change**, both found on the
frames rather than in a test: the status row's cluster was handed the whole
hundred-and-one-cell name and left the ladder in `render.go` nothing to give up
but the cluster itself, so at 120 the line came out as the single word `idle`
(hence `roomChipCap`); and the kin block, wrapping an uncut child name, spilled
its three-row cap and ended on a bare `—` with the state word cut off the bottom
(hence the per-name share).

**Row 5 — the landed card fused its state word into its clock.**
Files: `internal/tui3/taskdone.go` (`doneTail` joins the span with ` · ` like
every other fact on the row; the doc-comment cards and `tasksettle.go`'s were
respelled with it).
Test: `TestALandedCardSeparatesItsStateWordFromItsClock`
(`internal/tui3/donewords_test.go`).
Manual: `internal/manual/chat/tasks.md` — the landing-card section.
Before: `docs/design/polish/frames/seed-talk.120x40.txt` —
`? □ Cut every list · needs your look 12m00s · 4 files · merged`.
After: `docs/design/polish/frames/talk-after.120x40.txt` —
`? □ Cut every list on the task surface over… · needs your look · 12m00s · 4
files · merged`.

**Row 6 — the word for when the work began was `spawned`.**
Files: `internal/tui3/taskdone.go` (`doneSpawnWord` → `doneStartWord =
"started "`, and the card's own field `spawned` → `started`).
Test: `TestALandedCardSaysStartedAndNeverSpawned`
(`internal/tui3/donewords_test.go`), which also pins the other half: a card that
knows no start draws no stamp, which is row 2's fix seen from the card.
Manual: `internal/manual/chat/tasks.md` — the landing-card section.
The FIGURE half of row 6 was already closed by row 2 (`taskNode.spawnedAt`
answers the zero time for a restored node), so this change is the word.

**Row 10 — at 60 columns the room told a person least about the decision it was
standing on.** Both halves.
Files: `internal/tui3/tasksettle.go` (`settleHidWord = " · +1"`, and
`settleParts` ends the row in it when the fourth choice will not fit — and drops
the count rather than an answer when even that will not; `roomSettleHints` is the
foot's line as a ranked-prefix ladder and `app.settleHintAt` picks the longest
rung the legend can actually draw, measured through `legendLeft`/`legendRoom`
rather than a second copy of that arithmetic), `internal/tui3/room.go`
(`roomHint` asks the ladder), `internal/tui3/taskeffort.go` (one line:
`railHoldHintWord` asks the same ladder with ` · esc` as its tail — that foot,
and not `roomHint`, is the one on the captured frames, because the roster is
holding the keyboard there).
Tests: `TestANarrowAnswersRowCountsTheChoiceItDropped`,
`TestTheDroppedChoicesCountAnswersToNoPress`,
`TestARoomAskingForYourLookNamesItsAnswersAtEveryWidth`,
`TestASixtyColumnRoomDrawsTheAnswersOnItsFoot`
(`internal/tui3/settlenarrow_test.go`).
Manual: `internal/manual/chat/tasks.md` — the answers row and the room's hint.
Before: `docs/design/polish/frames/seed-room.60x30.txt` — the foot's right side
empty, the body reading `[a] accept · [l] look again · [n] not right`.
After: `docs/design/polish/frames/room-after.60x30.txt` —
`─ room · esc/←← main ──────── a accept · l look again · +1 ─` on the foot and
`[a] accept · [l] look again · [n] not right · +1` in the body, against the same
frame at 80 and 160 where both rows are whole. With the ROSTER holding the
keyboard the same ladder runs with `· esc` on the end of every rung
(`a accept · l look again · +1 · esc` at sixty), which is the foot the `seed-`
frames were taken on.

**The two hints still cut by character.** `jobpage.go:386` and
`taskrecord.go:455` painted their key rows through a plain `fit`, and both are
now on `hintFit`. The way out moved to the END of both feet, because that fitter
keeps its last clause to the last cell there is and spends the ones in front of
it first — so `esc back` at the head was the way out being given away FIRST.
`x stop it` leads the running job's foot: it is the one verb on that page a
person cannot reach anywhere else.
Files: `internal/tui3/jobpage.go`, `internal/tui3/taskrecord.go`,
`internal/tui3/narrow_test.go` (`hintsStillCutByCharacter` is now empty — the
ledger only shrinks, and the law's second half fails if a fixed file is left in
it).
Tests: `TestTheJobPageAndTheRecordCardKeepTheWayOutOnANarrowFoot`,
`TestARunningJobsFootKeepsStopLongestOfItsFourKeys`
(`internal/tui3/hintwhole_test.go`).
Manual: `internal/manual/chat/reading-a-task-page.md` — both key rows are quoted
there and both were respelled.
After: `docs/design/polish/frames/jobpage-hint-after.60x30.txt`
(`↑↓ scroll · c copy path · esc back`) and `jobpage-hint-after.120x40.txt`
(all four), `taskcard-hint-after.60x30.txt`
(`↑↓ scroll · m puts it in your message · esc back`) and
`taskcard-hint-after.40x24.txt` (the phone lane's bar).

### Not done by this lane, and why

- **Rows 7, 8 and 12** need `place_home.go` / `homeband_work.go`, `home.go`, and
  `render.go` / `phase.go` — files other lanes hold.
- **Row eleven** is left for the reason the section above already gives: what it asks
  for is a verb that UNPARKS, and there is no seam behind one.

---

## fixed — the second pass

Frames prefixed `chat2-` were captured from `bin/aforge` in a real terminal on
socket `polish-chat2`, against a demo home freshly seeded by
`cmd/aforge-demo-home` (the room fixture, the wide name and all). Both fixes
below were REVERTED and their tests watched to fail before the fix went back.

**Row 7 — a task's price is drawn twice on one home card.** The NAME ROW keeps
it: it is right-aligned in the money ink at the card's own margin, which puts
every task's price in one column a reader can run an eye down, where the
under-block's copy sat mid-sentence between a state word and a file count, in
the dim, lined up with nothing. The under-block is now TOLD the row above said
it rather than left to guess, so the band that draws the price nowhere else
(`drawWorkBand`, whose name row carries the age) is untouched.
files: `internal/tui3/homeband_work.go` (`homeWorkUnderSaid`),
`internal/tui3/place_home.go` (`app.homeCardWork`)
test: `TestACardDrawsATasksPriceOnceAndNotTwice`
(`internal/tui3/homecard_test.go`) — scoped to the work band, because the card's
own totals band (`touched 4 files · spent $0.52`) is a different fact about a
different subject and a conversation that ran one task is where the two agree.
reverted: the failure prints the audit's card —
`? Cut every list … $0.52` / `? needs your look · The four list…` /
`4 files · $0.52`.
before: `frames/seed-home.160x50.txt` ·
after: `frames/chat2-home-after.160x50.txt` — `? Cut every list on the task …
$0.52` / `? needs your look · The four list…` / `4 files`, with the file count
back out of the ellipsis. (No card is drawn under 160 columns, so the 120/80/60
frames beside it are the list alone.)

**Row 12 — the status line under a room wraps onto two lines.** NOT FIXED, and
half of it was already true. The fix shape's second half — "rank `crew balanced`
below the money and the tokens" — is `dropOrder` (`render.go`), where `segCrew`
is the SECOND thing given up and both `segCost` and `segCtx` are last. What the
frames show is the other half: under `hudWrap` (100) the ladder stops dropping
the moment the telemetry alone fits the width and then always takes a second
row, so at 80 nothing is dropped at all. Making the line one row at every width
is a real design decision against a law that is written down twice — the
`statusRows` header ("the only place this surface spends a row on chrome, and it
spends it exactly where the alternative is truncating the numbers a person
opened the terminal to read") and a table in `bundle_test.go` that pins two rows
at 70 and 60 WITH every segment surviving. `statusHeight` already keeps the
frame's geometry in step with it, so the composer does not drift out of the
layout's accounting. Overturning that wants the mandate row 7 of the chat audit
was given; this lane did not have it for this row and did not take it.

**S12 (LEDGER) — `dollars` renders a positive cost as `$0.0000`.** Fixed here
because the caller list is this file's as much as the chat's. `dollars` and
`railFigure` were ALREADY the same under a cent (`%.4f` both) — the audit's
premise that one had solved it is half true: four places is right down to a
hundredth of a cent and silently wrong under one. Both now read one rule with a
floor: `<$0.0001`, which never rounds to a lie and holds ONE width for every
amount beneath it, which is what a line whose stillness is the point needs. More
decimals were the other answer and are worse — `$0.000006` is a figure nobody
acts on and its width depends on how small it is.
files: `internal/tui3/settingspend.go` (`subCent`, `moneyFloor`),
`internal/tui3/app.go` (`dollars`, three lines inside the one function),
`internal/manual/chat/screen.md`
tests: `TestAPositiveCostIsNeverDrawnAsZeros`,
`TestTheMoneyFloorMovesNoFigureAboveIt` (`internal/tui3/settingspend_test.go`)
reverted: `a cost of 6e-06 is drawn "$0.0000"`.
Every caller was checked: the only readings that move are amounts in
(0, $0.00005), so `$0.00`, `$0.0001`, `$0.0052`, `$1.63` and the limits' whole
dollars are byte-identical. It is visible live in
`frames/chat2-phase-after.120x40.txt`, where a turn's first sub-cent cost reads
`<$0.0001` on the status line.

**Row 8 — a name's combining accent is dropped. NOT AFORGE'S LINE, and this
lane found where it is.** Every layer named in the row preserves the mark —
`listName`, `homeName`, `humanName`, `titleCase`, `raiseFirst`, `unpackName`,
`fit`, `bandSides` and `switcherSides` all return
`国際化とレイアウト幅 🌏 the Café Pricing Page` with U+0301 in the bytes, at every
width that does not cut before the `é`. The loss is in the RENDERER, and the
bisect is two tmux panes on one socket:

- a shell `printf` of the same decomposed string, captured with
  `capture-pane -p`, comes back `c a f e 314 201` — tmux keeps the mark;
- a fifteen-line `charm.land/bubbletea/v2 v2.0.8` program whose `View` returns
  that same string, captured the same way, comes back `c a f e` — the mark is
  gone before tmux sees it.

The mechanism: bubbletea asks the terminal for mode 2027 (Unicode core) at
startup and switches its cell buffer to `ansi.GraphemeWidth` only if the
terminal says yes (`tea.go:794`). tmux says no, so the buffer stays on
`ansi.WcWidth`, and `cellbuf`'s `printString` resets the cell it just wrote
BEFORE the zero-width rune arrives — so the mark lands on a fresh cell of its
own instead of on the glyph it belongs to and is dropped. Nothing in this
repository walks those runes.

What could be done about it here is one line at the frame seam — normalize the
frame to NFC before handing it to bubbletea — and that seam is
`internal/tui3/view.go`'s `frame()`, which this lane does not hold. It is a
canonical-equivalence normalization and not a rewrite: it preserves the accent,
where today's behaviour deletes it. The other answer is upstream. Either way the
row is not `home.go:4769` and no amount of work in `internal/tui3`'s words layer
will move it.


---

## fixed — the third pass

Frames prefixed `t5-` were captured from `bin/aforge` in a real terminal on
socket `polish-t5`, against a demo home freshly seeded by `cmd/aforge-demo-home`.
Both changes below were REVERTED and their tests watched to fail before the fix
went back.

**The job page's foot rides under its last row, and names the way out only where
the head does not.** Both halves are `audit-tasks.md` rows 5 and 15, and they
land on this page as much as on the record card: a job whose log is eight lines
long drew twenty-seven blank rows under it with the rule and the keys pinned to
the bottom of the terminal, and `esc back` was on the frame twice. A log long
enough to SCROLL still fills the frame, because there the bottom of the frame is
where the content ends.
files: `internal/tui3/jobpage.go` (`jobPageTitleLine` answers the head line and
whether it had room for its right corner; `jobPageFootKeys` takes that answer;
the pad loop is `min(room, len(body))`), `internal/tui3/taskrecord.go`
tests: `TestAPageWithNoComposerPutsItsFootUnderTheLastRowItDrew`,
`TestAPageWhoseBodyScrollsStillFillsTheFrame`
(`internal/tui3/pagefoot_test.go`),
`TestTheJobPageAndTheRecordCardNameTheWayOutOnceOnEveryFrame` and
`TestBothPagesDrawTheWayOutInTheHeadOrOnTheFootAndNeverBoth`
(`internal/tui3/hintwhole_test.go` — the rewrite of
`TestTheJobPageAndTheRecordCardKeepTheWayOutOnANarrowFoot`, which is not deleted
and whose narrow case is not weakened).
before: `frames/t5-jobpage-before.{160x50,120x40,80x24,60x30}.txt` ·
after: the same names with `-after`.

**Row 11 — the tasks place's foot over a parked row: CLOSED AS NOT A DEFECT, and
here is the evidence.** The row says `enter` opens a room that is not there and
`→ stop it` acts on nobody. Both are false of the code as it stands, and the
frames say so:

- **`stop it` is real on a parked node.** `tasksPlace.verbs` (`place_tasks.go`)
  names the verb only where `stopTaskTarget` answers AND `stopDoors` answers, and
  cancelling a QUEUED node is a door the engine has. On the seeded home, `→` over
  `○ Write the change entry and open the pull request against dev` draws the strip
  `s stop it` — captured — and `s` cancels it.
- **`enter` opens a room that has something in it.** The same row's `enter` opens
  the node's room, which draws its header, its kin line (`part of: Fold the
  settled work on the task page`) and the guard sentence `nothing on this page yet
  — it fills in as the task works`. That is not "a header and nothing under it";
  it is the emptiness law answered with the one honest thing there is to say.
- And the foot is already conditional: `tasksPlace.hint` names `enter open its
  room` only where `taskSheetNodeFor` finds the node and falls back to
  `enter go inside it` otherwise.

What the row actually asks for — a verb that UNPARKS — has no seam behind it, and
a capability that cannot work is absent rather than broken. The second pass above
reached the same conclusion from the source; this pass reached it from the frames
as well, so the row is closed rather than left.

---

## fixed — the accent

**Row 8 — a name keeps its combining accent all the way onto the screen.**
REPRODUCED FIRST, both halves, on one tmux socket: a shell `printf` of
`the café pricing page` captures back with U+0301 in the bytes, and the same
name through `bin/aforge` captures back as `the Cafe Pricing Page` with the mark
gone. The store is honest — the seeded `meta.json` holds `e` + U+0301 — and so is
every layer of ours. The loss is BELOW us, and the previous lane named the
mechanism: bubbletea asks the terminal for mode 2027 at startup and only switches
its cell buffer to grapheme widths once the terminal answers yes (`tea.go:794`);
tmux answers no, so cellbuf's `printString` resets the cell before the zero-width
rune arrives and the mark lands on a cell of its own.
THE FIX IS NFC AT THE FRAME SEAM. `internal/tui3/view.go`'s `frame()` is now a
wrapper over `frameBody()` that returns `norm.NFC.String(body)`, so `e` + U+0301
reaches the terminal as the single rune `é` — one cell under either width table,
and no information lost, NFC being a canonical mapping.
**`golang.org/x/text` is already a direct dependency** (`go.mod` line 32,
v0.40.0) and `unicode/norm` is already imported by `internal/plan/terrain.go`, so
nothing was added.
**THE COST IS ZERO ALLOCATIONS AND THERE IS NO GATE**, which is a decision. Two
were tried. `norm.NFC.IsNormalString` — the obvious gate, and the one the brief
suggested — costs ONE allocation per call whatever it finds, which is an
allocation per keystroke to avoid a call that allocates nothing. A hand-rolled
`unicode.IsMark` walk allocates nothing and is about twice as quick on a frame of
this program's own chrome (10µs against 21µs on six kilobytes), and buys that by
answering in a second place a question norm is the authority on, for a saving
smaller than one entry render. `norm.Form.String` spans the string for the first
byte composition could change and, finding none, hands back the string it was
given: measured at 214 allocations for a frame with and without it, and the
four-thousand-line scroll law is unmoved at 218 against its ceiling of 220.
Files: `internal/tui3/view.go`.
Tests: `TestAFrameNeverBreaksALetterFromItsAccent`,
`TestComposingAFrameCostsNothingWhenThereIsNothingToCompose`
(`internal/tui3/framecompose_test.go`).
Reverted: dropping the composition prints "the frame drew the café pricing
page — an e and a combining acute on their own, which the cell buffer puts on
two cells"; replacing `norm.NFC.String` with the `norm.NFC.Bytes` form somebody
reaches for costs two allocations a frame and fails the second test.
Frames: `home2-before.120x40.txt` (line 11, `the Cafe Pricing Page`, no U+0301 in
the bytes) → `home2-after.120x40.txt` (line 11, `the Café Pricing Page`, a
composed `é`).
