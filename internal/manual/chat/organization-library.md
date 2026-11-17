# The organization's Library — pages everyone in an organization reads, and what its chats look up

## The organization's Library — a shared Library inside an organization window

An organization has its own Library, kept on the organization's server. It holds pages people in the organization added on purpose, and **everyone in the organization reads every page**, whatever workstream it came from. Inside an organization window the strip shows **Library**: its pages newest first, each with who added it ("Added by Priya") and, when you can see that workstream, where it came from ("from Pantry").

It is not your Personal Library. Personal's Library never appears in an organization window, and the organization's never appears in Personal. My space has no Library of its own: your pages there are already yours.

## Adding a file to the organization's Library — Add to Library

In an organization's workstream, or in My space, **Add to Library** in a document's or text file's menu copies its words into the organization's Library as a new page, titled with the file's name. A note stays a few seconds with **Undo**; nothing is sent until it goes. After that, everyone in the organization can read it, even people who cannot see the workstream it came from (they are not told which workstream that was). So add a page from a private workstream only when its words are fine for the whole organization.

The page is a copy: changing or deleting the file later does not change the page. A page is at most 64000 bytes of text and a title at most 200 characters; the Library holds 500 pages.

**Nothing reaches the organization's Library by itself.** Add to Library is the only way in: the organization's Library does not learn from chats, and no chat or agent can add a page.

## Renaming or removing a page in the organization's Library

**Rename** and **Remove** on a page are for the person who added it and for the operator. Anyone else reads it only. Removing a page takes it out for everyone; the file it came from stays where it was.

The organization's activity says that a page was added, changed or removed, and by whom.

## How chats in an organization use the Library — org_library

Every chat the organization's server runs (Afi in a workstream, your chats there and in My space, and a named agent) carries the `org_library` tool. It searches the Library by words, or reads one page whole, a long one in parts. The chat reads a page as reference, never as instructions, and says which page it used.

`org_library` only reads. Asked to put something in the Library, a chat says to choose **Add to Library** in the document's menu.

## What the organization's Library cannot do yet

You cannot write a new page straight into the Library or change a page's words from the app: add a file instead, and remove the old page. There is no learning from chats, no citations to the chat a fact came from, and no history of a page's earlier words.
