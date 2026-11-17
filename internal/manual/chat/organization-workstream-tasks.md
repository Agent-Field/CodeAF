# Tasks in a workstream — who owns a task, who answers its question, ticking, waiting on, Factory jobs as tasks

## what a task is — one piece of work, one owner, done by Afi, a chat, the Factory or a person

A workstream's Tasks tab is the shared list of its work; everyone in the workstream sees the same list. A task is one piece of work with ONE owner. Who does it varies: Afi, a chat, the Factory, or a person (a person's task is just a checkbox). Who answers for it does not.

A task has a title, a few lines saying what is wanted (the request), its owner, who does it, where it came from (a chat, a person, the Factory, a standing order), its state, what it waits on, an optional due date, an optional heading that groups it, and the files or links it made. There are no sub-tasks, priorities, estimates or custom fields.

The list's sections are always in this order: Waiting on someone, Running, Up next, Done. Work that ended without finishing sits under Waiting on someone, in the app's words: Failed (with its reason), Interrupted, Stopped by you (to whoever pressed Stop; everyone else reads "Stopped by Marco"), Didn't report back. A heading such as "Release 2.4" or "Factory" sits in the most urgent section any of its tasks is in, with all its tasks under it.

## open a task — in place in Tasks, back to the list, Open as its own tab

A click on a task opens it in place, inside the Tasks tab: the workstream's header and tabs stay, and a bar over the page shows the task's name with "‹ Tasks" to go back, "Open as its own tab", and the task's own More (⋯). Escape, or pressing the Tasks tab again, also goes back, to the same place in the list. A task opened from the Inbox, from a file's "From" line or from a chat's list of what it made opens the same way, in its workstream's Tasks tab.

"Open as its own tab" (or Command-click on a row) shows the task alone in the window's one preview tab: opening the next task replaces it, unless you pinned it or worked in it. A chat keeps its own tab.

A Personal workstream keeps its tasks on this computer; an organization workstream keeps them on the organization's server. They behave the same.

## who can answer a task's question — only the owner, Everyone means the first answer, the fallback after a day

When a task needs a choice or an approval, it asks its owner and shows as "Needs Priya". Only the owner sees buttons on the question; anyone else sees who answers and may comment. Someone else pressing answer is refused with "only the task's owner answers its question".

A task can carry more than one Ask — approval by several people is several Asks, each with its one decider, and the task waits on all of them; an Ask with its own decider is answered by that person alone ("only Priya answers this Ask"). Others attach a view instead.

If the owner is Everyone, anyone who can edit the workstream may answer; the first answer decides, a second is refused with "this question was already answered", and the task records who answered. A question left unanswered for a day also goes to the workstream's fallback person, if it has one.

Who owns a new task: whoever asked for it. Work nobody asked for directly (from the Factory or a standing order) goes to the workstream's default owner.

## tick a person task, hand a task to someone, move it to another workstream

- Tick: only the owner ticks a person's task ("only the task's owner ticks it" otherwise). Ticking it unblocks the tasks that were waiting only on it: a person's task loses its "waiting on", and an Afi or chat task stays in Up next with the line "Ready to start: <the ticked task> is done" until it is started (see *Start — why a task stays in Up next*).
- Hand to someone: anyone who can edit the workstream changes a task's owner to another person or to Everyone. The thread says "Handed to Marco", and Marco's Inbox row says "handed to you by Priya". Taking a task yourself is not a hand-over and says nothing.
- Move: More › Move moves a task, with its whole conversation, to another workstream you can edit. Tasks that waited on it here stop waiting. Only the task's owner or whoever looks after the workstream moves it.
- Send back: see *send a done task back* below.

Reading tasks needs view on the workstream; adding, commenting, handing on and sending back need edit.

## who can stop or move a task — the owner or whoever looks after the workstream

Stopping a task or moving it to another workstream is for the task's owner and for whoever looks after the workstream (its steward, or anyone with full access to it). Anyone else who can edit is refused with "only the task's owner or whoever looks after this workstream can stop or move it", and the desk leaves Stop and Move to another workstream out of their More. They can still comment, hand it to someone or send it back. Answering a task's question and ticking a person's task stay the owner's alone, whoever looks after the workstream.

## send a done task back — Send back…, and what waits on it waits again

A done task's page has a framed line under its facts, "Done by Lena on Nov 20 · Not right? Send it back with what to change. A comment does not reopen a task.", with "Send back…" (also in its More). It asks why, then puts the task back in Up next. Anyone who can edit may send work back, a reviewer included. The thread, the task and the doer's Inbox row read "Sent back by Priya: <why>". An Afi or chat task sent back starts again with the reason. Every done task has the line, whatever it made, files only included. A Factory task's Send back reopens its job, with the reason left in the thread; where the job cannot be reopened from here (an organization's Factory), the line says "The Factory's own page reopens its work." instead. A new goal is a new task.

