# The factory

## /factory — the floor

The **factory** is a place, like home, activity and spend: the floor where work a chat splits
off, or that a connected repository sends, stands in rows grouped by where each item is. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), the map (`alt+.`), or the `Factory` button on the
tab bar, third after `Home` and `Chats`. `esc` goes back to the conversation.

On this machine the page reads your own factory floor: the items you made with `n` on the page
or added from a chat, kept under the codeaf home (see where factory items are saved). Until the
first one arrives the floor draws one dim line under the handover,
`work arrives here from chat, from n, and from the repositories you connect`, and `n new` still
works. Only when that floor cannot be opened, or over `--host` or `--at` to another machine,
does the page draw
`nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected`.
The only key it offers then is `esc back`.

The floor is one layout in three widths:

- **120 columns and wider**: the handover runs across the whole width at the top (one line, or
  four rows after `h`; see the handover), then a blank row, then two columns: the rows on the left (58% of the width to start with, never under 70
  columns), a dim `│`, and the **peek** on the right (never under 40 columns), which shows the
  item under the cursor. The divider moves (see resize the split). With no item under the
  cursor (an empty floor, or a filter that matches nothing) there is no peek and no `│`: the rows
  take the whole width.
- **90 to 119 columns**: the handover on top, then the rows across the full width, with no peek.
  Press `enter` on a row to see the item whole.
- **Under 90 columns**: no handover; each row is only its mark, its short name and its title.

At every width the rows, the peek and the item page stop 2 columns short of the right edge, as
they start 2 columns in from the left.

The rows window follows the cursor when they do not all fit. With the cursor on the first item
the list is back at its top, its first group heading showing. A group heading moves with its
first row: walking onto a group's first item shows its heading above it, and a heading is never
left alone on the last row with its rows below the edge.

## the keys on the factory floor

The bottom line names the keys that work for the item under the cursor. On the still made-up
floor, which has no verbs, it names the walking keys:
`↑↓ walk · enter open · space mark · / filter · [ ] repo · A backlog · z density · O order · priority · h handover · E recipe · esc back`.
On a floor that can be changed it names `enter open` and the item's own keys first (see the
factory's verbs), and when the line is too long the list keys are the first to go, then the
keys about the whole floor (`n new item`, `L launch marked`, `T talk`, `u read again`,
`g github`, `d hide`, the stage keys), so the item's own knobs, `t gate · c cap · e effort`,
still show at 120 columns.

- `↑` and `↓` (or `ctrl+p`, `ctrl+n`, the wheel) walk the items. A click selects a row, a second
  click opens it; a click in the peek moves nothing.
