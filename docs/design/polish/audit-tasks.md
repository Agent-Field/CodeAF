# The tasks place, the task page, and the jobs view — an audit

Captured against `bin/aforge` at `7f0aaf4b4` on the seeded demo home, at 160x50,
120x40, 80x24 and 60x30. Every row below closes on a frame in
`docs/design/polish/frames/`, named at the end of the row.

## What could not be captured, and why

The demo home (`cmd/aforge-demo-home`) seeds **no background jobs and no live
task**. So the jobs section in the column, the job page, and a task ROOM
(`room.go`, a task still running) have no frame here — rows 11, 15 and 16 are
read from the source and say so. Anyone extending the seeder with one running
job and one running node closes that gap; it is the single biggest hole in this
audit.

## Which lists go through `rowfit.go`, and which do not

`rowfit.go` states the constrained-space law. The package obeys it in five
places and re-implements it, differently, in four more:

**On the fitter.** `lanes.go:628` (`laneRowPlan`), `models.go:662`+
`palette.go:1273` (the model picker), `folderpick.go:724` (the folder hints),
`room.go:2031` (**the task room's header** — `roomHeadWord`, whose comment is
the best statement of the law in the package), `spendplace.go:400` and
`settingspend.go:253` (tails only), `phase.go`/`render.go` (the status line).

**Not on the fitter, each with its own arithmetic.** `tasksplace.go:683`
(`tasksRow` — the tasks list, row 1 below), `jobsview.go:174` (`jobRowLine`),
`deliverables.go:370` (`madeNote`), `taskrecord.go:498` (`taskCardBody`).
`jobRowLine` and `madeNote` reach roughly the right answer by hand;
`tasksRow` reaches the exact answer the law was written to forbid.

**Theme roles: clean.** `grep 'lipgloss.Color("'` over `task*.go`, `room*.go`,
`job*.go`, `lens.go`, `deliverables.go`, `tasksplace.go`, `place_tasks.go`
returns nothing. Every hue on this surface comes through `palette`. No row below
is a colour row.

---

1. A row cuts the task's OWN NAME to nine cells while a ninety-character sentence of detail beside it keeps full width — `internal/tui3/tasksplace.go:709` and `:722` — the list exists to let a person match names, and `✓ Put the…` matches nothing, so every scan of the page becomes a sequence of opens; at 160x50 — the most room anyone has — three of four rows still cut the name (`✓ Port the picker ont…`, `✓ Count the t…`), which is rowfit law 1 inverted exactly. The mechanism is two lines: the drop loop at `:709` only fires when `tasksFixedWidth(parts)+lead+8 > width`, so the facts are guaranteed everything and the name is guaranteed **eight cells**; then `:722` hands the label `width - lead - fixed`, i.e. the remainder after every surviving fact has taken its full spelling — `fix shape`: build a `rowPlan{primary: tasksLabel(entry), fields: […]}` and call `rowHalves`, with the detail sentence given a short spelling (`rowSay(middle, filesWord)` — "2 files" is the short form of "2 files · Annual is the default…") so it degrades instead of ending the tail; `roomHeadWord` at `room.go:2031` is the worked example in this same package — sev: high — frames: docs/design/polish/frames/task-list.160x50.txt, task-list.120x40.txt, task-list.60x30.txt, task-fold-open.160x50.txt

2. Sections named by time hold rows in no time order — `internal/tui3/tasksplace.go:151` (`readTasks` builds `order` from the world scan and never sorts it; `:238` appends each section in that order) — under "done today" the ages read `2h, 50m, 5h, 5h` and under "earlier" they read `8d, now, 2d`, so the one question the headings promise to answer — what happened most recently — cannot be answered by reading down, and a person looking for "the thing I ran just before lunch" has to read all ten rows and sort them in their head; the order is inherited from home's, which sorts PROJECTS by when somebody was last in a conversation (`internal/session/world.go:511`) — a fact about conversations, not about work — `fix shape`: sort each section by `tasksEntryAt(item.entry, now)` descending after the family pass at `:238`, keeping families contiguous under their root (sort roots, then sort each root's children); it is one `sort.SliceStable` and it makes the row order a function of the thing the heading names — sev: high — frames: docs/design/polish/frames/task-list.120x40.txt, task-list.160x50.txt

