# A workstream's People and rules — who is in it, workstreams inside workstreams and start without asking

## People and rules — the sheet behind People in a workstream's header

Every workstream has one People and rules sheet, opened from People in its header (below 1100 points wide, from More › People). It shows, in order: the people and teams in it, "Owner of incoming work", "If unanswered for a day", "Start without asking", "Afi asks before publishing or sending", and "Keeps running" (the Factory and each standing order, each with a switch). Every change applies at once and can be undone for five seconds; there is no Save button, and Done only closes the sheet.

Only the person who looks after the workstream (its steward — the person who made it, until they hand it on) changes this sheet. Everyone else in it sees the same sheet, read-only. Asking to change it when you do not look after it is refused with "only the person who looks after this workstream can change its rules or hand it on".

## adding people or a team to a workstream — add a team, everyone in it now and later

"Add people or a team…" picks persons and teams of your organization. Adding a team adds everyone in it, now and later: someone who joins Engineering next month is in every workstream Engineering is in, with nothing else to do. A team is a group of people, not a place; one workstream can have several teams, and making a workstream never asks "which team".

## removing a person from a workstream — what happens to someone's open tasks when I remove them from a workstream

Removing a person (or a team they were in through) offers their open tasks in that workstream, and in every workstream inside it, to you, the person who removed them: each becomes yours, its thread says "Handed to <you>", and you can hand it on from there. Finished work keeps who did it. Someone who takes themselves out leaves their tasks to the fallback person, or with none set, to the person who looks after the workstream.

## the amber dot and number on the Work list — how many things are waiting on me in each workstream

The Work list on the left shows the workstreams you are in. A workstream where something waits on you shows an amber dot and a number after its name: "Pantry • 3" means three of its tasks wait on you, the same tasks the Inbox lists under Waiting on you. A task waits on you when its question is yours to decide, when it has waited a day and you are the fallback person ("If unanswered for a day"), or when it is your task and nothing is working on it any more: **Interrupted** or **Didn't report back**, until you press Start again. A Failed task is not amber; the Work header counts it apart, as in "7 running · 1 failed". A question you put off with Later stops counting until then; answering one takes it off at once.

Nothing waiting draws nothing: no dot, no "0". Amber means only "waiting on you"; a workstream that is busy, failed or someone else's turn is never amber. In an organization's window the numbers are that organization's; Personal counts only Personal. An organization whose server does not keep tasks shows no numbers.

## owner of incoming work, and the fallback person — who is asked

"Owner of incoming work" owns tasks nobody asked for directly, such as ones the Factory creates: a named person. Everyone is no longer offered there, because a task has one owner; a workstream that already has Everyone keeps it until someone picks a person (then the first person to answer decides, and the task records who answered). It starts as the person who made the workstream. The fallback person is also asked when a task's question waits unanswered (see "who answers when the decider is away"), and takes the open tasks of anyone who leaves by themselves.

## who answers when the decider is away — on holiday, out sick, not answering, a stand-in, cover for someone

Every Ask has exactly one decider. If they do not answer, the workstream's fallback person answers for them: People and rules says it as "Santosh answers what waits unanswered for 1 day". The wait is half a day, 1 day (the default), 2 days or 3 days; only the person who looks after the workstream changes it, and the change is kept in Decisions ("Santosh answers what waits unanswered for 12 hours").

Once an Ask has waited that long it reaches the fallback person's Inbox under Waiting on you as well as the decider's, and either may answer; the first answer decides, and the decision records who gave it. Before the wait is up only the decider can answer. With no fallback person set, an Ask simply waits for its decider. There is no "away" switch: the wait is the same whether the decider is on holiday or just busy.

## start without asking — how much an agent may spend before it must ask

"Start without asking" is one amount per task: the most an agent may spend on one task before it must ask its owner. $0 means never start without asking — every task waits for a person. It replaces the older go-ahead allowances and the autonomy setting, which no longer appear on a workstream.

On an organization, the amount is honoured by the organization's server with no app open: it keeps one go-ahead over the workstream's Factory jobs, holding each job's budget to the amount, rewritten whenever the amount or the jobs change.

## what "asks first" means on a plan — the rule Afi asks before publishing or sending

Every workstream has the rule "Afi asks before publishing or sending", on unless its steward turns it off in People and rules. It binds every agent doing a task there (Afi or a chat): cheap, reversible work it does and lists under Assumed; anything a task publishes, sends, posts, replies to in public or pays for, it makes ready and then asks the task's owner before doing it. The Ask lands on the task, under Waiting on you.

