---
kind: fixed
title: codeaf app keeps a crew member that waits on the person reading as waiting, and names it by its handle in the Inbox
pr: 0
surface: [engine]
invalidates:
  - "A crew member woken by its manager read `working` the moment its turn was adopted, even while its question or approval waited on the person, so its row said running and its crew did not say needs_you. A woken turn now keeps an agent with an open ask at `needs_you`, exactly as a turn the person sends does: the member reads `asking` with the question as its line and the crew reads `needs_you`."
  - "An Inbox item from a crew member named it by its conversation title, and by nothing at all when the member had none yet (`where.path` was `[crew]`). It is now `[crew name, handle]`, \"Trudeau Watch / verifier\", the handle the crew's Team and Log use; `where.crew` names the one crew it works in (an ordinary membership before a crew it manages, the root team last) rather than whichever open crew was read last."
  - "A crew member the app could not open (another codeaf window holding its conversation) was retried every poll and never said, so its questions could reach nobody while its manager told the person it was waiting on them. The failure is now said once: a line on stderr, a warn `log` event, and a notice in the manager's chat; the member reads `failed` with `can't open here: <reason>` as its line; it is retried on a wait that doubles up to a minute and at once on `crews.log`, and its recovery is said once."
---

A member's permission prompt never becomes a decision packet and the manager
cannot answer it; it is the member's own ask, and the person answers it with
`asks.answer {agent: member}` from the crew or `inbox.answer` from the Inbox,
which closes it everywhere and leaves one receipt in the member's conversation.
