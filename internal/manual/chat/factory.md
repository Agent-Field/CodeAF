# The factory

## /factory — the floor

The **factory** is a place, like home, activity and spend: the floor where work a chat splits
off, or that a connected repository sends, stands in rows grouped by where each item is. Open
it with `/factory`, `alt+9` (`opt+9` on a Mac), the map (`alt+.`), or the `Factory` button on the
tab bar, third after `Home` and `Chats`. `esc` goes back to the conversation.

On this machine the page reads your own factory floor: the items you made with `n` on the page
or added from a chat, kept under the codeaf home (see where factory items are saved). Until the
first one arrives the floor draws one dim line under the handover,
`work arrives here from chat, from n, and from the repositories you connect` (or, with
repositories watched, what their read is doing; see after connecting github), and `n new` still
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

The floor names its keys in three places, each short:

- **The bottom bar is the way around**, never the row's verbs:
  `n new · R repos · m foreman · / filter · tab next place · esc back · ? keys`. A clause whose
  door is not there is left out (`n new` too while the row under the cursor needs you, where `n`
  answers no), and when the line is too long it drops clauses from the right of the floor's own,
  keeping `esc back` and `? keys`. `esc` says `esc clear` while a filter or a repository narrows
  the rows.
- **The peek's last row names the row's verbs**, at most five (see the peek's verbs).
- **`?` opens the key sheet**: every key with its word (see the ? key sheet).

The keys themselves:

- `↑` and `↓` (or `ctrl+p`, `ctrl+n`, the wheel over the rows) walk the items. Resting the
  pointer on a row selects it and the peek previews it. A click selects a row, a second click
  opens it; a click in the peek moves nothing, except on `▾ more`.
- `enter` opens the item on its own page (see the item page).
- `J` and `K` scroll the peek's description, `pgdn` and `pgup` a page; so does the wheel over the
  peek.
- `{` and `}` move the divider, `|` puts it back (see resize the split).
- `h` switches the handover between one line (the default) and four rows; remembered.
- `z` switches compact rows (one line each, the default) and comfortable rows (a long title on
  two lines, a dim line with the factory's read, a blank row between items). Not saved.
- `O` re-sorts the rows inside each group (see order the floor); the `?` sheet says the order
  in use, `O order · priority`.
- `g` opens the item on github (see open the item on github).
- `u` refreshes the item; `U` refreshes all, after asking (see when the floor is doing
  something).
- `p` is `shape steps`: the item's manager sets its steps now and runs nothing (see does opening
  an issue run anything).
- `space` selects or unselects a new item, and pauses or resumes a running one. `T` opens a chat
  about the item.
- `m` opens the foreman, the floor's own conversation for what to take first (see the foreman).
- `/` filters, `[` and `]` show one repository at a time, `A` shows the whole backlog.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.
- `R` repos, `E` recipe and `$` rail open the floor's settings.

## the peek's verbs — at most five, by the row's state

The peek's last row names what you can do to the item under the cursor, **at most five**, in the
order a person reaches for them, and only keys the floor can do:

- **New:** `enter open · r run · T chat · space select`. While a row is selected,
  `L run selected` takes the fifth place.
- **Running:** `enter open · x stop · T chat · space pause · S steer` (`space resume` while
  paused). Queued: the same without pause.
- **Needs you:** `enter open · y yes · n no · a in words · T chat`. At an approve step it reads
  `y continue · n send back · a in words`: `y` continues, `n` sends the run back to the step
  before, and `a` continues with your words.
- **Landed:** `enter proof · s approve · B request changes · v re-run checks · T chat`, with
  `e approve with changes` in place of `s approve` when a row on the sheet was not shown.

When the peek is narrow the row drops whole clauses from its right end, never half of one. Every
other key (the budget, the thinking, the stages, dismiss, refresh, open on github) is on the
`?` sheet and the item page.

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
(`3✓ 1✕`, waiting for you to approve, so it sits beside what waits on you, above the backlog),
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
`A` shows the whole backlog under `new`; `A` again keeps the older ones back. Dismissed items
(`d`) are never drawn.

## select new items and run them together — space and L, what is select

`space` on a new item selects it, and its lead turns to an accent dot; `space` again unselects
it. Only new items can be selected. The selection is kept through every re-read and when you
leave the page and come back, until codeaf exits. While anything is selected the peek's verbs
offer `L run selected`, and `L` asks first, with the triage estimates summed:
`run 3 selected · ~$7? [y] go · [n] not now`. `y` runs every selected item, clears the
selection and says `launched 3`; `n` or `esc` keeps it. With nothing selected `L` runs the item
under the cursor. What the foreman selects (`m`) is kept with the floor on this machine and is
still there after a restart.

## stages, not a pipeline

An item runs through **stages** in a fixed order that a repository or team writes once: for
example plan, write, test, review, neaten, proof. Each stage is one sentence ("read it as a
stranger would"), and it runs as an ordinary task with the ordinary crew picked for that kind of
work. There is no graph to draw and no model to choose per stage; the one word a stage may carry
is its thinking (`effort` in the recipe file), cheap or strong.

A stage can repeat until a condition holds (until green, until clean, until proven) up to a
number of rounds, and then it stops and asks you. A second round fixes what the first found, then
checks again: its brief names what the last round found and says to fix it in the checkout, run
the tests, and look again, so a review that found one thing does not just find it twice. Nothing inside a stage can add a stage, except plan, within the
bounds its recipe word allows (see who may change an item's stages). A stage can also be an
**approve** step, which is where the run holds for you: codeaf's default runs plan → approve →
write, so you read the plan before any code (see approve steps).

The peek shows a new item's stages as one line of names and a running item's as a strip of
marks; the item page lists them down its stage rail. A stage can carry a condition (`thin`,
`large`, `touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim with the
skip stroke `–` and `· skipped` on the stage rail, and `skipped · not thin` (or the condition it
missed) beside it. A stage switched off, or one the run went on past, is drawn the same way.

A stage's name is one lowercase word of letters and digits, 2 to 12 long (`review`, `arch`,
`e2e`), and an item has at most nine stages, so a digit reaches each one.

On a new item, `1` to `9` switch the stage with that number on or off (`1-9 stages` on the `?`
sheet), `s` adds a stage in
words (`after review, arch: read it for the architecture` puts a stage named arch after review;
with no `name:` the stage is named by its ask's first word, so `after review, make it neater`
is `make`), and `b` banks the item's stages
as its repository's recipe, into `.codeaf/factory.md` (see the recipe file). A new item starts
from its repository's recipe file, or codeaf's default when there is none. Once a run starts,
the stages it has not started yet are read again at each round, so a change the manager makes
during the run (see the manager shapes the run) reaches them; a stage that started is never
changed.

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

A stage line is `N. name · kind · ask · knob · knob`, the name one word; a name of two words is
named as a problem, `a stage is one word · "do through" is two`, and a tenth stage line as
`the run has nine stages already`. The sections are `## issue`, `## pr`,
`## ci` and `## chore`; a missing section runs the default for that kind. A kind's heading may
end in one word, `## issue · adapt`, `## issue · ask` or `## issue · fixed`, saying how much plan
may change its stages (see who may change an item's stages); no word is `adapt`. `## policy` and
`## habits` are sentences kept word for word. A line codeaf cannot read is named with its line
number and why, and the rest of the file still loads.

**The recipe is read from the repository's main branch, never from an item's branch** (see the
recipe is read from the main branch), so a stranger's pull request cannot rewrite the policy it
is held to. Policy and habit lines are
applied by codeaf's own code; they are never handed to the model as instructions.

`b` on a new item writes its stages into its kind's section of this file and leaves every other
section as written; banking a habit adds a `- ` line under `## habits`. Both are offered only when
codeaf knows where the repository is checked out; otherwise `b` is not on the bottom line.

codeaf reads the file for the repository you opened it in, and for any watched GitHub
repository whose checkout it has seen you open. Open codeaf inside a repository you watch once
and its recipe is read from then on.

## recipe file knobs — kind, when, until, max, fanout, gate, effort, proof, off, fixed

In a `.codeaf/factory.md` stage line, after the name:

- **kind**: `chat`, `check`, `gate` or `post`; `chat` when absent. A line named `approve`
  (`3. approve`) is a gate without saying so: the run holds there for you (see approve steps).
  `## approve` on a line of its own inside a kind's section is the same step at that place.
- **ask**: the first segment that is not a kind and not a knob. `check` runs it as a command.
- `when always`, `when thin`, `when large`, `when touches auth`, `when has ui`: the stage is
  skipped on an item the condition does not fit.
- `until done`, `until clean`, `until proven`, `until green`, and no other word. done: the stage
  reported done. clean: it reported zero findings. proven: every claim on the item has evidence.
  green: the check's command exited 0.
- `max N` (or `rounds N`): rounds before it stops and asks you.
- `fanout 3`, `fanout one`, `fanout per finding`, `fanout per file`, `fanout per claim`.
- `effort cheap` or `effort strong`.
- An older file's `gate plan` / `gate ship` / `gate none` is read as an approve step and written
  back as one: `gate plan` on plan is an approve after plan, `gate ship` an approve after its
  stage, `gate none` nothing; `gate plan when large` is `approve · when large`.

The file keeps these words; the screen says them its own way: `effort` is `thinking`, and an
item's `cap` is its `budget`.
- `proof a test, a screenshot`: what the stage must show, separated by commas.
- `off`: the stage is written but does not run. Every other stage is on.
- `fixed`: the team's law. Nobody may change the stage's ask, thinking or rounds, or switch it
  off: not the manager, not plan, not you (see who may change an item's stages). It may be
  switched on. `## issue · fixed` marks every stage of its section fixed as well. `fixed`
  followed by more words, as in `fixed bugs get a test`, is an ask, not the knob.

A line with only a name, such as `4. proof`, is codeaf's default stage of that name for that
kind; a name with knobs and no ask is that default with the knobs laid over it. A knob word
with a word it does not know (`until clen`) is named as a problem, never read as the ask.

## who may change an item's stages — the manager before a run, plan during it, you always

A run is a short program of one-word stages. Three hands may change it, through one door with
one set of bounds held by codeaf's own code, never asked of a model:

- **the manager, before a run**: it may give any stage a new ask, change a stage's thinking
  (cheap, strong, or nothing), add a conversation stage after any stage it names, switch a
  stage on, and skip a stage. Nothing has run, so nothing is out of reach except what the
  common bounds keep.
- **plan, during a run**: the same, but never a stage that is done or running, and nothing is
  added before one. The recipe gives each kind one word after its heading in
  `.codeaf/factory.md` saying how much plan may do:
  - `## issue · adapt`: within the bounds. The default when there is no word.
  - `## issue · ask`: the same, and the item's gate becomes `plan`, so you ratify the change
    before anything after plan runs.
  - `## issue · fixed`: no change at all. It is refused with
    `the recipe for issue is fixed; plan may not change the stages`.
- **you, always**: from the item's settings, or in words. The adapt word holds plan, not you.

**A stage marked `fixed` in `.codeaf/factory.md` binds all three hands, you too**, before a run
and during it: its ask, its thinking and its rounds never change and it is never skipped or
switched off, from the manager, from plan, from the item's settings or from a chip like
`rounds 3` on `n`. Switching a fixed stage on is allowed. Every road refuses with one sentence:
`security is fixed by the recipe · change .codeaf/factory.md to change it`. The law changes only
by changing the file on the main branch.

The common bounds, in every case:

- a stage is one word (`a stage is one word · "do through" is two`); an item saved before this
  bound is read with each name made one word, its first word that can be a name past a `do`
  (`do through` is `through`, a requested-changes `prove 2` is `prove2`);
- no two stages share a name, and an item has at most nine stages
  (`the run has nine stages already`);
- an added stage is a conversation with an ask, and an ask is at most 240 cells
  (`an ask is at most 240 cells`);
- thinking is cheap, strong, or nothing (`thinking is cheap, strong, or nothing`);
- the stage named `proof`, a gate stage, and a stage a policy line names are never skipped
  (`plan may not skip proof`; `tests pass before anything posts` names test);
- nobody may change the budget, where the run asks you, or the rounds this way.

One refused part refuses the whole change, and the refusal names the part. Every stage added or
changed keeps who changed it and why.

What changed is kept on the item and drawn as one dim line under the stages on the peek and in
the item page's head, who first:
`manager set review: thorough on security, code and architecture · added arch after review ·
skipped neaten · why: touches the call row`. A later change by someone else names them
(`· you switched on neaten`). An item nobody changed draws nothing there.

## the recipe is read from the main branch — a pull request cannot change the law for its own review

codeaf reads `.codeaf/factory.md` from the checkout's main branch, the copy git holds at
`origin/HEAD` (else `main`, else `master`), never from the branch you have checked out and never
from a pull request's branch. An edit to the file counts once it is merged. Only when there is
no git or no main branch to read is the working file read; a main branch without the file runs
the default recipe. codeaf's log says which, once per checkout: `recipe from main branch`,
`recipe from working tree` or `recipe from default`.

## the handover

The **handover** is what happened on the factory floor since you last looked. By default it is
one line across the top of the floor, then a blank row:

`◆ 2 shipped · 3 arrived · ? 5 waiting · $8.44 / $60 · polled 14s ago`

what shipped and arrived this stretch, what waits on you (in the question colour), the day's
spend against the day's rail, and when a source was last read. **Waiting counts the items that
ask a question and the landed items waiting for you to approve,** so `? 2 waiting` is one
question and one proof sheet; the full strip's `? 2 waiting on you` and the tab bar's `? 2`
count the same, and `nothing waits on you` means neither. A part whose count is zero is
left off; a narrow window drops parts from the right. While a source is being read the last part
says where the read is, `⠋ reading Agent-Field/CodeAF · 1 of 3`, or `github · ⠋ polling` when
the source does not say; while every item is read again, `⠋ refreshing 8 items · 3 done`. A
floor with nothing on it while a read is out shows that part alone, never `quiet`.

`h` switches to the four-row strip and back, and the choice is remembered in `factory.json` in
the codeaf home, beside the split:

1. `◆ handover · since 23:12 · 7h 12m · $8.44`, with a line out to the edge.
2. `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`, or
   `quiet · nothing happened while you were away`.
3. `? 2 waiting on you` or `nothing waits on you`, and at the right a `24h` sparkline.
4. `3 repos · github · chat · benches 2/6 · polled 4m ago`, and `$11.31 / $60 today` at the
   right.

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
5. The budget and the thinking in fixed slots, so their values line up from item to item:
   `budget  $5`, `thinking  —`. A budget of nothing is a blank slot. Where the run holds for
   you is an approve step among the stages, never a slot here.
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
9. `chat`, when the item has its own conversation.
10. What the forge says, each block absent when empty: `comments` (the last three, `author · 2h`
    then the words), `checks` (one row per check: mark, name, state), `files` (`+218 −44 · 6
    files`, then up to six paths), `activity` (the last five events). The item's page on GitHub is the `github ↗` link in the meta row.

The bottom row names the row's verbs, at most five, starting with `enter open` (`enter proof`
on a landed item), with one blank row above it, and only keys the floor can do: a still floor
with no verbs says just `enter open`. It is cut from its end to the peek's width (see the peek's
verbs).

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
in `…` with a dim `▾ more` at the right. To see the rest:

- `J` scrolls it down a row and `K` up a row; `pgdn` and `pgup` move a page.
- The wheel over the peek scrolls it three rows a notch. Over the rows the wheel walks the items.
- A click on `▾ more` scrolls it a page.
- `enter` opens the item's page, where the whole description stands (below).

The scroll stays where you left it while the floor reads itself again. Moving to another item
starts that item at its top.

To read everything, open the item (`enter`, or click its row twice). The item page's left column
starts with `issue`, and its pane is the whole description rendered from Markdown at 72 columns, then
`read 3m ago · u again` (or `⠋ reading · 4s` while a read is out, `waiting to read` while it
waits its turn, `not read yet · u reads it` before the first read, and `u reads it again` when
the time of the read was not kept), the factory's read, what codeaf read in one dim line (type,
size, estimate, priority and its reason, `bug · M · ~$3 · priority 2 · …`), the facts and risks,
the questions it would ask the author, what plan changed about the stages, and then the comments
(each whole), checks, files, activity and links. `J`, `K`, `pgdn` and `pgup` scroll it, so do the
wheel over the pane and a click on `▾ more`, and its bottom row says `J K scroll` while there is
more to see. Over the left column the wheel walks its rows.

## the item page — issue, manager, steps, settings

`enter` on a row of the factory floor (or a second click on it) opens that item on its own page,
across the full width; `esc` closes it and puts the cursor back on the same row. Nothing about
the floor (filter, repository, selection) changes while it is open.

The top row is the page's bar: the run control, then a trail of breadcrumbs,
`Factory › codeaf › #1551 filters lost on compact`, and at the right the step the run is on, its
round and the spend over the budget, `review · round 1/2 · $1.42 / $5` (see the run control).
While the run waits on you the second row is its question with the keys that answer it. Then a
blank row, the left column, a dim `│`, and the center.

The left column is the **issue's map**, one row per part of the item, the steps nested two
cells under `steps`, then the item's actions under a blank row:

```
issue
manager
steps  running 4m · $0.31
  ● plan ?
  ● write
  ⠋ review ↻ 1/2 until clean
  ○ test ↻ 2 until green
  ○ arch +
  ○ approve ?
log
settings

shape steps
open on github
refresh
dismiss
```

A step row is its mark, its one-word name, then what fits of its loop: `↻ 2` the most rounds it
may take, before it runs (only where it may take more than one); `↻ 1/2` the round it is on over
its most, once it runs; `until clean` what it runs until; how long it has run. `+`, dim: the
manager, the plan or you added or changed this step; the recipe did not set it. `?`, dim: the
run stops to ask you here (an approve step, and the last step for the sign-off on an item
that has an approve step). A step held for you says `waiting for you` in amber. A step that ran as a
conversation wears the conversation's mark.

- `issue`: the whole issue and what codeaf read (see read the whole issue).
- `manager`: the item's own conversation, the team's lead (only where `T chat` works). Resting
  the cursor on it opens it in the center at once, as an ordinary idle chat with the issue
  already in it; making it asks no model anything, and nothing runs until somebody types.
- `steps`: where the run stands, and every step as written with its knobs.
- `log`: the run's log, once it has said anything (see the log).
- `result`: the proof sheet, the diff and the checks, once something came out.
- `settings`: thinking, budget and which steps are on.
- `shape steps`, `open on github`, `refresh`, `dismiss`: each does what its key does (`p`, `g`,
  `u`, `d`). `shape steps` is there while the item has steps still to come (see the manager sets
  the steps when you ask).

The page opens on the step waiting on you, else the running step, else `result` for a landed
item, else `issue`. Under 72 columns the left column is one line above the center.

## the run control on the item page — run, pause, continue, space

The bar's first cells are one button that does what the run needs next, by `space` or a click:

- `▶ run` on an item that has not run, and on a run that is paused (it goes on in the same chat);
  on an item the manager never shaped, the cursor first goes to `manager`, its chat opens in the
  center, and the run starts there, so you watch the manager shape the steps before they run;
- `= pause` on a run that is moving;
- `▶ continue` on a run held at an approve step (a step of kind gate, a person): the step's row
  says `waiting for you`, the second row says `approve · waiting for you` (or the run's own
  question) with `space continue`, and `space` or a click lets the run go past it.

A queued or landed item has no control. `?` names `space` by what it does now. On the item page
`space` never selects: select is the floor's.

The button sends what it shows, not a toggle: pressing `pause` twice (or a click and a key landing
together) holds the run once and leaves it held, and `run` on a paused item goes on once. A
doubled press never undoes itself.

## move around the item page — arrows, tab, click, hover, the wheel

`↑` and `↓` walk every row of the left column in order, the steps and the actions included,
and the center follows the row. A step's chat the cursor only passed over is let go of when the
cursor moves on or you go back to the floor, so walking the steps leaves no tabs; one you gave the
keys to (`enter`, `→` or a click on its box) stays a tab. `→` or `tab` gives the keys to the center's chat box, `esc` or
`tab` gives them back, and `esc` on the page goes back to the floor. `enter` acts on the row: the
issue and the manager open the manager's chat, a step with a chat puts the keys in its box, an
action does what it says.

A click does what the key does: on a row it moves the cursor there (a second click is `enter`),
on an action it acts at once, on the control it runs, pauses or continues, on a crumb it goes
there, and on the chat's box it gives the box the keys. Resting the pointer on any of these
highlights it, one thing at a time, on a ground apart from the cursor's; the highlight goes when
the pointer leaves. The wheel scrolls what is under the pointer: the chat in the center, or the
left column when it is longer than the page.

## the settings of an item — where to change the budget, thinking, which stages run, add or remove an approve step

Open the item and walk to its last row, `settings`. The center is a table:

```
thinking   —             e
budget     $3            c

1 plan     on
2 approve  on
3 write    on
4 test     off

1-9 stages · s add a stage · w set in words · b save stages as the recipe
```

`e` turns how hard the model thinks, `c` raises the budget, `1` to `9` switch that stage on or
off (an approve step too: switched off, the run goes on past it), `s` adds a stage in words
(`after test, approve` adds an approve step after test), `w` sets them in words and `b` saves
the stages as the repository's recipe. `e` and `c` work from any row of the page. `1-9`, `s`, `w` and `b` answer on this row only: on any other
row they do nothing at all, so a letter typed toward the manager never opens
`+ stage › “after review, make it neater”`. A key is
shown only where it works: a running item's budget has no key, because it cannot change
mid-run, and its stages carry no numbers. `enter` here does nothing.

A stage the recipe file fixes is the team's law, and it shows it: the lock `§` stands where its
`on` would, the row is dim, and its number does not switch it off. Pressing it says
`security is fixed by the recipe · change .codeaf/factory.md to change it` on the note line,
with the stage's own name. The recipe page `E` refuses the same way: its number, `w` on it, and
`s` words that name it all say that sentence, and its pane shows the lock with the ask dim.
Switching a fixed stage that is off on is never refused.

## breadcrumbs on the item page — how to get back to the floor from an item

The item page's top row, `Factory › codeaf › #1551`, is a row of buttons. Click `Factory` to go
back to the floor, exactly as `esc` does. Click the repository, `codeaf`, to go back to the
floor showing only that repository (as `[` and `]` choose one; `esc` there clears it). Click the
number, `#1551`, to open the item on github, as `g` does; on an item with no page on github the
number is not a button. Resting the pointer on a crumb highlights it.

## start a conversation about an issue — enter on the issue row, when enter on the item page does nothing

To chat about an item before anything runs, open it and press `enter` on its `issue` or
`manager` row, or `T` anywhere on the page: the cursor goes to `manager`, and the item's own
conversation opens in the center of the page, the same chat view as anywhere else, with your
keys in its box. The page stays: the bar and the left column are still there, and `esc` gives
the keys back. An item with no conversation yet asks for one first; while it opens the line above
the keys says `⠋ opening #12's conversation…`. On the issue row the bottom line says
`enter chat`, on the manager `enter talk`.

From the floor, `T` opens the item's conversation as a chat of its own instead, and the first
`esc` on its empty box comes back to the floor (see chat about an item). So does `enter` on the
page when the window is under 72 columns, too narrow to keep the page beside the chat.

Where this window cannot make the item's conversation, `enter` on the issue row opens the item
on github, as `g` does, and the bottom line says `enter open on github`. Where neither works,
`enter` says on the pane's last row `nothing to open yet · r run` (without `r run` where `r`
does nothing). `enter` on the `log`, `result` and `settings` rows opens nothing.

## a step is a room — enter on a step, its conversation in the center of the item page

A step that ran as a conversation (plan, review: see a stage is a conversation) is shown whole
in the center when its row is under the cursor: the same view as any conversation, live while
the step is going, scrolled with the wheel, its tasks beside it, and a box to type into. The trail reads
`Factory › codeaf › #12 › review` and the hint says `enter conversation`. `enter`, `→`, `tab`
or a click on the box gives the box your keys; `esc` gives them back. The conversation you were
in before goes on running behind. Under 72 columns `enter` opens it on its own, and the
first `esc` on its empty box comes back to the item page on the same step.

A step with no chat says why in the center instead, and opens nothing:
`review has not started`, `review has no conversation yet · it opens when a round ends` (a
round's conversation is kept once the round ends), `test is a check · its log is below`,
`plan is your answer · it has no conversation` for a gate, `post is a write to github · it has
no conversation`, `neaten is off on this item`, `neaten is skipped on this item`.

## the log

Once a run has said anything, the item page's left column has a `log` row under the steps. Its
pane is the run's log, oldest at the top and the newest at the bottom, as many of the last lines
as fit; each line starts with its time, `12:04`, dim at the margin, then a mark for its kind and
its words. Three voices: a **thought** (the run thinking aloud, a round starting again) is dim;
**your own words**, the `steer: …` lines `S` and the conversation leave, are ink; everything else
(what a step said, a test, a failure `✕`, a success `✓`, a question `?` in amber) is the quieter
second voice. A claim a step made that is not for the proof sheet is a `claimed: …` line, and a
batch of reads handed to a quick task is one line, `read ×4 · find handed to quick task 1`. An
item that never ran has no log row. The log keeps the last 400 lines.

## typing to the manager — enter the box, the keys type, esc gives the keys back

With the `manager` row under the cursor the center is the manager's own conversation, the chat
view itself, your words and the manager's replies live as they are written. Your keys go into its box by `enter` on the manager
row, `→` or `tab`, or a click on the box.

While the box has your keys every key types, letters that are keys elsewhere (`s`, `r`, `t`,
space) included, and the arrows move in what you typed; nothing on the page acts. `enter` sends
your words to the manager as your message and you stay on the item page. `esc` or `tab` gives
the keys back to the page.

Before the item has a conversation the center is a box of its own,
`› enter or click to talk to the manager` (`› enter or click to talk · r runs it` before a run),
and `⠋ manager is thinking` while it works; the first words make the conversation. Where this
window cannot make the item's conversation there is no box.

## answering a question — y, n, a

A run stops and asks when it cannot go on alone, and the item moves to **needs you**. The
question is the run's own sentence, drawn as is in the peek (under the title) and on the item
page's second row, its `?` amber: `plan is ready · continue, or send it back?`,
`review is not clean after 2 rounds: 3 findings · one more round, or go on as is?` (one round
is `after 1 round`, one finding `1 finding`),
`budget of $5 reached · $5 more, or stop?`, `review did not finish: … · skip it, or stop?`.

- `y` answers yes: continue, one more round, $5 more, skip it. The bottom line says
  `answered #12 · yes`.
- `n` answers no: at an approve step it sends the run back to the step before; for a spent
  budget it stops the item; for rounds it goes on as is. It says `answered #12 · no` (`answered #12 · send back` at an approve step).
- `a` opens `answer ›` for words (`go, but keep the old flag`); they reach the next round's
  brief (at an approve step: the run continues with them) and it says `answered #12 in words`.

`[y] yes · [n] no · [a] in words` (`[y] continue · [n] send back · [a] in words` at an approve
step) is drawn only where the floor can answer. `y`, or `a`, on an
item that is not waiting says `#12 is not waiting on you` on the bottom line and changes nothing
(`n` there is new work, and `a` on a thin new item asks its author).

## the manager shapes the run — what happens when you press r

The manager is the author of the run. Shaping is its first act, not a stage: when you press `r`
(or `L`), after the manager is made and before the first stage, the runner gives it one turn of
its own conversation with the ask `Shape the run for this item now. What the person said: …`
(what you said to it beforehand, if anything). That turn thinks cheap, unless
codeaf read the item as large (`L`). The manager answers by calling `factory_run` once: it may
give a stage a new ask, change a stage's thinking, add a stage after one it names, switch a
stage on, or skip one, within the bounds under who may change an item's stages. Then the run
goes on:

- what it set goes on the item's log and into the manager's conversation as one line,
  `manager set review: thorough on security, code and architecture · added arch after review ·
  why: touches the call row`, and the item page's stages redraw;
- when it changes nothing, the line is `the recipe stands`;
- when its turn fails or takes longer than a minute, the line is
  `the manager did not answer · the recipe stands`, and the recipe runs as it is;
- when the manager's window was in the middle of answering you, the runner waits for that answer,
  asks once more, and if it is still busy the line is
  `the manager was busy in the window · the recipe stands`;
- when the bounds refuse what it set, the line is
  `the manager's change was not applied: <why> · the recipe stands`.

Shaping never stops the run: you read what the manager set at the first approve step (see approve
steps). An item the manager already shaped
because you talked to it first (see tell the manager what you want before it runs) is not shaped
again: that is the run's shape.

Talking first in the window still shapes: if you told the manager what you want and pressed `r`
with its conversation still open, the shaping turn runs in that same window, so you see the
manager think, at the thinking you set there. If the conversation is open in another codeaf
instead (a window on the engine host), the runner cannot give it a turn, and the line is
`the manager is open in another window · the recipe stands`.

Mid-way the same happens on a smaller scale: each line you say to the manager is the steer, as
always (see talk to the manager), and the manager is also given one turn,
`The person said: … · reshape the stages not yet started if that is what they mean, else leave
them`. So `do a thorough review on security, code and architecture`, said during plan, can
change review's ask before review starts; the log then says `manager set review: …`. A stage that
started is never changed.

## does opening an issue run anything — no; the manager sets the steps when you ask, shape steps, p

Opening an item's page runs nothing and spends nothing. The steps on the left are the recipe's:
the repository's `.codeaf/factory.md` as the main branch has it, else the built-in recipe
(plan, approve, write, test, review, security off, proof). The manager row opens at once as an
idle chat with the issue in it; no model is asked anything until somebody types.

The manager shapes the steps in exactly three ways:

- **`▶ run`** (or `r`) on an item it never shaped: its shaping turn (see the manager shapes the
  run) streams in its chat in the center, the steps on the left redraw as it sets them, then they
  run one by one, holding at each approve step.
- **You ask it in its chat**, `make the review about security`, and its `factory_run` changes the
  steps at once. Nothing runs.
- **`shape steps`** (`p`, or the row under the item's facets; a press or a click does the same):
  the cursor goes to `manager`, its chat opens, and the manager takes one shaping turn there with
  the ask `Shape the run for this item now.` Nothing runs. Before a run it may change every step;
  during one, only the steps not yet started. The note line says what it set, `manager set …`,
  `the recipe stands`, or why its turn did not happen (`the manager did not answer · the recipe
  stands`). An item whose run is over has nothing to shape and says so.

After a `shape steps` that changed nothing, the item says `manager kept the recipe`, and `▶ run`
does not shape it again. The manager's own verbs, `factory_run` and `factory_answer`, never wait
on an approval card: a shaping turn nobody is at the keyboard of could not answer one.

## tell the manager what you want before it runs — one word per stage, it sets the asks

Open the manager (`T`, or `enter` on the issue row) and say what you want before you press
`r`: `do a thorough review on security, code and architecture`, `skip neaten, this is one line`.
Nothing is running, so the manager sets it at once with `factory_run` and no card is shown: the
stages on the item page redraw, and the manager says what it set in three lines at most. Each
stage is one lowercase word (`review`, `arch`, `e2e`), at most nine of them, and each has an ask
that says what done looks like; the manager changes asks before it adds stages, and adds a stage
only for work no stage covers. It never drops proof. It may add an approve step after a stage
you want to read first (`hold for me after test`), or skip one when you ask it to; a fixed one
stays.

Budget, thinking for every stage and notes for the stages still go through
`factory_item`'s card (see changing an item from its conversation). `factory_run` is on the
manager's belt only; a stage's own conversation does not have it, and a conversation that manages no
item is told `this conversation is not the manager of any item on the floor`.

## approve steps — where the run holds for you, continue, send it back, plan approve write

An **approve** step is a stage that is you. codeaf's default recipe runs plan → approve →
write → test → review → proof, so the run holds after the plan until you say continue; a pull
request and a red main end on one instead. When the run reaches an approve step the item moves to
**needs you**, the step's mark is the amber `?`, and the question is the step before it:
`plan is ready · continue, or send it back?` (`ready to start · continue?` when nothing ran
before it). The manager says the same into its conversation, `asking you: …`, and can explain
what it waits on if you ask it there.

- `y` (or `space` on the item page, its top bar's `▶ continue`, or `continue` said to the
  manager) continues. `answered #12 · continue`.
- `a` with words (`keep the old flag`) continues with your words handed to the steps after as
  your note.
- `n` (or `no, use the other file` said to the manager) sends the run back to the step before
  with your words: that step runs again in a new round with them in its brief, the log says
  `sent back to plan: use the other file`, and the approve step holds again after it. With
  nothing before it, `n` stops the item.

An approve step with nothing after it is the landing itself: the run lands and the proof sheet
waits for your approval (see approve, request changes, re-run checks). An item with no approve
step at all ships itself when its proof is all green.

The recipe file places approve steps (`3. approve`, or a line `## approve` inside a kind's
section; see the recipe file), the manager may add or remove one, and so may you: `s` on the item
page with `after test, approve`, or a digit on its row in settings to switch it off. They replace
the old `ask me at`: an item or recipe that still says it is read as approve steps (plan is an
approve after plan, pull request one before the result, never none).
The shaping question `run these stages?` went with it: you read what the manager set at the
first approve step.

## approve, request changes, re-run checks — s, B, v, how do I approve a landed item

A landed item's page opens on its **proof** sheet: one row per claim and policy row, `✓` shown or
`✕` with `— not shown`, its evidence dim at the right and its medium (`test`, `screenshot`,
`policy`) as a chip in its own column at the right edge. The evidence never repeats the medium:
a test row reads `0.3s` beside `test`, not `test · 0.3s`. The sheet's last line counts it and names its keys:
`all 6 shown · s approve`, or `1 of 6 not shown · e approve with changes · B request changes`.

- `s` **approves** it and ships it: `#12 shipped`. On a sheet with a row not shown the run
  refuses, in its words: `#12 has a claim not shown`.
- `e` approves saying you changed something first, the one that ships a sheet with a row not
  shown: `#12 shipped with changes`. It does not count toward a habit.
- `B` **requests changes**: it opens `request changes ›` for what to prove
  (`prove restart survival`); the item goes back on a bench with one more stage, `prove`, and
  says `#12 changes requested · prove restart survival`.
- `v` **re-runs the checks**: every check stage runs again and puts what it shows on the sheet:
  `#12's checks are running again`.
- `enter` on the sheet is the default: approve when every row was shown, and when any was not,
  request changes (`enter request changes`), with `prove` and that row's words already typed.

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
steered, `S` does nothing. Words typed into the item's chat while it runs steer it the same way
(see talk to the manager).

## who is the manager of an issue — the item's chat runs it

Every item's run has a manager: the item's own chat, the conversation `T` opens. When you run an
item (`r` or `L`) that has no chat yet, the run makes it first, exactly as `T` would, without
asking any model. Either way the chat is the **lead** of the item's team (`#12 · <title>`, under
the one `factory` team), and each stage's conversation joins that team as a member, so the teams
rail and the team menu show the manager above its stages.

Its opening brief tells it so, in six blocks, before the item's facts (its read, its stages with
their asks and loops, the recipe's policy and habits, budget and thinking):

    You are the manager of #12 in acme/web.
    You know: the issue and its comments, what codeaf read of it, the repository's recipe, policy and habits, the checkout, and what the person has said here.
    You do: shape the run, start it when asked, report each stage here, answer the person, and hold at each approve step until the person says continue, saying what you wait on.
    Shape the run with factory_run: stages are one lowercase word each, at most nine. Each stage has an ask that says what done looks like, a loop (until, rounds, fanout) and one line of why. Keep the recipe's stages unless the item says otherwise; change asks before adding stages; add a stage only for work no existing stage covers. Never drop proof. An approve step is where the run holds until the person says continue: keep the recipe's, add one (named approve) after a stage the person wants to read first, and skip one only when the person asks. Nothing posts outward before an approve step.
    You are the inbox: every question a stage asks comes to you first, and you reply with factory_answer. Answer it yourself when the issue, the recipe, the stages, the notes or what the person said here settle it. Send it to the person (ask_person, one line of why) when it turns on taste, scope, risk, credentials or access, money, or anything those do not settle; the stage waits, and the person answers here.
    Say what you set in three lines at most, then stop. Do not narrate.

What the manager does:

- **It hears every stage.** The run writes one line into the chat per event (`plan started`,
  `plan done · 2m · $0.04 · …`, `landed · proof sheet ready · your approval`); the lines are
  listed under what the manager hears (the stage conversations page).
- **What you type there reaches the run** as its brief or its steer (see talk to the manager).
- **It is the inbox**: every question a stage asks reaches it first, and it answers with
  `factory_answer` or sends the question to you (see a stage asks a question).
- **It shapes the run with `factory_run`** (see the manager shapes the run), with no card; the
  budget, thinking and notes go through `factory_item`, whose card still asks you
  before anything lands. It cannot run, stop, approve or post: those stay your keys on the floor.

An item whose chat cannot be made (no folder known for it) runs without a manager; its log says
`the item's conversation could not be made: …`.

## a stage asks a question — the manager answers it, or asks you and the stage waits

A stage never asks you directly. When a stage's conversation asks a question (a clarification, a
choice, a confirmation or a judgement call), it goes to the item's manager first, and the stage
waits for the answer:

- the manager's chat says `plan asks: which storage shape should this use?`, and the runner gives
  the manager one turn on it, with the question, its answers and the stage's own pick;
- the manager replies with `factory_answer`. If the issue, the recipe, the stages, the notes or
  what you said in the chat settle it, it answers itself: the chat says
  `manager answered plan: sqlite, the recipe says so`, and the stage goes on at once with it;
- if it turns on taste, scope, risk, credentials or access, money, or anything those do not
  settle, the manager sends it to you with one line of why: the chat says
  `plan asks you: which storage shape should this use? · sqlite / jsonl · a taste call`, the
  item needs you with the question on it, and the stage waits. A manager turn that fails, takes
  longer than a minute, or calls neither also sends the question to you.

Answer in the manager's chat at any time, in any words (`sqlite`, `use jsonl, it streams`), or
with the floor's answer keys; the chat says `answered: …` and the stage goes on with your words.
The question and its answer go on the run's notes, so the stages after it read them too.

A pause keeps the question on the item: answer it while paused and the answer is kept
(`noted for plan: …`), the item shows paused again rather than needs you, and when you run it the
step carries on in its own chat, told your answer. The question also survives codeaf being
closed, and an answer given then is kept the same way. Unanswered, it is dropped when the stage's round
starts again, and the stage asks again if it still needs to. Permission prompts never take
this road. One question at a time: a second one waits for the first.

## the factory's verbs, keys that change an item

On your own floor the keys that change an item's own settings always work (`n`, `c` budget, `e` thinking, `w`, the stages, `space`, `d`), and `r` and `L` run it (see running an
item). Stop, pause, answers, steering, approve, request changes and re-run checks work in every
window too, whichever process runs the floor's items. `enter` on a row never runs anything; it
opens the item page. The peek names at most five of these for the row's state (see the peek's
verbs); `?` lists them all.

