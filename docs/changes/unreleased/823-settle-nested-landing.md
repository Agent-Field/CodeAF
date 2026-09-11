---
kind: fixed
title: Nested landing accept banks the question and writes `you took this as done`
pr: 823
surface: [chat, engine]
invalidates:
  - "Landing questions were raised without banking their words, so ResolveQuestion often saw said=false and emitted no EventQuestionAnswered. They bank on raise now, and a resolving landing answer still emits the answered event when nothing was banked — so a --host surface that only drops questions on answered or withdrawn cannot keep a settled landing open."
  - "publishLandingQuestion returned early on settle when landingAsked was empty, even if the question was banked or only drawn from OpenQuestions replay. Settle now retires from the bank (or the standing shape) either way."
  - "The Spark combine for #807 reported tui3 settle-path reds (path chips, jobs log reader, OpenRouter connect). On clean origin/dev those names are green; the reds were combine noise against a tree that lacked #783's nested-card fix and #801's harness-clock fixes, not a live settle bug on trunk."
  - "statesAnswerKey's comment still named settleCardKey and always walked ↑ before the letter. Since #789 ↑ moves the question block's pointer; the helper tries the bare landing letter first and keeps the ↑ walk as fallback."
---

The nested-landing e2e that waited for `you took this as done` was red on the
#807 combine tree because that tree never carried #783's rule that a part which
asked here also lands its answer here. Clean `dev` already had that card path;
this change hardens the question door beside it and pins accept through focused
tests.
