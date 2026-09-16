# Home as mission control — the ruling and the lanes

*Owner-shaped 2026-09-10 over four rounds of the picker
(the removed first draft supplied the initial critique; the
picker artifact is the visual record). Status: **ruled 2026-09-10** — the owner took every default in §6. Everything in §2 and §3 is verified against the tree
on 2026-09-10; file references are to `dev` at `893d66067`.*

*Amended 2026-09-15: **laws 2 and 3 were re-ruled** by the owner after reading a
real quiet home — six of the seven panels whispering over a screen that was five
sixths scaffolding. A panel's column is now its content's to say: what has rows
is the field at the left, what has none gathers in a rail down the right edge,
and `projects` and `spend` are pinned to the top of that rail. The laws below
carry the change and say what each of them used to say; §1's screen is redrawn
under it.*

## 1. The ruling in one screen

Home is not a list of chats. It is a fixed set of **panels**, each answering one
question a person has when they walk up to a colleague's desk, each with its
own keys, each a door to the place that owns it. The chat list is one panel.

```
 codeaf                                                      $0.14 / $20.00 · thu 9:49am
 home   tasks   spend   settings
 ───────────────────────────────────────────────────────────────────────────────────────

 needs you                                       projects
 ? Searching for Apartments Near Minto      2h      ~/codeaf      12 chats · 1 running   master, 2 dirty
   needs your ok to run bash   1 yes  2 not now     ~/pricing-site   5 chats · a job up   main
 ? Clever Bet Prediction Model              6h      ~/infra          3 chats · quiet 4d
   the 2024 season only, or all three?  enter
                                                  spend                    today $0.14 of $20
 where you were                                     ▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪▪  under a cent per chat
 › Understanding Hash Tables in Data …      2m      ▁▂▁▃▅▂▁▁▇▃▂▅▂▁   14 days $34.10 · opus 63%
   here · explain open addressing vs chaining
   Understanding Bloom Filters in Eight …   11h
   AI Influencers and Developers in …       11h    running · 2
   Locate Recent Sandbox Task in …           1d    ◐ Generate and Display First 200 Primes   4m
   70 more                                           working · checking
                                                    npm run dev · pricing-site           up 3h
 since you left · 12h
   Spark Fleet Ssh Audit · 2 hosts up, 1 not       scheduled
   made reports/apartments-minto-street.md           the 6am repo watch
   the 6am repo watch found nothing changed          top movers before the open
   learned 2 things about codeaf                     2 rules hold

 ───────────────────────────────────────────────────────────────────────────────────────
 › say what you want done                                               here ~/codeaf
   type to reach anything · ↑↓ ←→ move · enter open · 1 2 answer · alt for the map
```

The field on the left is every panel with rows, in the order table's order; the
rail on the right is the pinned pair and, under a blank row, whatever is quiet
today — here `running`, `since you left` and `scheduled` all hold something, so the
rail is only its pinned top. THE SCREEN A PERSON ACTUALLY SEES MOST DAYS is the
inverse of this one: one panel in the field and six headings in the rail, which
is the reading that produced the 2026-09-15 amendment.

### The laws

1. **One primary action.** The box. Everything above it sets up what you type or
   where you press enter.
2. **The field and the rail** (ruled 2026-09-15, replacing *act left, watch
   right*). A panel with rows stands in the **field** — the columns left of the
   last one, filled from the top left corner down. A panel with nothing in it
   stands in the **rail**, the last column, flush with the right edge, as its
   heading and its whisper. `projects` and `spend` are **pinned** to the top of
   the rail whatever they hold, because their height is the same on every
   machine on every day; one blank row separates that pair from the panels that
   are in the rail only because they are quiet today. One column under 110
   cells, two to 170, three past it — and the field FILLS rather than balances,
   so a frame whose panels fit in one column leaves the middle of a wide screen
   empty rather than spreading two short columns over it.

   *What this law was.* Until 2026-09-15 the column was a fact about the panel:
   the person's three on the left, the machine's four on the right. It was
   replaced because on a quiet machine — which is most machines most of the time
   — six of the seven panels held nothing, and the screen was five sixths
   scaffolding standing over the one panel with anything in it. The trade is
   named in law 3.
