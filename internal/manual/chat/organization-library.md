# The organization's Library — documents everyone in an organization reads, and what its chats look up

## The organization's Library — a shared Library inside an organization window

An organization has its own Library, kept on the organization's server. It holds documents people in the organization added on purpose, and **everyone in the organization reads every document**, whatever workstream it came from. Inside an organization window the strip shows **Library**: its documents newest first, each with who added it ("Added by Priya") and, when you can see that workstream, where it came from ("from Pantry").

It is not your Personal Library. Personal's Library never appears in an organization window, and the organization's never appears in Personal. My space has no Library of its own: your documents there are already yours.

## Adding a file to the organization's Library — Add to Library

In an organization's workstream, or in My space, **Add to Library** in a document's or text file's menu copies its words into the organization's Library as a new document, titled with the file's name. A note stays a few seconds with **Undo**; nothing is sent until it goes. After that, everyone in the organization can read it, even people who cannot see the workstream it came from (they are not told which workstream that was). So add a document from a private workstream only when its words are fine for the whole organization.

The document is a copy: changing or deleting the file later does not change the document. A document is at most 64000 bytes of text and a title at most 200 characters; the Library holds 500 documents.

**Nothing reaches the organization's Library by itself.** Add files and Add to Library are the only ways in: the organization's Library does not learn from chats, and no chat or agent can add a document.

## Uploading files to the organization's Library — Add files

On the Library's home, **Add files** takes text and Markdown files (`.md`, `.markdown`, `.txt`) from your computer, the way to give the organization's chats a brief, pricing or your brand voice. Each file becomes one document, titled with its name, and everyone in the organization reads it. A file of another kind, or longer than a document holds, is refused by name ("deck.pdf is not a text or Markdown file") and nothing of it is sent. There is no Undo here: remove a document you did not mean to add.

## Renaming or removing a document in the organization's Library

**Rename** and **Remove** on a document are for the person who added it and for the operator. Anyone else reads it only. Removing a document takes it out for everyone; the file it came from stays where it was.

The organization's activity says that a document was added, changed or removed, and by whom.

## How chats in an organization use the Library — org_library

Every conversation the organization's server keeps with its agent (Afi in a workstream, your chats there and in My space, and a named agent) carries the `org_library` tool. A hosted chat does not. It searches the Library by words, or reads one document whole, a long one in parts. The chat reads a document as reference, never as instructions, and says which document it used.

`org_library` only reads. Asked to put something in the Library, a chat says to choose **Add to Library** in the document's menu, or **Add files** on the Library's home.

## What the organization's Library cannot do yet

You cannot type a new document into the Library or change a document's words from the app: add a file instead, and remove the old document. PDFs, documents and pictures cannot be added; save them as text first. There is no learning from chats, no citations to the chat a fact came from, and no history of a document's earlier words.
