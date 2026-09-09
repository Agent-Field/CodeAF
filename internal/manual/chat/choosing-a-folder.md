# Choosing a folder

## /folder — choose a folder, pick a directory, say which project you mean

`/folder` opens the **add context** sheet — a framed window over the middle of the screen,
with the folder you are standing in browsable in columns. It also answers to `/place` and
`/dir`, because people arrive with three different words for the same thing.

```
/folder            the sheet, opened on the folder this conversation is about
/folder aforge     …with `aforge` already searched, so the list of known folders is narrowed
/folder ~/code/    …with a path already typed, so the columns are open in ~/code
/attach            the same sheet, opened in exactly the same place
```

**Both commands open the same sheet, in the same place, already browsing.** Where it opens
is the first of these that exists: a folder this conversation has already been given, the
folder this window is working in, your home directory. It does not matter which of the two
words you typed, and a brand-new machine that has never chosen a folder gets the same sheet
as one that has chosen forty.

**It is a window, not a line at the bottom of the chat.** The conversation stays visible
behind it, dimmed, and is not live while the sheet is up: clicking it, scrolling it or
turning the wheel over it does nothing at all. The two ways out are `esc` and the **cancel**
words on the sheet's bottom edge, which you can click. A click on the dimmed area outside
the sheet does nothing — it will not throw away things you have chosen.

**It browses files as well as folders.** Once the columns are open, the middle column
lists the subdirectories and then the files inside them, with each file's size against the
right edge; a large preview of whatever the cursor is on sits beside them. So one sheet
answers both questions — which project do I mean, and which file do I want to send — and
`/attach` with no path after it opens the same sheet on the folder this conversation is
standing in.

The two things do different things when you choose them, and the last row of the sheet
always says which:

- **A folder** goes to the conversation. It is registered as a scoped reference, appears on
  the row above your message box, and every later model request carries it. Its contents
  are never pasted anywhere.
- **A file** goes onto the next message, on the same tray a pasted screenshot lands on. A
  picture travels as content to be looked at; anything else travels as a path aforge's
  `read` tool can open.

**Moving the cursor, opening a folder and reading a preview attach nothing at all.** The
only gestures that choose anything are `alt+m` and the action row.

It opens **instantly**. Nothing is scanned when you press enter on the command: the rows
come from what aforge has already seen, in layers, best first.

1. the folders **this conversation is already about** — ones you have named here, and ones
   a task's ground resolved to and the conversation wrote down
2. the directories **this conversation has been reading and writing in**, most recent first
3. every **project on this machine** — the same list `alt+w` cycles on the task composer
4. **repositories and ordinary folders under your home directory**, from an index built quietly in the
   background and refreshed about once a day

Inside a layer the order is how often you have picked that folder here, weighted by how
recently — so after a week of use the folder you want is usually the first row, and the
whole gesture is `/folder` then `enter`.

`esc` leaves everything exactly as it was: your half-written message comes back untouched,
and nothing has been chosen.

## Type a word to filter, open a row to browse

The box at the top of the sheet is a **search box**, and it is empty when the sheet opens —
so the first thing you type is a search rather than four more characters on the end of a
path. Where you are is said in two places you cannot type over: the **breadcrumb** above the
columns, and the location on the sheet's own top edge.

It does two jobs, and which job it is doing depends only on the shape of what is in it.

- **A word filters.** `agent`, `tui3`, `notes` — the list narrows with fuzzy
  matching of names and path segments, initials and small spelling slips. For example,
  `afroge` can find `aforge`; stronger literal matches lead typo matches.
- **A path browses.** Anything starting with `/`, `~/`, `./` or `../` — and a bare `~`,
  `.` or `..` — turns the list into **columns**.

A bare word is never treated as a path. Typing `agentfield` means "find it for me", not
"open ./agentfield".

**You never have to retype a path you can already see.** Press `→` on a row of the list, or
click it, and the columns open on that folder. The search that found it is spent — the box
goes empty again, so the next thing you type is a new search rather than an edit of the old
one. That is what a search result is for: find it by name, then walk into it.

**Clearing the box comes back.** `ctrl+u` empties it, and an empty box means "wherever the
columns are" — so searching and then clearing puts you back where you were standing rather
than on a list of everything. That is also how you reach the remembered folders, the
projects and the index from inside the sheet: clear the box, then type a word.

