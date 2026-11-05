---
kind: added
title: a chat open on another computer says when a turn is running there
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a chat row's status running meant only that another computer held the chat open; nothing said whether a turn was running there, so home said held on <device> either way and the app could not tell open from working. Is true now: the holder beats as soon as a turn starts or ends, the lease keeps working, rows carry working:true (hello: chats.working), and home says working on <device> while a turn runs there."
  - "Was true: a directory beat carried fence, pending and upload progress only. Is true now: it also carries working, sent only to a relay that answers Codeaf-Working-Beats: 1 (the Go relay and the hosted relay both do after this change); an acquire or a release clears it and a publish keeps it."
---

Another computer's chat used to say only that it was held there. Now the
holder tells the relay, with a beat sent the moment a turn starts or ends,
whether a turn is running, and every other computer can say so:

- **Home** says `working on <device>` while a turn runs there and
  `held on <device>` while the chat is merely open.
- **The app protocol** puts `working:true` on such a `running` row, and
  `hello` advertises `chats.working`.
- **Relays** keep the flag on the lease. Both the Go relay and the hosted
  relay advertise `Codeaf-Working-Beats: 1`; a client never sends the field to
  a relay that does not, because older relays refuse unknown fields. Every
  member of a relay cluster must run a build that keeps it before any member
  advertises it.

It is display only: taking a chat does not consult it.