- **New:** `r` runs it; it holds at each approve step (see approve steps). `c` sets the
  **budget**, the most it may spend: $2, $5, $8, $15, $30. `e` sets the **thinking**, how hard
  the first stage's model thinks: none, cheap, strong. `w` opens `in words ›` for both at once
  (`$8, stronger`). The digit keys `1` to `9` switch a stage on or off and
  `s` adds a stage (named on the `?` sheet). `space` selects, `L` runs the selected (see select
  new items). On a terminal-made item `g` puts it on github too, or takes it off; on any other
  item with a page on github, `g` opens that page. `a` asks a thin item's author its questions.
  `d` **dismisses** the item: it leaves the floor until it changes.
- **Running or queued:** `x stop · space pause · S steer` (see steer a running item); `e` steps
  the running stage's thinking.
- **Needs you:** `y yes · n no · a in words`, and `S steer`, `x stop` (see answering a question).
- **Landed:** `s approve` on a clean sheet, `e approve with changes` when a row was not shown,
  `B request changes`, `v re-run checks`, `d diff` (see approve, request changes, re-run checks).
- **Any state:** `T chat` opens a chat about the item (see chat about an item), on the floor and
  on the item page alike; it is named only where the floor can make one. `u` refreshes it.
- **Anywhere:** `n new` opens `new work ›`, at the bottom of the rows column, on the repository the list shows (or the first one;
  on a floor with no items yet, the name of the folder this window was opened in), and the new
  item's card is under the cursor when it is made.

