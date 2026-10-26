---
kind: fixed
title: Show the first frame without waiting on furrow detection
pr: 1954
surface: [chat, engine, docs]
invalidates:
  - Building a session ran furrow to find out whether the folder is watched; it now starts that question in the background and the session never waits for it.
  - The workspace snapshot, restore, fork and merge tools were decided when the belt was built; on a watched folder they now join the end of the belt when furrow answers.
  - A session closing while another session on the same folder was still waiting for furrow used to wait for it too; it now leaves at once.
---

A cold launch used to block the engine hello behind `furrow --version` and
`furrow status`, two to three seconds on a folder furrow was never pointed at
(with the furrow that shipped before 0.3.3). Each session now asks furrow from a
goroutine it owns and joins at close, so first-frame time no longer depends on
how fast furrow is: on a cold engine and a real profile it was about 0.5 s
instead of 3.9 s then, and it stays about 0.4 s whatever furrow does.

An unwatched folder still gets none of the four tools. On a watched folder they
are appended to the end of the tool list when furrow answers, like any other
late capability, so the prompt cache is invalidated once at the tail and never
moves anything ahead of it. The first request of a session waits up to 200 ms for
a question that is already running, so a message sent the instant the session
exists usually carries the tools and pays no invalidation. Tools never leave the
list mid-conversation: a folder that stops being watched later keeps them, and
they answer with furrow's own refusal. A furrow that fails or hangs is
remembered as not attached for thirty seconds rather than asked again.
