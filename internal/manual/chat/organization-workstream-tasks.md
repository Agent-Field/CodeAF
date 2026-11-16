# Tasks in a workstream — who owns a task, who answers its question, ticking, waiting on, Factory jobs as tasks

## what a task is — one piece of work, one owner, done by Afi, a chat, the Factory or a person

A workstream's Tasks tab is the shared list of its work; everyone in the workstream sees the same list. A task is one piece of work with ONE owner. Who does it varies: Afi, a chat, the Factory, or a person (a person's task is just a checkbox). Who answers for it does not.

A task has a title, a few lines saying what is wanted (the request), its owner, who does it, where it came from (a chat, a person, the Factory, a standing order), its state, what it waits on, an optional due date, an optional heading that groups it, and the files or links it made. There are no sub-tasks, priorities, estimates or custom fields.

The list's sections are always in this order: Waiting on someone, Running, Up next, Done. Unfinished work keeps the app's words: Failed (with its reason), Interrupted, Stopped by you, Didn't report back. A heading such as "Release 2.4" or "Factory" sits in the most urgent section any of its tasks is in, with all its tasks under it.

A Personal workstream keeps its tasks on this computer; an organization workstream keeps them on the organization's server. They behave the same.

## who can answer a task's question — only the owner, Everyone means the first answer, the fallback after a day

When a task needs a choice or an approval, it asks its owner and shows as "Needs Priya". Only the owner sees buttons on the question; anyone else sees who answers and may comment. Someone else pressing answer is refused with "only the task's owner answers its question".

If the owner is Everyone, anyone who can edit the workstream may answer; the first answer decides, a second is refused with "this question was already answered", and the task records who answered. A question left unanswered for a day also goes to the workstream's fallback person, if it has one.

Who owns a new task: whoever asked for it. Work nobody asked for directly (from the Factory or a standing order) goes to the workstream's default owner.

## tick a person task, hand a task to someone, move it to another workstream, reopen it

- Tick: only the owner ticks a person's task ("only the task's owner ticks it" otherwise). Ticking it starts the tasks that were waiting only on it.
- Hand to someone: anyone who can edit the workstream changes a task's owner to another person or to Everyone. The thread says "Handed to Marco".
- Move: More › Move moves a task, with its whole conversation, to another workstream you can edit. Tasks that waited on it here stop waiting.
- Reopen: replying on a done task reopens it, so the history stays one thread. A new goal is a new task.

Reading tasks needs view on the workstream; adding, commenting, handing on and moving need edit.

## waiting on — the only way to order tasks, no circles

A task can wait on other tasks of the same workstream. That is the only ordering tool: no start dates. A task cannot wait on itself, on a task of another workstream, or in a circle ("that would make tasks wait on each other in a circle"). Only the owner changes what a task waits on and its due date.

## Start all — five or more tasks from one request wait in Up next

When one request makes five or more tasks, they land in Up next under one heading with Start all. Start all starts every task of that heading that waits on nothing unfinished; the rest start as what they wait on finishes. Person tasks are not started (the person does them) and Factory tasks start at the Factory.

## a Factory job is a task — its state, its question, never copied

Every Factory job in the workstream appears in Tasks as a task done by the Factory, under the Factory heading. It is the job itself, read again each time, never a copy. Its state is the job's in the app's words (Failed, Interrupted, Stopped by you, Didn't report back), its question is the job's open question, and its facts line says which step it is on ("Step 4 of 4, Review"). Answering its question answers the job, under the job's own rules.

You can hand it to someone, give it a due date or a heading, or make other tasks wait on it. You cannot set its state by hand: "a Factory task's state is its job's: stop, start or answer the job instead".

## comments on a task, Afi's offer, Add it, undo, a private request noted on the task

A task's Activity and comments is one thread everyone in the workstream sees: what was done, people's comments, and answers. Comments go to people. Afi never acts on a comment; it may offer under one ("Afi can add this to the draft · Add it"). Anyone may press Add it; the owner is told and can undo for 5 seconds, after which "the five seconds to undo this have passed".

A private conversation with Afi stays private. If something you ask Afi there changes shared work, the task gets one line ("Priya asked Afi to keep it under 300 words"), never the conversation. "Post this to the task" adds Afi's answer to the thread, marked as posted by you.

## someone left the workstream — who gets their tasks, handed to the fallback

When a person is removed from an organization workstream, each of their unfinished tasks there (and in every workstream inside it) is handed to the workstream's fallback person ("If unanswered for a day"), or to the person who looks after the workstream when there is none. The thread says "Handed to mia". Done tasks keep who did them, and a person's own checkbox task moves with the hand-off. Anyone who can edit can hand a task on again by hand.

## a file changed while a task was running — carry on or start again

When someone changes the contents of a file that a running task's work read, that task stops and asks its owner "Layout changed while this ran. Carry on?", with "Carry on with the new version" and "Start again from the new version". Only the owner answers, and either answer starts the task again. A rename alone flags nothing, and a task that is not running is left alone. A Factory task cannot be stopped this way, because its state belongs to its job: its thread only gains the line "Layout changed while this ran".
