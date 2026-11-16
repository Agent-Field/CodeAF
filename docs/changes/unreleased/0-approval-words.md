---
kind: renamed
title: The approval mode is "tool approvals" everywhere, and its answers say ask, allow and block
pr: 0
surface: [chat, docs]
invalidates:
  - "The settings sheet labelled the tools.approvalMode row \"ask before running\" while the search and the settings tool called it \"tool approvals\". The sheet, the registry and the manual now all say \"tool approvals\"."
  - "The registry labelled tools.approval \"tool approvals\" while the sheet called it \"tool exceptions\". Both now say \"tool exceptions\", so the label no longer collides with the mode row."
  - "/permissions tailed a deny rule with `refused` and a prompt rule with `asks every time`. It now says `block` and `ask`, the sheet's own block by default and ask by default without the default."
  - "The approvals chip, /status and home spelled the deny posture `refuses`. They now say `blocks`, and the note after a move says every call the rules do not name is blocked."
  - "The stored values are unchanged: tools.approvalMode still holds prompt, allow or deny, and rules are still written bash:prompt or deny rm -rf *. Only the words a person reads moved."
---

Follows #1301 using the words #1792 put on the settings sheet (ask by default,
allow by default, block by default) instead of the #1301 draft (ask every time,
run without asking, never run). The draft does not match the gate: ask by default
still runs read-only calls without asking, and allow by default still asks about
critical shell commands and anything sent in your name.

The consent card's per-call answers (`allow once`, `always`, `deny`) and the
transcript's `allowed` / `denied` are unchanged. The panel-versus-registry label
mismatches other than these two rows are left to #1303.