A key that takes words opens a one-line box at the bottom of the right column, just above the
peek's verbs (which stay the column's last row, one blank row between them), except `n`'s,
which stands at the bottom of the rows (and every box stands under the rows when there is no
peek): `enter` sends, `backspace` edits, `esc` cancels. After a key the bottom line says what
became of the item (`#12 is running`, `#12 stopped · branch kept`); a refusal, such as
`#1540 is on a bench; stop it first`, is said there in the floor's own words instead.

## what does ask me at mean, how do I make it stop after the plan

`ask me at` is gone (2026-10-09): where an item's run stops to ask you is an **approve** step
among its stages (see approve steps). codeaf's default already stops after the plan: plan →
approve → write. To stop somewhere else, add one there: `s` on the item page with
`after test, approve`, or ask the manager to (`hold for me after test`). To let a run go
without stopping, switch its approve steps off on the settings row; with none, a green proof
ships by itself. An item or recipe that still says `ask me at` (or `gate plan`, `gate ship`,
`gate none`) is read as approve steps.

## what is chat on an item, what was talk on the factory floor, what is select, what is dismiss — the floor's words

The floor names each key by what it does:

- `r` **run**: run every stage; it holds at each approve step.
- `T` **chat**: a conversation about this item with the issue loaded (see chat about an item).
- `space` **select**: tick the row to run several together; `L` **run selected**.
- `e` **thinking**: how hard the model thinks, the chat's Thinking dial.
- `c` **budget**: the most this item may spend.
- `1`-`9` **stages**: turn a stage on or off.
- `g` **open on github**, `u` **refresh** (read the issue again: size, cost, priority), `d`
  **dismiss** (take the row off the floor; it is then `dismissed`).