3. A filter that matches nothing makes the page state a falsehood about the machine — `internal/tui3/tasksplace.go:591` and `:609`, fed the filtered reading by `place_tasks.go:376`+`:855` — typing `zzz` draws `work aforge ran on its own. nothing.` across the top of a machine that has run ten tasks worth $2.21; the head sentence is a claim about the PLACE and the filter is a property of the QUERY, and `head()` computes both from `len(r.items)` after `tasksFiltered` has already replaced `r.items` with the survivors — a person who typed a word they half-remembered is told their history is empty; `fix shape`: `head()` must read the unfiltered reading — `r.held` is already the untouched count (`:225`) — or the place must pass `p.reading` rather than `a.tasksFiltered()` to the head line; the "nothing matches" news already has its correct home on the note line (`taskSheetFilterLine`) — sev: high — frames: docs/design/polish/frames/task-filter-none.120x40.txt

4. Every fact on a task row is joined to the next by a bare space, so the tail reads as one run-on phrase — `internal/tui3/tasksplace.go:738` (`b.WriteByte(' ')`) — `The Certificate Rotation incomplete now` is three separate facts (which conversation / what state / how long ago) that a reader has to re-parse into three, and `The Annual Toggle 2 files · Annual is the default and the monthly price stays visible beside it. $0.27 5h` mixes a real ` · ` inside one fact with bare spaces between facts, so the only separator on the row means two different things; every other list on this surface joins facts with `rowSep`/`railSep` (` · `), including this task's own PAGE one keypress away (`taskrecord.go:552`, `anthropic/claude-sonnet-4 · $0.31 · 38.4k tok`) — `fix shape`: falls out of row 1 — `rowTail` joins with `rowSep` and nothing else has to change — sev: high — frames: docs/design/polish/frames/task-list.120x40.txt, task-list.80x24.txt, task-list.160x50.txt

5. 24 blank rows under the list and 30 under the task page, with the rule pinned to the frame bottom — `internal/tui3/place_tasks.go:938` and `internal/tui3/pages.go:966` for the list; `internal/tui3/taskrecord.go:430`+`:437` for the page — the task page is the worse of the two: a six-line card is padded to fifty rows, so the reader's eye has to travel the whole frame to find a foot that says `esc back` when everything on the page ended at line 9, and the emptiness the law forbids in a figure is drawn here as thirty rows of it; `fix shape`: both fillers pad to a fixed `room` — let the frame's rule and foot ride under the last drawn row when the content is shorter than the room (the composer and the place strip keep their positions; only the rule moves up), and keep the pad only where the body scrolls — sev: high — frames: docs/design/polish/frames/task-page-done.160x50.txt, task-page-done.120x40.txt, task-list.160x50.txt

6. The header says `work aforge ran on its own. 10, $2.21 of it.` — a bare count with no noun after it — `internal/tui3/tasksplace.go:609` — "10," is a number a reader has to guess the unit of, and the comma splices two clauses that are not a sentence; the 60-cell spelling (`10 since aug 20, $2.21 of it.`) is worse, because "10 since aug 20" reads as if 10 were a sum of money; `fix shape`: give the figure its noun and make it one clause — `10 pieces of work since aug 20, $2.21 between them.` — with the noun coming from the same `plural` helper the rest of the file uses, so `1 piece of work` is right too; the emptiness branch above it (`nothing`) is already correct and should keep its shape — sev: med — frames: docs/design/polish/frames/task-list.120x40.txt, task-list.60x30.txt

