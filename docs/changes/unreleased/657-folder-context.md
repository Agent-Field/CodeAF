---
kind: changed
title: one browser chooses folders and files, with a real preview beside them
pr: 657
surface: [chat, engine, remote]
invalidates:
  - "The context browser offered folders only, and its third column listed the children of the highlighted row. The middle column now lists subdirectories and then files with their sizes, and that third region is a preview of whatever the cursor is on — a folder's contents, source with syntax colour and dim line numbers, re-indented JSON, a PDF's text, and a picture drawn in the terminal's own half cells. It is cell-resolution colour and not Kitty or iTerm graphics."
  - "A bare /attach answered `/attach takes a path · try /attach server.log`. It now opens that browser on the folder the conversation is standing in; the refusal is gone rather than unreachable. /attach with a path is unchanged."
  - "The browser could only choose one folder, and nothing else. alt+m now chooses the row under the cursor — file or folder — onto a tray that says how many things are on it and names each; the action row spells out both halves of what enter will do, with each kind's own verb (add this folder, attach this file, attach this picture, add 1 folder · attach 2 files). Twenty-four things is the cap and the twenty-fifth press says so. Browsing, focusing and previewing still attach nothing at all."
  - "Choosing a folder registered it and wrote the pick to disk under the keystroke. A confirm's whole batch — folder registration, which is a round trip over a connection, and each file's stat — now runs off the terminal loop, and its answer carries the session file it was made for so a sentence is never printed into a conversation that never gained the folder. Each failure names its own thing."
  - "The preview pane had no controls at all because it was not drawn. alt+p hides it and alt+o gives it the whole sheet; shift plus an arrow scrolls and slides it beside the list, and with the preview alone the plain arrows are its own. The wheel follows the pointer rather than a focus, a press in the pane does nothing, and a second press on the row already under the cursor walks into it now that there is no column of children to click."
  - "The picks and the index landing a second after the sheet opened rebuilt it, which discarded anything already chosen, reset the preview pane's state and left the pane blank until the next keystroke. All three now survive, and the cancelled read is made again."
  - "Preview cache hits previously called stat on the UI loop. Identity checks now run inside the bounded asynchronous reader, retain cached content only after validation, and preserve trailing spaces in absolute filenames."
  - "Selecting two subfolders in one repository used to replace the first attachment, and removal targeted the root. Selected scopes now stay independent in context, persistence and removal while sharing one working ground."
  - "Attached repository subfolders previously loaded only root instructions. Applicable AGENTS.md and CLAUDE.md files now load along the selected ancestry, with explicit directory scopes and the same total budget."
  - "Choosing a folder used to update the conversation record without telling the next model request. Explicitly attached folders now appear in its instructions with scoped project rules, and removal takes them out again."
  - "The local wire client could lack folder registration while the surface offered it. The engine now advertises folder support and the client exposes registration, removal and the remembered set."
  - "The folder picker previously lacked successive child columns and mouse actions. It now supports breadcrumbs, child browsing, explicit add/remove actions and removable folder indicators; search ranks path segments, initials and spelling slips, and discovers ordinary folders too."
  - "An older attachment-context disk read could overwrite a later removal. Publication now rejects readings whose folder set has changed."
  - "Attached instruction files could exceed their aggregate budget when earlier files left only a partial allowance. Each read now uses the remaining allowance and names any truncated source."
---

This entry describes the integrated engine, wire and context-browser behavior.

One sheet answers both questions a person has when they say what a turn is about:
which project do I mean, and which file do I want to send. `/folder` opens it with
folder intent and a bare `/attach` opens it with file intent, on the folder the
conversation is standing in; the intent decides only whether a folder door is
required to open at all, because attaching a file needs none.

The two things it chooses do different things and the last row of the sheet always
says which. A folder goes to the conversation as a scoped reference and reaches
every later model request. A file goes onto the next message's tray — a picture as
content to be looked at, anything else as a path the `read` tool can open.

Limits stated rather than dressed up: the picture renderer is half-cell TrueColor
and ANSI256 through the existing framebuffer-safe path, so it is coarser than a
graphics protocol and it survives a repaint; a blocking `stat`, `ReadDir` or `Read`
cannot be interrupted, so a read on a hung mount completes late and is then dropped
rather than aborted; and no Mac-specific rendering claim is made from a Linux box.

The control legend remains visible when the filter holds a path, including the
full preview door on narrow terminals. Account key-entry questions allow safe
chat navigation and explicit conversation stopping, alongside permission cards.