- On a landed item: `s` **approve**, `B` **request changes**, `v` **re-run checks**.
- `m` **foreman**, `U` **refresh all**, `h` **handover**.

These replaced older words on 2026-10-08: `T` was talk, `space` was mark, `t` was gate, `c` was
cap, `e` was effort, `u` was read again, `d` was hide, and the landed keys were sign off, send
back and check again; `p` (plan first) is gone, because the approve step after plan and `r` do
it. On 2026-10-09 `t` (ask me at) went too: an approve step is where the run holds.

## the ? key sheet — what the question mark key shows, every key on the factory floor and the item page

`?` on the floor or on the item page opens the key sheet over the page: every key that works
where you stand, beside its word, in four groups, `do` (the row's verbs), `set` (budget,
thinking, stages, in words, and on the item page `↑↓ settings`, the row they stand on), `also` (open on github, refresh, dismiss, new, foreman,
refresh all, handover, repos, recipe, rail, backlog, density, order, repo, the split, scroll)
and `move` (walk, or `↑↓ rows` on the item page, open, filter, tab next place, esc back, ? keys). Only keys the floor can do are
listed. The hint says `esc close`; `esc` or `?` puts it away and leaves you where you were. Other
letters do nothing while it is up.

## open the item on github — g

An item that came from GitHub has a page there, and the floor offers it three ways: its short
name on the row and on the peek is a link (where the terminal takes links, it opens on click,
and takes no extra room), the peek's dim meta row ends `github ↗`, and `g` on the item opens the
page in your browser. The `?` sheet names it `g open on github` where it works. After `g` the bottom line
says `opened #1662 on github`; on a machine with no browser it says
`could not open your browser` with the address to copy.