7. A task the record still calls running, with nobody behind it, is dated `now` forever — `internal/tui3/tasksplace.go:844` (`tasksEntryAt` returns `now` for any `entry.Live()`) with the note from `:793` — the row draws `· Rotate the wildcard certificate … The Certificate Rotation incomplete now`, in which the note says the window that was running this is gone and the age says it is happening this second; the two halves of the same row contradict each other, and the age is the half a person believes because ages are what they scan; `fix shape`: `tasksEntryAt` is right for a genuinely live row and wrong for a stopped one — split the case that `tasksNote` already distinguishes (`entry.Live() && !item.runs`) and date a stopped row from `entry.StartedAt`, drawing `incomplete · started 3d ago`; where nothing dated it at all, draw the note and no age (the emptiness law) rather than an age that means "the moment you looked" — sev: med — frames: docs/design/polish/frames/task-list.120x40.txt, task-list.80x24.txt, task-page-incomplete.120x40.txt

8. One task wears two different state words on two surfaces one keypress apart — `internal/tui3/tasksplace.go:866` (`gave up, said why`) versus `internal/tui3/taskview.go:483` → `taskdone.go:135` (`failed`) — the list row reads `✕ Rebuild the frame budget report … Why the Frame Jumps gave up, said why $0.18 8d` and its own page reads `failed · landed 8d ago · ran 4m 0s`; a person who filed the row under one word cannot find it under the other, and `failed` is outside the vocabulary CLAUDE.md sanctions for work (running / finishing / done / incomplete / needs your look) while `gave up, said why` is inside it; `fix shape`: one word, chosen once — have `taskCardWhenLine` take `taskStateWord`'s answer and `tasksMiddle` take the same, so the list's phrase and the page's phrase come from one function; `failed` is used elsewhere in the package and pinned by tests (`roomlead_test.go:118`, `toolcompact_test.go:148`), so the change is to make the LIST and the PAGE agree, not to rename the state everywhere — sev: med — frames: docs/design/polish/frames/task-page-refused.120x40.txt, task-list.120x40.txt

9. Work that failed is reported as having `landed` — `internal/tui3/taskrecord.go:556` — the refused task's page opens `failed · landed 8d ago · ran 4m 0s`, and "landed" is this codebase's own word for work that arrived; a person reading it is told, in the same six words, that nothing came of the run and that it came home; `fix shape`: the clause is appended on `!entry.EndedAt.IsZero()` alone — make the verb follow the state the segment before it just named: `landed` where the work is done, `stopped` or `ended` where it failed or was refused; `EndedAt` is the same stamp either way — sev: med — frames: docs/design/polish/frames/task-page-refused.120x40.txt

10. A task's own page never says which conversation or project it came out of — `internal/tui3/taskrecord.go:498` (`taskCardBody`'s band list has no source row) — the LIST row carries it (`item.row.Title` / `item.row.Project` at `tasksplace.go:692`, drawn as `Porting the Picker`), and the page — the surface a person opens precisely to learn more — drops it; on a machine with three projects and twelve conversations, `Port the picker onto the new list` alone does not say which codebase it touched, and the one fact tying the record to a conversation is available only on the row you have just left; `fix shape`: add a dim source line to the band list between the outcome and the spend facts, spelled the way the row spells it, so the page is a superset of the row it opened from — sev: med — frames: docs/design/polish/frames/task-page-done.120x40.txt, task-list.120x40.txt

11. The job page prints the engine's raw state enum, and spells the exit code differently from the column that opened it — `internal/tui3/jobpage.go:505` (`strings.TrimSpace(string(job.State))`) and `:508` (`"exit "+itoa(...)`) against `internal/tui3/jobsview.go:36`/`:227` (`jobsExitedWord`, `"exited "+itoa(...)`) — `jobsview.go` defines `jobsDoneWord`, `jobsExitedWord` and `jobsStoppedWord` with a comment explaining that "it failed" and "you stopped it" are different news, and the page then bypasses all three and renders whatever string the session package happens to store, so the same failed job reads `3 · exited 1` in the column and `job 3 · failed · exit 1 · ran 49s` on its page; `fix shape`: `jobPageEnding` calls the column's own word function — factor `jobRightWord`'s switch (`jobsview.go:222`) into a `jobStateWord(job)` both call, so the page cannot drift and a new `JobState` cannot leak its enum onto a screen — sev: med — frames: none (no job in the demo home; read from source)

