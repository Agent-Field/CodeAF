# Running on another machine

## What --host is — running this on my dev box, over ssh

`--host` runs the chat surface on the machine you are sitting at, and runs the
conversation on another machine, over ssh.

Everything about the conversation happens over there: the workspace, the files, the tools
it runs, the model key, the session file. Your terminal keeps the screen, the keyboard and
your draft. The two halves talk to each other with a small line-based protocol over the
ssh pipes; you never see it.

```
aforge chat --host devbox
aforge resume --host devbox
```

The flag's own help text reads:

```
run the session on another machine over ssh: host, user@host, or host:path/to/project
```

This is a session you sit in front of, the same as a local one — but **it is no longer tied to
this terminal.** The conversation lives on the far machine and your window attaches to it. Close
the lid mid-answer, lose your wifi, kill the terminal: the turn keeps running over there, and
running the same command puts you back in it, including whatever finished while you were away.
See *Staying on that machine* and *When the connection drops*.

The one case where closing really does end it is a far machine running `aforge engine` by hand
on a pipe, with no session host behind it. Then the pipe **is** the conversation's life. The
surface knows which of the two it has and never promises the stronger one.

## How to type the target

The target is parsed the way `scp` parses one: it splits on the **first** colon.

| What you type | What it means |
| --- | --- |
| `--host devbox` | that machine, working in the far machine's **home directory** |
| `--host me@devbox` | the same, with a user name |
| `--host devbox:code/app` | a path **relative to the far machine's home** |
| `--host devbox:/srv/code/app` | an absolute path on the far machine |
| `--host devbox:` | the same as naming no path |

**The path is the far machine's path.** It is passed on as you typed it and resolved over
there. It is never resolved against the directory you are standing in, and tab completion
in your own shell will not help you with it. A relative path is relative to the far
machine's home directory, not to wherever ssh happens to drop you.

Two refusals:

```
--host needs a machine: --host devbox, --host me@devbox, or --host devbox:code/app
```

and, for a target that starts with the colon, a refusal saying the target
`has no machine in front of the colon`.

## What you need to set up first

Nothing, beyond two things you already have.

1. **aforge is installed on the far machine**, on the PATH that a non-login ssh command
   sees.
2. **`ssh <machine>` already works** from where you are sitting.

There is no daemon to run, no config file of aforge's own and no key handling, and no port
to open — the two halves talk over the ssh pipes and nothing else. aforge runs your own
`ssh`, the one on your PATH, so your `~/.ssh/config`, your
host aliases, your keys, your jump hosts and your ssh agent all apply unchanged. If
`ssh devbox` works, `--host devbox` works.

**Anything ssh needs to ask you is asked before the chat takes the screen.** A key
passphrase prompt, an unknown-host-key question, or a version mismatch between the two
builds are plain text on a plain terminal, not a dialog inside a full-screen surface.
ssh's own stderr is printed as it arrives.

## Trying it against your own machine first — `--host localhost`

You do not need a second machine to rehearse this. If `ssh localhost` works on the machine
you are sitting at, `--host localhost` is a real connection over a real ssh pipe, and it
exercises every part of this page except one:

```
aforge chat --host localhost:code/app
```

The engine starts over there — which is here — through ssh, the session lives in its own
project folder, and dropping the link behaves exactly as it does against a machine across
the room. It is the fastest way to see what a dropped connection looks like before it
happens to you for real.

**What it cannot prove** is that the two halves do not quietly share a disk. Over
`localhost` they do: the same home directory, the same `~/.aforge`, the same files. So a
path that only works because both ends are one filesystem will pass here and fail against
a real machine. For that, use a machine you actually ssh to.

**To see a drop and a recovery on purpose,** ask for something slow, and from another
terminal kill the ssh child this session started:

```
pkill -f "ssh -T localhost aforge engine"
```

The status line grows its `connection` segment, the surface redials itself, and the answer
continues rather than restarting. Send something while it is down and the message is not
lost quietly — it comes back as `submit failed: reconnecting to localhost — try that again
in a moment`, and pressing enter again once it is back sends it.

