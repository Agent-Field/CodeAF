---
kind: changed
title: ctrl+enter queues a follow-up, and queued messages can be taken back
pr: 1714
surface: [chat, engine]
invalidates:
  - "Queueing a message for after the current turn was `ctrl+q`. It is now `ctrl+enter` only for non-empty words from this conversation's composer while its turn runs. At rest, on the start page, over a `/command`, or with an empty box it takes plain enter's action; `ctrl+q` is deliberately unbound. Mid-turn the queue carries words alone and refuses over pictures or a picked harness without changing the draft."
  - "A queued follow-up had no take-backs. It does now for messages queued from this window: a click removes one before its turn starts and returns words, pasted documents and plain slash tags to this conversation's composer. A new draft is kept and the returned message appended on a new line, with paste chips renumbered; pending answers cannot start the removed message, and multiple take-backs keep click order. `↑` does not reach the queue; it stays the parked block's and history's key."
  - "The keys row under the box named `ctrl+shift+enter stops and sends` while a turn ran. That slot is the queue key's now — `enter steers it in · ctrl+enter queue · esc interrupt`, with words in the box on a terminal that can send the chord. `ctrl+shift+enter` still stops and sends and the key sheet lists it; the foot no longer names it."
  - "The queued queue drew only a count, `after yield · N`. It draws one row per message above the box, dim, behind a reply arrow `↳` (a new vocabulary slot, `GFollowUp`, sharing `GReplyIn`'s byte by position — deliberately not the hollow circle a waiting task wears), with nothing under it: rows fit the frame, and only rows holding this window's own receipt light under the pointer and take a click."
  - "`ctrl+enter` marked a draft as a standing order. The chord is queueing's now; the explicit marked door is `/standing <words>`, which works on every terminal, and the hint under the box says the command."
  - "The manual said plain terminals could queue with `ctrl+q`. A decoded `ctrl+enter` now works wherever the terminal sends it, including modifyOtherKeys without a kitty reply. Hints and tips still require the terminal's key-support reply. Unsupported keyboards deliver plain enter on many terminals or a newline on some."
---

`UnqueueFollowUp` is on the engine and crosses the wire ([MethodUnqueueFollowUp]):
the surface takes a message back by the stream it has held since the moment it
queued, the engine answers false when the turn already drained it — in which case
the row stays and the message runs — and the take-back is asked off the update loop
like every other door. A hosted chat's agent is the telemetry tee
(`cmd/codeaf`'s countingAgent), which hands the surface a copy of each
follow-up's stream; the tee maps the copy back to the stream the remote agent
minted, or the take-back names a stream the far end never saw and answers false.