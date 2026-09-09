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
start work or change an assignment. There is no collection slash command. Chat can inspect and organize logical
collections through the `collections` tool. Explicitly shared context can reach
chats and task workers without enabling learned memory.

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


## Can I organize and inspect work by asking in chat

Ask to create a collection, file this chat in it, or inspect its work. The
`collections` tool supports list, show, find, create, add and remove. Omitting
`ref` on add or find means this conversation. Show resolves direct members
through their existing owners: chat titles, task state, ongoing items and file
availability. It does not resume a closed conversation or start its work.
Unavailable sources remain listed. Results are paged with `next_offset`.
The local `aforge collections show` command still prints references only.
Task workers can inspect collections but cannot reorganize them.

## How do I share a decision or finding across chats

Use `shared_context` to create a titled record with text and explicit targets.
Targets may name chats, tasks, collections, ongoing items or artifacts. Automatic
turn context currently reaches chats and ordinary task workers: their own
address, the worker's owning chat, and direct collections of those addresses.
Ongoing-item and artifact targets can be inspected explicitly, but do not yet
receive automatic delivery. A record aimed at a collection reaches its direct
member chats and tasks; ancestor folders are not implicitly included. Merely linking two records
does not share every message between them. One shared record keeps one ID even
when it has several targets.

Each revision records the conversation that wrote it; a revision from a different
chat carries that chat as its source, while history preserves earlier sources.
The runtime supplies the address; the model
cannot substitute somebody else's source. That address identifies where it was
recorded, not proof that the person endorsed every sentence. These records are
information, not instructions or permissions. Use the existing standing-order
flow for instructions or scheduled responsibilities.

## How do I revise, withdraw or inspect shared context

`shared_context` supports list, read, history, create, revise and withdraw.
Read the current revision before changing a record. Revise supplies its replacement
title, text and complete target set; a stale revision is refused, so simultaneous
edits do not silently overwrite one another. Withdraw retains its history but
removes it from applicable-context queries. History preserves earlier text,
sources and targets.

At each chat or ordinary task-worker turn, a bounded snapshot includes current
context for that work and its direct collections. Revised or withdrawn context
replaces earlier snapshots at the next turn; it does not interrupt an in-flight
model response. List and history return metadata in pages of 25; use `next_offset`. Read returns
up to 4,000 Unicode characters and `next_text_offset` when more remains. Continue
with the returned revision to avoid mixing versions. Create, revise and withdraw
return metadata; read by ID for the text. Records allow a 256-byte title, 65,536
bytes of text and 64 distinct targets. A turn includes at most six records with
1,200 characters each; truncation and additional records are identified. Task workers
can read shared context but cannot create, revise or withdraw it. This works
with learned memory disabled. It does not yet implement automatic consultation,
semantic discovery beyond links, or sharing context with a scheduled firing.