3. **Stable RANK, flexible column and height** (ruled 2026-09-15). A panel keeps
   its rank in whichever column it lands in, so two panels that both fill never
   swap places. Its height is what it holds. Its column is its content's to say
   (law 2). Position is a rank, not a pixel.

   *What this law was.* It read "a panel keeps its column and its rank in it".
   The column half was struck knowingly: home's geography now moves, and a
   person cannot learn that `running` is on the right the way they could before.
   What is bought with it is that the left of the screen is only ever the things
   that are actually going on, which is the thing a person walks up to home to
   find. The rank never moving is what keeps the cost to one axis.
4. **An empty panel whispers.** It stays as its heading and one dim line that
   names what arrives here and the one thing that puts it there — never that
   the panel is empty.
   This is a deliberate narrowing of the emptiness law, which keeps its full
   force for numbers (`$0.00`, `0 tasks`, a blank age all still draw nothing)
   and yields for panels, because a panel that vanishes teaches nothing. The
   whispers are in §4 and are the only prose about the product on the screen.
5. **A short terminal squeezes in priority order.** `scheduled` and `spend` give
   way first, then `since you left`, then `running`; `needs you` and `where you
   were` shrink last. A squeezed panel keeps its heading and its `N more` fold.
   A GROUP INSIDE A PANEL folds before the panel gives up a row above it: the
   rows of `needs you`'s `unread` group sit at the foot of the panel's list, so
   the ordinary bottom-up cut spends them first, and when none is left the
   group's line goes and the fold names it — `8 unread · tasks` (#884).
6. **Preselect the previous thing.** On double-space the cursor is on the chat
   you were in before this one (this window's own stack, `chattabs.go`
   `tabList`). Enter is a switch in two keys; esc goes back.
