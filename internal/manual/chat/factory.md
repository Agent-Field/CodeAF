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

- **120 columns and wider**: the handover runs across the whole width at the top, then a blank
  row, then two columns: the rows on the left (58% of the width to start with, never under 70
  columns), a dim `│`, and the **peek** on the right (never under 40 columns), which shows the
  item under the cursor. The divider moves (see resize the split).
- **90 to 119 columns**: the handover on top, then the rows across the full width, with no peek.
  Press `enter` on a row to see the item whole.
- **Under 90 columns**: no handover; each row is only its mark, its short name and its title.

The rows window follows the cursor when they do not all fit.

## the keys on the factory floor

The bottom line names the keys that work for the item under the cursor. On the still made-up
floor, which has no verbs, it names the walking keys:
`↑↓ walk · enter open · space mark · / filter · [ ] repo · A backlog · z density · O order · by obligation · E recipe · esc back`.
On a floor that can be changed it names `enter open` and the item's own keys first (see the
factory's verbs), and when the line is too long the list keys are the first to go.

- `↑` and `↓` (or `ctrl+p` and `ctrl+n`, or the wheel) walk the items.
- A click on a row moves the cursor there and the peek shows it; a second click on the same row
  opens it. A click in the peek moves nothing.
- `enter` opens the item under the cursor on its own page (see the item page).
- `J` and `K` scroll the peek's description a row down and up, `pgdn` and `pgup` a page
  (see read the whole issue).
- `{` and `}` move the divider between the rows and the peek, and `|` puts it back (see resize
  the split).
- `z` switches between compact rows (one line each, the default) and comfortable rows (a dim
  second line under each row with the factory's one-sentence read of it, and a blank row between
  items). The choice is not saved; every launch starts compact.
- `O` changes the order of the rows inside each group (see order the floor).
- `space` marks or unmarks the new item under the cursor (offered only on a new item).
- `/` opens a filter box at the top of the list.
- `[` and `]` show one repository at a time, then all of them again (offered with two or more).
- `A` shows the whole backlog of new items, or only the recent ones.
- `esc` clears a filter or a repository first; pressed again, it goes back to the conversation.
- `R` chooses which repositories the floor watches, `E` opens the recipe page, and `$` sets the
  day rail (each has its own section). They are named last on the line, `R repos · E recipe ·
  $ rail`, and are the first to go when it is too long; a key the floor cannot do is not named.

## what a row is

A row on the factory floor is one **item**: an issue, a pull request, a red CI run, a chore,
or work a chat split off. Rows are laid out in fixed columns so they line up down the page:
a mark, the short name right-aligned (`#1538`, or `ci` for a red CI run), the title (cut with
`…`), the repository's short name (`codeaf`), the facts, and at the far right how long ago the
item last moved (`12m`, `7h`, `3d`). Under 90 columns a row is only the mark, the short name and
the title.

The rows are grouped under muted capital headings with a count and a dim line out to the edge
(`NEEDS YOU · 1 ───`), in this order, with one blank row between groups. A group with nothing in
it is not drawn at all:

- `NEEDS YOU`: waiting on your answer. Its mark is `?`; its first fact is the question itself,
  in the question colour, and `[y/n]` stands just before the age.
- `STREAMS`: running items (a half-filled circle) and queued ones (`○`). A running row's first
  fact is a strip with one mark per stage (`●` done, `◐` running, `○` to come) and the stage it is
  in with how long it has run: `review 26m`. A queued row says `queued`.
- `NEW`: arrived and not started; no mark, except `✕` on a red CI run. The first fact is its kind
  and size: `bug · S`, `pr · ci ✓`, or `ci red`.
- `LANDED`: finished, waiting for your sign-off (`✓`). The first fact counts its claims: `3✓ 1✕`.
- `SHIPPED`: signed off (a dim dot), with `shipped 06:00`.

After the first fact come, in this order: the money (`$1.42/$5`, spent against the cap, or `~$3`
estimated), the author (`olu (stranger)` when the author is a stranger), and tags: `factory`,
`thin` (too underspecified to run without questions), `dup #950?`, `from chat ▸` and
`terminal only`. A row carries at most four facts and never wraps: as the window narrows, facts
are dropped whole from the right. The row under the cursor sits on a lifted background.

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
and when you leave the page and come back, until codeaf exits. `L` launches every marked item
(or the item under the cursor when nothing is marked) and clears the marks.

## stages, not a pipeline

