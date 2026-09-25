---
kind: changed
title: a conversation's tip covers the project on the keys row, with a cross, after 15 quiet seconds
pr: 1487
surface: [chat, docs]
invalidates:
  - "A conversation's earned tip was the keys row's lowest rung and replaced the whole row, so from the first exchange onward the controls and `space space home` were gone while a tip stood. The controls keep their place now, and the tip stands at the keys row's right end, covering `project: <path>` while it is up, with home's bulb and a cross; the project is back when it goes."
  - "A conversation's tip had no clock and no cross. It waits for 15 seconds with no key, click, scroll or paste and none since a turn ended or codeaf opened, and its cross puts it away (giving the project back) until the conversation is left (home, a place, another conversation) and come back to. Home's row does not wait."
  - "The Workspace tab's `disable hints` row explained itself in a sentence about which rows it silences. The line under it reads `disable💡 tips everywhere (requires restart)`. It still silences both rows and the what's-new lines."
  - "A conversation's tip was counted as shown when its slot took it, once per session. It is counted when the keys row first draws it, so a tip hidden behind the 15-second wait all session costs none of its six showings."
  - "The tip list was twenty-two rows. It is twenty-three: `space space takes you back to home`, the first row a conversation earns by talking (ahead of `/task`), armed only in a conversation whose door home opens and never on home, and retired by the two-space gesture itself (the new `home-gesture` event) — not by `/home` or the tab."
---
The controls are the keys that work right now and a tip is only a suggestion,
so the tip takes only the project's place, and only once somebody has stopped working.
