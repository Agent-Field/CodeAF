# Chats inside a workstream, and the tools an agent there uses on its tasks and files

## a chat inside a workstream is private — what is private and what is shared, can others see my chat's name

Use **New chat** in the Chats header to open a private conversation. What you and the chat say stays private to you. Its tasks and files are shared in the workstream. The Chats tab lists your own chats; another person's chat appears there only once they share it with you. The organization's server lists a chat to anyone else only after it is shared with them (see "Are chats private by default" in the organization sharing page); until then not even its name leaves you, and the operator does not see it either.

| What | Who sees it |
| --- | --- |
| What you and a chat say to each other, and its Asks | you only; on an organization's server, also anyone you share the chat with, to read |
| A chat's name, owner and state in Chats | you, and the people you share the chat with |
| The tasks and files a chat makes | everyone in the workstream |
| One line on a task when your private request changed it | everyone in the workstream |
| Your chats in My space, their names included | you only |

Afi's pane is private to you on a Personal workstream. On an organization, read its header: "Private to you" means the server keeps your conversation apart; "Everyone in <workstream> sees this" means an older server keeps a shared conversation. Anything on a task or a file is shared.

On an organization's server, the owner of a chat can later share it read only with someone or move it to another workstream or back to My space, and the operator's policy can make new chats visible beyond the owner; none of that happens unless someone does it. "Sharing and moving in an organization" says how. A hosted chat, which runs on the organization's own machine and is not part of a workstream, can be shared for more than reading: see "Chats that run on the organization's server".

## the Chats tab — Waiting on you, Working, Resting, New chat

The Chats tab lists your own chats as rows with one grey preview line, grouped in **Waiting on you**, **Working**, then **Resting**. The newest three resting chats show; the rest fold under "N earlier". Open a row to read or answer in the conversation. New chat is in the header and opens a chat with its own box. The list has only Afi's box, never a second box for starting chats.

## opening Afi — the bottom box and the closed pane

A workstream opens with Afi's pane closed. Use the box below the tab to ask Afi; the pane opens on its first reply. The Afi button opens it immediately. Its 340-pixel pane says what it is looking at and who can read it. Close Afi returns to the bottom box. In Files the box says "Ask Afi about these files". Existing conversation history does not open the pane when you enter a workstream.

## opening a chat in place, or as its own tab — Open as its own tab, back to Chats

A click on a chat opens it in place, inside the Chats tab, with its own box. Opening Afi yields the conversation’s box to Afi. The bar above it has "Chats" (back to the list), the chat's face and name, "Private to you", and "Open as its own tab". The box at the bottom writes to that chat only.

"Open as its own tab" opens the same conversation as a tab whose path reads Work › <workstream> › Chats › <chat>. Its right panel has two tabs, Tasks and Files, listing only what this chat made; a row opens that task or file. Pressing Afi swaps the panel for Afi's pane. A chat is open in one place at a time: opening it as a tab returns the workstream to its list of chats. Leaving a chat or closing its tab stops nothing; it keeps working.

## can Afi see my chats — Afi reads your own chats in the workstream with ws_chats

Yes, yours, and nobody else's. Inside a Personal workstream, Afi carries one more tool than the chats do: `ws_chats`. With it Afi lists your chats in that workstream (each one's name, whether it is waiting on you, working or resting, and its latest line) and reads what was said in one lately. Ask Afi "what is Launch post doing?" or "which of my chats needs me?" and it reads them to answer.

Your teammates cannot read your chat messages unless you share that chat with them on an organization's server, and then only to read; their Afi never reads them. Others see the tasks and files a chat made, never its messages. Afi does not copy what it reads there into a task, a comment or a file; the one line a private request leaves on a task is still the only trace.

On an organization's workstream Afi does not have `ws_chats`: it never reads your chats there.

## Can I start a chat in an organization's workstream — yes, on the organization's server

When the organization's server keeps people's chats (one that also gives you My space), an organization's Chats tab starts chats like a Personal one. Each runs on the organization's server, never on this computer, is private to you in what is said, and what it makes reaches the workstream's Files. Others in the workstream see what it makes as tasks and files, never what is said. It works with the workstream's tools and has no shell; a hosted chat, which has a shell and a folder it works in, is a different thing ("Chats that run on the organization's server").

On an organization's server that does not keep chats, the Chats tab has no box to start one, and says: "Chats aren't started in an organization's workstream: one would run on this computer, where what it makes never reaches Files and Afi can't read it. Ask Afi or add a task instead." Afi's pane works there as always, and a task you add is kept on the organization's server for everyone in the workstream. A chat you started there before that change is still listed on its Chats tab.

## does leaving a chat stop what it is doing — going back or shutting its tab stops nothing

