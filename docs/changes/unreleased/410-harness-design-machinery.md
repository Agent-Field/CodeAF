---
kind: fixed
title: the standalone harness-design tool renders a brief again, and both doors read one machinery map
pr: 410
surface: [chat, build]
invalidates:
  - "The standalone `harness-design` tool could not render a brief since 2026-08-18 because its machinery map drifted from the in-chat one; both doors read one exported `subharness.Machinery` map now."
  - "The designer guide never told the model that JSON delimiters must be ASCII; only the reviewer guide and two Go constants bolted on by each door did. The guide's own output contract says it now, and the constants are gone."
  - "The four `cmd/harness-design` tests are out of `.github/known-red.txt` and out of CLAUDE.md's list of clean-tree reds."
  - "`TestMachineryIsExactlyWhatTheGuidesRead` was vacuously green while `«kinds»` failed every partial render; it does its real job again."
---

Two doors render `internal/subharness/prompts/designer.md` and each kept its own map
of the guide's placeholders, so a placeholder the guide renamed was fixed in one door
and broke the other for a fortnight. The one-source-of-truth law says the fix is one
map both read, beside the numbers it quotes.
