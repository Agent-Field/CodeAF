# Attaching files

## Pasting a lot of text

A paste of **3 or more lines** becomes one compact `[paste 1 · 42 lines]` chip in the
message box. One- and two-line pastes stay ordinary text. A paste made only of real file
paths is still attached as files, and a paste into a box whose text starts with `/` stays
plain text so command arguments remain usable.

The chip holds the complete text locally. When you send, codeaf puts that complete text
into the message where the chip stood, headed `paste 1:` and enclosed in a text fence.
The model receives every character; your transcript keeps the short chip instead of
dumping the pasted document onto the screen. Sent paste chips cannot currently be opened
from the transcript.

## Paste chip — why did my paste turn into a tag

`[paste 1 · 42 lines]` means a large paste is folded, not lost. The number lets you refer
to it in the rest of the message, and the line count says how much it holds. The threshold
is **3 lines**. Arrowing onto the chip selects it as one unit; the selected chip is drawn
inverted. `left` and `right` cross the whole chip rather than walking through its label.
Typing while it is selected clears the selection and inserts after it.

Move the pointer over a paste chip to tint it. Click it to edit it. With the chip selected,
`enter` opens it instead of sending; `enter` sends normally whenever no paste chip is
selected.

**The model always gets the whole paste, wherever the message goes.** The tag is what the
screen shows; what is sent is `paste 1:` followed by the full text in a fenced block — on an
ordinary reply, on a message that waited and was steered into the running answer, on a
line typed into a task's room, on a card's redirect or change, and on a follow-up queued
with `ctrl+q`. The transcript, the waiting strip and the room keep the tag. If the model
ever says it cannot see a paste, that is a bug and not a setting.

## Edit what I pasted

Click a paste chip, or use `left` or `right` to select it and press `enter`. A centered
editor opens with the whole paste and a live `paste 1 · 42 lines` title. The ordinary
composer movement keys work there, including `alt+b` / `alt+f`, `ctrl+a` / `ctrl+e`,
the arrow keys, `home`, and `end`. `enter`, `alt+enter`, and `ctrl+j` insert a newline.

The footer says `esc keeps and closes · ctrl+x discards this paste`. `esc` keeps edits and
returns to the message. `ctrl+x` removes the chip and its held text.

## Remove a paste

Select a paste chip with `left` or `right`, then press `backspace` (from its right edge) or
`delete` (from its left edge). The whole chip and the text it holds are removed together.
Inside the paste editor, `ctrl+x` does the same thing. There is no recovery after the chip
is discarded, though ordinary unsent draft recovery still applies to the rest of the
message.

## Send a file with your message — /attach, how do I share a file with codeaf, how do I send it a photo or an image

`/attach <path>` puts an ordinary file — a log, a CSV, a PDF, a stack trace you saved —
on the tray above the message box, and it goes with the next thing you send.

`/upload` is the same command under the word most people bring with them.

```
/attach server.log
/upload ~/Downloads/sales-q3.csv
/attach                     the browser, so you can find the file and look at it first
```

**On the home screen a bare `/attach` does not open that sheet** — it says
`type the path after /attach · or drop the file here`, and `/folder` is the browser there.
Everything below is about `/attach` inside a conversation; *Attaching a file from home* has
the home half.

**With nothing after it inside a conversation, `/attach` opens the add context sheet,
including over `--host`.** It is the same framed window `/folder` opens locally, in the
same place and already browsing — the conversation stays visible behind it, dimmed, and does
not answer a click while it is up. It stands first in a folder the conversation already
holds, otherwise in the folder this window is working in, then in your home directory;
codeaf's own state folder is never where an owned conversation opens it. The sheet shows the
subdirectories and then the files with their sizes, and previews the thing under the cursor
— source with syntax colour, a picture drawn in the terminal's own cells, a PDF's text.
Moving the cursor shows you a file; it does not attach it. `alt+m` chooses one, or several,
and the last row says `attach this file · <path>` — or `attach 2 files` once you have chosen
more than one — and `enter` does exactly what it says. Over `--host`, the sheet browses the
machine you are sitting at and selected files travel with the message. "Choosing a folder"
is the full account of that sheet, its keys and its preview.

