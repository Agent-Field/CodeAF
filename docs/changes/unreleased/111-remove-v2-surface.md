---
kind: removed
title: the v2 chat surface is gone — tui2 is now only the library v3 draws with
pr: 111
surface: [chat, resident, build]
invalidates:
  - "`aforge chat --v2` opened the older surface, and AFORGE_CHAT_V2=1 did the same. Both are gone: there is no v2 door, and the binary has exactly one chat surface."
  - "`internal/tui2` was 'the older v2 surface'. Thirteen of its nineteen packages are deleted; what remains — `tokens`, `blocks`, `prose`, `modelui`, `reltime`, the root compositor — is a shared component library, not a surface."
  - "The settings sheet offered `nerd font`, `linear mode` and `sidebar` rows. All three steered only the dead surface and are removed everywhere — a toggle whose value nothing reads is worse than no toggle. v3's accessible rendering is the `--linear` launch flag, not a persisted row."
  - "The resident had an owner-pinned room policy selected by AFORGE_CHAT_V2. It is gone; the attached-session re-homing is the only delivery policy."
  - "`TestRegistryCoversEveryUserFacingEnvironmentPin` was a known-red. It is green and out of the ledger."
  - "`internal/tui2/settings` was the package on the wire at all three full-suite runner deaths. It no longer exists."
---
