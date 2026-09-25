# Reading a task's page — what is on this task page, what folds, and a page that looks stuck

A task's page is the whole of what the task did, drawn in the conversation: its
brief, its steps, its notes, and its report. It is built for the visit people actually
make — a glance to see whether the work is going right, and a steer if it is not — so
settled work opens first to an outline and the folded steps are one key away. This page
is about what is folded, how to open it, and what to do when the page seems empty or stuck at the top.

Click the task's name in the right-hand list to open its conversation, including
in the ordinary local window. While it loads, the page says so. Type a correction
there and press Enter to send it to that task; Escape returns to the main
conversation. Refreshing the task keeps opened blocks open and keeps an accepted
correction visible while its worker catches up with writing the transcript.
If reading fails, the page keeps any transcript already shown and says it is
retrying. A temporary read failure does not mean the task's history is gone;
finished tasks can recover their pages too.

## What is on this task page — everything a task's page shows, in order

A task opened from the rail starts with its full title, then the opening three lines of
its brief with a key to unfold the rest, then its declared checks. A part with nothing in
it is absent. **The folder a run's task works in is not shown**: it is the run's own
working copy, not a place you go, and each step's command is drawn without the change
into it, so the rows read as the work itself.

The brief stays in the head, folded to three lines with a key to unfold it. The work it
did is folded into chips it counts. The paragraphs it wrote as it went, each standing above the chip that
covers the work behind it. Anything you steered into it, drawn where you said it. The report
at the end. And pinned above all of it, one line saying what it is doing, how long it has
been going, what it has cost and how many calls it has made.

While the task works, its live work carries the same `↑`/`↓` token column the conversation
does — `↑` the task's newest request, `↓` what it has written — on a task running through
this machine's engine too (see "No token count inside a task" on the screen page).

Nothing here is thrown away — what is folded is one keypress from open.

## When did this task start — the task page says nothing where the time should be, or `started` is blank on a finished task

**The time on one task's own page comes from that task's record.** Its completion card says
`started 14:02` only when the record carries the instant the work began. Reopening the
conversation does not replace that instant with the time you sat down.

**How long it ran is the record's too.** The settled page's header and the completion card
show how long the work took, rounded to the second — one figure, spelled `29m 8s` in the
header and `29m08s` on the card. It is the time the work itself took, as the record gives
it: the time you took to answer a task that landed needing your look is not added, so a task
that worked five minutes and that you accepted an hour later still reads `5m`. A record
that gives no such figure but carries both instants shows the landing less the start. While
the work runs, the side list counts from the record's start, even in a window opened after
it began.

An older record may carry a duration but no start or landing instant. When that duration is
at least one second, the settled task page shows it in the header — for example `12m` —
while the completion card omits the entire `started 14:02` segment. A shorter or absent
duration leaves the header figure out too. The duration is never used to invent a
wall-clock time.

## The card quotes three backticks instead of a sentence — a code fence at the top of a task's report

**The quoted line on a landed card is the report's first line that says something**, not its
literal first line. A task that answers with a diff, a command's output or a JSON block
opens its report with the code fence around that answer, and a fence marker — three
backticks or three tildes, with or without a language word like `go` or `diff` after it — is
punctuation rather than a sentence. Blank lines and fence markers are passed over, and the
first line that is neither is what the card quotes.

So a report that is nothing but a fenced block quotes the first line inside the block, which
is the answer itself:

```
✓ ◆ Fix nil-map crash · done · 4m12s · 3 files
  "the guard is in and the regression test passes" · started 14:02 · ctrl+o output
```

A report with no line to quote at all draws no quotation marks — an empty pair would be the
card claiming the work said something. The card falls back to its subtitle and, failing
that, to `started 14:02` on its own.

Until 2026-09-11 the card took the report's first line literally, so work that answered
inside a fence drew a quoted half of three backticks and nothing else — the one line the
card exists for spent on the punctuation around the answer.

## Why most of the work is hidden on a task page — the `▸ worked` chips, the caption outline, and `ctrl+e`

**This is the answer to "my task page is hiding most of the work", "where did the tool
calls go", "why can I not see what the task did", and "what are these little grey lines".**
Nothing is lost. Settled work is folded, and one gesture opens it.

Between each paragraph the task wrote is the thinking and the calls that led to it, and
that stretch collapses to one dim chip:

```
▸ worked 2m · thought 10s · 14 tool calls · ctrl+e
```

The figures are counted from the rows the chip covers — how long that stretch took, the
thinking time when there was any, the number of captioned steps, and the real call count.
The chip is still counted fact, not a summary.