Path rules are `/image`'s: `~` is your home directory, a bare name is under the directory
this conversation is about, and an absolute path is left alone. Tab completes the path as
you type it. Over `--host`, that completion walks the machine you are sitting at, because
those are the bytes `/attach` is about to send.

The file lands on the tray as its own chip — `▤ server.log`, or `+ server.log` on a
terminal that cannot draw the box — and the message box stays the sentence you were
writing. The tray already holds pictures; files sit on the same row, and a file's chip
carries no number because there is nothing in the sentence for a number to point at.

**A folder after `/attach` is not a refusal.** It used to answer
`<name> is a folder · attach a file`; now the folder goes to the same place the `/folder`
picker's `enter` sends one, and says `folder · <path>`. See the "Choosing a folder" page
for what that does and what it does not. That is the local behavior. Over `--host`, the
folder is on this machine and the conversation is on the other one, so nothing is registered
and it says
`choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.`

## I dropped a file and nothing happened

Dropping a file onto the terminal is the same as `/attach <path>`. codeaf recognises a
drop made only of real local files, takes the path out of the message box, and shows each
file on the tray. Over `--host`, pressing `enter` sends those local bytes to the other
machine before the turn starts. A picture gets its numbered picture chip and `[image #n]`;
an ordinary file gets its unnumbered file chip.

A folder is not attached, and a drop containing prose or a path that is not a real local
file remains ordinary text. The 10MB picture limit always applies; over a connection the
16MB ordinary-file limit and 32MB total ordinary-file limit apply too. The visible chip is
the confirmation that the drop landed; if there is no chip, no file will be sent.

A full tray does not stop the command: `/attach` adds a second file rather than sending
the first.

When you press `enter`, the transcript shows your line with the file's name after it:

```
› why is this failing [server.log]
```

## Drops arrive however the terminal sends them

**Your terminal decides the shape, and codeaf handles both.** Most terminals write a drop
into the message box as one *paste* of the file's path. Others *type* the same path,
one character at a time, with nothing marking it as a paste.

Either way you get the chip. When the characters stop arriving, codeaf reads the run that
just landed, and if one complete terminal reading of it names real files on the machine
you are sitting at, the path comes out of the box and the files go on the tray.

**Nothing happens while you are still typing.** A half-arrived path names nothing, so
codeaf says nothing about it — no complaint, no half-attached file. Only a run that names
files that are really there is ever converted.

**Every shape a terminal writes is understood:** backslashed spaces
(`Screenshot\ 2026-08-27\ at\ 1.21.14\ PM.png`), raw un-escaped spaces, `'single'` and
`"double"` quotes, `file://` URLs and bare paths with `%20` escapes, several files joined
by spaces or newlines, and the narrow no-break space macOS puts before `AM` or `PM` in a
screenshot name. Inside WSL, `c:/…`, `C:\…`, `/C:/…`, `\\wsl.localhost\…`, and
`\\wsl$\…` work too. `~` means your home directory. A drop after words you had already
typed keeps the words and puts `[image #1]` where the path was.

**Typing a slash command is untouched.** `/help`, `/model`, `/export` and the rest are
told from a dropped path by the separator inside it: `/var/folders/…` has one, `/help`
does not. So a command is never mistaken for a file, and typing one costs exactly what it
always did.

**The one thing that rule costs you:** a file sitting at the very root of the disk —
`/notes.md`, with nothing between it and `/` — looks exactly like a command while it is
being typed, so it is not turned into a chip on the way. Pressing `enter` still attaches
it: the send door checks the line against the disk before it refuses anything.

## I dropped a file and it said unknown command