## When the connection cannot be made

The failed dial says what it found, rather than guessing:

- aforge missing over there:
  `aforge is not installed on <dest> — install it there, or put it on the PATH that a non-login ssh command sees`
- ssh could not get a session at all: `ssh could not open a session on <dest>`. ssh has
  already printed its own reason on the line above.
- no ssh on this machine: `this machine has no ssh on its path, and --host is ssh`, or
  `could not start ssh: <err>`.

If the two machines run different builds, the connection is refused at the door and this
side says:

```
<dest> runs a different version of aforge than this machine does — update the older one so both ends speak the same protocol
```

When the **far** machine is the older one it refuses first, and what you see is its own
sentence rather than that one:

```
error: engine: this build speaks protocol 3 and the surface speaks 4 — the two halves have to be the same build
```

Either way the fix is the same and it is one command: update aforge on the machine that is
behind. Nothing is negotiated down — two builds that might disagree about a frame must not
find that out three turns into a conversation.

## What runs on the far machine, and what stays local

The **far** machine owns the conversation and everything it touches:

- the workspace and every file in it
- every tool the session runs
- the API key and the model catalog's credentials
- the tool gate (what needs your approval) and the spend rail
- connected accounts
- the harness registry
- the session file the conversation is written to
- **everything you set up that keeps working** — reminders, watches, rules, overnight work:
  the store they live in, the clock that checks them, and the machine they run on

The **near** machine — the one you are sitting at — owns the surface:

- your ↑-history and your unsent draft, both keyed by the remote place (`dest:workspace`),
  so what you typed while working on `devbox:code/app` belongs to that place
- the model picker's cached list
- the terminal itself
- **the paths for `/image` and for `@` completion**, which are anchored here
- **the browser, the viewer and the file door** — the small `127.0.0.1` listener this
  window opens so that a path in a reply, `/files` and `/files <path>` can show you a file
  that is on the other machine (*Opening files from that machine*)

Because the launch forks before this machine reads any of its own settings, a missing
local `OPENROUTER_API_KEY` is not an error on this path. The key that matters is the one
on the far machine.

## How you can tell you are on another machine

**While the connection is healthy, the machine is shown as part of the place and nowhere
else.** There is no badge, no icon and no "connected" word — a working connection says
nothing about itself.

The one exception is a connection that is **not** healthy: while a dropped link is being
redialled, a `connection` segment appears in the status line reading
`reconnecting to devbox — trying for up to 5 minutes`, and it goes away again when the link
is back. Nothing is drawn at any other time.

The workspace is written with the machine in front of it and a colon between, the way you
would type it into `scp`:

- `devbox:/s/c/app` on the status sheet's `place` row
- `devbox:app` in the status line's place segment
- `devbox:/srv/code/app` in full in `/status`

The legend under the input box says the machine too, but as a segment of its own rather
than as a path prefix, because that line carries the conversation's name and not the
folder: `devbox · porting the parser`.

`/status` also names the session file with its machine in front of it, because that is a
path you may want to copy.

**And if another window is on the same conversation, you can tell from the input box.** A
window that does not hold the keyboard draws one dim line where its box was —
`typing from spark now` on the left, `enter takes it back` on the right — and nothing else
about the screen changes. A window that does hold it says nothing at all. See *Staying on
that machine* for the whole of how two terminals on one chat behave.

The `~` collapse still runs against **this** machine's home directory, so it rarely fires
on a remote path — expect to see the full path.

## What does not work over --host

This is the first half of the whole list, so you know before you rely on it, with the
exact sentence each one says.

1. **The git branch is not shown.** The probe would read this machine's repository at the
   remote path, and a coincidence is worse than a blank. It draws nothing and says
   nothing at all.

2. **`/connect` is off, with a reason.** The panel writes to this machine's account store
   and the session reads the other one's. Accounts already connected on the far machine
   keep working. It says exactly:
   `connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working.`