No. Going back to the list, opening another chat, or closing a chat's tab stops nothing: the chat keeps working on whatever it was doing and its tasks keep running. It rests on its own when nothing of its is running, and wakes when you write to it or a task of its needs you. To stop work, stop the task itself, or press stop in the chat while it is answering.

## the workstream tools — ws_tasks, ws_decisions, ws_task_create, ws_task_update, ws_task_comment, ws_files, ws_file_read, ws_file_create, ws_file_suggest, ws_file_comment

Every chat started inside a workstream, and Afi when it is inside one, carries ten tools onto the workstream's shared work:

- `ws_tasks` lists the tasks, or reads one whole with its activity and comments. `ws_decisions` lists what the workstream's people decided.
- `ws_task_create` makes a task; `ws_task_update` changes one this chat does (its state, its result, the check against the request, an Ask for its owner, what it assumed without asking); `ws_task_comment` writes on a task's thread. Starting and finishing work are checked against the decisions: work that goes against one is paused, and the person who made it is asked.
- `ws_files` lists files and folders; `ws_file_read` reads one, with the comments people left on a document's passages; `ws_file_create` makes a document, folder or link; `ws_file_suggest` suggests edits to a document; `ws_file_comment` comments on quoted words of a document, or answers a comment in its thread.

A canvas in the workstream's Files counts too: the agent drawing on a canvas filed there joins the workstream and carries the same tools, so it reads the brief and the other documents with `ws_file_read`. No agent reads a workstream's files from the app's data folder, its database or `/tmp`.

What a chat or Afi makes for the workstream is saved with `ws_file_create` and appears in Files for everyone in it; it is never written to disk as a loose file.

On a Personal workstream the desktop app serves them. On an organization whose agents run on its server, Afi there carries them all beside `workstream_pages`, with the same rules; the document about an organization workstream's agent says what is different there.

## does every chat turn into a task — when an agent makes a task, and when it does not

No. Not everything said in a chat becomes a task. An agent makes a task for work that runs beyond its reply, changes a file, or matters to someone else, and it says so in its reply. A quick answer stays in the chat. Before making a task it reads the workstream's tasks, and when one already covers the work it points you to that task instead of starting a duplicate.

A task an agent does is written as a brief another agent could do cold: a goal, a deliverable, acceptance lines, its inputs as references to files, earlier tasks' Results, decisions or links, and its constraints (see "a task's brief" on the document about tasks). Its request is a short summary. The agent never copies your private conversation into it. When it finishes, the doer leaves a hand-off note for the next task. You own the tasks you ask for unless you name someone else or Everyone.

## does the agent read comments on a document — answering a comment on a passage

Yes. Reading a document with `ws_file_read` gives the agent every comment people left on its passages: the words each thread is on, who said what, and whether it is resolved. It reads them before it changes the document. It answers a comment in that comment's own thread with `ws_file_comment`, and it can start a thread on words it quotes (words the document says once; it quotes more when they appear twice). It never answers a comment by quietly changing the words: a change still arrives as a suggestion, and the agent says so in the thread. What Afi writes in a thread shows as Afi's, and stays unread for you until you open it.

## why the agent did not do what a comment asked — offers under a comment

An agent never acts on a comment. A comment on a task or in a file is people talking to people, so there is no @-mention that gives an agent an order. When a comment looks like a request, the agent adds an offer under it ("Afi can add this to the draft") with one button; anyone in the workstream can take it, and the task's owner is told and can undo it for five seconds.

## the agent changed my document — suggestions, and when it edits directly

Agents change a document by suggestion: nothing changes until someone in the workstream accepts it, and anyone can accept or reject one. An agent rewrites a document directly only when it made that document and no person has edited it since. Two chats changing the same document both arrive as suggestions. On an organization's server too, a suggestion made with `ws_file_suggest` shows on the document for everyone in the workstream, each changed passage marked with **Keep** and **Drop** beside it and a bar above the title to review them one by one. A whole-document rewrite that cannot be applied waits as a suggestion too. Organization documents have no History or Restore action in this release.

## "Priya asked Afi to keep it under 300 words" — the one line a private request leaves

When something you asked an agent privately changes shared work, the task's thread gets ONE line naming who asked for what, such as "Priya asked Afi to keep it under 300 words". The conversation around it stays private. A private answer is shared only when you post it. What the request settles ("The notes stay under 300 words") is kept in the workstream's Decisions as yours.

## big work, "you coordinate it", a batch of tasks — Afi starts a named chat; the plan's Start and Change the plan

Give Afi big work (work that splits into five or more tasks), or tell it "you coordinate it", and it hands the work to a chat of your own instead of doing it in Afi's pane: it starts the chat with ws_chat_start, names it for the work (for example "Release 2.4"), gives it the brief, and says so in its reply. Afi stays free for quick questions. Afi's pane then offers "Open <name>", and in the Chats tab the chat's row says "Started by Afi".

