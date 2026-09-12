---
kind: changed
title: A question is drawn one way, in one frame, with the amber on its marks alone
pr: 933
surface: [chat]
invalidates:
  - "A question was drawn by whichever renderer its lane reached for, and which drawing it got was four decisions in four places — the asker's `Form` word, the count of its options, the width, and a renderer called by name. No longer true: `internal/tui3/questionchooser.go` is one ladder of the question's own properties, read top to bottom, and it never asks the question's Kind."
  - "The question hue was a violet of its own (`#C08FE8` dark, `#6F3FA8` light) on every word of a question — the head, each answer, each key — while home and the places said the same 'waiting on you' in amber. The violet is retired from both ladders: `palette.ask` paints with `warn`, and the amber touches `?`, `▸` and `◆` and nothing else. A law test fails a row painted in the hue."
  - "The block had no frame: a question was rows in the conversation. It hangs in one now, and so do the `ctrl+k` switcher card and the onboarding panel — `internal/tui3/frame.go` is the one primitive. The `/folder` chooser still carries its own pieces and the frame's comment says why."
  - "A permission's pointer opens on the answer that loses nothing — `enter` on a gate you have not moved the pointer on DENIES — and that is unchanged from `dev` on purpose. The 2026-09-11 design ruling placed it by `Question.Stakes` instead; the owner's follow-up the same day is DENY-FIRST UNTIL GRADED, because nothing upstream grades a call: `session/consent.go` stamps `StakesCostly` on every consent alike and `internal/approval` never produces `StakesIrreversible`, so a by-stakes rule read `rm -rf *` as ordinary. The seam for the ruling is engine-side (approval's always-ask shapes → irreversible → `consentAsk` passes it through), and `TestTheGateGradesNoCallAsIrreversibleAndMarksDenyTheSafeAnswer` in `internal/session` fails the day it moves."
  - "`esc` on a question jumped it to the status line. It folds IN PLACE to one titled rule — `── ? <head> · 3 answers · ◆ <pick> ──── space open ──` — and `space` opens it again."
  - "The receipt opened with the word `decided`. It opens with the vocabulary's settled mark and carries a working tail until the model's first output. `internal/e2e/tuiwords_test.go`'s needle moved with it."
  - "The receipt offered `c change` on every reversible decision and no door existed behind it. The key is gone until the door lands."
  - "The block's keys were one row of everything it could fit. They are two tiers: the frame's bottom edge carries exactly `↑↓ choose · enter take it · esc later`, and one dim row under it carries the rest, dropped right-to-left."
  - "The hidden `press c first` is gone. The panel's last row is `something else…`, and the pointer standing on it makes it an inline box; `c` still moves the pointer there."
  - "The status chip said how many questions were waiting. It says WHICH one, in the question's own words, when there is one."
  - "`/autonomy` was the only door to this project's question rules. They have a row on the settings page's Safety tab — the same reader and the same writer — and the manual names the row before the command."
  - "`internal/remote` carried `SetAutonomy` and no `Autonomy`, and the ordinary launch talks to its own engine through that client — so `/autonomy` answered `this conversation has no project to keep question rules in` on every machine, whatever project it was in. The read crossed the wire; it does now, and `remote.Version` is 15 for it. An engine with no such door REFUSES rather than answering `{}`, and the settings page keeps nil (not read) distinct from empty (a project with no rules yet): an empty map drew every kind as `ask me` on an engine that might be on `decide`."
  - "A question that offers more than one lifetime draws them as a row under its answers, cycled by `t how long`, defaulting to `just this once` and absent on anything irreversible. A PERMISSION IS NOT ONE OF THEM: a consent's answer is read as its key plus the banked comment, `session.Answer.Scope` never reaches the gate, and a row that let somebody pick `for this project` while the engine granted one call would be a promise about safety with nothing behind it. The engine-side seam is to read `Answer.Scope` in the consent path and map it onto the gate's own lifetimes."
  - "The digits row spelled `1–3` over the answers `1` and `3` — the ordinary shape of an irreversible gate, where the engine drops the widening answer — which named a key nothing answers. A range is drawn only where the keys run."
---

A question is the one object on this surface a person MUST act on, and it was
the object with the least agreement about how it is drawn: four renderers, four
ways of deciding which of them you got, a colour spent on whole rows that every
other surface spent on one mark, and a row of keys that gave up the way out
before it gave up `compare`. The owner ruled on eleven of those questions on
2026-09-11 and this is the first piece of the answer.

What holds it together is that each decision now has ONE place. Which drawing a
question gets is a ladder of the question's own properties — what it carries,
never which lane raised it — so two questions with the same evidence are drawn
the same way. What a frame is, is one type. What a key says is one table, read
by the block, by the page, by the manual and by the tmux suite's needles. Where
a permission's pointer opens is one line derived from the stakes, so no lane can
make `enter` mean yes on something that cannot be taken back by choosing a
different tool name.

The colour is the change that will be felt first. Amber on the marks alone means
a screen with a question on it has three coloured cells rather than four
coloured rows, and the thing a person is being asked is the only thing lit.
