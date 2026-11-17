# A workstream's People and rules — who is in it, workstreams inside workstreams and start without asking

## People and rules — the sheet behind People in a workstream's header

Every workstream has one People and rules sheet, opened from People in its header (below 1100 points wide, from More › People). It shows, in order: the people and teams in it, "Owner of incoming work", "If unanswered for a day", "Start without asking", and "Keeps running" (the Factory and each standing order, each with a switch). Every change applies at once and can be undone for five seconds; there is no Save button, and Done only closes the sheet.

Only the person who looks after the workstream (its steward — the person who made it, until they hand it on) changes this sheet. Everyone else in it sees the same sheet, read-only. Asking to change it when you do not look after it is refused with "only the person who looks after this workstream can change its rules or hand it on".

## adding people or a team to a workstream — add a team, everyone in it now and later

"Add people or a team…" picks persons and teams of your organization. Adding a team adds everyone in it, now and later: someone who joins Engineering next month is in every workstream Engineering is in, with nothing else to do. A team is a group of people, not a place; one workstream can have several teams, and making a workstream never asks "which team".

## removing a person from a workstream — what happens to their open tasks

Removing a person (or a team they were in through) hands their open tasks in that workstream, and in every workstream inside it, to the fallback person ("If unanswered for a day"). With no fallback set, they go to the person who looks after the workstream.

## the amber dot and number on the Work list — what is waiting on me in each workstream

The Work list on the left shows the workstreams you are in. A workstream where something waits on you shows an amber dot and a number after its name: "Pantry • 3" means three of its tasks wait on you, the same tasks the Inbox lists under Waiting on you. A task waits on you when its question is yours to decide, or when it has waited a day and you are the fallback person ("If unanswered for a day"). A question you put off with Later stops counting until then; answering one takes it off at once.

Nothing waiting draws nothing: no dot, no "0". Amber means only "waiting on you"; a workstream that is busy, failed or someone else's turn is never amber. In an organization's window the numbers are that organization's; Personal counts only Personal. An organization whose server does not keep tasks shows no numbers.

## owner of incoming work, and the fallback person — who is asked

"Owner of incoming work" owns tasks nobody asked for directly, such as ones the Factory creates: a named person, or Everyone (the first person to answer decides, and the task records who answered). It starts as the person who made the workstream. "If unanswered for a day" names a person who is also asked when a task's question has waited a day, and who takes the open tasks of anyone removed.

## start without asking — how much an agent may spend before it must ask

"Start without asking" is one amount per task: the most an agent may spend on one task before it must ask its owner. $0 means never start without asking — every task waits for a person. It replaces the older go-ahead allowances and the autonomy setting, which no longer appear on a workstream.

On an organization, the amount is honoured by the organization's server with no app open: it keeps one go-ahead over the workstream's Factory jobs, holding each job's budget to the amount, rewritten whenever the amount or the jobs change.

## the Factory switch and standing orders in a workstream — what works here without anyone asking

"Keeps running" lists what works in the workstream without anyone asking. The Factory switch says whether the Factory turns new issues into tasks here. Each standing order belonging to the workstream has its own row — when it runs ("Every Monday at 09:00"), then what it does and who owns its tasks — and one switch. Switching it off pauses it; switching it on again resumes it. Each switch says itself in a note with Undo for five seconds. An order that belongs to no workstream runs exactly as it always did.

