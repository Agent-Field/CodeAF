# An organization workstream's agent — on a driver's laptop or on the organization's server

## where an organization workstream's agent runs — driver or server, the operator's choice

An organization's workstreams live on its server (`codeaf factory serve`). Where each workstream's agent runs is one setting for the whole organization, `agent_runner`, which everyone can read and only the operator changes (desktop app: Settings › Organizations › the organization, "Where agents run"):

- `driver` — the agent runs on the laptop of the person driving that workstream, with their own model key. One person drives a workstream at a time; the hand lapses 90 seconds after their app stops renewing it. What it spends is posted to the workstream in their name.
- `server` — the organization's server runs every workstream's agent, with the model key the operator gave the server. Nobody drives; choosing `server` lets go of every driver's hand.

The server offers `server` only once it can run agents AND the operator has added a key. Until then the choice is not shown, and choosing it anyway is refused with "the server runs agents with the organization's model key; add one first".

Until the operator picks one, the server picks for them: `server` as soon as it has a model key, `driver` before. Adding the key moves every workstream's agent onto the server (and lets go of every driver's hand) unless the operator had chosen `driver`.

## Driver's laptop — what the agent cannot do on a laptop, and moving it onto the server

On a driver's laptop the agent has none of the workstream's tools. It cannot read or change the workstream's tasks, files or documents, and what it makes stays on that laptop, where nobody else sees it. The app says so wherever it runs there: Settings ("There it cannot read or change the workstream's tasks, files or documents; on the organization's server it can.") and the Afi pane's driving line. For Afi to work on the workstream itself, the operator adds a model key, or chooses "Organization's server".

## the organization's model key — add, replace or remove the key the server uses

Only the operator manages it, in Settings › Organizations › the organization. The key is sent to the server once and is never sent back: the list shows the provider (`openrouter`), the last four characters, who set it and when. No desk ever holds it. Removing it stops the server's agents from taking new messages ("The organization has no model key yet. Its operator adds one in Settings › Organizations.").

Over the wire: `GET /v1/org/keys`, `PUT /v1/org/keys/openrouter {"key"}`, `DELETE /v1/org/keys/openrouter`, all operator-only. A key must be one word of 12 to 512 characters.

## talking to the server's workstream agent — private to you, who can talk, one message at a time, stop it

In `server` mode a workstream's Afi pane is private to you: you have your own conversation with the server's agent, and nobody else in the workstream reads it unless you share it (read only), not your colleagues and not the operator. Its memory is yours too, so it does not repeat what someone else told it. What it does to shared things is visible to everyone as those things: the tasks it makes, the files and documents it writes, and one line on a task when your private request changed it ("Priya asked Afi to keep it under 300 words").

People who can edit the workstream talk to it; people who can only view it have no composer. It answers one of your messages at a time: a second while it works is refused with "the agent is still answering your last message; wait for it, or stop it". Someone else's conversation never makes yours wait. Only you can stop yours; it then says "Stopped.".

A workstream that had one shared conversation before gives each person only their own turns from it; nobody else's are copied.

What it spends is posted to the workstream under the person whose message it answered. A workstream that has spent its budget takes no more messages ("This workstream has spent its budget."). An archived workstream takes none either.

## what the server's workstream agent can and cannot do — tasks, files, documents, canvases, no shell, no repository

On the server the agent, Afi, carries `workstream_pages`, the workstream tools (`ws_tasks`, `ws_decisions`, `ws_task_create`, `ws_task_update`, `ws_task_comment`, `ws_files`, `ws_file_read`, `ws_file_create`, `ws_file_suggest`, `ws_file_comment`; the document about chats inside a workstream says what each does) and, on a server that keeps canvases, `ws_canvas`, which draws the workstream's canvases (the section on Afi drawing a canvas says how). So when you ask it to "turn these asks into tasks", it makes real tasks in the Tasks tab, not documents titled "Task: …", and it reads Tasks before it says who is doing what or what is waiting on you.

With `workstream_pages` it reads the workstream (name, purpose, budget, documents, decisions, sources and links), reads or writes one document, or makes a new document. It does not propose decisions: a decision records itself when an Ask is answered, and nothing waits for a Keep.

It cannot run commands, read or change a repository, open files on a computer, start or stop Factory work, search the web or remember things between workstreams. `driver` mode runs the agent on a person's laptop with that laptop's tools instead, and then it has none of the workstream's.

## can Afi draw a canvas in an organization — slides, social cards, a short plan, posters

Yes. In an organization workstream, the server's Afi draws canvases with `ws_canvas`, the same `canvas` commands and the same canvas guide your own Afi uses on your computer, so an organization's canvas is made to the same standard: the whole set written in one pass, its colours and fonts kept as the canvas's tokens, only the facts you gave it. Ask in the Afi pane ("make a social launch card set", "a short plan for the launch on a canvas"). Anything visual goes on a canvas, never into a document.

A canvas Afi makes is in the workstream's **Files and canvases**, shown as made by Afi for the person who asked, where everyone in the workstream sees it and the canvas updates live for anyone who has it open. Made while Afi does a task, it is under that task's **Result**, and a canvas it only looked at is linked to the task on the canvas itself ("Used by"). Afi can also work on a canvas a person made in the workstream: rename it, add frames, organize its content, set its colours and fonts.