`g` is offered only when the floor can name the page; with no such door there is no key. On an
item typed into the terminal with `n`, which has no page on GitHub, `g` keeps its other meaning:
it puts the item on GitHub too, or takes it off.

## when the floor is doing something — the spinner

Everything the floor does in the background says it is happening, with the same braille spinner
the transcript uses (`⠋`), and nothing is drawn when nothing is in flight. The spinner turns for
as long as a read, a door or a poll is in flight, and never stands still while one is, so a
spinner that moves is work still going:

- **An item being read:** its priority cell spins and the fact after its state says `reading…`
  or `refreshing…`. An item waiting its turn in a whole-floor re-read does not spin: it shows a
  still dim `·` and `waiting to read`. The peek's read says `⠋ reading…` while the first read is out, and the item
  page's read line says `⠋ reading · 4s`.
- **`u` refreshes the item under the cursor.** The line above the keys says `re-reading #6…`
  until the read is over, then the bottom line says `#6 refreshed · ~$0.0004`. The item page's
  read line says when it was made and the key, `read 3m ago · u refresh`.
- **`U` refreshes all, and asks first:** `re-read 8 items · ~$0.004? [y] go · [n] not
  now` stands at the bottom of the rows. `y` goes, `n` or `esc` does not. While it runs the
  handover says `⠋ refreshing 8 items · 3 done`.
