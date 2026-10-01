---
kind: fixed
title: settings says checker, no zero durations or byte counts, and help and manual match the screen
pr: 1711
surface: [chat, docs]
invalidates:
  - "/settings → Providers labelled the checking role `auditor`; it says `checker` (a saved pin keeps the `auditor` id)."
  - "A thinking row could say `thought for 0s`; an unknown or sub-second duration now draws nothing."
  - "A senior-dev submission in a plain folder said `0 bytes across 3 file(s)`; an unmeasured size draws nothing."
  - "`codeaf doc --help` printed `codeaf doc` without its PATH; it reads `codeaf doc PATH [--pages A-B]`."
  - "The manual said `codeaf --help` prints the environment table (it points to `codeaf help env`), that /settings has nine tabs (ten, with Teams), that a launch with a key always opens on Home (a first launch with nothing elsewhere opens the chat's greeting), and that an untitled manager is `@manager-…` (it is `@<team>`, or `@lead` for All teams)."
---