3. **A browser sign-in is off, and the card offers only "not now".** Approving is what
   opens a browser and waits on a loopback port, and the browser is here while the port is
   there. The ask row says exactly:
   `connecting an account is not available over --host yet`

4. **A key sign-in works, unchanged.** You paste a secret into a box on this screen and it
   travels on the wire like every other answer; nothing about it needs a browser or a
   port. This is the one entry on the list that is a capability, not a limit.

5. **`/settings` opens anyway, and says one sentence as it opens.** Half these rows are
   this surface's own — the mouse, the timestamps, the draft — and genuinely apply; the
   other half govern the conversation, which reads them from the far machine's profile. It
   says exactly:
   `these rows are this machine's — the ones that govern the conversation are read from the profile on the other one`

## More of what does not work over --host

The second half of the list, with the exact sentence each one says.

6. **The consent card's "always" writes nothing.** No save seams are handed over a
   connection, so the row says `allowed` rather than the local
   `always · saved — /permissions to change`. That is the truth: the answer holds for this
   session, on the far machine, and is written down nowhere.

7. **The YOLO badge is drawn from the far machine's posture.** It is carried once when the
   connection opens, read from that machine's own profile rather than off this laptop: a
   badge read off the wrong machine would be a safety claim about a machine nobody
   consulted.

8. **`/harness` is unavailable.** The registry is the far machine's and this build has no
   door onto it over the wire, so the command says exactly:
   `harnesses are unavailable here`
   rather than listing this machine's and offering to run them there.

9. **The task rail is absent by construction.** The remote session does not carry it, so
   there is no rail and no room. It says nothing; there is nothing to draw.

10. **`/image`, `/attach` and `@` are local, deliberately** — and this one is a capability as
    much as a limit. The picture or file is on the machine you are sitting at and its bytes
    travel with the message, so a relative path and the completion walk are anchored here
    rather than on the remote workspace. What you attach really does arrive over there; see
    *Attaching files*. The image
    ceilings are applied on this side, with the same words a local session uses:
    `session: <path> is over the 10MB image limit` and
    `session: these images total more than the 20MB a single message may carry — send them across a few messages`

11. **`/export` writes here, and the note says so.** The transcript is assembled from what
    this surface is holding, so the file lands on the machine you are sitting at. The success
    note gains the suffix, exactly:
    ` · on this machine`
    That is a fact about `/export` alone and no longer a fact about the connection — files do
    cross, both ways (*Attaching files*).

12. **Building a new sub-harness is switched off**, and not for the reason it used to be. A
    question raised while nobody is attached now *waits* for the next window — but a design's
    card never reaches this connection at all, because it is announced on a subscription this
    protocol has no door for rather than on a turn's stream. So there is nothing to hold. The
    designer is not offered over `--host` and aforge says it cannot build one from here.
    Running a harness that already exists is unaffected.

13. **File paths are clickable again, and this is now a capability rather than a limit.**
    They were not for a wave: the only thing your terminal could open was a path of the
    same name on this machine. Now the far machine is asked whether the file is really
    there, and a path it confirms is a link that opens the file itself — through a small
    door this window owns on `127.0.0.1`, never through `file://`. A path it has not
    confirmed stays plain text, exactly as at home, and a folder is not linked. `/files`
    opens that machine's folder as a page in your browser, `/files <path>` brings one file
    back and opens it in your own viewer, and a file dragged onto that page lands in the
    conversation's `attachments/`. The whole of it — what turns into a link and what does
    not, where the copies live, the 16MB ceiling, who else can reach those addresses — is
    on *Opening files from that machine*.

## Reminders and watches over --host — they work, and they belong to that machine

**Standing items are the one ambient capability a connection does not take away.** A
sub-harness design and an adaptive run are both switched off at the engine because their
card would arrive in an empty room; this card does not — it crosses the wire as an
ordinary event and your answer crosses back as its own frame.

So `remind me at 6`, `tell me when CI on main goes red` and `every Monday post the standup`
all work over `--host`. What to know is **whose machine they are on**:

