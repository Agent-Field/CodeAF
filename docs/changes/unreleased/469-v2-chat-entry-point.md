---
kind: removed
title: the v2 chat entry point, and everything only it reached
pr: 469
surface: [chat]
invalidates:
  - "`cmd/aforge/chat.go` opened with `runChat`, so the file a reader opens first when asking what `aforge chat` does described a window that claimed a resident role and ran the scheduler. Nothing had dispatched it for weeks — `main.go` sends `chat` to `runChatV3` in every arm — and it is gone, along with `chatResidency` and its twenty-nine methods, `buildChatBrain`, `chatBrain.standDown`, and the conversational tail of `buildBrain`. `aforge chat` is `internal/tui3` over `internal/session` and reaches none of chat.go."
  - "`brainOptions` had a `headless` field and a `hand` field, and `buildBrain` was described as one construction with two shapes — a window and a one-shot. There is one shape. `aforge do` no longer passes `headless: true`, and the branches that built a head, a commander, voice, a stream, the narrator, the arrival brief and the standing-watch installer went with the driver that was the only one that could take them."
  - "`cmd/aforge` imported `internal/tui` (v1's surface). It does not. `head.New` — the conversational head — now has no caller anywhere in the tree; `head.NewCompiler`, `head.ModelWords` and `head.WorkModelChoice` are `aforge do`'s and are untouched."
  - "`chatBrain` had a `leafGrace`, and closing a window waited up to `windowLeafGrace` (two minutes) for running leaves to land. Nothing can set a grace now, so the field, the wait and the constant are gone; `stop()` keeps the ordering that mattered — cancel the housekeeping, drain the dispatcher, and only then take the work's own context away — and says so as a law rather than as a window's manners."
  - "`residency.go` held both `chatWindow` and `chatResidency`. It holds the window alone and is named `window.go` for it."
  - "The alias spellings `chatCommander`, `chatPrefs`, `chatMediaModels`, `newVisitorCommander` and `saveChatPrefs` are gone; `internal/command` itself is untouched, and the tests that drive it name the package. `loadChatPrefs`, `attachmentStoreRoot` and `newSessionID` stay, because an errand still uses them."
  - "Nothing gated a command entry point that had lost its dispatch. `TestEveryCommandEntryIsReachableFromMain` in `cmd/aforge/deadroad_test.go` walks out from `main` over `go/ast` and fails on any `run<Command>` nothing can reach, so the next dead road announces itself in the change that orphans it."
---

An entry point with no caller is worse than an unused function: it is an account
of how the program works, sitting at the top of the file somebody opens to find
out. This one said a chat window took a resident role and ran the scheduler, and
it cost a whole recon pass on #323 before it was filed as #329.

Everything `aforge do` shares — `buildBrain`, the leaf settlement path,
`leafSpend`, `exhaustionWords`, `jobPlans` and its live revision roads — is
untouched, and both doors were run against a real model to prove it.