7. **One row of the frame draws its answers, and the key answers it from
   anywhere on home.** No cursor move. That row is the one under the cursor when
   it can take an answer and the top answerable row otherwise (#884), so a digit
   works on a frame nobody has walked and a landing somebody HAS walked onto is
   the row the key means. The answers are drawn on the row so the key is never a
   guess. THE WORDS ARE THE QUESTION'S OWN EVERYWHERE; the KEYS are too wherever
   the question's own keys are digits, which is every card a conversation stops
   on. The one exception is a landing in `unread`, whose keys are `[a]`/`[n]`
   on its card, in its room and on its record and are `1`/`2` here — because a
   bare letter on home types (home.go: the foot promises "type to search or start
   something new" and the promise has no asterisk), and the drawn digit is the
   one printable exception this screen makes. The digit is mapped back to
   `session.LandingYesKey`/`LandingNoKey` before the answer leaves the surface.
8. **Two marks, one accent, one hint line.** `GlyphNeedsHuman` in `hueWarn` for
   a person — a conversation or a watch that has STOPPED, never a landing on
   `unread`, which has already finished — the single spinner cell for work. Nothing else wears a glyph or a
   colour. Money is a number. Key hints live on the foot and the alt map, never
   inside a panel.
9. **Nothing grows past its budget — unless you open it.** Every panel folds
   inside itself with `N more`, and THE FOLD IS A TOGGLE (owner, 2026-09-15; it
   used to read `N more · <place>` and be a door into the place, and `where you
   were`'s `N more · type to find one` was not a stop at all). `enter` on it
   opens the panel: its cap is lifted, the other panels squeeze in law 5's order
   to their floors, and it takes the column; the fold reads `N fewer` and enter
   shuts it. One panel is open at a time. An open panel taller than the column
   shows what fits and its fold names the place for the rest — `3 fewer · 40
   more · tasks` — so nothing is left without a door. The fold wears no mark
   (law 8). A tall frame hands
   its spare rows out — only once every panel has its natural height — in the
   squeeze's order reversed, one row each round: `needs you` and `running` to 8,
   `where you were` from 5 to 10, `since you left` to 8, `projects` to 8,
   `scheduled` from 3 to 5; `spend` never grows. The air that is left sits under the
   shortest column (owner, 2026-09-10: a 55-row terminal was two short columns
   over thirty rows of air). The budgets are the order table's `rest`/`most`.
10. **Home is the summary of the tabs.** `running` opens tasks, `spend` opens
    spend, `scheduled` opens standing. The bar is four places: `home tasks spend
    settings`. Memory, standing and search stay reachable by their slash
    commands and return to the bar when they are in daily use.
11. **The pulse leaves its counts at home.** On home the pulse is the budget and
    the clock; the panels are the counts. Inside a chat the pulse keeps `2 want
    you · 1 moving` and gains the budget, on row one, so the two frames share one
    head: pulse, strip, rule, blank.

### What is retired

- The three-column tier and `homeSwitchCard`. The card's five bands become
  panel lines: `leftoff` is the snippet under the `here` row, `made for you`
  lines are receipts, the facts line is gone (the number is on the row where it
  is not zero).
- The ten registered bands as anything the resting grid draws (`homebands.go`).
  What a panel needs it takes from the reading. The band registry itself stays,
  because three surfaces still read it: the card beside the typed search
  (`homeCardRows`), the standing item's card (`StandingItemCard`) and the phone
  sheet (`homeSheetBody`). It goes when the last of them stops asking.
- `ctrl+t` as the way to start in another folder. Enter on a project row is.
- Per-panel key hints, project tags on rows in this window's own folder, and
  `~` as a project name. A chat whose workspace is the home directory or a
  scratch folder at the top of `/tmp` wears no tag at all; the projects panel
  still lists both, as the paths `~` and `/tmp/af-stop-ws`, and cuts any path
  from the left (`…/code/codeaf`) so its counts stay on the row.

## 2. What the engine knows today, panel by panel

Verified 2026-09-10. "On disk" means readable by any window without a live agent.

| Panel | On disk today | Not on disk | Model call |
|---|---|---|---|
| needs you | `PresenceQuestion{Kind, Text, Options, Asked, Full *Question}` per session (`taskpresence.go:228`); task `your call` from `Status`+`Ending` (`task_status.go`); `standing.Item.NeedsPerson` | — | none |
| running | `PresenceTask{Title, State, StartedAt, Phase}` per session, fresh 15s (`taskpresence.go:160`) | activity line (`TaskIndexEntry.Activity` is `json:"-"`), adaptive `done/total` (only in `orchestrate.Snapshot`), background jobs (in-memory `exec.backgroundJob`; logs under `<ws>/.codeaf/jobs`), cost of a running node | none |
| since you left | standing `LastFired` + `LastLookLine`; memory `ChangedSince`; landed tasks with `EndedAt`, `Outcome` (first sentence of the report, `task_index.go:201`), `FilesChanged`, `Cost`; `artifacts.jsonl{Path, Session, Title, Kind, Created}`; look stamp `.last-look` | `StartedAt` on the index (audit-jobs row 2) | none. `Outcome` is written by the worker already |
| where you were | `Meta.Title` (the `title` role, low tier); `Summary.LastUser` via `Peek`; `SessionRow.At`; this window's tab stack in-process | — | the existing `title` call only |
| projects | `World.Project{Name, Dir, Sessions}`, `Running()`, `At()`; git status cached 5s (`homeband_repo.go`) | — | none |
| spend | `usage.jsonl`; `UsageByDay`, `LastDays(14)`, `UsageByModel`; `DailyBudgetUSD` in config | one function: today against the ceiling | none |
| scheduled | `standing.Item{NextDue, When.Kind, LastChecked, LastFired, LastOutcome}`; `homepanel_next.go`'s one sentence per kind | — | none |
| something is wrong | `lanes/<model>.json` vendor health; `calls.jsonl` when enabled | host reachability (live 2s dial), ladder moves (narrated, not written), missing key (derivable, not stored) | none |

**Zero new model calls.** The one candidate, a `receipt` role at the low tier
writing a better one-liner for a landed task than the report's first sentence,
is declined for v1: the fix is a prompt law that a worker's report opens with
the result in one sentence, which costs nothing and improves every surface that
shows `Outcome`. Reconsider after a week of real receipts.

## 3. The changes

### Engine — `internal/session` (lane E)

- **E1** `PresenceTask` gains `Activity string`, `Done, Total int` (adaptive
  runs only, from the snapshot the owning process already has) and
  `SessionPresence` gains `Jobs []PresenceJob{Title, Dir, StartedAt}` from the
  live roster. Written on the 5s heartbeat, read by `ReadAllPresence`. Old
  readers ignore the new fields.
- **E2** `TaskIndexEntry.StartedAt` written at start, so elapsed is real on a
  restored row (closes audit-jobs row 2).
- **E3** `ArtifactsSince(indexFile, stamp)` (pass `home.Join("v3", session.ArtifactsIndexName)`;
  on a hosted surface filter `World.Artifacts` by `Created` instead) and
  `LandedSince(world, stamp)` (every landed row, parts included — a panel that wants
  one line per piece of work drops rows with a `Parent`), so the ledger's per-task
  and per-file lines are one call each. Both return nothing on a zero stamp.
- **E4** `SpendToday(lines, now) float64` and `SpendShare(usd, budget) float64` beside
  `UsageByDay`, both pure; the caller reads the budget through `config.DailyBudgetUSDAt`.
- **E5** worker report prompt: the first sentence is the result. `taskOutcome`
  unchanged.

### Surface — `internal/tui3` (lanes G, P, K)

- **G1** `homegrid.go`: the `homePanel` interface (`id`, `rank`, `column`,
  `rows(width, reading) []placeRow`, `whisper`, `min`), the column ladder
  (1/2/3), the squeeze in priority order, the whisper for an empty panel, and
  the merge into `placeFrame`'s body. Pure over the cached reading; no I/O on a
  draw (placelaws test 4 holds).
- **G2** panels as one file each, `homepanel_<name>.go`: `needs`, `running`,
  `left`, `recent`, `projects`, `spend`, `next`. `wrong` is wave 2.
- **G3** `recent`: five rows, the `here` row with its `LastUser` snippet, the
  previous-chat preselect from the tab stack, `N more · type to find one`. The
  typed drop-up is untouched.
- **G4** `projects`: rows from `World.Projects`, this window's folder first,
  repo clause from the cached reading, enter → `startBeside` (`keeper.go:766`).
- **G5** `spend`: today bar + the 14-day sparkline `spendplace.go` already draws
  (`sparkline`, `:616`), sized to the panel.
- **G6** `next`: `homeband_nextup.go`'s ordering and clause, as a panel.
- **P1** `needs`: rows from presence questions, task `your call` rows and
  standing `NeedsPerson`, oldest first (`homeattention.go`). Answers drawn on the
  row from `Question.Options`; digits route to the top row through the existing
  `answers.jsonl` door. Enter opens (or brings here, `takeover.go`).
- **P2** `running`: presence tasks and jobs across sessions; title · elapsed ·
  phase, then activity and `done of total` once E1 lands; `s` stops only what
  this window holds (the cross-window stop is not a door today and the row says
  `another window` instead).
- **P3** `left`: `addLedger` extended with a line per landed task
  (`Label · Outcome`, cost on the right when not zero) and per file made, the
  look stamp unchanged.
- **K1** chrome: the pulse row on the conversation frame (`view.go`
  `chatFrameLines`, `chattabs.go` `tabsHeight`); the place head and the chat head
  become the same four rows. `placeHeadRows` stays 4; `tabsLineRow`/`tabsBottomPad`
  retire. Tests in `header_home_test.go`, `chattabclose_test.go:355`,
  `roomcrumb_contract_test.go:456` are rewritten to the one head.
- **K2** the bar: `pages.go`'s order table shows four places; memory, standing
  and search remain registered places reachable by command and by `alt+.`.
- **K3** keys: `↑↓` within a panel, `←→` across columns, digits to the top
  question, `alt+.` map gains the panels. Columns win `→`: it opens a row's
  verb strip only where no column with rows lies to its right (§6 ruling 6).
- **K4** phone (<60 cells): the inbox stays; panels stack one column with the
  same whispers.

### Manual, tests, docs (lane M)

- `internal/manual/chat/home.md` rewritten around the panels and whispers;
  `keys.md` for the arrow grammar and the four-place bar; every `/memory`
  `/standing` `/search` alias still spelled (the gate demands it).
- `internal/e2e/tuiwords_test.go` needles for the panel headings and the
  whispers; `TestTUIE2E` home subtests re-pointed.
- `docs/changes/unreleased/<pr>-home-mission-control.md` with the
  `invalidates` list: the card, the bands, `ctrl+t`, the seven-place bar, the
  emptiness law's narrowing.

## 4. The whispers (copy of record)

A whisper names what ARRIVES in the panel and the one thing that puts it there.
It never announces emptiness: `nothing is running`, `no chats yet` and every
sentence like them are the emptiness law inverted into words and stay banned
(docs/DESIGN-LANGUAGE.md, "presence over labels"). The exception is the label,
not the announcement.

A whisper WRAPS at its column's width and is never cut: it takes the dim lines
it needs, indented like a row, and the panel's floor is its heading and all of
them. So no whisper carries an ellipsis, quoted or not — a `…` on one of these
lines would read as the screen running out of room (owner, 2026-09-10).

| Panel | Whisper |
|---|---|
| needs you | `questions from any chat or task land here · a digit answers them` |
| running | `work you send off with /task runs here on its own` |
| since you left | `what watches and tasks did while the terminal was shut` |
| where you were | `your conversations · what you type below starts one` |
| spend | `every chat and task is priced here` |
| scheduled | `reminders, routines, watches and rules · "remind me at 6" or "every morning at 9"` |
| projects | never empty: the launch folder is always a row |

## 5. Lanes

Worktrees off `origin/dev` at `~/af-<name>`, pull requests against `dev`, full
suites on the Spark, Opus for the mechanical lanes, one change entry per PR.

| Lane | Owns | Depends on | Size |
|---|---|---|---|
| **E** engine seams | E1–E5, `internal/session` only | — | small, 1 day |
| **G** grid + quiet panels | G1–G6 | — (reads presence as it is) | large, the wave's spine |
| **K** chrome + keys | K1–K4 | — | medium |
| **P** live panels | P1–P3 | E (for activity/progress/jobs), G (for the grid) | medium |
| **M** manual + e2e + docs | §3 last block | G, K, P | small, last |

Order: E ∥ G ∥ K, then P, then M. G lands with `running` and `since you left`
drawing what presence and the index hold today, so nothing waits on E to be
useful.

## 6. Ruled 2026-09-10 (every default taken)

1. **Jobs in `running`** — yes. E1 publishes them; P2 draws them.
2. **`something is wrong`** — wave 2, once the ladder and the host dial write memos.
3. **No `receipt` role** — the prompt law (E5) instead.
4. **The emptiness law narrows for panels** — written into `docs/DESIGN-LANGUAGE.md`
   ("presence over labels") and CLAUDE.md's design-laws list in this branch.
5. **Cross-window stop stays absent**, not broken; the row says `another window`.
6. **Columns win the arrow.** `←→` cross columns, stepping over a column with
   nothing to stand on to the next that has a row — without which the rail would
   be out of the arrows' reach on a wide frame whose field fills one column. `→`
   opens a row's verb strip only where no column with rows lies to its right. A row whose verbs `→` cannot
   reach keeps them on their chords (`ctrl+o`, `ctrl+e`, …), and the foot names one
   so the door is never invisible.
7. **A whisper never outranks a row.** When a squeeze has to drop a whole panel, every
   whispering (empty) panel goes before any panel with rows, each group lowest keep
   first (`byDrop` in homegrid.go). Found by lane R2 at 120×14: the old order kept
   `needs you`'s whisper and dropped every conversation.

## 7. Where the work runs

Lanes E, G and K run as Claude Code instances on the Spark under fleet
(`af-home-e`, `af-home-g`, `af-home-k`; worktrees `~/af-home-<lane>` on the
Spark, branches `home/mc-<lane>`, briefs in `spark:~/af-home-briefs/`, logs
`spark:~/af-home-<lane>.log`, reports `spark:~/af-home-<lane>.report.md`).
The integrator merges each into `home/mission-control`, builds `bin/codeaf`
for the owner after every viewable step, and runs the full suites once, at
the end, on the Spark.

### Amended 2026-09-15 — `next up` is `scheduled`, and its rows carry no clock

The seventh panel was `next up · reminders & routines`, and each row carried its
clock at the right (`in 20h`, `mon 8:30`, `holds`) beside a title that usually
said the schedule too. The owner renamed it `scheduled` — one word, no
explainer, because it holds four kinds of order and the explainer named two —
and struck the margin. A row is the person's own words and nothing at its
right; its time is said once, in its description under the cursor, in the one
sentence its kind has: `reminder · goes off tomorrow 9:00am`, `routine · every
morning at nine · next tomorrow 9:00am · last: …`, `watch · every five minutes
· last looked 3m ago · found: …`, `rule · always`. An order stopped on a person
is not on it: it is a row of `needs you`, and one thing is said once across the
columns.