- **A source being read:** the handover says `⠋ reading Agent-Field/CodeAF · 1 of 3` (or
  `github · ⠋ polling` when the source does not say where it is), then `polled 14s ago`.
- **A stage running on the open item page:** its mark in the left column spins, and
  its time counts up each second (see the item page).
- **A key waiting on its answer:** `T` says `⠋ opening #1's conversation…`, `b` says
  `⠋ banking…`, saving the repositories or the recipe says `⠋ saving…`, and `R` says
  `⠋ asking gh…`, each until the answer arrives.

`u` and `U` are offered only where the floor can read items again.

## bank a habit after clean approvals

After three approvals in a row without edits, the bottom of the right column offers a habit:
`habit forming — 3 approvals without edits on codeaf` and
`factory PRs from your own issues self-ship when the proof is green? [y] bank it · [n] not yet`.
`y` writes that sentence into the repository's habits, under `## habits` in its
`.codeaf/factory.md`, the way a taught line is written: on a branch `factory/recipe-<word>` in a
temporary worktree, committed as `recipe: <the sentence>`, pushed when a remote exists and opened
as a pull request when `gh` exists, so a teammate reviews it like code. The note line says exactly
one of `written · pull request #<N> opened for the team`,
`written · branch factory/recipe-<word> pushed · open the pull request when you want`,
`written · committed on factory/recipe-<word> · no remote to push to` or
`written · .codeaf/factory.md (not a git repository)`. `n` puts the offer away. The offer comes only where banking can be written.

## no recipe in the repo yet — write it so the team shares it

When an item of a repository lands and that repository's checkout has no `.codeaf/factory.md`,
the bottom of the right column offers, once, where the habit offer stands:
`no recipe in codeaf yet` and
`write the recipe into the repo so the team shares it? [y] write it · [n] not now`.
`y` writes the recipe the floor runs for it today into the checkout's `.codeaf/factory.md`, and
the note line says `recipe written · .codeaf/factory.md on main` with the branch the checkout
stands on. `n` puts the offer away for that repository for good: the factory's store keeps the
no, so no window asks again. The offer comes only where the recipe can be written, a file that is
there already is never written over, and a habit offer standing at the same time is answered first.

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

- **`bank it`** writes the line and, in a checkout that is a git repository, **opens a change for
  the team** instead of writing silently, because `.codeaf/factory.md` is the team's law and a
  teammate should review a taught line like code. The line is written on a branch
  `factory/recipe-<word>` (the first word of the line; a taken name gets `-2`, `-3`) cut from the
  default branch, committed as `recipe: <the sentence as typed>` with no trailers, pushed when a
  remote exists, and opened as a pull request with `gh pr create` when `gh` exists (the title is
  the sentence, the body the one-line why and the line it adds). It is made in a temporary
  worktree that is removed afterwards, so your working tree, even a dirty one, is not touched and
  the checkout stays on the branch it was on. What happened is said once, on the card's line and
  in the answer, as exactly one of:
  - `written · pull request #<N> opened for the team`: the branch was pushed and `gh` opened the pull request.
  - `written · branch factory/recipe-<word> pushed · open the pull request when you want`: pushed, and `gh` is not there.
  - `written · committed on factory/recipe-<word> · no remote to push to`: committed on the branch, kept in the checkout's repository.
  - `written · .codeaf/factory.md (not a git repository)`: the file was written in place.
  A line the file already has changes nothing: `already in .codeaf/factory.md · nothing to change`.
  codeaf is told `<line> is in <repo>'s recipe for <kind>`
  (or `… is in <repo>'s policy`, `… is in <repo>'s habits`) and the card says `banked`.
- **`not now`**: `nothing was banked: the person said no.`
- **Words**: `the person changed it: <your words>` and
  `Nothing is banked. Propose it again with that` (card: `changed in words`).
- **No answer** for fifteen minutes: `nothing was banked: the card was never answered`
  (card: `expired · nothing banked`).

It writes only where codeaf knows the checkout; otherwise the answer is
`codeaf does not know where <repo> is checked out; open codeaf there once`.

## chat about an item — T, the item's own conversation

`T` (**chat**) on an item, on the floor or on its page, opens a conversation that belongs to
that item, so you can talk it over or plan it before you run it, or while it runs. **It is the
item's manager** (see who is the manager of an issue). Nothing makes it before the first `T` or
the item's first run; after that every `T` opens the same one, and the item keeps it.

The first press makes, on this machine and without asking any model:

- a team named by the item, `#12 · fix the ledger double count`, nested under one team named
  `factory` (made once, the first time any item gets a chat);
- one conversation in that team, in the folder where the item's repository is checked out (or
  this window's folder when codeaf does not know the checkout). It opens with the item in front
  of it: the title, the repository, author and tier, where it asks you, its budget and labels,
  the body, its stages as numbered lines, the factory's read, the sentence that makes it the
  manager (`You are the manager of this item. …`), and
  `This is the item's own conversation on the factory floor. Nothing here runs it; the person does that on the floor.`

You land in the conversation, as when you open one from its tab; the one you were in goes on
running behind it. The first `esc` on its empty box, with nothing running, takes you back to the
floor on the same row, however many turns the model has answered (and onto the item page if that is
where you pressed `T`). That way back is taken once; reopened later from its tab it is an ordinary conversation, and there `esc` means
what it always means, so a first `esc` arms the rewind (`esc again to rewind`).

