---
kind: fixed
title: a device that finished a pairing writes its own directory record then and there
pr: 1742
surface: [chat, engine, docs]
invalidates:
  - "After a pairing finished, neither computer had a device record yet: a device wrote its own record only at its next chat start, so `codeaf devices` said `nothing is paired yet` on a computer that had just printed `Paired.`, and after a link pairing only the joining computer was on the list. Now the same upsert every chat start makes runs when a pairing finishes — on the showing side, the code-joining side, the approving side and the link-joining side — so each side's devices list shows the other at once. If the record could not be written, the pairing still stands: one line says so and the command still exits 0, because the next chat start writes the record anyway."
---

A device may only ever write its own record, so the fix does not build a record for the
other device: it reuses `syncsetup.PutOwnDevice` — the upsert every chat start makes —
with the name and platform this computer already knows. The pairing doors call it after
their success line and treat a failure as a note on the screen, never a failed pairing.
