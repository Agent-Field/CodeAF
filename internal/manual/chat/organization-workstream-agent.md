# An organization workstream's agent — on a driver's laptop or on the organization's server

## where an organization workstream's agent runs — driver or server, the operator's choice

An organization's workstreams live on its server (`codeaf factory serve`). Where each workstream's agent runs is one setting for the whole organization, `agent_runner`, which everyone can read and only the operator changes (desktop app: Settings › Organizations › the organization, "Where agents run"):

- `driver` — the agent runs on the laptop of the person driving that workstream, with their own model key. One person drives a workstream at a time; the hand lapses 90 seconds after their app stops renewing it. What it spends is posted to the workstream in their name.
- `server` — the organization's server runs every workstream's agent, with the model key the operator gave the server. Nobody drives; choosing `server` lets go of every driver's hand.

The server offers `server` only once it can run agents AND the operator has added a key. Until then the choice is not shown, and choosing it anyway is refused with "the server runs agents with the organization's model key; add one first".

## the organization's model key — add, replace or remove the key the server uses

Only the operator manages it, in Settings › Organizations › the organization. The key is sent to the server once and is never sent back: the list shows the provider (`openrouter`), the last four characters, who set it and when. No desk ever holds it. Removing it stops the server's agents from taking new messages ("The organization has no model key yet. Its operator adds one in Settings › Organizations.").

Over the wire: `GET /v1/org/keys`, `PUT /v1/org/keys/openrouter {"key"}`, `DELETE /v1/org/keys/openrouter`, all operator-only. A key must be one word of 12 to 512 characters.

## talking to the server's workstream agent — private to you, who can talk, one message at a time, stop it

In `server` mode a workstream's Afi pane is private to you: you have your own conversation with the server's agent, and nobody else in the workstream reads it, not your colleagues and not the operator. Its memory is yours too, so it does not repeat what someone else told it. What it does to shared things is visible to everyone as those things: the tasks it makes, the files and pages it writes, the decisions it suggests, and one line on a task when your private request changed it ("Priya asked Afi to keep it under 300 words").

People who can edit the workstream talk to it; people who can only view it have no composer. It answers one of your messages at a time: a second while it works is refused with "the agent is still answering your last message; wait for it, or stop it". Someone else's conversation never makes yours wait. Only you can stop yours; it then says "Stopped.".

A workstream that had one shared conversation before gives each person only their own turns from it; nobody else's are copied.

What it spends is posted to the workstream under the person whose message it answered. A workstream that has spent its budget takes no more messages ("This workstream has spent its budget."). An archived workstream takes none either.

## what the server's workstream agent can and cannot do — tasks, files, pages, no shell, no repository

On the server the agent, Afi, carries `workstream_pages` and the eight workstream tools (`ws_tasks`, `ws_task_create`, `ws_task_update`, `ws_task_comment`, `ws_files`, `ws_file_read`, `ws_file_create`, `ws_file_suggest`; the page about chats inside a workstream says what each does). So when you ask it to "turn these asks into tasks", it makes real tasks in the Tasks tab, not pages titled "Task: …", and it reads Tasks before it says who is doing what or what is waiting on you.

With `workstream_pages` it reads the workstream (name, purpose, budget, pages, decisions, sources and links), reads or writes one page, or makes a new page. It does not propose decisions: a decision records itself when an Ask is answered, and nothing waits for a Keep.

It cannot run commands, read or change a repository, open files on a computer, start or stop Factory work, search the web or remember things between workstreams. For any of that, use `driver` mode, where the agent runs on a person's laptop with their tools.

## does Afi know today's date — the date and time, your time zone, deadlines

Yes. Every message you send tells the server's Afi the date and time where you are: the app sends your computer's time zone with the message, and Afi works out "today", "tomorrow" and every deadline from that date, never from a date it read on a page. If no time zone arrives, it uses the server's own clock and knows that it is the server's.

When Afi changes something for you, it changes only what you asked, and its answer lists every change it made, including any status, rating, owner or date it changed along the way.

## Afi made a task on the server — who owns it, who it says made it, what Afi may change

A task Afi makes is owned by the person who asked, unless they named someone else in the workstream or Everyone; "done by" is Afi, and it came from Afi. Lines Afi writes on a thread are signed Afi. Afi changes only work an agent does (its own tasks and chats' tasks); on a person's task or a Factory task it can only comment. An Ask it puts on a task has at most one recommended choice, listed first, and the task then waits on its owner, who alone answers it.

Everything goes through the same rules a person's app uses, with the rights of the person whose message it is answering, checked again on every call: a stale task is refused ("it changed since you read it; read it again"), and someone who cannot edit the workstream cannot change it through Afi.

## who wrote this page — Afi, for the person who asked

A page or document Afi makes is shown in Files as made by Afi for the person who asked; the page's last change is in that person's name, not the steward's. The organization's activity names that person too.

## Afi changed my page — suggestions on the server, nothing lost

Afi rewrites a page directly only when Afi made it and no person has edited it. Any other page, and any page somebody has open for editing, gets Afi's text as a suggestion: it changes nothing in the page and waits for someone who can edit the page to take it. Afi says so in its reply, by the page's name, and never talks about versions or history. The document's header says the same thing to everyone: "Afi suggests edits here; they wait for someone to take them".

## a new organization workstream starts with an Overview page — never empty

When someone makes a workstream on the organization's server and sends no pages with it, the server gives it one page: titled "Overview", of kind overview, with nothing written in it yet. That is the same page the desktop app sends, so a workstream made from the app, from a script or from any other client always has a page people (and the server's agent) can write in. A workstream made with pages of its own keeps exactly those pages, and no Overview is added.

## the agent's text was kept, not applied — page History, current, Kept · not applied, Restore this version

When the server's agent writes to a page a person made or is editing live, its text does not go into the page. It waits as a suggestion, and the agent says so: "your text was not applied: it waits as a suggestion for someone who can edit the page to take it". Underneath, the page's History menu still lists it as a version marked Kept · not applied.

History marks **current** on the version the page actually holds, which the organization's server names (`live_version` on the page's versions). That is never simply the highest number: a kept version can be newer than the page. A kept version's row reads **Kept · not applied** and is not current.

Choosing a kept version shows its words, and **Restore this version** applies them. The restore goes into the live page as one edit, so everyone with the page open sees it at once, and it becomes the newest version and the current one. After a restore the kept row no longer says Kept · not applied. Restoring needs edit on the page, and the page open in your window.

Versions a person's editor keeps (after a pause, or on request) and restores are always ones the page held. A Personal workstream has no kept versions: there, current is the newest version.
