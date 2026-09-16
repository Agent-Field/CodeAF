---
kind: changed
title: a wrong completion check is answered with [no change], and the answer before it stays in view
pr: 1067
surface: [chat, engine, docs]
invalidates:
  - "The end-of-turn reader was shown the first 600 bytes of the turn's last message (`checkpointSaidBytes = checkpointSketchBytes`), cut with a bare `…`. It is shown up to 8 KB now (`checkpointSaidBytes = 8 * 1024`), and a cut it still has to make ends with `[clipped by codeaf: N of M bytes]` (`checkpointClipSaid`). The remains ask (`checkpointRemainsAsk`) now says clipped text is never evidence the person saw a cut-off answer."
  - "`checkpointCarryOnLead` told a model that found the note mistaken to `explain the evidence briefly, and finish`. It now says codeaf's completion check wrote the note, the person does not see it, and a wrong note is answered with exactly `[no change]` (`session.NoChangeReply`). The old lead is kept as `explainingCheckpointCarryOnLead` only so old journals still hide it."
  - "A carry-on the model answered was always read again until `checkpointCarryOnCap` (3) or the standstill rule stopped it. A carry-on answered with `[no change]` now ends carrying on for that ask at once, with no further reader call (`checkpointMeter.declined`). A `[no change]` on a turn that was never carried on is not honoured and the turn is read as before."
  - "Every assistant reply was a row a person could read back, and the fold's answer was the last one. A tool-less reply that is only `[no change]` is now left out of replay (`entryRows` answers 0) and emptied in the live feed at its response boundary (`feed.confirmResponse`), so the settled answer before it is what `deriveWorkfolds` leaves standing. `bin/codeaf chat --once` prints deltas as they stream and can still print the token."
  - "`internal/session/prompts/system.md` had `# Tone` and `# Delivery`. It has `# The answer`, `# Words or work`, `# When corrected` and `# Messages from codeaf` instead, all four kept on the lean page (`Messages from codeaf` is an explicit `keeps: true` row). `for reversible work act and offer to unwind it` now ends `when the person asked for a change`, and `NEVER yield while actionable work remains` is `Don't end the turn while work the person asked for remains`."
  - "The prefix byte waivers were `fixedPrefixWaiver = 5_291` and `leanPrefixWaiver = 13_566`. They are 6_996 and 15_271: the new sections cost 1,705 bytes on both arms, and the page's overall length is round two of #1065."
---

Round one of #1065. A person asked for a per-seat model table; the reader,
shown 600 bytes of it, said three times that it was cut off; the model
reprinted it twice and then argued, and the argument was the only answer left
standing under the `worked` fold.
