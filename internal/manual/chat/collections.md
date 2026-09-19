# Logical folders — grouping saved chats

## Group chats in folders — logical folders, the folders panel, /folders

Home has a `folders` panel for **logical groups of chats**. It is not a directory
on disk. The heading is the word `folders`. With nothing in it yet the panel
keeps that heading and one dim line:

```
logical groups of chats · /folders create Billing
```

It never says “no folders yet”. `/folders` focuses the panel. `/folders create
Billing` makes a logical folder named Billing. Nested folders and a folder shared
by two parents are allowed; a cycle is refused and nothing is half-applied.

Unfiled chats sit under a virtual **Root**. Root is a logical scope, not a path,
and `codeaf collections list` never prints a Root row.

`/folders` is **not** an alias of `/folder`. `/folder`, `/place` and `/dir` still
choose a filesystem directory. Filing a chat here does not change cwd, the
repository, `/attach`, or which project `/folder` named.

The CLI `codeaf collections` still files the same graph. There is no `/collections`
slash in this build.

## Saved chats — /folders add, new chat here, verbs n f m w x

A saved chat is one conversation identity (the 16-hex session id). Grouping it
does not copy the transcript.

On a `folders` row, `→` opens the verb strip:

`n` new chat here · `f` add current chat · `m` move this placement · `w` why here · `x` remove this placement

- **`n`** opens the start page with that folder pending. The first message mints
  a new chat and files it here. `esc` before sending creates no transcript.
- **`f`** and `/folders add <name-or-id>` file the **current** chat in that
  folder. Repeating the add is harmless.
- **`m`** moves only the placement you are on. Other folders that already hold
  this chat stay.
- **`w`** shows why this chat is here — who put it here and the reason — not a
  score.
- **`x`** removes that placement only. The chat and its history remain.

An old chat can be added to a new folder without merging or resuming it.

## Rename a folder — /folders rename, both parents

`/folders rename Receipts Invoices` changes that folder's display name. Receipts
can sit under Billing and Security at once; the new name shows through both
paths, and the chats inside it stay. Nested folders are members whose kind is
`collection`, not a second copy of a chat. A cycle — making a folder a child of
its own descendant — is refused and nothing is half-applied.

## Also in two folders — one chat, also in Billing and Security

The same saved chat can sit in two folders at once — Billing and Security, for
example. There is one identity, one history, one spend. Opening it from either
placement shows the same messages.

A row in one folder names the other as `also in` and the other folder's name, so
you can see it is not a second copy. Counts are unique conversation ids, not
paths through the graph.

A later message sent through one placement is there when you reopen through the
other. Removing one placement leaves the rest. Moving the Billing placement into
Receipts does not drop the Security one.

## Is /folder a logical folder? — /folders vs /folder, /place, /dir

No. `/folder` is a **filesystem** command. It opens the add-context sheet so this
conversation can be about a directory. Its other words are `/place` and `/dir`.
It does not group saved chats.

`/folders` (plural) is the **logical** command. It is not an alias of `/folder`.
`checkCommands` keeps them distinct: typing `/folders` never runs `/folder`.

| Command | What it names | What it changes |
| --- | --- | --- |
| `/folder` `/place` `/dir` | a directory on disk | which project this chat is about; not membership |
| `/folders` | a logical group of chats | home's `folders` panel |
| `/folders create <name>` | a new logical folder | the collections graph |
| `/folders add <name-or-id>` | the current chat's membership | one placement; not cwd |
| `/folders rename <name-or-id> <new-name>` | that folder's display name | the same folder under every parent |

Logical membership never changes cwd, the repository, or `/attach`. After you
file a chat, `/folder` still names the same path it named before.

## Where do I file a task

File a task reference with `codeaf collections add <collection-id> task
<task-id> --session <conversation-id>`. A task needs its conversation ID because
task numbers repeat across conversations. You can file the same task in several
collections alongside related chats and files. Filing does not move, copy, start
or stop the task; its original conversation still owns its execution.

