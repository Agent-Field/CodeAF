# Home — everything on this machine, in panels

## See all my projects — /home

Type `/home`. It takes the whole screen and shows **everything on this machine, from every
project**, not just the folder this window was started in — as seven **panels**, each
answering one question you would ask walking up to a colleague's desk:

```
 codeaf                                                      $0.14 / $20 · thu 9:49am
  home   tasks   spend   settings
 ───────────────────────────────────────────────────────────────────────────────────────

 Understanding Hash Tables                  2m    projects
 Understanding Bloom Filters               11h

 needs you
 ? Searching for Apartments Near Minto      2h     ~/codeaf      12 chats · 1 running  master
   thread: Searching for Apartments Near Minto      ~/pricing-site   5 chats

   needs your ok to run bash  1 allow once  2 always
 ? Clever Bet Prediction Model              6h   spend
                                                  ▁▂▁▃▅▂▁▁▇▃▂▅▂▁  14 days $34.10
 unread                                           opus 63%  ·  3 chats and 1 task today
   tier-B subs                                1d
   package release radar                      1d
   16 more

 tasks
 ⠋ Generate and Display First 200 Primes     4m
   Benchmark the Sieve                       1h

 since you left · 12h
   Spark Fleet Ssh Audit · 2 hosts up, 1 not
   made apartments-minto-street.md

 standing
   the 6am repo watch
```

Everything with rows in it is on the **left**, in the fixed order of the seven panels.
`projects` and `spend` are pinned at the **top right** and stay there. On a quieter machine
most of those left-hand panels have nothing in them, and they gather under the pinned pair
in the right-hand rail instead — heading and one dim line each — so the left of the screen
is only ever the things that are actually going on.

`esc` puts you back in exactly the chat you came from, untouched — nothing was closed and
nothing was sent while you were looking. The resting foot reads
`alt+p project · alt+e effort · alt+a approvals · alt+k chats · / commands`.
The project, approvals and chats hints appear only where those controls can act.
The controls stay the same as the cursor walks between rows. `→` opens the selected
row's verbs; `←` closes them.
`ctrl+o` still opens the selected row's folder and `tab` still walks places, but neither
is advertised in this bottom row. `alt+.` draws the
whole map over the cells you are already reading.

There is no argument form. The screen is how you name what you want; a command that took a
project name would be asking you to type out the very thing home exists to show you.

Home is the **first of the four places on the tab bar** — `home  tasks  spend  settings`.
It still does nothing on its own: no notifications and no alerts. You open it, you see where
things stand, and you either act on something or leave.

## See everything at once — what is on the home screen, the seven panels, and what an empty one says

**What has something in it is on the left; everything else is a rail down the right
edge.** A panel with rows stands in the **field** — the left of the screen, filled from the
top left corner down. A panel with nothing in it stands in the **rail**, flush with the
right edge, as its heading and its one dim line. So the panels move between the two sides as
work arrives and finishes, and the side a panel is on tells you whether it holds anything
before you have read a word of it.

**`projects` and `spend` are pinned to the top of the rail** and never move, because their
height is the same on every machine on every day. A blank row separates that pair from the
panels below them, which are in the rail only because they are quiet today.

**The rank never moves, only the side.** Within the field and within the rail the order is
always `sessions`, question rows, `projects`, `since you left`, `spend`,
`standing` — so two panels that both fill never swap places.

One column under 110 cells, where every panel is in that one order and there is no rail;
two columns from 110; three from 170, where the rail is the third and the field fills the
first. **A field that fits in one column never spreads two short columns over a wide
screen** — the point of the one busy column standing alone at the left is that your eye has
a single place to go. From 170 cells the middle column is the **descriptions'**: the
sentence under a row is drawn beside it there, level with its row, and a question raised
from home stands there as a card. Nothing else stands in it.

| Panel | What a row is | `enter` on a row | Dim line when it holds nothing |
| --- | --- | --- | --- |
| `projects` | a folder with conversations | no keyboard stop; click selects the next message’s project | never empty — the folder this window opened in is always a row |
| `sessions` | one of the fifteen most recent conversations | opens the conversation | `your recent conversations appear here` |
| `since you left` | what landed while you were away | opens the record, the file or the place | `what watches and tasks did while the terminal was shut` |
| `spend` | today, the fortnight, who it went to | nothing — its lines are read, never stood on or pressed; the heading opens the spend place | `every chat and task is priced here` |
| `standing` | a standing order — reminder, routine, watch or rule — soonest first | opens the standing place | `reminders, routines, watches and rules · "remind me at 6" or "every morning at 9"` |

**An empty headed panel keeps its heading and that one dim line** — it names what arrives there and
the one thing that puts it there, and it never says the panel is empty. On a narrow column
the line wraps onto a second line rather than being cut. This is the one deliberate
exception to the rule that nothing zero is drawn: a panel that vanished would teach nothing,
while a number still draws nothing at zero — never `$0.00`, never `0 tasks`.

## What needs me — unanswered questions, the ? bullet on conversations and tasks

Home has no `needs you` heading or empty attention panel. An amber `?` beside a
conversation or task means it has an unanswered question. The question stays on
that item's existing row instead of creating a duplicate. A task decision names
the task when its question identifies it; other questions stay on the conversation.
The question mark takes priority over the conversation's working or unread mark.

The selected row's description shows the question and its available answers. The
same digit keys answer it. Once the question is resolved and Home refreshes, the
question mark disappears. Questions belonging to items outside the visible lists
remain reachable as unheaded rows; they are not dropped because their owner is absent.
Standing items that need an answer also keep an unheaded question row.

**Every description opens with the thread the row belongs to**, as a title line of its
own — `thread: Searching for Apartments Near Minto`, spelled exactly as the conversation list spells
that conversation — then a blank line, and then the rest: the question, or a landing's
files and sentence, with the answers at the right. A watch made from home's own box belongs
to no thread and has no title line. On a frame with no description column the read row
grows three lines under itself for this; a terminal too short to keep those lines free
keeps one, and while such a row is read the rows under it move down two for the moment.

**A permission question is repeated exactly and nothing is added to it.** The session that
is stopped writes one sentence — `needs your ok to run ` and the tool's name — and the row
is that sentence, whole. A question with a paragraph shows only its first line.

**A question stands above every landing**, however old the landing is: a stopped
conversation is costing you something and a landing is not.

Four rows show, eight in a tall window, then `N more`. Amber is spent on the `?`
mark and on nothing else; a machine with nothing waiting has no accent on it at all.

## unread — the group of landings nobody has checked, work that finished and wants a look

**`unread` is every task whose call is yours: work that finished, where nobody could say
whether it is right.** It is a dim line inside the `needs you` panel, and the line is the
one word — no count after it, no clause at its right:

```
 unread
   tier-B subs                                                                 1d
   package release radar                                                      1d
```

The line is drawn only while the group has rows, and the rows are **newest first** — the
opposite of the questions above them, because nothing here is waiting and the freshest
landing is the one still in your head.

**A landing is one line at rest**: its name and how long ago it landed. **The right margin
of every row of the field is a time** — how long a question has waited, how long ago a
landing landed, when you last spoke in a conversation, how long a task has run, when an
order is due — and nothing else stands there.

**The row under the pointer or the cursor grows its description** — the thread's title
line, a blank, then how many files it wrote, the first sentence of what the work came to,
and its two answers at the right. A landing with no files to its name starts straight at
the sentence, never `0 files`:

```
   tier-B subs                                                                 1d
   thread: Billing Rewrite

   3 files · built the tier-B subscription flow                1 accept   2 not right
```

The two words are the task's own — `accept` and `not right` for work nobody could check,
which is every row that reaches this group. **The two keys are `1` and `2`
here and `[a]`/`[n]` everywhere else**, and that is deliberate: a bare letter on home always
types, because the box under it says "type to search or start something new" and the
promise has no asterisk. A digit drawn on the row is the one printable exception home makes,
so a landing takes `1` and `2` on this screen and its own letters on its card, in its room
and on its record. `enter` opens the conversation on the task's record.

**A landing wears no mark.** The `?` means something has stopped and will not move; this
work has already finished.

**Only a landing ages.** A `your call` is a row for two days after it landed; after that the
panel's last line counts it with the rest it hides — `3 more` — and `enter` there opens the
place, where every one of them still is. A question a conversation is stopped on, and a
watch that needs somebody, never age off home.

**A landing on `unread` is not repeated by `since you left`.** It goes back to that panel
as an ordinary line once it has been answered or has aged out of the group.

**On a short frame the whole group folds to one line before any question gives way** —
`8 unread`. `enter` expands that group; the `sessions` heading opens the sessions place.

## Answer from home — a digit answers the question that is drawing its answers

**Exactly one row of the frame draws its answers** — `1 allow once  2 always  3 deny`,
three at most, then `enter` — and **the key answers that row from anywhere on home**,
wherever the cursor is. No other row draws answers, so one `1` is never on the screen twice
with two meanings; the row the cursor is on says `enter` instead, and the rest say nothing at
their right. The words are that question's own and nothing here is put
into a vocabulary of home's; there is no `y`/`n` anywhere on this screen.

**The keys are the question's own wherever those keys are digits**, which is every card a
conversation stops on. The one place they are not is a landing in `unread`: its keys are
`[a]`/`[n]` everywhere else and `1`/`2` here, because a bare letter on home types.

**Which row is it?** The row under the cursor when that row can take an answer, and the top
answerable row otherwise. So a digit works on a frame you have not walked, and once you have
walked onto a landing in `unread` its `1`/`2` are the keys that are live.

Every kind of question can be answered this way: the whole question is in the file home
reads, and the answer goes back through the one door that knows which part of the engine
is waiting on it. **Not yet a question the model raised with `ask`**: that row says
`enter`, which brings the conversation here to answer it above the box.

Home leaves the answer on the other window's doorstep, so the row says
`answered · waiting for it to pick that up` until that window applies it — a second or
two while that window is running, or the moment it next opens when it was shut: a
conversation drains its doorstep as it opens, so an answer you left for a closed
conversation is settled the next time it runs, before its first turn. If it could not be
left at all, it says `could not leave that answer — open the conversation and answer it
there`.

**The first answer wins.** When two windows answer one question, the second is told who
answered and what they chose; it is never merged into a third answer nobody gave.

With something typed in the box a digit is a character going into it, never an answer.

## What opens when I press a landed row on home — I clicked a needs you row and it opened the chat

**A task's row opens its conversation with the task's record in front.** `enter` on a
`unread` row opens the conversation that ran the work, on the task's record, where the
question, its whole report and its two answers are — whatever project it belongs to, with
the conversation you were in left running behind it. You do not have to open it to answer:
`1` and `2` on the row itself send the same two answers.

A conversation's own question opens that conversation, where the question is waiting above
the box. Work that landed also reaches home through **the `since you left` panel**: a line
per task that landed, and `enter` on it opens that task's record.

A conversation another terminal is holding is not refused: `enter` brings it here — see
*Continue a conversation from another terminal*.

## How do I clear a your call row — settling work that landed

Work that landed as `your call` **waits until you decide about it**, however many days that
is: nothing more happens to that work until somebody answers it. Only its row on home ages —
two days after it landed it leaves `needs you` for the count on the panel's last line,
`3 more`, and the sessions place still lists it; `enter` on that line opens the panel and shows
it again.

Three ways to settle it, and they are the same door:

- **the landing card in the conversation** — `a <yes>` and `n <no>`, whose words are
  whatever that row is asking (`accept`/`not right`, `resolve it`/`drop it`), plus
  `s tell it`, which opens the task's page rather than answering, and a dimmer
  `d let codeaf decide this one`;
- **home** — walk onto its row in `unread` and press `1` or `2`. The conversation applies
  it on its own beat; if it is not running, the answer waits on its doorstep and is applied
  the moment that conversation next opens — the constructor drains the doorstep before
  anything else — and the row says `answered · waiting for it to pick that up` meanwhile;
- **the task's room** — enter on its roster row opens it, and the same chips stand at the
  foot of the page; `a`, `n`, `s` and `d` over an empty box answer it with nothing
  selected;
- **just say so.** "accept task 7", "that one isn't finished", "have another look at task
  7" — codeaf settles it through its `tasks` tool. Whichever is used first wins; the other
  says `already answered`.

Accepting merges the task's branch and unblocks everything queued behind it. The tasks page
has the whole of it under *Why is the task waiting for me*. **Home is where you find it,
not where it is settled.**

## Sessions — the fifteen most recent conversations on Home and what is running on this machine right now

The **sessions** section replaces the old list of individual tasks. It shows up to
fifteen conversations across projects, newest conversation activity first, regardless
of whether they have tasks. Closed conversations remain in history with dim titles.
Each row uses the full conversation title, truncated to fit, and its age. Selecting a
row opens that conversation; `→` offers the usual conversation actions.

Click **sessions** to open the **sessions** tab (formerly tasks), which holds the full
conversation trees under running and completed. The running section shows work in
flight across projects. Short frames show fewer rows with a
fold for the remainder of these fifteen. The heading leads to the full history.

There is exactly one conversation list on Home, under **sessions**. Open tabs,
recently closed conversations and saved history are combined by conversation identity
before choosing the fifteen most recent, so each conversation appears once.
The `opt+k` chats menu still lists open tabs; closing or reopening a conversation
updates the tab and the row in Sessions together.

## Close or put away a task from home — task row options, new in project, open folder, copy project

**`→` offers task options just as it does for threads:** `x close`,
`n new in project`, `o open folder`, and `p copy project` when the corresponding local
folder and action are available. On a wide home these appear beneath the description
in the middle column. Putting a task away hides that task from home and the unfiltered
Sessions list; it does not stop it, delete its record, or put away its conversation.
Find it again by typing its name in Sessions, then use `→` and `x reopen`.
The choice survives closing the app. Folder actions use the task's conversation project.

## Why does only one row spin — the one spinner on home

**Only one working conversation animates.** The other running conversations keep a
still working mark. Home's latest activity reading chooses the spinner; if it has none,
the first conversation with live work takes it, passing over a row that shows `?`. The page refreshes its saved facts every
three seconds. An `ask here` errand's row takes the spinner while its answer is coming, and so does
a row on its way here from another window.

Two reasons, and they are the same reason:

- **Calm.** Eleven cells turning at once is a screen you cannot glance at, and one moving
  cell says *this machine is working* exactly as well.
- **A flat wire.** The frame the spinner costs is the same frame whether one thing is
  running or twenty, so a busy machine costs an ssh connection no more than a quiet one.

Home wears two marks and only two: the amber `?` on a row waiting for a person, and that
spinner. In screen-reader (linear) mode nothing turns at all.

## What did it do while I was away — the since you left panel, what happened while the terminal was shut

**What happened on its own while you were not looking**, under `sessions` — in the field
when it holds anything and in the rail when it does not — headed with how long you were
away:

```
 since you left · 12h
   Spark Fleet Ssh Audit · 2 hosts up, 1 not                                  $0.42
   made apartments-minto.md                                            Pricing Site
   fired at 6am — nothing had changed
```

Four kinds of line, newest first, four of them — eight in a tall window — then
`N more`:

- **a task that landed** — its name and what it came to, the first sentence of its report,
  with how long ago it landed at the right and its cost under the cursor when it cost
  anything. A task that stopped without finishing says why instead: `lost the connection`,
  `went in circles`, `out of steps`;
- **a file a conversation made** — `made <name>`, with how long ago at the right and that
  conversation's name under the cursor;
- **a standing item that fired** — its own last-look line, in its own words; a one-off that
  fired and stood down reads `fired 3 minutes ago — it told you`;
- **what memory learned or let go** — `learned 2 things, let go of 1`.

**Every line is a door.** `enter` on a task opens that task's record, on a file opens the
file, on a firing opens standing and on memory's line opens memory.

It is measured from when you last **closed** home; the very first look has no "since" and
the panel keeps its heading and `what watches and tasks did while the terminal was shut`.
The memory line is not wired yet: its figures read zero on this surface, and a zero draws
nothing.

## Threads, open conversations and recently closed chats — the Home list

Home has **one bulleted conversation list under sessions**. It combines this window's
open tabs and saved history, deduplicates them, and shows the fifteen most recent
conversations, newest first. Bullets show running work and unread replies. The current
conversation is bold; its description can say `here` and show the last thing you wrote.
Closed conversations keep their place in this same chronological list, dimmed.

Enter opens or reopens a conversation and restores its tab. A short terminal folds
rows that do not fit behind `N more`. The Sessions heading opens the full Sessions tab;
typing into the box searches older history. The `alt+k chats` menu (`opt+k` on macOS)
continues to list open tabs only.

Closing with `→`, then `x close`, or `ctrl+e`, removes the same conversation from
Home's open rows, the tab strip and the default chats menu immediately. It keeps
running work and drafts. A waiting conversation carries a `?`;
the question remains reachable even when its tab is closed.

**On `space` `space` the cursor is already on the chat you were in before this one**, so a
switch back is two keys — `space` `space`, then `enter` — and `esc` still goes back to the
conversation behind home.

## Choose a project for the next message — click a project name or use alt+p

**Click a project name under `projects` to select it for the next message.** The
`project: <path>` label above the message box changes immediately, exactly as if
`alt+p` (Option+P on macOS) had cycled to that project. Hovering underlines the name;
clicking does not open a conversation or move the keyboard cursor. The choice lasts
for this window, including after sending a message and returning home.

The `projects` heading remains dim and opens nothing. Project rows are not keyboard
stops; arrow keys stay in the conversation list. To cycle projects from the keyboard,
use `alt+p`. To start a chat from a conversation row's folder, use `ctrl+t`.

```
 projects
   ~/codeaf          12 chats · 1 running   master, 2 files dirty
   ~/code/pricing-si…  5 chats
   /tmp/af-stop-ws
```

A row is the folder's path, how many conversations are in it and how many are running, and
where its repository stands — the branch and the dirty count as one clause. **This window's
folder is always the first row**, even before anyone has spoken in it, which is why the
panel is never empty. **Paths are absolute and truncate on the right**: `/tmp/landing-test`
stays `/tmp/landing-test` while it fits, and a longer path becomes `/tmp/landing-te…`
when space runs out. Only paths inside your home directory use `~/`; home itself is `~`.
A similarly named sibling directory does not get that abbreviation. Counts and repository
facts keep their columns as the path shortens.

**A project row selects a destination by click; it has no `enter` action or strip.** Typing
the project's name into the box lists every conversation in it. Five rows show, eight in a
tall window, then `N more`; typing a folder's name or path finds the rest. Pasting a folder
path into an empty box offers a one-use start there (*Start something new from home*).

`ctrl+t` on a conversation's row starts one in that row's folder; clicking a project
instead chooses the destination for the next message you send from Home.

## How many conversations does a project have — the count on its projects row

**It is on the project's row in the `projects` panel**: `12 chats · 1 running`, each half
only when it is not zero. There is no card on the right about a whole project any more, and
no right side at all on the resting screen.

Where to go for the whole figure instead:

- **`→` then `its chats`** on the row lists every conversation in the project as a search;
- **the sessions place** for the work a project has run, which is the record rather than a
  count;
- **the spend place** for what it has cost.

## Why are most projects collapsed on home — they are not, and how do I open a collapsed project

**Nothing on the resting screen is collapsed by project.** There are no project blocks, no
`elsewhere` rule and no `▸` project line to open: projects are rows of the `projects`
panel, and conversations are rows of the conversation list, each wearing its project's name when
it is not this window's.

What is folded is **the tail of each panel** — one `N more` line under it — and the fold
is a toggle: `enter` on it opens the panel (see *Where did the rest of my chats go*).

**Under 60 columns** — the phone shape — home is an inbox with the projects under it, and
there a project other than this window's *is* one folded `▸` line, which `enter` or a tap
opens in place, its mark becoming `▾` (*Home on a phone*).

## Why is my project tree gone, and where did the flat list go

**Both are gone on purpose, and nothing under them is out of reach.** Home was a tree of
project headings, then one flat list of every conversation ranked by what wants you first
under a `20 chats · what wants you first` line, with a card on the right. Neither answered
the questions a person has at a glance — what needs me, what is running, where was I — so
each of those is a panel of its own now.

| It used to be | It is now |
| --- | --- |
| `?` beside a conversation or task | an unanswered question on that row |
| `◐` rows under them | the `sessions` panel |
| the quiet rows and `▸ 15 more, quiet since 6d` | the conversation list, then `N more` |
| project headings, `alt+g` | the `projects` panel |
| the `since you left` ledger above the list | the `since you left` panel |
| the card on the right | nothing at rest; a card still stands beside a search on a wide frame |

## How do I group home by project — alt+g

**You cannot any more, and `alt+g` does nothing.** Home is panels now, and the
`projects` panel is the by-project view: every folder with a conversation in it, this
window's own first, each with `N chats · M running` and its branch. It is not a list to
walk and nothing on it opens; clicking a project's name picks it as the folder for the
next message. Typing the project's name into the box lists
every conversation in it, and `ctrl+t` on one of them starts a new one there.

## How do I hide the quiet chats — alt+q hide the quiet ones

**You cannot any more, and `alt+q` does nothing.** The panels already keep the quiet
ones out of the way: `needs you` and `sessions` hold what wants you or is moving, and
the conversation list shows the fifteen most recent conversations, with closed ones dimmed. A short frame folds what does not fit behind `N more`. Typing finds any conversation on the machine.

## Which column am I in — move between the columns on home: ↑↓ walk a panel, ←→ cross columns

**`↑` and `↓` walk the field**, from one panel into the next at its ends, and stop at
both ends: `↑` off the top row stays on it and does not climb onto the tab bar. **`←` and
`→` never leave the field** — `→` opens the row's own verbs under it and `←` closes them.
Arrow keys skip the rail. Project names there can be clicked to select the next message’s
folder; the spend figures and empty-panel hints have no click action.

**The panel the cursor is in marks its heading** with the cursor's ground; the words stay
where they were. A headed panel such as `sessions` lights its heading when the cursor is
inside it. The conversation and question rows have no heading. The row you are on
wears the same ground and its title goes bold; the row under your mouse pointer wears it too
while the pointer is on it.

**`→` opens a row's verbs, on every row and at every width**, and the chords work without
the strip: `ctrl+o` opens a conversation's workspace, the workspace a standing order stands
over, or the conversation a `since you left` line happened in; `ctrl+y` copies the path,
`ctrl+e` puts away or pauses, `ctrl+x` stops. The foot does not change from row to row and
names none of them; `alt+.` draws the map when you want the rest.

A digit answers the question row drawing its answers, wherever you are standing. `enter` acts on
the row under the cursor. `alt+.` draws the map.

## Why is a heading highlighted, why is one project name darker than the others

**Because the cursor is in that panel.** The heading of the panel holding the cursor wears
the cursor's ground — one heading per frame, following the keyboard, and never the mouse.
Walk into `sessions` and `sessions` is the heading that is marked.

No project is ever drawn darker than another on its own. If one row looks different, it is
one of two things: the **row under the cursor or the pointer**, which wears the ground and a
bold title, or **this window's own conversation** in the conversation list, which is always bold
and says `here`.

## Where the cursor starts on home — on my previous chat — and where the first down arrow goes

**Opening home with `space` `space`, `/home` or `alt+1` puts the cursor on the conversation
this window was in before the one in front** — the most recent other one on this window's
own tab stack — so going back is `enter`. A window that has held only one conversation has no
"before", and the cursor is on its own row in the conversation list, which says `here`.

**On a launch** — home greeting you — the cursor is on the conversation this window is
holding, the row `esc` drops back into.

**`↑` off the top row of home stays on it.** The tab bar — the row of four words — is
reached by clicking a word, by `tab`, or by a place's own chord (`alt+2` and the rest); on
every other place `↑` off the top row still walks up onto it. On the bar `←`
and `→` walk along the words without opening anything, `enter` or `↓` goes into the one under the
cursor, and `esc` puts the cursor back on the row it came from. Walking up onto the bar does
not move home's own cursor, so `↑` and then `↓` costs nothing.

**The cursor stays on the row it was on through every refresh**, rather than on a line
number — the order genuinely changes when work starts or finishes, and a cursor that stayed
put would move you onto something else between two glances.

## Why is the home screen empty — the dim line under each heading, and a quiet home

**It is not empty; the panels are waiting for their first rows.** On a machine that has
done nothing yet, every panel but one keeps its heading and one dim line naming what
arrives there — `questions from any chat or task land here · a digit answers them` under
`needs you`, `work you send off with /task runs here on its own` under `sessions`, and so on
(*See everything at once*). `projects` always has the folder this window opened in, and
the conversation list has this conversation from its first minute.

**That dim line is not a status.** It never says `nothing is running` or `no chats yet`; it
says what would put something there. It goes the moment the first row arrives.

A quiet morning on a busy machine is the same screen with fewer rows: `needs you` whispering,
`sessions` whispering, the conversation list full, `since you left` holding what fired overnight.
There is no accent anywhere when nothing is waiting on you.

Typing works exactly as it does anywhere: `? ask here: "…"` and `+ start a new
conversation: "…"` rise out of the box, and `enter` starts the conversation.

Over `--host`, in the fraction of a second before the far machine answers, home draws no
panels at all — a whisper over a server full of work would be untrue.

## Where did the rest of my chats go — N more, expand a panel, and panels cut off on a short or small terminal

**Every panel folds inside itself** with one dim line, `N more`, counting everything it is
not showing — the rows past its budget, the rows a short window took, and on the additional question rows the
landings that aged off it. **The fold is a toggle.** Walk onto it and press `enter` (or
click it): the panel opens and takes the column — every row it has, with the other panels
squeezed to their floors in the order below — and the line reads `N fewer`. `enter` again
folds it. One panel is open at a time; opening a second folds the first. An open panel
taller than the window shows what fits and its line still counts the rest — `3 fewer · 40
more` — and names no place, because `enter` on it folds rather than opens. The way to
those rows is the panel's **heading**: `sessions` opens sessions, `since you left` opens memory,
and `standing` opens standing. Conversations and extra question rows have no heading;
`projects` opens nothing. The foot under a fold says
which way it will go: `enter shows the rest`, then `enter folds them`. Opening lasts as long
as the window; a relaunch starts folded. The fold wears no mark: home spends its two marks
on the amber `?` and the one moving cell.

**A tall terminal grows the panels**, once every panel has what it naturally shows:
additional question rows, `since you left` and `projects` to eight rows; `sessions` keeps at most fifteen recent conversations; `standing` from three to five. `spend` never grows. What is left over is air
under the shorter column.

**A short terminal squeezes them in a fixed order**: `standing` gives way first, then `spend`,
then `since you left`, then `sessions`, then `projects`; the conversation list and additional question rows
shrink last. A squeezed panel keeps its heading, the rows that fit and its `N more` line;
only when every panel is down to that is a panel dropped — and the panels that are only
whispering go before any panel with rows, whatever their rank, so a very short window
(fourteen rows) still shows a conversation you can open rather than a sentence about what
would be there.

**Typing sees straight through every fold**: a search matches every conversation on the
machine, including the ones no panel is drawing.

## How do I see the collapsed sessions — 13 more

**Open the fold, or type.** The conversation list shows as many of its fifteen recent rows as fit and then
`13 more`; `enter` on that line opens the panel and shows them all, squeezing the other
panels, and `enter` again folds it. The box at the foot still searches every conversation on
the machine as you type — a project's name, a folder's name or a word from what a task came
to all find them — whether or not a panel is drawing the row.

Every panel's fold works the same way: `N more` under `sessions`, `since you left`, `standing`
and `projects` opens that panel. The places themselves — tasks, standing, spend — are on the
tab bar and their slash commands, not behind the folds.

Per-project folds — `▸ 13 more, quiet since 1d` under a project's own heading — belong only
to the **phone shape**, under 60 columns, where home is still an inbox with the projects
under it.

## The line at the top of home — the pulse, want you, moving, spend and allowance, the clock

The top line is the program's name and, right-aligned, what is true of the **whole
machine** right now. It is the first row of **every** frame — home, every place, and the
chat itself, where it sits over the tab strip (` home  <your chats>`), a rule and a blank:
the same four rows at the top wherever you are standing. **Inside a chat** and on every
place but home it reads:

```
 codeaf              2 want you · 4 moving · $0.55 / $500 · tue 1:11pm
```

**On home it drops the two counts** and keeps the budget and the clock —
` codeaf                              $0.55 / $500 · tue 1:11pm` — because the `needs you`
and `sessions` panels are those counts, row by row.

- `2 want you` — how many things have **stopped on you**: a conversation waiting for an
  answer, a standing order that will not fire until you say so, an errand holding a
  question. It is drawn in **amber**, which means one thing only: someone is waiting for a
  person.
- `4 moving` — how many things this machine has **in flight right now**: task nodes out,
  conversations mid-turn in another window, an `ask here` errand answering, a standing
  order firing. A conversation with three tasks out counts as three. It is drawn in
  **cyan**, shows from one, and disappears at nothing.
- `$0.55 / $500` — every model call written down **since midnight**, against the day's
  allowance, in **green**, the money colour. A machine with no allowance draws the figure
  alone.
- `tue 1:11pm` — the day and the time.

**Every segment but the clock disappears unless it is true** — never `0 want you`, and a
day that has cost nothing says nothing about money. The counts are one reading taken every
three seconds while home is open and every **ten seconds** inside a chat, so a question
another window asks can take up to ten seconds to reach a chat's top line.

**The money on this line is the money on the spend place and the `spend` panel**, to the
cent — one reading of one file, wherever you are standing.

## Where did standing, memory and search go — the four places on the tab bar

**They are still places; they are just off the bar.** The tab bar under the top line is four
words — `home  tasks  spend  settings` — and `tab`, `alt+1` … `alt+4` walk them. Standing,
memory and search open exactly as they did:

- **`/standing`** (or `/orders`), `alt+5`, or `enter` on a `standing` row;
- **`/memory`** (or `/memories`), `alt+6`, or `enter` on memory's line in `since you left`;
- **`/search`**, `alt+7`, or the typed door on home's box.

While you stand in one of the three, its word is drawn after the four so you can see where
you are; `tab` from there goes to home. `alt+.` draws the map of all seven with their
numbers. Home's own panels already summarise the three on the bar: `sessions` is a glimpse of
tasks, `spend` of spend, `standing` of standing.

## Why did a dashboard open when I started codeaf — home greets you

**Home is the first thing you see when you open codeaf.** The conversation your launch
would have opened is loaded and waiting underneath it: `esc` drops straight into it. In
effect the launch is the launch you always had, with home already open on top of it.

**The cursor starts on the conversation this window is holding** — its row in `where you
were`, wearing `here` — so the first frame already answers "where am I". `↑` off the top of
the column stays there (see *Where the cursor starts*); `esc` goes on with
what you were doing.

Nothing about *which* conversation opens is changed by this. The door picks it exactly as it
always did — this directory's most recently spoken-in chat, or a fresh one — before home is
drawn at all.

It greets you only when it has something to say. All of these have to be true:

- You opened codeaf **without naming a conversation**. `codeaf` or `codeaf chat`.
- The machine holds **a conversation other than the one this launch opened**. Somewhere
  else to go, in other words.
- It is a real terminal session — not `--once`, not `--host`.

When Home greets you there is no welcome box. Open tabs are listed immediately;
typing searches the rest of the saved conversations.

## Skip the home screen — launching straight into a conversation

Four ways, and each of them is you saying which conversation you mean:

| What you run | What you get |
|---|---|
| `codeaf chat --session <path>` | that conversation, no home |
| `codeaf resume` | the session picker, no home |
| `codeaf chat --once "text"` | replies printed with no surface; one reply normally, or every landing-woken reply when `--yolo` has a budget |
| `codeaf --host <machine>` | the far machine's session, no greeting — `space` `space` opens that machine's home |

And on a machine with only one conversation — a first run — home does not greet you.
There is no setting for this and no flag to turn it off: whether home greets you follows
from how you launched and what the machine holds, both of which answer themselves.

Not being greeted is not the same as being out of reach. Once you are in a conversation,
`/home` — or `space` twice on an empty box — opens the screen whenever you want it, on a
machine with one conversation and on one with none (see *Why is the home screen empty*).

## Close or archive a conversation — put junk away and clean up home

Select a conversation, press `→`, then **`x close`**. `ctrl+e` does the same.
Its tab closes and it leaves the default `alt+k chats` list immediately. Home keeps closed conversations dimmed in the same Sessions list while they are
among the fifteen most recent. The
foot says `closed · type its name to find it again`.

Nothing is deleted: its transcript, tasks, running work and draft remain. Closing
from Home also saves its archived status, so the bounded closed list can find it
after a restart. Closing a tab with `ctrl+w` retains it in this window's close stack
without archiving it on disk.

**Enter on a dimmed row reopens the conversation and its tab.** `→`, then `x reopen`,
or `ctrl+e`, does the same. Older closed conversations remain searchable by name.
On a phone-width terminal, Enter first opens the row's sheet; use its open action.

## Why did the list jump to the bottom when I typed — home's two shapes

Home has **two shapes**.

**With nothing typed it is the panels**, hanging from the top of the frame, with the cursor
on the chat you were in before this one.

**The moment you type a character it becomes one list, a drop-up.** It lifts so that its
last row — the action row, `start a new conversation: "…"` — lands directly above the box
you are typing into, with `ask here: "…"` between it and the matches, and the matches rise
above the pair **best one first**: the strongest match is two `↑` away, and each `↑` past it
walks into a weaker one. Everything to do with typing is then one cluster at the foot: your
words, the row saying what `enter` will do with them, and the hint under it. Clearing the
box puts the panels back.

**So the cursor does move between the two**, from up in the panels to the foot and back.
One keystroke of re-anchoring is cheaper than a page of panels pinned against the box.

**On a frame 136 columns or wider, a card stands beside the matches** — about the match
under the cursor (*The card beside a search*). It never moves while the list lifts, and it
goes empty on the `start a new conversation` row, which is a chat that does not exist yet.

## Switch between sessions — enter on home

`↑`/`↓` (or `ctrl+p`/`ctrl+n`) walk a column and `←`/`→` cross between columns.

**One click is `enter`.** A click on a row opens it, and a click on a fold that names a
place opens that place. A `projects` row is the one exception: a click on it picks that
folder for the next message and opens nothing. A click on a panel's **heading** opens the place the heading names:
`sessions` opens sessions, `since you left` opens memory, `spend` opens spend, and
`standing` opens standing. There is no `needs you` heading. The conversation list has no heading. The `projects` heading opens nothing
and stays dim. **A heading that opens somewhere underlines on mouse-over.** The
pointer on a heading moves neither the cursor nor the marked heading.
A click on a `/` command, `ask here` or the new-conversation row only selects it;
`enter` runs it.

`enter` opens the session under the cursor — **any row on the screen, in any project.**
The chosen journal is opened and replayed, and **the conversation you were in stays open
behind it**, still streaming its turn, still running its tasks, one `tab` away. It is not
closed and it is not paused.

`enter` on the one you are already in simply steps into it and says nothing — it is
already loaded underneath, so there is nothing to reopen and nothing to announce.

`enter` on a conversation **this** terminal is already holding behind the screen goes
straight back to it, for the same reason: it is alive, so there is nothing to reopen.

The resting foot reads, when all its controls are available:

```
alt+p project · alt+e effort · alt+a approvals · alt+k chats · / commands
```

(the box above it says `› type to search or start something new`, which is where that
promise moved on 2026-09-17: the lowest line is for keys)

and it says what THAT row's keys do on a row that has its own — a `since you left` line,
a fold door or the action row — with the available draft controls before `esc`.
The foot omits the `ctrl+o` and `tab` hints; both keys still work.

## Typing a long question on home — does the box wrap, and where does a paste go

**The box wraps, and it is three rows tall the whole time.** Home's foot box is drawn by
the same editor as the chat's message box: a sentence longer than the frame wraps onto
continuation rows. The moment you type anything the box stands **three rows** high and
stays there — it does not grow under your hand as a sentence wraps, and the list above it
does not step down a row when it does. Past three rows the window scrolls with the caret,
marked with `…` where the `›` was. Nothing you type is ever truncated out of view. There
is no key to open a new line here (that is the chat box's `ctrl+j`).

**An empty box is three rows too.** With nothing typed it draws the dim sentence about
what this screen does on its first row and holds the other two open under it, so the one
thing on this screen you type into is a block you can see *before* you have typed anything.
It also means the foot does not move on the first keystroke: a box that jumped from one row
to three the moment a letter landed would shift the list up under the hand reaching for it.

**On a very short terminal the box is one row again.** Under about thirteen rows there is
no room to hold three open and still leave a list worth reading above them, so the box
falls back to the single row it has always drawn and the list keeps its rows. The box is
just as usable — what gives way is the space around it, not what you came to read.

**`alt+enter` is a task and not `ask here`.** On home and on every other place the chord
opens the **composer layer**, where the three facts a task needs are settled — where, on
what, how much (the places page, *the composer layer*) — and a second `alt+enter` sends it
off.

**`ask here` is one `↑` and then `enter`.** The row is already on the screen while you
type: `? ask here: "…"` sits directly above `+ start a new conversation: "…"`, and the
cursor rests on the lower of the two, so the ask is one keystroke up. `ctrl+enter` is still
bound to it and is no longer named on the foot, because only a terminal that can tell
`ctrl+enter` from a plain `enter` ever sends it.

**A paste lands in home's box.** Paste while home is open and the text goes into the foot
box — searching, exactly as typing does — or into the ask-here exchange's own box when that
pane holds the keyboard. Pasted newlines are kept, so a pasted paragraph is fine as an
`ask here` question.

## Continue a conversation from another terminal — move it here, it says open in another window, it is still running in the other shell

A conversation another terminal has open is a **door**, and pressing `enter` on it **brings
it here**. Not two windows on one chat — the conversation leaves that terminal and arrives
in this one, with its work and its half-typed sentence.

**On the ordinary `codeaf chat`, it is instant and it takes one `enter`.** Your conversation
does not live inside the terminal you started it in: it lives in this folder's **engine**,
which is why it keeps working when you close the window. So the second terminal asks the
engine for the conversation and gets it back — mid-reply, in well under a second, with the
reply still arriving and every task still running. Nothing pauses. There is no
confirmation, because the way back is the same single `enter` from the other side.

The row's right margin says where the conversation is. `open in the engine` means the engine
has it — often with no window anywhere, which is what a conversation you left running looks
like. `another window` means a terminal is sitting in it. Either way `enter` opens it here,
and the row under the cursor says so — `another window · enter brings it here`.

**What the other terminal shows.** One line — `moved to another window · enter on home
brings it back` — and it lands on **home**, with the row it just lost under the cursor. It
has not lost anything: it stepped out of the seat, and one `enter` there brings the
conversation straight back.

**What comes with it.** The transcript, whole. Every running task, still running. The unsent
sentence in the other window's box arrives in yours.

**`codeaf chat` in a folder whose conversation is open elsewhere** — you opened codeaf and it
said `open in another window` — does not start a second one silently. It opens home with that row pointed at, so one `enter` continues where you
left off and `esc` gets on with a new conversation instead. A plain `codeaf` in a folder
whose engine is already holding a conversation simply **sits down in the one the engine
has**.

**The one road where it still asks first** is a window with no engine behind it —
`codeaf chat --no-host`, `--debug`, or a build old enough to predate the engine. See
*Moving a conversation from a window with no engine*. **And `--host` is the one place it
cannot happen at all**: the row says
`open in another window — go there, or start a new conversation here`.

## Moving a conversation from a window with no engine asks first — the move card, enter moves nothing, the cursor starts on leave it there

This is the road a window takes when there is no engine holding the conversation —
`--no-host`, `--debug`, a test. On the ordinary `codeaf chat` you will not meet it: see
*Continue a conversation from another terminal*, where one `enter` opens the conversation
instantly.

**`enter` asks a question and moves nothing**, because this move really does end the other
window. Home's foot line becomes that question, in one row:

```
Move this conversation here? · 1 move it here · 2 leave it there · its reply stops there; its tasks come here · esc leave it there
```

**The answer `enter` would take is `leave it there`.** That is the law every confirmation
here keeps: `enter` is the key people press to make a question go away, so the answer under
it has to be the one that loses nothing. To move it, press `1`. `esc` is `leave it there`.
Anything that moves the cursor off the row takes the question down; nothing has been written
and nothing in the other window knows you looked.

A narrower terminal drops whole clauses off the end of that line — what moving costs goes
first, then the answers — and what a very narrow one is left with is the question and
`esc leave it there`.

It used to be two enters, with the second `enter` doing the move — so leaning on `enter`
down a list of conversations ended another window with it.

## Moving a conversation from a window with no engine — why is moving a conversation slow, it says coming here and nothing happens, how do I cancel the move, that window did not answer

**Answering `move it here` asks the other window, and the row says it is coming.** The right
margin stops saying `another window` and says `coming here`, the row takes the page's one
turning cell, and the foot line carries the state: `moving it here — esc stops waiting`, or
`moving it here — that window is stopping its reply · esc stops waiting`.

**A mid-reply window no longer holds you up.** It stops where it is and hands the
conversation over on its next look — four times a second. The reply it had written so far
is in the transcript that arrives with it. **And if it had written nothing yet, this window
asks your question again for you**: one dim line — `the reply stopped when this conversation
moved — asking again` — and then the answer. A reply that had already started is *not*
asked again: what it wrote came with it, and its tasks resume from their checkpoints.

Past fifteen seconds with nothing happening over there, it says `that window has not
answered yet` and stops at that — it invents no cause. `esc` withdraws the request, leaving
the other window untouched. The moment it lets go, the row opens here.

**What comes with it.** Tasks that were running land `paused — it resumes` and start again
from their checkpoint in this window. The unsent sentence comes too.

**If nothing ever answers, the wait ends and says so.** A request is only good for ten
minutes; past that it says `that window did not answer — it still has it` and
`enter asks again`. **And if it comes free while you are looking at something else**,
nothing is opened under you: home says `it came free — enter opens it` when you come back.

## Open another project from home

**`enter` opens any row on this screen, whatever project it belongs to**, and **`ctrl+t` on
a row starts a fresh conversation in that row's folder** (*Start a chat in another
folder*). There is nothing to go to another terminal for and nothing to type.

