---
kind: fixed
title: one owned decision about what the final answer is, and a turn's end settles it once
pr: 234
surface: [chat, engine]
invalidates:
  - "There was no `<think>` policy anywhere — `grep -rn '<think' internal/provider internal/session internal/tui3` found nothing, and the tag and the working inside it were typed into the person's answer and kept in the transcript as words the model had said out loud. `internal/provider/answer.go` now carves a fenced region out of the answer channel and sends it where working goes, on both transports. `<think>` and `<thinking>` are read the same way, and only where the fence OPENS the reply — an answer that mentions the tag keeps its own words. OpenRouter strips the tag itself, so this is the `AFORGE_BASE_URL` case: vLLM, llama.cpp, Ollama, LM Studio, any gateway in front of a raw open model. Seventeen models probed through the router on 2026-09-01 produced it zero times."
  - "A response that came back with NO content and a long `reasoning` was read as an empty reply — a failed call — retried at the person's expense, and its words thrown away (`internal/session`'s `turnBroke`). The surface drew a turn that thought for nine seconds and said nothing, with no assistant block at all. For A TURN, the working is now the reply: it streams as the answer and it is what the transcript keeps. Measured on the real router: `qwen/qwen3.5-9b` answered a two-sentence question with zero content deltas and 3,066 characters of reasoning, and one call inside a live `deepseek/deepseek-v4-flash` conversation did the same with 76 reasoning deltas."
  - "The promotion above is NOT universal, and the bound is the useful fact: it happens only where the caller said its call asks for prose somebody reads (`provider.WithProseAnswer`, set on the turn's own request in `completeWithRetryReasoning` and nowhere wider). A gate asks for `{\"work\": false}` and its answer is read by a parser, so a deliberation salvaged into that slot would start work off a sentence the model was still arguing with itself about — and roughly one gate call in ten on the real router comes back empty-with-working. Fence-stripping is not bounded; junk is junk in a parser's input too."
  - "`StreamEvent` grew `FromAnswer`, which marks working carved out of the answer channel. It is DISPLAY ONLY and `reasoningBuffer.write` drops it: that working never travelled under a reasoning field, so there is none to replay it under, and `provider.MessageReasoning`'s encoder refuses an unnamed field outright — which would have failed the very next request of the conversation."
  - "#190 said no shape was found where a block actually missed its settle, and that still holds — but a delta arriving after the boundary did real damage anyway, and it was the CLASSIFIER rather than the settle. It opened a SECOND assistant block, which drew raw because nothing was left to settle it AND demoted the answer above it into narration, which is drawn plain (hierarchy.go). `app.settledTurn` now states the boundary and a late delta lands settled on the answer it belongs to."
  - "A note used to close the live block, so a line this surface wrote in the middle of a streaming reply — a notice about a reshaped request, a nudge — sent the rest of the answer into a second block and left the first half grey and unrendered. `app.noteWritten` takes `app.said`'s door now, the same one a person's own line has taken since the steer wave: the note lands under the block and the block goes on growing."
---

Issue #225. The report was two symptoms — a reply that sat raw until the next
question was asked, and a reply filed as thinking — and they turned out to be
three defects at two different seams, which is why one of them survived #190.

WHAT IS ANSWER AND WHAT IS WORKING IS NOW DECIDED ONCE, with the wire in hand,
and spent twice: on the events a surface draws and on the text the transcript
keeps. That is the property the report was really about — what a replay shows
later has to be what streaming showed live — and it is now true by construction
rather than by two readers happening to agree.

Nothing here knows a model's name. `thinkTags` is a list of WIRE SPELLINGS in
exactly the sense `streamDelta`'s three reasoning field names already are.
