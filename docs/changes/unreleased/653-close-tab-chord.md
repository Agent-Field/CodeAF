---
kind: added
title: ctrl+w closes the tab in front, the way every browser closes one
pr: 653
surface: [chat]
invalidates:
  - "`ctrl+w` in the message box deleted the word behind the caret. It closes the tab in front now; the word kill is `alt+backspace` and `ctrl+backspace`, both unchanged."
  - "The only ways to close a tab were the ✕ on it and `ctrl+w` on the switcher card. The chord answers on the conversation itself now, and on the new-chat page."
  - "The `/help` key sheet named `ctrl+w` as the word kill. That row says `alt+backspace`, and a new row above it names the close."
---

`ctrl+t` opened a tab and nothing shut one, on a row that is drawn as tabs — so
the hand that had learned half the browser's grammar found the other half
missing. `ctrl+w` is now the ✕ on the tab in front said with the keyboard: one
call into `tabDismiss`, which is why it ends no work, interrupts no turn, keeps
every draft and caret exactly where they were, and leaves the conversation on
`ctrl+k` to come back from. On the new-chat page it is that page's own ✕ — the
page comes down, the conversation underneath comes back, the half-written first
message is parked. On the last tab the window lands on home with the work still
alive behind it, so leaning on the key is clicking the ✕ over and over.

The trade is the readline word kill in the message box, and it is stated rather
than hidden: `alt+backspace` is what a Mac keyboard sends and `ctrl+backspace` is
what Windows sends, both still delete the word behind the caret, and `ctrl+w`
still edits the filter of every modal overlay — the model picker, the switcher,
the settings search — because each of those is read above the chord and is
looking at the person who pressed it.
