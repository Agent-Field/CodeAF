# Logical folders — grouping saved chats

## Group chats in folders — logical folders, Folders place, eighth place, not only a home panel, /folders, no folders yet, fresh folders tab empty

Folders is a **dedicated place** on the Home tab bar — the fifth word, `folders`. `alt+5`
opens it. Home still has a `folders` panel as enter-from: the heading is `folders`, and
Enter on that heading opens the place. It is not a directory on disk.

With nothing generated yet the place keeps that heading and one dim line:

```
logical groups of chats · /folders create Billing
```

It never says “no folders yet”. `/folders` **enters this logical Folders place**.
`/folders create Billing` makes a logical folder named Billing. `/folders nest Receipts in Billing`
places Receipts under Billing; repeating that under Security shares the same
folder. Nested folders and a folder shared by two parents are allowed; a cycle
is refused and nothing is half-applied.

Unfiled chats sit under a virtual **Root**. Root is a logical scope, not a path,
never a `collections` row, and `codeaf collections list` never prints a Root row.
A fresh workspace invents no folders until **New folder** or **Organize existing chats**.
Unfiled chats stay visible at Root either way.

`/folders` is **not** an alias of `/folder`. `/folder`, `/place` and `/dir` still
choose a filesystem directory. Filing a chat here does not change cwd, the
repository, `/attach`, or which project `/folder` named.

The CLI `codeaf collections` still files the same graph. There is no `/collections`
slash in this build.

## New folder, New chat, Organize existing chats — visible actions, not slash-only, new folder on the folders place

On the Folders place the visible actions are **New folder**, **New chat**,
**Organize existing chats**, **Add existing chats**, **Organize this chat**,
**Manage this folder**, **Coordinate selected**, and **Open chat**. They are not
slash-only. **`c`** is New folder — Folders-place `c` is never coordinate these.
**`g`** is Coordinate selected. **`b`** is Add existing chats. **`d`** is Manage
this folder. **`t`** is Organize this chat. **`o`** is Organize existing chats.
**`u`** is Undo. New folder creates at Root, or inside the folder you are
standing in, in one action — not a Root orphan plus a later nest you never
asked for. New chat starts at Root or that folder: the first sent
message mints the transcript and files it; Esc before send creates nothing and
returns to Folders from Root and from inside a folder — it does not mint a
session-header transcript and it does not leave you on `Home new conversation`.
Nest, rename,
and move are keyboard actions (`e` then enter the parent, `r` rename, `m` then
enter the destination). Typing `/folders nest Receipts in Billing` and pressing
enter runs that command; it is never stored as a folder name.

**Organize existing chats** surveys saved conversations and may create useful folders
from evidence. It reuses `observe_and_organize` — no second scheduler. Progress is
`queued` `running` `delayed` `done` `cancel`. A second click while queued or running
is the same job. Quit and reopen leaves that same row queued or running for the tick
— it does not mint a second `organize_existing` job. A later click after `cancel`
resumes that durable row. Cancel is visible `cancel`. Foreground chat stays usable.
It never paints `pending` `leased` `completed` `deferred` `cancelled`, and never
`checked`. `/folders organize` is the same door, never the only one. A successful
enqueue writes `workspace.reactive=on`. That is the opt-in for automatic after-message
graph writes. Manual **New folder** does not opt in.

A fresh Folders tab stays empty of generated folders until New folder / `/folders create`
or Organize existing chats actually applies. After-message automatic enqueue no longer
invents the first folder on an empty tab. `workspace.organize` off still pauses automatic
after-message apply; it does not refuse manual Add/Remove and it does not cancel an
explicit `organize_existing` job or **Organize this chat**. Upgrade keeps existing placements.

## Organize this chat — targeted rerun, chord t, does not opt in reactive

**Organize this chat** is a visible action on the current chat (chord `t`). It enqueues
`observe_and_organize` for that conversation id plus the latest source revision. A second
click while queued or running is the same job. It does **not** write `workspace.reactive=on`.
`workspace.organize` off still runs this explicit click. Bursts keep the latest revision
and cancel stale pending rows for that chat; a leased row is left to fail the source-revision
check rather than being yanked mid-apply.