12. Every child of an opened family repeats its parent's conversation title, on the rows where names are already being cut — `internal/tui3/tasksplace.go:692` (the source cell is built per row with no knowledge of the fold) — opening the `+3 under` fold draws `The Tab Bar's Counts` four times down the page, spending about 22 cells on each of three rows to repeat a fact the root row already stated two lines up, while those same three rows cut their names to `✓ Port the…` and `✓ Port the lex…`; `fix shape`: `tasksRow` takes a flag from `paint` (which already knows `line.kin` is a connector) saying the source is inherited, and omits the source cell on a child whose root drew it — the cells go straight to the names, which is where row 1 wants them — sev: med — frames: docs/design/polish/frames/task-fold-open.120x40.txt, task-fold-open.160x50.txt

13. The foot counts seven pieces of work under "done today" and the section shows four rows — `internal/tui3/tasksplace.go:646` (`tally`) against `:569` (`tasksUnderWord`) — the arithmetic is right (four roots, one of them holding three) and the numbers come from one source, but the ONLY thing on the frame reconciling them is `+3 under` in dim ink at the far right of row 2, eleven cells from the edge, where the eye does not go; a person who reads `7 done today` and counts four is looking at what they will report as a bug; `fix shape`: the shut fold's count is a claim about the row and should read as one — draw it beside the fold mark on the LEFT (`▸ 3 ✓ Count the tabs…`) where the mark that opens it already is, rather than at the far right among the facts, so the mark and its count are one target and one sentence — sev: med — frames: docs/design/polish/frames/task-list.120x40.txt, task-list.160x50.txt

14. Row order changes between launches for reasons that have nothing to do with the work — `internal/tui3/tasksplace.go:151` inheriting `internal/session/world.go:511` (projects sorted by when somebody was last in one of their conversations) — two captures of the identical demo home eight minutes apart, with nothing run in between, drew `done today` as `Port / Count / Write / Put` and then as `Count / Port / Write / Put`; opening a conversation restamps its project and reshuffles a page that is supposed to be a record; `fix shape`: the same one-line sort that fixes row 2 also fixes this, because a section ordered by its rows' own timestamps cannot be moved by a conversation being opened — sev: low — frames: docs/design/polish/frames/tasks.120x40.txt (the rig's own earlier capture, `Port / Count / Write / Put`) versus task-list.120x40.txt and task-fold-open.160x50.txt (this audit's, `Count / Port / Write / Put`)

15. `esc back` is named twice on every frame of the task page — `internal/tui3/taskrecord.go:56` (head, right corner) and `:59` (foot, first clause) — on a card whose whole body is six lines, two of the sixteen words on screen are the same instruction; `fix shape`: the head corner is the one a person's eye lands on when they open the page and the foot is the legend, so keep the head's `esc back` and let the foot start at `↑↓ scroll` — the foot is already conditional elsewhere (`jobPageKeys` swaps clauses on state), so this is one constant split in two — sev: low — frames: docs/design/polish/frames/task-page-done.120x40.txt, task-page-refused.120x40.txt, task-page-incomplete.120x40.txt

16. The jobs section's overflow line wears a fold mark on something that does not fold — `internal/tui3/jobsview.go:209` (`jobEarlierLine` draws `jobFoldMark(false, pal)`, and its own doc comment at `:206` says "it is not a fold and it opens nothing") — `▸ 4 earlier` under a section whose head is also `▸` invites a keypress that does nothing, which is the one state this surface is not allowed to be in; `fix shape`: drop the mark and draw the count alone (`4 earlier`), indented under the rows it counts — the head's `▸` then means exactly one thing on that section — sev: low — frames: none (no job in the demo home; read from source)

17. The clock changes grain between the row and the page it opens — `internal/tui3/tasksplace.go:705` (`sinceAt` → `5h`, `8d`) against `internal/tui3/taskrecord.go:564` (`countUpWord` → `6m 0s`, `4m 0s`) — the list says how long ago and the page says how long it ran, which are genuinely two facts, but `6m 0s` spends four cells on a zero the emptiness law would strike anywhere else, and reads at a glance as a third kind of number; `fix shape`: `countUpWord` drops a zero seconds component when a minutes component is present, so `6m 0s` becomes `6m` and `4m 30s` is untouched — sev: low — frames: docs/design/polish/frames/task-page-done.120x40.txt, task-page-refused.120x40.txt

---

## fixed

Landed on `ui/polish-v0` by the tasks fix lane. Every row below was verified by
capture on the seeded demo home, at 160x50, 120x40, 80x24 and 60x30; the `-after`
frames stand beside the originals in `docs/design/polish/frames/`.

**Row 1 — the name is whole before any fact gets a cell.** `tasksRow` is off its
bespoke drop loop and on `rowfit.go`: it builds a `rowPlan{primary:
tasksLabel(entry), fields: …}` and fits it, and the loop, `tasksCell`,
`tasksOmit`, `tasksFixedWidth` and the `tasksDrop` ladder are deleted. The detail
sentence carries a short spelling (`rowSay("2 files · Annual is the default…",
"2 files")`) so it degrades instead of ending the tail. At 60 columns every one of
the ten names is now whole.
files: `internal/tui3/tasksplace.go`
test: `TestTheTaskNameIsWholeBeforeAnyFactGetsACell`
frames: `task-list.{160x50,120x40,80x24,60x30}.txt` → `task-list-after.{160x50,120x40,80x24,60x30}.txt`

**Row 4 — the facts are joined by ` · ` and ranked.** Falls out of row 1:
`rowTail` joins with `rowSep`, and the row's own painter (`tasksPaintTail`) inks
each fact and each separator so the money hue survives a tail that may not be
painted whole. The ranking is `tasksFacts`: the shut fold's count, the note, the
age, the money, the conversation, what came of it, the kind.
files: `internal/tui3/tasksplace.go`
test: `TestTheFactsOnATaskRowAreJoinedByOneSeparator`
frames: as row 1

**Row 2 and row 14 — sections named by time hold their rows in time order.**
`readTasks` sorts each section through `tasksInTimeOrder`: roots newest first,
each root's workers newest first under it, families whole. `done today` now reads
`1h, 2h, 5h, 5h`. Row 14 is the same fix — a section ordered by its own rows'
stamps cannot be moved by somebody opening a conversation.
files: `internal/tui3/tasksplace.go`
test: `TestEachTasksSectionReadsNewestFirst`
frames: `task-list.120x40.txt` → `task-list-after.120x40.txt`, `task-fold-open-after.160x50.txt`

**Row 3 — a filter that matches nothing no longer makes the page lie.**
`tasksReading` carries `whole`/`wholeCost`, counted in `readTasks` before any
query, and `head` reads those. Typing `zzz` still draws `work aforge ran on its
own. 10 pieces of work, $2.21 between them.`; the news that nothing matches stays
on the note line.
files: `internal/tui3/tasksplace.go`
test: `TestAFilterThatMatchesNothingStillCountsThePlace`
frames: `task-filter-none.120x40.txt` → `task-filter-none-after.120x40.txt`

**Row 6 and the fold's count — both figures said in words.** The head is
`work aforge ran on its own. 10 pieces of work since aug 20, $2.21 between
them.`, with the noun from the same `plural` helper and `1 piece of work` right
too; a frame too narrow for the whole sentence drops the spend clause rather than
letting the line be cut mid-figure. `+3 under` is now `holds 3 more`, and it
leads the tail instead of trailing it after a bare space.
files: `internal/tui3/tasksplace.go`, `internal/manual/chat/tasks.md`
test: `TestTheTasksHeadAndItsFoldsSayTheirFiguresInWords`
frames: `task-list.{120x40,60x30}.txt` → `task-list-after.{120x40,60x30}.txt`

**Row 7 — a stopped row is no longer dated `now`.** `tasksAgeField` splits the
case `tasksNote` already distinguishes. The audit's `entry.StartedAt` does not
exist — the index holds one stamp, `EndedAt`, and it is zero on exactly these
rows — so the row draws its note and **no age**, which is the emptiness law's own
answer and the second half of the audit's fix shape. The phone card's tail asks
the same function.
files: `internal/tui3/tasksplace.go`
test: `TestARowNobodyIsRunningIsNotDatedNow`
frames: `task-list-after.{160x50,120x40}.txt` (`incomplete · The Certificate Rotation`)

**Row 12 — a worker does not repeat its root's conversation.** `tasksRow` takes
the laid-out line rather than the work alone, so the two facts about where a row
SITS — the fold it holds shut and the root that has already named their
conversation — come from the layout that decided them. The cells go to the names.
files: `internal/tui3/tasksplace.go`
test: `TestAnOpenedFamilyNamesItsConversationOnce`
frames: `task-fold-open.160x50.txt` → `task-fold-open-after.160x50.txt`

**Row 8 — one state word on both surfaces.** `tasksMiddle`'s failed branch takes
`taskStateWord`'s answer and puts the reason after it (`failed · the package
manager refused the archive`), with the state word alone as its short spelling.
The list and the page cannot drift.
files: `internal/tui3/tasksplace.go`
test: `TestTheListAndThePageSayOneWordAboutWorkThatFailed`
frames: `task-page-refused-after.120x40.txt`, `task-list-after.120x40.txt`

**Row 9 — work that failed is not reported as having landed.**
`taskCardEndWord` makes the verb follow the state the segment before it named:
`done · landed 3h ago`, `failed · stopped 8d ago`.
files: `internal/tui3/taskrecord.go`, `internal/manual/chat/tasks.md`
test: `TestTheListAndThePageSayOneWordAboutWorkThatFailed` (second half)
frames: `task-page-refused.120x40.txt` → `task-page-refused-after.120x40.txt`

**Row 10 — the task's page names the conversation it came out of.**
`taskCardSourceLine` asks the reading's own ladder (`tasksRowFor`) so the card
and the row cannot name two different conversations, and draws `out of Porting
the Picker` between the outcome and the spend facts. Nothing known, nothing
drawn.
files: `internal/tui3/taskrecord.go`, `internal/manual/chat/tasks.md`
test: `TestTheTaskPageNamesTheConversationItCameOutOf`
frames: `task-page-done.120x40.txt` → `task-page-done-after.120x40.txt`, `task-page-incomplete-after.120x40.txt`

### Not fixed here, and why

The numbers below are SPELLED AS WORDS on purpose: `scripts/ledger.py`
reads `Row <digit>` anywhere under `## fixed` as a closure, and every row in
this list is one that is NOT closed.

