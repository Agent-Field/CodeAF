# Files — a workstream's shared drive

## what Files is — documents, canvases, uploads, links and repos in folders

Files is a workstream's shared drive: everything worth keeping, in folders, made by people or by agents. It replaces the separate Pages, Sources and Canvases tabs. A file is one of five kinds, plus folders and shortcuts:

- **Document** — a page you write in together (the same editor, comments and suggestions pages always had);
- **Canvas** — a canvas made inside the workstream, by a person from New › Canvas or by Afi or a chat with the `canvas` command: an agent's canvas is filed here by itself, under that chat's name, the first time it changes, so nobody has to attach it (there is no Attach existing);
- **Upload** — a file from your computer (at most 25 MiB on an organization's server);
- **Link** — a web address;
- **Repo** — a connected repository, `owner/name`.

Every file shows who made it (a person, Afi, or a chat's name), the task it came from, and when it last changed. Every workstream starts with a **Decisions** document. Agents read every file in the workstream, in the workstream it sits inside, and in the Library.

Files is the workstream's own library. The app's **Library** is a separate shelf every workstream and agent reads, and nothing in Files reaches it by itself: only Add to Library, chosen by a person, puts a file there.

## where my old pages, sources and canvases went — nothing was lost

The first time a workstream's Files is opened, everything it kept before is folded in, once, and nothing is copied or lost: each page becomes a document (a page that held other pages also becomes a folder holding them), each source becomes a link, a repo or an upload, each canvas made inside the workstream becomes a canvas file, and a workstream with no decisions page gets a Decisions document carrying the decisions it already had.

## New, search, sort, folders — making and finding files

**New** offers Document, Canvas, Folder, Upload a File, Add a Link and Shortcut to a File Elsewhere. A new document or canvas is made in the open or selected folder and opens as a tab. A new folder appears with its name ready to type: Return keeps it, Escape removes it. Files can also be dropped onto the list from the computer; dropped on a folder, they go into it.

The search box finds files by name and by what they say, in this workstream and in the parent's files it reads (those come after, read-only). A click on a column name (Name, Kind, Made by, Changed) sorts by it. Folders nest without limit and fold open in place.

## select, open, rename, move, delete — the right-click menu and keys

One click selects a file and shows its facts in place of Afi's pane (kind, where, who made it, the task it came from, the tasks that use it, last change, open comments), with Open and Ask Afi about this; Space shows them too. A double click or Return opens it as a tab. Shift-click selects a run of rows, Command-click adds or removes one; with several selected, a bar offers Move to, Copy links and Delete — only what applies to every selected row.

The right-click menu: Open, Open Beside, Rename (in place, in the row), Move To, Copy Link, Add a Shortcut in Another Workstream, Add to Library, Ask Afi About This (adds the file to what Afi is looking at), Delete. A folder has the same menu without Add to Library. Add to Library shows only on a document or a text file, and only when your Library is turned on: a canvas, a picture or a link has no words to keep. Dragging files onto a folder moves them, with Undo for 5 seconds. Moving a file never breaks a task, shortcut or link that points to it.

## delete a file, restore a deleted file, version history — Delete asks first

Delete asks first, because it cannot be undone: "Delete “Brief”?", saying the file (and anything inside a folder) will be gone for everyone in the workstream, with Cancel and Delete. There is no way to restore a deleted file from the app and no version history of a file in this release, so nothing in Files mentions versions, and Afi and chats must never offer to bring an earlier version back. Several selected files are deleted the same way, with one question for all of them.

## shortcuts and a parent's files — a file from another workstream or the Library, read-only

A shortcut shows a file that lives in another workstream without copying it: there is still one file, changed in one place. You can only make a shortcut to a file in a workstream you can see. Make one with Add a Shortcut in Another Workstream on the file, or with New › Shortcut to a File Elsewhere… and the file's Copy Link pasted in.

A shortcut can also point at a Library page: Copy Link on the page in the Library, then New › Shortcut to a File Elsewhere… and paste it. It shows as "From the Library", and opening it opens the page in the Library.

A workstream inside another one shows its parent's files as one folder, "From <parent>", read-only there: open and copy their links, but rename, move and delete them in the parent.

## add a file to the Library — keep a workstream file for every workstream

Add to Library, in a file's right-click menu, keeps the file's words in your Library, where every workstream and agent can read them. A note shows for five seconds with Undo; nothing is kept if you press it. This is the only way something in a workstream reaches the Library.

## can Afi change my document — suggestions, accept and reject, fix another chat's draft

Afi and chats change a document by suggestion: the old words struck through, the new ones underlined, with Accept and Reject. Anyone in the workstream can accept or reject. An agent edits directly only a document an agent made (Afi, itself or another chat) that no person has edited yet, so one chat can correct another chat's wrong draft. Once a person edits a document, every agent changes it only by suggestion. When an agent is refused, it says who made the document and whether a person edited it. A comment never makes Afi act: to have Afi change the text, ask in Afi's pane, and the change arrives as a suggestion. Changing a file flags the running tasks that use it.