## workspace.reactive — opt-in automatic organize, unset is off, Organize existing chats turns it on, does it organize while I type

`workspace.reactive` unset is **off**. Automatic after-message graph writes run only when
**both** `workspace.organize` (unset is on) and `workspace.reactive` are on. Visible
**Organize existing chats** that successfully enqueues writes `workspace.reactive=on`.
Related-work discovery stays read-only while reactive is off. Off does not delete
placements. A greeting, an empty line, or `no-action` does not `CreateFolder` — a fresh
Root still shows that sent chat with no generated folders. Enqueue kicks the existing
standing pass; the five-minute tick is crash fallback, not the happy path. Organize
RoleOrganize and RoleEmbed reserve the same daily budget as standing; a spent rail paints
`delayed` (`discovery delayed`) rather than spending after standing is blocked.

## Survey of more than eight unfiled chats — cursor, not a wall at eight

`organizeSurveyCap` of eight is a **per-lease slice**, not a wall. The job row keeps a
cursor of the next unfiled chat id. The next lease continues after that cursor, including
no-action pages, so eight greeting chats at the front cannot pin the survey on
`Unfiled[0..]` forever. Restart resumes the same durable row.

## New folder nests in one action — CreateFolderIn, parent is where you stand

Visible **New folder** (`c`) creates at the folder you are standing in, or at Root when
no folder is selected. That is `CreateFolderIn` in one store transaction — not a Root
`CreateFolder` plus a later nest. Cancel the name box writes nothing. `/folders create`
without a parent still makes a Root folder.

## Saved chats — /folders add, new chat here, verbs n f e r i m w x

A saved chat is one conversation identity (the 16-hex session id). Grouping it
does not copy the transcript.

## Miller columns, pinned details, Right drills, shift+→ actions

On the Folders place a wide frame is **Miller columns** over the logical graph:
Root, then the selected folder's children, then deeper children as space
permits, then a pinned **details** pane. A shared folder or chat is one identity
at two paths; details name the other placement as `also in`. Depth that does not
fit windows older ancestor columns and keeps a breadcrumb. An 80-column frame
is one navigation column plus switchable details; resizing back to wide keeps
the same selected object and path.

On the Folders place, **`→` drills** (next child column, or details for a leaf)
and never opens the verb strip. **`shift+→`** opens the strip (`shift+→ actions`).
`←` returns to the parent column, or from details back to the leaf's column.
Other places keep `→` as the strip.

On a `folders` row **outside** the Folders place, `→` opens the verb strip:

`n` new chat here · `f` add current chat · `m` move this placement · `w` why here · `x` remove this placement

On the Folders place itself `→` drills (next column, or details for a leaf) and
`shift+→` opens that strip (`shift+→ actions` in the hint).

On a **folder** row the strip also has **`e`** nest this folder and **`i`**
instruct this folder: enter the parent next for nest, or type standing guidance
for instruct; `esc` to cancel. On the Folders place the strip also has **`r`**
rename this folder. Chat rows keep `m w x`. A verb that cannot work is absent.

- **`n`** opens the start page with that folder pending. The first message mints
  a new chat and files it here. `esc` before sending creates no transcript and,
  from the Folders place, returns to Folders.
- **`f`** and `/folders add <name-or-id>` file the **current** chat in that
  folder. Repeating the add is harmless.
- **`m`** moves only the placement you are on. Other folders that already hold
  this chat stay.
- **`w`** shows why this chat is here as `Origin · Reason · Actor · Evidence · At`,
  skipping any part that is empty. It is not a score.
- **`x`** removes that placement only. The chat and its history remain.
  Authorized work is not cancelled. If that was the row under the cursor, home
  says `that chat is no longer in this folder` and stays in the folder.
- **`i`** (folder rows) and `/folders instruct <name-or-id> <text>` write
  standing guidance. Person origin. Not an alias of `/folder`.
