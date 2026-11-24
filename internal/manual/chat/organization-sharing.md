# Sharing and moving in an organization — who can see a chat, a canvas or a workstream, and what the operator decides

## Can I share a chat, a canvas or a workstream with a teammate so they can read it — roles, grants, and where you do it today

On an organization's server, anything that can be shared is shared by a **grant**: a person, a team or the whole organization is given a **role** on one thing, and that is all a grant does. A grant never changes who owns the thing or where it lives (moving does that; see "Moving a chat between My space and a workstream"). It can carry an end date.

The organization's server can grant a role on a workstream, a canvas, a chat, and Afi's pane or a task's working conversation (see "Afi's pane and a task's working conversation as sessions"). Only someone who holds **share** on the thing may add a grant, and only up to what they hold themselves: a person who can only edit cannot hand someone else "full". Before it is added, the server can say who would gain or lose access, and the person sees that first.

**The desktop app has no sharing buttons for chats yet.** Today the grants are reached through the organization's server itself; nothing in the chat window offers them. Until someone shares one, nothing changes: a chat in My space or in a workstream stays private to you, as "A chat inside a workstream is private" says.

## What view, comment, edit and full let someone do — roles and verbs

A role is a named bundle of **verbs**. The verbs are fixed: view, comment, edit, run, steer, answer, share and manage. The four roles every organization starts with nest inside one another:

| Role | Verbs it holds |
| --- | --- |
| view | view |
| comment | view, comment |
| edit | view, comment, edit, run, steer, answer |
| full | all eight, so also share and manage |

The owner of a thing holds full on it. A person who reaches a thing in two ways (a share and a team grant, say) holds the verbs of both. A thing marked private is its owner's alone: shares and grants on it give nothing. Nobody holds "admin" on a thing; admin is the operator's word for the app, not a verb.

## Are chats private by default — can other people in a workstream see my chat's name, owner or state

Yes, private by default. A chat you start, in a workstream or in My space, is registered as private unless the organization's policy says new chats start otherwise. A private chat is hidden from every shared list: the workstream's people and the operator are told nothing of it, not its name, its owner, its state or when it last moved, and the server does not announce it to them when it is made. Only you and the people you have shared it with see it. Sharing it (a grant of view or more on that chat) adds it to that person's list of other people's chats in the workstream, with its name, owner, state and last activity, and never what was said; removing the grant, or its end date passing, takes it off their list again. Moving a private chat from My space into a workstream keeps it private.

This changed on 2026-11-24. Before it, every chat in a shared workstream was listed to the workstream's people by name, owner and state whatever its own privacy. An organization that wants that back sets the policy's default for chats to `owner` (the workstream's people) or `org` (every member); it applies to chats made after the change, and a chat that already exists keeps what it was made as.

## Bots — a token for a script or an integration, with the rights of the person who made it

A person can make a **bot**: a named identity with its own token, for a script or an integration that should reach the organization's server without a person's sign-in. Making one (`factory.org_bot_new`, `POST /v1/bots` with a name and, optionally, a narrower scope) answers the token once. It starts `cbt_`, it is never shown again, and the server keeps only a hash of it, so a lost token is replaced, not recovered. A bot has up to three live tokens, so one can be swapped for another. A person lists, adds a token to, revokes a token of, and removes only their own bots; the operator lists every bot and can remove any, but cannot mint a token for someone else's. The desktop has no bots screen yet.

A bot is never more than the person who made it. On every request it holds what its creator holds on that thing right now, narrowed by the organization policy's scope for agents (`agents.scopes` for `bot:<id>`, else the default scope) and by the scope the person gave it when making it (a role name from the policy). When its creator loses a share, the bot loses it on its next request. When the operator removes its creator from the organization, the creator's bots stop at once, and stay stopped if that person is added again. Nothing is copied: no grant ever names a bot.

What a bot makes belongs to its creator: a chat, a task or a file it creates is owned by the person who made the bot and recorded as made by the bot, and a chat is private like any other chat until it is shared. A bot cannot make bots, mint or revoke tokens, sign in as a person, use the operator's routes or the event stream, or start an agent run (messaging a chat's agent): those answer "a bot cannot use this yet", or "a bot cannot start an agent run yet". It reaches workstreams and what is in them, canvases, shared resources, My space, and the policy and people lists. A bot can also start, list, open, stop and delete hosted chats on the organization's server, as its creator, and cannot touch a person's stored keys there: see *bots and hosted chats*. A revoked, removed or switched-off token is refused with 401 and words that say which, and is never tried as a request from the server's own computer.

