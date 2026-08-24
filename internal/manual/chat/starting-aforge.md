# What aforge is, and how you start it

## What aforge is

Aforge is a working colleague in a terminal. You talk to it in ordinary language,
and it works in the directory you started it in — reading, writing, running
commands, searching, and handing longer jobs off to run on their own while you
keep talking.

It is a conversation you sit in front of. There is one conversation **on screen**
at a time, it is written down as you go, and you can leave it and come back to it.
One terminal can hold several at once — up to eight, each on its own project, one
in front and the rest running behind it; `tab` and home move between them. It is
not a background service and it does not keep working after you close the window:
the work happens while you are here, except for jobs and tasks it has already
started, which have their own rules.

## What it is called

The command you type is `aforge`. On screen the surface calls itself `openaf` —
that name appears at the top of `/help`, on the card that asks to connect an
account, and as the speaker heading in an exported conversation. They are the
same program.

## Starting it

| What you type | What you get |
| --- | --- |
| `aforge` | the home screen, over this directory's most recent conversation |
| `aforge chat` | the same thing |
| `aforge chat --session <path>` | that conversation, straight in, no home screen |
| `aforge resume` | the chat, opened on the picker of earlier conversations |
| `aforge chat --host devbox` | the chat here, the work on another machine |

**The very first launch on a machine with nothing configured** opens on a short setup
instead — your openrouter key, the crew, a daily ceiling; `enter` accepts, `esc` skips —
and then on the empty conversation. It is shown once, ever; the getting-started page has
the whole of it. Without a key in your shell or your profile, `aforge` still opens; a
`--once` or piped run with no key still stops at the door with
`aforge chat needs a model to talk with.`

**The first frame is home** — every project on this machine and every conversation
in them — with the conversation this directory would have opened loaded and ready
underneath it. `esc`, or `enter` on the row the cursor starts on, drops into that
conversation; everything after that is the chat exactly as it always was. Home
stays out of the way when you name a conversation, on a `--once` or `--host` run,
and on a machine whose only conversation is the one already open — though it is
still there to go to: `space` twice on an empty box, or `/home`, opens it on that
machine too. The home page covers the whole of it.

Run it in the directory you want it to work in. That directory is what it reads
and writes, and it is shown in the status line so you can always tell.

## The flags you can start it with

| Flag | What it does |
| --- | --- |
| `--model <slug>` | start on a particular model instead of the configured default |
| `--reasoning <level>` | how hard the model is asked to think: `off`, `low`, `medium` or `high` |
| `--session <path>` | open a particular conversation file instead of the most recent |
| `--host <host[:path]>` | run the conversation on another machine over ssh |
| `--once "<text>"` | send one message, print the reply, and exit — no screen, nobody watching |
| `--no-compact` | never shorten the conversation automatically |
| `--yolo` | run every tool without asking, subject to the limits that nothing lifts |

`--yolo` does not make aforge unstoppable: a small set of destructive commands
and anything that acts in your name still ask, whatever the setting says. See the
permissions page.

## Which folder does aforge work in, and where do my files go

It depends on where you started it, and there are exactly two cases.

**You started it inside a project.** Aforge borrows that directory. Its tools read
and write your repository, exactly where you are standing, and the status line
shows that directory's name — `app`, `my-site` — until the conversation names
itself, so you can always tell which project this conversation is about. The full
path is on `/status` and on the status sheet, under `place`, with the git branch
beside it.

**You started it anywhere else** — your home directory, a temp folder, a
launcher with no directory in mind. Then the conversation gets a workspace of
its own, inside its session folder, and anything it writes lands there instead
of scattered across wherever you happened to be. In that case the place reads
simply:

```
aforge
```

That word means "no project — this conversation has its own space". The real
path exists and is not a secret; it is just aforge's own bookkeeping, and
showing it where you look to answer "which project am I in" told you nothing.
Type `/status` and the `place` line gives you the full path to copy.

An owned workspace of that kind is quietly made into a git repository, so work
done there has undo history like work done anywhere else.

**And a conversation opened later can be in a different folder from the one you
started in.** `enter` on home opens a row of any project, and typing a path on
home starts a conversation there — each on **its own** workspace, with that
project's approval rules, crew, spend ceiling and saved shapes of work, resolved
the same way this one's were. A conversation never changes the folder it was born
in; there is simply more than one conversation. The status line's place word is
always the folder of the conversation on screen. See home's page under *Open
another project from home*.

## Where your conversations are kept

Every conversation is written to disk as it happens, under your home directory in
`.aforge/v3/projects/`, in a folder named after the project you were working in
and a folder of its own inside that. The conversation's own folder holds the
transcript and everything else the conversation kept. That transcript is what
`aforge` reopens when you come back, and what the picker lists. Closing the
window, or losing the connection, does not lose what was said.

Opened inside a project, aforge works in the project and leaves nothing of its
own in it. Opened where there is no project at all — your home directory, a
temporary directory, a launcher — it works in a folder of its own instead, so
scratch files and downloads land somewhere they can be thrown away with the
conversation.

## Asking aforge about itself

Aforge ships with this manual compiled into it, and it reads it with a tool
called `manual` rather than answering about itself from memory. So "what can you
do?", "what does ctrl+b do?", "can you read a PDF?" and "why did you just ask me
that?" are all fair questions to type straight into the conversation. If the
manual has nothing on something, that usually means aforge does not do it, and it
will tell you so instead of inventing an answer.
