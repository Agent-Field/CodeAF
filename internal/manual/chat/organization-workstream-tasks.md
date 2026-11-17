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

## tick a person task, hand a task to someone, move it to another workstream, send it back

- Tick: only the owner ticks a person's task ("only the task's owner ticks it" otherwise). Ticking it unblocks the tasks that were waiting only on it: a person's task loses its "waiting on", and an Afi or chat task stays in Up next with the line "Ready to start: <the ticked task> is done" until it is started (see *Start — why a task stays in Up next*).
- Hand to someone: anyone who can edit the workstream changes a task's owner to another person or to Everyone. The thread says "Handed to Marco", and Marco's Inbox row says "handed to you by Priya". Taking a task yourself is not a hand-over and says nothing.
- Move: More › Move moves a task, with its whole conversation, to another workstream you can edit. Tasks that waited on it here stop waiting. Only the task's owner or whoever looks after the workstream moves it.
- Send back: a done task's More has "Send back…". It asks why, then puts the task back in Up next. Anyone who can edit may send work back, a reviewer included. The thread, the task and the doer's Inbox row read "Sent back by Priya: <why>". An Afi or chat task sent back starts again with the reason. A new goal is a new task.

Reading tasks needs view on the workstream; adding, commenting, handing on and sending back need edit.

## who can stop or move a task — the owner or whoever looks after the workstream

Stopping a task or moving it to another workstream is for the task's owner and for whoever looks after the workstream (its steward, or anyone with full access to it). Anyone else who can edit is refused with "only the task's owner or whoever looks after this workstream can stop or move it", and the desk leaves Stop and Move to another workstream out of their More. They can still comment, hand it to someone or send it back. Answering a task's question and ticking a person's task stay the owner's alone, whoever looks after the workstream.

## waiting on — the only way to order tasks, no circles

A task can wait on other tasks of the same workstream. That is the only ordering tool: no start dates. A task cannot wait on itself, on a task of another workstream, or in a circle ("that would make tasks wait on each other in a circle"). Only the owner changes what a task waits on and its due date.

## Start all — five or more tasks from one request wait in Up next

When one request makes five or more tasks, they land in Up next under one heading with Start all. Start all wakes the doer of every Afi or chat task of that heading that waits on nothing unfinished, then shows those tasks Running; the rest stay in Up next until they can start. Person tasks are not started (the person does them) and Factory tasks start at the Factory. On an organization's workstream Start all starts the heading's Afi tasks on the organization's server; a chat's tasks stay in Up next, because a chat is woken on its person's computer.

## a Factory job is a task — its state, its question, never copied

