# Opening files from that machine

On a conversation started with `--host`, the files it is about are on the **other**
machine. This page is what you can do with them from where you are sitting: click a path
in a reply and have it open, `/files` to see that machine's folder as a page, `/files
<path>` to bring one file back and open it in your own viewer, and a drag onto that page
to send one the other way.

Everything except that one drag only **reads**. Nothing here edits a file over there, and
the copy that lands on this machine is a copy.

## Click a path over --host to open a file on the other machine

**A path in a reply is a real link again over a connection.** cmd+click it on a Mac,
ctrl+click on Linux — the same gesture that opens a path in a local conversation — and
the file opens in your browser: an image appears, a log or a source file is shown, a PDF
opens in the tab's own reader. What the browser cannot draw it offers to save.

The link is underlined, which is how you can tell there is something to click before you
hold the modifier down. A path with no underline is not a link, and the next section says
why.

It works on a path wherever aforge wrote it: in a sentence, in a list, in a table cell, on
a `read`, `write` or `edit` tool row, in the dim `·` notes. A path too long for the pane
wraps across rows and every row of it opens the same file.

A file the model **just wrote** is usually already on this machine before you click it,
so it opens at once — aforge quietly fetches a small file as it watches the `write` tool
make it. A larger one crosses when you click, and a fetch that takes more than about a
third of a second draws one line naming the file. Nothing spins.

The browser is where a click lands because that is what a terminal hands a web address to.
If you want the file in **your own** program — Preview, an editor, a spreadsheet — use
`/files <path>` instead.

## Why is that file path not clickable over --host

A path becomes a link only after **the other machine** has confirmed the file is really
there. Until then it is plain text, which is the same rule a local conversation follows —
only the machine answering it changed.

So a path is drawn plain when:

- **It has not been confirmed yet.** The first time a name appears aforge does not know
  it, so it asks that machine and the word becomes a link a moment later. A file a tool is
  still in the middle of writing is plain until it exists.
- **The file is not there at all** — a name the model invented, a path with a typo, a file
  deleted since. A link that opens nothing is worse than no link.
- **It is a folder.** Folders are not links over a connection; use `/files` and click your
  way to it.
- **It is outside what may cross** — see the last section of this page. Two places cross
  and nothing else does.
- **Something else printed it.** The output of a `bash` call, a `grep` result, the body of
  a `read` — those are another program's words and aforge draws them exactly as they
  arrived.
- **It is written inside `backticks` and was not confirmed.** Inline code is checked like
  any other word: when it really is a file the backtick shading is dropped and the
  underline becomes its one mark, and when it is not, it stays ordinary inline code.

`~` is never expanded over a connection: that tilde is **this** machine's home directory,
and the file is on the other one.

## Where cmd+click opens it — the little door on this machine

The link does not point at the file. It points at `http://127.0.0.1:<port>/f/<a long
random id>` — a small web address served by aforge **inside this window**, which fetches
the bytes over the same connection the conversation uses and hands them to your browser.

There is a reason it is not a plain `file://` link. `file:///srv/app/main.go` given to the
terminal in front of you means **this** machine's `/srv/app/main.go`, which is either
nothing at all or a stranger's file with the right name. That was the whole reason paths
were not clickable over `--host` for a while.

The door starts the first time you need one — a first link, a first `/files` — and a
conversation that never names a file never starts one. It closes when the window closes,
and every address it minted stops working at that moment.

If it cannot start at all, you get one line and no links:

```
the file door did not open on this machine
```

## See the picture, the log or the PDF it made over there

Ask for the picture as usual. What is different over a connection is only where it is:
the file is written on the far machine, and the row that names it is your way in.

**The picture is not painted into the terminal over a connection.** That drawing reads a
file on the machine you are sitting at, and this one is somewhere else. What you get is
the row naming the file, its size and its dimensions, with the path as a link. Click the
path and the picture opens in a browser tab; `/files <path>` hands it to your usual image
viewer instead.

The same is true of anything else a turn produces: a report, a chart, an export, a log.
The path in the reply is the door.

## Download one file from the other machine — /files <path>

`/files <path>` brings **one file** here and opens it the way your desktop would — `open`
on a Mac, `xdg-open` on Linux — so a `.csv` lands in your spreadsheet and a `.png` in your
picture viewer.

The path is a path **on the other machine**, relative to the workspace that conversation
is working in:

```
/files reports/q3.csv
/files /srv/app/build/site.pdf
```

A fetch under about a third of a second says nothing at all. A longer one draws one quiet
line naming the file and its weight if that is known. If the other machine refuses, you
get **its own sentence**, unchanged — the path is outside the two places that cross, or
the file is over the 16MB one file may cross.

On a conversation that is **not** on another machine, `/files <path>` only says so:

```
that form of /files is for a session on another machine — this one is local, so the paths in it are already yours to open
```

## Browse the other machine's files — /files

`/files` with nothing after it opens a **page** in your browser showing that machine's
workspace, and writes the address into the conversation as well, so you can paste it into
a different browser or reach it when nothing opened by itself.

What the page gives you:

- **Folders first, then files**, each with its size and when it was last changed. A time
  nobody knows is left blank rather than invented.
- **Click a folder** to go into it; the trail along the top walks you back out.
- **Click a file** to open it in a new tab — the same door a clicked path in a reply uses.
  Your browser's own "save" is the download.
