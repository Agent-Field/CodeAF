---
kind: changed
title: The hosted chat list carries each chat's title, and an owner's windows are named by the person
pr: 0
surface: [chat, remote, docs]
invalidates:
  - "Was true: a row of the hosted chat list (`GET /v1/hosted/sessions`) had an id, a state, two times and the verbs you hold, but no name, so the phone and the desktop listed every organization chat as untitled until it was opened, and opening a chat that was not running started it. Is true now: each row, your own and shared, carries `title` (the name the chat gave itself) when the chat has one, kept by the chats' host from the chat's own windows, so listing starts nothing; a chat with no title yet has no `title` field at all."
  - "Was true: a title a chat earned could be read only by a window that was open on it. Is true now: the chats' host keeps the title it last saw on any window's wire in the chat's row, so it survives the chat stopping and the host restarting; a title earned while no window is open reaches the list after the next time a window opens the chat."
  - "Was true: a row in someone's list for a chat they could only see as metadata (the operator's) was as much of the chat as a reader of it got. Is true now: that row still shows the identifier, owner, state and times and never a title, because a title is drawn from what was said in the chat; only rows with access `shared` carry one."
  - "Was true: on an owner's wire the other windows read the label that window's own computer sent, so `typing from phone now` named a device, two phones both read `phone`, and a guest could not tell whose window it was. Is true now: the organization server asks the chats' host to name an owner's window by the person, with the computer's own word kept after the name (`typing from abir's phone now`), and a bot reads `abir's bot` as it does for guests; the word alone never stands, and a remote stamp in the hello is dropped."
  - "Was true: an owner's wire was the one thing in a hosted chat nothing between the window and the engine read, and its label was the window's to choose. Is true now: it is still a byte-for-byte proxy on the organization server, and only the first line of the stream (the hello) has its name set where the chats' host hands the stream to the engine; taking the keyboard, sending and interrupting are unchanged."
---

The phone and the desktop show a chat list for an organization. Until now every row was untitled because the list never carried names, and fetching them one chat at a time would have woken every chat that was not running. The chats' host now keeps each chat's title from the wire its windows already hold, the list returns it, and the windows that see an owner's computer say whose it is.

Two limits. A title a chat earns while no window is open is listed after the next time a window opens it. And the chats' host and the organization server must both be this version for an owner's window to be renamed: an older host ignores the request and the window keeps the label it sent.