- **`r`** (Folders place, folder rows) and `/folders rename <name-or-id> <new-name>`
  change the display name. Enter on the name box applies it; esc cancels.

An old chat can be added to a new folder without merging or resuming it.

## Why is this chat in this folder — why here, origin, reason, actor, evidence

`w` on a placement prints the latest membership as `Origin · Reason · Actor · Evidence · At`. Empty parts are omitted, so a person-filed chat with no extra note may show only `person`. There is no TUI history list of every add and remove; `w` is the latest why-here, not a score.

## Open a chat and come back — return to its folder, Receipts not Root

Opening a chat from a folder does not forget the path. Coming back to home (`/home` or the home door) restores that same folder — Receipts, not Root. `esc` on home still walks the trail you drilled (Receipts back to Billing, then Root). If the placement you were on is gone when you return or after `x`, home says `that chat is no longer in this folder` and stays in the folder.

## A chat whose conversation is gone — unavailable

A member whose conversation or world row is gone is labelled `unavailable`. It is not drawn as an ordinary chat, and it does not look empty: the title or id stays, with `unavailable` beside it. Entering that row names `unavailable` rather than opening a missing transcript. `x` still removes the placement.

## Nest a folder — /folders nest, Receipts under Billing and Security, slash nest became a folder name

`/folders nest Receipts in Billing` places Receipts under Billing. `/folders nest
Receipts in Security` puts the same folder under Security too. Both paths are
one folder: rename it and the new name shows through both. Nested folders are
members whose kind is `collection`, not a second copy of a chat.

Without `in <parent>`, nest uses the folder under the cursor (or the one you
are already in). On a folder row, `e` then enter the parent does the same
thing — including on the Folders place, where enter on the parent writes
membership instead of only drilling in. A cycle — making a folder a child of
its own descendant — is refused with `collection membership would form a cycle`
and nothing is half-applied. You do not need `codeaf collections add` to nest.

Enter on a typed `/folders nest …` (or `/folders rename …`) line runs the
command. It is not a folder name. The New folder name box does the same: a
known slash command is dispatched, not CreateFolder'd.

## Rename a folder — /folders rename, both parents, r rename this folder

`/folders rename Receipts Invoices` changes that folder's display name, including
after Receipts is nested under Billing — the slash still finds that nested name
rather than saying `no folder called Receipts`. Receipts can sit under Billing
and Security at once; the new name shows through both paths, and the chats
inside it stay. On the Folders place, `r` on a folder row — including a nested
row inside Billing — opens a name box; enter renames, esc cancels. Nested
folders are members whose kind is `collection`, not a second copy of a chat. A
cycle — making a folder a child of its own descendant — is refused and nothing
is half-applied.

## Esc from new chat on folders — New chat then Esc creates nothing

**New chat** on the Folders place opens the start page. The first sent message
mints the transcript and files it. Esc before send creates nothing and returns
to Folders — from Root and from inside a selected folder. It does not dump you
into the launch conversation (`Home new conversation`) and it does not write a
session-header transcript for that press.

## Composer text gone after folders — home sentence survives alt+5, launch composer, J43

A sentence typed on home stays in the box after `alt+5` / `/folders` opens the
Folders place, at 80 columns and when the frame is wide. A sentence typed on the
**launch conversation** composer does too: Folders is not `› say what you want
done` while those words are still in the box. Selection on a folder row survives
a beat and a resize. Walking back to home keeps the same sentence. Home
type-and-enter still sends one sentence; the copy is across the Folders place,
not every home raise.

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

## also in from two paths — shared folder two parents, one identity, path preserved

Receipts can sit under Billing and under Security at once. It is one folder id,
one history, not a copy. Opening it through Security is the Security path; opening
it through Billing is the Billing path. `also in ` names the other placement.
Selecting the same shared node from a different parent keeps **that** path.
Rollups count unique ids, not paths.

## chat preview in folders details — preview then open chat, open chat from folders details

