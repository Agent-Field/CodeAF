# Conversation and tasks: product architecture and prototype

Status: interaction prototype, approved direction from September 5, 2026. The
runtime changes below are proposals. The prototype does not execute models, Git,
commands, or network requests. Existing runtime fixes remain on PR #653.

## Product boundary

The main conversation holds discussion, decisions and continuity. A task holds a
recognizable outcome, its own conversation, and the work needed to deliver it.
Both use the same agent core. The user addresses work, not an agent topology.

Ordinary work may stay inline. Create a task when an assignment benefits from
sustained execution, a separate workspace or a separately reviewable result.
Size, number of files and elapsed tokens alone are insufficient. A user can
explicitly choose “Run as task.” Automatic handoff needs a visible receipt naming
the task and what moved, and must continue the existing assignment without
repeating completed work. The automatic policy is not simulated in this prototype.

Opening a task changes the view, not who owns execution. Leaving it, hiding the
rail or closing a client does not cancel work. Persistence requires the execution
host to remain running; local work cannot continue while the machine is off.

## One durable identity, several views

Reuse the existing task identity, request revisions, journal, delivery receipts,
check declarations and result references. Do not create another task database or
rename every internal child node into a user-visible task.

| Concern | Authority | UI projection |
| --- | --- | --- |
| Conversation | Existing conversation ID and journal | Main discussion and its task list |
| Task | Existing stable task reference, owning conversation/project | Named outcome and task conversation |
| Assignment | Current request/revision, scope, checks, workspace and requested delivery | Inspectable assignment; acknowledged directions |
| Execution | Existing attempt/lifecycle and internal work graph | Running phase, dependencies and collapsed activity |
| Result | Artifact location, source revision, check evidence and unresolved issues | Reviewable result with precise delivery status |
| View state | Client state keyed by conversation/task reference | Recipient, draft, scroll anchor, expanded entries |

A task can use internal helpers. Their events contribute to the task's activity
and aggregate state. They become separate top-level tasks only when the user
commissions a separate outcome or explicitly splits the assignment. Preserve the
internal graph for cancellation, dependencies, resource ownership and diagnostics;
change its presentation rather than deleting its semantics.

## Messages have a recipient and a receipt

Opening a task pins its composer to that task. From the main conversation, an
explicit recipient selection can send a direction without changing views. Natural
language routing must name its interpreted recipient in the acknowledgement. If
several tasks match, retain the unsent message and clarify that recipient only.

Do not broadcast a casual sentence to every task. Do not treat discussion as a
scope revision automatically. A material direction advances the existing request
revision, invalidates stale acceptance evidence, and reaches the executing agent
at a safe boundary. A question can remain a question in the task conversation.

The transport must distinguish pending send, received and applied. An optimistic
bubble is not acknowledgement. Retries carry a message ID; reconnect replays
unacknowledged sends and events after the client's cursor, with deduplication.
A failed read keeps the last content and an explicit retry action. Opening a task
from another window must address the same durable task, including when it is idle.

Drafts and scroll anchors are keyed by recipient/view, not stored in one shared
composer variable. Escape returns to the main conversation, preserving both
views' drafts. Navigation never discards text or stops execution.

## Keep the conversation useful

Progress updates replace the existing task row. Tool output and internal helper
transcripts remain inside the task. Main context gets a bounded current task
summary and evidence references; detailed history is read on demand.

Questions and results have stable notification identities. Reconnect cannot
announce a completion twice. A finished piece does not finish the whole request.
The main assistant still owes the original outcome, including requested synthesis
or integration, and can consult task results when continuing that request.

A background event must not seize keyboard focus or replace a draft. Show a task
needing input in the rail and one actionable notice. Answering it in either view
resolves that same question. Batch unrelated completions rather than generating a
new main-model response for every internal event. Do not report a stale result
as satisfying a newer direction.

## State and delivery are separate facts

User states: Running, Needs your input, Done, Stopped, and Interrupted. Checking
is a running phase. A provider wait remains running with a truthful explanation;
a terminal/provider failure offers recovery with retained work.

“Done” means the requested outcome is delivered in the requested form. A branch
and commit can be a complete result while main stays unchanged. A work product
that lacks required evidence should say what remains, rather than merely display
a success color. Opening or reading a result does not accept, merge or publish it.
Those operations remain bounded by the user's request and existing permissions.

Stop applies to the task's execution, owned commands and internal helpers. It
preserves their partial work. Stopping a main response does not stop all tasks.
An explicit “Stop all work” action would require scope to be visible. Continuing
a completed outcome uses a new execution attempt/revision and retains earlier
results; an unrelated outcome becomes a new task.

Concurrent writes remain isolated or serialized by the existing workspace rules.
A calm UI cannot conceal two tasks writing incompatible changes to one checkout.

## Prototype scope

The inline fragment at `prototype/conversation-tasks.html` demonstrates:

- Main conversation and a flat list of outcome-based tasks.
- Opening and returning, including Escape, with per-recipient drafts.
- Direct task messages and explicit routing from the main composer.
- Starting a task without leaving the main conversation.
- A running task, a decision that pauses it, acknowledgement and continuation.
- Stop/resume and a completed result with its branch, changes and checks.
- Collapsed activity, responsive task navigation and keyboard controls.

“Advance demo task” produces deterministic simulated background events. The host's
optional design controls compare a right rail and a compact task strip. All task
content, checks and artifacts are illustrative. Sending text uses a local receipt,
not an AI response or semantic intent classifier. Client reconnection, durable
storage, scrolling restoration, old result history, provider recovery and real
work execution remain outside this prototype.

## Implementation order and proof

1. Introduce a task presentation model over existing events and IDs. Reuse task
   opening, message delivery and stored results. Keep the current executor.
2. Make recipient and per-view draft state explicit. Verify mouse and keyboard
   navigation through the actual local and remote terminal connections.
3. Project progress into task rows; deduplicate questions and result notices.
   Verify reconnect, lost acknowledgement, late completion and revised requests.
4. Only then replace file-count handoffs with the current capable agent choosing
   the existing task tool. Retain cancellation and premature-completion regressions.

Acceptance is a substantial repository issue while the user keeps discussing
another topic. The user opens and steers the task, returns with their draft intact,
answers one question from the main view, reconnects, stops/resumes work and receives
one accurately located result. Required outcomes: no wrong-recipient sends, no
lost directions or drafts, no orphaned execution, no duplicate completion and no
unrequested merge. Grade the resulting code independently, including public type
contracts; a pleasant task flow cannot substitute for a correct result.
