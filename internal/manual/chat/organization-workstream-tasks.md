# Tasks in a workstream — who owns a task, who answers its question, ticking, waiting on, Factory jobs as tasks

## Organization workstream tasks

An organization workstream keeps its shared tasks on the organization’s server.
Open its Tasks tab to see who owns each piece of work, who does it, what it waits
on and which question needs an answer. Afi, a chat, the Factory or a person can
do the work; each task has one owner. The sections below explain the list,
permissions, starting, stopping and sending work back.

## what a task is — one piece of work, one owner, done by Afi, a chat, the Factory or a person

A workstream's Tasks tab is the shared list of its work; everyone in the workstream sees the same list. A task is one piece of work with ONE owner and one doer. The owner is always a person: the one who answers for it, decides its questions and can send it back. The doer is who does the work: a person, Afi, a named agent or crew, or a Factory job. A task a chat does reads "Your chat" (or "Lena's chat" when it is hers), and its facts say "In the chat <name>". Afi refuses `owner: everyone` when making a task: choose the person talking to Afi ("me") or one person in the workstream, named by their id from `ws_tasks`. There is no shared owner: Everyone is no longer offered as an owner, and a task that already has it keeps it.

A task has a title, a few lines saying what is wanted (the request), a brief when an agent does it (see "a task's brief"), its owner, who does it, where it came from (a chat, a person, the Factory, a standing order), its state, what it waits on, an optional due date, an optional heading that groups it, the files or links it made, the files it uses, and an optional checklist of its steps. There are no sub-tasks, priorities, estimates or custom fields: steps of one piece of work go on its checklist.

The list's sections are always in this order: Waiting on you, Waiting on others, Running, Up next, Done. Empty sections are absent. Under the Open chip, Up next and Done are folded rows at the foot with their counts ("› Up next 18", "› Done 221"); press one to open it. The first section the list draws is never folded: when every task is Up next, Up next starts open, and only the sections after it fold. Waiting on you shows three tasks, then "3 more". The first group in the list starts open and every later group starts folded, until you open it. Everyone shows all the work; Mine shows what you own, do, or need to answer. Work that ended without finishing sits in the appropriate waiting section, in the app's words: Failed (with its reason), Interrupted, Stopped by you (to whoever pressed Stop; everyone else reads "Stopped by Marco"), Didn't report back. A heading such as "Release 2.4" or "Factory" sits in the most urgent section any of its other tasks is in, with them under it. A task waiting on you is the exception: it is drawn under Waiting on you, with its heading's name in the Group column, and not again under its heading, whose "0 of 7 done" still counts it. So Waiting on you always holds the rows it counts.

## a task's brief — goal, deliverable, acceptance, inputs and constraints, so another agent can do it cold

When an agent makes a task for an agent to do, it writes a brief that an agent who never saw your conversation can work from:

- **Goal** — one or two lines on why the work matters, and for whom.
- **Deliverable** — what comes back and where ("a document in Files").
- **Acceptance** — two to six lines the result is checked against. The doer's check answers them.
- **Inputs** — what the work builds on, as references: a file in Files, an earlier task's Result, a decision, or a link. A task it waits on is one of its inputs by itself.
- **Constraints** — lines it must keep to, such as "No meal planning" or "Under 200 words".

A task an agent does is refused without a goal, a deliverable and its acceptance lines, and an input that names a file or task the workstream does not have is refused too. Your private conversation is never copied into a brief. A person's task may have a brief, and doesn't need one. Tasks made before briefs existed show only their request.

## what a task's run starts with — its inputs already read, and the hand-off note from the task before

On an organization's server, when Afi starts a task, its first message already carries the brief, the decisions in force, and the words of every input: each file's text, and for each earlier task its hand-off note, its check line and its result files. That is at most about 48 KB (twelve thousand tokens); a longer input is cut and says how to read on. Afi reads anything else only when that doesn't cover it. Each file it was handed counts as used by that task, as a file it read itself does.

When an agent finishes a task it leaves a **hand-off note**: two to four lines on what it made, what it assumed, and what the next task should know. Done without one is refused. A run that ends having written its work, without a note, gets "Made ... Afi left no note of its own on what to carry forward." The note is the first thing the next task's run reads about this one.

## the agent can't do a task — it asks you instead of failing: Paste what it needs, Do it myself (take it back), Drop it

When the agent doing a task can't go on because an input is missing, or the task needs something it doesn't have (Afi on an organization's server has no web, so it can't open live store content), it doesn't mark the task Failed. It asks the task's owner. The task waits under Waiting on someone with the question "Can you give Afi what it needs?", its reason in its own words, and three answers:

- **Paste what it needs**: write or paste what it lacks (their text, the figures) with the answer. The task runs again with your words.
- **Do it myself**: you take it back. Done by becomes you, and the task is your own checkbox in Up next; what the agent made stays in the task.
- **Drop it**: the task is stopped, by you.

The agent may offer its own answers instead, such as "Paste their text". None of these answers is kept in the workstream's decisions. Failed is kept for errors: a run that broke off (the model refused, the connection dropped) still says Failed, with why.

## find a task — search tasks, filter tasks by state with the chips, Person and Group filters

The Tasks tab's row holds **Search n tasks** beside Everyone and Mine. Search matches titles and people (who does a task and who owns it), ignoring case. Under the tabs are the state chips, with their counts: **Open** (the default: everything not done), **Waiting on you**, **Running**, **Up next** and **Done**. A chip other than Open shows only its tasks, open and unfolded. **Person** and **Group** at the chips' right narrow the list to the tasks one person does or owns, or one group's. The counts follow the search and the filters, and each is the number of rows its chip shows. A chip with nothing in it is not drawn ("Running 0" never appears); Open, and the chip you are on, always are. No count anywhere in the Tasks tab, or on a workstream's tabs, reads 0: a new workstream's tab says "Tasks", not "Tasks 0". Nothing matching reads "Nothing matches · clear the search or pick Open". The chip, the search and the filters last while the tab is open; the list comes back on Open, unfiltered.