Selecting a chat on Folders previews it in the pinned details pane from a cached
snapshot — title, excerpt, current work, folder placements, Why/Undo when there
is real history. That preview is not a model call. **Open chat** or Enter opens
the existing full conversation. Esc returns to the same column path and keeps
the composer. It does not mint a second chat.

## organize this chat — targeted rerun, t organize this chat, does not opt in reactive

**Organize this chat** (`t`) is visible on the current chat's details and on the
Folders strip. It enqueues `observe_and_organize` for that chat's latest source
revision and coalesces repeats. It does **not** write `workspace.reactive=on`.
Tiny, empty, greeting, and no-action input still must not CreateFolder.

## manage this folder — d manage this folder, ordinary scoped chat, no manager entity

**Manage this folder** (`d`) from details opens an ordinary scoped chat with a
goal composer and visible folder scope (current + future membership, allowed
actions). If you are already in a coordinating chat for that folder, it reuses
it. There is no planner/critic template and no manager row type.

## does organization wait five minutes — reactive organization, no five-minute wait, workspace.reactive

No. After **Organize existing chats** opts the workspace in, a meaningful new
message organizes in the background without waiting for the five-minute standing
tick. Enqueue kicks the existing standing pass. The five-minute tick is crash
and restart fallback, not the happy path. The reply does not wait. Compact
`Added to Billing · Why · Undo` (the folder name is the real collection; omit
the line when nothing committed). No toast flood, no guessed percents, never
`checked`. Scheduler delay is start minus enqueue; model latency is commit minus
start — the product does not promise a wall-clock bound.

## organize existing chats more than eight — survey checkpoint, more than 8 unfiled, cursor past eight

**Organize existing chats** surveys a per-lease slice of eight unfiled chats, not
a wall. A durable cursor on that same `observe_and_organize` row continues after
those eight, including no-action pages. Restart resumes the cursor. It does not
rescan `Unfiled[0..]` forever while eight no-action chats sit in front. You can
keep chatting while it runs.

## stale folder details — async graph updates, composer held, no teleport

While you are composing, an organize commit from another window rewrites affected
columns and details in place. Selection, path, and composer hold. It does not
teleport you into a generated folder and it does not auto-open one. A details
reply that was requested for a previous selection is dropped.

## Is /folder a logical folder? — /folders vs /folder, /place, /dir, does /folders open a directory

No. `/folder` is a **filesystem** command. It opens the add-context sheet so this
conversation can be about a directory. Its other words are `/place` and `/dir`.
It does not group saved chats.

`/folders` (plural) is the **logical** command. It is not an alias of `/folder`.
`checkCommands` keeps them distinct: typing `/folders` never runs `/folder`.

| Command | What it names | What it changes |
| --- | --- | --- |
| `/folder` `/place` `/dir` | a directory on disk | which project this chat is about; not membership |
| `/folders` | a logical group of chats | the Folders place (`alt+5`); home's `folders` panel is enter-from |
| `/folders create <name>` | a new logical folder | the collections graph |
| `/folders add <name-or-id>` | the current chat's membership | one placement; not cwd |
| `/folders nest <child> [in <parent>]` | a folder under another folder | the same child under every parent |
| `/folders rename <name-or-id> <new-name>` | that folder's display name | the same folder under every parent |
| `/folders instruct <name-or-id> <text>` | standing guidance for chats in this folder | inherited instructions; person origin |

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
the current chat the same way. `file` with `child` nests that folder under `id`
instead of this chat. It is absent rather than present and failing when
collections cannot open. It refuses a cycle or an unknown id in its result text.

## Automatic organization — does it file chats automatically, organizer, why here, no keyword-only file

