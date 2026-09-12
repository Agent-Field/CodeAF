---
kind: fixed
title: a frame of ordinary calls opens on allow all, and dev is green again
pr: 994
surface: [chat, docs]
invalidates:
  - "`TestPermissionsFromOneStepAreOneFrame` expected four *costly* reads grouped as one frame to open on `deny all` wearing `safe answer`, and `dev` was red on it from the moment #946 merged — sixty-nine seconds after #985 shipped the ruling that `enter` on an ordinary call is `allow once`. The frame's pointer was always each question's own read over the set, so the code was right and the test was written against #933's deny-first-while-ungraded rule. The test now holds the ruling down: a group of ordinary calls opens on `allow all N` and its frame does not say `safe answer`; `TestTheGroupPointerIsEachQuestionsOwnReadOverTheSet` keeps `deny all` with its aside where one call alone would open on deny."
  - "The manual's *Several approvals at once* section said the pointer starts on `deny all`, marked `safe answer`, so that `enter` on an untouched frame denies them all. It says what the surface does: the pointer starts on `allow all N` when the calls are ordinary ones, on `deny all` marked `safe answer` only where one of them asked alone would open on `deny`, and a grave call never joins a frame — it asks on its own with the pointer on `deny`."
  - "The `-tags e2e` subtest `questionsGroup` waited for `safe answer` on the frame of four reads, an assertion the ruling made unmeetable on any run. It now waits for that word's absence, which is the observable of a pointer standing on the grant; the `questionGroupSafeWord` row in `tuiwords_test.go` says so."
---

Nothing in `.github/known-red.txt` moved: that ledger only shrinks, and this is the
fix rather than the excuse. The four screens under `docs/design/questions/screens/`
named `four-reads-in-one-batch-are-one-permission-frame-*` were captured before the
ruling and show `▸ allow all 4`; they were right by accident and are right on
purpose now. The next `TestQuestionsE2E` run rewrites them either way.