## Who sets the sharing rules — the operator's organization policy

The operator holds the organization's **policy**: which roles exist and the verbs in each, whether new chats start private, shared with the owner's workstream, or visible to every member, who may share (anyone holding share, or only the owner), and what agents may do. Everyone in the organization can read the policy; only the operator can change it, and a stale edit is refused with "412" so two edits never overwrite each other.

An organization starts on the default policy, which behaves as the server always did: the four roles above, chats private, the operator seeing what is not private and nothing that is. A role can be added or its bundle changed, and the four starting names can never be removed or renamed, because old shares and clients use them. A change to the policy changes who can see things, so every person who gains or loses access to something hears about it at once. A policy that makes new chats visible to every member applies to chats made after the change only; it never opens an existing chat.

## Can someone read a chat I shared with them — a read-only transcript, never talking into it

A person with at least **view** on a chat, Afi's pane or a task's working conversation can read what was said there, and ask again for only what is new, but cannot post to it: the message, stop and card doors stay with the person whose chat it is. Everyone else, the operator included, is told the chat is not there, with the same words as for a chat that does not exist: a private chat reveals nothing, not even that it exists.

Reading changes nothing: it does not mark the chat as touched, start its agent, or leave a line on the organization's record. When a grant is removed, runs out, or the chat is moved somewhere the person does not reach, the next read is refused. A role that holds steer or run does not give a talk door in this version for a chat in a workstream: steering someone else's chat there is not built. A chat that runs on the organization's own server (a hosted chat) is the one place it is: a person with steer can interrupt and steer it, one with answer can answer its questions, and one with run (the `edit` and `full` roles) can send messages and run commands in it as its owner, with the owner's GitHub identity and files; approving, settings and files stay the owner's. See *what can someone I shared my hosted chat with do*.

## Moving a chat between My space and a workstream — move is not share

A **move** changes where a chat lives, and so who can reach it; a **share** only adds or removes people. A chat's lines are kept under its workstream and its person, so a move relocates that folder: the chat keeps every line and is listed in its new place, and the old place no longer has it.

Only the person whose chat it is can move it, to a workstream they can edit or to their own My space; another person's space is never an option. Moving a chat to where it already is is refused with "it is already there". A chat that is answering a message cannot move: "stop it or wait for it to finish, then move it". Moving into My space makes it private again and drops the grants that were on it; the move says which ones. Moving into a workstream keeps its privacy and its grants. From then on the destination's rules apply: its budget pays for the chat's next messages, and a chat whose named agent the destination does not list still reads but takes no more messages ("start a new chat"). A move tells everyone who could see the chat before or after it, once. In a workstream shared with other people the server's chat list has always carried each member's chats' title, owner and state, so a chat moved in appears there for the workstream's people; only what was said stays its owner's.

Pages and files do not move this way: they keep **Move to workstream…** (see "Moving a document from My space into a workstream"). Afi's pane and a task's working conversation belong to their workstream and cannot be moved.

## A share that runs out — give someone access until a date, what happens when it ends, and who hears about it

A grant can end on a date. From that moment it grants nothing, whether or not anything has noticed. The organization's server also checks for grants that ran out, at the next end date and at least once a minute, and tells the people who lost access, so their list drops the thing without a refresh; it tells nobody else, and tells each thing once. A grant that ran out while the server was off is announced the next time it starts. A grant is refused when its end date is not in the future ("an expiry must be later than now"). The record of an ended grant stays; it simply shows as ended.

## Afi's pane and a task's working conversation as sessions — which can be shared

Afi's pane (your one conversation with a workstream's agent) and a task's working conversation are listed beside chats as **sessions**. They behave exactly as before: your pane is private to you, a task's conversation is read by everyone who can see its workstream, and who may talk to either is unchanged. Being listed lets a grant name them. A pane is shared like a chat (read only, by the rules above). A task's conversation is already read by everyone who can see its workstream; a grant on it adds a reader from elsewhere in the organization. Neither moves between workstreams.