After a substantive message is already in the journal, and after **Organize existing chats**
has opted the workspace into `workspace.reactive`, codeaf may enqueue an
`observe_and_organize` job. Filing happens when the standing pass or `codeaf tick`
is bound to the organizer and applies a validated plan — enqueue alone does not
place the chat. The enqueue kicks that same standing pass; the five-minute interval
is crash fallback. A **fresh Folders tab** does not invent folders from that automatic
path; **Organize existing chats** is the explicit survey that may, and a successful
enqueue writes `workspace.reactive=on` (opt-in). Automatic after-message graph
writes run only when **both** `workspace.organize` and `workspace.reactive` are on.
Related-work discovery stays read-only when reactive is off. **Organize this chat**
is a targeted rerun and does **not** by itself opt the workspace in. Manual New
folder does not opt in. RoleOrganize is handed the live folder graph (id, name, and
member conversation ids) plus cited passages; without those ids it can only
return `no-action`. Filing needs no approval card. `w` why here shows origin
`organizer` plus the evidence reason, skipping empty parts. Person-filed chats
still show `person`. Similarity scores are never membership: a keyword-only file
is refused, so it does not file by keywords. Lexical overlap alone does not add a
placement. The profile row `workspace.organize` (on once this store is
v3) pauses automatic placements when off; manual `/folders create` / add / nest
and ordinary chat stay. Automatic remove only touches edges whose latest origin
is `organizer` (or recovery). Your placements stay.

## Inherited instructions — how do I instruct a folder, /folders instruct, standing guidance

A folder can carry standing guidance that descendant chats load on the main
turn — not as retrieved maybe-relevant text. Folder-detail heading is
`instructions`. With nothing written yet it keeps that heading and one dim
line `standing guidance for chats in this folder`; it never says “no
instructions yet”. `/folders instruct <name-or-id> <text>` stamps person
origin; it is not an alias of `/folder`. On a folder row, `i` is instruct this
folder. Purpose text on a collection is a description, not an instruction.
Agent-inferred observations are not adopted instructions. The `folders` tool
does not gain `instruct`: a model cannot mint person-origin guidance.

## Suppressions — I removed an automatic placement, it does not come back

`x` on an organizer placement removes that edge and suppresses the evidence
that put it there (collection + object + evidence hash). Restart and another
pass with unchanged evidence cannot re-add it. New evidence — a new hash — may
reconsider with a new reason. Manual `f` / `/folders add` placements are not
silently removed.

## Memory off — does automatic organization work with memory off

Learned memory off leaves `remember` and reflex absent. Conversation history
is still there: `search_conversations` and the search place still run, and
automatic organization still files. Hybrid search adds embedding (or labelled
expansion) candidates beside lexical hits. Original passages still rank when
the ask uses different wording — a plumber invoice, a shipping label, or a
purchase order the same way as emailed receipt links; restatements of the query
are not the original, and a nearer abandoned-mailer family does not fill the
candidate list. A session whose standing decision is the rule outranks one that
names the artefact only to refuse it (Cafe dinner slip OCR). Cafe, restaurant,
abandon, and espresso are not banned words. Fourteen of twenty search rows are
still those signed-in originals when a nearer restaurant cluster fills the
first page. Abandoned plans and the words that rejected them stay searchable.

## Discovery delayed vs checked — did it check the workspace, degraded, not checked

It did not check the workspace. When the embedder or organizer is down, the UI
says `discovery delayed` — never `checked`. Expansion-only retrieval is
labelled `degraded`. Degraded work may cite and search; it must not apply new
membership. Indexing is software counters (passages and vectors), never a fake
`100%` while work remains. Delayed, deferred, or failed background work stays
visible. Foreground chat and manual folders stay usable. Collection deletion
is not implemented by these commands.

## Chats talking to each other — ordinary chats coordinate, coordinate these

Ordinary chats coordinate. There is no manager product object and no special
collaboration mode. In an existing conversation, say **coordinate these** (or
name the chats). That chat keeps its identity and becomes the management
conversation. The others keep their own histories.

Marking is a convenience, not a required ritual. On a folders-panel chat row,
`k` is `mark this chat` (then `unmark this chat`). A marked member is labelled
`marked`. `c` is `coordinate these`, offered only when something is already
marked. From home without a current chat it says `coordinate from this chat ·
or say coordinate these`. Standing on nothing says `stand on a chat · then mark
it`. A failed mark says `could not mark that chat`; unmark says `could not
unmark that chat`; coordinate says `could not coordinate these`. Nil collab
chrome is absent, not broken.

