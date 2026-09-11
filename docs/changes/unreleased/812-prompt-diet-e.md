---
kind: changed
title: The page keeps existence, the load reply keeps the prose — long mechanics are pulled, not pushed
pr: 812
surface: [chat, engine]
invalidates:
  - "`prompts/system.md` carried a whole `# Things that keep working after this window` section, 2,482 bytes: the waking kinds, `when.kind: hold`, the `when.in`/`when.at` grammar, the RFC3339 arithmetic, the card's four answers, the background-checks row, and the `[something you set up fired]` frame. NOW it is one existence line — a reminder, a watch, a rhythm or a rule is `stand`'s, propose it and never do it instead. The mechanics are `standDescription` and `standSchemaJSON` (tools_standing.go), the firing's own line already carries `standingNewsRule` (standing_run.go), and the chat manual's keeping-an-eye page has the rest."
  - "`standing_absence_test.go` pinned eleven fragments of that section on a `mayStand` render (`WAKING OR HOLDING`, `when.kind: hold`, `SAYING WHEN`, `A CARD OFFERS`, `BACKGROUND CHECKS ARE ON AND NOBODY IS ASKED`, `[something you set up fired]` and the rest). NOW `TestTheSectionWithSchedulingIsExistenceAndNothingElse` pins the one-line present case and asserts those fragments are NOT on the page, because each is owned somewhere cheaper. The law moved, so the test moved with it."
  - "`capabilityGroup` (tools_capabilities.go) was a name and a member list. NOW it has a third field, `prose`: the paragraph a model needs the moment it loads that group, emitted under the `Loaded: ` line by `loadCapability`. `media`, `settings` and `harnesses` have one; `questions` has none. The `Loaded: ` line itself is unchanged and still first, because checkpoint.go's `loadedAndNeverUsed` reads the armed names off it up to its first full stop."
  - "The page's two paragraphs defining a sub-harness against a subharness (`programFacts`) were sent on every request of a belt that carries neither verb. NOW the page keeps one routing line naming `list_harnesses` and `build_harness` and the group to load them from, and the definition is the `harnesses` group's prose. `list_subharnesses` is named nowhere on the page; the loading verb's own catalog lists it."
  - "The accounts block was three bullets and 1,142 bytes — how an `<id>_request` tool is shaped, which methods are asked about, that a send is asked about first and never repeated, that what the person turned off is absent rather than failing. NOW it is one existence line. `serviceRequestDescription` already names the address and says which half of the account is turned off; `gmail_send`, `slack_send` and `calendar_create` already say the call leaves in the person's name and cannot be called back."
  - "`gmail_send` and `slack_send` said nothing about a message that goes unanswered. NOW both say NEVER SEND THE SAME THING TWICE because the first went unanswered, and the chat manual's accounts page answers \"does it resend an email or a Slack message when nobody replies\"."
  - "The Tool Policy carried a 710-byte media-making essay unconditionally, on a belt where every making verb is shelved. NOW the page keeps one line — anchor in a real medium, specify positively, `manual` for the rest — and the essay is the `media` group's prose. `generate_image` and `generate_video` already state it in their `prompt` field, where it is read at the call."
  - "`internal/session/prompts/adaptive.md`, `harness.md` and `subharness.md` were embedded and rendered on their own predicates. They stopped being embedded some time ago and were dead files; `adaptive.md` described `run_adaptive`, a tool this build no longer has at all. NOW they are deleted. The chat manual's adaptive-runs, subharnesses and saved-programs pages are where that material lives."
  - "The fixed prefix was 47,435 bytes (prompt 23,391 + tools 24,044). NOW it is 43,581 (prompt 19,537 + tools 24,044), 4,419 under an unchanged 48,000 budget. Nothing was raised and no law was dropped."
---

Every byte on the page is bought on every request of every turn, and most of what
came off it was mechanics for a verb the model was not carrying yet. The rule the
prompt diet files each law under is that existence is the page's — one line, so
the model can plan around a verb before it holds it — mechanics ride with the
verb in its own description, consequences ride with the event that causes them,
and the long runs of prose that only make sense once you are inside the thing are
pulled on demand, by `manual` or by the `Loaded:` reply of `load_capability`.