- **Row thirteen** (the shut fold's count moves to the LEFT of the mark) — **half
  done**, and the other half is CLOSED AS A DESIGN CALL by the second pass below.
  The count is now a sentence and leads the tail, so the mark and the count read
  as one claim about the row rather than as a fact among the facts. Drawing it
  *inside* the family column (`▸ 3 ✓ Count the tabs…`) was left alone: that column
  is exactly two cells on every row of the page, and widening it on the rows that
  fold would leave every glyph on the page ragged — or, widened everywhere, would
  take two cells off every name on the page, which is exactly what row 1 was
  fixed to stop. The audit's fix shape does not settle that, and it is settled
  here: the count stays a sentence at the head of the tail.

### The manual, in the same change

`internal/manual/chat/tasks.md` carries the head sentence, the row's layout law,
the fold's count (its `## ` heading included, with the old `+3 under` spelling
kept in the body so somebody searching for what they used to see still lands on
the page), the card's source line and the `landed`/`stopped` verb.

---

## fixed — the second pass

The `t5-` frames were captured from `bin/aforge` in a real terminal on socket
`polish-t5`, against a demo home freshly seeded by `cmd/aforge-demo-home` (the
room fixture, three background jobs, the 101-character title). **Every fix below
was REVERTED and its test watched to fail before the fix went back**, and the
revert used is named on each row.

