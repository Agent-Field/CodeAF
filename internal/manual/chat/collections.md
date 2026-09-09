# Organizing references in collections

## Logical folders, collections and filesystem folders

`aforge collections` is a local command for organizing references to conversations,
tasks, ongoing items and files. A collection is a logical folder with a stable ID
and name. Several collections can reference the same record. Collections can
contain other collections, including one child shared by several parents;
circular membership is refused. Renaming a collection preserves its ID.

This command does not change the home dashboard, chat tabs or `/folder`.
`/folder` chooses filesystem context for a conversation. Collection membership
does not move transcripts, attach a working directory, grant write permissions,
start work or change an assignment. There is no collection slash command or
automatic context injection yet.

## Creating, listing and renaming collections

Run `aforge collections` or `aforge collections list` to list collections.
`aforge collections create "Marketing"` prints the new ID and name.
Use that ID with `aforge collections rename <collection-id> "Launch"`.
`aforge collections show <collection-id>` lists that collection's direct references.
Order follows creation and insertion, rather than inferred relevance.

All commands accept `--json` for structured output and `--db <path>` to choose a
separate collection database. The default is `~/.aforge/v3/collections.db`;
`AFORGE_HOME` moves the state root. Collections do not require a model API key
or optional learned memory. This database must be separate from the memory
database. An incompatible database is refused rather than reset.

## Adding, removing and finding a chat or work reference

Use `aforge collections add <collection-id> <kind> <record-id>`.
The kinds are `collection`, `conversation`, `task`, `standing` and `artifact`.
A task also requires `--session <conversation-id>` because task numbers repeat
in different conversations. For example, `aforge collections add <collection-id>
task 1 --session <conversation-id>` records that specific task.

`artifact` takes a local file path, stored as an absolute path. This is a path
reference, not a versioned content identity; renaming the file does not update it.
Collection references must name an existing collection. Other references are
not checked for availability, so they can be retained while their source is offline.
These commands list references, not current execution status or record contents.

`aforge collections find <kind> <record-id>` lists direct memberships; use the
same `--session` when finding a task. `aforge collections remove <collection-id>
<kind> <record-id>` removes only that membership. It neither deletes the original
record nor stops ongoing work. Repeating add or remove is harmless. Collection
deletion, automatic organization, inherited instructions and communication
between conversations are not implemented by these commands.