What happens is a **second conversation**, not this one moving. The conversation you were
in is left running exactly where it was — its turn keeps streaming into its own transcript,
its tasks keep running, it keeps its lock — and the new one is built on **its own**
workspace, with that project's approval rules, its crew, its spend ceiling and its saved
shapes of work. Nothing is carried across, because nothing crosses.

The **tab strip** above the transcript then shows both, and `tab` over an empty message
box goes back.

One refusal is still possible and it leaves home standing: the folder is gone —
`that folder is gone · <path>`, and nothing is opened. Home already knew — the row reads
`folder gone` in its right margin, see *Enter does nothing on a row — the folder is gone* —
and this line is the check made again on the keystroke, for a folder deleted in the seconds
since.

**How many you already have open is never a refusal.** See *How many conversations can one
terminal hold*.

## How many conversations can one terminal hold — is there a limit, too many open, why can I not open another, let go, quiet a while, does codeaf close old chats

**Opening another is never refused**, over every door. The ninth and the fiftieth open
like the first, from home's `enter`, a typed path, the switcher (`alt+k`), search or
`/new`. There used to be a cap of eight, and taking a ninth said `8 open is as many as
codeaf holds — /quit closes this one`. That sentence is gone.

**Past twelve open, a quiet conversation you have not looked at for fifteen minutes may
be let go of.** Twelve is how many rows the switcher card draws. The one left longest ago
goes first, and only if letting go costs you nothing: nothing turning, no task or job
running, no question waiting on you, no turn that finished while you were away that you
have not been shown, and no words of yours still in its box. Where every held conversation
is busy, the window holds more than twelve rather than closing work or refusing a new one.

You are told, in the conversation that stayed in front:

```
let go · <name> — quiet a while — open it again from home
let go · <name> — quiet a while — its work keeps running
```

The second sentence means the work keeps running on its engine. Otherwise the
conversation is closed, because nothing else could run it. Either way the transcript is
on disk and every door — home, the switcher, search — opens it again. A window left
completely alone keeps what it holds; opening another conversation, or one of the
others finishing, is what collects a quiet one.

Each conversation that is still held is fully alive whether or not you are looking at it.
`/quit` closes the one in front. `alt+k` shows the first twelve as rows; home's
the conversation list shows the fifteen most recent conversations; typing searches every saved conversation. `/status` carries the count as `2 open · 1 waiting`.

## let go · quiet a while — a conversation this window let go of, too many open

**A conversation you had open was let go of because it had been quiet and you had not
looked at it.** Past twelve open, the one left longest ago — idle fifteen minutes, nothing
turning, nothing waiting, no draft — is let go of so this window can stop holding it.

The note in the conversation that stayed in front is one of:

```
let go · <name> — quiet a while — open it again from home
let go · <name> — quiet a while — its work keeps running
```

The second means the work keeps running on its engine. Either way the transcript is on
disk; home, the switcher and search open it again. Opening another is never refused.
*How many conversations can one terminal hold* is the rest of the rule.

## Enter does nothing on a row — the folder is gone

**Its project folder is not on this disk any more.** A conversation started in a directory
that has since been deleted, renamed or moved — a scratch folder under `/tmp` wiped by a
reboot is the usual way — cannot be opened, because the conversation would come up as an
agent whose tool root does not exist and every command in it would fail.

Home says so **on the row**, at every width: `folder gone` in the right margin, where the
age would be.

`enter`, `ctrl+t new chat here` and `ctrl+o open folder` all want that directory, so each of
them refuses rather than pretending. The row's verb strip drops `n new in project` and
`o open folder` for the same reason: a strip only ever names letters that work.
`ctrl+y copy path` and `p copy project` still do, because a path is a string.

**On enter.** Nothing is opened, home stays up, the conversation you were in is untouched,
and `that folder is gone · <path>` appears on home's message line at the foot of the screen.

**The conversation itself is not lost.** Everything codeaf recorded about it lives under
`~/.codeaf/v3/projects`, not in the workspace. What cannot happen is *continuing* it,
because there is nowhere to continue it.

**What to do:** recreate the folder at that exact path and the row opens again on the next
refresh (home re-checks every three seconds); or start a new conversation in a project that
exists — `ctrl+t` on one of its rows.

## Why does it say elsewhere — on a standing item, and nowhere else

**Almost never, and never on a conversation.** Every project but the one this window
launched in used to carry a dim `elsewhere`, and `enter` on one of its rows opened nothing.
That is gone: `enter` opens any conversation on home, in any project.

**Home never says it on a standing item either, any more.** Pressing `enter` on a
**standing item** — a reminder, a watch, a rule — opens the conversation that set it up,
whichever project it belongs to; it used to refuse with `elsewhere · <the item's workspace>`
at the foot when that project was not this window's, and that refusal is gone. An item that
was set up from home and never became a conversation opens the **standing place with the
cursor on that item** — its own page is the honest answer to "show me this thing". The one
place the sentence `made from home — no conversation to open` is still said is the standing
place itself, when `enter` there asks for the conversation behind such an item.

A second project is a second **conversation**, built the way the first one was, on its own
workspace, with its own gate. A conversation still never moves between projects — though it
can be **about** another folder without moving: see *What also about means on a row* below
and *Choosing a folder*.

`another window` is a different sentence and still means what it always did — see
*Continue a conversation from another terminal*.

## What also about means on a row — the conversation is about another folder

A conversation is filed under the project it is **standing in** — the folder codeaf was
opened in. It can also be **about** other folders: ones you named with `/folder` or
`/attach`, and ones a task's ground settled on and the conversation wrote down. On a
**search match** the row's dim tail ends with the name:

```
Flaky pipeline               2 running · 3h · also about wisp
Tuesday notes                            1d · also about wisp +2
```

One name and a count, never a list. The card beside a search names them in full under
`also about`. The resting panels do not carry the clause — a row of the conversation list is its
title, its project and its age — so typing the folder's name is the way to find every
conversation about it.

**A row with nothing to add says nothing.** A conversation about exactly the project it is
filed under draws no such clause.

Where the work itself goes is *Which folder does a task work in*, and how a conversation
comes to be about a folder is *Choosing a folder*.

## Switch between projects without leaving — work on two projects or two repos at once in one terminal

**One conversation can be about more than one folder.** Name the other project — `/folder`,
`/attach ~/code/other`, or the path in your own words when you ask for the work — and the
work you ask for goes there; you do not need a second conversation for a second repository.
What does not move is where the conversation is **standing**: its own working directory, its
`AGENTS.md` and its settings stay the folder it was opened in. *Choosing a folder* has both
halves.

For separate conversations with their own project settings, models and histories, codeaf
holds as many as you open over every door — opening another is never refused. One is on
screen and the rest stay alive behind it. Past twelve, a quiet conversation left alone
may be let go of; *How many conversations can one terminal hold* is the whole of that.

All of the following holds over the ordinary engine socket, `--host`, `--at` and
`--no-host` alike:

- **`enter` on home** opens any conversation, in any project, and leaves the one you were in
  open; `ctrl+t` on a row starts a new one in that row's folder.
- **`tab`**, pressed with an empty message box, goes to the conversation you were in before
  this one. Press it again and you are back. It is `cd -`.
- **`/new`** adds a conversation in this project — unless the one on screen is fresh and
  empty, in which case it takes its place.
- **the tab strip** above the transcript draws one tab per open conversation, and marks the
  ones stopped on a question.
- **`/quit`** closes the one in front and brings the previous one forward. It leaves codeaf
  only when that was the last one.
- **`ctrl+c`** closes all of them, on the press that lands and with nothing asked
  first. Work the codeaf service is running keeps going and is waiting when you open the
  workspace again; work running inside this terminal stops with it.

## How do I switch to my other chat — and is it still running

**`tab` with an empty message box**, or `space` `space` and then `enter` — home opens with
the cursor already on the chat you were in before this one. Either goes straight to it;
nothing is reopened and nothing is replayed from cold that does not have to be.

**Yes, it is still running.** A conversation you are not looking at is **not paused**: its
turn finishes into its own transcript, its tasks run, its background jobs run, its lock is
held and it keeps writing itself to disk. Its conversation remains available in home's `sessions`
panel while they run. A desktop notification tells you when it finishes a turn or stops on
a question — even while the terminal is focused, because a focused terminal is no longer
evidence that anybody is looking at *that* conversation.

`/status` says how many are open and how many want you: `2 open · 1 waiting`, and the tab
strip above the transcript draws them. A conversation stopped on a question is a row of
`needs you`.

## Does my draft move when I switch — what a switch keeps

**What you were in the middle of stays with the conversation you were in.** Coming back to
it redraws it from its own transcript, and these come back with it:

- the unsent sentence in the message box, and the pictures attached to it — including any
  message you typed while it was busy, which is folded back into the box rather than
  dropped, and the documents behind any compact paste chips in it;
- anything you had started typing at one of that conversation's **task pages**, kept per
  task: open the task again after coming back and your line is where you left it;
- where you were reading;
- how much of an approval countdown was left, given back to you whole rather than run down
  while you were away — and only if the conversation is still asking;
- the room you had open;
- the transcript, the task column, the meters, the model, the title and any card still
  waiting for an answer — all of which are read back from the conversation itself.

**These are forgotten:** copy mode, a rewind you were part way through, the settings panel,
the model picker, `/history`, the deliverables shelf, a task column focus. Each is
something you are in the *middle* of, or a door onto something the whole terminal shares.

`/new` is the exception, and deliberately: **the draft goes with you**, not with the
conversation. `/new` carries the box's text — and the documents behind its paste chips —
into the new conversation and clears it in the old one. What you had started typing at a
**task page** does not come with you: those lines belong to the conversation whose tasks
they were typed at.

## Searching from home — find an old chat from anywhere

**Just type.** There is no prefix and no mode: the box at the foot of home searches every
project on the machine as you type, live, and the panels give way to the matches.

It matches five things, and the first that scores highest wins the row: the conversation's
name, the project it is in, **the folders it is about**, the titles of the tasks it ran, and
**what those tasks came to** — the one-sentence outcome in the project's record. That last
one is the closest thing to remembering something by what happened rather than by what it
was called: typing `postgres` finds the conversation whose task outcome mentions the
connection pool, even though nothing in its name does.

Matching a project's name keeps every conversation in it.

**A folder's name finds the chats about it wherever they were held.** A conversation opened
in your home directory that spent an afternoon on `~/code/wisp` is filed under `~` and not
under wisp — so typing `wisp` finds it too, alongside the conversations held inside wisp
itself. Those come first: standing in a folder is a stronger claim on its name than being
about it.

Ranking is match quality first — a whole word beats a name that starts with what you typed,
which beats a word inside it, which beats the letters appearing in order. Then two things
break ties: a conversation **waiting on you** beats a cold one it ties with, whatever their
ages, and after that the more recent one wins.

`↑`/`↓` walk the matches, `enter` opens the highlighted one. `esc` clears the box and puts
the panels back; a second `esc` closes home.

**The matches grow upward out of the box, best one first** — see *Why is the best search
result at the bottom* below. On a frame 136 columns or wider the card beside them follows
the cursor, which is how you tell two similarly named conversations apart without opening
either.

## Why is the best search result at the bottom — the order of the matches

**The strongest match is the first conversation above the two typing rows — `ask here` and
`start a new conversation` — so two `↑` get you to it.** Each `↑` past that walks into a
weaker match, and `↓` comes back down toward the box.

That is upside-down next to an ordinary ranked list, and deliberately so. A list you read
*downward* puts its best answer at the top. While you are typing, home's list is read
*upward* out of the box — so its best answer belongs at the bottom, under your hand and one
keystroke away.

**The ranking itself is unchanged** — match quality, then `waiting on you`, then recency
(see *Searching from home*). What the drop-up changes is only which end of the column that
ranking is drawn at.

**A project's heading stays above its own rows.** The projects stack by rank and so do the
conversations inside each one, but a project name drawn under the things it names would
read upside-down. At rest there is no ranked list at all — home is the panels, read top
down.

## Can home search by meaning — semantic search

**No, and it is not going to.** Home's search is lexical and local — it matches the words
you type against names, task titles and outcomes already in memory, and answers inside a
keystroke with nothing loaded and no model called.

Two things cover what a meaning-search would have been for:

- **The row that offers to start a conversation never goes away.** A query that matches
  nothing still reads `start a new conversation: "…"` above the box, so the worst case of a
  search that missed is that your words become the first message of a new chat — which is
  very often what you wanted.
- **Ask the chat instead.** It has a `tasks` tool over the whole project record and you can
  ask it in sentences: *"what was that thing where we fixed the flaky auth test?"* Home is
  the fast layer; the conversation is the thoughtful one.

## Start something new from home — typing does all three at once

Whatever you type is **three things at the same moment**: a new conversation waiting to be
sent, a live query over the machine, and — if it starts with `/` — a command. You do not
choose between them before you start typing.

**Everything about typing sits together at the bottom of the screen.** The moment you type
a character the panels give way to a drop-up: the matches rise from the foot, and the
**last** row of the list is the action row — `start a new conversation: "…"` with your words
quoted back — sitting directly above the box you are typing into.

```
 alpha
 ○ Pricing Sheet Import                                                 2h
 ○ Pricing                                                             12m   ← two ↑

 ? ask here: "pricing"
 + start a new conversation: "pricing"
 ─ glm-5.3-flash:auto · ◇ asks ───────────────────────────────────────
 › pricing
 enter starts a new conversation and sends this · ↑ ask here · esc clear   project: ~/codeaf
```

**The cursor rests on the action row by default.** So typing and pressing `enter` starts a
fresh conversation and sends what you typed, however many matches are on screen. One `↑`
reaches `ask here`, which sits directly on top of it, and a second reaches the matches.