## The columns — the folder above, where you are, and what is inside the row you are on

Browsing draws three successive regions, the way a file browser does:

```
  ~ › code › aforge-v2 › internal
  cmd          ›   session/                    1  package session
  docs             tui3/                       2
  internal         agent.go        24.1 KB     3  import (
                   places.go        6.4 KB     4  	"context"
  add this folder · ~/code/aforge-v2/internal/session   repository · dev · clean
```

- **left** — what is in the folder above, with the one you are standing in a shade brighter,
  and its own scroll so the folders ABOVE the one you are in are on screen too. It is narrow
  on purpose: it is there to say where you are standing, not to be read down.
- **middle** — what is inside where you are: the subdirectories first, each with a trailing
  `/`, then the files with their sizes right-aligned. The cursor lives here, and the row it
  is on wears a band across **this column only** — never a stripe across the whole window.

  Every row leads with a **one-cell mark saying what kind of thing it is**: `▸` a folder,
  `◆` source or configuration, `▤` text and documents, `▣` a picture, `▶` audio or video,
  `▦` an archive, `·` anything else. On a terminal that cannot draw those the same seven
  are `>` `*` `=` `#` `+` `%` `.` — one character either way, so the sizes stay lined up
  down the column. Restrained colour reinforces the mark; type never depends on colour alone.
  Filenames use readable ink, folders keep their slash, and sizes stay quieter.
  Plain terminals keep the same shapes without colour.
- **right** — a **preview of the thing under the cursor**, with the same type marks on it.
  On a folder that is the folder's own contents, so the next level is on screen before you
  walk into it. On a file it is the file: source with syntax colour and dim line numbers, a
  picture drawn in the terminal's own cells, a PDF's text. The source is coloured by lexing
  the **whole file at once**, so a comment or a string that runs over several lines is one
  comment or one string all the way down.

The path above them is a **breadcrumb**, and every segment of it is clickable: press `code`
and you are back in `~/code` with the cursor on the folder you just left. On a narrow frame
the breadcrumb drops levels from the left — `… › tui3` — rather than shrinking the names.

| Key | What it does |
|---|---|
| `↑` `↓` | move down the middle column |
| `→` | walk into the folder under the cursor. On a file it does nothing — a file is not somewhere to go |
| `←` | walk out to the folder above, cursor left on the one you came from |
| `tab` | complete the highlighted name into the box, whole — the columns follow, so a folder lands where `→` does |
| `alt+m` | choose the thing under the cursor, or unchoose it. Chosen things gather on a tray above the action row |
| `alt+p` | hide the preview, and show it again |
| `alt+o` | give the preview the whole sheet, and give the sheet back. With the preview alone, `↑↓` scroll it and `←→` slide it sideways |
| `shift+↑` `shift+↓` | scroll the preview beside the list, without moving the cursor |
| `shift+←` `shift+→` | slide the preview sideways, to read past the end of a long line |
| `alt+h` | show the hidden files and folders, and hide them again |
| `enter` | do what the action row says |
| `esc` | leave, having changed nothing |

There is no focus to keep track of: **the pane a key acts on is the pane that is drawn.**
With a list on screen the plain arrows walk the list and `shift` plus an arrow moves the
preview; with the preview alone there is no list to walk, so the plain arrows are its own.

**The mouse does all of it too, in all three columns.** A click in the middle column moves
the cursor, and a second click on the row you are already on walks into it when it is a
folder. A click in the left-hand column walks back out. A click on the breadcrumb goes back
to that level.

**A click in the right-hand column acts on that row** when it is showing a folder's
contents. A directory opens in one click. A file becomes the current selection and its
source, prose or picture is previewed at once; neither gesture adds anything until you use
the action row. There is no double-click timing anywhere. When the right-hand column is showing a
**file** — source, prose, a picture — there is nothing there to select and a click does
nothing: it is a pane you read, and `shift` plus an arrow or the wheel is how you read past
the bottom of it.

The wheel follows the pointer: turned over the preview it scrolls the preview, turned over
the names it walks the names. Clicking is navigation and never a choice; the only things
that choose are `alt+m` and the action row, below.

One level is read at a time and nothing walks deep. Reading a folder and reading a file for
the preview both happen in the background, so a directory with forty thousand entries in it
or one on a network mount never holds a keystroke — and moving off a row cancels the read
you no longer want.