An item runs through **stages** in a fixed order that a repository or team writes once: for
example plan, write, test, review, neaten, proof. Each stage is one sentence ("read it as a
stranger would"), and it runs as an ordinary task with the ordinary crew picked for that kind of
work. There is no graph to draw and no model to choose per stage; the one word a stage may carry
is its effort, cheap or strong.

A stage can repeat until a condition holds (until green, until clean) up to a number of rounds,
and then it stops and asks you. Nothing inside a stage can add a stage, except plan, within the
bounds its recipe word allows (see adapt). A stage can also be a gate: `plan` comes back with the plan before any code, and `ship` waits for your sign-off.

The peek shows a new item's stages as one line of names and a running item's as a strip of
marks; the item page lists them down its stage rail. A stage can carry a condition (`thin`,
`large`, `touches auth`, `has ui`); on an item it does not fit, the stage is drawn dim, with
`· skipped` on the stage rail and `skipped · not thin` (or the condition it missed) beside it.

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

The **handover** is what happened on the factory floor since you last looked. It is four rows
and a blank row across the whole width at the top of the floor, above the rows and the peek. It
is drawn at 90 columns and wider, and only when the window is tall enough to leave the rows at
least six lines under it.

1. A muted heading with a line out to the edge: `◆ handover · since 23:12 · 7h 12m · $8.44`,
   when the stretch began, how long ago, and what it spent. The spend is left off when nothing
   was spent.
2. What happened: `✓ 2 shipped #1661 #1663 · 3 arrived · 1 question handled`. A part whose
   count is zero is left off. When nothing at all happened the row says
   `quiet · nothing happened while you were away`.
3. What waits on you: `? 2 waiting on you`, in the question colour, or
   `nothing waits on you`. At the right, labelled `24h`, a sparkline of the last
   twenty-four hours of activity, one cell an hour, the current hour last. It is not drawn
   when no hour had anything in it.
4. The floor in one dim line: `3 repos · github · chat · benches 2/6 · polled 4m ago`, how many
   repositories, which sources are connected, how many benches are busy out of how many, and
   when a source was last read. At the right, the day's money: `$11.31 / $60 today`, what was
   spent against the day's limit. With nothing spent today it draws only `/ $60 today`. The
   made-up moving floor adds how fast its clock runs, such as `· 150×`; a real floor never does.

A narrow window drops the shipped items' names first, then the last facts on a row; the money
keeps its place.

## the peek

At 120 columns and wider, the right column of the factory floor shows the item under the cursor
as a short document: blocks with one blank row between each; a block with nothing to say, or a
zero, is left out along with its blank row. In order:

```
#1 Total double-counts an entry added twice
factory-demo · bug · S · santosh · 8h

Add appends without checking the id, so Total sums the pair.

touches money      maybe a duplicate of #7      thin

gate  ship      cap  $5      effort  —

● plan    ◐ write    ○ test    ○ review    ○ proof

When the same entry id is added twice, Total counts it
twice. The ledger should treat the second Add as a no-op …

» talk

enter open · space mark · d hide
```

1. The short name and title, then dim: repository, kind, size, author, age.
2. For an item that needs you, the question (`?` in amber) and `[y] yes · [n] no · [a] in words`.
3. The factory's one-sentence read of it.
4. Facts, dim, with no labels: what risky ground it touches, `maybe a duplicate of #7`, `thin`,
   `stranger`. Too wide, they drop from the right.
5. The chips: `gate`, `cap` and `effort`; no cap when there is none, `—` for no effort word.
6. The stages: `●` done, `◐` running (round, minutes left), `○` to come; a new item's are the
   ones it would run. What plan changed follows on a dim row.
7. A running item's stage line, such as `review 1/2 · 3 findings · fixing`;
   `queued · benches full · a bench frees it`; or `merged 06:00 · $1.90`.
8. The description, at most six rows, cut with `…` and a dim `▾ more`. A landed item shows its
   claims and policy rows here instead.
9. `talk`, only when the item has its own conversation.

The bottom row names the keys, starting with `enter open`: `space mark · d hide` for a new item
(plus `r run · p plan first` where a floor can launch), `y n answer · a in words · x stop`,
`s steer · p pause · x stop`, or `a ship anyway · c send back · o check again`.

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
with `issue`, and its pane is the whole description wrapped at 72 columns, then the factory's
read, the facts, and, for a thin item, the questions it would ask the author. `J`, `K`, `pgdn`
and `pgup` scroll it, and its bottom row says `J K scroll` while there is more to see.

## the item page

`enter` on a row of the factory floor (or a second click on it) opens that item on its own page,
across the full width; `esc` closes it and puts the cursor back on the same row. Nothing about
the floor (filter, repository, marks) changes while it is open.

The top two rows are the item: its short name, title, repository, author, where it stands and for
how long (`running 26m`), with spend over the cap on the right (`$1.42 / $5`); then the chips with
their keys, `gate ship [t] · cap $5 [c] · effort — [e] · places: codeaf`. When plan changed the
item's stages, a third dim row says what it changed (see adapt). A blank row follows.

On the left is the **rail**: `issue` first (see read the whole issue), then `talk` when the item
has its own conversation, then one row per stage: `●` done (with `×3` when it split into
tasks), `◐` running (with its round, `review 1/2`), `○` to come, a stage switched off dim, and a
stage whose condition does not fit dim with `· skipped`. The page opens on the stage waiting on
you, else the running stage, else `proof` for a landed item, else `issue`. `↑` and `↓` (or `←`
and `→`) walk it, and a click on a rail row selects it. Under 72 columns the rail is one line
above the pane.

On the right is the row under the cursor. For a stage: its settings, dim
(`review · chat · until clean · max 2 · fanout per-finding · when always`), what it is asked to
do, a blank row, what it has to say (`runs after test`, the running log, the question waiting on
you, its result, or the claims on `proof`), a blank row, and the item's keys on the last row.

Every verb key keeps working on the item while its page is open (see the factory's verbs):
`t c e s w b g a d x y n o p r L`, and `1` to `9` switch stages. The hint line names them,
after `↑↓ stages`, and ends with `esc floor`. A box that takes words opens on the last row of
the pane.

## ship or send back from the item page

`enter` on a stage of the item page does not open it yet; it says
`the stage's conversation opens here once streams are conversations` on its own line above the
keys. The one exception is `proof` on a landed item, which is the item's sheet: there `enter`
ships when every claim was shown (`enter ship`), and when any was not, **send back is the default
key** (`enter send back`) and opens the `send back ›` row with `prove` and that claim's words
already typed; `enter` again sends it. `a` ships anyway, `c` sends back in your own words, `o`
checks again.

## the factory's verbs, keys that change an item

On the made-up moving floor every key below works; the bottom line names only the keys that
work for the item under the cursor. On your own floor in the shipped binary the keys that change
an item's own words work (`n`, `t c e` chips, `w`, `1-9` and `s` stages, `space`, `d`) and
nothing that launches, steers, answers or ships is offered on the bottom line, because nothing
can launch yet. `enter` on a row never launches; it opens the item page.

- **New:** `r run · p plan first · space mark · L launch marked · 1-9 stages · s stage ·
  t c e chips · d hide · n new`. `r` launches with the ship gate, `p` with the plan gate, and
  `L` launches every marked item (or this one). `t` cycles the gate plan, ship, none; `c` the
  cap $2, $5, $8, $15, $30; `e` the first stage's effort: none, cheap, strong. `w` opens
  `in words ›` for chips (`$8, plan first, stronger`). `g` puts a terminal-made item on github
  too, or takes it off. `a` asks a thin item's author its questions. `d` hides the item.
- **Running or queued:** `s steer · p pause · x stop · e effort`. `s` opens `steer ›`; `p`
  pauses and resumes; `x` stops and keeps the branch; `e` cycles the running stage's effort.
- **Needs you:** `y n answer · a in words · s steer · x stop`. `a` opens `answer ›`.
- **Landed:** `a ship anyway` when a claim failed, `c send back`, `o check again`, `d diff`.
  Shipping from a clean sheet is `enter` on its proof, on the item page. `d` says
  `the diff is the appendix` and that it opens in your editor later.
- **Anywhere:** `n new` opens `new work ›` on the repository the list shows (or the first one;
  on a floor with no items yet, the name of the folder this window was opened in), and the new
  item's card is under the cursor when it is made. On the made-up moving floor,
  `S sleep 8h` jumps its clock eight hours and starts a new handover.

A key that takes words opens a one-line box at the bottom of the right column (or under the
rows when there is no peek): `enter` sends, `backspace` edits, `esc` cancels. A refusal, such as
`#1540 is on a bench; stop it first`, is said on the bottom line beside the keys.

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

Calling it adds nothing. A card asks you first:
`wants to put this on the factory floor: <title>`, with the repo, kind and size under it and
the chat's reason. It answers to `1 add it`, `2 not now`, or words typed into its box
(`say what to change… (enter sends it)`).

- **`add it`** writes one item to the floor as `new`, from chat, and the chat is told
  `#<id> <title> is on the factory floor`. The floor redraws with it.
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

**Nothing is written before `1`.** A card asks first:
`wants to add to <repo>'s recipe for <kind>: <line>` (or `wants to add to <repo>'s policy: …`,
`wants to add to <repo>'s habits: …`), with `recipe · <repo> · <kind>` under it. It answers to
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
  times; for a pull request also its checks (`ci ✓`, `ci running`, `ci ✕`) and `+N −M`
  lines. Size is S, M or L (up to 50, 400 changed lines, or more). The author is `owner` for
  your own login with write access, `collaborator` for others with write access, otherwise
  `stranger`.
- **A change on GitHub updates the row's words** (title, text, labels, checks, lines) and never
  your gate, cap, stages or triage. A dismissed item that changes comes back as new.
- **The facts line** says `github` and `polled 4m ago`; a failed read says
  `github · not reachable` (or `token refused`, `repository not found`).
- **Nothing is ever written to GitHub by itself.** The poll only reads: no comment, label,
  close or pull request is made by it.

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
no rail set there is no `/ $N` at all. The rail is a figure to read against; nothing stops at it
yet. `$` is not offered on a floor whose store cannot keep it.

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
title, its text cut to 3000 characters, its labels, its kind and its repository, and answers:

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
Reading does not change a row's age.

What it never does: launch, hide, dismiss, label, comment, change a gate, cap or stage, or ask you
anything. It is facts drawn dim, never a decision. It runs only in the window that opened the
floor, one window per machine, and only while a key resolves; with no key there is no reading
at all. Each call is a row on the spend ledger named `factory triage`, on the low seat.

## order the floor — O

`O` on the factory floor cycles how rows are ordered inside each group: `by obligation` (the
default: newest first, as the floor has always been), `first` (the triage priority, 1 before 5,
older first on a tie, unprioritised items last), `age` (oldest arrival first) and `cost`
(cheapest first, by what the item has spent or else its estimate; items with neither last). The
groups never move: `NEEDS YOU` stays on top in every order.

The bottom line names the current order, `O order · first`. In the `first` order each row shows
the triage reason dim at the far right, before the age (`main is red    6h`); it is the first thing
a narrow row drops, before any fact. `O` does nothing on the item page. The choice lasts until
codeaf closes, like the density; it is not saved.

## what the factory does not do yet

Be plain about this when asked:

- **Items arrive three ways:** from a chat with `factory_add` (after you answer its card), from
  `n` on the floor, and from the GitHub repositories you watch (see connecting github). They
  are kept on this machine and are still there next launch.
- **Chips and stages can be set; nothing launches.** On your own floor `t c e`, `w`, `1-9`, `s`
  and `d` change an item, and no key launches, steers, answers, signs off or sends back. Those
  verbs work only on the made-up moving floor, which needs a development build made with
  `-tags factorymock` and started with `CODEAF_FACTORY_MOCK=1`.
- **Only GitHub is connected.** No GitLab or Linear.
- **The recipe file is read only where codeaf knows the checkout.** It is read for the
  repository you opened codeaf in, and for any watched GitHub repository whose checkout codeaf
  has seen you open. Every other repository runs the default recipe until then, and for those
  `b` is not offered, no habit is offered for banking, and the recipe page (`E`) is drawn but
  not changed.
- **The day rail is read, not enforced.** Nothing stops when the day's spend passes it.
- **Nothing posts anywhere.** Writing to GitHub (a comment, labels, a close, a pull request)
  happens only from a post stage, and post stages do not run yet.
- **A stage's conversation does not open yet**, and neither does the diff. `enter` on a stage
  says `the stage's conversation opens here once streams are conversations`.
- **Nothing launches from the chat.** `factory_add` only puts an item on the floor as `new`.
- **The foreman does not open yet.** There is no conversation with the factory itself.
- **`CODEAF_FACTORY_FIXTURE=1`** shows a still, made-up floor (three repositories, ten items)
  in place of yours, with no verbs.
- **Over `--host` or `--at` to another machine** the page draws `nothing connected yet`, the
  chat has no `factory_add`, and nothing polls GitHub.
- **`Factory ? N` on the tab bar appears only after the first open** of `/factory`.

## where factory items are saved

Factory items are saved on this machine, in the v3 factory folder under the codeaf home:
`~/.codeaf/v3/factory`, or `v3/factory` under `CODEAF_HOME` when that is set. Each item is one
small file named by its number, such as `1.json`, and a change rewrites that file. They are not
in any repository and not on any server; nothing is posted, synced or uploaded. The same folder
holds `repos.json` (the GitHub repositories you watch) and `sources.json` (when GitHub was last
read). A chat's
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