- The item is created, checked and fired on the **far** machine, in the far machine's
  workspace, under the far machine's own profile rules — not this laptop's.
- It keeps working after this window closes and after the connection drops.
- Background checks belong to the **far** machine: the first thing you set up over the
  connection installs its timer, and the `background checks` settings row turns that one.
  Neither ever touches the machine you are sitting at.
- Pausing or stopping one writes to the far machine's store, and a write that store
  refuses is shown as its own refusal rather than redrawn as done.

Three readings are absent over a connection, and each says nothing rather than guessing:

- **Home opens with no rows on it.** Where the list would be it says exactly:
  `home shows this machine's projects, and this session is on another`
  — so a remote session has no `◦` item band and no `p`/`s` keys on one. The tab bar, the
  composer and the other six places are all there as usual. The status line's
  `◦ keeping an eye on 2` still counts, and it counts the **far** machine's items for the
  workspace this window is on, because over `--host` that path is the far machine's own.
- **`/status` prints no `keeping watch` line.** The OS timer is the far machine's and its
  state is read from a file on that disk. A line drawn from this laptop's timer would be a
  status about a machine nobody consulted.
- **No row ever shows the firing mark `◐`.** Nothing on any disk says an item is firing at
  this instant — a run is in flight inside whichever process holds the tick lock — so the
  surface does not claim it. That is true locally too.

## Connecting an account over --host

`/connect` is off over a connection. The panel writes to this machine's account store
and the session reads the other machine's. It says exactly:

```
connecting an account is not available over --host yet — the sign-in opens a browser here and the account belongs to the machine over there. accounts already connected on that machine keep working.
```

**Accounts already connected on the far machine keep working.** The tools that use them
run over there. Only new sign-ins are off.

When the conversation asks you to sign in with a browser, the card offers only "not now",
and the ask row says exactly:

```
connecting an account is not available over --host yet
```

Approving would open a browser here and wait on a loopback port there.

**A key sign-in works unchanged.** Pasting a secret into a box on this screen needs no
browser and no port, so that road stays open over `--host`.

## Settings over --host

`/settings` opens over a connection and says one sentence as it opens:

```
these rows are this machine's — the ones that govern the conversation are read from the profile on the other one
```

Half the rows are this surface's own — the mouse, the timestamps, the draft — and those
genuinely apply to what you are looking at. The other half govern the conversation, and
the conversation reads them from the profile on the far machine. Change those over there.

**Asking aforge to change a setting goes the other way.** `settings` and `change_setting`
run inside the session, which is on the far machine, so they read and write **that**
machine's profile — which is the profile the conversation actually obeys. So over a
connection the two doors land in two different files: the panel edits this laptop, and
asking edits the machine the work is on.

## Approvals over --host

**Approvals are read from the far machine's own settings, not from the laptop you are
sitting at.** The session is built over there, and its tool gate is over there.

Two consequences you can see:

- **The consent card's "always" writes nothing.** No save seam is handed over a
  connection, so the row reads `allowed` instead of the local
  `always · saved — /permissions to change`. That is the truth: the answer holds for this
  session, on the far machine, and is written down nowhere. To make an approval stick, set
  it with `/permissions` on the far machine.
- **The YOLO badge is drawn from the far machine's posture.** It is carried once when the
  connection opens, read from the far machine's own profile — not off this laptop. A badge
  read off the wrong machine would be a safety claim about a machine nobody consulted.

## Harnesses over --host

**`/harness` is unavailable.** The registry belongs to the far machine and this build has
no door onto it over the wire, so the command says exactly:

```
harnesses are unavailable here
```

It does not list this machine's harnesses and offer to run them over there.

**Building a new sub-harness is switched off over a remote connection.** The tools that
design one are not on the far session's belt at all, so asking for one gets you a plain
answer that it cannot be done from here — nothing starts and nothing is spent. The reason is
that the card asking whether to keep the finished page is announced on a subscription this
protocol has no door for, so it never crosses at all. A question that *does* cross and finds
nobody attached is held for the next window; this one is not one of those. Build harnesses in a
session running on that machine directly.