Which tasks will ask is known before anything runs: a task done by an agent whose title or request says it posts, publishes, sends, replies or pays "asks first" (for example "Post the notes to Discord" or "Reply to the three GitHub issues"), and its run is told to stop for you at that step. Writing a draft, an email's text or a blog post does not ask; sending or posting it does. A task you or a colleague do, or the Factory does (it keeps its own approvals), never asks first. Ask Afi "which of these will ask me first?" and it answers from the plan.

Turning the rule off reads "Afi publishes and sends without asking", and either change is kept in Decisions. Off, nothing asks first, though a task that goes against a decision still asks.

## when Afi and chats ask first — published, sent or paid for; a shared file; every task

The ask rule has three settings, chosen in People and rules by the person who looks after the workstream:

- **Before anything is published, sent or paid for** (the default). Everything else is done and listed under Assumed.
- **Before anything that changes a shared file.** Also asks before a task edits, updates, rewrites, renames or deletes something already in the workstream.
- **Before every task starts.** Each agent task first asks its owner, with how it means to do the work as the recommended answer.

The rule line reads "Afi asks before publishing or sending", "Afi asks before anything that changes a shared file" or "Afi asks before every task starts", and each change is kept in Decisions. Whatever the setting, a task that goes against a decision still asks.

## Afi decides without asking — exceptions to the ask rule, fewer Asks, stop asking me about

The way to fewer Asks is a looser rule, not a longer Inbox. Every Ask an agent raises carries a kind, a few words for what sort of question it is ("docs wording", "release date"). People and rules lists, under **Afi decides without asking**, each kind the workstream has let go.

When an agent would ask a kind on that list, it does not ask: it goes ahead with the answer it would have recommended, and the task lists it under Assumed, as "Keep this wording? · Keep Afi's wording (Afi decides docs wording here without asking)". Send back still overturns it. An Ask with no recommended answer is still asked, a person's Ask is never decided for them, and the questions codeaf puts itself (a task that cannot be done, a changed file, a decision conflict, an invite nobody accepted) are never let go. Removing a kind from the list makes Afi ask about it again; both are kept in Decisions.

## Afi suggests loosening the rule — too many Asks waiting, Let Afi decide … here?, Yes, Not now

When more than 10 Asks are waiting in one workstream and at least 3 of them share a kind, People and rules shows the person who looks after it a suggestion from Afi: "Let Afi decide docs wording here?", with its evidence ("Six of your last ten answers about docs wording took Afi's recommendation unchanged.") and two buttons.

**Yes, stop asking about docs wording** adds the kind to "Afi decides without asking". **Not now** hides that suggestion for 7 days. Afi never suggests a kind you mostly answered differently from its recommendation, and never changes the rule by itself. Nobody else in the workstream sees the suggestion.

## the Factory switch and standing orders in a workstream — what works here without anyone asking

"Keeps running" lists what works in the workstream without anyone asking. The Factory switch says whether the Factory turns new issues into tasks here. Each standing order belonging to the workstream has its own row — when it runs ("Every Monday at 09:00"), then what it does and who owns its tasks — and one switch. Switching it off pauses it; switching it on again resumes it. Each switch says itself in a note with Undo for five seconds. An order that belongs to no workstream runs exactly as it always did.

On an organization, the server does the Factory switch's work with no app open: each issue opened after the switch went on joins the workstream, owned by the "Owner of incoming work". A workstream listing the issue's repository among its sources takes it first, then one listing no repository; the oldest wins a tie. Its labels never start it: "Start without asking" does, within that amount, or it waits for its owner.

