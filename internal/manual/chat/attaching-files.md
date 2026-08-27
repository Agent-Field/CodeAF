# Attaching files

## Send a file with your message — /attach

`/attach <path>` puts an ordinary file — a log, a CSV, a PDF, a stack trace you saved —
on the tray above the message box, and it goes with the next thing you send.

`/upload` is the same command under the word most people bring with them.

```
/attach server.log
/upload ~/Downloads/sales-q3.csv
```

Path rules are `/image`'s: `~` is your home directory, a bare name is under the directory
this conversation is about, and an absolute path is left alone. Tab completes the path as
you type it. Over `--host`, that completion walks the machine you are sitting at, because
those are the bytes `/attach` is about to send.

The file lands on the tray as its own chip — `▤ server.log`, or `+ server.log` on a
terminal that cannot draw the box — and the message box stays the sentence you were
writing. The tray already holds pictures; files sit on the same row, and a file's chip
carries no number because there is nothing in the sentence for a number to point at.

## I dropped a file and nothing happened

Dropping a file onto the terminal is the same as `/attach <path>`. The terminal sends its
local path as a paste; aforge recognizes a paste made only of real files, removes the path
from the message box, and shows each file on the tray. Over `--host`, pressing `enter`
sends those local bytes to the other machine before the turn starts. A picture gets its
numbered picture chip and `[image #n]`; an ordinary file gets its unnumbered file chip.

A folder is not attached, and a paste containing prose or a path that is not a real local
file remains ordinary text. The same 10MB picture limit, 16MB ordinary-file limit, and
32MB total ordinary-file limit apply. The visible chip is the confirmation that the drop
landed; if there is no chip, no file will be sent.

A full tray does not stop the command: `/attach` adds a second file rather than sending
the first.

When you press `enter`, the transcript shows your line with the file's name after it:

```
› why is this failing [server.log]
```

## What aforge does with an attached file

**It is told the path, not the contents.** An attached file is a file, and aforge already
has a `read` tool — so a 4MB CSV stays out of the conversation until something actually
wants a row of it. The message it receives carries your sentence and then, plainly:

```
attached file: /path/to/server.log
```

More than one file on the same message becomes:

```
attached files:
/path/to/server.log
/path/to/sales-q3.csv
```

That is the whole difference from a picture. A picture has to travel *as content* because
nothing on the belt can turn a PNG into something a model can look at; a file does not,
because `read` opens it.

So aforge may open an attached file, read part of it, `grep` it, or never touch it at all
— it is a file on disk that you have pointed at, and what it does with it is up to what
you asked for.

## Where did my file go

**On a local session, nowhere.** The file stays exactly where it is. aforge is running on
the same machine, the path you typed already means something to it, and copying the file
would only give you two of them.

**Over `--host`, it is copied to the far machine.** The engine there has never seen your
disk, so the bytes travel with the message and the far end writes them into that
conversation's own `attachments/` folder before the turn starts. The path in the message
is a path on **that** machine — the one that owns the transcript — so it is true for the
session reading it, and it is nothing you can open here.

The name it lands under keeps yours, with the moment it arrived and a short digest of its
contents in front:

```
20260824-141233-a1b2c3d4-server.log
```

The stamp makes the folder read in the order things arrived, the digest means the same
file attached twice is one file, and your own name on the end is what tells aforge what it
is holding before it opens anything.

`attachments/` sits inside the conversation's own folder, beside the transcript — so
deleting a conversation takes its attachments with it, and nothing you attached is ever
swept away while the transcript still refers to it.

## Upload a file to the machine aforge is running on

Yes — this is what `/attach` does over `--host`, and it is the point of it.

The path you type is anchored to **this** machine, the one you are sitting at, exactly the
way `/image` and the `@` completion are. You are naming a file on your own laptop. Its
bytes travel with the message, and the far machine writes them down under that
conversation's `attachments/` folder before the turn opens.

So `/attach ~/Downloads/crash.log` on a laptop connected to a dev box puts `crash.log` on
the dev box, and the session there reads it from a real path with its ordinary tools.

Two things are true and worth knowing:

- **The size ceilings only exist over a connection.** Locally nothing moves, so nothing is
  refused for weight. Over `--host` the limits below apply, because the bytes are crossing
  a wire.
- **The name is a name, never a path.** The far end chooses the directory and refuses
  anything that looks like it wants to choose for itself, so a file cannot be written
  outside that folder.

If the connection was opened without a door for files, the message is not sent and you
keep your tray:

```
this connection cannot carry a file · the words were not sent
```

**The browse page drops into the same folder.** `/files` over `--host` opens that
machine's workspace as a page in your browser, and a file dragged onto it lands in the
very same `attachments/` folder, under the same time-stamped name. The difference is that
a drop **says nothing** — no message, no turn, nothing in the conversation — where
`/attach` is the same landing place with your own sentence on it. See *Opening files from
that machine*.

## The file is too big