The conversation ID is the 16-hex name of the folder holding its journal at
`~/.codeaf/v3/projects/<the workspace path with its separators turned to dashes>/<id>/transcript.jsonl`;
it is the same ID the transcript header carries, and `CODEAF_HOME` moves the root.
`codeaf collections show <collection-id>` prints the IDs already filed back to you.
There is no command that lists conversation IDs.

## Creating, listing and renaming collections

Run `codeaf collections` or `codeaf collections list` to list collections.
`codeaf collections create "Marketing"` prints the new ID and name.
Use that ID with `codeaf collections rename <collection-id> "Launch"`.
`codeaf collections show <collection-id>` lists that collection's direct references.
Order follows creation and insertion, rather than inferred relevance.
Reading a fresh home or an existing empty file does not create a database.
`create` initializes it when needed.

All commands accept `--json` for structured output and `--db <path>` to choose a
separate collection database. The default is `~/.codeaf/v3/collections.db`;
`CODEAF_HOME` moves the state root. Collections do not require a model API key
or optional learned memory. Pointing `--db` at a database that already belongs
to another feature is refused, and so is a database written by an incompatible
version; neither is reset. With no collections yet, listing answers `No
collections found. Create one with codeaf collections create <name>.`

## Database is locked, busy, two windows or another codeaf command at the same time

Several codeaf commands and windows may use one collections database at the same
time. A write waits up to ten seconds for another one to finish rather than
being dropped. If that wait
runs out, it answers `the collections database is busy being written by something
else (waited 10s)`. Nothing was changed, and running the command again is safe.

Listing, showing and finding never wait for a writer and never create the
database, including when `--db` names an existing empty file. If the selected
path cannot be opened, the command says which path and why: `is not a regular
database file` for a directory or other non-file, `permission denied` when it
cannot be written, and `is not a collections database` when its contents do not
belong to collections.

## Adding, removing and finding a chat or work reference

Use `codeaf collections add <collection-id> <kind> <record-id>`.
The kinds are `collection`, `conversation`, `task`, `standing` and `artifact`.
A task also requires `--session <conversation-id>` because task numbers repeat
in different conversations. For example, `codeaf collections add <collection-id>
task 1 --session <conversation-id>` records that specific task.

`artifact` takes a local file path, stored as an absolute path. A relative path
is resolved against the directory you run the command in, and `~` is expanded,
before it is stored. `find` needs the same absolute path; symlink aliases remain
different references. This is a path
reference, not a versioned content identity; renaming the file does not update it.
The file does not have to exist. Collection references must name an existing
collection, and both the collection you add to and the collection you name must
exist or the command answers `collection not found`. Other references are not
checked for availability, so they can be retained while their source is offline.
These commands list references, not current execution status or record contents.

`codeaf collections find <kind> <record-id>` lists direct memberships; use the
same `--session` when finding a task. A record in none answers `No collection
references this record.` `codeaf collections remove <collection-id> <kind>
<record-id>` removes only that membership, answering `This collection no longer
references it; the original record is unchanged.` It neither deletes the original
record nor stops ongoing work. Repeating add or remove is harmless, and removing
something that was never there is not an error.

When the `folders` tool is on the belt it can `list`, `file`, `unfile` and `move`
the current chat the same way. It is absent rather than present and failing when
collections cannot open. It refuses a cycle or an unknown id in its result text.

## Automatic organization, inherited instructions, and chats talking to each other

Not in this build. Automatic organization does not file chats for you. A logical
folder does not attach inherited instructions that descendant chats pick up.
Conversations do not send each other messages. You create folders and add, move
or remove placements yourself — from the `folders` panel, `/folders create` /
`/folders add`, the `n f m w x` verbs, or `codeaf collections`. Collection
deletion is not implemented by these commands either.
