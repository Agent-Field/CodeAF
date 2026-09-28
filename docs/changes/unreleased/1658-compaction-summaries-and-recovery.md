---
kind: changed
title: compaction can end with a summary, refused requests recover, and every pass leaves a visible line
pr: 1658
surface: [chat, engine, remote]
invalidates:
  - "Compaction never summarized: it only stubbed old tool results and folded old assistant work. When those are not enough, the conversation's own model now summarizes the oldest part, older user messages included. An automatic or manual pass keeps the three most recent user messages when no cut can reach its line; recovery from a refused request can keep fewer to reclaim room. /compact always goes on to a summary in the same pass when there is enough older material."
  - "A request's window was the smallest among all of a model's endpoints. A request that carries tools is now measured only against endpoints that take tools. Default routing sends no require_parameters, so the router can still hand it to one that takes none: a size refusal from such an endpoint is sent once more before anything is shortened, its window is not used for later tool requests, and every learned endpoint window lapses after 30 minutes."
  - "The lane sheet read tool support from supports_tool_choice.function. It reads supported_parameters, the list the router filters on under require_parameters."
  - "The xhigh and max thinking budgets were reserved in full whatever the window. They now shrink to the room the prompt leaves and are dropped below 1,024 tokens."
  - "A model that takes no tools was sent the tool list and retried with `Retry 1/1: removed tools`. It is sent none from the start, reads a short chat page, and the person is told once why. A model the catalog does not know is sent no tools for 30 minutes only after a retry without them was answered."
  - "/compact waited ten seconds and then said `compact failed: the engine did not answer in time` about a pass that went on to land. A new surface waits up to five minutes (session.CompactPatience), and a message sent meanwhile is not queued behind it. An old surface talking to a new engine still reports that ten-second failure even though the pass can land later: the wire Version stays at 20 because no frame or method changed, and Decision 3 in docs/REMOTE.md reserves a version bump for a protocol change."
  - "MethodCompact rode the remote ordered lane. It is its own call class, classWork, on a goroutine of its own."
  - "A /compact that changed nothing said only `nothing to compact`. It now says why."
  - "After /compact the status line kept the last request's weight until the next message was sent. It drops as soon as the pass lands."
  - "The post-compaction meter and the ⚭ line counted only transcript text while the next request still carried tool definitions. Both now include the sent definitions in their estimates."
  - "A switch back to a model without tools carried earlier tool-call protocol messages and could be refused. Its request now carries the earlier calls and results as readable text; tool-capable requests keep their original history bytes."
  - "DeepInfra's `Requested input length … exceeds maximum input length …` refusal was retried as an ordinary error. It now enters overflow recovery and teaches the endpoint's stated limit."
  - "A finished compaction was a full-width rule that folded into `▸ worked` when the turn ended. It is one dim `⚭ compacted` line that stays visible, including after dev's trailing-bookkeeping fold."
  - "A /compact pass that folded work while its summary failed said only `compacted`. It now says `summary skipped: <why>` in its note, including over a remote engine; a successful remote reply to an older surface still reads as plain success. A visit to Home during the pass no longer loses that conversation's note or meter update."
  - "An unobserved helper call could consume a tool-less model's one visible notice. The notice is now spent only on a call with a stream observer."
  - "A strict endpoint pin could resend the same oversized tool request to that endpoint and ignore its learned limit on later tool requests. It now pays one refusal and sizes the next request against the pinned endpoint's limit. A routed resend logs its first 400 as well as the answer. Equal limits refresh their disk date, and future dates are clamped."
  - "An ordinary request could send `max_tokens` even when its computed output ceiling did not bind. It now omits that field until the ceiling actually binds."
---

A conversation could fill its window and then be stuck: the request was refused
as too long and `/compact` found nothing to do. The summary is the rung that was
missing, and the size check now counts only what a request really carries, so a
refusal names a shortfall that compaction can actually close.
