# Organizing references in collections

## How do I group chats in logical folders

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

## Where do I file a task

File a task reference with `aforge collections add <collection-id> task
<task-id> --session <conversation-id>`. A task needs its conversation ID because
task numbers repeat across conversations. You can file the same task in several
collections alongside related chats and files. Filing does not move, copy, start
or stop the task; its original conversation still owns its execution.

## Creating, listing and renaming collections

Run `aforge collections` or `aforge collections list` to list collections.
`aforge collections create "Marketing"` prints the new ID and name.
Use that ID with `aforge collections rename <collection-id> "Launch"`.
`aforge collections show <collection-id>` lists that collection's direct references.
Order follows creation and insertion, rather than inferred relevance.
Reading a fresh home does not create a database. `create` initializes it when needed.

All commands accept `--json` for structured output and `--db <path>` to choose a
separate collection database. The default is `~/.aforge/v3/collections.db`;
`AFORGE_HOME` moves the state root. Collections do not require a model API key
or optional learned memory. Pointing `--db` at a database that already belongs
to another feature is refused, and so is a database written by an incompatible
version; neither is reset. With no collections yet, listing answers `No
collections found. Create one with aforge collections create <name>.`

## Adding, removing and finding a chat or work reference

Use `aforge collections add <collection-id> <kind> <record-id>`.
The kinds are `collection`, `conversation`, `task`, `standing` and `artifact`.
A task also requires `--session <conversation-id>` because task numbers repeat
in different conversations. For example, `aforge collections add <collection-id>
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

`aforge collections find <kind> <record-id>` lists direct memberships; use the
same `--session` when finding a task. A record in none answers `No collection
references this record.` `aforge collections remove <collection-id> <kind>
<record-id>` removes only that membership, answering `This collection no longer
references it; the original record is unchanged.` It neither deletes the original
record nor stops ongoing work. Repeating add or remove is harmless, and removing
something that was never there is not an error. Collection deletion, automatic
organization, inherited instructions and communication between conversations are
not implemented by these commands.