With no task open, a row is one line: the mark, the title, then three columns, the group, who does it, and its state ("4m", "due Fri", "waits on 1").

## read tasks side by side with the arrow keys — the split Tasks list, 420 wide

A task opens only when you pick its row. With at least 1100 pixels of content width and Afi's pane closed, the picked task opens beside a 420-pixel list, and picking another row swaps it. Beside a task the rows are narrow: a title wraps to two lines before it is cut, and who does it is their face only (the name shows when you point at it). The state chips step aside while a task is open beside the list, unless a filter is on. With focus in the list, Up and Down move through the visible tasks without pressing Back. Running rows show their current step when the runner supplies one.

Two icon buttons sit at the top right of the open task: **Open as its own tab** and **More** (⋯). Nothing on the task is ever wider than its pane; its controls wrap instead.

## open a task — how do I go back to the task list after opening a task, Open as its own tab

Below 1100 pixels of content width, or while Afi's pane is open, selecting a task opens it in place, inside the Tasks tab. The workstream's header and tabs stay, and a bar shows “‹ Tasks”, the task's name, and the same two icon buttons: Open as its own tab and More (⋯). Escape, “‹ Tasks”, or pressing the Tasks tab again returns to the same list position. A task opened from the Inbox, a file's “Made by task”, “Used by” or “From” line, or a chat's list of what it made opens in the same workstream's Tasks tab.

“Open as its own tab” shows the task alone in the window's one preview tab: opening another task replaces it unless you pinned it or worked in it. In the Tasks list, Command-click (Control-click on Linux) toggles selection instead, at every width: below 1100 pixels, or with Afi's pane open, it selects the row without opening it. A chat keeps its own tab.

## select several tasks — shift click, command click, bulk Done by, Give to Afi

Shift-click selects the visible range from the first selected task. Command-click, or Control-click on Linux, adds or removes a task. Shift with Up or Down extends a selection. Folded tasks are not included. A selection of several tasks stays marked in the list, and the pane says "3 tasks selected" with the controls they share: Done by, Owner, Due and Group, then "Only what all three allow". A row's checkbox selects or unselects it; it never marks it done. Clear returns to reading one task. One task is never a selection, so changing one task's Done by asks no question first. Below 1100 pixels of content width, or with Afi's pane open, the list stays when you select: the selection's pane sits above the rows, so you can keep adding to it.

Done by offers only choices every selected task allows: the owner or workstream steward can change a doer, but completed tasks and Factory tasks cannot be handed over here. The choices are Afi, your chats in this workstream, You, and its people. Named agents and the Factory are not offered as replacements. When every selected task allows Afi and they are not all Afi's already, the pane opens with Afi chosen: “Give these N to Afi”, the consequences, Give and Cancel. Choosing another doer shows the same with its name. The owner is unchanged. Ready agent tasks start through the normal Start door; unfinished waits stay Up next. If one task is refused, the others are still handed over. The selection names each refused task and its reason; the list refreshes before you retry. The What happens section names a task’s predicted ask-first step when the workstream supplies one, from its ask rule. Past five tasks those lines are counted instead ("6 will ask you first, before anything is published or sent").

A Personal workstream keeps its tasks on this computer; an organization workstream keeps them on the organization's server. They behave the same.

## who can answer a task's question — only the owner, Everyone means the first answer, the fallback after a day

When a task needs a choice or an approval, it asks its owner and shows as "Needs Priya". Only the owner sees buttons on the question; anyone else sees who answers and may comment. Someone else pressing answer is refused with "only the task's owner answers its question".

A task can carry more than one Ask — approval by several people is several Asks, each with its one decider, and the task waits on all of them; an Ask with its own decider is answered by that person alone ("only Priya answers this Ask"). Others attach a view instead.

If the owner is Everyone, anyone who can edit the workstream may answer; the first answer decides, a second is refused with "this question was already answered", and the task records who answered. A question left unanswered for a day also goes to the workstream's fallback person, if it has one: every one of the task's Asks, the decision check's included, appears under Waiting on you in the fallback person's Inbox and gets buttons on the task view.

Every Ask you decide reaches you, not only the task's own: a task's second and third Ask, and the Ask the decision check raises when work goes against a decision, each get their own row under Waiting on you in the Inbox, and each counts in the Work list's amber number for that workstream.

## Why the Inbox has an amber dot — what counts as waiting on you