Its limits: it reaches only this workstream's canvases, and only with the rights of the person it is answering (someone who can only view the workstream gets an Afi that can open a canvas but not change it). A frame someone's hand is on keeps Afi's change as a proposal for them, as on a laptop. The server has no screen, so Afi cannot look at a picture of what it drew; it checks the frame's contents instead. It cannot export a canvas, trash or copy one, change who is in it, or bring in a file from the server's disk. A server that has no `canvas` program keeps no canvases, and its Afi has no canvas tool at all.

## does Afi know today's date — the date and time, your time zone, deadlines

Yes. Every message you send tells the server's Afi the date and time where you are: the app sends your computer's time zone with the message, and Afi works out "today", "tomorrow" and every deadline from that date, never from a date it read on a page. If no time zone arrives, it uses the server's own clock and knows that it is the server's.

When Afi changes something for you, it changes only what you asked, and its answer lists every change it made, including any status, rating, owner or date it changed along the way.

## Afi's task cards — where is what Afi planned, nothing happened yet, Afi proposes, Add 2 tasks, untick a card, who is told

On an organization's workstream, Afi proposes smaller sets of tasks in its pane as editable cards, each with a tick, title and fact controls for Done by, Owner, Due and Group. Untick or edit them, then press **Add 2 tasks** (the number ticked). The line names who will be told. Until Add, these cards make no tasks. Once added, the cards show the receipt and cannot be added twice.

Five or more tasks arrive as a named plan in your Inbox instead of cards in the pane. The pane shows one receipt, "A plan of 7 tasks is in your Inbox", with **Open Inbox**. The tasks share a batch group, are owned by the requester and stay Up next until **Start**. The Inbox shows the plan while every task is still Up next; starting one removes it from Plans to start. The receipt follows its tasks: once one has started it reads "The plan of 7 tasks has started" with how far it is ("1 running · 6 up next"), and "The plan of 7 tasks is done" at the end. Afi's own reply under the cards says only what it proposed and who does each, never whether the tasks exist yet or that you must press Add or Start, because the reply stays on the page after you do.

Something only for you, with no date, Afi simply makes and names in its answer. A typo or anything under half an hour it does itself. Your cards are private to you, like the rest of your pane. Afi only proposes in an app that draws the cards; Afi working on its own never proposes.

## Afi made a task on the server — who owns it, who it says made it, what Afi may change

A task Afi makes is owned by the person who asked, unless they named someone else in the workstream or Everyone; "done by" is Afi, and it came from Afi. Lines Afi writes on a thread are signed Afi. Afi changes only work an agent does (its own tasks and chats' tasks); on a person's task or a Factory task it can only comment. An Ask it puts on a task has at most one recommended choice, listed first, and the task then waits on its owner, who alone answers it.

Everything goes through the same rules a person's app uses, with the rights of the person whose message it is answering, checked again on every call: a stale task is refused ("it changed since you read it; read it again"), and someone who cannot edit the workstream cannot change it through Afi.

## who wrote this page — Afi, for the person who asked

A page or document Afi makes is shown in Files as made by Afi for the person who asked; the page's last change is in that person's name, not the steward's. The organization's activity names that person too.

## Afi changed my page — suggestions on the server, nothing lost

Afi rewrites a page directly only when Afi made it and no person has edited it. Any other page, and any page somebody has open for editing, gets Afi's text as a suggestion: it changes nothing in the page and waits for someone who can edit the page to take it. Afi says so in its reply, by the page's name, and never talks about versions or history. The document's header says the same thing to everyone: "Afi suggests edits here; they wait for someone to take them".

When Afi suggests edits to a document (`ws_file_suggest`), they show on the document itself for everyone in the workstream, whether or not someone has it open: each changed passage is marked, with **Keep** and **Drop** beside it, and a bar above the title says how many edits are suggested, with Review one by one, Keep all and Drop all. Keep writes that edit into the page as everyone sees it; Drop removes it. An edit whose words have since changed on the page can no longer be kept, and the bar says so.

## a new organization workstream starts with an Overview page — never empty

When someone makes a workstream on the organization's server and sends no pages with it, the server gives it one document: titled "Overview", of kind overview, with nothing written in it yet. That is the same page the desktop app sends, so a workstream made from the app, from a script or from any other client always has a page people (and the server's agent) can write in. A workstream made with pages of its own keeps exactly those pages, and no Overview is added.

## page actions — no History or Restore in this release

Organization pages have no version-history interface in this release. Page actions does not offer History or Restore this version. Afi's suggested edits are reviewed on the document itself with Keep and Drop; use those actions to take or reject them.

## pages Afi reads appear under Uses on its task

When Afi is working on a task, pages it reads through `workstream_pages` appear under Uses on that task, just like files read through `ws_file_read`. Reading the workstream includes the start of every page, so those pages count as inputs too. Reading one whole page adds that page. Afi's general workstream chat does not attach its reads to a task.
