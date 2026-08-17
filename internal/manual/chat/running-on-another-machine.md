# Running on another machine

## What --host is

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

This is a session you sit in front of, the same as a local one. Closing the terminal ends
it; nothing keeps running on the far machine afterwards.

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

## What runs on the far machine, and what stays local

The **far** machine owns the conversation and everything it touches:

- the workspace and every file in it
- every tool the session runs
- the API key and the model catalog's credentials
- the tool gate (what needs your approval) and the spend rail
- connected accounts
- the harness registry
- the session file the conversation is written to

The **near** machine — the one you are sitting at — owns the surface:

- your ↑-history and your unsent draft, both keyed by the remote place (`dest:workspace`),
  so what you typed while working on `devbox:code/app` belongs to that place
- the model picker's cached list
- the terminal itself
- **the paths for `/image` and for `@` completion**, which are anchored here

Because the launch forks before this machine reads any of its own settings, a missing
local `OPENROUTER_API_KEY` is not an error on this path. The key that matters is the one
on the far machine.

## How you can tell you are on another machine

**The machine is shown as part of the place, and nowhere else.** There is no badge, no
icon, no "connected" word and no extra segment in the status line.

The workspace is written with the machine in front of it and a colon between, the way you
would type it into `scp`:

- `devbox:/s/c/app` in the legend under the input box
- `devbox:app` in the status line's place segment
- `devbox:/srv/code/app` in full in `/status`

`/status` also names the session file with its machine in front of it, because that is a
path you may want to copy.

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

10. **`/image` and `@` are local, deliberately.** The picture is on the machine you are
    sitting at and the bytes travel with the message, so a relative path and the
    completion walk are both anchored here rather than on the remote workspace. The image
    ceilings are applied on this side, with the same words a local session uses:
    `session: <path> is over the 10MB image limit` and
    `session: these images total more than the 20MB a single message may carry — send them across a few messages`

11. **`/export` writes here, and the note says so.** The transcript is assembled from what
    this surface is holding; there is no door for putting a file on the far machine's
    disk. The success note gains the suffix, exactly:
    ` · on this machine`

And one more, which that list does not yet mention: **building a new sub-harness does not
work over `--host`.** Running one that already exists does.

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

**Building a new sub-harness does not work over a remote connection.** This is a known
limitation. If you ask for one in conversation (`make a harness for …`), the design starts
on the far machine, but the finished design card has no road back to your screen: your
turn ends with **no reply and no card**, and the design's window later expires unanswered.
Nothing is saved. Build harnesses in a session running on that machine directly.

**Running a harness that already exists is unaffected.** The offer card rides the turn's
own stream, so a turn whose words match a registered harness still asks you, and answering
`yes` still runs it over there.

## Attaching a picture, and @ paths, over --host

`/image` and `@` completion are **local on purpose**. The picture is on the machine you
are sitting at, and its bytes travel with the message.

So a relative path you type after `/image`, and the `@` completion walk, are both anchored
**here** — to the directory you launched from — and not to the remote workspace. If you
want a file that lives on the far machine, that path will not find it.

The image size ceilings are applied on this side, with the same words a local session
uses:

```
session: <path> is over the 10MB image limit
session: these images total more than the 20MB a single message may carry — send them across a few messages
```

## Exporting over --host

`/export` writes the file **on this machine**, the one you are sitting at. The transcript
is assembled from what the surface in front of you is holding, and there is no door for
putting a file on the far machine's disk.

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

Every call gives up after 10 seconds, so a dead pipe never leaves your terminal frozen.
When the connection dies, every call in flight fails and every open stream is closed with
an error, so no turn is left spinning.

You see one sentence:

```
the connection to devbox is gone — run the same command to pick the conversation back up
```

If the far end said why, its reason is added in parentheses. A connection you closed from
this side reads `this connection is closed` instead.

**Run the same command again.** That is not advice dressed up: the conversation is on the
far machine's disk, and the same command opens it again. Nothing that reached the session
file is lost — the far machine is the only writer of it, and on every road out it
interrupts the turn in flight (keeping its partial reply, exactly as `ctrl+c` does) and
flushes the file. A closed lid, a killed ssh and a closed surface are all the same event.

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
