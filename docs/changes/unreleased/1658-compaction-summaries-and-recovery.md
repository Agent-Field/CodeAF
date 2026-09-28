---
kind: changed
title: compaction can end with a summary, refused requests recover, and every pass leaves a visible line
pr: 1658
surface: [chat, engine, remote]
invalidates:
  - "Compaction never summarized: it only stubbed old tool results and folded old assistant work. When those are not enough, the conversation's own model now summarizes the oldest part, older user messages included. The three most recent user messages stay word for word unless keeping three cannot get under the line; then it tries two, the latest with its preceding reply, and the latest alone. /compact always goes on to a summary in the same pass when there is enough older material."
  - "A request's window was the smallest among all of a model's endpoints. A request that carries tools is now measured only against endpoints that take tools."
  - "The lane sheet read tool support from supports_tool_choice.function. It reads supported_parameters, the list the router filters on under require_parameters."
  - "The xhigh and max thinking budgets were reserved in full whatever the window. They now shrink to the room the prompt leaves and are dropped below 1,024 tokens."
  - "A model that takes no tools was sent the tool list and retried with `Retry 1/1: removed tools`. It is sent none from the start, reads a short chat page, and the person is told once why."
  - "/compact waited ten seconds and then said `compact failed: the engine did not answer in time` about a pass that went on to land. A new surface waits up to five minutes (session.CompactPatience), and a message sent meanwhile is not queued behind it. An old surface talking to a new engine still reports that ten-second failure even though the pass can land later: the wire Version stays at 20 because no frame or method changed, and Decision 3 in docs/REMOTE.md reserves a version bump for a protocol change."
  - "MethodCompact rode the remote ordered lane. It is its own call class, classWork, on a goroutine of its own."
  - "A /compact that changed nothing said only `nothing to compact`. It now says why."
  - "After /compact the status line kept the last request's weight until the next message was sent. It drops as soon as the pass lands."
  - "A finished compaction was a full-width rule that folded into `▸ worked` when the turn ended. It is one dim `⚭ compacted` line that stays visible, including after dev's trailing-bookkeeping fold."
---

A conversation could fill its window and then be stuck: the request was refused
as too long and `/compact` found nothing to do. The summary is the rung that was
missing, and the size check now counts only what a request really carries, so a
refusal names a shortfall that compaction can actually close.