## Hidden folders — .config, .github, dotfiles

Hidden folders are out of the way rather than out of reach. Two ways in:

- **`alt+h`** shows them — every one of them, `.git`, `node_modules` and `vendor` included —
  and pressing it again puts them back out of sight.
- **Typing a name that starts with a dot** shows them by itself. `~/.con` finds `.config`
  without your having to think about a toggle first.

The right-hand preview of a folder keeps the same rule as the columns, so what it lists is
what you can walk into: `.git`, `node_modules` and `vendor` are absent from it until `alt+h`
is on, and never shown there as a row that would not open.

## The action row — add this folder

The last row of the sheet is the one thing on it that **chooses** anything, and its right
end carries what is known about the **thing under the cursor** — `repository · main · clean`
or `folder · 214 files` for a directory, and `7.7 KB · changed 3d` for a file. Nothing is
drawn where the disk had nothing to say, and `changed` is the file's modification time: it
is not "last opened", which no filesystem here answers honestly and aforge keeps no record
of.

The last row of the sheet is the one thing on it that **chooses** anything:

```
add this folder · ~/code/agentfield          repository · main · clean
```

It names the thing the cursor is on, so what `enter` would do is written down rather than
remembered — and clicking that row does exactly what `enter` does. Everything else on the
sheet moves you around; this row is the only thing that commits.

**It uses the verb that belongs to the kind of thing under the cursor**, because adding a
folder and attaching a file are two different acts on two different objects:

```
add this folder · ~/code/agentfield          repository · main · clean
remove this folder · ~/code/agentfield
attach this file · ~/code/agentfield/server.log
attach this picture · ~/Desktop/shot.png
```

**On a folder this conversation is already about, the same row says
`remove this folder · <path>` and that is what `enter` does** — so taking one off never
needs a mouse. Removing leaves the sheet open, because clearing two is a tidy-up and the
list you are tidying is the one in front of you; adding closes it, because the sheet has
then done its job.

**Several things at once.** `alt+m` chooses the row under the cursor and puts it on a small
tray one row above the action row, which says how many things are on it and names each of
them; a click on one of those cells takes it back off. A chosen row wears a `▪` beside the
cursor mark, so which row you are ON and which rows you have CHOSEN are two facts you can
see at the same time. The action row then spells out both halves of what `enter` will do:

```
  3 chosen ·  ▪ docs/  ▪ main.go  ▪ shot.png
  enter · add 1 folder · attach 2 files
```

Twenty-four things is as many as one message can carry, and the twenty-fifth press says
`24 is as many as one message can carry · send these first` rather than silently dropping
it. You can walk into other folders between choices — the tray is not about one level. A
folder you choose that the conversation already holds is left alone rather than taken off.

The work a confirm does runs in the background, because registering a folder is a round
trip when the conversation is on another machine and attaching a file is a look at the
disk. Nothing on screen claims otherwise, and each thing that fails says so by name:
`no such file: main.go`, `no such folder · ~/code/gone`.

The dim tail at the right end is what the machine already knows about that folder:

```
repository · main · clean
folder · 214 files
AGENTS.md
```

- A **repository** says so, with the branch its HEAD is on and whether the tree is clean or
  dirty. A repository whose head has no name says `repository` and stops.
- Anything else says `folder`, with how many things are directly inside it. An empty folder
  says `folder` and nothing more — never `0 files`.
- `AGENTS.md` appears only when that file is really there.

A fact aforge has not established draws nothing at all rather than a blank or a zero. The
facts are read in the background as your cursor lands on a row, so a big repository may
take a beat to say whether it is dirty — the keys never wait for it.

## An empty folder, and one you are not allowed to read

They are different things and the browser says which is which, where the rows would have
been. A folder that **was** read and has nothing inside it says `nothing below here` — and
you can still add it; a leaf is a perfectly good choice. A read that **failed** says why:

```
this folder cannot be read · permission denied
this folder is no longer here
this is a file, not a folder
this folder cannot be read
```

A permission is never reported as emptiness.

**The preview pane says the same kind of thing about a file it could not show**, in its own
words, exactly as they are written:

```
this file cannot be read · permission denied
this file is no longer here
this file cannot be read
this file is empty
this is not text · nothing to show here
only the first part of this file was read
more of this file is not shown
more of this folder is not shown
this terminal cannot draw pictures
this picture could not be opened
this document is pages of pictures · there is no text in it
this document could not be read
this document is too large to read here
```