Every Factory job in the workstream appears in Tasks as a task done by the Factory, under the Factory heading. It is the job itself, read again each time, never a copy. Its state is the job's in the app's words (Failed, Interrupted, Stopped by you, Didn't report back), its question is the job's open question, and its facts line says which step it is on ("Step 4 of 4, Review"). Answering its question answers the job, under the job's own rules.

You can hand it to someone, give it a due date or a heading, or make other tasks wait on it. You cannot set its state by hand: "a Factory task's state is its job's: stop, start or answer the job instead".

## comments on a task, Afi's offer, Add it, undo, a private request noted on the task

A task's Activity and comments is one thread everyone in the workstream sees: what was done, people's comments, and answers. Comments go to people. Afi never acts on a comment; it may offer under one ("Afi can add this to the draft · Add it"). Anyone may press Add it; the owner is told and can undo for 5 seconds, after which "the five seconds to undo this have passed".

A private conversation with Afi stays private. If something you ask Afi there changes shared work, the task gets one line ("Priya asked Afi to keep it under 300 words"), never the conversation. "Post this to the task" adds Afi's answer to the thread, marked as posted by you.

## send a request to another team's workstream — ask another workstream for work, one you are not in

Work for another team is a request into their workstream, not a task in yours. On a task, More › "Send a request to" lists every workstream in the organization you may ask: the ones you are in, and every one that is not private, each with who receives it. Pick one, write what you want (the first line is its title), and press "Send request".

It becomes a task in that workstream, owned by its owner of incoming work (People and rules), who also does it, or done by Afi when that owner is Everyone. It waits in Up next until they start it. Its page says "From" you and your workstream, and its thread starts "Requested by Priya from Marketing". Your task gains one line, "Sent a request to Engineering: <title>". If you are not in that workstream you see only that it was sent and who owns it, never its other work. A private workstream you are not in cannot be asked, and an archived one is not listed.

Afi never sends a request on its own: asked about another workstream's work, it names that workstream and leaves the request to you. A Personal workstream has nobody else to ask, so the item is absent there.

## @name in a comment — who a comment reaches, mention someone, Comments for you

There is no chat between people; a comment on a task is how you say something about it. A comment reaches the task's owner, and anyone it names with @: "@abir", "@Abir" or "@AbirDas" all reach Abir. Only people in the workstream (named on it, or in one of its teams) can be reached; an @ that names nobody there, or one inside a word such as an email address, reaches nobody and the comment is still kept.

A comment that reaches you shows in your Inbox under "Comments for you", with the newest comment's words and who wrote it, until you open the task. Opening it there or in Tasks marks it read; a new comment makes it unread again. Your own comments never reach you, and comments older than two weeks are not counted. Afi reading a task never marks it read for you.

This works in an organization workstream. A Personal workstream has only you in it, so nothing there is for anyone else.

## someone left the workstream — who gets their tasks, handed to the fallback

When a person is removed from an organization workstream, each of their unfinished tasks there (and in every workstream inside it) is handed to the workstream's fallback person ("If unanswered for a day"), or to the person who looks after the workstream when there is none. The thread says "Handed to mia". Done tasks keep who did them, and a person's own checkbox task moves with the hand-off. Anyone who can edit can hand a task on again by hand.

## a file changed while a task was running — carry on or start again

When someone changes the contents of a file that a running task's work read, that task stops and asks its owner "Layout changed while this ran. Carry on?", with "Carry on with the new version" and "Start again from the new version". Only the owner answers, and either answer starts the task again. A rename alone flags nothing, and a task that is not running is left alone. A Factory task cannot be stopped this way, because its state belongs to its job: its thread only gains the line "Layout changed while this ran".

## does a comment reopen a done task? — no, a comment never changes a task

A comment or a posted answer on a task never changes its state. Writing a receipt or a piece of news on a finished task leaves it done, and nothing waiting on it is blocked again. To reopen work on purpose, use Send back on the task's More; the owner of a person's task can also untick it.

## Start — why a task Afi or a chat does stays in Up next, Mark done

A task that Afi or a chat does is Running only while something is actually doing it. When what it waits on is done, or when Start all is pressed, it stays in Up next until its doer is woken. On a Personal workstream the task page shows **Start**: it hands the task to Afi's conversation (or to the chat that does it) and only then shows Running; Start all wakes each doer once with all of its tasks. On an organization's workstream **Start** runs an Afi task on the organization's server, as the person who pressed it and with their rights, in a run of its own that is not anybody's conversation with Afi: what it did shows in the task's thread and results, and the task ends done or failed with the reason. If the server cannot run Afi (no model key from the organization) Start says so and the task stays in Up next. A chat's task cannot be started from an organization's workstream; it starts from that chat. In every case the task's owner can finish it by hand with **Mark done** in its More, or stop it with Stop.

## due date — pick any day; change it on the task

A task can carry one due date, any day at all. Add task's Due opens a month with Today, Tomorrow and In a week above it; the arrows on the month move between months, and the keyboard moves a day or a week with the arrows and a month with Page Up and Page Down. On the task page its owner presses the due date in the line of facts, or uses More › "Add a due date…", to change or clear it. A missed date reads "Due Nov 21 · 2 days late".

## set what a task waits on — More › Waiting on

On the task page, its owner opens More › Waiting on and ticks the tasks it should wait on (or unticks one to stop waiting). The menu lists what it already waits on, then the workstream's open tasks, newest first; a task that would make a circle is never offered, and the Factory's own issues stay out of it. The facts line then reads "Waiting on: …", and the task that waits shows "Then: …" on the other.
