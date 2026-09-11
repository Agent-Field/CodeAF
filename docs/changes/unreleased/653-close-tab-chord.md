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
missing. `ctrl+w` is now the ✕ on the tab in front said with the keyboard. An
idle tab dismisses immediately; one writing a reply, running tasks or holding a
question first offers `keep running`, `stop work` and `cancel`, with the cursor on
the non-destructive choice. Keeping work preserves every draft and caret and leaves
the conversation under `ctrl+k`, Home and `ctrl+shift+t`; stopping cancels only that
conversation's reply, tasks, adaptive runs and jobs. On the new-chat page it is
that page's own ✕ — the page comes down, the conversation underneath comes back,
and the half-written first message is parked. Closing the last tab lands on Home.

The trade is the readline word kill in the message box, and it is stated rather
than hidden: `alt+backspace` is what a Mac keyboard sends and `ctrl+backspace` is
what Windows sends, both still delete the word behind the caret, and `ctrl+w`
still edits text filters such as the model picker and settings search. The
conversation switcher owns the chord separately: it dismisses the selected
background tab while preserving its work, or uses the normal close card for
the current conversation when that conversation is active.
