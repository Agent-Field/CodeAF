# Organizing references in collections

## How do I group chats in logical folders

`codeaf collections` is a local command for organizing references to conversations,
tasks, Factory jobs and files. A collection is a logical folder with a stable ID
and name. Several collections can reference the same record. Collections can
contain other collections, including one child shared by several parents;
circular membership is refused. Renaming a collection preserves its ID.

This command does not change the home dashboard, chat tabs or `/folder`.
`/folder` chooses filesystem context for a conversation. Collection membership
does not move transcripts, attach a working directory, grant write permissions,
start work or change an assignment. There is no collection slash command or
automatic context injection yet.

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
Names are unique without regard to case, so `create` or `rename` to a name
another collection already has answers `another collection already has that
name`; renaming a collection to its own name in another case is allowed.
Case is compared letter by letter without regard to language: `Ärger` and
`ärger` are one name, and so are `ΣΊΣΥΦΟΣ` and `σίσυφος`, but `İstanbul` and
`istanbul`, or `Straße` and `STRASSE`, are two. `list` and `show` print every
collection and every reference, however many there are.
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
The kinds are `collection`, `conversation`, `task`, `process`, `crew`,
`factory_job` and `artifact`. There is no kind
for an automation. `standing` named a standing item, which codeaf no longer has: a
row that already holds one can still be found and removed, and adding a new one is
refused with `standing items are gone, so a collection cannot gain one`.
A task, and a process, also requires `--session <conversation-id>` because task
numbers repeat in different conversations. For example, `codeaf collections add <collection-id>
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
something that was never there is not an error. Automatic organization,
inherited instructions and communication between conversations are not
implemented by these commands.

## Adding, finding and removing a Factory job

Use `codeaf collections add <collection-id> factory_job <job-number> --factory
<factory-id>`. The job number must be a positive decimal without a sign or leading
zeroes. The factory ID is required: the same number in two factories names two
different jobs. Use the same `--factory` with `find` and `remove`. Other kinds
refuse a factory ID. `show` prints `factory_job  <job-number>  factory <factory-id>`;
`--json` includes the `factory` field.

A plain collection can reference a job alongside any other records. A job can
belong to only one workstream collection. Adding a job already in another
workstream answers `that job belongs to another workstream` and leaves every
reference in that add unchanged. These commands retain references without asking
the Factory whether the job is available and never start a job.

## Deleting or duplicating a collection

`codeaf collections delete <collection-id>` deletes the collection, its
memberships, and every membership naming it in another collection, answering
`Collection deleted; the records it referenced are unchanged.` Nothing it
referenced is deleted, moved or stopped. `codeaf collections duplicate
<collection-id> <name>` makes a new collection holding the same references, in
the same order; the new name follows the same uniqueness rule as `create`.

A workstream collection refuses deletion with `mark a workstream done instead`.
Duplicating one makes a plain collection with the same references; it creates no
workstream and claims no jobs.

## Workstreams in an app client: stages and agent chats

An app client can create a workstream backed by a collection with the same ID.
It includes one agent conversation, an optional origin chat, and the references
you choose. Creation opens the agent without sending a first message or running
a model. Repeating a nonempty client ID returns the original workstream.
Names follow collection naming rules; the client ID is at most 128 bytes and a
create takes at most 200 explicit references.

A workstream starts in Shaping. Observing a member job queued, running, needing
you or done changes it to Building, which persists even if jobs return to Backlog.
Only marking the workstream done gives Done; all jobs merged does not. Reopening
returns it to Building. Marking done can archive its member chats while keeping
the agent's record. Collection rename and membership commands use the same store.

## Workstream pages, records, plans and go-aheads

Pages, page history, plans, decisions, questions and settings live in SQLite.
Markdown is an export, not a second store. A new workstream has an Overview page;
it can be renamed but cannot move or be deleted. Other pages can form a tree;
deleting a page deletes its descendants. Saving a stale page or plan refuses
without changing it. Restoring a page creates a new version. Page titles take
1–200 bytes and editor documents at most 1 MiB; record text takes 1–2000 bytes.
Client IDs, titles and source labels refuse control characters. Why, how it went,
record text and draft text allow newline and tab, and refuse other control
characters. Malformed app requests return `bad_params`.

Decisions and questions can be proposed, kept, dropped and replaced. Keep can undo a drop;
a replaced record cannot change again. Plans store milestones, ordered waves
and job or draft items. Wave auto-start is stored without starting jobs.
A plan holds at most 500 milestones, 500 waves and 5,000 items; its stored
milestones and waves together encode to at most 4 MiB of JSON.
Plans beyond these limits return `too_large` without changing anything or
sending a change event.

A go-ahead requires a member job, a planned wave or milestone, a positive cap
at most $10,000 and an end in the future at most 90 days away. Expired grants
read as ended. Marking done ends every still-active grant; granting on a done
workstream is refused. These are stored permissions; Factory enforcement is a
separate capability. Workstream numbers use recorded job facts and agent spend.
Updating, marking done and reopening persist the first observed start and return
those numbers using the same reads as getting the workstream.
Unavailable readings stay absent; a measured zero is present. Budgets and autonomy
settings do not themselves start work.

## What a workstream's agent can do: proposals, pages and suggested edits

A workstream's own agent in the app has seven tools. Other conversations, tasks
and errands do not receive them. `workstream_read` reads the workstream, pages,
decisions, questions, go-ahead proposals, plan, grants and sources. Counts, costs, states and dates come
from records. An absent number is unknown; the agent must say so and never
estimate it. Page live blocks such as `[live: jobs]` show current facts.

`propose_decision` and `propose_question` create proposals by the agent. The
agent proposes; the person keeps. `propose_jobs` adds 1–20 draft jobs to the
plan, leaving existing waves and settings intact. Drafts create no Factory
jobs and start nothing; the person launches them. A plan conflict is retried
once against a fresh read, then refused if it changes again.
Each `propose_jobs` draft's text is at most 4,000 bytes; a plan stores draft
text up to 80,000 bytes. A workstream holds at most 5,000 pages, including
deleted pages, and 5,000 records of every kind and state.

`write_agent_page` creates or updates only the agent's own pages. Every write
carries an AI mark, its written-at time and a named source; history keeps that
attribution. New pages titled Spec or Decisions have Read by jobs on, ignoring
case and surrounding space; other new pages have it off. Updates keep the flag.
The agent refuses the person's pages with “That page is the person's. You write
only your own pages; suggest edits on theirs with suggest_page_edit.” Page
documents are limited to 1 MiB. A page that changed must be read again before
writing. Done workstreams refuse all these writes.

Tool arguments refuse unknown fields and repeated object keys, including inside
sources and draft jobs, before reading or writing the workstream. The agent keeps
its page titled “Where it stands” current; the Overview shows it with the AI mark.

## Suggested edits on your workstream pages: keep or drop

`suggest_page_edit` suggests 1–50 edits on one of your pages, using its version
from `workstream_read`. The agent quotes unique plain text from one paragraph,
heading, list item or code block, without Markdown marks or escapes. Each find
is 1–4,000 bytes; each replacement is at most 4,000 bytes. The suggestion and
each edit carry a reason of 1–200 bytes on one line. Only code blocks accept
line breaks in replacements. A workstream holds at most 5,000 suggestions,
including decided suggestions. Page reads include open suggestions.

You keep or drop one edit or all proposed edits in a suggestion. Until you keep
an edit, the page is unchanged. Each keep makes one new page version attributed
to you, with the suggestion as its source; keep all still makes only one version.
Drop leaves the page and history untouched. Remaining edits in the same block
move with kept text. An unchanged block can move in the document and still apply;
changing its text makes its edits stale. A keep with a stale edit changes nothing.
Done workstreams allow reading suggestions and refuse creating, keeping or dropping them.
The suggestion source kind is written only by keeping; source parameters refuse it.

## Workstream draft size, budget and sources

Draft plan items accept optional size `S`, `M` or `L`, a budget greater than zero
and at most $10,000, and a source. Job plan items refuse these fields. Older
plans without them still read normally. These fields are stored without
starting work. The agent cannot set draft size or budget. `propose_jobs` gives
each added draft the agent conversation as its source, or an explicit page,
member job or HTTP(S) link. Existing items and a conflict retry keep their sources.

App page creation and saves accept an optional source: chat, message, Factory
job, page or URL. A message source needs its chat ID; a job source needs its
Factory ID. Source labels take at most 200 bytes on one line. The source becomes
the page's `updated_from` and that version's `from` in history. A later save
without a source clears `updated_from` and has no `from` on its version.

## Workstream go-ahead proposals, keeping and undo

`propose_goahead` makes a proposed `goahead` record with the reason as its text
and structured scope, target, cap and end. It grants nothing. Keeping it grants
exactly one go-ahead atomically; keeping it again changes nothing. Dropping a
kept proposal revokes its still-active grant. Keeping it after that grants a
new one. Records retain the latest grant ID, including after a drop. Go-ahead
records cannot be replaced with `record.supersede`.

The proposed cap is greater than zero and at most $10,000, including fractional
dollars such as $12.50. The agent's `ends_on` date must be after today in UTC and
at most 89 days ahead; the end is that day's 23:59:59 UTC. Proposal and keep both
check that the target exists and the end is future and within 90 days. Proposing
or keeping on a done workstream conflicts. A refused keep changes nothing.
Expiry, revocation and marking done leave a kept record kept while ending its grant.

`record.list` returns decisions and questions by default. Use
`include_goaheads: true` for every kind, or `kind: goahead` for only go-ahead
records. The agent's record reads include go-ahead proposals and their grant IDs.

## Watching workstreams: morning wakes, costs, Apply, Undo and Not now

With morning wakes on, a workstream can wake once for today's 08:00 slot in
this machine's zone, while the app is open, once that slot has passed and if it
is at or after creation. Before 08:00 no morning slot is eligible; closed days
are never caught up.
The app first checks after 30 seconds, then every minute; opening late wakes
once, without replaying a backlog of missed days. Related-issue wakes are
stored as a preference but are not available yet. Job-event handling is ready
for finished, merged, failed and needs-you events; automatic delivery awaits
the Factory event source. Nothing else wakes the agent. Done or autonomy Off
stops new wakes.

Each wake makes one model call, bounded to three minutes, and returns one card
with a short summary and up to ten ranked applied changes and proposals. Propose only applies
nothing. Small changes can move plan items to manual waves without a wave
go-ahead, add follow-up drafts with a budget below $5 to a manual wave, and
write the agent's pages. A draft stays a draft. Decisions and questions remain
proposals. The person's pages are never written by a wake.

## Applying and undoing wake proposals: Apply, Undo applied and Not now

Apply chooses one or all proposals, regardless of autonomy; a decision or
question becomes kept. Undo applied restores changes only while their targets
are unchanged. Updated pages restore a previous version as a new agent version;
new pages are restorably deleted. A changed target refuses a single Undo;
Undo applied leaves that item with “Changed since; not undone.” Not now
dismisses the remaining proposals. Repeating an action changes nothing.
Done workstreams refuse Apply and Undo.

Every recorded wake cost counts toward workstream spend. Unknown costs stay
absent. At most 24 wakes per workstream and 200 per home can run per local day,
with one turn at a time. A running wake left over 15 minutes becomes failed on
the next check or list: “Stopped before it finished.” Its cost is unknown.
An unreadable answer says “The agent's answer could not be read.” Any recorded
cost still counts toward spend.

## Workstream sources: files, repos, issues, links and kept work

The app's workstream Sources are pointers kept with `source.add` and returned
oldest first by `source.list`. Five kinds are available: a file or folder on
this computer; a repo named `owner/name` or a local folder; an issue with its
repo and number; an HTTP(S) link; or a kept record, page, message or member
Factory job. They are separate from collection members and canvases. Removing
a source with `source.remove` removes only its pointer and leaves its target
intact. The engine does not copy or index source contents.

File paths must exist, be absolute and already clean, with no control
characters, dot segments, doubled separators or trailing separator. Local
repos must be folders. Source strings must be valid UTF-8. Paths take at most
4,096 bytes; links at most 2,048 both as sent and after URL normalization,
with a host and no whitespace or credentials. Titles take 1–200 bytes of
trimmed text on one line; absent titles are derived from the target. Issue
numbers run from 1 to 2,147,483,647. Kept reference IDs take 1–128 bytes.
Kept records and live pages must belong to the workstream; a kept job must
be a member. A missing file or local repo gets `gone: true` when listed.

`workstream.create` accepts up to 200 initial sources, excluding kept work.
All are checked before creation, and a refusal creates no agent or collection.
Repeating a client ID returns its first workstream without rechecking source
paths, even if a file has since disappeared. Duplicate targets keep one source.
Sources never add text to the workstream's
why. A workstream holds at most 2,000 sources, proposals included; the next
addition returns `too_large`. Done workstreams permit listing but refuse
adding, updating and removing until reopened.

## Agent source proposals, Read by jobs and reading content

`propose_source` asks the person to keep or drop a source, with a reason of
1–500 bytes. A proposal is marked `added_by: agent`, `proposed: true` and
`read_by_jobs: false`. `source.update` with `keep: true` keeps it; removing it
drops it. Keeping preserves the reason and author. Adding the same proposed
target as the person also keeps it. Repeating an existing target adds nothing.

`source.update` changes the title or Read by jobs, or keeps a proposal. At
least one field is required. `keep: false` is refused, and enabling Read by
jobs on a proposal requires keeping it in the same request. A change that
alters nothing sends no event. Jobs must exclude proposals; consuming the
Read by jobs selection is the Factory plan step's responsibility.

The workstream agent reads the list with `workstream_read` (`what: sources`)
as complete JSON, up to the 2,000-source limit. It reads one source with
`what: source` and `source_id`. A regular UTF-8 text file is read within the
existing 60,000-byte answer cap, with “The file was cut.”
when cut. Binary or unreadable files get a one-line explanation. A link in the
final path component answers “The source is a link and was not read.” Links in
intermediate folders remain usable. Folders list up to 200 entry names without
following entries. Kept pages render as Markdown and records as
text; named repos, issues, links, messages and jobs remain pointers. Reading
never writes the target. These tools remain scoped to the workstream's agent.
