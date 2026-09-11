---
kind: added
title: Every call can be watched — one seam for a call's whole life, fed from the readings it already has
pr: 868
surface: [engine]
invalidates:
  - "A call that nobody is streaming is NOT invisible any more. `provider.WithCallProgress(ctx, func(provider.CallProgress))` attaches one callback to a context, and every call made under it reports going out, parking on a provider's pacing, thinking, writing and how it ended — so a division, a sizing pass, a mark read or any other caller of internal/session's callRole can be drawn while it runs. Until this, `WithStreamObserver` was the only per-call seam and it carries the ANSWER's text, which is exactly what an errand must not type into a room."
  - "`provider.CallProgress.Attempt` names WHICH CONCURRENT REQUEST of one question a report is about — 0 for the caller's, 1 and up for a rescue racing beside it — and not how many times anything was retried. A losing arm reports `End: provider.CallEndCancelled`, which is exhaust and not a failure; a reader that drew it as a failure would be drawing this build's own hedging policy as provider weather."
  - "`provider.WithPacingNotice` is no longer its own account of anything. The pacing park is a PHASE of the same state machine (`provider.CallPaced`), and internal/provider/dispatch.go flips the older bool and the phase from one `park(bool)` closure, so the two cannot disagree. The bool door survives only because its one caller is internal/session/agent.go; retiring it is one line there, after which `WithPacingNotice`, `pacingNoticeFrom` and the pacing context key all delete together."
  - "The token counts, the first token and the ending on that seam are NOT counted a second time. They are forwarded from internal/provider/armwatch.go's `streamWatch.note` — the one place every streamed reading is already folded for the hazard controller — and from internal/provider/calllog.go's `Client.record`, the model-call log's only writer, whose law is that every attempt writes a start row and exactly one row that ends it. A `go/ast` law (`TestTheCallProgressSeamHasOneFeeder`) fails the build if a second feeder appears."
  - "`lanestub.Profile.TearAfter` stages the connection going away underneath an answer — so many visible deltas, then no finish frame, no usage and no sentinel. It is the ending the other knobs could not stage: a refusal is a status, a stall is silence, a cancel is this process's own decision, and a reply that was arriving and then was not is none of those. It was 1,884 of 1,887 in-stream failures in the 2026-09-10 census."
  - "internal/session's `formingInterval` (toolhint.go) and internal/provider's `callProgressBeat` are now the SAME RULE written twice — ten reports a second, with the moments that change what a row says exempt. The copy that should survive is the provider's, because that package is beneath session and the reverse import is impossible; the fold is one line in toolhint.go."
---

A task room can draw a worker's call — the model thinking, the tokens climbing,
the seconds since it went out — because a worker's turn streams through the
session's own observer. Nothing else could. A division, a sizing pass and a mark
being read are each one call, and each drew a blank line for between 12 and 219
measured seconds, which is indistinguishable from a process that has stopped.

The events were never missing. Every streamed reading in this process is already
folded into the hazard controller's watch, and every attempt's two ends are
already written down by the one door the model-call log has. This is the seam
that forwards them, and it adds no decoder, no counter and no second opinion
about when a call began.