The amber dot and number on Inbox in the app's left rail count only what waits on you: a chat's question to you, every Ask you decide (a task's own, the decision check's, and the ones that reach you as a workstream's fallback person), and your work that was cut off before it finished (stopped by a restart, or ended without its report). Nothing else turns it amber. A task that failed is red in Tasks and is not counted; due dates, work sent back to someone else, and comments for you have their own Inbox rows without the dot. A workstream's number in the Work list counts the same things for that workstream.

Who owns a new task: whoever asked for it. Work nobody asked for directly (from the Factory or a standing order) goes to the workstream's default owner.

## who is doing this task — what Your chat means, Factory for Marco, and what the row says

Each row in Tasks reads left to right: a mark for the task's state, the title, who does it, and one short state word. The mark is never amber, even when the task waits on you (amber means the Waiting on you count); it is a check when it is done, a box for a person's work, a dot while an agent works, and a hollow ring for an agent's work not started yet or asking. Who does it is the doer's face and name: "You", "Afi", "Lena", "Your chat" for a chat of yours, "Lena's chat" for one of hers (you see its tasks, never the chat), or "Factory #128". When the owner is someone other than you, their name follows: "Factory for Marco", "Afi for Lena". The owner is never shown where the doer goes. The state word is short: "asks you" or "asks Marco", how long it has run ("4m"), "due Fri", "waits on 1", or "failed" in red. Every running row says how long it has run, and its second line says what it is doing now (a Factory job's step); a failed row's second line says why. Work up next and done work say nothing there. A heading names who does its tasks: "Release 2.4 · 2 of 6 done · Your chat, Afi, Lena".

The task view says it above the title ("Afi is doing this · running 4m", "Your chat is doing this", "Afi did this") and its facts say which chat ("In the chat Launch post"). Inbox rows lead with the doer: "Your chat · waiting on you · Pantry", "Afi · Finance · for Marco".

## asks you on a task row — an agent paused on a question is not Running

An agent that asks a question stops until it is answered, so its task is not running. In Tasks its row has the hollow ring, not the working dot, and its state word is "asks you" (or "asks Marco" when it waits on someone else), never a running time such as "3h". It sits under Waiting on you, counted by that heading and the Waiting on you chip, never by Running. The Inbox's Running for you lists it under Asking you, "paused on your answer"; a task that waits on it lists it as "asks you" in its Waiting on row. Answering starts the agent again, and the task is Running once more.

## change who does a task — Done by, offload to Afi, take it back, and Change the owner

**Done by** is the first control under a task's title, before Owner, Due, Waiting on and Group, and Add task has the same choice. It lists You, the workstream's people (team members included), Afi and your chats in the workstream. Choosing Afi or a chat offloads the task: it starts at once when nothing it waits on is unfinished, otherwise when that is done. Choosing You takes it back: if an agent is running it, the agent stops and what it made stays in the task. The owner never changes when the doer does; **Owner** is its own control. Only the task's owner or whoever looks after the workstream changes Done by, anyone else is refused ("only the task's owner or whoever looks after the workstream changes who does it"). A done task is sent back first, and a Factory task is always its job's. Unfinished work goes back to Up next for its new doer. The thread gains one line, "Marco handed this from Afi to Lena", and a person who now does it finds it in their Inbox.

On an organization's workstream Done by can be changed only on a server new enough to keep it; on an older one it is shown and cannot be opened. A chat is offered there once the organization's chats run on its server.

## Afi gave the task to the wrong person — a review is done by the reviewer

When Afi makes a task it names who does it: Afi, a person in the workstream by name, or one of the workstream's agents. A review or a sign-off meant for Marco is done by Marco and owned by whoever asked, so it lands in Marco's Inbox and Marco ticks it. Before this, Afi could only say "a person", which always meant the owner, so "Marco reviews the store text" came out done by the person who asked. Afi is refused when it names someone outside the workstream or an agent the workstream has not added. If a task still has the wrong doer, its Done by control fixes it.

## tick a person task, change the owner, move it to another workstream

- Tick: a person's task is ticked by its owner or by the person doing it ("only the task's owner or the person doing it ticks it" otherwise). A task made for Marco, by you or by Add on Afi's cards, stays yours to answer for, and Marco ticks its checklist and marks it done, with no hand-over first. Ticking it unblocks the tasks that were waiting only on it: a person's task loses its "waiting on", and an Afi or chat task starts by itself (see *a workstream task starts automatically*) or, where it waits for a person's Start, stays in Up next with the line "Ready to start: <the ticked task> is done".
- Change the owner: the **Owner** control under the title, or More › **Hand to someone**, which lists the workstream's other people. Anyone who can edit the workstream changes a task's owner to another person: anyone named on the workstream, or in one of the teams it is shared with. A task always has one owner, so Add task and the Owner control never offer Everyone (a task made with owner Everyone before keeps it until handed on). The thread says "Handed to Marco", and Marco's Inbox row says "handed to you by Priya". Taking a task yourself is not a hand-over and says nothing. Who does the work is a separate choice; see *change who does a task*.
- Move: More › **Move to another workstream** moves a task, with its whole conversation, to another workstream you can edit. Tasks that waited on it here stop waiting. Only the task's owner or whoever looks after the workstream moves it.
- Send back: see *send a done task back* below.

Reading tasks needs view on the workstream; adding, commenting, handing on and sending back need edit.

## who can stop or move a task — the owner or whoever looks after the workstream

Stopping a task or moving it to another workstream is for the task's owner and for whoever looks after the workstream (its steward, or anyone with full access to it). Anyone else who can edit is refused with "only the task's owner or whoever looks after this workstream can stop or move it", and the desk leaves Stop and Move to another workstream out of their More. They can still comment, hand it to someone or send it back. Answering a task's question stays the owner's alone, whoever looks after the workstream, and ticking a person's task is the owner's or the person's doing it.

## I answered Afi's question and nothing happened — an answer puts Afi back to work, unless the task still waits on other work

When Afi, working a task on an organization's server, asks a question, its run ends and the task shows "Needs" and the person it asks. Answering the last open question on the task sets it Running and starts Afi on it again straight away, in the task's own run, with what it was asked and the answer you chose ("Internal team only"), so it carries on from there. "Go ahead anyway" on a paused task does the same. If the answer lands while Afi is still finishing the turn that asked, it starts again the moment that turn ends. A task with more than one question waits until every one is answered. An answer never starts a task that still waits on unfinished work: the answer is kept and its decision recorded, the task goes to Up next, and its thread says "Up next: waits for Confirm the launch date before it starts" (with the titles it waits on). It starts by itself once that work is done, as any waiting task does, and its thread then says "Started: Confirm the launch date is done". If the server cannot run Afi any more (no model key), the task stays Running with nothing behind it until its owner stops it or marks it done.

## what Afi's conversation shows when you press Start — Start "the task", never words you did not write

On a Personal workstream, Start and Send back hand the task to Afi's conversation (or the chat that does it). The conversation shows what you did, as your line: Start "Compare ways to move", Start all 3 tasks in "Move day", or Sent back "Compare ways to move": and your own reason. The fuller instruction the doer is given (the task, what was asked, mark it running and done) is not shown as a message from you.

## send a done task back — Send back…, and what waits on it waits again

A done task's view has a framed line under its facts, "Done by Lena on Nov 20 · Not right? Send it back with what to change. A comment does not reopen a task.", with "Send back…" (also in its More). It asks why, then puts the task back in Up next; "Send back" stays idle until you write why, and a send back with no reason is refused with "say why it goes back". Anyone who can edit may send work back, a reviewer included. The thread, the task and the doer's Inbox row read "Sent back by Priya: <why>". An Afi or chat task sent back starts again with the reason. Every done task has the line, whatever it made, files only included. A Factory task's Send back reopens its job, with the reason left in the thread; where the job cannot be reopened from here (an organization's Factory), the line says "The Factory's own view reopens its work." instead. A new goal is a new task.

When a done task is sent back or unticked, every task that waits on it waits again, a done one included. One that was running or done goes back to Up next (a Factory task keeps its job's state), and each one's thread says "Waiting again: <title> was reopened". A done task reopened this way reopens what waits on it in turn.

## waiting on — the only way to order tasks, no circles

A task can wait on other tasks of the same workstream. That is the only ordering tool: no start dates. A task cannot wait on itself, on a task of another workstream, or in a circle ("that would make tasks wait on each other in a circle"). Only the owner changes what a task waits on and its due date.

## a plan — five or more tasks from one request wait for Start (Start all) and Change the plan

When one request makes five or more tasks, they arrive as a plan: one heading in Up next with two buttons under it, **Start** and **Change the plan**. Nothing starts until someone presses Start (it was called Start all before). Start wakes the doer of every task of that heading an agent does (Afi, a named agent or crew, or a chat's agent) that waits on nothing unfinished, then shows those tasks Running. Pressing Start is consent for the whole plan: each of the rest starts by itself once what it waits on is done, with no other click, unless its own view says "Waits for you to start". Person tasks are not started (the person does them) and Factory tasks start at the Factory. Change the plan opens Afi's pane with "Change the plan for <name>: " typed, to say what should be different. On an organization's workstream the plan's Start starts every one of those tasks on the organization's server, each in a run of its own that begins from the task's request, never from a private chat. There are no chats on an organization's server, so when you ask Afi there for big work it makes the plan itself, every task done by Afi unless a person is to do it, and waits for Start; asked to coordinate, it reports how the plan is going in its pane.

## a Factory job is a task — its state, its question, never copied

Every Factory job in the workstream appears in Tasks as a task done by the Factory, under the Factory heading. It is the job itself, read again each time, never a copy. Its state is the job's in the app's words (Failed, Interrupted, Stopped by you, Didn't report back), its question is the job's open question, and its facts line says which step it is on ("Step 4 of 4, Review"). Answering its question answers the job, under the job's own rules.

You can hand it to someone, give it a due date or a heading, or make other tasks wait on it. You cannot set its state by hand: "a Factory task's state is its job's: stop, start or answer the job instead".

## comments on a task, Afi's offer, Add it, undo, a private request noted on the task

A task's Activity and comments is one thread everyone in the workstream sees: what was done, people's comments, and answers. Comments go to people. Afi never acts on a comment; it may offer under one ("Afi can add this to the draft · Add it"). Anyone may press Add it; the owner is told and can undo for 5 seconds, after which "the five seconds to undo this have passed".

A private conversation with Afi stays private. If something you ask Afi there changes shared work, the task gets one line ("Priya asked Afi to keep it under 300 words"), never the conversation. "Post this to the task" adds Afi's answer to the thread, marked as posted by you.

## Afi posted a long comment, where did Afi's draft go — what Afi makes is a file, the one line with Open

What Afi or a chat makes on a task is a file, never a long comment. A comment from an agent is a line or two. When one writes more than 400 characters or more than 3 lines on a task's thread, the words are kept as a document in the workstream's Files instead, made by that task and named by its first heading or else by the task's title, and the thread gets one line: the agent's mark, "Afi made" and the document's name, with Open. Open shows the document in place, where you can read it, edit it and comment on its passages; it is also listed under the task's Result.

This holds on your own computer and on an organization's server alike. A person's comment is never turned into a file.

## attach a file in a comment, and Attach to the task from Afi's pane

- **In a comment**: **Attach** (the paperclip at the end of a task's comment box) picks files from your computer; they show above the box as chips you can take off before sending. Sending adds them to Files, attaches them to the task, and ends the comment with "Attached Interview notes.pdf". Nothing is uploaded until you send, so a comment you give up leaves nothing behind. Someone who only views the workstream has no Attach.
- **From Afi's pane**: with a task in front of you, Afi's latest answer can go two ways. "Post this to the task" puts the words on the thread. **Attach to the task** makes them a document in Files, named from the answer's first line, made by Afi for you and listed under the task's **Result**; the thread says "Attached “Brief for Lena”, drafted with Afi". Either way your conversation with Afi stays private.
- A task Afi made from your pane says "Added by Afi for you" (or "for Marco") in its facts, never that Afi is a chat.

## send a request to another team's workstream — ask another workstream for work, one you are not in

Work for another team is a request into their workstream, not a task in yours. On a task, More › "Send a request to" lists every workstream in the organization you may ask: the ones you are in, and every one that is not private, each with who receives it. Pick one, write what you want (the first line is its title), and press "Send request".

It becomes a task in that workstream, owned by its owner of incoming work (People and rules), who also does it, or done by Afi when that owner is Everyone. It waits in Up next until they start it. Its view says "From" you and your workstream, and its thread starts "Requested by Priya from Marketing". Your task gains one line, "Sent a request to Engineering: <title>". If you are not in that workstream you see only that it was sent and who owns it, never its other work. A private workstream you are not in cannot be asked, and an archived one is not listed.

Afi never sends a request on its own: asked about another workstream's work, it names that workstream and leaves the request to you. A Personal workstream has nobody else to ask, so the item is absent there.

## @name in a comment — who a comment reaches, mention someone, Comments for you

There is no chat between people; a comment on a task is how you say something about it. A comment reaches the task's owner, and anyone it names with @: "@abir", "@Abir" or "@AbirDas" all reach Abir. Only people in the workstream (named on it, or in one of its teams) can be reached; an @ that names nobody there, or one inside a word such as an email address, reaches nobody and the comment is still kept.

A comment that reaches you shows in your Inbox under "Comments for you", with the newest comment's words and who wrote it, until you open the task. Opening it there or in Tasks marks it read; a new comment makes it unread again. Your own comments never reach you, and comments older than two weeks are not counted. Afi reading a task never marks it read for you.

This works in an organization workstream. A Personal workstream has only you in it, so nothing there is for anyone else.

## someone left the workstream — who gets their tasks, handed to me, the remover, or the fallback

When a person is removed from an organization workstream, each of their unfinished tasks there (and in every workstream inside it) is handed to whoever removed them, so it is theirs to hand on. Someone who takes themselves out leaves their tasks to the workstream's fallback person, or to the person who looks after the workstream when there is none. The thread says "Handed to Mia", by name. Done tasks keep who did them, and a person's own checkbox task moves with the hand-off. Anyone who can edit can hand a task on again by hand.

Someone removed from the whole organization is different: the tasks they owned go to the admin who removed them, into that admin's **Handed to you**, to hand on. A task given to someone invited who has not joined yet waits until they join.

## attach a file to a task — Attach in the comment box, drag a file onto a task, what a task uses

A task's view lists what it made (**Result**, or **Files** on a person's task). There is no **Uses** list on the view any more, and no Remove from this task: what a task reads is linked to it on the file instead.

- **Attach** at the end of the task's comment box uploads files from your computer with your comment (see *attach a file in a comment*).
- **Drag a file onto a task**: a row dragged from Files onto a task's row in Tasks, or onto its open view, attaches it. A file dragged from your computer onto a task is added to Files first, then attached ("Added to Files and attached").

The link lives on the file: its facts in Files say "Used by" the task, and a document's or a canvas's view says "Made by task …" and "Used by …" (or "Used by 2 tasks", which lists them), each opening the task. When a file a running task uses changes, the task asks its owner whether to carry on. Someone the workstream is shared with to view sees what is attached but has no Attach and no Remove.

## Afi links files to the task by itself — what a task's run reads and makes

When Afi or a chat does a task, nobody has to attach its files. Every file of this workstream it reads is linked to the task (the file's facts say "Used by" it), and every document or canvas it makes is the task's own, under **Result**, made by that task. A file from the workstream this one sits inside is read but not attached, because it belongs to that workstream's tasks. Talking to Afi in your own pane, outside a task's run, links nothing by itself. Documents read through `workstream_pages` count as inputs too: `read` shows every document's start and marks those documents; `page` marks the one it reads. The task's view does not list what it read; the file's own view does.

## a canvas on a task — its live picture, open it inside the task, Open beside

A canvas a task made or uses shows a small picture of its first view, repainted a moment after anyone changes it. Clicking it opens the canvas inside the task, with a bar naming the task to go back (Escape too) and **Open beside** to put it in the window's preview tab. Organization canvases use the same reader as the open canvas, so their real first view appears too.

## a task's result — read the document without opening it, Result above Request, Checked against

A task's view reads, top to bottom: the state line ("Afi is doing this · waiting on you"), the title, the fact controls (Done by, Owner, Due, Group), the **Waiting on** row, its question for you (the Ask), then **Result** with the first lines of what it made, the doer's **Checked** line, the hand-off note, **Assumed**, then **Request** (or its **Brief**) cut to six lines with **More** to read the rest, the **Checklist**, and **Activity and comments**. Result sits above Request because once work is done, what was made matters more than what was asked.

**Activity and comments** has a switch at its right: **All**, or **Comments only**, which keeps people's comments and what was posted to the thread and hides the run's own lines. The view ends in one box, **Comment …**, with the faces of who hears it: the task's owner unless that is you, and anyone you @name. On a task Afi (or a chat) does that you may steer, the same box has a two-way switch at its left: **Comment · everyone in <workstream>** or **Tell Afi** (see the box's two sides, Comment or Tell Afi).

The first document an agent's task made is shown inside Result: its name, where it is in Files and **Open**, then its heading and first few lines, so you can read it without leaving the task. **Open** is for editing it. Anything else the task made (more files, a canvas's picture, a pull request) is a row under it. A document that cannot be read shows only its line with Open.

Under the result, the doer's check reads "Checked against the request: …", or "Checked against the request and 2 decisions: …" when it also checked the work against that many of the workstream's decisions: the ones in force when the check was written, which its run was given. A task with no check shows no line.

## a task's brief — Goal, Deliverable, Acceptance, Inputs, Constraints, and the hand-off note

A task made with a brief shows it under **Brief**: the request as a summary, then **Goal** (why, and for whom), **Deliverable** (what comes back, and where), **Acceptance** (the lines its check answers), **Inputs** (each a row: a file opens, a canvas shows its picture, another task opens in place and stands for its Result, a decision reads as its words, a link opens in the browser) and **Constraints**. Only the parts with words show. A task made before briefs, or by a person in Add task, shows only its **Request**.

When the doer finishes, the two to four lines it leaves for the next task show as **Hand-off** under the Result. A task with no note shows none.

## a checklist inside a task — steps, not sub-tasks, tick a step, 2/5, where did Add a step go

A task can carry a **checklist**: the steps of that one piece of work ("Outline", "First draft", "Check the numbers"), each one line that is ticked or not. A step is not a task: it has no owner, due date or view of its own. Work someone else does, or work delivered on its own, is a separate task instead.

- Afi writes the checklist when it makes a task for steps of one deliverable, and ticks each line as it finishes it while doing the task.
- On the task view the **Checklist** sits under the request. Anyone who can edit the workstream ticks or unticks a line, rewords one by clicking it, or removes one. There is no Add a step: the steps are the doer's, written with the task, and a task without steps shows no checklist. Someone it is shared with to view sees the lines and cannot tick them.
- The task's row in Tasks shows how far it is, "2/5", among its dim counts; a task without a checklist shows none.

A checklist holds at most 30 lines of up to 200 characters each; past that the work is more than one task.

## the counts on a task's row — files and canvases

A task's row in Tasks carries dim counts after its title: a file mark with how many files it made or uses, a canvas mark with how many canvases, and a checklist's progress such as "2/5". A task with nothing attached and no checklist shows no count at all.

## a file changed while a task was running — carry on or start again

When someone changes the contents of a file that a running task's work read, that task stops and asks its owner "Layout changed while this ran. Carry on?", with "Carry on with the new version" and "Start again from the new version". Only the owner answers, and either answer starts the task again. A rename alone flags nothing, and a task that is not running is left alone. A Factory task cannot be stopped this way, because its state belongs to its job: its thread only gains the line "Layout changed while this ran".

## does a comment reopen a done task? — no, a comment never changes a task

A comment or a posted answer on a task never changes its state. Writing a receipt or a piece of news on a finished task leaves it done, and nothing waiting on it is blocked again. To reopen work on purpose, use "Send back…" on the done task's view (or its More); the owner of a person's task can also untick it. Either way, what waits on it waits again.

## a workstream task starts automatically once what it depends on is done — Starts when ready, or hold it for your Start

A task Afi or a chat does that depends on other tasks **starts automatically** once everything it depends on is done: nobody presses Start. Its thread says "Started: <the finished task> is done". This is on by default, in Personal and in organizations.

- **Settings › Work** has one switch, "Start tasks when what they wait on is done". Turned off, such tasks stay in Up next for a Start, with "Ready to start: <title> is done" in their thread. In an organization the server keeps your choice for the tasks you own.
- **Each task** that depends on others says on its facts line "Starts when ready" or "Waits for you to start". Click it to pick the other; the thread records who did. Only the task's owner, or whoever looks after the workstream, can change it ("only the task's owner or whoever looks after this workstream can say whether it starts by itself"). Picking "Starts when ready" on a task that is already ready starts it at once.
- **A plan** (five or more tasks from one request) holds for its Start. Pressing Start is consent for the whole plan: what it releases afterwards starts without another click, even with the Settings switch off. A task set to hold still wins.
- **Nothing else changes.** An Ask on the task, the workstream's spending rule ("Start without asking") and the decision check still pause it. A task done by a person is never started for them; it lands in that person's list and Due. A Factory task starts at the Factory.
- **Where it runs.** On Personal this computer wakes the doer while codeaf is open (a doer mid-turn gets it when the turn ends). On an organization the server starts an Afi task with the rights of whoever finished the work it depended on; a chat's task there does not start by itself, it stays in Up next with "Ready to start" until someone presses Start, and Start runs it on the server.

## Start — why a task Afi, an agent or a chat does stays in Up next, Mark done, I pressed Start but the chat was busy

A task that Afi or a chat does is Running only while something is actually doing it. When what it waits on is done it goes ahead with no click, unless it waits for a person's Start: its view says "Waits for you to start", it belongs to a plan nobody has started, Settings › Work's "Start tasks when what they wait on is done" is off, or its doer could not take it (then its thread says "Ready to start: <title> is done"). Such a task stays in Up next until its doer is woken. On a Personal workstream the task view shows **Start**: it hands the task to Afi's conversation (or to the chat that does it) and only then shows Running; a plan's Start wakes each doer once with all of its tasks. If the chat is in the middle of something when you press Start, the task goes into that chat's queue, the way a message you queue does, and is sent when its current turn ends: the task says Running and a note says "Queued · Nitya v1 starts it when it finishes what it is doing" (with the chat's name). On an organization's workstream **Start** runs the task on the organization's server, as the person who pressed it and with their rights, in a run of its own that is not anybody's conversation: Afi's task as Afi, an agent's or a chat's task as that agent, starting from the task's request (a chat's private conversation never reaches it). What it did shows in the task's thread and results, and the task ends done or failed with the reason. A run that wrote its view or file and ended without saying done is Done, with what it wrote under Result (after your answer to its Ask, the view it wrote before counts); one that leaves the task waiting on unfinished work puts it in Up next. Only a run that wrote nothing, or broke off, is Failed ("Afi stopped before saying the task was done"). If the server cannot run agents (no model key from the organization) Start says so and the task stays in Up next. A task added already running (a caller of the server that makes it with the state running) is started the same way, as the person who added it: its doer is woken before it says Running, and if nothing can take it the add is refused with the reason and no task is kept. It never sits Running with nothing behind it. On a Personal workstream an agent's or crew's task is handed to that agent's own conversation, the way a chat's is. In every case the task's owner can finish it by hand with **Mark done** in its More, or stop it with Stop.

## the Working conversation — see what the agent is doing on a task, who can see it

A task Afi or a chat does has a **Working** section on its view, between Result and Activity and comments. It shows the conversation that does the task: what the agent says, each step it takes (folded into one line each) and what it made. Its first line says who started it ("Started by mia"), and the line above it says whether it is working now. Folded, it shows only its latest line; **Open the conversation** shows all of it in place, and **Fold it** goes back to the latest line.

On an organization's workstream the Working conversation is the server's run of that task, and **everyone in the workstream sees it**, live, under the caption "Everyone in <workstream> sees this". On a Personal workstream it is the conversation of Afi or of the chat that does the task.

The Working section has no box of its own: you steer the agent through the task's one box at the bottom (see the box's two sides, Comment or Tell Afi). A section with nothing in its conversation yet is not drawn.

