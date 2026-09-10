---
kind: fixed
title: dev is green again — the question-demo pin is plumbing, and a retry's room is counted, not sent
pr: 805
surface: [chat, engine]
invalidates:
  - "`AFORGE_QUESTION_DEMO` (the question page's fixture door, #780) was registered nowhere, so `TestRegistryCoversEveryUserFacingEnvironmentPin` failed on every full run. It is operator plumbing beside the other one-launch pins."
  - "`TestAnEmptyFirstVerdictIsAskedAgainWithRoomAndItsAnswerStands` asked the gate's retry for a bigger `max_tokens` than its first call. Since #665 the shaped seam sends no `max_tokens` at all — the room is what a reply is counted as, never what it is sent with — and the test states that law."
---

Test-only and a registry line. Both were red on dev's full run at `81fbbdf0b`
and by name at `069038e4d`; nothing a person sees changes.
