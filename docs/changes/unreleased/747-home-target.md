---
kind: changed
title: The box at home is a draft, and the rule above it says which conversation
pr: 747
surface: [chat]
invalidates:
  - "`enter` at home opened a conversation in this window's own workspace and ignored the row under the cursor. It opens one at the TARGET — the folder on the rule above the box, which follows the cursor's row until `alt+w` pins it."
  - "The scope chip `here ~/aforge-v2` sat against the right of home's box row. Home has no chip; the rule one row up says `→ new conversation in ~/aforge-v2 · glm-5.3-flash` instead. The other six places keep the chip."
  - "`/model` at home switched the model of the conversation behind the screen, wrote the answer where it could not be read, and saved the choice as the launch default. It pins the DRAFT and says `model · glm-5.3 · for the next conversation you start here`; nothing behind home is touched and no default is written."
  - "Ten commands at home (`/model` `/resume` `/folder` `/attach` `/files` `/crew` `/permissions` `/connect` `/harness` `/subharness`) opened an overlay home cannot draw and the keyboard cannot reach, which then appeared over the conversation the next `esc` landed in. `showPage` closes every one of them; `/model` draws its list in home's own body; `/resume` and `/folder` answer in one line; the rest open a conversation at the target and run there."
  - "A command's answer at home landed only in the conversation behind the screen. The first line of it is now on home's own message line as well — including `there is no command called /x · / lists them`."
  - "`ctrl+t` at home refused a typed sentence with `clear or send your message first · ctrl+t starts fresh in <path>`. That refusal is deleted: `ctrl+t` is `enter` for the row under the cursor and carries whatever is typed."
  - "Home's foot named `ctrl+enter ask here`. The chord is still bound and is no longer advertised — most terminals cannot send it — and the foot reads `enter starts a new conversation and sends this · ↑ ask here · ↑↑ pick a match · esc clear`. `alt+enter` was never `ask here`; it is the task layer, on home as on every place."
  - "`alt+w` and `alt+o` were swallowed on every place. Home claims them: `alt+w` walks the folder round this machine's projects, `alt+o` opens the model list. Pressing either label on the rule does the same as its chord."
  - "The vocabulary had no mark for a destination. `tokens.GTarget` (`→`, geometry) is that slot, and U+2192 is now a vocabulary byte — `internal/tui2/modelui/result.go` carries a literal exemption for the `String()` on its log line."
---

Home's box was three things at once — a new conversation, a live query, a command
— and it said where exactly one of them would land, in a chip `enter` did not
read. The rule above the box is that chip with the disagreement removed: it says
the folder and the model the next conversation will open on, and `enter` honours
both.

The folder pin is spent when a conversation starts from home; the model pin is
not. A folder is where *this* sentence goes and a model is how you like to work,
and the owner ruled on that split rather than leaving it to be discovered.

`docs/design/icons/DESIGN.md` carries the new mark and the one cost of taking a
character this common.
