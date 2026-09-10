# The places, audited — every room but home, at three sizes

*Lane Q, 2026-09-10, on `home/mc-q` at `142ecb2f1` (lanes G and K merged). Every
frame below was drawn by the package's own labs (`everyPlaceTable` in
`internal/tui3/placeeveryone_test.go`, the empty labs `placeApp` + `showPage`,
`standingPlaceApp(t, nil, nil)`, `memoryPlaceApp(t, nil)`, `searchLab`) through
`app.frame()` at 80×24, 120×45 and 180×45, with a truecolor palette whose SGR
runs were mapped back to palette roles — so a tier named here is the role the
code asked for, not a guess from a screenshot. Rows are 0-based terminal rows.*

## The ranked findings

| # | Law | Finding | Where |
|---|---|---|---|
| 1 | no jump | **The foot rule moves a row between places.** A place with a note line (tasks, settings) draws its foot rule at `H-4`; every other place draws it at `H-3`. `tab` from tasks to standing moves the rule and the whole body's bottom edge by one row. The same jump happens *inside* tasks: the note `9 finished today · 191 earlier` exists only when there are rows, so the first task landing moves the rule up. | `pages.go:1139` counts `len(note)` into the foot; `place_tasks.go:1611`, `place_settings.go:146` |
| 2 | empty | **Standing's empty prose runs off the frame.** `an order stands until you stop it, and it can reach just this conversation, this project, or everywhere.` is 103 cells and is drawn unwrapped at 80 columns. `TestEveryPlaceTakesExactlyTheWholeFrameAtEveryWidth` never sees it because its lab always holds three orders. | `standingplace.go:458`, `place_standing.go:167` |
| 3 | empty | **Four empty places announce emptiness in words**, which "presence over labels" bans: `nothing stands here yet — …` (`placeprose.go:88`), `nothing learned yet — say "remember that …" …` (`memoryplace.go:49`), `nothing spent yet — the first model call writes a line here.` (`place_spend.go:547`), `no tasks yet — /task <brief> starts one` (`taskview.go:88`, drawn by `tasksTeach`). | as cited |
| 4 | empty | **Empty places draw paragraphs, not a whisper.** Tasks 4 lines (`tasksplace.go:1914`), standing 3 (`standingplace.go:455`), memory 5 (`memoryplace.go:28` + `:49` + the footer `:444`), spend a 5-row paragraph (`place_spend.go:539`), search 4 (`searchplace.go:311`,`:317`). Home says the same kind of thing in one dim line under a heading (`homegrid.go` `homeWhisper`). Memory keeps teaching until it holds 8 lines (`memoryTeachBelow`), so the prose sits over the first seven rows and vanishes all at once on the eighth — a 4-row jump. | as cited |
| 5 | hover | **Hover paints two different steps.** Tasks, standing and settings paint the *cursor* step with an accent `· ` in the gutter (`palette.go:881` `overlayLead`, `place_tasks.go:1217`); memory, spend and search paint the *selected* step. THE GROUND LADDER says cursor and hover are ONE step, the cursor step. The accent `·` is a second accent on a screen whose cursor `›` already spends it. | `palette.go:876`; memory/spend/search row painters |
| 6 | click | **Three click grammars.** Tasks: one click moves the cursor and opens the row (`place_tasks.go:1076` `taskSheetEnter`). Settings: first click selects, a click on the selected row activates (`settings.go:2235`). Standing, memory, spend, search: a click only moves the cursor and never acts (`pages.go:234`, `place_spend.go:692`, `place_memory.go:897`, `place_standing.go:941`, `place_search.go:448`) — a second click on the same row does nothing. Home (lane G) is select-then-open with one-click folds (`home.go:3868`). | as cited |
| 7 | hierarchy | **The keyboard cursor wears two different steps.** Tasks, standing, settings: cursor step. Memory, spend, search: selected step. Home: cursor step. | tag dumps, row 7 of each 120×45 frame |
| 8 | hierarchy | **Row subjects are not in the primary tier.** Tasks paints every title `muted` (`The i0 errand`); standing and settings paint every resting subject `dim` (`overlayRowTinted`'s default arm, `palette.go:839`) so the margin `asks first · Mondays at 9am` lifts to `ink` on the cursor row while the subject beside it stays bold ink — and on a *hovered* row the margin goes `ink` while the subject stays `dim`, brighter fact than thing. Memory, spend, search and home paint subjects `ink`. | `palette.go:837-841`; tasks row painter |
| 9 | hierarchy | **Lit headings.** Memory's head `40 held · 1 shelves` is `ink` (tier 1) and its section `shelves · biggest first` is `muted`; tasks' head `tasks · 200 pieces of work` is `muted` and repeats the page name the bar already bands. Standing, spend and search headings are `dim`; home's panel headings are `muted`. | `memoryplace.go:419`,`:426`; `tasksplace.go:1260` |
| 10 | hierarchy | **Bold outside the band.** Search bolds the matched word on every row (`Add a **report**-only mode`); the tab bar is the only other bold at rest. | `searchplace.go:276` |
| 11 | hierarchy | **A second accent on settings.** The section chip `Session` is `accent` + bold + selected ground, on the same frame as the cursor's accent `›`. And the section bar is followed by a second full-width rule (row 5) — a place drawing chrome. | `place_settings.go:56-58` |
| 12 | component | **Five left edges.** Body text starts at column 0 (tasks, memory, spend), 1 (search, every teaching paragraph, home headings), 2 (standing's heading and shelves, settings' chips). Home's rows put a 2-cell mark gutter after a 1-cell lead. | tag dumps, rows 4–8 |
| 13 | component | **Three cursor marks.** Tasks, standing and settings lead the cursor row with an accent `›`; search leads *every* row with a dim `›`; memory, spend and home lead with nothing and let the ground speak. | `palette.go:879`; search row painter |
| 14 | hierarchy | **Memory says `1 shelves`.** The count clause never singularizes. | `memoryplace.go:476` |
| 15 | hierarchy | **Section air is missing on standing.** `in this conversation`, `for this project`, `everywhere` sit directly under the rows above them with no blank line; screen 2a's rhythm is one blank line before every section heading. | `standingplace.go` `standingLines` |
| 16 | focus | Every list place opens with its cursor on its first stop, which is its primary row — except **spend**, whose first stop is the budget line `today $7.50 of $500 · /budget sets the limits`; the rows that are doors (`what it was for`) start eight rows lower. | `spendplace.go:395` |

## Each surface, row by row

`above` is the rows before the body; `below` is the rows after the body's last
row at 80×24 (clearance blank, rule, note, box, hint). The rule row is the same
at 120×45 and 180×45 shifted by the height.

| Surface | above | below | rule row 80×24 / 45 | with nothing in it | tiers: heading · subject · note · margin | components | hover | click | jumps |
|---|---|---|---|---|---|---|---|---|---|
| **tasks** | 4 | 5 full, 4 empty | 20 / 41 full · 21 / 42 empty | 4 dim lines: `tasks is the history of work this machine has run.` … `no tasks yet — /task <brief> starts one` (`tasksplace.go:1914`). A machine with conversations but no tasks lists the conversations instead (`tasks · 2 chats`). | head `muted` with the page name; section `finished today` `dim`; subject **`muted`**; note in the margin `dim` (`1h · it came home clean`) | head + window control · sections · rows `› ✓ title … age · outcome` · fold `▸`/`▾` families · note line under the rule · hint | cursor step + accent `·`; cursor never moves | one click opens (card) | rule moves when the first row lands (note appears) |
| **spend** | 4 | 4 | 21 / 42 | 5-row paragraph ending `nothing spent yet — the first model call writes a line here.` (`place_spend.go:539`) | budget row `dim`; head `dim` with `money`; sections `dim`; subject `ink`; facts `dim`; money `money` | budget row · head + window · sparkline + axis · loudest-day row · 2 sections of `· subject … $` rows · `▸ 11 more` | **selected** step | moves only | — |
| **standing** | 4 | 4 | 21 / 42 | 3 lines, second runs off the frame | head `dim` at col 2; shelves `dim` with no air; subject **`dim`**, bold `ink` on the cursor; margin `dim`, `ink` on cursor/hover | head + window · shelf headings · overlay rows `› ◦ subject   rope · cadence` | cursor step + accent `·`; margin lifts, subject does not | moves only | — |
| **memory** | 4 | 4 | 21 / 42 | 5 lines incl. `nothing learned yet …` and `nothing here is a setting, all of it is editable`; teaching stays up to 7 lines | head **`ink`**; section **`muted`**; shelf `muted`; subject `ink`; facts `dim`; age `dim` right | head · section + kind legend · shelf `▾ you · 40 · …` · rows `· subject · facts … age` · `▸ 37 more, on this shelf` | **selected** step | moves only | prose → header swap at 8 lines |
| **search** | 4 | 4 | 21 / 42 | 4 dim lines ending `type words you remember — …` | no heading; subject `ink`; snippet `dim` with **bold** match; age `dim` right | rows ` › chat · snippet … age` | **selected** step | moves only | — |
| **settings** | 4 | 5 | 20 / 41 | never empty | section chips: current **`accent`** bold on selected ground; subject `dim`, bold `ink` on cursor; value `dim`/`ink` | chip bar + **its own rule** · rows `› name … value` · description under the cursor | cursor step + accent `·` | select, then activate | — |
| **task room** (lane K) | 4 (pulse · strip · crumb · titled rule), no blank | 3 (legend rule · steer box · status) | 20 | — | — | — | — | — | head is 4 but spells a crumb row and a titled rule where a place has a rule and a blank |
| **conversation** (lane K) | 4 (pulse · strip · rule · blank) | 4 (legend rule · box · blank · status line) | 20 / 41 | — | — | — | — | — | — |

The 180-column frames add one defect of their own: standing's margin
(`asks first · Mondays at 9am`) is drawn at column 75 rather than flushed right,
because `overlayFill` places the note after the label rather than against the
edge; every other place flushes its margin.

## What home already does, which is the spelling the places take

Lane G's grid (`homecell.go`) is the reference neighbour, and where it and
screen 2a disagree the places follow home so that `tab` crosses no seam:

- heading: the section word, lowercase, `muted`, one blank row above;
- row: a 2-cell mark gutter (a mark or blank), the subject in `ink`, the facts
  `dim`, the margin `dim` and right-flushed;
- the cursor row and the hovered row are ONE step — the cursor ground, the
  subject bold, the facts lifted to `ink` — with no accent mark in the gutter;
- an empty region keeps its heading and one dim whisper;
- a fold line opens on one click, a row is selected by the first click and
  opened by the second.

Screen 2a paints section headings `dim`; DESIGN-LANGUAGE's accent budget and
home paint them `muted`. The places follow home, and that disagreement is the
design's to settle, not a lane's.

## Owed to other lanes

- **Lane K**: the task room's head is pulse, strip, crumb, titled rule — four
  rows, but not the place's four (no blank under the rule), so its body starts
  one row higher than a place's. The conversation's foot keeps a blank row
  between the box and the status line that no place has.
- **Lane G**: home's headings are `muted` where screen 2a says `dim` (above).