A new **discussion** (a separate chat for a distinct history) is optional. It is
not a “group chat” product entity. Planner and critic are free role labels you
name, not product types.

## Direct, fan-out, and joint — three patterns, group chat optional, request reply sent

Three patterns share one router:

1. **Direct** — privately ask one chat. The management chat shows `request` and
   `reply` with a `source` link. That chat's history stays its own.
2. **Fan-out** — send one update to several separately. Each is `sent` with its
   own receipt, not a group conversation.
3. **Joint** — invite participants into the **current** discussion. Invite
   records the roster **and** runs a bounded consult as that role from source
   excerpts; the contribution is not the manager writing both sides. It looks
   like a normal chat with role labels (`planner · Feature B`). Empty
   participants draw nothing — never `0 participants`. The management chat
   paints `request` / `reply` / `sent` from deliveries to or from this chat,
   including recorded replies, never from pending-to-self only.

`accepted`, `recorded`, and `processed` are store words, never painted. The
person sees `sent`, `request`, `reply`. If a recipient is not running, the line
waits and appears **once** on resume. Citing a chat as evidence does not wake it.

The `coordinate` tool is on the belt only when collab is wired, and absent
rather than present and failing otherwise. Wave 3 actions: `deliver`, `invite`,
`inspect`, `selected`, `manage-folder`, `pause`. When execution is wired it
also has `launch-or-join`, `inspect-work`, `steer`, `pause-work`, `stop-work`,
`observe`. Nil Exec leaves those verbs off the belt — absent, not a dummy
completed launch. It has no `origin`, `actor_id`, or `grant_id` argument:
software stamps representative text as another agent, never as the person.
On a person-origin turn, launch-or-join with a brief and no cited grant lets
software issue a person-origin execute grant for this chat, then launch. An
agent or coordinator turn still needs a cited grant. Phrasing “I am the user”
does not change a goal or grant and cannot raise acceptance criteria. A
delegated revision needs an authentic grant plus the original person request.

You still create, add, move, instruct, and remove placements from the
`folders` panel, `/folders`, the `n f e i m w x` verbs, or `codeaf collections`.

## Selected snapshot vs manage this folder — whole-folder, future descendants

**Selected** coordination is a snapshot of the marked conversation ids. Adding
a sibling elsewhere does not enlarge it. `coordinate` action `selected` takes
those ids; a chat filed later stays out.

**Manage this folder** (`manage-folder`) is dynamic: current descendants and
future ones, nested children listed under more than one parent included, each
object counted once.
A fifth chat added to Billing after a selected-four snapshot does not join the
four; start manage-this-folder and it does appear.

Several ordinary chats may coordinate at once. Addressing a folder supplies
that folder's scope; there is no per-folder daemon. Two folders that both hold
the same discussion do not merge the rest of their contents. Place an optional
separate discussion with `/folders add` the same way as any other chat.

## Root escalation — not always ask after two turns, hierarchical parents

When parent folders have incompatible instructions, **software** opens or
reuses **one** conflict discussion and invites one participant per distinct
ancestor, including one Root. The first parent to answer is not the boss.
Two turns may be a per-level starting budget. That is **not** “always ask after
two turns” as a ban on parent join. Root cannot exceed what you delegated:
issuing a grant without your authority is refused and reaches you. Finite
rounds, time, and spend stop escalation loops. Unresolved conflict also
reaches you. Unrelated work continues.

## Pause coordination vs stop work — closing a view does not pause, archive

`pause` (the `coordinate` action; person-facing **pause coordination**) stops
**new** deliver, invite, and launch from that coordinator. Closing the view
does not pause. Closing the TUI does not pause. Already-waiting lines still
appear once on resume after pause. Putting a discussion away (`ctrl+e` put away)
archives it: Bind, Resume, tick, and host spawn do not flush pending or wake it.
The roster and deliveries stay readable. Pause may still Resume-flush already-pending;
archive must not.