**Three ways open the outline**, and any of them works on a page that has already finished:

- **`ctrl+e`** over an empty message box opens the newest chip. Press it again to close it.
- **click** the chip.
- **scroll up** when the page is already at its top — one wheel tick, `pgup`, or `↑`. It
  opens the chip nearest the top and keeps your place: the rows you were reading stay on
  the same screen lines and the work appears above them. Nothing jumps. The same gesture
  opens a folded run of tool calls, so scrolling up keeps reaching further back rather
  than stopping dead.

An opened chip stays open, and the chip line stays with it so you can close it again.
Under it is one caption per step: the short line the model said before that batch, the
plain floor made from the calls when it said nothing, or — after a long silence — a line
from the dwell narrator. Click a caption, select it and press `enter`, or use `ctrl+o` on
the live caption to open the tool rows under that one step. The outline is the account;
the rows are one expand further.

**A caption can be wrong.** It is a heading about what the step is trying to settle, not
proof of what ran or what came back. The tool rows beneath it are the truth. Open the
caption whenever its words and the work appear to disagree.

## What is that line — caption, why did it collapse, and how do I see what it did

The short status line over a batch is its **caption** — one sentence of about
5 to 10 words naming what the step is doing and where. A collapsed stack of
those lines is the **outline**. On a narrow window a caption wraps; it is never
cut mid-sentence with an ellipsis. A caption ends only where its sentence ends,
so a filename, version number or path keeps its whole self: `reading livesteps.go`
is not shortened to `reading livesteps`, and a caption never begins in the middle
of a word. It says what each step is trying to settle rather than repeating commands
the rows already name. A live caption may shimmer while its rows are folded; opening
it stops the shimmer and shows the running calls.

Use **`ctrl+e` on an empty box** to open the newest `▸ worked` chip onto the outline.
Then click the caption, select it and press `enter`, or press **`ctrl+o` on the live
caption** to see what it did. Press the same gesture again to fold those rows. A caption
keeps the present-tense words it was born with after the work ends, so an old outline may
say `checking the parser` rather than rewriting history to `checked the parser`.

**Keep everything open**: set `ui.work` to `open` and no chip on any page starts folded —
in a task's page exactly as in the conversation.

### What stays on the page whatever happens

**Every paragraph the task wrote stays standing**, above the chip that covers the work
before it, and so does the report at the end. So the page reads as the story of the work
with its steps folded, rather than as a scroll you have to read to find out what
happened.

**What the task is doing right now keeps its caption standing.** Its tool rows may fold
under that live line; `ctrl+o` or a click opens them. A long live run that has no caption
yet still uses the older overflow fallback described below, so silence never removes the
door to its calls.

**These never fold either**, wherever they are on the page: your instruction, every
correction you typed into the running work, every call that failed, and every question the
task asked you. They are the record of what you asked for and what you decided, and a
fold may never hide your own words.

### Why the conversation's chips are cut differently

Out in the conversation the page folds **by turn** — one chip per question you asked,
hiding the work between the question and its answer. A task's page folds **by phase**
instead, because a task is one long question and folding it by turn would put the whole
page behind one chip.

## See earlier tool calls in one long run with no caption yet — the `↳` fallback, scroll up or `ctrl+o`

When one live stretch has no caption yet and has made more calls than fit, the page keeps
the old fallback: a screenful of the newest ones above a dim line reading:

```
↳ 87 earlier tool calls · scroll up or ctrl+o
```

Three ways open it: **scroll up** at the top of the page, **`ctrl+o`** with an empty box,
or **click** the line. Scrolling back down to the newest line re-joins the live edge — the
page follows the task again as it works — and the opened calls stay open. A task's page
keeps its own fold state, separate from the conversation's; `ctrl+o` inside a task never
folds or unfolds anything in the conversation you left behind.

`ctrl+o` and `ctrl+e` are different keys here: `ctrl+o` opens the live caption's rows,
this no-caption fallback run, or the long instruction at the top of the page; `ctrl+e`
opens a `▸ worked` chip onto its caption outline.

**How many calls the page keeps on screen**: as many as your window is tall, and never
fewer than three. The number is taken from the window at the moment the page is drawn, so
resizing the terminal, growing the draft by a line, or the task roster changing its width
all re-fit it. Only the overflow folds. The conversation keeps exactly the last **3**
calls in the no-caption fallback above a line reading `N earlier tool calls · ctrl+o`,
and scrolling the conversation never opens a fold — `ctrl+o` or a click does.

## What the line at the top of a task's page tells you

