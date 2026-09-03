---
kind: fixed
title: every door that opens the chat surface parks the standard logger in chat.log
pr: 450
surface: [chat, remote]
invalidates:
  - The standard-logger redirect into the profile's chat.log was true of the in-process door only; the ssh (--host), relay (--at) and unix-socket doors left the logger on stderr, and it is now every door's, done once in cmd/aforge's runSurface.
  - The four doors no longer call tui3.Run directly; cmd/aforge/chatv3_surface.go is the only path to it, and a test reads the package's sources to keep it that way.
  - internal/guard's comment claiming a recovered fault never tears through the alt screen was true of one door in four and is true as written now.
---

A log line written while the surface owned the terminal landed on the alt screen as a raw
row on three doors in four — a recovered fault with its stack, a checkpoint warning, a
media fallback — spliced into whatever the person was typing and gone with the next
repaint, so a fault on a connection had no findable home. A surface that owns the terminal
owns the logger, and it owns it from the one place every door passes through: runSurface
arms the byte meter, parks the logger in the profile's chat.log for the surface's whole
lifetime, and hands the terminal back with both undone. A profile that cannot take the
file keeps stderr, because a lost frame is better than a lost warning.