That chat splits the work into tasks under a group with the same name, with owners and what waits on what. Five or more appear in Tasks as a plan with **Start** (once spelled "Start all") and **Change the plan**, and nothing in it starts until you press Start. **Change the plan** opens that chat with "Change the plan for <name>: " ready in its box, so you tell the chat that owns the plan. With fewer than five, the chat asks you first, or goes ahead within the workstream's "Start without asking" amount. Once started, the chat runs the tasks in order, each starting by itself once what it waits on is done (pressing Start is consent for the whole plan), within the workstream's "Start without asking" rule, checks each result against its request, and comes to you only when a task needs you, with an Ask on that task. One name leads to two places: in Chats, your private conversation about the work; in Tasks, the shared group everyone in the workstream sees.

On an organization whose agents run on its server, Afi has no ws_chat_start: it cannot start or read your chats there. There Afi gives the tasks one group in Up next itself, and the organization's server works each task an agent does in a run of its own, never in anyone's private pane; a plan of five or more waits for one press of Start first.

## how Afi breaks work into tasks — how many tasks, how big a task is, where the steps of a task go, too many tiny tasks, one Start for the plan

When Afi (or a chat coordinating a batch) breaks big work down, it makes **5 to 8 tasks, never more**. Each task is a deliverable someone could review when it is done: a document, a canvas, a pull request, a booking or a decision, hours to days of work. Afi never makes a task for anything under 30 minutes of work, such as fixing a typo, "open the file" or "find the link", even when you ask for one: it does it right away instead, with a suggestion on the document where it may not rewrite it, and says what it changed. When it cannot reach the thing (the document is not in the workstream), it says what it can do now and offers it, for example to fix it as soon as the document is in Files. It may also make it a step of the closest task.

The steps of one deliverable are not tasks of their own. They are the task's checklist: lines inside the task that are ticked as each is finished (see a checklist inside a task). Every task in the plan names its owner, a person already in the workstream, and who does it.

Five or more tasks from one request arrive as one plan with **Start** and **Change the plan**. One press of Start releases the whole plan; there is no Start to press for each task. If a plan has too many tasks, or tasks that are too small, press **Change the plan** and say so, for example "merge these into five".

## can the agent read a file I uploaded — uploads in Files read through ws_file_read

Yes. A file uploaded to a workstream's Files (Upload a File…, or dropped onto the list) is read by its Afi and its chats with `ws_file_read`, the same tool that reads a document. A text file — Markdown, plain text, source code such as `.rs`, `.ts`, `.go` or `.tsx`, CSV, JSON — answers its words. A long one comes in parts of about 100 KB: the answer says `more_from`, and reading again with `from` set to it gives the rest.

A file that is not text (a picture, a PDF, a spreadsheet, an archive) answers only its name, size and type, with the line "This upload is not text, so it has no words to read here." The agent then says what it is from its name, or asks you about it.

The agent never reads a workstream's files from disk. They are kept in the app's own store (on an organization, the server's), which no agent may open: it is told never to look for them in the app's data folder or its database, never to search the disk for them, and never to copy them anywhere such as `/tmp`. This holds on a Personal workstream and on an organization's alike, so a citation in a document points at the file in Files, never at one person's computer.

## did the agent save it to the Library — what a workstream's chat makes goes to its Files, not the Library or disk

A conversation that belongs to a workstream (its Afi, or one of its chats) keeps what it makes in that workstream: a brief, a comparison, a plan or a list is saved with ws_file_create and appears in the workstream's Files, where everyone in it sees it. It is not written to disk as a loose file, and it does not reach the app's Library. The Library is a separate shelf: something made in a workstream gets there only when a person adds it with Add to Library. What such a conversation says is also never captured into the Library by itself, whatever the Library's own setting is.

Such a conversation cannot set up work on a clock either. Asked to do something every Monday or each morning, it says to set it up under People and rules › Keeps running, where the workstream's standing orders live, instead of making one itself.

## where is the Decisions panel — Decisions is the first row in Files; why Afi names a task, not its id

Open Decisions from the Overview side block or the first row in Files. New answered Asks show the question with its answer underneath, plus who, when and where it came from. Search finds questions and answers; results group into This week and Earlier. Older records keep their saved decision text, and a replaced decision stays greyed. When Afi or a chat says it recorded or proposed a decision, that is where it is.

Afi and the chats talk in the words you see on the screen. They name a task by its title, a file by its name and a person by their name. They never quote an id (a long string such as `tk_5afd…`), a tool or field name, or a draft number such as "Overview v3", because Files keeps only the current text. They also never talk about "the system" or a window. The places they point you to are Tasks, Chats and Files. If a reply still quotes an id, ask for the task by its title and they will find it.

## A failed tool step says what failed

A failed step in an organization's chat says what could not finish, such as “Couldn't read a canvas”. Its failure is written in words as well as marked in colour.
