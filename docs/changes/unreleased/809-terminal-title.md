---
kind: added
title: The terminal's own tab says where you are in aforge, in Terminal.app and iTerm2 too
pr: 809
surface: [chat, docs]
invalidates:
  - "In Terminal.app and iTerm2, an aforge tab showed the running binary's path (`/Users/santos…`): the surface declared its title only through Bubble Tea v2's `tea.View.WindowTitle`, which is OSC 2, the window title, and those two terminals label a tab with the icon name, OSC 1, which nothing wrote. NOW the same sentence goes out both ways: OSC 1 through `tea.Raw` from `app.Update` (`app.retitle`), only when it changed, and OSC 2 on the view. tmux, kitty and Ghostty read OSC 2 and are unchanged."
  - "Bubble Tea v2 has no `tea.SetWindowTitle` command; a ruling or a memory that names it describes v1. The v2 door is `tea.View.WindowTitle`, and anything else, like the icon name, goes through `tea.Raw`."
  - "The title was `project · conversation name`, with a `?` when any conversation in the window was waiting and a `✓` when a turn landed while the window was blurred (`windowtitle.go`, `app.landedAway`). NOW `internal/tui3/title.go`'s `terminalTitle` owns it, and it names the place, not the project: `aforge` on home at rest, `N want you · aforge` on home, `<short title> · aforge`, `new conversation · aforge`, `? <short title> · aforge` when this conversation waits on you, `<task label> · task · aforge`, `<place word> · aforge`, and ` @ <host>` before ` · aforge` over `--host`. The `✓` and `landedAway` are gone."
  - "A title outlived aforge until the shell's next prompt replaced it. NOW an empty OSC 1 is written after the program stops, on every way out, and Bubble Tea clears OSC 2 as it closes."
  - "`oscSafe` (notify.go) dropped only `;`, BEL, ESC and C0. NOW it drops every control character, C1 and DEL included."
---

The title is read when nobody is looking at the window, so it answers the one
question a glance from another tab can use: where in aforge this tab is standing,
and whether it is waiting on you. It is plain text within sixty cells, and the
name is cut before the ` · aforge` that tells an aforge tab from a shell's.