**Row 5 — a page with no composer ends where its content ends.** The task
record card drew a six-line card and then padded to the bottom of the terminal,
so at 160x50 the rule was on row 48 and `esc back` on row 49 for a page that had
ended at line 11. `taskCardFrame` and `jobPageFrame` now draw
`min(room, len(body))` body rows and let the rule and the key sheet ride under
the last one. **A page that SCROLLS keeps its pinned foot** — there the bottom of
the frame is where the content ends — so the pad is gone and nothing else is.
files: `internal/tui3/taskrecord.go`, `internal/tui3/jobpage.go`
tests: `TestAPageWithNoComposerPutsItsFootUnderTheLastRowItDrew` and
`TestAPageWhoseBodyScrollsStillFillsTheFrame`
(`internal/tui3/pagefoot_test.go`)
reverted: the pad loop was put back on each page in turn — the first test prints
the whole frame with its forty blank rows; and the second was watched to fail
against the OVER-application (every body line drawn, the frame's own trim keeping
the tail), which drew a scrolling log starting at line 182 instead of at the top.
before: `frames/t5-taskpage-before.{160x50,120x40,80x24,60x30}.txt`,
`t5-jobpage-before.{160x50,120x40,80x24,60x30}.txt` ·
after: the same names with `-after`.

**The tasks LIST half of row 5 is CLOSED AS NOT A DEFECT, and this is the
reason.** The list is a PLACE: it has a composer and a place strip at fixed rows
underneath it, and `composerlayer.go` states that the composer does not move.
Blank space between a short list and the rule there is not padding to nowhere —
it is the room the composer is standing on, and ten pieces of work in a fifty-row
frame is ten pieces of work. This product is deliberately calm and that emptiness
is honest. The two `t5-tasklist-{before,after}` sets at all four widths are
byte-identical apart from one row's age ticking from `42m` to `1h` between
captures, which is the point: nothing on the list moved, and nothing should.