**Where it appears:** a tab on the tab strip, like any conversation you open. Its team sits under
the one `factory` team in the team menu and on the teams rail, and that team is folded: a hundred
items with a chat are one `factory` row until you stand in it. When the item is dismissed with
`d`, its conversation is put away (home's archive line) and its team is closed.

`T` is not named, and does nothing, over `--host` or `--at` to another machine, from `--once`,
or on the still made-up floor: nothing there can make a conversation.

## the foreman — m, what should I take first, factory_floor, why can't my chat read the floor

`m` on the factory floor opens the foreman: one conversation for the whole floor, for deciding
what to take first ("what should I take first this morning and why?", "select the three
cheapest bugs"). **It is never made by default.** The first `m` makes it and every later `m` opens the
same one. It is named `foreman`, sits in the one `factory` team beside the items' own teams, and
opens with `You are the foreman of this factory floor.` and each repository's policy lines.
You land in it as with `T`, and the first `esc` on its empty box goes back to the floor.

It reads the floor with the `factory_floor` tool: `waiting` (items that need you), `risky`,
`cheapest` (ten, by estimate), `oldest` (ten), `all` (up to fifty), or one `item` in full (its
read, facts, stages, the question it waits on and the first 2000 characters of its body). A row
is the ref, title, kind, repository, size, estimate, priority and its reason, state, age and
risk; what is not known is left out.

**It proposes by selecting.** It selects new items, and the tool tells it
`selected #1 #4 #6 · press L on the floor to run them`. On the floor a row it selected wears the
accent lead, as a `space` select does, and the peek offers `L run selected`. What it selects is
kept with the floor on this machine, so it survives a restart, until you run or unselect it; a
selection on an item that is no longer new is dropped.

**It never runs, ships or posts.** Only a new item can be selected (the tool says
`#3 is running, and only a new item can be selected`), and `L` on the floor is the only way to run
them (see what the factory does not do yet).

**Only the foreman reads the floor.** `factory_floor` is on the foreman's belt and on no other
conversation's: an ordinary chat, an item's own conversation (`T`) and a stage do not have it.
A chat that wants something on the floor proposes a new item with `factory_add`, an item's own
conversation changes its item with `factory_item`, and to ask about the whole floor you open the
foreman with `m`. `m` does nothing over `--host` or `--at`, from `--once`, or on the still made-up
floor.

## changing an item from its conversation — factory_item, skip a stage, add an approve step, raise the budget, leave a note

An item's own conversation (`T`) is the item's hub. In it codeaf can propose a change to that
item with the `factory_item` tool: stages to add (`after review, read it for auth holes`, or
`after test, approve` for an approve step where the run holds for you), skip or switch on; its
budget in dollars (the tool's `cap`); its thinking (the tool's `effort`: `cheap`, `strong`,
`default`); or a note its stages will read. The card speaks the floor's words: `budget` and
`thinking`; only the tool's own field names (`cap`, `effort`) are the engine's. It is for the item the conversation is
about, and it is on the belt wherever `factory_add` is. (It replaced `factory_stages`, which
could change only the stages.)

**Nothing changes before `1`.** A card asks one question built from what changes:
`#1 · skip the review stage?`, `#1 · add an approve step?`, `#1 · raise the budget to $8?`, `#1 · add a note for the stages?`,
or `#1 · change the plan?` for several. Its body is only what changes: `now:` and `after:` for the
stages, `budget  $5 → $8`, `thinking  — → strong`, `note: …` and `why: …`.
It answers to `1 yes`, `2 keep it`, or words (`say what to change… (enter sends it)`). There is
no clock on it.

- **`yes`** applies it and codeaf is told the item now, only what is set:
  `#1 now: plan · approve · write · test · proof · budget $8`, and that the change is made. The
  card says `changed`.
- **`keep it`**: `nothing changed: the person said no.` (card: `kept as it was`).
- **Words**: `the person changed it: <your words>` and `Nothing changed. Propose it again with that`.
- **No answer** for fifteen minutes: `nothing changed: the card was never answered`.
- A change that leaves the item as it is asks nothing: `nothing changed: the item already runs that way`.

The recipe's bounds hold before any card: proof and a stage the policy names are never skipped,
a fixed stage never changes, a stage that has run is never touched, and under `fixed` a stage change is refused
with `nothing changed: the recipe for issue is fixed; plan may not change the stages` (and nothing
else in that card changes). Notes are kept on the item, and every stage's brief opens with them.
Nothing launches from it.

## the item card in a conversation — live item status, #12 in a reply

Wherever a conversation is about an item on the floor, the item is drawn as one live card:
`▤ #1 <title>` with `repo · kind · size` on the right, and under it where it stands —
`new · budget $5`, `running 24m · $1.42 / $5`, `? plan is ready`, `landed · 3✓ 1✕`,
`shipped · $1.90` — with its stages on the right (`○ plan  ○ approve  ○ write  ○ test  ○ proof`, the running
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
- **Why your issues are not showing up yet after you watched a repository:** the first read
  starts the moment you save the picker (`enter` on `R`), within about a second, wherever the
  poll on this machine runs; while it reads, the floor says which repository it is on, how
  many of how many are done and how many items it has listed so far. GitHub is polled every
  minute after that.
- **Rows arrive repository by repository as the read goes.** A repository's issues stand on
  the floor as soon as its list is in, before any comment is read, and its pull requests once
  theirs are; you do not wait for every repository to finish. A repository that fails to read
  keeps no one else's rows off the floor and is read again from where it was on the next tick.
- **On a busy repository the comments fill in over the next minutes.** One read spends at most
  40 requests on comments and on pull requests' lines and files; past that, items arrive
  without them and the next reads, a minute apart, fetch the next ones until all are in. An
  item whose comments are not read yet says `comments not read yet` on its page; one with
  none says nothing.
- **The token, in this order:** `GH_TOKEN`, then `GITHUB_TOKEN`, then a token kept in
  the profile, then what `gh auth token` answers, and `gh` is asked only after you have said yes
  to it on the floor. The token is never shown or logged.
- **No watched repository or no token: nothing is connected.** Nothing polls, and the floor's
  facts line says only `terminal · chat`.
- **Who polls:** the codeaf window you are sitting in, whether or not a background engine
  holds your chats, and the engine too when it has the floor open. They take turns through the
  floor's lock, so only one of them reads GitHub at a time, and only one of them reads new items
  for triage, so a read is never paid for twice.
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
  and never where it asks you, its budget or its stages. A changed title or text clears the item's read so triage
  reads it again; a label change alone keeps the read. A dismissed item that changes comes
  back as new.
- **The facts line** says `github` and `polled 4m ago`; a failed read says
  `github · not reachable` (or `token refused`, `repository not found`).
- **Nothing is ever written to GitHub by itself.** The poll only reads: no comment, label,
  close or pull request is made by it.

## after connecting github — I selected repositories but see no issues and nothing loading, what do I do now

Nothing: an empty floor right after you watched repos is the read starting. Once you watch
repositories the floor reads them for you, starting the moment you save
with `R`, and it says the read is loading while it runs. The bottom line says
`watching 3 repositories · reading them now` for as long as the handover says it is reading,
and from then until rows stand the floor says which of four things is true, one sentence each:

- **The first read is under way.** From the moment you save, before the read has said
  anything, the handover's line is `⠋ reading the repositories you watch`, the floor reads
  itself every second, and it keeps saying so until the read reports where it is or rows stand.
  Once the read reports, the handover's line says where it is,
  `⠋ reading Agent-Field/CodeAF · 1 of 3`, and counts the items listed so far once there are
  some, `⠋ reading Agent-Field/CodeAF · 2 of 3 · 200 items so far`; under it the floor says
  `reading the repositories you watch · rows stand here as issues and pull requests arrive`.
  It does not say `quiet` while a read is out: quiet means nothing is happening and nothing
  happened. Each open issue and pull request becomes a row as its repository is read, one
  repository's rows at a time, and comments keep filling in for a few minutes after.
- **The read has not answered.** After half a minute with no word, the spinner stops and the
  handover's line says `no word from the read yet` (then the source's trouble, if any), and under
  it `the read was asked for and has not answered · R to check the repositories · if codeaf was just updated, restart it`.
  The bottom line keeps `watching 3 repositories` without `reading them now`. Never `quiet`:
  check the list with `R`, and restart codeaf if it was just updated.
- **The read is done and nothing is open.** The floor says
  `nothing open in 3 repositories · github polls every minute · n adds work by hand`: the
  repositories you watch have no open issue or pull request. A new one arrives within a minute of
  being opened on GitHub, and `n` makes an item by hand meanwhile.
- **Nothing is connected.** The page says
  `nothing connected yet · the factory floor arrives here when a chat splits work off or a repo is connected`;
  see connecting github.

A floor with a source but no watched repository says
`work arrives here from chat, from n, and from the repositories you connect`. With the four-row
handover (`h`) the facts row carries the same progress in the source's own clause,
`github · ⠋ reading Agent-Field/CodeAF · 1 of 3`, and the shift row says
`nothing happened while you were away` without `quiet` while the read is out, unanswered or
failing; a failing read's line says `github · not reachable`.

## does the floor show only new issues or every open one

Every open one. The first read takes every open issue and pull request of each repository you
watch, however old, and later reads take what changed. `NEW` on the floor means codeaf has not
done anything with the item yet, not that it is new on GitHub.

## my chats run but the factory never fills — the engine is another build

The background engine that runs your chats outlives the codeaf that started it, so after you
install or build a new codeaf, the engine can still be an older build, without the factory's
tools or its manual page. Your window attaches only to an engine of its own build.
Any other engine, including one built from a changed working tree, a `dev` build, or one that
cannot say which build it is, does not get your conversations: the window opens them in itself,
the way `codeaf chat --no-host` does, reads the floor itself, and says so once on the notice
line, for example
`this workspace's engine is another build (0ld0ld00 built 2026-10-06 09:00) · this window runs its own · the old engine keeps the chats it already has; stop it when they are done: codeaf engine --stop --workspace '/srv/app'`.
The old engine keeps every chat it was already running, and nothing is stopped for you. When
those chats are done, run that command; the next codeaf you open starts the engine from your
build.

## what github gives an item — comments, changed files, check runs, its page

Each GitHub item on the floor carries, beside its row's words:

- **Its page:** the issue's or pull request's address on GitHub, which opens it in your
  browser. An item typed in the terminal or split off a chat has none, and opening it says
  `this item is not on github`.
- **The last three comments**, oldest first, each kept to its first 1000 characters, with who
  said them and when. They are read only when the item is new or its comment count changed, so
  an item nobody commented on costs no extra request. On a busy repository they arrive over
  the minutes after the row does, and until then the page says `comments not read yet`.
- **For a pull request, the changed files:** the 20 with the most changed lines, each with its
  added and removed lines, read once each time the pull request changes.
- **For a pull request, the check runs** on its head, by name, each with its state (`success`,
  `failure`, `in_progress`, `queued`) and its page. The row's short `ci ✓` stays the summary.
- **Its activity**, the last 20 things that happened to it: `arrived from github`,
  `changed on github` (its title, text or labels changed), `read` (triage read it), `talked` (its
  own conversation was made with `T`), and `stages changed by plan`.
- **While github is being read** the floor says so; it is marked for the length of each read and
  cleared when the read ends, or when codeaf next starts after a crash.

## refresh an item — u, refresh all — U

`u` (**refresh**) reads the item in front of you again from GitHub now, without waiting for the next poll: the
issue or pull request by its number, its last three comments (read even when the count did not
move), and a pull request's files and check runs. Then its read is cleared, so triage reads it
again. A terminal item has nothing upstream; `u` only clears its
read. While it runs the row says `refreshing`; while triage reads an item it says `reading`.

`U` (**refresh all**) does the same for every item on the floor except the dismissed, in turn, and says
`refreshing 8 items · 3 done` while it runs. **Before it starts, the floor shows what it would
cost:** the number of items times the average cost of a triage read here, or $0.0005
an item when no read has been priced yet. Nothing is read or spent until you say yes. Each read it
causes is a call on the cheapest seat, and a row on the spend ledger named `factory triage`.

With no GitHub connected, `u` on a GitHub item says `github is not connected`. `u` and `U` are on
your own floor only, never on the still fixture.

## choose which repositories the floor watches — R, find a repo in the list, why a repo says here

`R` on the floor opens a list over the floor headed
`watch · repositories github sees as <login>`, with `3 watching · 143 listed` at the right. Under
it is the filter line, `› type to filter`, and then the repositories in sections:

- **`WATCHING` first:** every repository you already watch, in one place at the top, so the
  ticks are never scattered through a long list. A watched repository GitHub no longer lists
  stays here so you can untick it. A repository you tick now stays where it is until the list
  opens again, so the cursor never jumps.
- **Then one section per owner** (your account and each organisation), owners ordered by their
  most recently pushed repository, and inside each the most recently pushed first.
- **Each row** is `[x] owner/name` (watched) or `[ ] owner/name`, then, each in its own column:
  `12 open` (open issues and pull requests together, as GitHub counts them), `private`, `here`
  and `pushed 3m`. A column no row has anything in is not drawn.

To find a repository in the list, just type: any key the list does not use goes into the
filter, no `/` first (a `/` typed first works too). Words match anywhere in `owner/name`, case
aside; `agent-field/` keeps only that owner's repositories, and `agent-field/api` that owner's
repositories with `api` in the name. Nothing is scored: the list keeps its sections and order.

A repository says `here` when it is checked out on this machine and codeaf knows where, and
`clone on first run` when it is not: the floor's stages run only in a checkout, so the first
`r` on one of its items asks to clone it into `~/.codeaf/v3/factory/repos` (see it says CodeAF
is not checked out). The list's last line says so for the repository under the cursor:
`not checked out here · it is cloned on the first run`, or `checked out at <folder>` for one
that is. Where codeaf cannot clone, the column is blank and the line says
`codeaf does not know where owner/name is checked out · it is watched and read, and its stages
wait until it is cloned`.

- `↑` `↓` walk, `space` ticks or unticks, `enter` saves, `ctrl+o` orders every section by how
  many are open (the most first) and again back to the last push, `backspace` takes a letter off
  the filter, `esc` clears the filter and then closes without saving:
  `space watch · enter save · ctrl+o by open · esc close · type to filter`. `o` is always a
  letter, so a name that starts with one is typed like any other; `ctrl+o` is offered only when
  some repository has an open count.
- Saving writes `repos.json`, starts the first read and says
  `watching 3 repositories · reading them now`
  (or `watching no repositories · the floor keeps what chat and n bring`); see after connecting
  github for what the floor says while the read runs.
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
- Where codeaf knows the checkout: `1-9 stages` switch a stage on or off, `s add a stage` adds
  one in words (`+ stage ›`), `e thinking` steps its thinking, `w` sets its knobs in the recipe
  file's own words
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

What it never does: run, dismiss, label, comment, change where an item asks you, its budget or a stage, or ask you
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
- **Items run on this machine only.** `r` and `L` run them (see running an item), and stop,
  pause, answers, steering, approve, request changes and re-run checks work from any window on
  it.
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
- **The foreman only reads and selects.** `m` opens it (see the foreman); it cannot run,
  ship, post or change an item, and the person's `L` is the only way to run its selection.
- **`CODEAF_FACTORY_FIXTURE=1`** shows a still, made-up floor (three repositories, ten items)
  in place of yours, with no verbs.
- **Over `--host` or `--at` to another machine** the page draws `nothing connected yet`, the
  chat has no `factory_add`, and nothing polls GitHub.
- **`Factory ? N` on the tab bar appears only after the first open** of `/factory`.

## running an item — r and L, where the run stops

`r` runs the item under the cursor: every stage, holding at each approve step (see approve
steps). `L` runs every selected item (or this one). There is no plan-first key: the default
holds after the plan already. The bottom line says `#12 is running` once the floor's next read
sees it start, and `#12 is queued · a bench frees it` only when that read still finds it queued
behind full benches. A run item is `queued`, takes one of the floor's
benches (four at once; `CODEAF_FACTORY_BENCHES` pins another number) and runs its stages in
order. It lands with its proof sheet for you to approve, or, with no approve step among its
stages, ships by itself when every claim was shown.

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
verb: run, stop, pause, answer, steer, approve, request changes and re-run checks. A window that is not
the owner hands the verb to the owner, which carries it out within about a second, and a refusal
(the day rail, `#12 has landed · approve it, or request changes`) is said on that window's bottom
line in the owner's own words. If nothing answers within five seconds the window says
`the floor's runner did not answer · this window's engine may be another build · restart codeaf`
and nothing happens later. The usual cause is a window attached to an engine of another codeaf
build, one that does not run the floor; quit codeaf and start it again so both are the same build.

**What a stage may do.** A chat stage runs with the allow posture (what `--yolo` gives) in the
item's own worktree, never your checkout, so it edits files and runs commands without asking; your own approval
settings do not narrow it (see what a stage may do).

**Spend.** A chat stage's calls are on the spend ledger under its own conversation, and the
item's spend counts them against its budget and the day rail.

## it says CodeAF is not checked out — clone it, where does it go

A run never starts without a checkout. `r`, `L` and `L` on the foreman's selection first look
for where the item's repository is checked out on this machine. When codeaf knows no folder for
it, nothing starts and the question stands at the bottom of the floor, such as
`CodeAF is not checked out on this machine · clone it into ~/.codeaf/v3/factory/repos? [y] clone · [n] not now`.

- `y` clones it, saying `⠋ cloning Agent-Field/CodeAF…` while it runs (a large repository takes
  a minute), into `~/.codeaf/v3/factory/repos/<owner>/<name>` (`v3/factory/repos` under
  `CODEAF_HOME` when that is set), records that folder as the checkout, and then runs the item.
  It uses `gh repo clone` when gh is installed, else `git clone https://github.com/<owner>/<name>`,
  over the connected GitHub account. A clone that fails says
  `could not clone Agent-Field/CodeAF · <git's last line>` and nothing runs.
- `n` (or `esc`) runs nothing and says
  `not run · clone CodeAF first, or tell codeaf where it is in the repos list`.
- Where codeaf cannot clone, it says
  `not run · codeaf does not know where CodeAF is checked out · open it from that folder once`.

`L` over several repositories without a checkout asks once per repository, in turn; a `y` to
each clones it, and the items run once every one has a folder. A `n` keeps the selection.

**Your own checkout comes first.** Opening codeaf inside a watched repository records that
folder as its checkout, and a repository with any known folder is never cloned. The repos list
(`R`) says `here` for a repository checked out on this machine and `clone on first run` for one
that is not.

## where an item's work lives — a worktree and a branch per item

An item never works in your checkout. The first round that needs a folder makes the item its
own git worktree, `~/.codeaf/v3/factory/work/<repo>-<number>` (such as `work/api-12`), on its
own branch, `factory/<number>-<slug>`, the slug from the title, at most forty characters (such
as `factory/12-total-double-counts`). Every later round, and every stage, works in that same
folder, so four items running at once never write over each other. The item's log says
`branch: factory/12-total-double-counts` once, when the worktree is made.

The branch starts from your remote's default branch (`origin/HEAD`) when git knows it, else
from the branch your checkout is on. **A pull request's branch starts at the pull request's
head**: codeaf fetches `refs/pull/<number>/head` from `origin` (GitHub serves it for a fork's
pull request too) with the base branch beside it, so the change is `git diff origin/<base>...HEAD`
in the worktree. A worktree made before that, or one whose pull request was pushed to since, is
moved to the head the next round when nothing of its own is on it, and the log says
`at the head of #12: <commit>`; one a step already committed into stays where it is. A
repository with no `origin` cuts a pull request's branch the way it cuts an issue's, and a fetch
that fails says `could not fetch the head of #12: <git's last line>` and the stage does not run. **Your checkout is never touched**: uncommitted changes
in it stay where they are, are not an error, and do not follow the item.

A `pr` post stage pushes the branch first, with `git push -u origin <branch>` and never with
force, then opens the pull request from it. A failed push says
`could not push factory/12-…: <git's last line>`; a repository with no `origin` says
`#12 has no remote to push to`. A worktree git could not make says
`could not make a worktree for #12: <git's last line>`, and the stage does not run.

Stop, approve and request changes all keep the worktree and the branch. Nothing deletes either yet: to
clean one up, `git worktree remove <folder>` in your checkout, and the branch stays.

## where factory items are saved

Factory items are saved on this machine, in the v3 factory folder under the codeaf home:
`~/.codeaf/v3/factory`, or `v3/factory` under `CODEAF_HOME` when that is set. Each item is one
small file named by its number, such as `1.json`, and a change rewrites that file. They are not
in any repository and not on any server; nothing is posted, synced or uploaded. The same folder
holds `repos.json` (the GitHub repositories you watch), `repos/` (the repositories a run cloned
because this machine had no checkout of them), `sources.json` (when GitHub was last
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