- A file that is **not text** — an executable, an archive, a video, a compiled object — says
  so rather than showing you its bytes. Its size and kind are named on the pane's foot
  line, which is all there honestly is to say about it.
- Only the first **256 KB** and the first **400 lines** of a file are read, and the pane says
  `only the first part of this file was read` when that happened. A file read that far has
  no line count on its foot, because nobody counted the rest.
- A `.json` file that was minified is re-indented before it is shown, so a document that is
  one long line is a shape you can read. A file its author already indented is left exactly
  as they wrote it.
- **A picture is drawn in the terminal's own cells** — one cell is two stacked pixels, in
  colour, with the aspect ratio preserved. It is cell-resolution colour and not Kitty or
  iTerm graphics; it is coarser than those and it is the only kind that survives a repaint.
  On a terminal that cannot paint colour at all the pane says
  `this terminal cannot draw pictures` and names the picture's shape, format and size
  instead.
- A **PDF** shows its text. A scanned one, which has no text in it at all, says
  `this document is pages of pictures · there is no text in it` rather than showing you
  nothing without explanation.
- **Nothing is ever run in order to show it.** A preview opens a file for reading and looks
  at a directory. There is no shell-out, no converter and no network call, and no escape
  sequence inside somebody's file can reach your terminal — every line is stripped of
  control bytes twice on the way to the screen.

## What choosing a folder actually does — this conversation is now about it

`enter` — and the action row, which is the same act — says one line into the conversation:

```
folder · ~/code/agentfield
```

and three things are then true.

**I am told about it, by its exact path, on the very next thing you say.** The folder is
written into my own instructions under `Attached folders`: the absolute path, whether it is
a repository, and — if it has an `AGENTS.md` or a `CLAUDE.md` of its own — that file's rules
quoted under a heading naming that folder and saying they hold there and nowhere else. So I
never go hunting for a folder you chose, never ask you which one you meant, and never blend
two attached projects' house rules together. Instruction excerpts are limited to 4 KiB per
file and 12 KiB across attached folders; a partly used total leaves only its remaining
bytes for the next file. A cut excerpt names the file to read for the rest.
When you select a subfolder in a repository, applicable `AGENTS.md` and `CLAUDE.md`
files are read from the repository root down to that selected folder. Each excerpt
names its own directory scope; more specific nested rules take precedence there.
Sibling folders are not searched for rules, and shared ancestors are quoted once.
Two subfolders selected in the same repository remain separate attachments. Removing
one removes only that selected scope and its rules; the other stays attached. They
still share one repository when work needs an isolated branch.

Choosing a directory **inside a repository** attaches the repository, because a branch is
cut from a repository and not from a directory in it — and the line you get back says so by
naming the project rather than what you pointed at. Nothing about that is hidden from
either of us: the directory you actually pointed at is kept beside the project on the
record, and I am told both, so I start where you pointed and can still reach the rest.

What I am **not** given is the folder's
contents: attaching is a reference, not a copy, and I look inside with `ls`, `grep` and
`read` on the path you chose, the same as anywhere else. A folder that is no longer on disk
when you attach it is described that way rather than silently.

That line is said **only after the conversation has actually taken the folder**. Where it
cannot, nothing is added and you get
`this conversation cannot be given a folder · it has no way to remember one, so nothing
would reach the next request` instead. The picker does not open at all on such a
conversation, because a list you cannot choose from is not worth drawing.

**A folder inside a repository is added as the repository**, because work is cut from a
repository and not from a folder inside one — and the line says so rather than quietly
printing a folder you did not pick:

```
folder · ~/code/agentfield · the repository holding ~/code/agentfield/internal/session
```

**The conversation is about that folder, and it remembers.** The choice is written into the
conversation's own record, so closing the terminal does not lose it, and the next task you
propose **stands there**: a folder you named is the top rung of the ground ladder — "you
said so" — so nothing has to be worked out and you are not asked. *Which folder does a task
work in* has the whole ladder. The next `/folder` also opens with that folder on the first
rows.

**Work aimed at it is allowed to go there.** A task, or the model's own edits, can now
reach that folder — which is the whole point of choosing it. What it does **not** do is
move this conversation: the directory it is anchored to, the `AGENTS.md` it reads, and its
own status line stay exactly as they were.

