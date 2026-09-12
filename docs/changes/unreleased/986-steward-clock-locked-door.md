---
kind: fixed
title: the goal owner's clock moves through a locked door, and -race is green on dev again
pr: 986
surface: [engine]
invalidates:
  - "`go test -race` was believed clean on `dev`. It was not: `internal/session` reported two data races on `TestADoneEndingNamesTheCheckItCouldNotRun` on untouched `dev`, and no gate in this repository runs the detector, so nothing said so. Both are gone. `Steward.now` and `Steward.started` now move only through `setClock` and `setStarted`, which take the same `s.mu` the product's reads take, and `internal/session/stewardclock_law_test.go` fails any caller — fixture or product — that assigns either field directly."
  - "The setters were introduced as a fixtures-only door, and a comment said the product writes neither field after the goal owner is built. That was never true: `makePrincipal` in `principal_wire.go` handed over the session's own start by assigning the field. It uses the door as well now, so the door is the only way in."
---
The race had no road a person could open — nothing reads the goal owner's clock
before `makePrincipal` returns, and every product read afterwards is under one
lock — so it was the fixtures that reached past it, on live sessions whose wall
clock was already reading those fields. What that cost was a standing red under
the detector, which teaches the next real race report to be ignored. Nothing a
person sees changes, and there is no e2e; the detector is the acceptance, and it
is green at `-count=5` over the headline test and the whole clock-moving family
in `checkpoint_test.go`, `checkmemo_test.go`, `principal_test.go`,
`turnwall_test.go` and `wallclock_test.go`. `docs/rules/ci.md` now says plainly
which gates do not run the detector — all of them — so a green gate is never
again read as a clean `-race`.