**Row 15 — `esc back` is named once per frame, and always once.** Two laws met
here and were read as a conflict: the way out is the LAST clause of a key row,
because `hintFit` keeps its final clause to the last cell there is (audit-jobs's
hint rows); and the way out was drawn TWICE on every frame, once in the head's
right corner and once at the end of the foot. **The resolution is that the foot
carries the way out only where the head does not.** `taskCardTitleLine` and
`jobPageTitleLine` now answer the head line AND whether it had room for its right
corner, and `taskCardFootKeys`/`jobPageFootKeys` take that answer — so the head's
corner and the foot's clause cannot both be on, or both be off, on one frame. The
head loses its corner where a long title fills the line, and there the foot takes
the way out back, last, exactly where the earlier law wants it.
The held sheets order their clauses one way differently from the full ones —
`m puts it in your message · ↑↓ scroll` rather than the reverse — because
`hintFit` protects whatever is LAST and slices it if it will not fit whole, and a
twenty-five-cell mention left at the end would be the clause a narrow foot cut.
The scroll is both the clause worth keeping and the cheapest to keep.
files: `internal/tui3/taskrecord.go`, `internal/tui3/jobpage.go`
tests: `TestTheJobPageAndTheRecordCardNameTheWayOutOnceOnEveryFrame` — the
rewrite of `TestTheJobPageAndTheRecordCardKeepTheWayOutOnANarrowFoot`, onto the
new law and with the narrow case kept rather than weakened — and
`TestBothPagesDrawTheWayOutInTheHeadOrOnTheFootAndNeverBoth`, which counts the
rows of a real frame (`internal/tui3/hintwhole_test.go`)
reverted: three ways, each watched to fail — the foot always naming it (two rows
say `esc back`), the foot never naming it (a 101-character title at 80 columns
leaves the page with no way off at all), and the same on the job page.
before: `frames/t5-taskpage-before.120x40.txt` (`esc back` on row 1 and row 30) ·
after: `t5-taskpage-after.120x40.txt` (`m puts it in your message · ↑↓ scroll`).

