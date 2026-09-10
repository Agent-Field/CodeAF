---
kind: fixed
title: the job footer stops defeating de-duplication, and every footer is budgeted inside its own cap
pr: 812
surface: [chat, engine]
invalidates:
  - "A tool result carrying a background-job report was never content-addressed, so no result in a leaf could collapse to `[identical to the result of …]` while any job was out. It is addressed on the body now, with the report riding on the pointer; only a result that is NOTHING BUT a report is still carried whole."
  - "`stripJobFooter` was applied at six sites; a compacted result's tail and a stub's quoted outcome were not two of them, so both ended in a background job's elapsed time instead of the tool's own last line. Both strip now."
  - "The decisions record in message[0] carried every decision the session had ever made and was re-read from `decisions.jsonl` on every rebuild of that message. It carries the newest eight with a line naming how many older ones the file holds, and it is held against the file's size and modification time. `Question.Check` is still asked against the whole record — the cap is on the rendering only."
  - "A reduced view was written whenever it was SMALLER than the result it replaced, which let a rewrite that reclaimed ninety-one bytes re-bill five thousand behind it. It must now reclaim at least a `stubPrefixShare` share of what it disturbs, exactly as the stubbing pass must; below that the result is left verbatim and looked at again next request."
  - "A footer was appended AFTER a result had been cut to fit `bare.MaxResultBytes`, so a 50 KB result plus a job footer plus a fix line was over the cap. The body gives up the room now, the way task_audit.go's boundedResult already did it; a result that was already oversized on its own is left alone."
---

Four defects in the per-turn bill, found by the prompt-diet audit (`docs/design/prompt-diet/DESIGN.md`
§5). Three of them cost bytes on every round of every session that ran a background command, and the
fourth broke a promise about how large a result can be. Nothing a person or a model reads is worded
differently; what changed is which bytes are sent twice.

The de-duplication one is the expensive one. `Toolbox.finishResult` foots every result with the state
of the outstanding jobs, and that state carries an elapsed time — so two identical reads of the same
file were two different strings, and the pointer that exists to keep a re-read out of the window was
silently off for the whole length of exactly the sessions that run longest.

A follow-up in the same lane, from the wave's own bench capture: the reduced view
`toolcompact.go` writes in place of a consumed result was gated on the RESULT's
size and not on what the rewrite reclaims, so a 934-byte result became an
843-byte view whose pointer was a ninety-character path — ninety-one bytes saved
against 5,260 re-billed uncached. It now has to clear `stubPrefixShare`, which is
stub.go's own law and stub.go's own constant.