Before an order is switched on, its row opens a preview: the next three times it will run, and the task each run adds — its title, that Afi does it, and its owner (the workstream's "Owner of incoming work"). The switch inside the preview is what turns it on.

## adding a standing order to a workstream — every Monday, each morning, the 1st of the month

"Add a standing order…" at the foot of People and rules opens two questions: "What to do", in your own words, and "When": Each morning, Every weekday, Every week (pick the day), Every month (pick the day of the month), or Every hour, with the time of day. The preview under them shows the next three runs and the task each run will add. "Switch on" saves it and turns it on; Cancel keeps nothing. Only the person who looks after the workstream adds one.

The sheet sets a schedule. It cannot start an order on an event, such as "when a release ships": nothing in the sheet watches for one, so it is not offered.

In Personal, an order runs on your computer while codeaf is open there; what fell due while it was closed runs once, late, when it opens. An organization's workstream is different: its server keeps and runs them (next section).

## standing orders in an organization's workstream — kept and run by its server, never your computer

In an organization's workstream, standing orders are kept and run by the organization's server. Everyone who can see the workstream sees them under "Keeps running"; only the person who looks after it adds, switches or removes one. They run while every member's computer is off.

Each run is the workstream's own agent on the server, with only the workstream's tools (its tasks, files and documents): no shell, and nothing on anybody's computer. It asks the owner of incoming work "Run … now?" on its task unless "Start without asking" covers what one run may spend; Go starts it as whoever said go, and the task's Stop ends it. A run missed while the server was down runs once, late.

The server runs them only when the operator has given it a model key. Without one, "Add a standing order…" is absent and the sheet says "Standing orders run on the organization's server, which needs a model key from the operator."

A standing order is never run on a person's own computer for an organization: codeaf refuses it there ("an organization's standing orders run on its server, never on this computer"), and an organization's order made on a computer before 2026-11-17 no longer runs.

## a standing order's tasks — Came from, the group in Tasks, and when it asks

Each run of a workstream's standing order adds one task to it. The task says where it came from on its document ("From the standing order Every Monday at 09:00"), is done by Afi, and is owned by the workstream's "Owner of incoming work". In Tasks, an order's tasks fold into one group named after it, so forty runs never bury the work people added by hand.

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

- **Tasks still running.** "Archive Launch plan?" says how many stop ("2 tasks are running; they stop."), with Cancel and Archive. An agent's task paused on someone's answer counts as running. Archive stops them the way a task's own Stop does: each shows "Stopped by you". Tasks that are up next, and a person's task waiting on someone, keep their state.
- **A workstream inside another.** Under "Keep these files in Pantry:" it lists what it holds, every one ticked; untick what Pantry should not show. Folders and its own Decisions are not offered. Nothing moves: each ticked one stays in the archived workstream, and Pantry gains a shortcut to it.

The toast then says what happened, for example "Archived Meal planner · 3 files kept in Pantry". Undo brings the workstream back; tasks it stopped stay stopped, and the shortcuts stay in the parent.

Delete appears in More instead of Archive only for a workstream that holds nothing at all: no tasks, nothing in Files. Any other workstream is archived, never deleted. Folding a workstream back into a heading of its parent is not offered in the app.

## my computer is asleep or offline — is the work lost? no internet, connection refused, message not sent

No. When the computer running the agents is asleep or offline (this Mac's engine stopped, or an organization's server cannot be reached), the workstream keeps showing what it last read, and the line under its name says so: "This Mac is offline · answers send when it is back; work is paused, not lost" (or "The organization's server is offline · …"). Its tasks, files, Asks and decisions are all kept. An agent's turn that was running stops with the computer; when the computer is back, the task is still there, and a task that did not finish can be started again from its document. Closing the app stops the turns it was running on this computer (an organization's server keeps working without it); such a task reads "Interrupted: the app closed while this was running", and Start runs it again.

A message sent while this computer cannot reach the model is kept, not failed: it reads "Paused · can't reach the model from this computer" and "It sends again by itself when the connection is back.", in grey, and is not counted as failed. The chat looks again on a slowing clock (30 seconds, then up to every five minutes) and sends it as soon as the model answers; Retry message sends it at once. Quitting and reopening the app keeps it, with Retry message still offered.

## Fold a workstream into another, and Undo

On a workstream's own tabs, More → **Fold…** opens a sheet to choose another open workstream in the same space. Fold moves its tasks, folders, files and decisions into the destination. Your chats follow on this desk and remain private. The destination keeps its people, rules and existing launch plan. Undo returns the source's saved plan too. The folded workstream disappears from Work and Archived. In an organization the destination's Decisions gains one line saying who folded what into it ("Marco folded “Launch swag” into this workstream"), and Undo takes that line away with the rest.

Stop working tasks first. If the source contains other workstreams, fold those first. You must look after both workstreams in an organization; older servers without Fold do not offer it.

The toast's **Undo** returns the moved work and the chats to the original workstream. If work changed after folding, Undo refuses rather than replacing it: "work changed after folding; Undo would replace that work". Nothing is discarded by that refusal.

## Bring along a chat's files and attachments, or move into an organization

Bringing a chat into a workstream also carries its recorded outputs and message attachments, files made for its tasks, and files attached as those tasks' inputs. A chat that began outside any workstream brings these into Files too. Attachments keep their original maker. Their task links remain on the files in the destination's Files. Folders and deleted files are not a chat's work.

Moving a Personal workstream into an organization carries its Files, including upload bytes, documents, canvases and task links, together with its non-Factory tasks and their threads. A Personal chat brought into an organization's workstream carries just that chat's tasks and files. The chat conversation stays private on this desk and appears in the destination's Chats. Factory jobs stay where they ran. Each upload keeps the normal 25 MiB limit; the import request, including base64 and metadata, is bounded at 36 MiB. An unreadable or oversized upload prevents the move; it is never replaced with a path the organization's server cannot open.