**Row 17 — the clock does not spend four cells on a zero.** `countUpWord` drops
a rung whose remainder is zero: `6m`, never `6m 0s`; `4m 30s` and `1m 5s` are
untouched. It is the rule `internal/tui2/reltime`'s `Elapsed` already states for
the settled figure, said here for the live one, so the surface has one grain of
clock rather than two. Every reading that moves is an exact minute or an exact
hour: the bound on a bash row (`20s / 1m`), the phase clock (`checking · 15m`),
the room header (`12m`) and the record card (`ran 6m`).
files: `internal/tui3/toolview.go`, and the two expectations that quoted the old
spelling (`bundle_test.go`, `phase_test.go`)
tests: `TestACountUpDropsARungWhoseRemainderIsZero` and
`TestATaskThatRanAWholeNumberOfMinutesSaysSoOnItsCard`
(`internal/tui3/clockgrain_test.go`)
reverted: the old two-rung spelling — `a duration of 1m0s is spelled "1m 0s"` and
`the card's clock line is "done · ran 6m 0s"`.
before: `frames/t5-taskpage-before.160x50.txt` (`done · landed 43m ago · ran 6m 0s`) ·
after: `t5-taskpage-after.160x50.txt` (`done · landed 1h ago · ran 6m`).

**Row 11 — the job page says the column's word and never the engine's enum.**
`jobRightWord`'s switch is factored into `jobStateWord(job)`, and `jobPageEnding`
calls it. The same failed job now reads `3 · exited 0` in the column and
`job 3 · exited 0 · ran 49s` on its page. The page's separate `exit N` clause is
gone with it: `exited 1` already carries the code.
files: `internal/tui3/jobsview.go`, `internal/tui3/jobpage.go`
test: `TestTheJobsColumnAndItsPageSayOneWordAboutHowAJobEnded`
(`internal/tui3/jobwords_test.go`)
reverted: `the page for a failed job says "failed · exit 1 · ran 32s" and the
column beside it says "exited 1" — one job, one word`.
before: `frames/t5-jobpage-before.120x40.txt` (`job 3 · failed · ran 49s` under a
column row reading `make check   3 · exited 0`) ·
after: `t5-jobpage-after.120x40.txt` (`job 3 · exited 0 · ran 49s`).

**Row 16 — the jobs overflow line wears no mark it cannot open.**
`jobEarlierLine` draws `4 earlier` with no `▸` in front of it, at the rows' own
indent (`jobRowIndent`, now one constant the rows and the count share) so it sits
under what it counts. The section's head keeps the only fold mark on it.
files: `internal/tui3/jobsview.go`
test: `TestTheJobsOverflowCountWearsNoMarkItCannotOpen`
(`internal/tui3/jobwords_test.go`), which walks the ASCII palette too
reverted: `the overflow line is "  ▸ 4 earlier" and it wears the fold mark "▸",
which opens nothing`.
frames: none — the demo home seeds three jobs and the section fits all three, so
there is no overflow line to capture. `TestAJobsSectionCountsTheRemainderOnAnEarlierLine`
is the frame for it.

### The manual, in the same change

`internal/manual/chat/tasks.md` (the overflow line, both job feet, the record
card's foot and the fact that it rides up, the page's ending word, `ran 4m 12s`),
`internal/manual/chat/reading-a-task-page.md` (both key rows and the page's
ending), `internal/manual/chat/keys.md` (the card's foot) and
`internal/manual/chat/screen.md` (the count-up's zero rung, and the bound's
`2m`).
