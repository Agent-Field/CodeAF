# Choosing a folder

## /folder — choose a folder, pick a directory, say which project you mean

`/folder` opens a picker over the directories aforge already knows about. It also answers
to `/place` and `/dir`, because people arrive with three different words for the same
thing.

```
/folder            the picker, opened on what is already known
/folder aforge     …with `aforge` already typed, so the list is narrowed
/folder ~/code/    …with a path already typed, so the columns are open in ~/code
```

It opens **instantly**. Nothing is scanned when you press enter on the command: the rows
come from what aforge has already seen, in layers, best first.

1. the directories **this conversation has been reading and writing in**, most recent first
2. every **project on this machine** — the same list `alt+w` cycles on the task composer
3. every **repository under your home directory**, from an index built quietly in the
   background and refreshed about once a day

Inside a layer the order is how often you have picked that folder here, weighted by how
recently — so after a week of use the folder you want is usually the first row, and the
whole gesture is `/folder` then `enter`.

`esc` leaves everything exactly as it was: your half-written message comes back untouched,
and nothing has been chosen.

## Type a word to filter, type a path to browse

The box under the list is one box doing two jobs, and which job it is doing depends only on
the shape of what you type.

- **A word filters.** `agent`, `tui3`, `notes` — the list narrows with the same fuzzy
  matching the `@` file list uses.
- **A path browses.** Anything starting with `/`, `~/`, `./` or `../` — and a bare `~`,
  `.` or `..` — turns the list into **columns**: the folder above on the left, the folders
  inside where you are in the middle, and what aforge knows about the highlighted one on
  the right.

A bare word is never treated as a path. Typing `agentfield` means "find it for me", not
"open ./agentfield".

In the columns:

| Key | What it does |
|---|---|
| `↑` `↓` | move down the middle column |
| `→` | walk into the folder under the cursor |
| `←` | walk out to the folder above, cursor left on the one you came from |
| `tab` | complete the highlighted folder's name into the box, whole — the columns follow, so it lands where `→` does |
| `enter` | take the folder under the cursor |
| `esc` | leave, having changed nothing |

Only folders are shown. Hidden folders (anything starting with `.`), `.git`, `vendor` and
`node_modules` are skipped — the same rule the `@` file list follows. One level is read at
a time; nothing walks deep.

## What the right-hand column tells you about a folder

The third column is what the machine already knows about the folder under your cursor, dim,
one line per fact:

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

## What choosing a folder actually does right now

`enter` says one line into the conversation:

```
folder · ~/code/agentfield
```

and writes the choice down, so the next `/folder` opens with that folder near the top.

**That is all it does today, and it is worth being plain about.** Choosing a folder does
**not** move this conversation's own working directory, does not change which `AGENTS.md`
the model is reading, and does not make edits land somewhere else. The directory the
conversation is anchored to is set at launch, or once with `/workspace <path>`, and where a
task's work happens is decided when the task starts. If you want this conversation to be
about a different project, start one there — `/home` lists every project on the machine and
opens a conversation in any of them.

## Open another project, work on a different repo, change directory

Three different asks, three different answers.

- **"Which folder do you mean?"** — `/folder`. It picks one, says so, and remembers it.
- **"This chat has no project; give it one."** — `/workspace <path>`, once. A conversation
  that already has a workspace answers `this conversation already has a workspace` and
  changes nothing.
- **"I want to work in a different project."** — `/home`, then open or start a conversation
  under that project. A conversation is anchored where it was started; there is no command
  that picks it up and moves it.

There is no `cd`. Typing a path into the message box does not change anything either — the
model reads and writes through its own tools, against the directory the conversation was
started in.

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
the whole thing. It does **not** open the picker and does not choose the folder as a place;
it is text in your message, and the model resolves it.

The same list opens after `/attach `, `/image ` and `/export ` when you press `tab`, so the
folders are offered there too.

## Every refusal /folder can give you

Exactly as they are written:

```
choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on.
nothing to offer yet · type a path after /folder, or use the picker's box
no folder matches · type a path to browse
no such folder · <path>
nothing below here
```

- The first is `/folder` on a session opened with `--host`. The folders this program can
  read are on the laptop you are sitting at; the conversation is on the other machine, so
  every row it could draw would be somewhere the work cannot go. Type the far machine's
  path into whatever asks for one instead.
- The second is a brand-new machine with no projects, nothing touched yet and no index —
  typing a path is the way through.
- The third is a filter that matched none of the known folders. The folder may still be
  there; the picker only ranks what it has seen, so type its path.
- The fourth is `enter` on a row whose folder has since been moved or deleted. The rows
  come from memory, and one stat at `enter` is what catches that.
- The last is the middle column of a folder with no folders inside it. You can still press
  `enter` on it — a leaf is a perfectly good choice.