**It is drawn under the conversation tabs, not above them.** Those say which conversation
this task belongs to (see *Conversation tabs* on the screen page); this header says where
you are inside that conversation.

The pinned header is the whole glance, and it says as much of this as your frame is wide
enough for, in this order:

```
⠙ main ▸ Port the loader · working · 3m 20s · $0.42 · 14 tool calls · bash
```

`main ▸ Port the loader` is the trail — the conversation, then every piece of work
between it and this page. **Every step of it but the last is a door**: clicking one opens
that page, clicking `main` comes back out to the conversation, and a `…` stands for steps
a narrow frame could not spell and opens the nearest one it hid. The last step is the page
you are standing on and does nothing.

The task's name first — that never gets cut while there is room for it — then what it is
doing, how long it has been going, what it has cost, how many calls it has made, and what
is running right now. While a request is out and has not come back, that last fact is
**that call's own clock, and its rate while there is one to measure** — see *A call has
been open for ages* below. A stretch where nothing has arrived for ten seconds and no call
is open says `still working`, which is the truest thing the page can say about a silence.

**Anything nobody has published is simply absent.** A task that has cost nothing shows no
cost, one that has called nothing shows no count, and a queued one has no clock — a figure
that is zero is a figure nobody measured. On a narrow terminal the line gives up facts
from the end, in that order, and never cuts the name.

The first of those facts — the word for what the work is doing — has a fixed vocabulary,
and the next section lists every word it can be.

## A call has been open for ages — the page says nothing, is it stuck, how long has this request been running

**It says how long, now.** While a request is out and has not come back, the last fact on
the header is that call's own clock, counting up, with its rate beside it while there is a
rate to measure:

```
⠙ main ▸ Port the loader · working · 12m 4s · $0.42 · 14 tool calls · 11m 38s · 47 tok/s
```

The two clocks are different things and both are worth having. The first is the whole
task's age. The last is **this request's** age — the one that has not answered yet.

This used to be invisible, and that was the worst silence on the page. A model call that
is still streaming has written no row yet, so nothing the page could see knew about it,
and a call that ran twelve minutes looked exactly like a task doing nothing at all. The
page reads the work's pulse for it now, which is the one place that knows a request went
out and has not returned.

**When the call comes back the clock goes away** and the line returns to naming whatever
is running. A task that is not running, a call that has answered, and work whose pulse has
been taken away with its landing all draw nothing there — a figure nobody is measuring is
a figure this surface does not print.

The rate is the same one the status line at the bottom of the screen draws, in the same
words, so the two can never tell you different things about how fast the same call is
going.

## What the word at the top of a task's page means — the task page header says sizing the work, queued, checking what it left, the header word on a task's page

The pinned header of a task's page carries one word for what the work is doing right now.
Every word it draws:

- `queued` — admitted and not started; nothing is in its way but a free slot. Where it is
  held behind named work instead, it reads `waits: <the work it waits on>`.
- `sizing the work` — the task's worker asked to hand parts of its work out and is waiting
  while a reading decides whether, and how. A brand new task never waits on it: its worker
  starts at once, and any parts somebody drew are weighed beside it. While the reading
  runs, the page shows the request it is waiting on under the worker's last line (see
  *Why is a task I just started showing an empty page*).
- `working` — its worker is getting on with it. This is the ordinary one.
- `checking what it left` — the worker has stopped and what it produced is being read.
- `closing gaps · round 1 of 2` — the check found something and a round is closing it.
- `finishing` — a gap is being tied off on work that is otherwise done.
- `waiting` — still running, but its calls to the model are being paced.
- `stopping` — you ended it and it is still letting go; `stopped` once it has.
- `your call` — the machine has taken it as far as it can and the decision is yours. It
  never stands alone: the reason rides beside it, so the header reads
  `your call · nobody could check it` or
  `your call · conflicts with your branch: parser.go`.
- `merged`, `branch kept`, `in your own folder`, `conflicted`, `stopped`, `done`,
  `incomplete` — it is over, and the word says how it ended: its branch came home; the work
  is finished and left on its own branch because your checkout is one codeaf will not write
  to; there was no branch to bring home, so it edited your own files; the merge clashed; it
  was ended early; it is over and nothing was said about a branch; or it ended without
  finishing, with the reason beside it (`incomplete · ran out of steps`). **There is no
  `failed`** — the engine keeps that name for itself and what you read is `incomplete` and
  a plain sentence.
- A sub-harness being designed says what it is doing in its own words — `designing`, and
  `awaiting your look` while its page sits waiting on you.

