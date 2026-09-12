---
kind: changed
title: the controller-acted list moves onto the phase type, beside Waiting()
pr: 982
surface: [engine]
invalidates:
  - "A test that needed to know whether the waiting controller had spoken kept the list of phases that count by hand — `theControllersWord` in `internal/provider/hedge_test.go` closed its signal on `case PhaseAllSlow, PhaseBelowPace, PhaseSwitching, PhaseSwitchingModel:` — and that list lived nowhere else, so a phase added or a rung renamed left the fixture silently short. No longer true: `PhaseNews.ControllerActed()` sits beside `PhaseNews.Waiting()` in `internal/provider/phase.go` and the fixture reads it. A rung written without an answer to both questions now fails by name in `internal/provider/phase_controlleracted_test.go`, and a second copy of the clause anywhere in the tree fails there too."
---
`Waiting()` and `ControllerActed()` are deliberately two questions, not one:
waiting is what a person sits through and this is what this build did about it —
`first word` is a wait nobody has acted on, `trying again` is the relax ladder
and not the router, and `asking` is the controller deciding not to act until a
person answers, so none of the three counts as the controller having spoken. No
behaviour changes; it is where one fact lives.
