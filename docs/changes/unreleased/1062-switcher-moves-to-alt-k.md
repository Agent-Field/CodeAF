---
kind: changed
title: ctrl+k kills to the end of the line, and the conversation switcher moves to alt+k
pr: 1062
surface: [chat]
invalidates:
  - "The conversation switcher is `alt+k` (`⌥k` on a Mac), not `ctrl+k`. Everything else about the card is unchanged — `enter` opens, `esc` cancels, `1`…`9` take a row, `→` reaches the fold, `ctrl+w` puts a conversation away — and `tab`, the `Chats ▾` control and a place's `alt+.` map are still the other ways in. The legend above the box now reads `alt+k switch`."
  - "`ctrl+k` is an EDIT now: delete from the caret to the end of this line, the pair to the `ctrl+u` that has always killed to the start of it. It is bound in the message box, in home's own box and the errand pane beside it, and in every typed filter and value box on the surface; it stops at the newline rather than joining two lines, and a second press on an emptied line does nothing. `internal/tui3`'s `killpairlaw_test.go` is the law that keeps the pair together — a key switch that answers one half without the other fails the build, with the first-run and onboarding string fields named as the two exceptions, since neither has a caret for an end-of-line to mean anything against. A whole-box flush — caret at the head of a one-line draft — goes on the kill ring first, exactly as `ctrl+u`'s does."
  - "The switcher's reverse is `alt+shift+k` and it now arrives on EVERY terminal. It was `ctrl+shift+k`, which an ordinary terminal sends as the same byte as `ctrl+k` and which was therefore bound only where the terminal had taken the kitty keyboard protocol's disambiguation flag. `ctrl+tab` and `ctrl+shift+tab` are unchanged and still need that terminal; `shift+tab` still walks the card back everywhere."
  - "On a Mac whose Option key composes accents, the switcher's chord types `˚` instead of opening the card — the tax every `alt+` chord on this surface already pays. `˚` is on the dead-key table now, and it is the ONE character on that table that arms the note in a conversation as well as on a place, because it is the one chord bound in both. The conversation draws the short form in its hint slot: `⌥ types a letter · turn on option as meta`."
  - "`config.KeyQuickSwitch`'s doc said the setting governed the switcher's own chord. It never has: `ui.quick_switch` governs `ctrl+tab` and its reverse, and the binding always browses. The doc says so now."
  - "The manual said `ctrl+k` saved a harness design from inside its room, on a row that was deleted along with the rest of that grammar. The room is answered by `1` save · `2` change · `3` drop like the page in the conversation, and nothing in the room is on a chord."
---

`ctrl+k` is kill-to-the-end-of-the-line in every shell on the machine, and this
surface already had the other half of that pair on `ctrl+u`. Spending the letter
on a card meant a hand that has typed that chord for twenty years got a list of
conversations instead of an edit, in the one box on the surface people type
into all day.

Every `ctrl+<letter>` here is spent, so the edit could not move somewhere else —
the switcher had to. `alt+` is the modifier class the places are already built
on, the letter is the one people have learned, and the move pays for itself on
the reverse gesture: `alt+shift+k` is a different byte from `alt+k`, where
`ctrl+shift+k` was not, so walking the ring backwards stopped being a thing only
half the terminals in the world could do.

What it costs is a Mac whose Option key is composing accents, where the chord
types `˚` and the card does not come. That is the same setting every other
`alt+` chord here needs, the surface already watches for it, and it now says so
in the conversation rather than only on a place.