## Where edits go — the folder you are standing in, and the ones you chose

Two rules, and they are the whole of it.

- **The folder you started aforge in is edited directly.** A write is a write; the file on
  disk changes the moment the model writes it. Nothing about that has changed.
- **A folder you chose is not.** Edits aimed there are kept for this conversation — you
  see `changes for <name> · 3 files · /land` above the message box — and your own copy of
  that folder does not move until you run `/land`. For a repository that is a branch taken
  from its current commit; for a plain folder it is a copy of it.

So choosing a folder is cheap and safe to get wrong: nothing reaches it until you have
been shown what changed and said so. See `## /land` below.

`bash` is the exception, and it is worth knowing: a shell command runs in the folder you
are standing in and touches whatever it names, including files in a folder you chose. Only
the model's `read`, `write` and `edit` go through the copy.

## /land — merge what you did into my folder, put the changes in, land the work, I committed or rebased before landing

`/land` on its own shows what is waiting and moves nothing:

```
changes for agentfield · 2 files · shared.txt, notes.md · type /land now to put them in
```

`/land now` is what actually puts them in, and says so:

```
agentfield now has the changes · 2 files
```

For a repository the work is committed on a branch of its own, and merged into your
checkout only where aforge is willing to write it — your uncommitted changes are left
alone, and if the merge cannot settle you get `could not put it all into agentfield ·` and
the sentence naming the files that clashed, with the branch kept so nothing is lost.

**Where your checkout is on a protected branch, the branch is kept instead and your
checkout is not touched.** `main`, `master`, `dev`, `develop`, `development`, `staging`,
`stage`, `trunk`, `production`, `prod`, `release` and any remote's default branch are never
merged into by automatic task landing. You get the sentence on its own — `its branch chat/agentfield-b22fb8 was
kept: your checkout is on dev, which tasks do not merge into automatically`
— and it is not a failure: the work is finished and on that branch, and `git merge` takes
it whenever you want it. The same keep happens when you are on a different branch than when
the work was cut, when you moved that branch to another commit yourself after the cut, or
when you are on no branch at all. Commits written by aforge's own landings do not count as
you moving it. For a plain folder the files are copied back over it by name, **whole or not
at all**: if any one of them cannot be placed, your folder is
left exactly as it was and you get `could not put it all into agentfield ·` with the reason.
The changes stay waiting, so `/land` is still there to try again once the way is clear.

With more than one folder waiting, `/land` asks which: `changes are waiting for agentfield
and notes · say which one · /land agentfield`. Then `/land agentfield now`.

**A folder lands whole.** There is no way to land some of the files and keep the rest
today — you either put the folder's changes in or leave them waiting.

## Work in a folder directly, without keeping the changes aside

Say so in your own words — "work in ~/code/notes directly", "edit it in place", "just
change it there". That is remembered for that one folder, and from then on writes aimed at
it change the folder itself immediately, exactly like the folder you are standing in.
Nothing guesses this; it is only ever set because you said it.

## Where did my changes go — the folder still looks unchanged

If the model edited files in a folder you chose with `/folder` and that folder still looks
exactly as it did, nothing is broken: the changes are waiting for this conversation, and
the row above the message box says so — `changes for agentfield · 3 files · /land`. Run
`/land` to see what they are and `/land now` to put them in.

The folder you are standing in is the other case entirely: writes there landed the moment
they happened, and there is nothing waiting.

## You changed my files? — what has actually reached my folder

Only two things ever change a file on disk without you saying anything more:

- a write in **the folder you started aforge in**, which is direct and always has been;
- a `bash` command, which runs there and touches whatever it names.

Everything the model writes for **a folder you chose with `/folder`** is kept aside until
you run `/land`. So a folder you chose is untouched until the moment you say so, and you
are shown the list of files first.

## Undo what you did to my folder — throwing away changes that have not landed

Changes waiting for a folder you chose have not reached it, so there is nothing to undo
there: simply do not run `/land`. They stay waiting when you close the conversation and
are offered again when you reopen it; they go for good when the conversation itself is
deleted.

Work that **already landed**, and changes in the folder you are standing in, are a
different question with a different answer — see
`## Undo what the agent did to my files — restoring the workspace` on the workspace page.