When a done task is sent back or unticked, every unfinished task that waits on it waits again. One that was running goes back to Up next (a Factory task keeps its job's state), and each one's thread says "Waiting again: <title> was reopened". Done tasks that waited on it stay done; send them back too if they need redoing.

## waiting on — the only way to order tasks, no circles

A task can wait on other tasks of the same workstream. That is the only ordering tool: no start dates. A task cannot wait on itself, on a task of another workstream, or in a circle ("that would make tasks wait on each other in a circle"). Only the owner changes what a task waits on and its due date.

## a plan — five or more tasks from one request wait for Start (Start all) and Change the plan

When one request makes five or more tasks, they arrive as a plan: one heading in Up next with two buttons under it, **Start** and **Change the plan**. Nothing starts until someone presses Start (it was called Start all before). Start wakes the doer of every Afi or chat task of that heading that waits on nothing unfinished, then shows those tasks Running; the rest stay in Up next until they can start. Person tasks are not started (the person does them) and Factory tasks start at the Factory. Change the plan opens Afi's pane with "Change the plan for <name>: " typed, to say what should be different. On an organization's workstream the plan's Start starts the heading's Afi tasks on the organization's server; a chat's tasks stay in Up next, because a chat is woken on its person's computer.

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

A comment or a posted answer on a task never changes its state. Writing a receipt or a piece of news on a finished task leaves it done, and nothing waiting on it is blocked again. To reopen work on purpose, use "Send back…" on the done task's page (or its More); the owner of a person's task can also untick it. Either way, what waits on it waits again.

## Start — why a task Afi or a chat does stays in Up next, Mark done, I pressed Start but the chat was busy

A task that Afi or a chat does is Running only while something is actually doing it. When what it waits on is done, it stays in Up next until its doer is woken. On a Personal workstream the task page shows **Start**: it hands the task to Afi's conversation (or to the chat that does it) and only then shows Running; a plan's Start wakes each doer once with all of its tasks. If the chat is in the middle of something when you press Start, the task goes into that chat's queue, the way a message you queue does, and is sent when its current turn ends: the task says Running and a note says "Queued · Nitya v1 starts it when it finishes what it is doing" (with the chat's name). On an organization's workstream **Start** runs an Afi task on the organization's server, as the person who pressed it and with their rights, in a run of its own that is not anybody's conversation with Afi: what it did shows in the task's thread and results, and the task ends done or failed with the reason. A run that wrote its page or file and ended without saying done is Done, with what it wrote under Result; only a run that wrote nothing, or broke off, is Failed ("Afi stopped before saying the task was done"). If the server cannot run Afi (no model key from the organization) Start says so and the task stays in Up next. A chat's task cannot be started from an organization's workstream; it starts from that chat. In every case the task's owner can finish it by hand with **Mark done** in its More, or stop it with Stop.

## due date — pick any day; change it on the task

A task can carry one due date, any day at all. Add task's Due opens a month with Today, Tomorrow and In a week above it; the arrows on the month move between months, and the keyboard moves a day or a week with the arrows and a month with Page Up and Page Down. On the task page its owner presses the due date in the line of facts, or uses More › "Add a due date…", to change or clear it. A missed date reads "Due Nov 21 · 2 days late".

## set what a task waits on — More › Waiting on

On the task page, its owner opens More › Waiting on and ticks the tasks it should wait on (or unticks one to stop waiting). The menu lists what it already waits on, then the workstream's open tasks, newest first; a task that would make a circle is never offered, and the Factory's own issues stay out of it. The facts line then reads "Waiting on: …", and the task that waits shows "Then: …" on the other.

## why my task says Interrupted — codeaf restarted while it was running

**Interrupted means nothing is working on the task any more, and what it already did is kept.** On a Personal workstream, when codeaf's engine starts again (after the app or the computer restarted, or the engine stopped), every turn that was running has ended. Any task Afi or a chat was doing whose chat is not working or asking right now becomes Interrupted, and its activity says "Interrupted: codeaf restarted while this was running".

**It resumes once without anyone pressing anything:** codeaf wakes the same chat (or Afi) again with the task and the words "Carry on from where you stopped", and the activity says "Resumed after codeaf restarted". If codeaf restarts again before that run finishes, the task stays Interrupted under Waiting on someone. Its page then offers **Start** again, which hands it back to the same chat (or to Afi). Its owner can also Mark done or Stop it from More. A chat that was still working or asking when the engine came back keeps its task Running.

A task that says Running while its chat is not working and not asking shows **Start again**, with "Nothing is working on it", and pressing it wakes that chat with the task.

The Factory's tasks are not affected: a Factory task's state is its job's, and the Factory recovers its own steps.

## task stuck on Running after the server restarted, or pressed Start on a running task and nothing happened

On an organization workstream, Afi's work on a task runs on the organization's server, and the server keeps a note of every run while it goes. **When the server starts again, every task it was working on is settled within about ten seconds; none is left saying Running with nothing behind it.**

- The task's activity says "Interrupted: the server restarted while Afi was working on it", then "Resumed after the restart". Afi carries on from its own conversation for that task, checking what it already wrote before writing again, with the rights of whoever started it.
- A task resumes **once** without anyone pressing anything. If the server restarts during that second run too, the task stays **Interrupted** under Waiting on someone, with the reason "The server restarted while Afi was working on it. Press Start to run it again."
- A run that stops reporting for a minute while the server is up becomes **Didn't report back**, with "Afi stopped reporting back while working on it. Press Start to run it again."

**Start on a task that says Running with nothing working on it starts it again**: its page shows **Start again** with "Nothing is working on it", and its activity says "Started again by <name>: nothing was working on it". Start on a task Afi really is working on changes nothing.
