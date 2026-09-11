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
`aforge collections show <collection-id>` lists that collection's direct references,
then any work placed in it under its own line, `Placed here, so this folder's rules
reach it:` — a placement is not a reference, and only a placement gives the folder's
rules reach. `--json` answers `{"references": [...], "placed": [...]}`.
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
references this record.` Folders whose rules reach it follow under `Placed in, so these
folders' rules reach it:`, each `(placed directly)` or `(placed 1 folder(s) below)`;
`--json` answers `{"references": [...], "placed": [...]}`. `aforge collections remove <collection-id> <kind>
<record-id>` removes only that membership, answering `This collection no longer
references it; the original record is unchanged.` It neither deletes the original
record nor stops ongoing work. Repeating add or remove is harmless, and removing
something that was never there is not an error. Collection deletion, automatic
organization, inherited instructions and communication between conversations are
not implemented by these commands.


## Organizing by asking in chat: what have I got filed in a folder, and filing a watch under one

Ask to create a collection, file this chat or a piece of ongoing work in it, or
inspect its work. The `collections` tool supports list, show, find, create, add
and remove. Omitting `ref` on add or find means this conversation; to file ongoing
work instead, such as a watch, `ref` names kind `standing` and the id `stand`
returned for it. Filing is `add`: it never makes the folder's rules reach the work,
which only `place` does. Show lists what `aforge collections show` prints, with its
references under `items`: what the folder files, then under `placed` what is placed
in it, whose rules reach it. Each row carries its kind — any of `conversation`,
`task`, `standing`, `collection` (a subfolder) or `artifact` (a file) — its id, a
one-line title and, where its owner keeps one, its state. It does not
resume a closed conversation or start its work. Unavailable sources remain listed.
Find accepts a case-insensitive name fragment or a member reference. Omitting both
finds collections containing this chat; name and reference cannot be combined.
Find by reference lists what `aforge collections find` prints: the folders that
file it under `items`, then under `placed` the folders whose rules reach it; only
those carry a depth (0 placed directly, higher for a folder further up). One page
holds at most 25 across both lists; `next_offset` continues. `governing` answers
which folders' rules reach a record. Task workers can inspect collections but
cannot reorganize them.

## How do I share a decision or finding across chats

Use `shared_context` to create a titled record with text and explicit targets.
Create and revise require a targets array; an explicit empty array is allowed.
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
title, text and complete target set; omitting targets is refused (an explicit empty
array clears them). A stale revision is refused, so simultaneous
edits do not silently overwrite one another. Withdraw retains its history but
removes it from applicable-context queries. History preserves earlier text,
sources and targets.

At each supported chat, worker, checker, fork or scheduled turn, a bounded snapshot includes current
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
semantic discovery beyond links, or automatic cross-work activation. Scheduled runs read context for their owning standing item; they do not acquire the setup chat's folder bindings.
The conversation's completion reader sees the same bounded snapshot as the turn.
A task's separate read-only checker also receives shared context and fresh governing directions for its owning work. It keeps its existing acceptance and file checks.

## Can I still read a record after removing my chat from its collection

Yes. Reading `shared_context` by ID can reach records outside this conversation;
reading them does not add membership or restore their applicability. A read
returns `applicable_here`: whether this exact revision is current, not withdrawn,
and explicitly applies to the current conversation or ordinary worker's scope.
An old revision, withdrawn record or record outside this scope returns false
while its text remains readable. `scope_note` explains that distinction.

Use list without explicit targets to inspect what currently applies here. Removing
one collection link still leaves context available through another matching link.
Removing the last link retires that context at the next turn, including after
reopening a chat. Rejoining restores it if the record is still current and active.
A record's existence is not proof that it applies here, and its information is
not an instruction or permission.

## Which folder rules apply here, and how do I change them

A reference made with `collections` action `add` helps navigation and shared
information. It does not make that folder's instructions govern the work.
In a conversation with you, `place` explicitly adds a governing folder binding;
`unplace` removes that binding. Both take `id` for the folder and optional `ref`
for the work (default: this chat). Several bindings may apply. These actions do
not move files, change old outputs or remove navigation references. A move between
governing folders is removing one binding and adding the other; for ongoing work,
`stand` op edit with the new `placement` does both on one card.

Use action `governing` to inspect current governing folders and their depths:
zero means direct, higher values mean ancestors through governing placements.
Folder rules reach those ancestors' descendants only when explicitly declared.
Workers and unattended sessions can inspect these bindings but cannot change them.
The terminal has the same two verbs: `aforge collections place|unplace
<folder> <kind> <id>`, beside `add|remove`, which only file a reference.
Existing reference memberships are never promoted into governing bindings when
the database upgrades.

## Why did this run use those instructions or call that tool

Ask to inspect `context_trace`. It reads the current execution journal and returns
recorded context selections plus original journal-line references for tool calls,
replies and outcomes. A selection records the inputs supplied to execution; it
does not prove the model followed them. Standing input text is retained with its
revision because those records can later change. Shared context points to its
immutable source revision.

Pages contain at most 40 rows with a bounded preview. Continue with `next_line`;
when detail is omitted, the original journal line contains it. Tool arguments,
reply bodies and model reasoning are not duplicated in this view. A returned
loop is not a successful outcome, and a tool reply is not proof of an external
effect. Overlapping execution windows are marked ambiguous. A run started by a
standing order records its cause (`parent_cause: standing_occurrence`, with the
order, its instructions version and its `occurrence.json`); every other
execution says `not_recorded`. This is local execution evidence, not a complete
history of why every background decision happened. Journals written before this feature
have no retrospective context selection receipts.

Recording requires a writable journal. A journaled turn reports when its context receipt could not be saved. Missing
or failed journal writes do not create a retrospective receipt; absence is not proof that no work ran. Inspection
stops after a bounded 16 MiB scan and reports the limitation explicitly.

Forked hands receive fresh governing context but have no separate journaled
selection receipt yet. Their parent retains its existing fork call and result.