**`briefing a worker` is not one of them.** That one is on the status line under the message
box in the conversation while your turn is being handed to a task — before the task, and its
page, exist at all.

## The line above the box on a task's page — which model is serving this task, the task's thinking rung, `◇ on its own`, why alt+a does nothing in a task, the seam inside a task room

The rule over a task page's message box is the same seam a conversation has over its own
box, with the task's facts on it and the way out where the conversation's name would be:

```
─ room · esc/←← main · task glm-5.2 (friendli):high · ◇ on its own ──── ↑↓ history ─
› steer the node
```

Left to right: the way out, **the task's own model** (led by the word `task`, and
`next model …` once you have retargeted it) with the machine answering for it in brackets,
**how hard this task thinks** — the rung set on
it, or the rung it inherited from your conversation — and **`◇ on its own`**, then the
machine answering for it, `via friendli`, whoever served it.

**The model and the rung are doors, while the task can still be moved.** Press the model
name, and the picker opens aimed at *that task*: it switches from its next request on and
your conversation's model is untouched. Press the rung, or `alt+e`, and the task's rung
walks one step — the note reads `task 9 · thinking · high · its next call takes it`. A task
that has finished, failed, been stopped, is your call, or is a node inside an adaptive
run keeps both words as facts that do not react.

**`◇ on its own` is a reading, not a wheel.** A task runs every tool without asking — a
dangerous command is refused rather than asked about — and there is no posture you can walk
a task to. So `alt+a` inside a task's page changes nothing and says so: `approvals · a task
runs on its own — its tools do not ask · what this conversation runs without asking is on
the conversation's own seam · esc`. Your conversation's `◇ asks` / `◇ YOLO` cell is one `esc`
away, on its own line, and the status line inside a task's page never draws the `YOLO`
badge.

## The status line at the bottom of a task's page — no provider, no tok/s in a task room, why is the rate blank inside a task

The bottom row of a task's page is the keys — `x stop`, `↑↓ history` — and nothing
else; the numbers are on the line above the box, after the task's own cells (the section
above), since 2026-09-17. The task's own name is on the breadcrumb bar at the top of the
page — the room chip that used to lead the bottom row (`⠋ Ship the parser fix`) went with
that row.

At the right end of the line above the box: the task's live rate while its model is
writing — `38 tok/s` — or the task's own phase words while it is in a stage that is
producing nothing, exactly as the conversation's line reads them: `running go test · 41s`,
`connecting · 1.2s`, `paced · retry in 6s`.

**The machine is named whoever served it** — `task glm-5.2 (z-ai)` on the seam when the
vendor serves its own model — and while the task's first answer is still being written, as
soon as the machine writing it has named itself.