**Where it opens is at the right end of the keys row under the box, and `enter` honours it.**
The rule reads `glm-5.3-flash:auto · ◇ asks`: the model, effort and approvals start at the
left, and the keys row under the box ends with `project: <path>`, right-justified, naming
where the conversation will open (until 2026-09-22 the path stood at the rule's right).
The effort word follows a colon with no badge. The keys keep their room: a long project
path is cut on the right, one ellipsis, and goes entirely where the keys leave it less
than a word.
With nothing pinned the folder **follows the row your cursor is on** — walk onto another
project's row and the rule re-points — and with nothing under the cursor it is this window's
own project. `alt+p` pins it, `/model` pins the model, `alt+e` walks the rung and `alt+a`
walks the gate; *Change the model before starting* has the whole of all four.

One `↑` steps off that row **up** onto `ask here: "…"`, which answers the same sentence in
the pane on the right instead of opening a conversation for it — see *Asking from home*. A
second `↑` reaches the best match. The hint under the box tracks which of the two `enter`
means: the line in the example above on the action row, and `enter open · ↓ back to
starting a new conversation · esc clear` once you are on a match.

A line beginning with `/` is the third thing typing can be — a command, run rather than
sent. See *Running a slash command from home*, directly below.

Starting a conversation this way is `/new` followed by your sentence, so everything `/new`
does applies. On a surface with no fresh-session seam it refuses in `/new`'s own words,
`/new is unavailable here`. **Where the door itself fails, your sentence is not sent
anywhere**: on `new session failed: <error>` home closes and your words are put in the
message box unsent — never delivered to the conversation this window was already holding.

**Pasting a folder path into an empty home box offers one Enter to start there.**
The complete paste must name one existing local directory. The action row reads
`start a new conversation in <that folder>`. If the next key is Enter, it opens a
conversation there without sending the path as a message. Any other key — including
space, an arrow, Backspace, a shortcut or Shift+Enter — cancels the offer and keeps normal
editing behavior. A second paste also cancels it. The remaining text is an ordinary
message for the project selected at the right of the keys row; returning to the same path does not rearm
it. Clear the box and paste the folder path again to get a fresh offer.

Pasting into existing text, including whitespace or a newline, never activates the offer.
Typing a path or project name does not activate it either. Conversations and the `ask here`
pane retain ordinary paste behavior. Recognized slash commands such as `/home` still run
as commands. The folder notice may remain, but the path stays in the box.

## Running a slash command from home — can I type /settings on the home screen

**Yes. A line that begins with `/` is a command, and `enter` runs it rather than sending
it.** Typing `/settings` on home and pressing `enter` opens the settings panel; it does not
start a conversation whose first message is the word `/settings`.

**The screen says which `enter` you are about to press, before you press it.** With a command
in the box the action row reads `+ run /settings` in place of `+ start a new conversation:
"…"`, and the foot under the box reads `enter runs this command · ↑ ask here ·
↑↑ pick a match · esc clear`.

**Every command has a FATE here, and the list says which before you press `enter`.** Each row
of the `/` drop-up reads `<command>   <fate> · <what the command does>` — so `/compact` says
it will open a conversation first and `/quit` says which conversation it closes. The whole
table is in *What each command does on home*, one section down.

**The command list opens over home's box too.** Typing `/` shows every command above the
seam, alphabetically from top to bottom, with no thread, task, project or place results
mixed in. Keep typing to filter; the best name match is selected without changing that
order. ↑ / ↓ choose, PgUp / PgDown and the mouse wheel scroll, and Enter takes the row.
A command that takes words leaves `/model ` in the box ready for its argument. Esc clears
the home draft. Moving past the command word into its arguments restores ordinary search.
Other ordinary slash-command mentions remain prose. *Typing a slash to see the command
list* describes the shared token and filtering rules.

**A pasted path is not a command.** A folder pasted into an empty home box offers
`start a new conversation in <folder>` only until the next key. Enter accepts; any other
key returns it to ordinary message text. See *Start something new from home*.

**You can ask about a command instead of running it.** Type it and press `↑` then `enter`
— the `ask here` row — and the answer comes back in the pane on the right without the
command being run (see *Asking from home*).

## What each command does on home — the fate on every row of the / list

Home is not a conversation, so a command that is *about* a conversation cannot simply act on
one behind your back. This is every fate, in the words the drop-up draws them in.

| The words on the row | What you type | What happens |
| --- | --- | --- |
| **`pins the next conversation's model`** | `/model` · `/model <slug>` | The list opens in home's own body; the pinned model appears on the rule above the box. Nothing behind home is touched. |
| **`next conversation's folder`** | `/project` · `/project <path>` | Bare, opens the folder browser **aimed at the next conversation**; picking a folder pins it, with no duplicate footer message. With a path, pins that folder at once, opens nothing and says nothing. Either way `project: ~/src/parser` at the right of the keys row shows the selection, and neither road writes a second sentence over the keys. |
| **`opens the page`** | `/settings` `/set` `/config` · `/home` · `/search` · `/spend` · `/standing` · `/memory` `/memories` · `/history` · `/task` (bare) | A place replaces a place, exactly as before. |
| **`this list is /resume`** | `/resume` `/sessions` | Says `this list is /resume · enter opens a row` — home *is* that list. |
| **`onto home's tray`** | `/attach <path>` | The file — or picture — rides on home's own tray into the conversation you open next. Home says `attached · notes.md · rides with the next conversation`. A bare `/attach` opens the browser aimed at the next conversation's folder, and a file chosen there lands on the tray. |
| **`opens a conversation here first`** | `/files` · `/folder` `/place` `/dir` · `/manual` · `/crew` (bare) · `/permissions` `/perms` · `/connect` · `/harness` · `/subharness` · `/copy` · `/select` · `/rewind` `/undo` `/back` · `/compact` · `/export` `/save` · `/standing <words>` · `/task <brief>` | Opens a conversation at the target — the folder at the right of the keys row and the model on the rule above the box — then runs there. Home closes, exactly as `enter` closes it. `/manual` is on this road since 2026-09-22: it is a question put to the model, so it needs a conversation to be asked in. `/folder` joined it the same day — it gives THIS conversation a folder, and home has no this; the pin it used to be here is `/project`. |
| **`answers here`** | `/help` · `/status` · `/cost` · `/cache` · `/budget` · `/crew <preset>` · `/debug` · `/stop` · `/remember` · `/forget` · a word nobody defined | Answers with a note, and the first line of that note is put on home's own line under the box. `there is no command called /pricing · / lists them` is now something you can read. |
| **`runs on the conversation behind home`** | `/land` · `/land <folder>` · `/workspace <path>` | Acts on the conversation this window is holding behind the screen — not on the one `enter` would open — and its answer is echoed onto home's line. |
| **`a fresh conversation behind home`** | `/new` `/clear` `/clean` `/reset` | Replaces the conversation behind the screen and says `started a fresh conversation behind home`. It is not the same act as `enter`, which opens a conversation at the target. |
| **`closes the conversation behind home`** | `/quit` `/exit` `/q` | Closes it and says `closed · <its name>`. When it was the last conversation this terminal was holding, codeaf leaves. |

**The fate is never the half that gets cut.** On a narrow window the command's own
description gives way first, whole, and what `enter` will do stays on the row.

## Attach a file before starting — /attach on home, the tray rides into the new conversation

**`/attach ~/logs/server.log` typed on home puts the file on HOME's tray, and starts no
conversation.** The chip appears on the row above home's box, and the line under it says
`attached · server.log · rides with the next conversation`. When you then type a sentence and
press `enter`, the conversation that opens has the file already attached to its first
message. `/attach ~/shots/shot.png` is the same road for a picture.

**A drop does the same thing without a command.** Drag a file onto the window while home is
up and it lands on the same tray. So does a paste.

**A bare `/attach` opens the browser** (since 2026-09-22) — the same sheet a bare
`/project` opens, aimed at the folder the next conversation opens in; a file chosen there
lands on home's tray. Choosing `/attach` on the `/` list with `enter` opens it at once; the
`/attach <path>` row under it is for a typed path. (It used to answer `type the path after
/attach · or drop the file here`.)

**A folder after `/attach` is not a file.** `/attach ~/src/parser` on home pins the next
conversation's folder — the same decision `/project` makes — and updates the project
path at the right end of the keys row.

**The tray belongs to you, not to a conversation.** It survives walking into a conversation
and back out to home, and the chips you put on it here are the chips the next conversation
starts with. Home's tray is a reading and not a target: a chip comes off on the row above a
conversation's own box, where the `x` is. At phone width home draws no tray row at all; the
files are still there, and the conversation you open shows them.

## Change the model before starting — /model on home, the seam above the box

**The model the next conversation will answer on is written on the rule above home's box**,
at the left: `z-ai/glm-5.3-flash:auto · ◇ asks`.
Home and conversation seams both keep the complete model identifier, including the
organization before `/`, for the current model or a model pinned for the next conversation.
The project sits at the right edge of the keys row under the box. A long path
truncates at its right end before the project field disappears on narrow frames.
With nothing pinned that is this window's own model. Two doors change it, and they are the
same door:

- **`/model`**, or **pressing the model's name on that rule**, opens the model list in home's
  own body — the same filterable list `/model` opens in a conversation. Type to narrow it,
  `↑↓` to walk it, `enter` to take the row, `esc` to leave it alone. The foot while it is up
  follows the cursor and reads `↑↓ pick · → providers · alt+s sort · enter choose · ctrl+t
  effort · esc back` on a model, and `↑↓ pick · ← back · alt+s sort · enter choose · esc back`
  inside an open provider fold. `← back` stands beside `↑↓ pick` because both move the cursor.
- **`/model <slug>`** typed into the box pins it straight away, with no list.

**`→` and `ctrl+t` work here, exactly as they do under `/model`.** `→` or `tab` opens the
providers behind the model under the cursor and `enter` on one pins it — a provider pin
belongs to your home rather than to one conversation, so it is as writable from this draft
as from a live chat. `ctrl+t` walks how hard that model thinks, and what it dials is held on
the DRAFT: it is spent on the next conversation you start here and touches nothing behind
home. Both keys named themselves in the filter box for a while and neither answered; they
are on the foot now, and they answer.

**Home says nothing on the line under the box.** It used to read `model · glm-5.3 · for the
next conversation you start here`, and the rule above the box already carries the pinned
model — for as long as the pin lasts, rather than until the next note replaces it. The current
model is always bold and bright on the seam, whether pinned or not.
`alt+o` (`opt+o` on a Mac) does not open the model list on Home.

**It changes the draft and nothing else.** The conversation this window is holding behind
home keeps the model it had, and nothing is written down until a conversation actually opens
on the pin — at which point it is an ordinary model switch, note and all.

**The model and project selections last as long as this window does.** Starting a
conversation, returning home, moving the cursor or clearing the box does not reset the
selected project. A new window starts with its own default.

**`alt+p` is the same gesture for the folder** — press it, or press the path at the right of
the keys row, and
the target walks through the projects in the panel's order, including projects with only
standing work, and wraps after the last. Clicking a project name in the panel selects it directly. All three controls share one
selection, shown only as `project: <path>` at the right of the keys row. If `/project` selected a destination outside the panel,
the next cycle starts at its first project. With just one destination already selected,
`alt+p project` is absent.

**`alt+e` and `alt+a` are the same gesture for the two cells after the model.** `auto`
is how hard the next conversation will think — with nothing pinned, the `thinking` row in
`/settings` folded with any level set on the model — and `alt+e`, or a press on the cell,
walks it one rung: home says `thinking · high · for the next conversation you start here`.
`◇ asks` is what it will run without asking — with nothing pinned, the `ask before running`
row, or `YOLO` under `--yolo` — and `alt+a`, or a press, walks asks → guardian → YOLO →
asks, never onto `refuses`: home says `approvals · YOLO · tools run without asking, including overwrites ·
critical commands still ask · for the next conversation you start here`, and the cell
wears the warning hue while the gate is open. Both are carried onto the conversation
`enter` opens. **The rung lasts as long as this window does, like the model; the gate is
spent** — after the conversation opens, Home's approvals cell returns to the
standing choice. The selected project remains pinned. Neither cell is drawn on a window whose session has no dial for it, and over
`--host` the far machine's rows decide.

**Home and conversations share the model, effort and approvals controls.**
Other full-screen places have no general conversation message box; return Home
with `space` `space` (or `alt+1`) to start a conversation.

**`/project` is the third door onto the same pin, and it is the one that shows you the disk.**
Typed bare on home it opens the folder browser with the title `the next conversation's
folder`. Its action row reads `open the next conversation in · ~/src/parser`, and `enter`
there pins the target and drops you back on home with the rule already changed. Nothing on
that sheet touches the conversation behind home. `/project ~/src/parser` skips the browser
and pins the folder straight away without a word — the keys row's own `project: <path>` is
the answer; a path that is not a folder is refused as `no folder there · <path>` and
nothing changes. This was `/folder` on
home until 2026-09-22, when the pin became a command of its own.

## The dim sentence above the rule on home — what is that tip over the box, why did it change

The one dim line directly above the rule over home's box is a **tip**: one sentence naming a
key or a command you have not used yet, and what it does — `/project sets the project folder
for a new conversation`, `ctrl+t starts a fresh chat in this project`. It is drawn only while
the box is empty and nothing else is up, it moves on to the next tip every time you come to
home and every two minutes at rest, and each tip goes away for good the first time you do
what it names. It sits at the right, led by a bulb and closed by a small cross: clicking it
means ENOUGH FOR NOW, and the row stays blank until you leave home and come back, when a
different tip is there. The tip you put away keeps its whole allowance and comes round
again. **A conversation says its tips differently**: its tip stands at the right end of
the keys row, over the project, with the bulb and the cross, and it appears only after
15 seconds in which you have not pressed, clicked, scrolled or pasted anything. Both rows draw from the same one list. The whole list,
what makes each one appear and disappear, and the **disable hints** row on the Workspace
tab that turns them off, are on the *hints and tips* page.

## Typing @ on home — does the @ file list work on home, complete a path into home's box

Yes, since 2026-09-22. Type `@` and a letter or two into home's box and the same list a
conversation's box opens appears in home's column: files and folders under the folder the
next conversation opens in (the one at the right of the keys row), ranked as you type,
`folder` and `img` tags on the right. `↑`/`↓` pick, `enter` puts the path into your sentence
after the `@`, and choosing a picture takes the half-typed token out and puts the picture on
home's tray instead, saying `attached · shot.png · rides with the next conversation`. `esc`
closes the list and leaves the word alone — and it is the innermost layer of the three esc
walks through on home, so the first `esc` takes the list and a later one leaves home. Tasks
are not on this list — a task pointer is minted when a conversation sends, and home has none
yet. While the walk is still running the column reads `looking…`; with no match it reads
`no file matches`.

## How do I get back to the dashboard or the home screen from any page — press space twice

**From inside any conversation, press the space bar twice with an empty message box.**
That is the way back to home, and it lands with the cursor on the chat you were in before
this one.

There is no `ctrl+` chord for it: every `ctrl+<letter>` this surface has is already taken,
and `esc` was not available either — on an idle conversation it already arms rewind and
already drops a message you parked. What was left is the one keystroke that reliably means
nothing: a message that starts with two spaces is a message nobody meant to send that way.

**The first space types itself, plainly.** It is the *second* space, arriving to find a box
that still shows nothing with that space behind the cursor, that takes the whole draft away
and opens home. So a space you actually wanted is never eaten: space then `x` leaves ` x`.

**Wherever the door is drawn, two spaces open it.** That includes a box holding only blank
lines, from a `ctrl+j` or an `alt+enter` you did not mean. It also includes the other
places: the same two spaces, typed into a place's own empty box — the tasks roster's filter,
the memory filter and the search query — open home from there. Places without a box,
such as spend and standing, count the two spaces directly. The door still
loses to a space that already means something where you are standing: on the settings panel
space is the row's `activate` verb, inside a task's record `space` pages the card, and on
home itself two spaces type into home's own box.

It works with a turn running — `esc` puts you back in it, still running. It does nothing
when the box already has words in it. It works on a machine with one conversation, on one
with none, and over `--host` — where what opens is the **far machine's** home.

## What does pressing space twice do — the home door at the foot of a conversation

When the box is empty, the keys row under the box ends with:

```
/ commands · space space home
```

The model controls and chats shortcut can precede it. It costs no extra row — it is the keys row the frame
already has — and it **vanishes the moment you type anything**, because it is a door
and not decoration. It also goes while a turn is running, where the same row has something
more urgent to say (`esc interrupt`); the gesture still works then, it is just not being
advertised. (Until 2026-09-17 these words were the right end of the rule above the box.)

**It stays when a tip is showing.** A conversation's tip covers the project at the keys
row's right end, never the keys. Until 2026-09-24 a tip took the whole row and the
door went with it. One of the tips is about the door itself:
`space space takes you back to home`.

**You can click it.** A press on the words `space space home` opens home; a press on the
rule beside them is a press on a rule.

It appears on a fresh machine too, from the first minute, and over `--host` as well: a
machine with one conversation or with none still has a home to go to. The rule that keeps
home from *greeting* a first run is a different rule — not being greeted by home and not
being able to reach it are two different things.

## space space does nothing — why the gesture did not open home

Three reasons, and neither the machine holding nothing nor `--host` is one of them any
more:

- **The box had words in it.** The gesture fires only when the second space arrives to find
  a box with nothing in it a person would call text. ` x` and then two spaces is a draft.
  The dim line at the foot is the honest test: if it reads `space space home`, two spaces
  open home.
- **It was a paste.** Pasted text arrives whole and never reaches the key router, so two
  leading spaces in a paste are two spaces (*Is there a key for home?*).
- **Home is already open.** On home, space is a character in the search box.

A machine with one conversation, or with none, opens home all the same: its panels keep
their headings and the dim lines naming what arrives there (*Why is the home screen empty*),
not a refusal. So does a session over `--host`, which opens the **far machine's** home.

## how do I get back to home with one chat

Three ways, and they all work from the first minute on a fresh machine:

- `space` twice on an empty message box
- `/home`
- a click on the words `space space home` in the dim line above the box

The launch itself does not greet you with home while the only conversation on the machine
is the one it just opened — that is a rule about greeting, not about reach — so on a
machine with one chat, home is something you go to rather than something you land on.
What you find there is that chat as the first row of `threads`, saying `here`, and
this folder as the first row of `projects`.

## Is there a key for home?

Three of them. **`alt+1`** goes straight there from anywhere — home is the first of the
four places on the tab bar, and each answers to its own position, `alt+1` through `alt+4`
(the three places off the bar answer `alt+5` through `alt+7`).
**Space twice on an empty box** goes there from inside a conversation, and **`tab`** walks to
it from any other place. `/home` opens it too.

`alt+<digit>` arrives in every terminal codeaf runs in — it is sent as escape-then-digit and
has been for forty years — which is why the place keys are on `alt`. `ctrl+<digit>` has no
encoding a terminal can send at all.

There is still no `ctrl+` chord for home: the plain ones are all taken (`ctrl+.` is the
tasks place, `/history`).

## What landed while I was away — since you left, and the look stamp

Home remembers when you last **closed** it, and the `since you left` panel says what
finished after that — a line per task that landed, a line per file a conversation made, the
watches that fired and what memory learned (*What did it do while I was away*).

That is the whole mechanism **on home**: no badge, no list of unread things, and no mark of
its own on a row. (A session's own window does send a desktop notification when its turn
finishes or it stops on a question while you are looking elsewhere — see "Why a session says
it needs you" below. Home itself never does.)

Looking at the screen is what counts as seeing, so nothing is marked seen the instant it
appears. The panel holds while the screen is open — a refresh does not silently unmark the
news between two glances — and closing home writes the new mark for next time.

Three honest edges: the very first time home opens there is no "last time", so nothing is
reported rather than everything; work that lands in **the conversation this window is in**
is never news, because you watched it happen; and if codeaf is killed with home open, the
same work is simply reported once more on the next open — repeating news is the safe
direction to fail in.

## Why a session says it needs you — waiting on you

A session that has asked you something and can go no further writes that down, and home is
where you see it without opening the window it is in. It is a row of **`needs you`**, wearing
the amber `?`, with **what it is asking** under its title in the question's own words —
or `needs your ok to run <the tool>` for something it needs permission to run — and the
longest wait at the top of the panel.

**Five things count as being asked**, and they are one list so that no window can say
`working` about a session that is really stopped:

- an **approval question** — a tool call held at the gate;
- a **connect offer** — a service it wants to sign you in to;
- a **sub-harness offer** — a saved shape it wants to hand the work to;
- a **task proposal** waiting for your yes;
- an **adaptive run out of fuel**, parked at its gate until you top it up, finish on what
  is done, or stop it — its line is the gate's own, `out of fuel · $100.00 of $100.00`.

A session that gave no words for what it is waiting on shows no line at all rather than a
placeholder.

This is read out of a small file each live session keeps in its own folder, refreshed
every five seconds and believed for fifteen. So a window that was killed, or a laptop that
closed, stops claiming to need you within a glance.

`enter` on the row opens it. **For the ordinary questions you do not have to go at all** —
see the next section.

## Answer a question from home — approve a command in another window

**You can answer it here, without opening the window it is in.** The top `needs you` row
that has answers draws them on its second line, out at the right, while the row is being
read — under the pointer or the cursor — and **pressing the digit answers it, from anywhere
on home**, whether or not the chips are on the screen at that moment. No row says `enter`;
`enter` on any of them opens the conversation to answer it there.

The chips are the ones the question has:

- It is **waiting for permission to run something**: `1 allow once  2 always  3 deny`.
- It is **asking whether to start a task**: `1 yes  2 no`.
- It is **asking whether to keep an eye on something**: `1 yes  3 just once  0 not set up`
  — or `1 yes  0 not set up`, when what it is asking about is a **one-off reminder**, which
  has no `once` answer at all. **`0` is how you say no from home**: nothing is set up,
  nothing is run, and the card in that window settles as `not set up`. There is no
  `2 change when or where` here, on purpose: that answer is a request for a text box; open
  the conversation to say a different time or place.

`2 always` means what it means in the window: **that session stops asking about that
tool** for the rest of its life. It does not write a permission rule into your settings. The
handful of shapes codeaf always asks about — the ones that wipe a disk — are asked about
again whatever you press here.

The digits are keys **only while nothing is typed**. Every other moment a `1` is a `1` going
into the box, which is a search and a new conversation at the same time.

**A question raised by _this_ window closes home on its way in**, so the card is on the
screen you are looking at rather than behind it. Home then says `answered · deny` at the
foot and the row reads `answered · waiting for it to pick that up` until the other session
picks it up, on its next heartbeat. **An answer that arrives too late is ignored**, and a
window that has stopped refreshing for fifteen seconds cannot be answered from here: its
chips are not drawn.

A session that stops on an approval question while its window is unfocused also sends a
**desktop notification** — `<conversation> · waiting on you` — and a turn that finishes on a
window you are not looking at sends `<conversation> · turn done`.

## Why a task says incomplete on home

The project's record of its work is append-only: a task writes a row when it starts and
another when it lands. So a machine that lost power, or a codeaf that was killed, leaves
rows on disk that say `running` forever.

Home never repeats that claim. **It asks the session itself.** A live session says out
loud, every few seconds, which task nodes it currently has out; the task details show it as running only while the session still names that node.
Every other live-looking row is **work that was under way when the window went**, and it is
on no panel: the sessions place and the card beside a search say `incomplete` against it.

A session too old to keep that file, but whose journal a window is holding, falls back to
the older answer: the lock is asked, and its rows are believed. That is the same rule with
less to go on, not a different one.

## Home on a fresh machine, and over --host

A machine that has held nothing yet draws the same panels as a full one: each keeps its
heading and the dim line naming what arrives there, `projects` has the folder this window
opened in, and the conversation list has the conversation you opened it from as soon as there is
one (*Why is the home screen empty*).

Over `--host` home lists **the machine your session is running on**. The projects, the
conversations in them and the work each of those ran are read on the far end and carried
here, so what you are looking at is the server's afternoon rather than your laptop's — and
the right end of the tab bar says `on <machine>` so you can see which. Enter on a row opens
that conversation beside the one you are in rather than in place of it.

Two things a remote home does not do, and both are silences rather than sentences. **No row
is ever marked `that folder is gone`** — the folders are on the other machine and a stat here
would report every one of them as deleted. And in the fraction of a second before the far
machine's first answer arrives, home draws **no panels at all**: a dim line saying what
arrives there over a server full of work would be the one wrong thing this screen can say
about somebody else's disk.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window,
work somebody starts in a second terminal, or a session stopping to ask a question all
show up without you doing anything. There is no file watcher: home reads, and closing the
screen stops the reading. (Standing items — reminders, watches, rules — are a different
mechanism and do keep going; see the keeping-an-eye page.)

The resting screen does not animate, except the one spinner on the first working conversation row
(*Why does only one row spin*); the ages simply change on the next reading. In screen-reader
(linear) mode nothing ever animates.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.

## What is the ◦ row on home — the little circle, and where the things keeping an eye on your project are

**A standing thing — a reminder, a watch, a rule, an overnight job — is on home's panels
three ways:**

- **while it is asking you something**, it is a row of `needs you`, with the amber `?` and
  what it is asking under it;
- **while it is firing**, it is a row of `standing` like any other order — it is not a
  task and has no row on `sessions`. It is still the item — `ctrl+e` pauses it, `ctrl+x` stops
  it, and `alt+e` raises how hard it thinks;
- **while it is simply waiting for its time**, it is a row of `standing`, soonest first, with
  when it goes off at the right — `in 20h`, `mon 8:30`.

`enter` on a question row opens the conversation that asked for it; on a `sessions` row it
opens the conversation; on a `standing` row it opens the standing place.

The `◦` mark itself belongs to the standing place and to a conversation's own lines —
`◦ leave for the train · in 4m`. `∙` is a paused item there, and `◆` means the thing went off
after the last time you spoke in the conversation behind it. The standing place (`alt+5`,
`/standing`) is every promise this machine has made, with how much rope each has.

**Retired items are nowhere on home.** Something that fired once and finished, or that you
stopped, is a thing that happened — the `since you left` panel says so the morning after.

**Typing does not find them.** The box at the foot searches conversations, and a standing
row riding along under a query would be a row the query never considered; the standing
place is where every one of them is.

## How do I pause a reminder from home — the → strip, and ctrl+e / ctrl+x

Put the cursor on the item's row (or point at it). **`ctrl+e` pauses it** and **`ctrl+x`
stops it for good**, from any column, with no strip (on a `sessions` row this window holds,
`ctrl+x` asks to stop that task instead). **`→`** shows the row's options, including
`p pause it` or `r resume it` for a paused item. They appear below its description in the
middle column on a wide home, or under the row on a narrower layout. While they are
drawn those letters are the verbs. `esc` or `←` closes them.

A third chord, **`alt+e`**, raises how hard that item thinks — see "Make a reminder think
harder" below. Home says `paused · <your words>` or `stopped · <your words>` at the foot and
redraws the row from the store, so what you see is what is on disk rather than what the
keypress hoped for.

Home's box takes every letter, always, so a `p` is a `p` in your sentence wherever the
cursor rests — *unless the strip is on screen*. That visible strip is what buys the two bare
letters, and it is the only state on this surface where a printable key is not a character.

A window whose build cannot write to the store says `this window cannot change
it` rather than pretending.

`enter` on the row **opens the conversation that asked for it** — that is the answer to
"why did I get this?", whatever project it belongs to. Something you set up from home that
never became a conversation opens the standing place with the cursor on it instead.

## Make a reminder think harder — how hard a standing item thinks, and alt+e on its row

The machine's **thinking** row does not reach a standing firing at all. An install
dialled to `max` still does not turn every check on the machine into a deep pass,
and an item nobody has dialled asks for nothing.

**`alt+e` on an item's row is how you raise the one that deserves it.** Put the cursor on
the standing item — on home at rest it has a row under `standing`, or under `needs you`
while it is asking you something — and press it: the rung climbs one step each
press — `low`, `medium`, `high`, `xhigh`, `max`, then back to `low` — and home says
`thinking high · <your words>` at the foot. The item's sheet on a phone then carries a dim
`thinking high`, read straight back from the item's own document.

An item nobody has dialled says **nothing** at all about it, which is not the same as
`low`: it means nobody chose and no rung is sent. The rung is kept with the item, so it
survives closing codeaf, and it is what that item's firings **and** its checks ask for
from then on.

`alt+e` does nothing on a conversation's row: that rung belongs to the window that
conversation is open in, and the machine's own default is the `thinking` row of `/settings`.
A window that cannot write to the store says `this window cannot change it`.

## Keeping an eye on — the status line, and /status

When the project this window is in has something standing, the status row at the
foot of the frame grows one dim segment:

```
◦ keeping an eye on 2
```

**Nothing at all when there is nothing** — a line that permanently read
`keeping an eye on 0` would be a permanent reminder of the absence of a thing.
The glyph **breathes** — it becomes the same spinner every running thing here
wears — only while one of them is actually firing. The rest of the time it is
still.

`/status` adds one line under the same heading:

```
keeping watch   installed · last check 4m
keeping watch   while a window is open · last check 4m
keeping watch   nothing is checking · say "remind me…" to start
keeping watch   nothing is checking · background checks are off · /settings
```

It says **`installed`** when the machine's own timer is set up, so the checking
happens with no terminal open at all. It says **`while a window is open`** when
there is no timer but this window is running the pass itself, every five
minutes. When neither is true it says **`nothing is checking`** — nothing was
switched off, and the items are still there and still due; there is simply
nothing running the checks right now. The tail says which way it got there:
`say "remind me…" to start`, because setting up the first standing thing is what
installs the timer, or `background checks are off · /settings`, because something
has stood here before — so the timer went on once — and it is not on now.

The `last check` half is dropped when nothing has ever run. A window with no
ambient side at all — `--host`, a build without it — prints **no line**, and so
does one whose timer could not be read: an absent answer is left absent rather
than reported as "off".

## Why did a card appear asking me about a reminder — saying yes to something standing

When you say something that would keep working after this window closes — "remind
me at 6 to leave", "tell me when CI on main goes red", "every Monday post the
standup note" — a card appears in the conversation and **nothing is set up until
you answer it**:

```
╭─ ? ◦ every Monday at 9, post the standup ──────────────────────────────────
│ every Monday at 9, post the standup note from the git log
│ when · Mondays at 9am
│ where · for this project
│ costs · about $0.02 a run, at most once a day
│ [ 1 yes, set it up ]  [ 2 change when or where ]  [ 3 just once ]  [ 0 no ]
│ I'll keep doing this Mondays at 9am, for this project, until you stop it
╰────────────────────────────────────────────────────────────────────────────
```

Three bands make it different from the card that proposes a task: **`when ·`**, in
the words you said or the words it worked out; **`where ·`**, how far it reaches;
and **`costs ·`** — what one run may spend and how often it may run. A watch that
has to *look* at something adds `checked every 5 minutes`. If it made the timing up
rather than reading it off what you said, the band asks instead of stating:
`Mondays at 9am — you didn't say, so that's my guess. Right?`

**The answers**, by key, by `←`/`→` and `enter`, or by clicking one:

- `1 yes, set it up` — it gets set up and starts happening.
- `2 change when or where` — the box below becomes a place to say the **time or
  the place** you want instead: "make it 8", "only in this project", "everywhere".
  Nothing is set up until a new card comes with them in it.
- `3 just once` — do it now and leave nothing behind.
- `0 no` — nothing is set up, nothing is run, and the row settles as
  `not set up`. `esc` does exactly the same thing.

**The line under the answers says what the one you are on will actually do**, written
out of this card's own facts. A **one-off reminder's card draws no `3`**: "do it now" for
a line meant for six o'clock is the wrong thing at the wrong moment. The `0` is on every
one of them.

**There is no clock on this one**: no countdown, and no moment where it answers on your
behalf. If the turn ends with the card still up it says `ended · nothing was set up`. An
answered card settles in place, grey, with the answer and what it came to on its bottom
edge — in a conversation and in home's `ask here` pane alike.

**The first time you ever set one up** the machine's own timer goes on, and one dim line
says so, once, ever:
`checks every 5 minutes, window or not · background checks under /settings`.

## What does a ◦ line in the middle of my conversation mean — news from something standing

Once something is set up it writes **one line and never more** into a
conversation you have open — the one that asked for it when that is open, and
otherwise whichever one of that project you are sitting in (the keeping-an-eye
page has the whole order):

```
◦ every Monday at 9 · set up
◦ every Monday at 9 · said: the standup note is in notes/standup.md
? keep main green · your call: the fix touches migrations
∙ remind me at 6 to leave · stopped
```

**When the line appears.** If the chat is open when the thing fires, the line is
drawn **at once**, the moment the firing arrives. If the chat was **shut** when it
fired, the same line is drawn when you open it: one row for each thing that was
waiting, oldest first, above the first thing you type.

That is the whole of it. **A check that found nothing writes nothing** — a watch
that ran faithfully for thirty mornings and found nothing leaves your
conversation exactly as quiet as it was, and home's `since you left` panel is where
you go to confirm it really did look.

## Ask here — a reminder or a watch without opening a conversation

While you are typing, the row directly above `start a new conversation` is
`ask here: "…"`. It answers the sentence **in a pane of its own** — a real conversation
with a real transcript, kept outside `~/.codeaf/v3/projects` so home never grows a session
row for a one-off errand. One `↑` reaches it, and `ctrl+enter` does it without leaving the
box.

**Every exchange is a row below the conversation rows**, marked `?`, because it is
the thing you asked for a minute ago — with what it is doing in the tail:

```
 ? remind me at 6 to leave                          ? waiting on you
 ? what did we decide about pricing                 ⠹ working · 4s
```

**Several can be open at once**: a second `ask here` adds a row rather than replacing the
first. `enter` on the row hands the keyboard to the exchange, and **on the panels the
exchange takes the whole screen while it holds the keyboard**, at every width — the panels
have no column to spare for it; `esc` or `tab` brings them back, with the row still at the
end of the conversation list.

**An exchange outlives home.** Closing this screen does not end it, and neither does opening
another conversation; the row is still here, still waiting, when home opens again. It is
filed only once it is over, you have seen what it came to, and you have moved off its row —
or when you quit.

The whole of it — where the record goes, how the card is answered, how to turn the exchange
into an ordinary conversation — is on its own page: *Asking from home*.

## Where did the card go — no preview on the right of home, and why hovering changes nothing on the right side

**The resting home has no card and no right-hand preview at any width.** The columns are
panels now, and every fact the card carried has a row of its own: what a conversation is
stopped on is its `needs you` row, what is in flight is its `sessions` rows, what it made is
a `made <file>` line in `since you left`, and where you left off is the line under the
`here` row. The facts line is gone — a number is on the row where it is not zero.

**Mouse and keyboard navigation share one selected row.** Moving the mouse onto a row
selects it; the next arrow key takes over and removes the mouse highlight. A stationary
pointer cannot override that choice. `→` and its options always act on the latest
selection. Leaving the list keeps that selection, and there is no separate right-hand
preview at rest.

**The one card left is beside a search.** On a frame 136 columns or wider, while you are
typing, the match under the cursor has a card to the right of the list (*The card beside a
search*). Under 60 columns a row opens a full-frame sheet instead (*Opening a row on a
phone*).

**The machine's own card is gone too.** `↑` off the top of the column stays on the top row,
and each thing that card said has a place: `keeping an eye on` is the standing place
(`alt+5`), `today` is the `spend` panel and the pulse line, `agents` is the pulse's
`4 moving`, and `thinking` is the `thinking` row of `/settings`.

## The card beside a search — the preview on the right while you type

**While something is typed, on a frame 136 columns or wider, a card stands to the right of
the matches**, about the match under the cursor — or the one under your pointer while it is
on one. It is read top to bottom as bands with blank lines between them, and each band is
drawn only when it has something to say:

1. the conversation's **name**, the brightest text on the screen;
2. directly under it, one dim line of **where it is** — `~/codeaf · master, 1 file
   dirty · here` — a link that opens the folder in terminals that make hyperlinks;
3. the **question** it is stopped on, and the digits that answer it;
4. **the work** — each task and what it came to, three of them, then `▸ N more tasks`;
5. **the files it made**, as links;
6. **where you left off**, the last exchange;
7. **what is scheduled** and **news since you last looked**;
8. a dim line of **facts** — `touched 12 files · spent $1.25 · 34k tokens · last active
   12m`;
9. one dim line naming the strip: `→ verbs: close, new in project, open folder,
   copy project`.

It never moves while the list lifts under your typing, and it goes empty on the
`start a new conversation` row, because that chat does not exist yet. A frame too short for
all of it drops bands from the bottom and never touches the name. Nothing that is zero is
drawn.

## What do the keys on a home card do — open, new chat, folder, and copy path

**The card beside a search does not list letters.** Its last dim line names the strip and
the words instead — `→ verbs: close, new in project, open folder, copy project` — because
a letter is a verb only while the strip naming it is on screen.

**The chords work on any conversation row, card or no card**, and each acts on the row you
are looking at: the row under your pointer when there is one, the cursor's row otherwise.

| chord | what it does |
| --- | --- |
| `ctrl+t` | a fresh conversation **in that row's own folder** — the one you were in keeps running |
| `ctrl+o` | asks the machine to open that conversation's workspace folder |
| `ctrl+y` | copies the workspace path |
| `ctrl+e` | puts the conversation away, or pauses a standing item |
| `ctrl+x` | stops a standing item for good |
| `alt+e` | raises how hard a standing item thinks; nothing on a conversation row |

`y` and `n` are a question's own first two answers on the `→` strip — `y let it send`,
`n not this time` — and exist only while that strip is drawn.

A row whose folder is no longer on this disk says `that folder is gone · <path>` and starts
nothing. A failed folder open says `could not open <path>` on home's message line, and a
copied path says `copied <path>`.

## How do I answer without opening the chat — the verb strip on a row

**Press `→` on the selected row.** With an empty message box, the bottom row starts
with `→ options` to show this shortcut. The hint disappears when you type. Compact Home offers the same options shortcut as a tappable footer item. On a wide home, its options appear at the bottom of
its description in the middle column, wrapping as needed. On a narrower home without
that column, they appear directly under the selected row. **While the options are drawn,
those letters are the verbs and the box is asleep**:

```
1 let it send   2 not this time   x close   n new in project
esc or ← to leave · enter opens it instead
```

`esc` or `←` closes it, `enter` still opens the row, and moving off the row closes it too.
In the middle column the options leave the list in place. In the narrow layout the strip
pushes the rows under it down. The options are shown only while their shortcuts are active.

**The verbs are the row's own:** a question's first two option words on its own answer keys; a
conversation's `x close` and, where it has a folder, `n new in project`,
`o open folder`, `p copy project`; a standing item's `p pause it` or `r resume it`; a task this
window runs, `s stop`.

**`→` opens the strip on every row of the field, at every width** — the arrows never
leave the field (owner, 2026-09-17) — and a conversation's verbs are on their chords too:
`ctrl+e`, `ctrl+o`, `ctrl+y`, `ctrl+t`. The foot names none of them and says the same
sentence on every row. A project's row has no strip: clicking it selects the next message's project.

## The work on the right of home — what each task came to

**On the card beside a search**, the work band is the tasks this conversation ran, **name
first**, two lines each:

```
Port the Picker                          2h
  the roster resumes cleanly · 14 files

Fix the nil-map                          3h
  the parser handles nested tags · 2 files · $0.12
```

- **The first line is what the task was called**, and how long ago it landed, hard against
  the right edge. Work that has not landed has nothing at that right edge.
- **The second line is what it came to**, clipped to one line. The file count and the cost
  join it, each **only when it is not zero**.

**A task that is done wears no mark at all.** Every other state leads the second line:
`◐ running · <what it is doing>`, `◐ finishing · checking what it left`, `queued`,
`✕ incomplete · <what is still missing>`, `? your call · <what it is asking>`,
`■ stopped · <what it came to>`, and `✕ incomplete · a fault: <what broke>` — only that
last one coloured bad. **The word `failed` is nowhere on home.**

On the resting panels, `sessions` names the conversation that owns the work.
Completed work can also appear under `since you left`.

## How do I see more tasks on the right — ▸ …5 more tasks

**On the card beside a search, `▸ N more tasks` names the sessions place** out at the right
margin rather than unfolding, because a card is not the place that holds them; `ctrl+.`, or
`tab` onto `sessions`, is the way there. A band of files or folders with more behind it says
`▸ …3 more files`, and a click on that line — or `→` with the strip closed — opens it, and
`▾ …3 fewer` folds it back. The fold always cuts between tasks, never through one.

**On the resting panels, `sessions` folds as `N more`**, and `enter` on that line opens the
panel. The phone sheet folds its work band by family and counts all the
tasks hidden behind its fold.

## I clicked a task on home and nothing happened — open a task from the card

**On the card beside a search, click the task's own row** — the line carrying its name — and
the task's **record** opens: what it was asked to do, what it came to, the files it touched,
and the tail of its own output. It is the same card `enter` opens on a row of the tasks
place, so `esc` comes back one layer at a time.

On the resting panels, `enter` on a `sessions` row opens its conversation.
A pending task decision keeps its answer controls, and a landed line under
`since you left` opens the task record.

**Move the mouse over the card and the doors show themselves**: the line under the pointer
lights, and lines that do nothing stay plain.

## How much air is on the card — the four groups and the one gap law

**The blank rows on the card beside a search mean something.** It is four **groups**, and
the gap says which is which:

| group | what is in it |
| --- | --- |
| identity | what this is called, and where it lives |
| activity | what it is stopped on, what it ran, what it made |
| economics | what it cost |
| verbs | what can be done with it |

**One gap law, everywhere on the card: one blank row inside a group, two between groups.**
Two lines that want to be closer than a blank row are **one band**, which is exactly what
the title and the place line are.

**Air is the first thing the card gives up.** On a frame too short to hold the card at that
rhythm it is redrawn at the flat one-blank-row rhythm, and only a frame too short for
*that* starts dropping whole bands from the bottom. The phone sheet, which is one column and
not a card, keeps the flat rhythm.

## Why does the preview on the right look cramped — spacing, hierarchy and width

**At rest there is no preview on the right**; the panels are the page. The card beside a
search can look cramped for one of three reasons:

- **the gaps** — one blank row everywhere is a card on a frame too short for the wide
  rhythm; make the window taller;
- **the width** — the card is 36 cells at 136 columns and grows to 56 by 176, taking half of
  every extra cell; under 136 there is no card at all;
- **the shades** — the title is the brightest text on the screen, the section words are one
  shade under it, and the facts, fold lines, place line and verbs are dim. That is the whole
  ladder: three roles, no fourth hue, no bold under the title and no underlines.

## What a narrow home card does with a long row

A card too narrow for a line breaks it between its clauses rather than cutting the end
off. Whole facts move onto following rows: the final key, branch fact, file age, scheduled
time, news text, task file count or cost, and answer chip remain visible. A single clause
wider than the card is still clipped. The card's place line clips from the left so the
path's basename remains visible.

**The verbs line breaks the same way** — `→ verbs: close, new in project, open
folder,` / `         copy project` — keeping the comma on the row it ends and hanging the
second row under the first word.

A **panel row** on a narrow column gives way in its own order: the project tag goes first,
then the title is cut with `…` — and the age, `here` or `another window` stays at the margin
until the title would be down to twelve cells.

## What happened in this conversation while I was away — news since I last looked

**On the resting home, the `since you left` panel is where that lives** — one line per task
that landed, per file made, per watch that fired, each a door (*What did it do while I was
away*).

A conversation's own **news band** — `◆ N things since you left`, with each item underneath
reading `<age> · <words> · <text>`, for example `4m · keep main green · the tests passed` —
is drawn on the **phone sheet** under 60 columns and on the **card beside a search**. It
shows three items, then a `▸ …N more things` door. An absent or empty inbox draws no news
band at all, and looking at the band does not consume the news.

## Where are the files it produced — deliverables on a conversation

Every file a conversation made since you last looked is a **`made <name>` line of `since you
left`**, with the conversation's name at the right; `enter` opens the file.

The card beside a search and the phone sheet list the files a conversation produced, newest
first, as `· <basename>` with its age out at the right — `· report.md   2h`. The basename is
a clickable path in terminals that support file links. It shows three files, then a
`▸ …3 more files` door. A conversation with no indexed files draws no files band at all.

## Where did we leave off — the last exchange on a conversation

**For the conversation this window is in, it is the line under its `here` row** in `where
you were`: the last thing you said, in your own words. A launch that has not been spoken in
yet has no such line.

For any other conversation, the card beside a search and the phone sheet keep the two sides
of the last exchange together: your last message as one muted line beginning `› `, followed
by the reply in dim text wrapped to at most two lines. A conversation with no turns draws no
last-exchange band.

## Where does this repository stand — branch and dirty files on home

**On the project's row in the `projects` panel**, out at the right: `master, 2 files dirty`.
A changed branch can read `feature/home, 2 files dirty, ahead 1, behind 3`. The branch and
the dirty count are one clause about one checkout, so they are joined by commas. Every
unknown or zero clause disappears, so a clean repository on main reads only `main`, and a
folder that is not a repository adds nothing.

The card beside a search carries the same clause on its place line, after the address:
`~/codeaf · main, 1 file dirty · here`.

Home refreshes this reading for a folder at most once every five seconds, and a failed or
timed-out Git check draws nothing. The reading arrives a moment after home opens. A folder
that is no longer on this disk says `that folder is gone` in place of the branch it cannot
have.

## Standing on home — what is scheduled, reminders, routines, watches and rules, what is next up and when

**The `standing` panel, last of the seven**: every standing order this machine will act
on, from every project, **soonest first**, with the rules that simply hold at the end.
Four kinds of order stand on it — a **reminder** (`remind me at 6`), a **routine** (`every
morning at nine`), a **watch** (`tell me when CI goes red`, `when go.sum changes`) and a
**rule** (`never change the public API without telling me`). Each row is your own words
and **nothing at its right**:

```
 standing
   the 6am repo watch
   top movers before the open
   tell me when CI goes red on master
   never change the public API without telling me first
```

**When it happens is said once, in the row's description under the cursor, and each kind
says it one way** — a fixed sentence you learn once, the kind word first. It is there at
every width: beside the row in the middle column from 170 cells, and as the line under the
cursor's row on a narrower frame.

- a reminder: `reminder · goes off tomorrow 9:00am`
- a routine: `routine · every morning at nine · next tomorrow 9:00am · last: done, two branches landed`
- a watch: `watch · every five minutes · last looked 3m ago · found: the last five runs are green`
  — or `found nothing`, which for a watch is the commonest finding and a real one
- a rule: `rule · always`

Moments read `today 6:00pm`, `tomorrow 9:00am`, a weekday inside the week (`mon 9:00am`),
a date beyond it (`21 sep 9:00am`), and `now` once they have arrived. A routine that has
never fired has no `last:`; a watch that has never looked has no `last looked`. An order in
the middle of a pass says what the pass is doing instead of its clock.

**An order stopped on you is not on `standing`** — it is a row of `needs you`, with its
question, and comes back here the moment you answer. Paused, stopped and retired orders are
not on it either.

Three rows show, five in a tall window, then `N more`, which `enter` opens. **Every row is a
door into the standing place**, where the orders are kept and changed.

With nothing standing it keeps its heading and
`reminders, routines, watches and rules · "remind me at 6" or "every morning at 9"` — the
words that set one up. On a short terminal `standing` is the first panel to give way.

## How much did today cost — the spend panel on home

**The `spend` panel, pinned in the rail under `projects`**, is the day and the fortnight in three dim lines.
**Nothing under its heading is selectable or clickable**: the arrows step over its lines, the
pointer does not light them, and a press on one does nothing. The heading is the door — `enter`
cannot reach it, but a click on the word `spend` opens the spend place, as does `/spend` or `alt+3`:

```
 spend
   ━━━━━╌╌╌╌╌╌╌╌╌ 34%
   ▂▄▅█▄▆▅▇▄▅▂▅▂▄  14 days $204.36 · loudest sun $88.10
   glm-5.3 55% · opus 31%  ·  3 chats and 1 task today
```

- **The heading** says `spend`. Today's cost and allowance appear only on the top line,
  without a duplicate beside this heading.
- **A thin meter** follows once the day has spent a twentieth of the allowance.
- **The fortnight**, a block a day with today at the right, then `14 days $N · loudest
  <weekday> $M`.
- **Who it went to and what for**: the top models by share, then `N chats and M tasks
  today`.

Each clause gives way whole on a narrow column. With nothing on the ledger it is its heading
and `every chat and task is priced here`. `spend` never grows in a tall window, and it is the
second panel to give way on a short one. The money is the pulse line's and the spend place's
to the cent.

## What has this conversation cost — the spend band on the card beside a search

**A single conversation's bill is on the card beside a search**, on its dim facts line:
`touched 12 files · spent $1.25 · 34k tokens · last active 12m`. Each clause is independent:
zero or unknown files, spend and tokens are omitted, and a line with no true fact at all is
not drawn. The resting panels do not carry it — `spend` is the whole machine's day.

**It counts the talking and the work the talking started, in one figure.** The turns —
your messages, the answers, and the small calls beside them — are added up by the session
itself and written to the session folder at the end of every turn, so home can read them
without opening the transcript. Every task and every unattended run this conversation
commissioned is added from the project's task index. `spent $1.25` is those two halves
together.

`tokens` is input plus output as one sum, over the same two halves. `touched 12 files` is how
many files this conversation's work wrote, summed over its tasks.

`/cost` and `/status` inside the conversation still answer for the live session. A whole
**project's** figures are on the spend place.

## Home on a phone — waiting on you, tasks, since you left

Under **60 columns** home stops being panels and becomes an **inbox** across every
project. It is the same screen and the same data; the shape is what changes, because a
phone-width frame is walked one row at a time rather than scanned.

Top to bottom:

1. `sessions`: the fifteen most recent conversations, combining open tabs and saved history without duplicates.
2. Additional question rows, without a heading — conversations not already listed above,
   standing items that need a look, and ask-here panes holding a card, from **any** project.
3. Home ask exchanges retain their own answer rows.
4. `since you left` — what landed while you were not in the room.
5. **The projects.** This window's own project is drawn open with its remaining rows; every
   other project is one folded line — `▸ wisp   6 · 2d` — that `enter` or a tap opens in
   place.

Each section shows **three rows** and folds the rest into `▸ …N more`; `enter` or a tap on
that line opens it in place. A section with nothing in it is not drawn at all. **A row
appears once**: a conversation carrying a question is not repeated in the additional
question rows or under its project.

Sessions uses single conversation lines with status bullets; closed history is dimmed
in the same chronological list. Other inbox rows are **two lines** — the label, and its dim tail
indented under it. A tab with a pending question carries an amber `?` on its existing row,
including when the question belongs to a task inside that conversation.

**Typing still searches**, exactly as at every other width, with `? ask here` and
`+ start a new conversation` against the box at the foot.

A tap on `since you left` opens memory. Other section headings fold their section away.
Mouse motion does nothing at this
width — there is no hover on glass — and every key still works.

## Opening a row on a phone — the sheet, and ‹ back

There is no second column under 60 columns, so `enter` — or a **tap**, in one gesture
rather than two — opens the row's card over the **whole frame**. The top row reads
`‹ back`, the title and the place are under it, and everything below is the card's bands.

The order is what you can act on first: the answers, then the state, then
`since you left`, then the work, then what is next, then where it left off, then the
files it produced. The repository, the keys and the spend are behind one `▸ more` at the
foot; `→` or `m`, or a tap on that line, opens it.

Keys on the sheet:

| key | what it does |
| --- | --- |
| `esc`, `←`, or a tap on `‹ back` | back to the inbox, with the cursor exactly where it was |
| `enter`, or a tap on the title | open the conversation this card is about |
| `↑` `↓` `PgUp` `PgDn` `g` `G` | scroll the card |
| a digit | answer the question the card is showing |
| `→` or `m` | open everything behind `▸ more` |
| `→` then `p` / `s`, or `ctrl+e` / `ctrl+x` | on a standing item: pause it, stop it |

A standing item's sheet carries your words, where it is, when it goes off, what it has
done, `4 runs · spent $0.08`, `ran 3 times this week · $0.04`, and how hard it thinks when
you have said. A frame too short for all the bands **drops them from the bottom** — never
the title and never the answers.

**Rotating the phone costs nothing.** Crossing 60 columns swaps the inbox for the panels
and back, and the cursor, what you typed and any errands are all still there.

## Approve a command from your phone — the answer bands

On a wide screen the answers to another window's question are chips on its `needs you`
row — `1 allow once  2 always  3 deny`. Under 60 columns the same answers are drawn as
**full-width bands on the sheet**, one to a row, in the consent sheet's own shape: the
digit on the left, the label beside it, and the **whole row** is the target. A tap
anywhere on the band gives that answer, and so does the digit.

It is the same act and the same rules as at any width: the answers are the ones **that
session offered**, a window this one cannot reach is not answered, and once a key lands
the band reads `answered · waiting for it to pick that up` until the other session takes
it. A window with no way to leave an answer draws no bands at all.

The bar under the box is the phone's legend: at most three wide targets — `open · new · ask
here` on the inbox, `‹ back · open · more` on a sheet, `‹ back · send · more` on an errand.
Tap one, or press the key it names. Below width **24** the plain hint line is drawn instead.

## Main chat versus subtasks — why is the work nested on Home?

A row of the conversation list is the **main conversation**; opening it returns to its chat. The
conversation also appears in `sessions` while it is among the fifteen most recent.
Its tasks are nested under it in the Sessions tab, and completed work can appear under
`since you left`.

On the card beside a search and on the phone sheet, a conversation's tasks form a tree under
their actual parents. Clicking a task opens that task's record. A child needing a person
brings its family forward; work still moving comes before settled work. The compact preview
keeps three task names in parent-first order, so it never shows a child without its visible
ancestry, and `N more tasks` names the sessions place, which holds the full conversation tree.

## What home cannot do yet — stop another window's task, and a something is wrong panel

**Stop a task another window is running.** A `sessions` row held by another terminal says
`another window` and offers no stop at all — the stop is a door into this window's own
engine, and a verb that would end the wrong work or fail is left off rather than drawn.
`enter` brings the conversation here, where `s stop` on its row, or `/stop`, ends it.

**Tell you something is wrong with the machine.** There is no `something is wrong` panel
yet — a host that stopped answering, a model provider falling over, a missing key. Those
are said where they happen: in the conversation that met them, and on `/status`.

**Group by project or hide the quiet chats.** `alt+g` and `alt+q` are unbound; the
`projects` panel and the panels themselves do those jobs.

**Show a preview card at rest.** The card is only beside a search, on a wide frame.

**Change a panel's place or turn one off.** The seven panels and their order are fixed, and
which column each stands in is read off what it holds — a panel with rows is in the field at
the left, a quiet one in the rail at the right — so nothing about them is yours to set; only
their heights and their sides move with the terminal and the day.

## Conversation bullets — running tasks, answering and unread replies

Every conversation in Home has a bullet. A dim bullet means there is no unread
reply known to this window. A working mark means the conversation is answering or has
queued or running tasks or background commands, even after its answer ends. This applies
to the conversation in front, including one with no name yet, and those held in other tabs. The mark clears when the last
work finishes; one completed task does not hide another that is still running, and a
task waiting for your decision carries its `?` on the task's own row without hiding the
conversation's other work.
The first working row animates when no other Home row owns the spinner. An amber `?`
means there is an unanswered question and takes priority over those marks.
A bright filled bullet means a reply finished while you were away. Opening the conversation
clears that unread state. Recently closed rows keep dim bullets. These indicators
use this window’s live conversations; they do not infer unread history from other
windows or persist read status across restarts.

In ask here, the thinking, writing and running indicator starts animating as soon as
you submit, including follow-up messages. Linear mode keeps a still mark.

## How does Sessions behave on a short Home screen

Sessions keeps priority over auxiliary panels when the window is short. Running work
keeps its status bullet, with the most recently active running conversation animated.
A task awaiting your decision has its own question indicator; its parent conversation
does not repeat that indicator unless it has a separate question.

## Does a run create another conversation in Sessions or the chats menu

A run’s tab is a view inside its parent conversation. Home’s Sessions list and the chats menu keep one row for that conversation, using its conversation title. The run’s own tab remains available beside it.

## Why does a closed conversation say another window

Closing a tab hides that conversation from tabs and the chats menu. It does not
stop its work or release an agent still held by this window. Such a conversation
remains in Home history and can reopen here; it must not claim another window
holds it. The other-window label is reserved for a conversation actually held
elsewhere.