A person's task, and a Factory task, have no Working section.

## the box's two sides, Comment or Tell Afi — steer the agent on a task, switch who hears the box, where did the Tell Afi box go

A task's view has **one box**, at the bottom. On a task Afi (or a chat) does, the task's owner and whoever started the run see a **two-way switch** at the box's left, the side in use with its words and the other as its figure:

- **Comment · everyone in <workstream>**: a comment in the thread. An @name still reaches that person. A comment is never an instruction to the agent.
- **Tell Afi** (or the chat's name): your words go to the agent. While it works they land at its next step; on a Personal workstream, when nothing is working, they are sent as a new message. On a done Afi task this side reads **Revise with Afi**, and Send reads **Revise** (see revise finished work). On an organization everyone in the workstream reads what you tell it too: typing on a task is shared, while Afi's pane and your own chats stay private.

The box opens on **Comment**, unless the task is waiting on Afi's next step: you may tell it, the task is not done, nothing is working on it and no question is open on it (a question is answered on its card). Then it opens on **Tell Afi**. A side you pick stays picked while the task is open. Anyone who may not steer sees only the Comment box, with no switch. There used to be a separate "Tell Afi…" box under Working; it is gone, and this switch does what it did.

## revise finished work — change what Afi made without reopening the task, Revise with this

**A done task is revised, never reopened.** On an organization's workstream, when Afi has finished a task, its owner (or whoever started its last run) can still switch the task's box to **Revise with Afi** and type there. The words start a new run of Afi on the same task: it changes what it made as asked, usually by rewriting its document or suggesting the change if a person has edited it, and posts one line saying what it changed. The task stays Done the whole time, and nothing that waits on it is held up again. The run shows in the Working conversation like any other, and what it costs is added to the task's.

**Revise with this.** When someone comments on a task Afi is doing or has done, and the comment asks for a change ("please add a short FAQ", "can you make it shorter", "use the old price"), Afi offers **Revise with this** under it. Anyone who can edit the workstream may take it. Taking it hands the comment's words to Afi at once: into the run while one is working, or as a revision of the done task. The Working conversation says whose comment it came from. A comment that asks for nothing ("shipped this morning") gets no offer, and a comment is never acted on until someone takes the offer.

To change what a task asks for instead, edit its request; to have it done again from the start, use Send back.

## due date — pick any day; change it on the task

A task can carry one due date, any day at all. Add task's Due opens a month with Today, Tomorrow and In a week above it; the arrows on the month move between months, and the keyboard moves a day or a week with the arrows and a month with Page Up and Page Down. On the task view its owner presses **Due** in the fact controls under the title to change or clear it. A missed date reads "Due Nov 21 · 2 days late".

## set what a task waits on — the Waiting on row, Change, Wait on a task

Under a task's fact controls, **Waiting on** is a row: one line per task it waits on, with that task's mark, title and state ("done", "running", "up next", or the person doing it). Its owner presses **Change** (or **Wait on a task** when it waits on no task yet) and ticks the tasks it should wait on, or unticks one to stop waiting. The menu lists what it already waits on, then the workstream's open tasks, newest first; a task that would make a circle is never offered, and the Factory's own issues stay out of it. The task that waits shows "Then: …" on the other. Someone who cannot change it sees the row only when the task waits on something.

## add a task by hand — Add task, the row at the top of the list

**Add task** in the workstream's header opens a row at the top of Tasks, and the button looks pressed while the row is open. The row has a title field, then the same four controls a task has: **Done by** (Afi unless you pick someone), **Owner** (you), **Due** and **Group** (an existing group, none, or New group…), with **Cancel** and **Add task** at the end of the line. One line under it says what Add will do: "Afi starts it when you add it. It asks before anything is published or sent." for Afi or a chat, "It goes in your Tasks. Nothing starts by itself." for you, or "Marco sees it in their Tasks when you add it." Return adds it; Escape lets it go. Anything bigger than one task is better asked of Afi.

## a task's More menu (⋯) — Mark done, Hand to someone, Move to another workstream, Stop, Delete

The ⋯ at the top right of a task holds verbs only, in this order: **Mark done**, **Hand to someone**, **Move to another workstream**, **Stop**; then **Ask someone**, **Send a request to** and **Delete**. A verb shows only to someone allowed to do it. Who does it, the owner, the due date and what it waits on are the controls under the title, never the menu, and **Send back…** is on a done task's own line.

## why my task says Interrupted — codeaf restarted while it was running

**Interrupted means nothing is working on the task any more, and what it already did is kept.** On a Personal workstream, when codeaf's engine starts again (after the app or the computer restarted, or the engine stopped), every turn that was running has ended. Any task Afi or a chat was doing whose chat is not working or asking right now becomes Interrupted, and its activity says "Interrupted: the app closed while this was running".

**It resumes once without anyone pressing anything:** codeaf wakes the same chat (or Afi) again with the task and the words "Carry on from where you stopped", and the activity says "Resumed after the app opened again". If codeaf restarts again before that run finishes, the task stays Interrupted, under Waiting on you (its owner's), until someone presses Start. Its view then offers **Start** again, which hands it back to the same chat (or to Afi). Its owner can also Mark done or Stop it from More. A chat that was still working or asking when the engine came back keeps its task Running.

A task that says Running while its chat is not working and not asking shows **Start again**, with "Nothing is working on it", and pressing it wakes that chat with the task.

The Factory's tasks are not affected: a Factory task's state is its job's, and the Factory recovers its own steps.

## how many tasks run at once — Queued · 2nd, the limit of 3 per workstream and 10 per organization

On an organization's server, **at most 3 of Afi's tasks run at once in one workstream, and at most 10 in the whole organization.** Starting more is never refused: each extra task is accepted and waits its turn, and its row reads **"Queued · 1st"**, "Queued · 2nd" and so on, its place among that workstream's waiting tasks.

- Inside a workstream, waiting tasks start in the order they were started, each as soon as one of the three running finishes.
- When the organization is full, a free place goes to the workstream that has waited longest for one, so one workstream's big plan cannot hold back everyone else's.
- **Stop** on a waiting task takes it out of the queue; the places behind it move up.
- A task waiting when the server restarts waits again afterwards. It was never working, so it is not Interrupted.

A running row has a second line saying what Afi is doing at this moment, in words ("Reading crash log 9 of 14"); a task waiting for its place has none. The Work header counts the tasks being worked on, those waiting for a place and those that failed: "7 running · 2 queued · 1 failed", each part only when it is not zero.

These limits are fixed for now; nobody can change them in Settings yet. On a Personal workstream each chat does one thing at a time, and these limits do not apply.

## Afi paused a task to ask whether to continue — spending cap, budget, 60 steps, 15 minutes

On an organization's server, a task Afi runs is **paused with a question to the task's owner, never failed**, when it reaches a limit:

- **Spending:** past the task's cap, the question reads "Afi has spent $0.61 on this · keep going?". The cap is the workstream's **Start without asking** amount when one is set, and **$0.50** otherwise.
- **The workstream's budget**, once it is spent. This is checked before the run spends anything and every few seconds while it runs; raise the budget first, or Afi pauses again.
- **Steps:** at 60 tool calls.
- **Time:** at 15 minutes.

The task moves to its owner's Waiting on you, and its activity says "Paused: …". The owner presses **Keep going**, which starts Afi again on the same conversation with the same allowance again, or **Stop here**, which stops the task. **Tell it…** starts Afi again with what you typed, so you can say how to carry on ("Only the first ten"). None of these is recorded as a workstream decision.

**A model error that may pass** (the provider busy or failing, a dropped connection) is tried once more, 20 seconds later. The activity says "Afi hit a model error and tries again in 20 seconds". A second error, or one that will not pass (a refused key, say), fails the task with its reason.

## task stuck on Running after the server restarted, or pressed Start on a running task and nothing happened

On an organization workstream, Afi's work on a task runs on the organization's server, and the server keeps a note of every run while it goes. **When the server starts again, every task it was working on is settled within about ten seconds; none is left saying Running with nothing behind it.**

- The task's activity says "Interrupted: the server restarted while Afi was working on it", then "Resumed after the restart". Afi carries on from its own conversation for that task, checking what it already wrote before writing again, with the rights of whoever started it.
- A task resumes **once** without anyone pressing anything. If the server restarts during that second run too, the task stays **Interrupted**, under its owner's Waiting on you, with the reason "The server restarted while Afi was working on it. Press Start to run it again."
- A run that stops reporting for a minute while the server is up becomes **Didn't report back**, with "Afi stopped reporting back while working on it. Press Start to run it again."

**Start on a task that says Running with nothing working on it starts it again**: its view shows **Start again** with "Nothing is working on it", and its activity says "Started again by <name>: nothing was working on it". Start on a task Afi really is working on changes nothing.

## deleting a canvas removes it from Files too

An organization canvas moved to the trash no longer appears in live Files. Restoring the canvas brings its file back with its task links. Permanently deleting the canvas removes it from live Files as well.

## delete a workstream task, Undo a deletion, and keep the files it made

Open a task's More menu and choose **Delete**. Only the task's owner or whoever
looks after the workstream can delete it; for an older task owned by Everyone,
anyone who can edit may. Running work must be stopped first: "Stop this task
before deleting it". A task can still have a run finishing behind its displayed
state; wait for it to stop before deleting. Delete is absent on older org servers
that do not support it.

Delete removes the task from Tasks, Work and the Inbox, including its unanswered
questions. Tasks that waited on it stop waiting; their activity gains the dim line
"No longer waiting: “<title>” was deleted". Deleting a prerequisite does not itself
start another task. **Files it made stay in Files**, with their contents intact.
Deleting a standing-order task does not switch off its order. That run stays
deleted after a Personal restart; future runs still make their own tasks.
Deleting a Factory task hides it here; it does not delete the Factory job. Personal
checks the observed job state too. If it cannot read that state, it refuses:
"The Factory isn't answering; read this task again before deleting it".

"Task deleted · files stay in Files" offers **Undo** for ten seconds, like moving
from My space. Undo restores the task and its conversation, and restores the waits
of dependent tasks that have not changed since deletion. It preserves newer edits
and does not restore waits on prerequisites that have since been deleted. Both
stores allow thirty seconds for the Undo request to arrive; after that it is
refused. Afi has no task delete or restore tool: these are a person's actions.