**It does not go quiet because your conversation's own turn has ended.** That is the ordinary state
while a task runs: you hand the work out, your turn ends, and the task works for minutes
with nothing happening in the conversation (the conversation's own row says `working`
through it, the state word's own table on the screen page says why). The row keeps
drawing the task's own reading throughout.

**What is on that row and is still the conversation's**: the bill, the cache, the context
meter and the job count. Those measure a session, and a task
runs inside yours — the cost figure already includes what your tasks have spent (see
"What the `$` on the status line counts" on the screen page).

**A task that has said nothing recently draws nothing** — no machine and no rate, rather
than the conversation's clock or another task's. A stage that is genuinely still going
says so again every few seconds, so an empty right edge means this task is between things,
not that the row has lost track. `esc` leaves the room and the row is the conversation's
again.

**Two tasks on the same model each keep their own reading.** Until 2026-09-10 they did
not: the surface filed this news by model, so two tasks on one model overwrote each
other, a room could show no rate and no provider at all, and the row could carry a stale
line left from the conversation — `running ask · 4m 55s` on a page whose task was writing.

## Can't scroll in a task — the wheel and pgup do nothing

You can. The wheel, `pgup`/`pgdown`, and `↑`/`↓` over an empty box with no history to
recall all scroll a task's page. If the page is short — the task has said less than a
screen's worth — there is nothing to scroll and the page hangs from the top with the
slack under it, exactly as a young conversation does.

If the page looks short because most of it is behind chips, that is the fold doing its
job: `ctrl+e` or a scroll up at the top opens it.

## Task page is empty, looks stuck, or hangs at the top with a blank below it

A page that shows a few rows at the top and empty space beneath is a task that has not
said much yet: the space is the slack under a short page, the same as a new conversation
shows. It fills from the top as the task works, and the page follows the newest line
until you scroll up.

Three things that look like the same picture and are not:

- **A task that has finished** ends its page with `this task has finished — say it to main`
  — and, where it was spawned under another task,
  `this task has finished — say it to main, or open its parent, Ship the port`. There is
  nothing more coming; scroll up to read what it did, and the foot names where the words in
  your box can still go, because the box is still there and the worker is not.
- **A task that has not started** — one still queued behind the running ones — opens
  with its title, brief and declared checks before any steps have been written. The
  page then says `nothing on this page yet — it fills in as the task works`. The roster's row for it
  says `queued`; the page fills when it starts.
- **A row that was never a task.** A background job — a server, a build, a watch, a video
  render — sits in the `jobs` section under `tasks` and `standing`, not among the families,
  and its page is a card, not a chat, because a job has no agent and writes no transcript.
  What it shows is the name, the handle `job 4`, the command, the clock or ending, and the
  end of its log. A job that has not written its first line yet draws no tail and no error.
  Its row is the one under the `jobs` label.

No task page ever draws an empty body under its header. Whatever is true of the task,
the page says it in one dim line, above whatever else it already knows:

- a finished task whose transcript is gone from the disk keeps its report and says
  `this task's transcript is not here any more`
- a task that is queued, or one still working with nothing written for it yet, says
  `nothing on this page yet — it fills in as the task works` — except while a model is
  being asked on its behalf, when the line is that request instead
- a page waiting on a read that is genuinely in flight — which is what opening a task on
  another machine or in another conversation does — says
  `loading this task's conversation…`. A page with nothing on the way never says it
- a read that came back with an error says `couldn't read this task's conversation ·
  retrying`, which is a different fact from either of the two above and keeps its beat

**A run's task page follows its newest step the same way.** The page `enter` opens on a run's
row — a row drawn in the tasks place — keeps up with the work: the newest step walks in at the
bottom, and the page stays stuck to that live edge until you scroll up, which releases it.
Scrolling back to the bottom resumes the follow, and a page on a task that has settled is a
still page, never re-read. The step being run right now is drawn one step early, the running
glyph beside its command.

## Why is a task I just started showing an empty page

It should not, and if it does the version you are on is older than this page. A task
opened the second it starts has journaled nothing yet — its first message is still being
written — so there is no transcript to replay for a few seconds.

What the page draws in that gap is what it already holds: **the full title, the folded
brief, and the declared checks**, followed by a note about what the task is doing now when there is one. Then it says why there are no steps yet.

```
Widen the import pipe so the nightly run stops timing out.
rate limited
nothing on this page yet — it fills in as the task works
```

Three things worth knowing about that page:

- **The head stays.** The title, brief and declared checks remain when the first
  step arrives. Only the note about there being no steps leaves the page.
- **Opening it starts nothing.** The page is a reader onto work that is already running;
  pressing the row again closes the page rather than starting anything, and no task is
  ever run twice by looking at it.
- **It does not repeat the header.** The header above is already spending its one word on
  the state — `working`, `waiting`, `queued` — with the clock beside it. The body gives
  the reason underneath that word, which is what the header had no room for.

A background job whose log is empty or unreadable draws no tail and no error — a job that
has written nothing yet is a job that started a second ago — and never the old sentence
`a background job keeps a log, not a transcript`, which is gone with the composer that
used to sit under the page.

The line comes off the moment there is anything to draw, because it answers one question
— why is there nothing here — and a page with something on it is not asking it.

**While the task says `sizing the work`, the page shows the request it is waiting on** —
under the worker's transcript, or in that line's place on a page with nothing written yet:
the thinking mark while the model thinks, which model of how many, how long the request has
been out, what has come back (`↓`, thought included) and the machine answering:

```
✳ asking deepseek/deepseek-v4.1-flash · 1 of 2 · thinking 41s · ↓ 4,465 · deepinfra
```

Before anything comes back it reads `first word 3.1s` and has no mark, no `↓` and no
machine. It goes the moment the reading ends.

## Watch a background job's log — the page tails it live, where is the log

Press `enter` on the job's row in the `jobs` section, or click it. Opening a job opens a
**page**, not a chat: a full-frame card with the name (or `job 4` until one arrives), the
handle and clock or ending (`job 3 · exited 1 · ran 49s` — the same word the section's
row uses, never the engine's own `failed`), the command, the log tail, and a foot. The
rule and the foot sit under the last row the page drew unless the log is long enough to
scroll, in which case they are at the bottom of the frame. The title is the name
or the handle — never the raw command, which the body draws once. There is no composer on
it at all.

The log is the last **200** lines of the file, oldest at the top and newest at the bottom,
dim, re-read four times a second for as long as the job is running. Colour codes and
control characters in the output are stripped before anything is drawn. The **absolute log
path is on this page**, in the foot — not on the row. `c` copies it; the confirmation
begins `copied `.

The page follows the newest line as it grows; `↑`/`↓` scroll, and scrolling to the bottom
re-joins the live end. When the job ends, the page takes **one more reading** — a process
writes its last lines and then exits, so the reading taken at the moment it exited would
be short of the ending.

The keys, quoted:

- running: `x stop it · c copy path · m puts it in your message · ↑↓ scroll`
- settled: `c copy path · m puts it in your message · ↑↓ scroll`

**`esc back` is not on either of them, because the head is already saying it** — in the
right corner of the title row, where your eye lands when the page opens. Naming it again
at the end of the foot spent two of the page's words on one instruction. Where a long
name takes the whole head line there is no corner left, and then the foot carries the way
out, **last**: `x stop it · ↑↓ scroll · c copy path · m puts it in your message · esc back`.

A narrow terminal fits these feet by dropping WHOLE clauses — never half of one — working
backwards from the end, and the last clause is kept to the last cell there is. So at
forty columns the running foot reads `x stop it · ↑↓ scroll` and the settled one
`↑↓ scroll`; `m` and `c` are gone off the line and still work. `x` is first on the running
foot because stopping something is the one thing on that page you cannot do from anywhere
else, and the scroll is last because it is the cheapest clause worth keeping.

`x` stops a running job (the tasks page has the confirmation). `m` drops the name, the
handle, the ending, and the last few log lines into your message box underneath, then
closes the page. Over `--host` the body says `its log is on ` plus the host name, because
the file is on the engine's machine; `jobs output 4` is how you read it there.

A job that has written nothing yet draws no tail and no error. The old feet
`this log grows as the job works — say it to main` and
`a background job keeps a log, not a transcript` are gone: there is no composer to refuse.

## Each task page has its own message box — I opened a task and my typing vanished, whose words are in the box

**What you type at a task stays at that task, and what you typed for the conversation
stays in the conversation.** Open a task's page and the box is that task's own: empty
the first time, and holding whatever you last typed there every time after. `esc` brings
the conversation's box back exactly as you left it, with the caret where it was.
Clicking from one task straight to another does the same between the two of them.

Each box keeps its text, its caret, its compact `[paste 1 · 42 lines]` chips and its
tray. `enter` sends the box in front of you and clears only that one — steering a task
never spends the sentence you were writing for the model, and a message the engine
refuses leaves its words in the box that holds them.

Two things worth knowing:

- **A task page's line is kept on disk, like the conversation's.** Close the window or
  crash, open codeaf here again, click that task: your half-typed correction is in the
  box with its documents and its tray. It is never restored into the conversation, and
  never into another conversation's task with the same number (the keys page has the
  detail).
- **A task page's line belongs to that conversation.** `/new` carries the sentence in
  the conversation's box into the new conversation, as it always has, and leaves the
  task pages' lines with the conversation they were typed into.

Older builds had one box for everything. Typing a message for the model, clicking a
task's row and pressing `enter` sent that message to the task — the box never changed,
so nothing on the screen said anything had.

## Steer a task from its page — the `└` elbow, the `· delivered` clause, and how corrections read back

`enter` inside a task's page sends what you typed to the task itself. It arrives on the
page where you said it, under whatever the work had already done, drawn as an **elbow**:

```
└ the config lives under etc/ · delivered
```

The `└ ` is what says this is a correction to work already moving and not a new question.
A task's page is one question — the instruction the task was given, folded at the top of
the page — and everything you say on the page after that bends that one question, so the
page's turn count does not move when you steer.

The clause after your words is what the sending did, and it is news: it is there for a few
seconds and then fades off the row, leaving the elbow. While a correction is being saved,
queued or sent, it says `· sending`; that is not a receipt. The task send runs outside the
keypress, so you can leave the page while the engine is answering. `· delivered` is the ordinary one
and says the only thing you cannot see for yourself — the words crossed to the worker and
did not vanish on the way. `· it was waiting on its pieces — your line wakes it` appears
instead when the task had handed its work out and was parked on the reports; then nothing
was running to read your line at its next step, and your line is what starts it moving.
`· held on the task's record — it is being checked, and it cannot land as done without
this` is the third: there was nobody inside the task to read you, because its work was
already in front of the checker. The words are kept on the task, the check is not allowed
to land it as done over them, and the task takes another round with them instead.

**A correction can change what the task is judged by, and only yours can.** Say "CSV
instead of JSON" into a running task and the worker can fold it into the task's own
done-condition, citing the line you sent; from then on that is what the checker judges the
finished work against, and the first done-condition is history. Your words are kept
verbatim beside the new condition, so you and the checker both see what you actually said.
What the model says into a task with `tasks id N say` never does this — that is one part
of the work talking to another, and it cannot change what somebody else's work is graded
on.

**Reopen the page and your corrections are still corrections.** Leave and come back, or
open the task tomorrow, and each line you steered comes back as a `└ ` elbow in the place
you said it, with no clause on it — the elbow's position is the record of where the words
went. Older builds drew them as fresh `›` questions, so a page read back showed your
corrections as extra instructions and counted turns nobody had opened.

Steering is refused rather than quietly re-pointed when the work is over — a finished
task, a background job — and then a question comes up offering to send the words to the
main conversation or to ask for the work to be started again. Nothing is sent anywhere
until you answer it. A task that is still running is never refused: if nobody is inside it
to read you — it is being checked, or its worker has just closed — the line is held on the
task's record instead, which the clause above says out loud.

**A run task's page carries this box too**, and it says when the note is read rather than merely
saved. Under the box the run page writes `the worker reads a note at its next step`: the note
goes to the task's store and waits until the worker asks for its next step, which is when it
reads what you wrote. A task that has ended, `done` or `incomplete`, takes no next step, so
its page leaves that sentence out.

## Task page says finished but the work is still running

It does not any more. If you are on an older build, this is what you were seeing: a job's
page drew `task finished — esc to return` (that was the old wording) under a header whose
clock was still counting up
— `video · working · 32s` over a foot claiming the work was over.

The cause was that a background job is not a task in codeaf's own graph, so every attempt
to open a **room** for one was refused — which is the **ordinary** answer for a job, on a
perfectly healthy conversation — and that refusal was read as "the work has landed". A
job's page is a card now, not a room, and it draws the job's own clock or ending, so the
two cannot disagree.

What each foot means now:

- `x stop it · c copy path · m puts it in your message · ↑↓ scroll` — a
  background job that is still running. The lines above it are its log and they are still
  arriving. `x` stops it. `esc back` is in the head.
- `c copy path · m puts it in your message · ↑↓ scroll` — a background job that
  has ended. Nothing more is coming; scroll up to read what it wrote.
- `this task has finished — say it to main` — a **task** that is over, whatever kind it
  was. Nothing more is coming; scroll up to read what it did. Where the task has a parent
  the line offers that door too: `…, or open its parent, Ship the port`.
- `this task has finished — say it in docs pass` — the same landing on a page that is
  **reading another conversation's task**. The door is the owning conversation, by its
  name when this window has one and `…say it in the conversation that owns it` when it
  does not — never `main`, which on that page is the wrong conversation for the words.

**A job's page has no composer, so none of those job feet name a door for typed words.**
`m` is how a job's ending reaches the conversation. A task's foot still names a door,
because the box is still there and the worker is not. `esc` is already on the legend under
the transcript (`room · esc/←← main`) and on the pinned header above a task's room, so a
task's foot spends its cells on the half nothing else on the screen is saying.
- **no foot at all** — an ordinary task still working. There is nothing to say at the bottom
  of the page, because the next thing to arrive is what happens next.

The header is the other half of the answer, and it has always been true: if it says the work
is running, it is running. The next section lists every word it draws.

## How long did a call take in a task — the 1.4s at the end of a call's row on a task's page, and the dim lines between its calls: `guardian allowed`, `stuck? nudged`, `Retry 1/3` on my task page

A task's page draws the same facts about work in flight that the conversation
draws, and until recently it drew none of them: the page was built from a
second copy of the conversation's wiring, and the copy had fallen behind.

**How long a call took** now sits at the right-hand end of that call's row —
`1.4s` under ten seconds, `12s` under a minute, `2m04s` above one. It appears
the moment the task reports *that* call finished, which is usually before its
result comes back: calls in a batch run together and the result waits for the
slowest of them, so the figure is the call's own and not the batch's. A call
too quick to be worth a number gets none. The same figure comes back when you
open the page after the work has landed — the journal keeps each call's own
duration, so a finished room still says `3.0s` and `7.0s` on the right rows
rather than drawing the calls with no clock at all.

**A retry inside a task** shows, in the same words the conversation uses. When
the model's reply is cut and the step asks again, the half-answer that was cut is
taken off the page — it belongs to a reply that will never exist — and a dim row
says what happened, that it is being asked again, and which try this is:
`the model went quiet mid-reply · asking again · 2 of 3`. Where the step gives up
on that model and finishes on another, the row names where the rest is coming
from instead of counting: `the reply lost its thread · moving to gpt-5-mini`.
Before this, a task's page kept the dead half-answer above the live one with
nothing to explain it.

A failed connection or retryable request also replaces its partial answer before
trying again, with `the model would not take the request · asking again · 2 of 4`
or whichever reason applies. The failed attempt does not stay above the
replacement. Stopping during the retry wait still keeps the partial reply you
saw; a retry that never starts discards nothing.

**And a step that runs out of tries says so** rather than leaving the page
looking as though nothing has arrived: `gave up after 4 tries · <what the
provider said>`. A step that failed once and was never asked again draws
`error: <what went wrong>` — "gave up" is a claim about a struggle.

**The dim `· ` lines between calls** are notes about what happened between steps. Three of them reach a task now:

- `Retry 1/3: removed max_tokens` — the request had to be reshaped to be
  accepted. The work carries on; the line is there so a reply that took three
  tries does not look like one that took one.
- `stuck? nudged · read` — the task caught itself asking for the same thing
  over and over and was told so.
- `guardian allowed · bash` — a call that would have asked a person to approve
  it was approved by the guardian instead.

None of them is a failure and none of them needs an answer. A task's page is
still quiet when the work is going well: only failure speaks.

## My task correction is sending or has no answer

A correction is saved with its original recipient before it can be sent. Corrections to
one task go in the order you typed them. `sending` includes time waiting for that save
or an earlier correction; it does not establish that the engine received anything.

If the engine refuses the correction, its words are recovered for that task without
replacing a newer draft. If the connection fails and delivery is uncertain, the row says
`no answer — it is not known whether this arrived`. The correction remains a pending
message with its original identity, rather than becoming a fresh message in the box.

If the conversation the correction belongs to was **closed** while it was still crossing,
there is no page and no record left to keep it, so the answer is said out loud instead:
`task 7 was not corrected, and its conversation is not open here · these words are only
in this line: ` followed by the whole sentence, which also joins your `↑` history. It is
not held for a retry and is never moved to the conversation now in front.

## Retry a task correction without sending it twice

On an unresolved correction, `r` asks again and `esc` leaves it on the page. An engine
that remembers message identities returns its existing receipt if it already admitted
that correction. The current engine writes the identity and direction to its task record
before acknowledging it. A write failure refuses delivery; a failure to undo an already
saved admission remains uncertain. This protects direction admission, not arbitrary
external effects of the worker's tools.

If the engine cannot recognize repeated identities, the offer says
`send it again (it may arrive twice)` and there is no automatic repeat. Two separate
presses of Enter intentionally create two messages, even when their text is identical.

## Close the window while a correction is pending

Pending corrections survive with their recipient, message identity and original time in
the same structured record as drafts. Reopening that conversation and task restores the
unresolved row; retrying uses its retained identity. A definitely refused message is
recovered for its own task. Closing a conversation for good clears its kept corrections;
an unsent message waiting for its local save is not released after that close.

The local save must succeed before a new correction crosses to the engine. A missing
record path or failure to create a message identity refuses sending and keeps the words.
A failed plain-text export is shown, but does not undo a successful structured save.

## Switch conversations while a task correction is sending

A send keeps the original engine and owning conversation, whatever page is on screen.
Switching to a conversation held alongside it can leave the original send running.
Its receipt and recovered draft belong to that original conversation.

If the conversation behind that engine connection was replaced, the engine refuses a
mismatched recipient with `not sent — that conversation is not open`. It does not send
the words to another conversation's task with the same number. An engine unable to check
the intended conversation is refused before the correction crosses the wire.

A task viewed through another conversation's reading connection has no steering door.
Its editor is separate from the main draft and from any local task with the same number.
Task corrections carry text; attachments stay in the recipient's tray and the page says
that those files were not sent with the correction.

## How do I open a task under this one? — enter on its row, escape to come back

The `under it` section is the whole subtree in store order, not only the direct
children, and every row in it is drawn exactly as the rail draws a task: the
state mark (the spinner while it works), the name, its `#id` at the end, and the
tree's own connectors (`├─`, `└─`). A part that is running says the command it is
on, such as `bash go test ./...`, and under that how long it has run and what it
has cost, each left out when the store has not got it.

Select any row there and press `enter` to open that task's page. Press `esc` to
return to the page you came from; the trail at the top of the page reads
`<conversation> ▸ <parent title> ▸ <this task>` so you can see where it returns.
This opens a page rather than changing the rail's fold.