**Your working directory does not move.** The folder aforge is standing in — the one the
status line shows, the one the model's `AGENTS.md` and project settings come from, the one a
bare `notes.md` in your message means — is the directory you started aforge in, and nothing
picks it up and moves it. `/workspace <path>` sets it **once**, for a conversation that never
had one.

So the two are different questions: **where you are standing** is fixed for the life of the
conversation, and **what the conversation is about** grows as you name folders.

If the conversation is about two folders and the work does not say which, you are asked
once — `this conversation is about two places — <one> and <other> — so say which one this
task is about` — and the answer is remembered.

## Work on two projects in one chat, switch folders, open another repo, change directory

Four asks, four answers, and the first one is new.

- **"Work on my other repo in this same conversation."** — name it: `/folder`, or `/attach
  ~/code/other`, or name the path when you ask for the work ("fix the flaky test in
  `~/code/other`"). The conversation becomes about that folder as well as the one it is
  standing in — a folder you picked the moment you pick it, one you named in a request as
  soon as work is grounded there — and the next task about it needs no asking. You do not
  have to start a second conversation for a second project.
- **"Which folder do you mean?"** — `/folder`. It picks one, says so, remembers it, and
  the conversation can work there from then on — through a copy, landed with `/land`.
- **"This chat has no project; give it one."** — `/workspace <path>`, once. A conversation
  that already has a workspace answers `this conversation already has a workspace` and
  changes nothing.
- **"Move this conversation to another project."** — that one cannot be done. A conversation
  stands where it was started; `/home` opens or starts a conversation under any project on
  the machine, and that is the way to be standing somewhere else.

There is no `cd`, and a path typed into the message box does not move where you are
standing. What it does do is tell the work where to go: a path in your own words is the
ground ladder's top rung, so "fix the flaky test in ~/code/wisp" sends that work to
`~/code/wisp` whatever folder this window was opened in.

## Which folders is this conversation about — where can I see them, remove a folder

Three places. A folder the conversation merely read from shows nothing at all; it is the
model looking at the disk, which it could always do.

- **Above the message box.** A conversation that is about somewhere carries a small dim cell
  per folder on the same row the attachments ride — `▥ agentfield ✕` — and **clicking one
  takes that folder off the conversation**, which says `folder removed · ~/code/agentfield`.
  Three are named and the rest are counted (`+2 more folders`); the counting cell is a
  sentence and does nothing when pressed. Where a build has no way to take a folder off, the
  cells are still drawn and simply carry no `✕`. **The keyboard's way to the same thing is
  `/folder` and `enter` on that folder's row**, where the action row reads
  `remove this folder · <path>`.
- **`/folder`** — the first rows are this conversation's own folders, most recently used
  first. That is the whole set, on demand, in one keystroke.
- **Home** — the row for a conversation that is about somewhere beyond the project it is
  filed under ends `· also about <name>`, and `+2` when there are more; the card beside it
  says `also about` and names them, three at a time with `▸ …N more folders` behind the
  rest. Typing a folder's name into home's box finds conversations about it whichever
  project they were held in. *Home* has the whole screen.

Sixteen folders is as many as one conversation keeps. Past that the oldest one it worked
out for itself is dropped; a folder you named yourself is the last thing to go.

**Taking one off again.** A folder you attached can be removed, and removing it is
complete: it comes off the conversation's record, off the rows `/folder` opens with, and
out of my instructions, so the next thing you say reaches me with no mention of it. A
folder that has since been deleted from your disk can still be removed — the record
outlives the directory on purpose, so that a folder that went away is not stuck on the
conversation forever. Asking to remove a folder this conversation was never about is
refused in those words: `this conversation is not about <path>`. Removing a folder does
**not** touch the folder itself, and it does not throw away changes waiting for `/land`.

A folder I worked out for myself — the project a task turned out to stand in, rather than
one you chose — is not called attached and is never quoted to me as instructions you gave;
it is only where work went.

## Attach a folder — /attach with a directory after it

`/attach <path>` with a **folder** after it used to refuse with
`<name> is a folder · attach a file`. It does not any more: the folder goes to the same
place `/folder`'s `enter` sends one, and says the same line.

```
/attach ~/code/agentfield
folder · ~/code/agentfield
```

A **file** handed to `/attach` still goes on the tray as a file, exactly as before. So the
one command covers both, and you do not have to know in advance which of the two you are
pointing at.

Two things that are deliberately not this:

- **Dragging a folder onto the window still refuses** with
  `<name> is a folder · attach a file`. A drop is a gesture nobody typed, and reading a
  decision about your project out of a mouse would be inferring far too much.
- **Pasting a folder path** into the message box behaves the same way a drop does.

## Folders in the @ list

Typing `@` offers folders as well as files now. A folder row is marked `folder` on the
right, the way a picture row is marked `img`, and its path is written with a trailing
slash:

```
internal/tui3/           folder
internal/tui3/app.go
```

Choosing a folder inserts its path into your sentence exactly the way choosing a file does
— `@internal/tui3/` — which is a fast way to point the model at a directory without typing
the whole thing. It does **not** open the picker and does not add the folder to the ones
this conversation is about; it is text in your message, and the model resolves it.

The same list opens after `/attach `, `/image ` and `/export ` when you press `tab`, so the
folders are offered there too.

## Every refusal /folder can give you

Exactly as they are written:

```
choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.
this conversation cannot be given a folder · it has no way to remember one, so nothing would reach the next request
no folder matches · type a path to browse
no such folder · <path>
nothing below here
this folder cannot be read · permission denied
this folder is no longer here
this is a file, not a folder
this folder cannot be read
no such file: <name>
this is a folder now, not a file · <name>
these folders are already here
24 is as many as one message can carry · send these first
```

And the two `/land` gives you, exactly as they are written:

```
nothing is waiting · what this conversation writes in the folder it is standing in is already there
putting changes into a folder is not available over --host yet — the conversation is on the other machine.
```

- The first is `/folder` on a session opened with `--host`. The folders this program can
  read are on the laptop you are sitting at; the conversation is on the other machine, so
  every row it could draw would be somewhere the work cannot go. Type the far machine's
  path into whatever asks for one instead.
- The second is a conversation that has no way to hold a folder at all. Nothing is added
  and nothing pretends to be; `/folder` does not open.
- There is no longer a refusal for a machine with nothing remembered. `nothing to offer yet`
  used to stand here, and it met the one person least able to type a path; the sheet opens on
  your own tree instead.
- The third is a search that matched none of the known folders. The folder may still be
  there; the picker only ranks what it has seen, so type its path.
- The fourth is the add action on a row whose folder has since been moved or deleted. The
  rows come from memory, and one stat at that moment is what catches it.
- `nothing below here` is a folder that was read and has nothing inside it at all. You can
  still add it — a leaf is a perfectly good choice.
- The next four are reads that failed: a folder you are not allowed to open, one that has
  been moved or deleted since the row naming it was drawn, a path that turned out to name a
  file, and every other way a read can go wrong. None of them is ever reported as an empty
  folder.
- `no such file: <name>` and `this is a folder now, not a file · <name>` are a file that
  changed under you between the row you chose and the moment you pressed enter. The rest of
  what you chose still goes through; only the thing that moved is refused, by name.
- `these folders are already here` is a set of chosen folders with nothing left to do,
  because the conversation already holds every one of them.
- `24 is as many as one message can carry · send these first` is the choice cap. Nothing is
  silently dropped when you reach it.
- `/attach` handed a path is unchanged and still answers its own refusals
  (`no such file: <path>`, `<name> is already attached`); `/attach` with nothing after it
  opens this browser instead of correcting you.

## Finding preview controls while a path is typed

Every chord the sheet owns is named exactly once, across **two lines**, so neither is a
wall of shortcuts.

- The **foot row** above the action carries what you reach for: `esc cancel`, `enter add`,
  `←→ walk`, `alt+m choose`, `ctrl+u search`, and the preview's own door — spelled
  `alt+o preview` on a frame too narrow to draw the preview beside the list, and
  `alt+o wide` on one that is already showing it.
- The **box's own placeholder**, on screen whenever the box is empty, says what typing does
  and carries the two remaining chords: `search folders and files · or type a path ·
  alt+p preview · alt+h hidden`.

A terminal with too few rows gives the foot row back to the file list, and `esc · cancel`
stays on the sheet's bottom edge either way.

## Browse while keeping an unsent chat draft

Go Home and run `/folder` or bare `/attach` in Home's box. The sheet opens over
the conversation you were in; its unsent draft stays underneath, dimmed, where you
can see it and cannot type into it. Escape — or the **cancel** words on the sheet's
bottom edge — closes the sheet and returns that draft unchanged.