**That is fixed, and it is worth knowing what it was.** On a terminal that types a drop
rather than pasting it, the path landed in the box as plain text — and because it began
with `/`, `enter` handed it to the slash router, which answered about a screenshot:

```
there is no command called /var/folders/…/Screenshot · / lists them
```

Now the path becomes a chip before you ever press `enter`. And if a drop somehow reaches
`enter` still spelled out — an odd terminal, a shape nobody has met yet — the send door
checks the line against the disk before it refuses. A line that names real files is
attached and says so:

```
that was a file, not a command · attached
```

A folder dropped or pasted gets the drop road's own sentence instead — `<name> is a folder ·
attach a file` — while the original pasted text is still inserted at the cursor in
home and conversation message boxes. The notice does not discard the path. Only an
empty home box offers to start in the pasted folder: the next Enter accepts, any other
key or another paste dismisses the offer, and the text remains an ordinary draft. A mixed
paste containing files and a folder stays entirely as text, with nothing attached.
An unknown command that names nothing on the disk still refuses exactly as it always did. That sentence belongs to the drop and the paste alone: `/attach
~/code/thing`, typed, refers the folder and answers `folder · ~/code/thing`. A drop is a
gesture nobody typed, and reading a decision about your project out of a mouse would be
inferring far too much.

## Dropping a file on the home screen

**The home screen takes a drop exactly as a conversation does.** Drag a screenshot onto
the terminal while home is up — the box at the foot, or the `ask here` pane beside the
list — and the picture goes on a tray drawn directly over that box, with `[image #1]`
where the path would have been. An ordinary file goes on the same tray and writes no word
at all.

**The tray lights `start` even with nothing typed.** A file on the tray *is* a message, so
`enter` starts a new conversation and sends it — and the pictures go with you into that
conversation, because the tray belongs to you rather than to the screen. The same is true
of `ctrl+enter`: an errand carries what was dropped into it.

**The list underneath keeps working.** Home's box is a search over every project on the
machine at the same time as it is the first line of a conversation, and `[image #1]` is
cargo rather than words — so the list filters on what you typed around the token and never
on the token itself.

This used to be broken in exactly the way it looks: the drop landed in home's box as the
raw escaped path, and `enter` handed it to the slash router, which answered `unknown
command`. Both roads now end on the tray.

## Drag and drop shows the path as text

If you can still see the escaped path sitting in the message box, one of three things is
true.

- **It has not settled yet.** The path becomes a chip a fraction of a second after the
  last character arrives. Pressing `enter` does not lose it: the send door spends the drop
  first, so the chip is on the message either way.
- **The file is not on this machine, and codeaf says so.** codeaf attaches what it can
  `stat` on the computer you are sitting at. Over `--host` that is still your laptop,
  which is the point — the bytes travel. But a terminal on your Mac talking over `ssh` to
  codeaf on a Linux box delivers a *Mac* path, and there is nothing at that path here. A
  drop that names nothing on this machine now answers

  ```
  Screenshot 2026-08-31 at 5.21.40 PM.png is not on this machine
  ```

  and leaves the text exactly where it landed, so nothing is lost. Several missing files
  say `3 files are not on this machine`. The sentence is owed only to a paste that is
  *plainly* a drop — every word an absolute path — so a sentence that merely mentions a
  file is inserted in silence as it always was.
- **A Windows path inside WSL is local.** `c:/Users/…`, `C:\Users\…`, `/C:/…`,
  `\\wsl.localhost\<distro>\…`, and `\\wsl$\<distro>\…` are translated before the file is
  checked. If the file exists under WSL's mount, the text settles into a chip just like a
  `/home/…` path. Outside WSL the same text stays text and the note names the file, such
  as `Screenshot (1).png is not on this machine`.
- **The box already starts with a `/command`.** That is deliberate. `/attach ` followed by
  a dropped file is the command being used exactly as documented, so the path stays as its
  argument and `enter` runs the command. See below.

## Dragging a file from Windows into WSL

Yes. Windows Terminal and the VS Code terminal may send a drag from Explorer as exactly

```
'c:/Users/you/Pictures/Screenshots/Screenshot (1).png'
```

codeaf recognises that as the local WSL file at
`/mnt/c/Users/you/Pictures/Screenshots/Screenshot (1).png` and turns it into the same chip
as a Linux path. A bare `C:\Users\…` path, either quote style, a `file:///C:/…` URL, and
the `\\wsl.localhost\<distro>\…` or `\\wsl$\<distro>\…` spelling work too. A UNC path is
local only when its distro is this WSL distro.

`/mnt` is WSL's default automount root. If `[automount] root` in `/etc/wsl.conf` names
another root, codeaf uses that instead: `root = /drives` makes `c:/Users/…` read from
`/drives/c/Users/…`. This applies to a drag, a pasted path, `/attach`, `/image`, `/export`,
and a local copy destination chosen in `/files` because all use the same path reading.

## I copied a screenshot and pasted it — nothing happened

codeaf does not read picture bytes from the Windows or macOS clipboard. If the clipboard
contains a picture rather than a file path, the terminal sends no path into the message
box, so its paste shortcut with that picture pastes nothing into the box. Most terminals
consume their paste shortcut before codeaf sees a key. If a terminal does pass `ctrl+v`
through, codeaf leaves it unbound; it does not read clipboard pixels. Effort uses
`alt+e` (`opt+e` on macOS).

Two things do work: drag the screenshot file onto the terminal, or paste the screenshot's
path. Either one gives codeaf a real local file to put on the tray. If your screenshot is
only in the clipboard, save it as a file first.

## A drop into a box that already holds a command

**It stays text, and that is on purpose.** Type `/attach ` or `/image ` first and then
drop the file: the path is the command's argument, and turning it into `[image #1]` would
break the one line on this surface whose whole job is to take a path. `enter` then runs
the command and the file lands on the tray by that road instead.

The rule is exactly: a message box whose text already begins with `/` keeps a dropped path
as plain text. An empty box, or one holding ordinary words, converts it.

## What codeaf does with an attached file

**It is told the path, not the contents.** An attached file is a file, and codeaf already
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

So codeaf may open an attached file, read part of it, `grep` it, or never touch it at all
— it is a file on disk that you have pointed at, and what it does with it is up to what
you asked for.

## Where did my file go

**On a local session, nowhere.** The file stays exactly where it is. codeaf is running on
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
file attached twice is one file, and your own name on the end is what tells codeaf what it
is holding before it opens anything.

`attachments/` sits inside the conversation's own folder, beside the transcript — so
deleting a conversation takes its attachments with it, and nothing you attached is ever
swept away while the transcript still refers to it.

## Upload a file to the machine codeaf is running on

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
no such file: <what you typed>
<name> is already attached
<name> is 24MB and over the 16MB file limit
the files on this message are over the 32MB limit
could not read <name>
this connection cannot carry a file · the words were not sent
choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.
```

- `no such file` is a path that is not there.
- `<name> is already attached` means it is on the tray already; the same path twice is one
  chip.
- The two size refusals are the ceilings, and they only ever appear over `--host`.
- `could not read` is a file that vanished or became unreadable between attaching and
  sending.
- `this connection cannot carry a file` means the connection was opened without a door for
  files; your words were **not** sent and your tray is still yours.
- `choosing a folder is not available over --host yet` is `/attach <a directory>` across a
  connection. Nothing is registered on the far conversation.

**A bare `/attach` is not on this list any more.** It used to answer
`/attach takes a path · try /attach server.log`; it now opens the add context sheet on the
folder this conversation is standing in, so you can find the file rather than being told to
know its path. It opens over `--host` too, browsing the machine you are sitting at, because
files travel. See "Choosing a folder".

**A folder is refused only where it cannot reach the conversation.** `/attach ~/code/thing`
used to answer
`<name> is a folder · attach a file`; locally it now goes to the folder door and says
`folder · ~/code/thing`. Over `--host`, it registers nothing and says
`choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.`
On home it pins the next conversation's folder instead and says
`next conversation opens in ~/code/thing`, because there is no conversation there to attach
one to. Dropping a folder on the window still refuses with the old
`<name> is a folder · attach a file` sentence — see "Choosing a folder".

## Attaching a file from home — /attach on the home screen, before there is a conversation

**Home has a tray of its own and `/attach <path>` fills it.** No conversation is opened for
it: the chip appears above home's box, home says
`attached · server.log · rides with the next conversation`, and the file is attached to the
first message of whatever conversation you start next. `/image <path>` is the same for a
picture, and a drop or a paste onto home does it with no command at all.

**A bare `/attach` there asks for the path** — `type the path after /attach · or drop the file
here` — rather than opening the browser. `/folder` is the browser on local home, and it is
aimed at which folder the next conversation opens in (see "Choosing a folder"). Over
`--host`, `/folder` says why this machine's folder cannot be that far conversation's folder.

**The tray survives the walk.** Attach a file on home, go into a conversation, come back: it
is still there. Home's tray row cannot be clicked; a chip comes off on a conversation's own
tray, where the `✕` is.

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
five codeaf accepts.

The reverse is not true: `/image` refuses anything that is not one of those five, with

```
<name> is not a picture · png, jpeg, webp and gif are
```

so `/attach` is the general word and `/image` is the specific one.

On the tray the two are told apart by their own glyph — `▣ #1 shot.png` for a picture,
`▤ server.log` for a file — and by the number, which only a picture carries. In the
transcript a picture keeps its `[#1 shot.png]` marker and a collapsed control underneath
with **preview** and **open original** actions;
an ordinary file remains the `[server.log]` marker alone.

## Drag a file in, or paste a path

**Dragging a picture onto the terminal attaches it** — your terminal hands the file's path
over, and codeaf recognises the picture extensions and turns it into a numbered chip with
`[image #1]` in your sentence.

**Dragging an ordinary file in attaches it too**, as an unnumbered file chip. It used to be
left in the box as text; it is not any more, because over `--host` a path in a sentence
names a file the far machine has never seen, and a chip is what makes the bytes travel.

To keep a dropped path as *text* — to talk about a path rather than send the file — type
`/attach ` or `/image ` first and drop onto that line, or write the path yourself after
some words. A box that already begins with `/` keeps the path as the command's argument.

Over `--host` the difference matters more than it looks. A path left as plain text is a
path on **your** machine, and the session on the far machine cannot open it — nothing
travelled. The chip is what makes the bytes travel. If you handed a path to that session
and were told the file is not there, that is what happened.

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

## Mention a file, a team or another chat with @

Type `@` in the message box. The list under it has three words on the first row,
**team**, **chat** and **file**, then teams, conversations, tasks, and files. Typing
filters every section at once. `@file:` keeps only files, `@team:` only teams,
`@chat:` only conversations. The three words are buttons: a press types that prefix,
the word under the pointer takes a background, and the hint names the key, `click`.

Choosing a file still puts `@` and the path in the sentence, and nothing is read
until the model asks. Choosing a team puts `●harbor` in the team's colour. Choosing
a conversation puts `@handle`, or a short slug of the title when it has no handle,
and the hint on the row is the full title. A conversation that is in no team is
still on the list.

After you send, that team mark and that `@handle` stay clickable. A press on the
team opens the teams page with it selected. A press on the conversation opens it. Over
`--host`, against an engine with no teams doors, a press on the team opens the
conversations view on it instead. The
model is handed a short digest of each one, not the transcript, and the other
conversation is not messaged and not woken. The words in your transcript are the
words you typed.