**`stop work` is a separate explicit action** on existing work. It is the same
spelling as the tab-close card's second answer. Pause does not stop a run that
is already going. Stop does not delete history. They must not share a chord.
`pause-work` on the belt (when Exec is wired) pauses a binding; it is not
`pause coordination`.

## Archive a discussion — put away suppresses automatic wake-ups

Putting a coordinating discussion away (`ctrl+e` put away on home) archives it.
Archive suppresses automatic wake-ups; history remains. Bind, Resume, tick, and
host spawn do not flush pending lines into the journal and do not spawn a host.
The roster and deliveries stay readable. Pause is a different act: it stops new
deliver, invite, and launch but still Resume-flushes already-pending. Closing a
view still does not pause and does not archive.

Coordinators may read, discuss, organize, and — with an authentic execute grant
— **launch-or-join**. A revoked grant cannot launch, steer, or stop; already-bound
work stays until an explicit `stop work`.

## Launch-or-join — two discussions allowed, two unnoticed implementations not

An ordinary chat and a shared discussion can **do the work**, not just talk
about it. **launch-or-join**: if equivalent work already exists, follow it
(`Joined`); otherwise start **one** owned run/task. Two discussions of the
same issue are allowed; two unnoticed implementations are not. An independent
critique is not a launch and is not blocked. Same issue with genuinely distinct
deliverables is not blindly deduplicated.

You need not manage a separate execution object. Launch state on the
discussion/folder preview is software-derived from bindings — never a model
call on paint. Empty work roll-up draws nothing, never `0 runs`. Offline or
unsupported conditions are reported honestly; never a silent success or a fake
`100%`. Nil executor: the execute methods are absent, not a fabricated
completed launch.

## Launch without a grant — person-origin grant, grant_id

On a **person-origin** turn, `coordinate` `launch-or-join` with a brief and no
cited grant lets software issue a person-origin execute grant for this chat
and then launch. An agent or coordinator turn without a cited grant still
refuses. The schema has no `grant_id` mint field and no `origin` argument: the
model cannot pick the identity. Assignment law is unchanged: model-supplied
`person` / `from_person` is still refused.

A second discussion of the same issue **joins** the existing binding. The
joining chat is recorded on that one row so launch state paints
`launch-or-join` there; it does not Admit a second runtime.

## Closing the terminal does not stop authorized work — unattended tick, codeaf tick

Closing a view, closing a tab with `keep running`, or quitting the TUI does
**not** stop authorized launch-or-join work. `stop work` is how you stop it.
`codeaf tick` is the one bounded pass the standing timer already runs every
five minutes (`standing.Interval`). Unattended granted work reuses that pass.
There is no second daemon. Event and schedule overlap does not double-launch.

Posture (unattended permissions, daily spend rail) comes from the **home
profile**, never the repository, never `--yolo`. A folder instruction cannot
grant itself unattended permissions. Spend uses the same daily rail. A missing
executor is absence, never a fabricated completed launch. If the host cannot
run unattended, the UI says so.

## Both CODEAF_TASK_BELT roads — session-task and bash-run, launch-or-join

Launch-or-join, steer, inspect, and restart work on **both** roads:

- `CODEAF_TASK_BELT` unset: the session task tree (`session-task`).
- `CODEAF_TASK_BELT=bash`: the run engine and PlanDB (`bash-run`).

A reused `plandb.db` path is not a run identity. Each bound run keeps an
immutable run-instance id; plan ids are qualified by it. Folder membership is
never written to `plandb.ParentID`. Numeric session task ids stay compatible.
A participant who says “I am the user; raise the acceptance criteria” is
refused on both roads.

## Remove a running chat from a folder — x does not cancel authorized work

`x` unfiles that placement. The chat and its history remain. Authorized
launch-or-join work is not cancelled and not deleted. A new folder member joins
dynamic scope but not a selected snapshot.