Over a connection, two ceilings apply. Locally there are none — the file is not going
anywhere.

**One file may be 16MB.** Bigger, and you get, exactly:

```
server.log is 24MB and over the 16MB file limit
```

You get that at the door when you type `/attach`, so a file too big to send never sits on
the tray pretending it will. If a file grows past the ceiling *after* you attached it, the
same sentence arrives when you press `enter`, and the tray comes back with everything
still on it.

**All the files on one message may be 32MB together.** Past that:

```
the files on this message are over the 32MB limit
```

Attach fewer, or send them across a few messages.

The numbers come from the wire, not from taste: a whole message travels as one line, that
line may weigh 64MB, and bytes inside it cost a third more than the file does. 16MB per
file leaves room for two large ones, a screenshot and the sentence they came with.

Pictures are counted separately and have their own ceiling of **10MB each** — see the
`/image` refusals.

## Every refusal /attach can give you

Exactly as they are written:

```
/attach takes a path · try /attach server.log
no such file: <what you typed>
<name> is a folder · attach a file
<name> is already attached
<name> is 24MB and over the 16MB file limit
the files on this message are over the 32MB limit
could not read <name>
this connection cannot carry a file · the words were not sent
```

The first is a bare `/attach` with nothing after it. The second is a path that is not
there. The third is a directory — attach the file inside it, not the folder. The fourth
means it is on the tray already; the same path twice is one chip. The fifth and sixth are
the ceilings, and they only ever appear over `--host`. The seventh is a file that vanished
or became unreadable between attaching and sending. The last means this connection was
opened without a door for files; your words were **not** sent and your tray is still
yours.

A few refusals come from the far machine instead and arrive with `engine:` in front of
them — the file arrived with no usable name, or with a name that was really a path:

```
engine: an attached file arrived with no name
engine: "../../etc/passwd" is a path and not a name — an attachment names itself and the engine chooses where it goes
```

## Attaching a picture is a different thing

`/image <path>` is the door for a picture, and a picture travels **as content** so it can
actually be looked at.

You do not have to remember which word is which. **A picture handed to `/attach` is still
treated as a picture** — it goes on the tray as `▣ #1 shot.png`, gets its `[image #1]`
token in your sentence, and is looked at rather than read. png, jpeg, webp and gif are the
five aforge accepts.

The reverse is not true: `/image` refuses anything that is not one of those five, with

```
<name> is not a picture · png, jpeg, webp and gif are
```

so `/attach` is the general word and `/image` is the specific one.

On the tray the two are told apart by their own glyph — `▣ #1 shot.png` for a picture,
`▤ server.log` for a file — and by the number, which only a picture carries. In the
transcript a picture is marked `[#1 shot.png]` and a file `[server.log]`.

## Drag a file in, or paste a path

**Dragging a picture onto the terminal attaches it** — your terminal hands over the file's
path as text, and aforge recognises the picture extensions and turns it into a chip.

**Dragging anything else in leaves you the path as text**, because a path in a sentence is
already a useful thing to say to aforge on a local session — it can just `read` it. To put
that file on the tray as an attachment, type `/attach ` first and drop the file onto the
line: a line that already starts with `/` keeps the dropped path as the command's
argument.

Over `--host` the difference matters more than it looks. A path dropped as plain text is a
path on **your** machine, and the session on the far machine cannot open it — nothing
travelled. `/attach` is what makes the bytes travel. If you dropped a path in and got told
the file is not there, that is usually what happened.

## Take a file off the tray before you send

Three ways, all the same as for a picture:

- **`backspace` over an empty message box** removes the last thing on the tray.
- **Click a chip** and it comes off.
- **Send the message** — the tray empties into it.

Removing a picture also takes its `[image #n]` token out of your sentence and counts the
ones behind it down. Removing a file changes nothing in the sentence, because a file never
put anything there.

If sending fails for any reason, **the tray comes back** — everything that was on it,
ahead of anything you attached while the message was in flight, and never twice. A refusal
that also lost your attachments would make you go and find the files again.

## Get a file back — downloading from the far machine

**Files come back now, and there are two ways.**

`/files <path>` brings **one file** off the far machine and opens it the way your desktop
would — the path is a path on that machine, relative to the workspace the conversation is
working in. And `/files` with nothing after it opens that machine's folder as a page in
your browser, where clicking a file opens it in a tab and your browser's own save is the
download. A path in a reply is a link over a connection as well: cmd+click opens the file
itself. The whole of it is on *Opening files from that machine* — where the copies land,
the 16MB ceiling, and why editing your copy changes nothing over there.

What is still true is that **`/export` writes here**. The transcript is assembled from
what the surface in front of you is holding, so over `--host` its success note says so
with the suffix ` · on this machine`:

```
exported · ~/chat.md · on this machine
```

The `/files` **list** — the index of things conversations have made — is this machine's
own record, so on a connection that has no file door it still says as it opens:

```
these are the files made on this machine — what that session made is written down on the other one
```
