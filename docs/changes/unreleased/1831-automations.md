---
kind: changed
title: automations replace standing orders, and rules become memories marked always
pr: 1831
surface: [chat, engine, remote, resident, docs]
invalidates:
  - "Something to happen later was a standing item, made with `stand` and listed on `/standing`. It is an automation now: the `automation` tool, the `/automations` place, and `/automations add …` for an exact line."
  - "Standing items fired from an OS timer running `codeaf tick` with no window open. Nothing is installed on the machine any more: automations run only while a codeaf window is open, one `codeaf clock` process runs them, and the first start removes the old timers. Closing codeaf pauses them: a run it cut off runs again, once, when a window next opens."
  - "A rule (always use tabs here) was a standing order. It is a memory marked always now: `/always <text>`, or `remember` with always, which asks first."
  - "`internal/standing` held the ambient side. It is deleted; `internal/automation` holds the store, the schedule and the clock."
  - "The settings sheet had a `background checks` row. It is gone, and `standing.background` is a retired key."
  - "Standing items were kept across builds. They are not migrated; `~/.codeaf/v3/standing/` is left on disk and no longer read."
  - "Home's `ask here` errands lived under the standing root. They live in `~/.codeaf/v3/errands`."
  - "The remote wire carried `Standing.*`, `ResolveStanding` and a marked submit. They are gone; automations cross as `Automations.*` on wire version 21."
  - "A plan-born worker's brief closed on the standing orders and the rules. It closes on the rules alone."
  - "An unattended goal owner said yes to a standing card on the person's behalf. An automation card waits for the person."
  - "The v1 resident's charters, watches, practice loop and `codeaf wake` pass ran on a timer. They are removed; `codeaf wake` and `codeaf tick` are hidden verbs that do nothing."
  - "The tab bar had seven words. It has eight: `Automations` stands between `Memory` and `Spend` (still `alt+7`), and under 84 columns `Spend` and `Settings` fold behind `more ▾`."
  - "A turn's banner was always OSC 777, which iTerm2 ignores. In iTerm2 it is OSC 9 now, and an automation's banner rides the terminal there and in Ghostty, kitty, WezTerm and foot, so clicking it brings back the tab."
  - "The memory tidy ran on the standing pass. Nothing runs it now; the clock does not."
  - "Home's `since you left` panel whispered `what watches and tasks did while the terminal was shut`. It whispers `what happened while you were away`, because nothing runs while codeaf is shut."
---

The design is docs/design/automations/DESIGN.md.