- **A file too big to cross is not a link.** Its row says `too big to cross` beside it
  rather than letting you click something that would fail.
- **A very large folder is cut**, and the last row says how many entries you were given.
- **Drag a file onto the page** to send it the other way — the next section.

The page shows what that **conversation** may show, which is not the same as everything on
that machine. It is served by the same door as the links, on this machine only, and it
stops working when the window closes.

## Send a file to the other machine — drag it onto the browse page

Drag a file from your desktop anywhere onto the browse page and let go. The page says
`sending <name> …` with a percentage while it goes, and then where it landed:

```
landed at <that conversation's folder>/attachments/20260824-215842-d9e4ec72-notes.csv on devbox
```

The path is that machine's own answer, shown exactly as it came back — the same folder,
the same time-stamped name, as a file you send with `/attach`.

Two things are worth knowing before you rely on it.

**It always lands in that conversation's `attachments/` folder**, under a name stamped
with the time it arrived. You cannot choose the folder, and nothing already there is
overwritten. That is deliberate: writing anywhere else on somebody's machine is a write,
and writes belong to the conversation, where you can see them and approve them.

**A drop says nothing.** No message is sent, no turn starts, nothing appears in the
conversation, and you are not charged for anything. The chat learns about the file when
**you mention it**. If what you wanted was to hand the model a file *with* a sentence about
it, that is `/attach`, which lands in the very same folder and then says something.

A file over 16MB is refused before it is sent, in the same words the other machine would
have used.

## The file I dropped is in attachments/ — how do I get it into the project

Ask for it. Say something like:

```
put attachments/20260824-215842-d9e4ec72-notes.csv next to the other data files
```

The model has the same tools it always has, on that machine, and moving or copying a file
into the workspace is an ordinary write it does in front of you — which is the point.
aforge itself never writes anywhere on that machine except the one attachments folder, so
the step where a file becomes part of the work is a step you asked for and can see.

You can also just tell it the path and ask it to read the file. It is a real path on that
machine; nothing has to be moved for the model to open it.

## Where do the downloaded copies go on this machine

Two places, both under `~/.aforge/v3/remote/`:

- `cas/` holds the bytes, filed by their content, so the same file fetched twice crosses
  the wire once.
- `mirror/<machine>/<the path it has over there>` is the copy your viewer is actually
  handed, under the file's own name — which is why the window title says `q3.csv` and not
  a row of hex.

So `ls ~/.aforge/v3/remote/mirror` answers "whose files am I holding". Deleting that whole
folder is safe and is the one gesture that clears everything fetched from every machine;
anything you still want is fetched again next time you ask.

**Editing your copy changes NOTHING over there.** The copy is bytes to look at: there is
no write-back and no editor loop in this version, so a change you make here stays here and
is quietly replaced the next time that file is fetched. To change a file on that machine,
ask the conversation to change it — that is what the conversation is for.

## How big a file can cross, and the head start on small ones

**16MB is the most one file may cross a connection**, in either direction. Past that you
get that machine's own sentence rather than a wait:

```
engine: report.pdf is 42MB and the most one file may cross this connection is 16MB
```

On the browse page a row over the ceiling is not a link at all and says `too big to
cross`, so you find out before the click rather than after it.

**Files the model writes get a head start.** When the `write` tool makes a file of 2MB or less,
aforge fetches it quietly in the background before anybody clicks anything, so the click
is instant. Bigger files are left alone: spending your connection on a 12MB file on the
chance that somebody looks at it is not a bargain worth making. Nothing on the screen ever
says this happened, and a fetch that fails is simply one you will do when you click.

There is no partial fetch, no range and no tail: a file crosses whole or not at all.

## Who else can reach these links

Only you, and only from this machine, and only while this window is open.

- **The door listens on `127.0.0.1`** and on nothing else. It is not on your network, not
  on your wifi, and nothing outside this machine can reach it — the address only means
  anything on the computer you are sitting at.
- **Every address carries an unguessable id** minted for one file. Another program on this
  machine cannot ask the door for a path of its own choosing; it can only fetch what this
  conversation itself decided to link.
- **A wrong id and a wrong token get exactly the same answer** — `404`, with nothing to
  tell one from the other. A refusal that told you which of the two you got wrong would be
  a refusal helping somebody guess.
- **They die with the window.** Quit aforge and every link it minted stops resolving, the
  browse page included. A tab you left open reloads to nothing.

Your ssh connection is still the only way in to that machine. This door adds no route to
it: everything it serves came back over the connection you already had.

## What of that machine you can actually see

Two places, and nothing else:

- **the workspace** that conversation is working in, and
- **that conversation's own folder** — where its transcript and its `attachments/` live.

Symlinks are followed to where they really point, and only ordinary files cross — not a
device, not a socket, not a folder-as-a-file. Anything else you name gets that machine's
own refusal:

```
engine: /etc/passwd is outside this conversation's workspace and its own folder, and nothing outside those two crosses this connection
```

This is the same law that governs `/attach` and everything else that moves on a
connection, and it is that machine's law rather than a rule the window in front of you
applies — so it holds the same whether you asked by clicking a path, by `/files`, or by
typing a path into the browse page's address.

It also means the browse page is a view of **this conversation's places**, not a file
manager for that computer. To reach something outside those two, ask the conversation
about it: the model works over there and is not limited to what crosses the wire.
