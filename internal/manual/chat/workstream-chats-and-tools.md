# Chats inside a workstream, and the tools an agent there uses on its tasks and files

## a chat inside a workstream is private — what is private and what is shared

A chat you start inside a workstream (the box at the bottom of its Chats tab, "Start a new chat in <workstream>") is yours alone: nobody else in the workstream sees it or what is said in it. What it makes is shared. Its tasks appear in the workstream's Tasks for everyone, with the chat's name as who does them, and its files appear in Files with the chat's name as who made them. The Chats tab says so under the list: "Only you see your chats. The tasks and files they make are shared with everyone in <workstream>."

The same holds for Afi's pane beside the workstream: it is private to you, on a Personal workstream and on an organization's, including when the organization runs its agents on its server. Anything you type to an agent is private; anything on a task or a file is shared.

## the Chats tab — Afi first, Waiting on you, Working, Resting

The Chats tab lists your own chats in this workstream. Afi is always first, under "Always here"; its row opens Afi's pane. The rest sit in three sections, in this order: **Waiting on you** (the chat asked you something; its face has an amber head), **Working** (something of its is running) and **Resting** (nothing of its is running). Each row shows the chat's face, its name, its latest line and when it last changed. A chat rests when nothing of its is running and wakes when you write to it or a task of its needs you. An empty tab says "No chats yet · start one below".

## opening a chat in place, or as its own tab — Open as its own tab, back to Chats

A click on a chat opens it in place, inside the Chats tab, with Afi still beside it. The bar above it has "Chats" (back to the list), the chat's face and name, "Private to you", and "Open as its own tab". The box at the bottom writes to that chat only.

"Open as its own tab" opens the same conversation as a tab whose path reads Work › <workstream> › Chats › <chat>. Its right panel has two tabs, Tasks and Files, listing only what this chat made; a row opens that task or file. Pressing Afi swaps the panel for Afi's pane. A chat is open in one place at a time: opening it as a tab returns the workstream to its list of chats. Leaving a chat or closing its tab stops nothing; it keeps working.

## the workstream tools — ws_tasks, ws_task_create, ws_task_update, ws_task_comment, ws_files, ws_file_read, ws_file_create, ws_file_suggest

Every chat started inside a workstream, and Afi when it is inside one, carries eight tools onto the workstream's shared work:

- `ws_tasks` lists the tasks, or reads one whole with its activity and comments.
- `ws_task_create` makes a task; `ws_task_update` changes one this chat does (its state, its result, the check against the request, an Ask for its owner); `ws_task_comment` writes on a task's thread.
- `ws_files` lists files and folders; `ws_file_read` reads one; `ws_file_create` makes a document, folder or link; `ws_file_suggest` suggests edits to a document.

What a chat or Afi makes for the workstream is saved with `ws_file_create` and appears in Files for everyone in it; it is never written to disk as a loose file.

On a Personal workstream the desktop app serves them. On an organization whose agents run on its server, Afi there carries all eight beside `workstream_pages`, with the same rules; the page about an organization workstream's agent says what is different there.

## does every chat turn into a task — when an agent makes a task, and when it does not

No. Not everything said in a chat becomes a task. An agent makes a task for work that runs beyond its reply, changes a file, or matters to someone else, and it says so in its reply. A quick answer stays in the chat. Before making a task it reads the workstream's tasks, and when one already covers the work it points you to that task instead of starting a duplicate.

A task's request says only what is to be done, in a few lines. The agent never copies your private conversation into it. You own the tasks you ask for unless you name someone else or Everyone.

## why the agent did not do what a comment asked — offers under a comment

An agent never acts on a comment. A comment on a task or in a file is people talking to people, so there is no @-mention that gives an agent an order. When a comment looks like a request, the agent adds an offer under it ("Afi can add this to the draft") with one button; anyone in the workstream can take it, and the task's owner is told and can undo it for five seconds.

## the agent changed my document — suggestions, and when it edits directly

Agents change a document by suggestion: nothing changes until someone in the workstream accepts it, and anyone can accept or reject one. An agent rewrites a document directly only when it made that document and no person has edited it since. Two chats changing the same document both arrive as suggestions. On an organization's server, a suggestion from Afi is a version in the document's History marked Kept · not applied, and accepting it is Restore this version.

## "Priya asked Afi to keep it under 300 words" — the one line a private request leaves

When something you asked an agent privately changes shared work, the task's thread gets ONE line naming who asked for what, such as "Priya asked Afi to keep it under 300 words". The conversation around it stays private. A private answer is shared only when you post it.

## big work — five or more tasks, a batch, a named chat, a plan with Start

When Afi is given big work that splits into five or more tasks, it starts a named chat to run it and stays free for quick asks. The tasks share one group with the same name as that chat and arrive in Up next as a plan with "Start" and "Change the plan"; nothing starts until a person presses Start. Afi tells you with a button, "Open <name>", that opens the chat. In the Chats tab that chat's row says "Started by Afi". On an organization whose agents run on its server, Afi gives the tasks the one group in Up next, but there it starts no separate chat, and nothing yet works an organization's task by itself: each waits in Up next until its owner marks it done or stops it.

## did the agent save it to the Library — what a workstream's chat makes goes to its Files, not the Library or disk

A conversation that belongs to a workstream (its Afi, or one of its chats) keeps what it makes in that workstream: a brief, a comparison, a plan or a list is saved with ws_file_create and appears in the workstream's Files, where everyone in it sees it. It is not written to disk as a loose file, and it does not reach the app's Library. The Library is a separate shelf: something made in a workstream gets there only when a person adds it with Add to Library. What such a conversation says is also never captured into the Library by itself, whatever the Library's own setting is.

Such a conversation cannot set up work on a clock either. Asked to do something every Monday or each morning, it says to set it up under People and rules › Keeps running, where the workstream's standing orders live, instead of making one itself.
