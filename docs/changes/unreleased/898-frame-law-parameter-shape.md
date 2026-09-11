---
kind: fixed
title: the frame law follows the surface through a function that takes it as a parameter
pr: 898
surface: [chat, docs]
invalidates:
  - "`internal/tui3/framedisk_law_test.go` was blind to a package function that takes the surface as a PARAMETER, and its head comment said so as its fifth blind spot. `surfaceGraph.callsIn` made a surface edge out of `x.method()` only where `x` was spelled the same as the enclosing method's own receiver name, so `placeFrameWithBar(a, …)` had its body walked — it is a package function — while every `a.…` call inside it was read as a call on some other value and dropped. A call on any parameter typed `*app` is now the same edge as a call on the receiver, resolved against `app`. The type decides and the spelling only keys the lookup: a parameter named `a` of any other type contributes nothing."
  - "`TestTheLawSeesEveryShapeOfIndirectionItClaims` did not plant the parameter shape. It plants `func planted(a *app)` calling into a method that reads the disk and asserts the walk reaches it, so deleting the parameter branch turns the test red naming that shape."
  - "The law now SEES two live reads the frame can reach, `func errandHomeDir` calling `os.Getwd` (homeexchange.go:1158, behind `placeFrameWithBar` via `app.composerRows → app.composerWhere → app.composerOpensAt → app.errandPlace`) and `func readDraftKeep` calling `os.ReadFile` (draftkeep.go:319, which the frame reaches by name). Both are reported by `TestTheFrameNeverReadsTheDisk` and neither is allowlisted: the entry for `.readDraftKeep` is deliberately absent, so the law stays red on them until whoever owns triage moves each read to `open`/`tick` behind the memo, deletes it, or names it on `frameDiskDoors` with the line saying why it is once-per-epoch and not per-frame."
---

The law's whole value is that a reader can predict what it reaches. It reached
`x.method()` through the method's own receiver name and through a bare package
function, and stopped where a package function took the surface as a parameter —
which is how half of `internal/tui3`'s drawing helpers are written, so the gap
was not academic. Closing it does not fix the two reads it now sees; it makes
them visible, which is the part that had to come first.

The `.readDraftKeep` allowlist entry from the interrupted attempt is NOT kept.
An allowlist entry is a claim that the read happens once per epoch rather than
per frame, and that claim belongs to the triage that owns these two findings.