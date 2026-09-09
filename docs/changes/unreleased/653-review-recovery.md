---
kind: fixed
title: Keep guest recovery with its owner and preserve unanswered corrections
pr: 653
surface: [chat]
invalidates:
  - "A finished task on a READING page said `this task has finished — say it to main`, in the foot and in the box's own placeholder. Both halves were wrong on that page: main is THIS window's conversation, whose task of the same number is different work — so the foot aimed the words at the wrong owner, and the placeholder overrode the read-only word with an offer about a box that never could steer from here. NOW: the foot draws [roomGuestFinishedRefusal] through `app.roomDoneRefusal` — `this task has finished — say it in docs pass` with the owner's own name, or `…say it in the conversation that owns it` where nothing has named it — and the placeholder keeps `Reading this task… (esc: main)`, which is exactly as true of a landed task as of a running one. The refusal-law test now accepts the owner's conversation as a place words can go, beside main."
  - "Closing a conversation could turn a crossing with a lost answer into a refused draft, lose its original message name and compact paste payload, or claim it was kept on a page that no longer existed. NOW: crossings and unanswered corrections retain their owner, original identity and full snapshot on disk across close, reopen and restart. A late lost answer stays uncertain, even after a later refusal; reopening the original task offers an explicit same-name retry without putting those words into any composer. Ordinary unsent drafts still leave with the closed conversation. Tests accept the correction before losing its receipts and prove retries never deliver it twice."
  - "A guest page that took the engine's final answer — the conversation under it was replaced — kept its subscription to the owner's task lane until the page was closed. `tookGuestNotice`'s lost path returned without re-arming AND without stopping the watch, so this window stayed a reader on a conversation that had already said its last word to it. NOW: `taskGuest.dropWatch` releases the lane the moment the answer is final — where `tookGuestRecord` learns it from the read, and where a late notice reaches the lost page — and nils the stop in the same motion, so the ordinary release on the way out cannot close the subscription a second time."
---

Three endings on the task surfaces claimed a place that was not there. A landed
guest page named this window's conversation as the door for words that belong to
another one; a correction answered after its conversation was closed was said to
be kept on a page the close took with it; and a page whose conversation was
replaced went on holding the owner's lane as though there were more to hear.

The shape of all three fixes is the same law: a sentence about where something
is kept has to name a place that exists, and a subscription with nothing left to
carry is given back at the moment that becomes true, once.
