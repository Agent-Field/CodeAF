# Logical data shapes and persistence

2026-09-09. The person asked for a mathematical/computer-engineering explanation
of what folders and chats contain and whether that informs database choice.
These are simplified logical shapes derived from current code, not replacement
Go structs or a new storage design. Identity/address fields are omitted here.

[Visual record-and-reference diagram](DATA-MODEL.svg) shows the same model in
four panels: folder membership, chat-owned data, context revisions, and ongoing
work/artifacts. Sequence arrows express order; reference arrows are labeled.
The storage strip describes current ownership, not a proposed migration.

```text
Member = Reference<Folder | Chat | Task | OngoingItem | Artifact>
Folder = (name, ordered unique members)

Chat = (ordered journal, session metadata/state, owned task graph)
TaskGraph = (tasks, dependency relation, delegation-parent relation)
Task = (brief, acceptance, state, own journal, result/output references)

OngoingItem = (origin, original words, wake condition, action,
               grant, limits, current state, retained activity)
Artifact = (path reference, current availability)

Context = (immutable revision sequence, current revision)
Revision = (title, text, source, ordered explicit targets, withdrawn)
Source = Reference<Chat | Task | Artifact>
Target = Member
```

“Contains” has different meanings: a chat owns its journal, whereas a collection
lists references to independently owned records. Chats' filesystem/session
locations also differ from logical collection membership. Reference reuse does
not duplicate content or change the original owner.

Let F be folders and U the possible member records. Membership is a relation
M ⊆ F × U with an ordering for each folder. Duplicate membership within one folder
is refused. Its folder-only portion is acyclic, permits multiple parents and is
therefore more general than a tree. A chat with zero incoming membership edges
is valid. A shared chat has more than one incoming membership edge.

The other relations remain typed and independent: task ownership, task dependency,
delegation parent, ongoing origin, context source and context target. TaskNode's
`parent` is specifically not its `dependsOn` edge. Shared context is not itself
an allowed collection member/reference kind; it has a separate revisioned store
and targets the allowed records. Context selection by explicit targets does not
imply accepted authority or execution consumption.

Chat journals are ordered histories of journaled messages, tool interactions and
markers, with a shared reader that also reconstructs retained earlier history
around compaction. They are not unordered graph-node bags. Likewise revision
history has order; work has mutable current state; files have payloads. A graph
view can expose references between these structures without making every journal
entry, content part or state field a new globally managed node.

## Database implication

A logical graph does not by itself require a graph database. Current collection
and context stores already use SQLite tables for collections, memberships,
contexts, context revisions and revision-specific targets. The current session
engine separately owns JSONL journals and task checkpoints; standing owns item
JSON and retained activity/ledger files; artifacts remain files.

Retain those boundaries for this grooming wave. A unified read view should ask
existing owners for their state rather than create another authoritative graph
copy. Indexing and query shape can be optimized independently. SQLite supports
recursive queries of trees and graphs: https://www.sqlite.org/lang_with.html .
No measured bottleneck or multi-host transactional requirement has been supplied
that warrants changing the database in this discussion.

An evidence relation that was never retained cannot be recovered simply by moving
the existing records to another database. Conversely, task dependencies already
exist within TaskGraph even though the simple folder/context viewer did not expose
them. Viewer coverage, retained evidence and database capability are three distinct
questions.

Sources inspected: internal/workspace/{workspace.go,store.go,context.go};
internal/session/{world.go,transcriptread.go,session.go,task_run.go,task_store.go};
internal/standing/standing.go. All current-code observations refer to the backend
review worktree. This note confirms no schema migration or new primitive.

Next: enumerate concrete reads and writes needed by the product and their
consistency requirements. Use those to determine missing fields/relations or
indexes before considering another database or universal relation type.