**Adaptive runs are switched off over a remote connection**, and for the same reason: a
run's fuel gate arrives on that same standing lane. A run left switched on would spend the
money, stop at its cap, and wait four hours for an answer nobody could give it. The
`run_adaptive` tool is simply not there.

**Running a harness that already exists is unaffected.** The offer card rides the turn's
own stream, so a turn whose words match a registered harness still asks you, and answering
`yes` still runs it over there.

## Attaching a picture, and @ paths, over --host

`/image` and `@` completion are **local on purpose**. The picture is on the machine you
are sitting at, and its bytes travel with the message.

So a relative path you type after `/image`, and the `@` completion walk, are both anchored
**here** — to the directory you launched from — and not to the remote workspace. If you
want a file that lives on the far machine, that path will not find it. To reach one of
those, click it where the reply names it, or use `/files` — that is the other direction,
and *Opening files from that machine* is the page for it.

The image size ceilings are applied on this side, with the same words a local session
uses:

```
session: <path> is over the 10MB image limit
session: these images total more than the 20MB a single message may carry — send them across a few messages
```

## Exporting over --host

`/export` writes the file **on this machine**, the one you are sitting at. The transcript
is assembled from what the surface in front of you is holding, so that is where it lands.

**There is a door for moving files between the two machines** — it is simply not this one.
`/attach` sends a file to the far machine, and a file the conversation made over there can be
fetched back to this one. See *Attaching files*.

The success note says so. It gains the suffix ` · on this machine`, so the whole note
reads, for example:

```
exported · ~/chat.md · on this machine
```

## --yolo and --no-compact cannot travel

These two flags are refused rather than quietly ignored. They are properties of the
session, and the session is built on the far machine, so a flag typed here has nowhere to
land.

Naming either with `--host` is an error that names the machine the setting lives on:

```
--no-compact and --yolo cannot travel over --host: the session is built on <dest>, so set it there — `ssh <dest> aforge chat --no-compact --yolo` — or open the settings panel on that machine
```

Set them on the far machine: run `aforge chat` there with the flags, or open the settings
panel on that machine.

## --model and --reasoning over --host

`--model` and `--reasoning` do work over a connection. They are applied immediately after
the connection opens rather than being carried in it. The session has existed for a
millisecond and nothing has been asked of it, so your first turn rides the model you
named.

`/model` inside the conversation works over the wire as well, and the far machine answers
calls in the order they arrived — a `/model` followed by a message is a message on the new
model.

## When the connection drops

**It redials by itself.** A dropped link is not the end of the session any more — the surface
keeps trying, and when it gets back in it picks the conversation up where you left it,
including the turn that was running while you were gone.

While that is happening the status line says, quietly:

```
reconnecting to devbox — trying for up to 5 minutes
```

Every call still gives up after 10 seconds, so a dead pipe never leaves your terminal frozen.

If it cannot get back at all, you see the sentence you always saw:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

If the far end said why, its reason is added in parentheses. A connection you closed from
this side reads `this connection is closed` instead.

**The three roads out are three different things now**, which is what makes the above safe.
Closing the window on purpose leaves the conversation running. A link that simply dies means
the same — the far machine assumes you are coming back. Ending the conversation is its own
gesture. Only against a far machine with no session host do all three collapse back into one,
and there the pipe really is the conversation's life.

Nothing that reached the session file is lost either way: the far machine is the only writer of
it, and it flushes on every road out.

A fuller account of what survives, and what a returning window does and does not get back, is
in *When the connection drops* and *Staying on that machine*.

## One headless message over a connection

```
aforge chat --host devbox --once "text"
```

This is deliberately the same shape as a local `--once` run: the reply goes to stdout, and
tool lines, compaction lines and failures go to stderr, so a script cannot tell which
machine answered. The session is closed when the turn ends.

`aforge resume` refuses `--once` and points you at the right form:

```
aforge resume opens the session picker; for one headless message use: aforge chat --host <dest> --once "text"
```