Before an order is switched on, its row opens a preview: the next three times it will run, and the task each run adds — its title, that Afi does it, and its owner (the workstream's "Owner of incoming work"). The switch inside the preview is what turns it on.

## adding a standing order to a workstream — every Monday, each morning, the 1st of the month

"Add a standing order…" at the foot of People and rules opens two questions: "What to do", in your own words, and "When": Each morning, Every weekday, Every week (pick the day), Every month (pick the day of the month), or Every hour, with the time of day. The preview under them shows the next three runs and the task each run will add. "Switch on" saves it and turns it on; Cancel keeps nothing. Only the person who looks after the workstream adds one.

The sheet sets a schedule. It cannot start an order on an event, such as "when a release ships": nothing in the sheet watches for one, so it is not offered.

In Personal, an order runs on your computer while codeaf is open there; what fell due while it was closed runs once, late, when it opens. An organization's workstream is different: its server keeps and runs them (next section).

## standing orders in an organization's workstream — kept and run by its server, never your computer

In an organization's workstream, standing orders are kept and run by the organization's server. Everyone who can see the workstream sees them under "Keeps running"; only the person who looks after it adds, switches or removes one. They run while every member's computer is off.

Each run is the workstream's own agent on the server, with only the workstream's tools (its tasks, files and pages): no shell, and nothing on anybody's computer. It asks the owner of incoming work "Run … now?" on its task unless "Start without asking" covers what one run may spend; Go starts it as whoever said go, and the task's Stop ends it. A run missed while the server was down runs once, late.

The server runs them only when the operator has given it a model key. Without one, "Add a standing order…" is absent and the sheet says "Standing orders run on the organization's server, which needs a model key from the operator."

A standing order is never run on a person's own computer for an organization: codeaf refuses it there ("an organization's standing orders run on its server, never on this computer"), and an organization's order made on a computer before 2026-11-17 no longer runs.

## a standing order's tasks — Came from, the group in Tasks, and when it asks

Each run of a workstream's standing order adds one task to it. The task says where it came from on its page ("From the standing order Every Monday at 09:00"), is done by Afi, and is owned by the workstream's "Owner of incoming work". In Tasks, an order's tasks fold into one group named after it, so forty runs never bury the work people added by hand.

When "Start without asking" covers what one run may spend, the order runs alone. Otherwise each run first asks its owner "Run … now?" on its task, with Go, Skip this one and Not right; that reaches the owner's Inbox like any question. A run that ends needing a person's call asks on its task too. A run that did not finish shows Failed with its reason; one you skip or stop shows Stopped by you.

## a workstream inside a workstream — nesting, one level only, and what it shares with its parent

A workstream can sit inside one other workstream (Meal planner inside Pantry), and the nesting stops there: a workstream that is already inside another cannot be a parent, and one that has workstreams inside it cannot move inside another. Making one goes through New workstream: its card's "Inside" line offers only top-level workstreams.

A workstream inside another:

- always has its parent's people. Its sheet says "Same people as Pantry" and its people cannot be changed there; change them on the parent and they change on every workstream inside it.
- reads its parent's files, shown as "From Pantry".
- starts with a copy of its parent's rules and may change its own copy; changing it never changes the parent's.
- keeps its own tasks and chats.

## archiving a workstream — what it asks first, running tasks that stop, and what stays in the parent

Archive is in the workstream's More menu (…). It takes the workstream off the Work list; its tasks and documents stay readable and searchable, and Bring back in More undoes it.

Archive asks first only when it has to; otherwise it archives at once, with Undo for 5 seconds:

- **Tasks still running.** The question says how many will stop ("2 running tasks will stop."). Archive stops them the way a task's own Stop does: each shows "Stopped by you". Tasks that are up next or waiting keep their state.
- **A workstream inside another.** Under "Keep these files in Pantry:" it lists what it holds, every one ticked; untick what Pantry should not show. Folders and its own Decisions are not offered. Nothing moves: each ticked one stays in the archived workstream, and Pantry gains a shortcut to it.

The toast then says what happened, for example "Archived Meal planner · 3 files kept in Pantry". Undo brings the workstream back; tasks it stopped stay stopped, and the shortcuts stay in the parent.

Delete appears in More instead of Archive only for a workstream that holds nothing at all: no tasks, nothing in Files. Any other workstream is archived, never deleted. Folding a workstream back into a heading of its parent is not offered in the app.

## my computer is asleep or offline — is the work lost? no internet, connection refused, message not sent

No. When the computer running the agents is asleep or offline (this Mac's engine stopped, or an organization's server cannot be reached), the workstream keeps showing what it last read, and the line under its name says so: "This Mac is offline · answers send when it is back; work is paused, not lost" (or "The organization's server is offline · …"). Its tasks, files, Asks and decisions are all kept. An agent's turn that was running stops with the computer; when the computer is back, the task is still there, and a task that did not finish can be started again from its page. Closing the app stops the turns it was running; such a task reads "Interrupted: the app closed while this was running", and Start runs it again.

A message sent while this computer cannot reach the model is kept, not failed: it reads "Paused · can't reach the model from this computer" and "It sends again by itself when the connection is back.", in grey, and is not counted as failed. The chat looks again on a slowing clock (30 seconds, then up to every five minutes) and sends it as soon as the model answers; Retry message sends it at once. Quitting and reopening the app keeps it, with Retry message still offered.