- `enter` opens the item on its own page (see the item page).
- `J` and `K` scroll the peek's description, `pgdn` and `pgup` a page.
- `{` and `}` move the divider, `|` puts it back (see resize the split).
- `h` switches the handover between one line (the default) and four rows; remembered.
- `z` switches compact rows (one line each, the default) and comfortable rows (a long title on
  two lines, a dim line with the factory's read, a blank row between items). Not saved.
- `O` re-sorts the rows inside each group (see order the floor).
- `g` opens the item on github (see open the item on github).
- `u` reads the item again; `U` reads every item again, after asking (see when the floor is
  doing something).
- `space` marks or unmarks a new item, and pauses or resumes a running one. `T` opens the
  item's own conversation.
- `m` opens the foreman, the floor's own conversation for what to take first (see the foreman).
- `/` filters, `[` and `]` show one repository at a time, `A` shows the whole backlog.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.
- `R` repos, `E` recipe and `$` rail are named last and are the first to go.

## the peek's keys and the bottom line — one list of keys

The peek's last row and the bottom line are **one list of keys at two widths**: the same keys,
in the same order, `enter open` first, then the item's own verbs, then `T talk`, then what shapes
a run (stages, gate, cap, effort), then `d hide`, `g github`, `u read again` and `n new item`.
When either line is too narrow it drops keys from the end of that list, so the two always lose
the same keys in the same order, and a key is spelled one way on both (`e sign off with
changes`). The floor's own keys (`/ filter`, `A backlog`, `z density` and the rest) follow on
the bottom line only, and go first when it is too long.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. Rows stand on one grid so they line up down the page: a state mark,
the **priority** in one cell, the short name right-aligned so the numbers line up by digit
(`#1538`, or `ci`), the title, the repository's short name, the facts, and at the far right how
long ago the item last moved (`12m`, `7h`). Under 90 columns a row is the mark, the priority, the
short name and the title.

The priority is the cheap read's guess at what to take first: `▇` first (in the accent), `▅`
second, `▃` third, `▁` fourth, dim; nothing for an item nobody ranked. A patched font draws
chevrons instead; a screen reader hears `!`, `:` and `.`. While the item is being read the cell
is a spinner instead. During a whole-floor re-read (`U`) only the row being read right now spins;
a row still waiting its turn shows a still dim `·` in that cell and `waiting to read`.

On a forge item the short name is a link to its page where the terminal takes links.

Groups have muted capital headings (`NEEDS YOU · 1 ───`) and one blank row between them, in
this order: `NEEDS YOU` (the question in its colour, `[y/n]` before the age), `LANDED`
(`3✓ 1✕`, waiting for your sign-off, so it sits beside what waits on you, above the backlog),
`STREAMS` (a stage strip `●●◐○` and `review 26m`, or `queued`), `NEW` (`bug · S`,
`pr · ci ✓`, `ci red`) and `SHIPPED` (`shipped 06:00`). An empty group is not drawn.

After the first fact come the money (`$1.42/$5` spent, in money's green, or `~$3` estimated,
muted like the other facts, because it is a guess), the author, tags (`thin`, `dup #950?`,
`from chat ▸`, `terminal only`). While the item is being read, `reading…` or `refreshing…` (or
`waiting to read`) stands right after the state fact in place of the money, author and tags, and it
is the first fact a narrow row drops.
The title keeps at least 28 columns: when a row is too narrow for the title and every fact, the
facts drop as a column, the same fact from every row at once, the rightmost first and the state
fact last, so a fact missing from a row means the row has none, never that it ran out of room.
A long title then takes the width the facts leave. In the comfortable density (`z`) the title's
second line is as wide as its first. The row under the cursor sits on the same lifted background the
Chats list and the Teams page use.

## paused, stopped and skipped — what the marks mean

Each mark on the floor means one thing, so a held, ended or skipped stage never looks like one
that is running, failed or still to come:

- **Paused** (`space` on a running item): the row's lead mark and the held stage are a pause
  mark (`=` on a plain font, a pause icon on a patched one), muted. The row says `paused 13m`,
  counted from when the floor first saw it paused; the peek says `paused 13m · write ×3` and
  never `13m left` or what it was editing. The item page's head says `paused 13m`.
- **Stopped** (`x`): the item is new again and its lead mark is a stop square (`■`, or a stop
  icon), muted. The row says `stopped · branch kept`, the stage it stopped on wears the square
  with `· stopped`, and the line under the peek's stages says `test · stopped · branch kept`,
  never `✕` or the failure the stage was in the middle of.
- **Skipped**: a stage switched off, one whose condition does not fit (`when thin`), or one the
  run went on past, is a dim stroke (`–`, or a minus icon; `-` for a screen reader) with
  `· skipped`, never the `○` a stage still to come wears. On a shipped item a stage plan
  skipped reads `– neaten · skipped`.
- **Waiting to read**: during `U`, a row the read has not reached yet shows a still dim `·` and
  `waiting to read`; only the row being read spins.

## filter the factory floor, search for an item

`/` on the factory page opens a one-line filter box at the top of the list, and the list narrows
as you type. Every word you type has to hold. A plain word matches anywhere in an item's title,
repository, area, kind or author. These words mean something more:

- `risky`: any risk the triage read named (`touches money`, `has ui`), or a large item. `cheap`: estimated at $2.50 or less.
- `strangers`: written by someone outside the project. `mine`: written by you.
- `spend`: about spend, billing, the ledger or caps. `ui`: in the tui, pages or render areas.
- `prs`: pull requests. `bugs`: bugs. `thin`: too underspecified to run without questions.

`enter` keeps the words and closes the box; `backspace` takes a letter back; `esc` throws the
words away. With the box closed, the words stay under the repository line, and `esc` clears
them before it leaves the page. When nothing matches, the list says
`no item on the floor matches`.

## show one repository on the factory floor

The first line of the list says which repositories it shows: `all repos`, or one repository's
short name and how many items it has, such as `codeaf · 7`. `]` steps to the next repository and
`[` to the one before, and past the last one the list shows all of them again. An item shows
under a repository it arrived on or touches. That line, and the filter's words under it, stay at
the top while the rows scroll. `esc` goes back to all repositories before it
leaves the page.

## older new items, the backlog, and A

The `new` group shows only what arrived in the last three days. Older open items are kept back,
and a dim line at the end of the group says how many, such as `12 older open items behind A`.
`A` shows the whole backlog under `new`; `A` again hides the older ones. Dismissed items are
never drawn.

## mark new items

`space` on a new item marks it, and its mark turns to an accent dot; `space` again unmarks it.
Only new items can be marked. The marks are kept through every re-read
and when you leave the page and come back, until codeaf exits. `L` with marks asks first, with
the triage estimates summed: `launch 3 marked · ~$7? [y] go · [n] not now`. `y` launches every
marked item, clears the marks and says `launched 3`; `n` or `esc` keeps them. With nothing
marked `L` launches the item under the cursor. Marks the foreman
makes (`m`) are kept with the floor on this machine and are still there after a restart.

## stages, not a pipeline

An item runs through **stages** in a fixed order that a repository or team writes once: for
example plan, write, test, review, neaten, proof. Each stage is one sentence ("read it as a
stranger would"), and it runs as an ordinary task with the ordinary crew picked for that kind of
work. There is no graph to draw and no model to choose per stage; the one word a stage may carry
is its effort, cheap or strong.

A stage can repeat until a condition holds (until green, until clean, until proven) up to a
number of rounds, and then it stops and asks you. A second round fixes what the first found, then
checks again: its brief names what the last round found and says to fix it in the checkout, run
the tests, and look again, so a review that found one thing does not just find it twice. Nothing inside a stage can add a stage, except plan, within the
bounds its recipe word allows (see adapt). A stage can also be a gate: `plan` comes back with the plan before any code, and `ship` waits for your sign-off.

The peek shows a new item's stages as one line of names and a running item's as a strip of
marks; the item page lists them down its stage rail. A stage can carry a condition (`thin`,
`large`, `touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim with the
skip stroke `–` and `· skipped` on the stage rail, and `skipped · not thin` (or the condition it
missed) beside it. A stage switched off, or one the run went on past, is drawn the same way.

On a new item, `1` to `9` switch the stage with that number on or off, `s` adds a stage in
words (`after review, make it neater` puts it after review), and `b` banks the item's stages
as its repository's recipe, into `.codeaf/factory.md` (see the recipe file). A new item starts
from its repository's recipe file, or codeaf's default when there is none. A run keeps the
stages it launched with.

## the recipe file — .codeaf/factory.md

A repository's recipe lives in that repository, in `.codeaf/factory.md`. A person edits it by
hand and the floor writes it back. A repository with no file runs codeaf's default recipe. The
shape (indented here; in the file each line starts at the margin):

    # factory recipe · codeaf

    ## issue
    1. plan · chat · read the issue and say how · gate plan when large
    2. write · chat · fanout 3
    3. test · check · go test ./... · until clean · max 2
    4. review · chat · read it as a stranger would · until clean · max 2
    5. security · chat · when touches auth
    6. proof · chat · show each claim in its own medium

    ## pr
    1. read · chat · what changed and why
    2. checks · check · ci
    3. review · chat · until clean · max 2 · fanout per finding
    4. proof

    ## policy
    - tests pass before anything posts
    - a stranger's PR never runs write

    ## habits
    - factory PRs from your own issues self-ship when the proof is green

A stage line is `N. name · kind · ask · knob · knob`. The sections are `## issue`, `## pr`,
`## ci` and `## chore`; a missing section runs the default for that kind. A kind's heading may
end in one word, `## issue · adapt`, `## issue · ask` or `## issue · fixed`, saying how much plan
may change its stages (see adapt); no word is `adapt`. `## policy` and
`## habits` are sentences kept word for word. A line codeaf cannot read is named with its line
number and why, and the rest of the file still loads.

**The recipe is read from your own checkout of the trunk, never from an item's branch**, so a
stranger's pull request cannot rewrite the policy it is held to. Policy and habit lines are
applied by codeaf's own code; they are never handed to the model as instructions.

`b` on a new item writes its stages into its kind's section of this file and leaves every other
section as written; banking a habit adds a `- ` line under `## habits`. Both are offered only when
codeaf knows where the repository is checked out; otherwise `b` is not on the bottom line.

codeaf reads the file for the repository you opened it in, and for any watched GitHub
repository whose checkout it has seen you open. Open codeaf inside a repository you watch once
and its recipe is read from then on.

## recipe file knobs — kind, when, until, max, fanout, gate, effort, proof, off

In a `.codeaf/factory.md` stage line, after the name:

- **kind**: `chat`, `check`, `gate` or `post`; `chat` when absent.
- **ask**: the first segment that is not a kind and not a knob. `check` runs it as a command.
- `when always`, `when thin`, `when large`, `when touches auth`, `when has ui`: the stage is
  skipped on an item the condition does not fit.
- `until done`, `until clean`, `until proven`, `until green`, and no other word. done: the stage
  reported done. clean: it reported zero findings. proven: every claim on the item has evidence.
  green: the check's command exited 0.
- `max N` (or `rounds N`): rounds before it stops and asks you.
- `fanout 3`, `fanout one`, `fanout per finding`, `fanout per file`, `fanout per claim`.
- `gate plan`, `gate ship`, `gate none`; `gate plan when large` makes only the gate conditional:
  the stage runs on every item and stops for you only on a large one. It leaves the stage's own
  `when` alone, so `when thin · gate plan when large` is a stage for thin items that asks first
  when one is also large.
- `effort cheap` or `effort strong`.
- `proof a test, a screenshot`: what the stage must show, separated by commas.
- `off`: the stage is written but does not run. Every other stage is on.

A line with only a name, such as `4. proof`, is codeaf's default stage of that name for that
kind; a name with knobs and no ask is that default with the knobs laid over it. A knob word
with a word it does not know (`until clen`) is named as a problem, never read as the ask.

## adapt — how much plan may change the stages

Three things change what an item runs: a stage's `when` (read off the item, no judgment), the
plan stage (judgment, within bounds), and you. The recipe gives each kind one word after its
heading in `.codeaf/factory.md`:

- `## issue · adapt`: plan may add, switch on and skip stages within the bounds. The default
  when there is no word.
- `## issue · ask`: the same, and the item's gate becomes `plan`, so you ratify the change
  before anything after plan runs.
- `## issue · fixed`: plan may not change the stages. It is refused with
  `the recipe for issue is fixed; plan may not change the stages`.

The bounds are held by codeaf's own code, not asked of the model. Plan may add only a
conversation stage with an ask, placed by a time word (`after test, …`) or after review. It may
never skip a gate stage, the stage named `proof`, or a stage a policy line names
(`tests pass before anything posts` names test). It may never touch a stage that is done or
running, nor add one before it. It has no way to change the cap, the gate, the effort or the
rounds. One refused part refuses the whole change.

What plan changed is kept on the item and drawn as one dim line under the stages on the peek and
in the item page's head, `plan added security · skipped neaten · why: touches billing`. An item
plan never changed draws nothing there.

No run calls plan on your own floor yet, so today only the made-up floor
(`CODEAF_FACTORY_FIXTURE=1`) shows the line, on its shipped item.

## the handover

The **handover** is what happened on the factory floor since you last looked. By default it is
one line across the top of the floor, then a blank row:

`◆ 2 shipped · 3 arrived · ? 5 waiting · $8.44 / $60 · polled 14s ago`

what shipped and arrived this stretch, what waits on you (in the question colour), the day's
spend against the day's rail, and when a source was last read. **Waiting counts the items that
ask a question and the landed items waiting for your sign-off,** so `? 2 waiting` is one
question and one proof sheet; the full strip's `? 2 waiting on you` and the tab bar's `? 2`
count the same, and `nothing waits on you` means neither. A part whose count is zero is
left off; a narrow window drops parts from the right. While a source is being read the last part
says `github · ⠋ polling`, and while every item is read again, `⠋ refreshing 8 items · 3 done`.

`h` switches to the four-row strip and back, and the choice is remembered in `factory.json` in
the codeaf home, beside the split:

1. `◆ handover · since 23:12 · 7h 12m · $8.44`, with a line out to the edge.
2. `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`, or
   `quiet · nothing happened while you were away`.
3. `? 2 waiting on you` or `nothing waits on you`, and at the right a `24h` sparkline.
4. `3 repos · github · chat · benches 2/6 · polled 4m ago`, and `$11.31 / $60 today` at the
   right. The made-up moving floor adds its clock's speed, `· 150×`.

The handover is drawn at 90 columns and wider, when the window leaves the rows six lines.

## the peek

At 120 columns and wider, the right column shows the item under the cursor as a short document:
blocks with one blank row between each, and a block with nothing to say is left out. Its title
stands on one fixed row, level with the first group heading when the list is at its top, and
stays there while the rows scroll under the cursor, so the peek keeps its whole height. In order:

1. The short name and title (the short name is a link to the item's page), then dim: repository,
   kind, size, author, age, and `github ↗` when the item has a page there.
2. For an item that needs you, the question exactly as the run asked it (`?` in amber, the
   words in ink) and, where the floor can answer, `[y] yes · [n] no · [a] in words`.
3. The factory's one-sentence read, or `⠋ reading…` while the first read is out.
4. Facts, dim: `touches money`, `maybe a duplicate of #7`, `thin`, `stranger`.
5. The gate, cap and effort in fixed slots, so their values line up from item to item: `gate  ship`,
   `cap  $5`, `effort  —`. A cap of nothing is a blank slot.
6. The stages: `●` done, `◐` running, `?` waiting on you, `✕` failed, `○` to come, the pause
   mark held, the stop square where a person stopped it, and `–` skipped, every cell of the
   strip one width (its longest name, at most 16 columns; a longer name ends in `…`), with the
   round as `2/2` on a stage that may run more than one. When the strip does not fit, whole
   cells drop and a dim `…` stands where they were; the running stage always stays. Under them,
   dim, why a stage stopped: `review · review is not clean after 2 rounds`, or
   `test · stopped · branch kept`.
7. A running stage's line (`review 1/2 · 3 findings · fixing`), a paused one's
   `paused 13m · write ×3`, what frees a queued item, or `merged 06:00 · $1.90`.
8. The description, rendered from Markdown (headings, lists, code, links as their words, never
   raw backticks), 60 columns wide, at most six rows, cut with `…` and `▾ more`. A landed item
   shows its claims here instead.
9. `talk`, when the item has its own conversation.
10. What the forge says, each block absent when empty: `comments` (the last three, `author · 2h`
    then the words), `checks` (one row per check: mark, name, state), `files` (`+218 −44 · 6
    files`, then up to six paths), `activity` (the last five events). The item's page on GitHub is the `github ↗` link in the meta row.

The bottom row names the keys, starting with `enter open`, with one blank row above it, and
only keys the floor can do: a still floor with no verbs says just `enter open`. It is the same
list as the bottom line, in the same order, cut from its end to the peek's width (see the keys
on the factory floor).

## resize the split — { } |

The divider between the floor's rows and the peek can be moved at 120 columns and wider. `{`
moves it 4 columns left (a wider peek), `}` 4 columns right (wider rows), and `|` puts it back at
58% of the width. You can also drag the divider with the mouse: press on the `│`, move, let go.

The rows never get narrower than 70 columns and the peek never narrower than 40; a key or a drag
past either stops there. The position is remembered as a share of the width in `factory.json`
in the codeaf home, beside `config.json`, so it comes back on the next launch and keeps its
proportion on a narrower or wider terminal. Under 120 columns there is no peek and the keys do
nothing.

## read the whole issue — J K on the peek, the issue row on the item page

The peek shows at most six rows of an item's description. When there is more, its last row ends
in `…` with a dim `▾ more` at the right. `J` scrolls it down a row and `K` up a row; `pgdn` and
`pgup` move a page. Moving to another item starts that item at its top.

To read everything, open the item (`enter`, or click its row twice). The item page's rail starts
with `issue`, and its pane is the whole description rendered from Markdown at 72 columns, then
`read 3m ago · u again` (or `⠋ reading · 4s` while a read is out, `waiting to read` while it
waits its turn, `not read yet · u reads it` before the first read, and `u reads it again` when
the time of the read was not kept), the factory's read, the
facts, for a thin item the questions it would ask the author, what plan changed about the
stages, and then the comments (each whole), checks, files, activity and links. `J`, `K`, `pgdn`
and `pgup` scroll it, and its bottom row says `J K scroll` while there is more to see.

## the item page

`enter` on a row of the factory floor (or a second click on it) opens that item on its own page,
across the full width; `esc` closes it and puts the cursor back on the same row. Nothing about
the floor (filter, repository, marks) changes while it is open.

The top row is a trail: `Factory › codeaf › #1551 filters lost on compact`, the crumbs dim and
the item in ink, with where it stands and for how long (`running 26m`) and the spend over the
cap (`$1.42 / $5`) at the right. `esc` climbs one crumb, back to the floor. The second row is
the gate, cap and effort with their keys, `gate  ship [t]  cap  $5 [c]  effort  — [e]`. The
repository is the crumb above and is not said again; an item that touches more repositories
names the others, dim, `also harness, agentfield`;
on an item waiting on you it is the question instead (see answering a question). Then a blank
row; the rail and its pane start on the fourth row on every item. What plan changed about the
stages is in the issue pane (see adapt).

On the left is the **rail**: `issue` first (see read the whole issue), then `talk` when the item
has its own conversation, then one row per stage, then `proof` when the item has a sheet and no
stage is named proof, then `log` once the run has said anything (see the log). A stage reads `●`
done (with `×3` when it split into tasks), the braille spinner while it runs (with its round,
`review 2/2`, and how long it has run, `4m`), `?` waiting on you, `✕` failed, `○` to come, the
pause mark with `· paused` while it is held, the stop square with `· stopped` where a person
stopped it, and a stage switched off, one whose condition does not fit, or one the run went on
past dim with `–` and `· skipped` (see paused, stopped and skipped); a stage
with a conversation has its mark after the name (see a stage is a room). The page opens on the stage waiting on you, else
the running stage, else `proof` for a landed item, else `issue`. `↑` and `↓` (or `←` and `→`)
walk it, and a click on a rail row selects it. Under 72 columns the rail is one line above the
pane.

On the right is the row under the cursor. For a stage: its settings, dim
(`review · chat · until clean · max 2 · fanout per-finding · when always`), what it is asked to
do, a blank row, what it has to say (`runs after test`, its round and its newest log lines, the
question waiting on you, its result, why it failed, or the sheet on `proof`), a blank row, and
the item's keys on the last row.

Every verb key keeps working on the item while its page is open (see the factory's verbs), and
`1` to `9` switch stages. The hint line names them, after `↑↓ stages` (and `enter conversation`
on a stage that has one), and ends with `esc floor`. A box that takes words opens on the last
row of the pane.

## a stage is a room — enter on a stage

A stage that ran as a conversation (plan, write, review: see a stage is a conversation) is a
room you can walk into. On the item page its rail row has the conversation's mark after its
name, the trail reads `Factory › codeaf › #12 › review`, and the hint says `enter conversation`.
`enter` opens it the way `T` opens the item's own conversation: you land in it, the one you were
in goes on running behind, and the first `esc` on its empty box, with nothing running, comes
back to the item page on the same stage. While it opens the line above the keys says
`⠋ opening #12 › review…`.

A stage with no room says why on the pane's last row instead, and opens nothing:
`review has not started`, `review has no conversation yet · it opens when a round ends` (a
round's conversation is kept once the round ends), `test is a check · its log is below`,
`plan is your answer · it has no conversation` for a gate, `post is a write to github · it has
no conversation`, `neaten is off on this item`, `neaten is skipped on this item`. The next key
or a move of the cursor puts the keys back.

## the log

Once a run has said anything, the item page's rail ends with `log`. Its pane is the run's log,
oldest at the top and the newest at the bottom, as many of the last lines as fit; each line
starts with its time, `12:04`, dim at the margin, then a mark for its kind and its words. Three
voices: a **thought** (the run thinking aloud, a round starting again) is dim; **your own
words**, the `steer: …` lines `S` and the conversation leave, are ink; everything else (what a
stage said, a test, a failure `✕`, a success `✓`, a question `?` in amber) is the quieter
second voice. A running stage's pane shows the same lines under its round. A claim a stage made
that is not for the proof sheet is a `claimed: …` line, and a batch of reads handed to a quick
task is one line, `read ×4 · find handed to quick task 1`. An item that never ran has no log
row. The log keeps the last 400 lines.

## answering a question — y, n, a

A run stops and asks when it cannot go on alone, and the item moves to **needs you**. The
question is the run's own sentence, drawn as is in the peek (under the title) and on the item
page's second row, its `?` amber: `plan is ready · go, or change it?`,
`review is not clean after 2 rounds: 3 findings · one more round, or go on as is?` (one round
is `after 1 round`, one finding `1 finding`),
`cap of $5 reached · $5 more, or stop?`, `review did not finish: … · skip it, or stop?`.

- `y` answers yes: go on, one more round, $5 more, skip it. The bottom line says
  `answered #12 · yes`.
- `n` answers no: for a plan or a cap that stops the item; for rounds it goes on as is. It says
  `answered #12 · no`.
- `a` opens `answer ›` for words (`go, but keep the old flag`); they reach the next round's
  brief and it says `answered #12 in words`.

`[y] yes · [n] no · [a] in words` is drawn only where the floor can answer. `y`, or `a`, on an
item that is not waiting says `#12 is not waiting on you` on the bottom line and changes nothing
(`n` there is new work, and `a` on a thin new item asks its author).

## sign off, send back, check again — s, B, v

A landed item's page opens on its **proof** sheet: one row per claim and policy row, `✓` shown or
`✕` with `— not shown`, its evidence dim at the right and its medium (`test`, `screenshot`,
`policy`) as a chip in its own column at the right edge. The evidence never repeats the medium:
a test row reads `0.3s` beside `test`, not `test · 0.3s`. The sheet's last line counts it and names its keys:
`all 6 shown · s sign off`, or `1 of 6 not shown · e sign off with changes · B send back`.

- `s` signs off and ships it: `#12 shipped`. On a sheet with a row not shown the run refuses,
  in its words: `#12 has a claim not shown`.
- `e` signs off saying you changed something first, the one that ships a sheet with a row not
  shown: `#12 shipped with changes`. It does not count toward a habit.
- `B` opens `send back ›` for what to prove (`prove restart survival`); the item goes back on a
  bench with one more stage, `prove`, and says `#12 sent back · prove restart survival`.
- `v` runs every check stage again and puts what they show on the sheet:
  `#12 is being checked again`.
- `enter` on the sheet is the default: sign off when every row was shown, and when any was not,
  **send back** (`enter send back`), with `prove` and that row's words already typed.

`d` says `the diff is the appendix` and that it opens in your editor later. Each key is named only
where the floor can do it.

## what goes on the proof sheet — claims, evidence, repeats

The proof sheet holds what was shown, not what was said. Only the **proof** stage (and a
send-back's `prove`) and the **check** and **post** stages put rows on it; what plan, write,
test or review claim goes to the log as `claimed: …` lines instead. One claim is one row: the
same words again, in any case and with any punctuation (`Refunds count once.` and
`refunds count once`), update the row rather than add one. A claim given with no evidence is
not shown: it reads `✕` with `no evidence given`, whatever it said of itself. A later claim
with evidence replaces an earlier one without, and a later one without evidence never undoes
an earlier one that had some. Policy rows (`policy`) are kept the same way.

## steer a running item — S

`S` on a running, queued or waiting item opens `steer ›`: words handed to the round running now
and to every round after it (`keep the old flag`). The bottom line says
`steered #12`, and the log shows `steer: …` in ink. `space` pauses a running item (`#12 paused`)
and resumes it (`#12 resumed`); a round cut by the pause starts over. `x` stops it and keeps the
branch: `#12 stopped · branch kept`, and the item is new again. On an item that cannot be
steered, `S` is the made-up floor's `S sleep 8h`.

## the factory's verbs, keys that change an item

The bottom line names only the keys that work for the item under the cursor. On your own floor
the keys that change an item's own words always work (`n`, `t` gate, `c` cap, `e` effort, `w`,
`1-9` and `s` stages, `space`, `d`), and `r`, `p` and `L` launch (see running an item). Stop,
pause, answers, steering, sign-off, send back and check again work in every window too, whichever
process runs the floor's items. `enter` on a row never launches; it opens the item page.

- **New:** `r run · p plan first · space mark · T talk · L launch marked · 1-9 stages ·
  s stage · t gate · c cap · e effort · d hide · n new item`. `r` launches with the ship gate, `p` with the
  plan gate; the bottom line says `#12 is running` once the floor's next read sees it start, and
  `#12 is queued · a bench frees it` only when that read still finds it queued behind full
  benches. `L` launches this item, or asks first about the marked ones (see mark new
  items). `t` cycles the gate plan, ship, none; `c` the
  cap $2, $5, $8, $15, $30; `e` the first stage's effort: none, cheap, strong. `w` opens
  `in words ›` for the gate, cap and effort at once (`$8, plan first, stronger`). On a
  terminal-made item `g` puts it on github too, or takes it off; on any other item with a page
  on github, `g` opens that page. `a` asks a thin item's author its questions. `d` hides the item.
- **Running or queued:** `S steer · space pause · x stop · T talk · e effort` (see steer a
  running item); `e` cycles the running stage's effort.
- **Needs you:** `y n answer · a in words · S steer · x stop · T talk` (see answering a
  question).
- **Landed:** `s sign off` on a clean sheet, `e sign off with changes` when a row was not shown
  (spelled the same in the peek and on the bottom line), `B send back`, `v check again`,
  `T talk`, `d diff` (see sign off, send back, check again).
- **Any state:** `T talk` opens the item's own conversation (see talk it through), on the
  floor and on the item page alike; it is named only where the floor can make one.
- **Anywhere:** `n new item` opens `new work ›`, at the bottom of the rows column, on the repository the list shows (or the first one;
  on a floor with no items yet, the name of the folder this window was opened in), and the new
  item's card is under the cursor when it is made. On the made-up moving floor,
  `S sleep 8h` jumps its clock eight hours and starts a new handover.

A key that takes words opens a one-line box at the bottom of the right column, just above the
peek's key line (which stays the column's last row, one blank row between them), except `n`'s,
which stands at the bottom of the rows (and every box stands under the rows when there is no
peek): `enter` sends, `backspace` edits, `esc` cancels. After a key the bottom line says what
became of the item (`#12 is running`, `#12 stopped · branch kept`); a refusal, such as
`#1540 is on a bench; stop it first`, is said there in the floor's own words instead.

## open the item on github — g

An item that came from GitHub has a page there, and the floor offers it three ways: its short
name on the row and on the peek is a link (where the terminal takes links, it opens on click,
and takes no extra room), the peek's dim meta row ends `github ↗`, and `g` on the item opens the
page in your browser. The bottom line says `g github` where it works. After `g` the bottom line
says `opened #1662 on github`; on a machine with no browser it says
`could not open your browser` with the address to copy.

`g` is offered only when the floor can name the page; with no such door there is no key. On an
item typed into the terminal with `n`, which has no page on GitHub, `g` keeps its other meaning:
it puts the item on GitHub too, or takes it off.

## when the floor is doing something — the spinner

Everything the floor does in the background says it is happening, with the same braille spinner
the transcript uses (`⠋`), and nothing is drawn when nothing is in flight:

- **An item being read:** its priority cell spins and the fact after its state says `reading…`
  or `refreshing…`. An item waiting its turn in a whole-floor re-read does not spin: it shows a
  still dim `·` and `waiting to read`. The peek's read says `⠋ reading…` while the first read is out, and the item
  page's read line says `⠋ reading · 4s`.
- **`u` reads the item under the cursor again.** The line above the keys says `re-reading #6…`
  until the read is over, then the bottom line says `#6 read again · ~$0.0004`.
- **`U` reads every item again, and asks first:** `re-read 8 items · ~$0.004? [y] go · [n] not
  now` stands at the bottom of the rows. `y` goes, `n` or `esc` does not. While it runs the
  handover says `⠋ refreshing 8 items · 3 done`.
- **A source being read:** the handover says `github · ⠋ polling`, then `polled 14s ago`.
- **A key waiting on its answer:** `T` says `⠋ opening #1's conversation…`, `b` says
  `⠋ banking…`, saving the repositories or the recipe says `⠋ saving…`, and `R` says
  `⠋ asking gh…`, each until the answer arrives.

`u` and `U` are offered only where the floor can read items again.

## bank a habit after clean sign-offs

After three sign-offs in a row without edits, the bottom of the right column offers a habit:
`habit forming — 3 sign-offs without edits on codeaf` and
`factory PRs from your own issues self-ship when the proof is green? [y] bank it · [n] not yet`.
`y` writes that sentence into the repository's habits, under `## habits` in its
`.codeaf/factory.md`; `n` puts the offer away. The offer comes only where banking can be written.

## from chat to the factory floor — factory_add

A conversation can offer a piece of work to the factory floor with the `factory_add` tool. The
chat calls it when you say something belongs on the factory floor, or when it judges a piece of
work is self-contained enough to run on its own later. It is there only when this conversation
has a factory floor behind it; without one the chat has no such tool and says it cannot. It is
there on every ordinary launch on this machine: plain `codeaf`, whose conversation runs in the
session host, as well as first-run setup, `--no-host` and `--debug`. A window that comes back to
a conversation while its card is still up is shown the card. It is absent over `--host` or `--at`
to another machine (that machine's floor is not the one your page reads), from `--once`, and
inside a task; add work there with `n` on your own floor.

Calling it adds nothing. A card asks you first, in one question: `put this on the factory floor?`.
Under it is the item drawn the way the floor will show it (the title with `repo · kind · size`
on the right, then `new` and the estimate with the stages it would run), then the chat's reason.
It answers to `1 add it`, `2 not now`, or words typed into its box
(`say what to change… (enter sends it)`).

- **`add it`** writes one item to the floor as `new`, from chat, and the chat is told
  `#<id> <title> is on the factory floor`. The card's foot says `added · #<id>` and its body
  becomes the item's live card (see the item card in a conversation).
- **`not now`** writes nothing: `nothing was added: the person said no.`
- **Words** write nothing either. The chat is told `the person changed it: <your words>` and
  `Nothing is on the floor yet. Propose it again with that`, and asks again with a new card.
- **No answer** writes nothing. There is no clock that adds: after fifteen minutes the card
  comes down with `nothing was added to the factory floor`, and the chat is told
  `nothing was added: the card was never answered`.

**Nothing launches from the chat.** An added item waits on the floor like any other `new` row
until you launch it from the factory page.

## teaching the recipe in conversation — factory_recipe, always run a security review in a repo

Say a rule out loud ("in my repo always run a security review when auth is touched",
"never post without green tests", "for PRs, two review rounds") and codeaf can offer it to
the repository's recipe, `.codeaf/factory.md`, with the `factory_recipe` tool. It is there
exactly where `factory_add` is, on the ordinary launch on your own machine, and absent over
`--host`, `--at`, from `--once` and inside a task.

It offers a single line: a stage for `issue`, `pr` or `ci` in the file's grammar
(`security · read it for auth holes · when touches auth`), a policy sentence, or a habit.
A stage line the file cannot read is refused before any card, with the reason. A stage of the
same name already in that section is replaced where it stands; otherwise it is added at the end.

**Nothing is written before `1`.** A card asks first, in one question:
`add this to <repo>'s recipe for <kind>?` (or `add this to <repo>'s policy?`,
`add this to <repo>'s habits?`). For a stage its body says `now:` with the stages that kind runs
today and `after:` with the new one marked `+`, then the line itself; for a policy or a habit,
the sentence; and `why:` with the reason when the chat gave one. It answers to
`1 bank it`, `2 not now`, or words (`say what to change… (enter sends it)`).

- **`bank it`** writes the line; codeaf is told `<line> is in <repo>'s recipe for <kind>`
  (or `… is in <repo>'s policy`, `… is in <repo>'s habits`) and the card says `banked`.
- **`not now`**: `nothing was banked: the person said no.`
- **Words**: `the person changed it: <your words>` and
  `Nothing is banked. Propose it again with that` (card: `changed in words`).
- **No answer** for fifteen minutes: `nothing was banked: the card was never answered`
  (card: `expired · nothing banked`).

It writes only where codeaf knows the checkout; otherwise the answer is
`codeaf does not know where <repo> is checked out; open codeaf there once`.

## talk it through — T, the item's own conversation

`T` on an item, on the floor or on its page, opens a conversation that belongs to that item, so
you can talk it over or plan it before you launch it, or while it runs. **It is never made
by default.** The first `T` makes it and every later `T` opens the same one; the item keeps it.

The first press makes, on this machine and without asking any model:

- a team named by the item, `#12 · fix the ledger double count`, nested under one team named
  `factory` (made once, the first time any item is talked through);
- one conversation in that team, in the folder where the item's repository is checked out (or
  this window's folder when codeaf does not know the checkout). It opens with the item in front
  of it: the title, the repository, author and tier, its gate, cap and labels, the
  body, its stages as numbered lines, the factory's read, and the sentence
  `This is the item's own conversation on the factory floor. Nothing here launches it; the person does that on the floor.`

You land in the conversation, as when you open one from its tab; the one you were in goes on
running behind it. The first `esc` on its empty box, with nothing running, takes you back to the
floor on the same row, however many turns the model has answered (and onto the item page if that is
where you pressed `T`). That way back is taken once; reopened later from its tab it is an ordinary conversation, and there `esc` means
what it always means, so a first `esc` arms the rewind (`esc again to rewind`).

**Where it appears:** a tab on the tab strip, like any conversation you open. Its team sits under
the one `factory` team in the team menu and on the teams rail, and that team is folded: a hundred
items talked through are one `factory` row until you stand in it. When the item is hidden with
`d`, its conversation is put away (home's archive line) and its team is closed.

`T` is not named, and does nothing, over `--host` or `--at` to another machine, from `--once`,
or on the still made-up floor: nothing there can make a conversation.

## the foreman — m, what should I take first, factory_floor

`m` on the factory floor opens the foreman: one conversation for the whole floor, for deciding
what to take first ("what should I take first this morning and why?", "mark the three cheapest
bugs"). **It is never made by default.** The first `m` makes it and every later `m` opens the
same one. It is named `foreman`, sits in the one `factory` team beside the items' own teams, and
opens with `You are the foreman of this factory floor.` and each repository's policy lines.
You land in it as with `T`, and the first `esc` on its empty box goes back to the floor.

It reads the floor with the `factory_floor` tool: `waiting` (items that need you), `risky`,
`cheapest` (ten, by estimate), `oldest` (ten), `all` (up to fifty), or one `item` in full (its
read, facts, stages, the question it waits on and the first 2000 characters of its body). A row
is the ref, title, kind, repository, size, estimate, priority and its reason, state, age and
risk; what is not known is left out.

**It proposes by marking.** It marks new items, and is told
`marked #1 #4 #6 · press L on the floor to launch them`. On the floor a marked row wears the
accent lead, as a `space` mark does. Its marks are kept with the floor on this machine, so they
survive a restart, until you launch or unmark them; a mark on an item that is no longer new is
dropped.

**It never launches, ships or posts.** Only a new item takes a mark (`#3 is running, and only a
new item takes a mark`), and `L` on the floor is the only launch (see what the factory does not
do yet). `factory_floor` is on the belt wherever `factory_add` is, so any conversation can read
the floor. `m` does nothing over `--host` or `--at`, from `--once`, or on the still made-up
floor.

## changing an item from its conversation — factory_item, skip a stage, plan first, raise the cap, leave a note

An item's own conversation (`T`) is the item's hub. In it codeaf can propose a change to that
item with the `factory_item` tool: stages to add (`after review, read it for auth holes`), skip
or switch on; its gate (`plan`, `ship`, `none`); its cap in dollars; its effort (`cheap`,
`strong`, `default`); or a note its stages will read. It is for the item the conversation is
about, and it is on the belt wherever `factory_add` is. (It replaced `factory_stages`, which
could change only the stages.)

**Nothing changes before `1`.** A card asks one question built from what changes:
`#1 · skip the review stage?`, `#1 · plan first with a $8 cap?`, `#1 · add a note for the stages?`,
or `#1 · change the plan?` for several. Its body is only what changes: `now:` and `after:` for the
stages, `gate  ship → plan`, `cap  $5 → $8`, `effort  — → strong`, `note: …` and `why: …`.
It answers to `1 yes`, `2 keep it`, or words (`say what to change… (enter sends it)`). There is
no clock on it.

- **`yes`** applies it and codeaf is told the item now, only what is set:
  `#1 now: plan · write · test · proof · gate plan · cap $8`, and that the change is made. The
  card says `changed`.
- **`keep it`**: `nothing changed: the person said no.` (card: `kept as it was`).
- **Words**: `the person changed it: <your words>` and `Nothing changed. Propose it again with that`.
- **No answer** for fifteen minutes: `nothing changed: the card was never answered`.
- A change that leaves the item as it is asks nothing: `nothing changed: the item already runs that way`.

The recipe's bounds hold before any card: proof, a person's gate and a stage the policy names are
never skipped, a stage that has run is never touched, and under `fixed` a stage change is refused
with `nothing changed: the recipe for issue is fixed; plan may not change the stages` (and nothing
else in that card changes). Notes are kept on the item, and every stage's brief opens with them.
Nothing launches from it.

## the item card in a conversation — live item status, #12 in a reply

Wherever a conversation is about an item on the floor, the item is drawn as one live card:
`▤ #1 <title>` with `repo · kind · size` on the right, and under it where it stands —
`new · gate ship · cap $5`, `running 24m · $1.42 / $5`, `? plan is ready`, `landed · 3✓ 1✕`,
`shipped · $1.90` — with its stages on the right (`○ plan  ○ write  ○ test  ○ proof`, the running
one lit).

It appears in three places: after `factory_add` adds the item (as that card's body); at the top
of an item's own conversation (`T`), in place of the opening note the model reads; and under a
reply that names an item the floor knows, `#12` in the current repository, once per item per
turn.

It stays live: the floor is read again when a card settles and every three seconds while a
conversation with a card is on screen. `enter` on the selected card, or a click on it, opens the
item page, as `enter` on the floor does. Over `--host` it draws from the same news the engine
sends, and opens nothing, because there is no floor page there.

## connecting github — watch a repository, issues and pull requests on the floor

The floor reads open issues and open pull requests from the GitHub repositories you watch.

- **Which repositories:** the ones you tick with `R` on the floor (see choose which
  repositories the floor watches), kept in `repos.json` in the factory folder
  (`~/.codeaf/v3/factory/repos.json`), each written `owner/name`, as
  `{"repos": ["acme/api"]}` or a plain `["acme/api"]`. The file can still be edited by hand. A
  running poll reads the list on every tick, and a window with no poll yet looks again every 30
  seconds, so a change reaches the floor without a relaunch.
- **The token, in this order:** `GH_TOKEN`, then `GITHUB_TOKEN`, then a token kept in
  the profile, then what `gh auth token` answers, and `gh` is asked only after you have said yes
  to it on the floor. The token is never shown or logged.
- **No watched repository or no token: nothing is connected.** Nothing polls, and the floor's
  facts line says only `terminal · chat`.
- **How often:** every 60 seconds while a codeaf window is open on this machine; after a
  failure the wait doubles, up to 10 minutes. Only one window polls at a time. An unchanged
  list costs no rate limit.
- **What is read:** number, title, the first 2000 characters of the text, author, labels,
  times, its page on GitHub and its last three comments; for a pull request also its checks
  (`ci ✓`, `ci running`, `ci ✕`), `+N −M` lines, its 20 most changed files and each check run
  by name (see what github gives an item). Size is S, M or L (up to 50, 400 changed lines, or
  more). The author is `owner` for your own login with write access, `collaborator` for others
  with write access, otherwise `stranger`.
- **A change on GitHub updates the row's words** (title, text, labels, checks, lines, comments)
  and never your gate, cap or stages. A changed title or text clears the item's read so triage
  reads it again; a label change alone keeps the read. A dismissed item that changes comes
  back as new.
- **The facts line** says `github` and `polled 4m ago`; a failed read says
  `github · not reachable` (or `token refused`, `repository not found`).
- **Nothing is ever written to GitHub by itself.** The poll only reads: no comment, label,
  close or pull request is made by it.

## what github gives an item — comments, changed files, check runs, its page

Each GitHub item on the floor carries, beside its row's words:

- **Its page:** the issue's or pull request's address on GitHub, which opens it in your
  browser. An item typed in the terminal or split off a chat has none, and opening it says
  `this item is not on github`.
- **The last three comments**, oldest first, each kept to its first 1000 characters, with who
  said them and when. They are read only when the item is new or its comment count changed, so
  an item nobody commented on costs no extra request.
- **For a pull request, the changed files:** the 20 with the most changed lines, each with its
  added and removed lines, read once each time the pull request changes.
- **For a pull request, the check runs** on its head, by name, each with its state (`success`,
  `failure`, `in_progress`, `queued`) and its page. The row's short `ci ✓` stays the summary.
- **Its activity**, the last 20 things that happened to it: `arrived from github`,
  `changed on github` (its title, text or labels changed), `read` (triage read it), `talked` (its
  own conversation was made with `T`), and `stages changed by plan`.
- **While github is being read** the floor says so; it is marked for the length of each read and
  cleared when the read ends, or when codeaf next starts after a crash.

## refresh an item — u, the whole floor — U

`u` reads the item in front of you again from GitHub now, without waiting for the next poll: the
issue or pull request by its number, its last three comments (read even when the count did not
move), and a pull request's files and check runs. Then its read is cleared, so triage reads it
again. A terminal item has nothing upstream; `u` only clears its
read. While it runs the row says `refreshing`; while triage reads an item it says `reading`.

`U` does the same for every item on the floor except the dismissed, in turn, and says
`refreshing 8 items · 3 done` while it runs. **Before it starts, the floor shows what it would
cost:** the number of items times the average cost of a triage read here, or $0.0005
an item when no read has been priced yet. Nothing is read or spent until you say yes. Each read it
causes is a call on the cheapest seat, and a row on the spend ledger named `factory triage`.

With no GitHub connected, `u` on a GitHub item says `github is not connected`. `u` and `U` are on
your own floor only, never on the still fixture.

## choose which repositories the floor watches — R

`R` on the floor opens a list over the floor headed
`watch · repositories github sees as <login>` (with how many are watched at the right), one row
per repository: `[x] owner/name   private · pushed 2d` for a watched one, `[ ] owner/name` for one
that is not. Watched repositories GitHub no longer lists stay at the top so you can untick them.

- `↑` `↓` walk, `space` ticks or unticks, `/` filters by name, `enter` saves, `esc` clears the
  filter and then closes without saving: `↑↓ walk · space watch · / filter · enter save · esc cancel`.
- Saving writes `repos.json` and says `watching 3 repositories · github polls every minute`
  (or `watching no repositories · the floor keeps what chat and n bring`).
- **With no GitHub token** `R` asks first. When `gh auth status` answers within 3 seconds it asks
  `connect github: gh is logged in as <login>, use it? [y]` (`y use gh · n a token instead ·
  esc not now`); otherwise it opens a row `github token ›` whose words are drawn as dots. A yes
  or a token GitHub accepts says `github connected` and opens the list. The token is kept in your
  profile beside the model key, never shown and never logged; `github did not take that token`
  and `gh answered no token · gh auth login, then try again` are the two refusals.
- `R` is not offered on a floor whose store cannot keep repositories (over `--host`, or the still
  made-up floor).

## the recipe page — E

`E` on the floor opens the recipe page for the repository the floor's `[ ]` line shows (or the
floor's only repository). With every repository showing it says
`pick a repository with [ ] first · a recipe is one repository's`.

It is the item page with no item: the kinds `issue · pr · ci` across the top with the current one
in ink and `recipe · <repo>` at the right, the stage rail at the left and the selected stage at
the right (its knobs, its sentence, `runs first` or `runs after <stage>`, or
`switched off in this recipe`). A line of `.codeaf/factory.md` that did not load is one dim line
above the rail, `line 7: until is one of done, clean, green, proven`.

- `[` `]` (or `h` `l`) switch the kind, `↑` `↓` walk the stages, `esc floor` goes back.
- Where codeaf knows the checkout: `1-9` switch a stage on or off, `s` adds one in words
  (`+ stage ›`), `e` steps its effort, `w` sets its knobs in the recipe file's own words
  (`until clean, max 3, effort strong`; a word it cannot read says why), and `b` saves the whole
  recipe, every kind, to `.codeaf/factory.md`:
  `saved to .codeaf/factory.md · new work on codeaf runs it`.
- Where it does not, the page is drawn but not changed, and the note line says
  `codeaf does not know where <owner/name> is checked out`.

## the day rail — $

`$` on the floor opens the row `day rail ›` (with the rail already set in it, `$60`). Type a dollar
figure and `enter`: `the day rail is $60`. `0` or `off` takes it off: `the day rail is off`. A word
that is not a figure says `a rail is a dollar figure, like $60`.

The rail is kept with the floor's store (`sources.json` in the factory folder) and the
handover's money reads it: `$11.31 / $60 today`, or `/ $60 today` before anything is spent. With
no rail set there is no `/ $N` at all. Once today's spend has reached the rail, a launch is refused
with `the day rail is $60 and today's spend has reached it`; an item already running is not
stopped by it. `$` is not offered on a floor whose store cannot keep it.

## connections on the settings page — github

The settings page's **Connections** tab has a `github` row when this window can ask how it
reaches GitHub: `github  santoshkumarradha · via gh` (gh's own login, after you said yes to it),
`<login> · token` (a token kept in your profile), `<login> · from the environment` (`GH_TOKEN` or
`GITHUB_TOKEN`), `not connected`, or `not reachable`. It is read once when the page opens.
`enter` on it opens the floor with the same connect prompt `R` asks (`enter connects`), so a
token can be changed there.

## triage on arrival — the read, size, estimate and risk, from one cheap call

Every new item on the floor is read once, by one call on the cheapest seat (the `small work`
row in /settings; a row you cleared follows the conversation's model). The call sees the item's
title, its text cut to 3000 characters, its labels, its kind, its repository and its last three
comments (each cut to 600 characters, so the read sees where the discussion stands), and answers:

- a one-sentence read (shown under the row in the comfortable density, `z`, and on the peek),
- a type (`bug`, `feat`, `chore`, `question`) and a size (`S`, `M`, `L`),
- an estimate in dollars (the `~$3` on the row),
- risks in short phrases, such as `touches money`, `touches auth`, `has ui`, `migration`,
- a possible duplicate (`#950`), only when the text names one,
- a priority from 1 (take it first) to 5 (later) and a reason of at most five words, used by
  the `first` order.

It only fills what is empty: a type GitHub's labels gave, a size the poll measured, and anything
you set win over the model. One item is read every two seconds, newest first; with nothing new it
looks again every thirty seconds. Each item is read ONCE: an answer that cannot be understood is
not asked for again, and the item simply has no read. A call that fails is tried three times.
Reading does not change a row's age. An item whose title or text changes on GitHub, or that you
refresh with `u` or `U`, is read again; the row says `reading` while its call runs, and the floor
quotes the last read's cost.

What it never does: launch, hide, dismiss, label, comment, change a gate, cap or stage, or ask you
anything. It is facts drawn dim, never a decision. It runs only in the window that opened the
floor, one window per machine, and only while a key resolves; with no key there is no reading
at all. Each call is a row on the spend ledger named `factory triage`, on the low seat.

## order the floor — O

The rows inside each group are ordered by `priority` by default: priority 1 first, unranked items
last, then the newest issue number first (the floor's own number for chat and terminal items).
`O` cycles `priority`, `first` (priority, then older first), `age` (oldest arrival first) and
`cost` (cheapest first). The groups never move: `NEEDS YOU` stays on top in every order.

**The rows do not move under you.** An order is applied when the floor is first read and when
`O` is pressed; after that a re-read never reorders the rows, even when it changes an item's
priority. Two things move a row: a new arrival, which slides in at the top of its group, and a
change of state, which moves the row to the top of its new group. Press `O` round to sort again.

The bottom line names the current order, `O order · first`. In the `first` order each row shows
the triage reason dim before the age (`main is red    6h`), the first thing a narrow row drops.
`O` does nothing on the item page. The choice lasts until codeaf closes; it is not saved.

## what the factory does not do yet

Be plain about this when asked:

- **Items arrive three ways:** from a chat with `factory_add` (after you answer its card), from
  `n` on the floor, and from the GitHub repositories you watch (see connecting github). They
  are kept on this machine and are still there next launch.
- **Items run on this machine only.** `r`, `p` and `L` launch (see running an item), and stop,
  pause, answers, steering, sign-off, send back and check again work from any window on it.
- **Only GitHub is connected.** No GitLab or Linear.
- **The recipe file is read only where codeaf knows the checkout.** It is read for the
  repository you opened codeaf in, and for any watched GitHub repository whose checkout codeaf
  has seen you open. Every other repository runs the default recipe until then, and for those
  `b` is not offered, no habit is offered for banking, and the recipe page (`E`) is drawn but
  not changed.
- **The day rail stops launches, not running items.** A launch past it is refused; an item
  already running goes on.
- **Only a post stage writes to GitHub** (a comment, labels, a close, a pull request), through
  the connected GitHub account, and only what the recipe's policy allows.
- **The diff does not open yet.** `d` on a landed item says where it will be.
- **Nothing launches from the chat.** `factory_add` only puts an item on the floor as `new`.
- **The foreman only reads and marks.** `m` opens it (see the foreman); it cannot launch,
  ship, post or change an item, and the person's `L` is the only launch.
- **`CODEAF_FACTORY_FIXTURE=1`** shows a still, made-up floor (three repositories, ten items)
  in place of yours, with no verbs.
- **Over `--host` or `--at` to another machine** the page draws `nothing connected yet`, the
  chat has no `factory_add`, and nothing polls GitHub.
- **`Factory ? N` on the tab bar appears only after the first open** of `/factory`.

## running an item — r, p and L

`r` launches the item under the cursor with the ship gate, `p` with the plan gate, and `L`
launches every marked item (or this one). A launched item is `queued`, takes one of the floor's
benches (four at once; `CODEAF_FACTORY_BENCHES` pins another number) and runs its stages in
order. It lands with its proof sheet for your sign-off, or ships by itself on gate none when every
claim was shown.

**What runs today:**

- A **check** stage runs its command in the item's own worktree (see where an item's work
  lives); the exit code is the answer. With no known checkout it says
  `codeaf does not know where <repo> is checked out`.
- A **post** stage writes through the connected GitHub account (comment, label, pr, close),
  when the recipe's policy allows it. `pr` first pushes the item's branch to `origin`.
- A **chat** stage (plan, write, review; in codeaf's default recipe test and proof too, so test
  runs what the change implies itself, and `3. test · check · go test ./...` in the recipe file
  makes it a check) is a conversation, named `#12 · review`, made in the
  item's own team under `factory` in the team menu, where you can open it. It works unattended,
  for at most two hours, and ends with `stage_result` (see a stage is a conversation).
- A **gate** waits for you.

**Every verb works from any window.** One process on this machine runs the floor's items, the
owner: on the ordinary launch it is the session host, so items keep running when the terminal
closes; with `--no-host` it is the first window that opened the floor. Every window still has every
verb: launch, stop, pause, answer, steer, sign-off, send back and check again. A window that is not
the owner hands the verb to the owner, which carries it out within about a second, and a refusal
(the day rail, `#12 has landed · sign it off, or send it back`) is said on that window's bottom
line in the owner's own words. If nothing answers within five seconds the window says
`the floor's runner did not answer · is codeaf running?` and nothing happens later.

**What a stage may do.** A chat stage runs with the allow posture (what `--yolo` gives) in the
item's own worktree, never your checkout, so it edits files and runs commands without asking; your own approval
settings do not narrow it (see what a stage may do).

**Spend.** A chat stage's calls are on the spend ledger under its own conversation, and the
item's spend counts them against its cap and the day rail.

## where an item's work lives — a worktree and a branch per item

An item never works in your checkout. The first round that needs a folder makes the item its
own git worktree, `~/.codeaf/v3/factory/work/<repo>-<number>` (such as `work/api-12`), on its
own branch, `factory/<number>-<slug>`, the slug from the title, at most forty characters (such
as `factory/12-total-double-counts`). Every later round, and every stage, works in that same
folder, so four items running at once never write over each other. The item's log says
`branch: factory/12-total-double-counts` once, when the worktree is made.

The branch starts from your remote's default branch (`origin/HEAD`) when git knows it, else
from the branch your checkout is on. **Your checkout is never touched**: uncommitted changes
in it stay where they are, are not an error, and do not follow the item.

A `pr` post stage pushes the branch first, with `git push -u origin <branch>` and never with
force, then opens the pull request from it. A failed push says
`could not push factory/12-…: <git's last line>`; a repository with no `origin` says
`#12 has no remote to push to`. A worktree git could not make says
`could not make a worktree for #12: <git's last line>`, and the stage does not run.

Stop, ship and send back all keep the worktree and the branch. Nothing deletes either yet: to
clean one up, `git worktree remove <folder>` in your checkout, and the branch stays.

## where factory items are saved

Factory items are saved on this machine, in the v3 factory folder under the codeaf home:
`~/.codeaf/v3/factory`, or `v3/factory` under `CODEAF_HOME` when that is set. Each item is one
small file named by its number, such as `1.json`, and a change rewrites that file. They are not
in any repository and not on any server; nothing is posted, synced or uploaded. The same folder
holds `repos.json` (the GitHub repositories you watch), `sources.json` (when GitHub was last
read, the day rail and what a triage read costs) and `busy.json` (what is being read right now). A chat's
`factory_add` and the page's `n` write to the same folder, so an item added from a chat is a row
on the floor straight away. Deleting the folder empties the floor.

## the Factory button on the tab bar

The tab bar at the top of every page shows `Factory` third, after `Home` and `Chats`. When items
on the factory floor wait on your answer it carries their count in the question colour:
`Factory ? 5`. With nothing waiting it carries no number at all. The count is what the floor said
the last time it was read, and the floor is read while the factory page is open, so it appears
after you have opened `/factory` once. On a narrow bar the places after it fold into `More ▾`
first; a `Factory` with a count stays on the bar. `alt+9` still opens it, whatever its place on
the bar.
