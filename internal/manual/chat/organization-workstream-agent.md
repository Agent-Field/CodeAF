# An organization workstream's agent — on a driver's laptop or on the organization's server

## where an organization workstream's agent runs — driver or server, the operator's choice

An organization's workstreams live on its server (`codeaf factory serve`). Where each workstream's agent runs is one setting for the whole organization, `agent_runner`, which everyone can read and only the operator changes (desktop app: Settings › Organizations › the organization, "Where agents run"):

- `driver` — the agent runs on the laptop of the person driving that workstream, with their own model key. One person drives a workstream at a time; the hand lapses 90 seconds after their app stops renewing it. What it spends is posted to the workstream in their name.
- `server` — the organization's server runs every workstream's agent, with the model key the operator gave the server. Nobody drives; choosing `server` lets go of every driver's hand.

The server offers `server` only once it can run agents AND the operator has added a key. Until then the choice is not shown, and choosing it anyway is refused with "the server runs agents with the organization's model key; add one first".

## the organization's model key — add, replace or remove the key the server uses

Only the operator manages it, in Settings › Organizations › the organization. The key is sent to the server once and is never sent back: the list shows the provider (`openrouter`), the last four characters, who set it and when. No desk ever holds it. Removing it stops the server's agents from taking new messages ("The organization has no model key yet. Its operator adds one in Settings › Organizations.").

Over the wire: `GET /v1/org/keys`, `PUT /v1/org/keys/openrouter {"key"}`, `DELETE /v1/org/keys/openrouter`, all operator-only. A key must be one word of 12 to 512 characters.

## talking to the server's workstream agent — who can talk, who can only read, one message at a time, stop the agent while it is still answering someone else

In `server` mode a workstream's Agent panel is one conversation everyone who can see the workstream reads. People who can edit the workstream talk to it; people who can only view it read along with no composer. The agent answers one message at a time: a second message while it works is refused with "the agent is still answering santosh; wait for it, or stop it". Anyone who can edit may stop it; the conversation then says "Stopped by abir.".

What it spends is posted to the workstream under the person whose message it answered. A workstream that has spent its budget takes no more messages ("This workstream has spent its budget."). An archived workstream takes none either.

## what the server's workstream agent can and cannot do — workstream_pages, no shell, no repository

On the server the agent carries one tool, `workstream_pages`, and nothing else:

- read the workstream: name, purpose, stage, budget, pages, decisions, sources and links;
- read one page whole;
- write a page's words, or make a new page;
- propose a decision (a person keeps it).

It writes as the person who asked, and their rights are checked on every write. A page edited live (one somebody has opened for editing) is never written over: the agent's text is kept as a new version in that page's History, marked Kept · not applied, and anyone with edit applies it with Restore this version (next section).

It cannot run commands, read or change a repository, open files, start or stop factory jobs, use a computer, search the web or remember things between workstreams. For any of that, use `driver` mode, where the agent runs on a person's laptop with their tools.

## a new organization workstream starts with an Overview page — never empty

When someone makes a workstream on the organization's server and sends no pages with it, the server gives it one page: titled "Overview", of kind overview, with nothing written in it yet. That is the same page the desktop app sends, so a workstream made from the app, from a script or from any other client always has a page people (and the server's agent) can write in. A workstream made with pages of its own keeps exactly those pages, and no Overview is added.

## the agent's text was kept, not applied — page History, current, Kept · not applied, Restore this version

When the server's agent writes to a page that is edited live, its text does not go into the page. It is kept as the newest version in the page's History, and the agent says so: "your text was not applied: it was kept as version N in the page's History, marked Kept · not applied".

History marks **current** on the version the page actually holds, which the organization's server names (`live_version` on the page's versions). That is never simply the highest number: a kept version can be newer than the page. A kept version's row reads **Kept · not applied** and is not current.

Choosing a kept version shows its words, and **Restore this version** applies them. The restore goes into the live page as one edit, so everyone with the page open sees it at once, and it becomes the newest version and the current one. After a restore the kept row no longer says Kept · not applied. Restoring needs edit on the page, and the page open in your window.

Versions a person's editor keeps (after a pause, or on request) and restores are always ones the page held. A Personal workstream has no kept versions: there, current is the newest version.
