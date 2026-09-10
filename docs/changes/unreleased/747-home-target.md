---
kind: changed
title: The box at home is a draft, and the rule above it says which conversation
pr: 747
surface: [chat]
invalidates:
  - "`enter` at home opened a conversation in this window's own workspace and ignored the row under the cursor. It opens one at the TARGET — the folder on the rule above the box, which follows the cursor's row until `alt+w` pins it."
  - "The scope chip `here ~/aforge-v2` sat against the right of home's box row. Home has no chip; the rule one row up says `→ new conversation in ~/aforge-v2 · glm-5.3-flash` instead. The other six places keep the chip."
  - "`/model` at home switched the model of the conversation behind the screen, wrote the answer where it could not be read, and saved the choice as the launch default. It pins the DRAFT and says `model · glm-5.3 · for the next conversation you start here`; nothing behind home is touched and no default is written."
  - "Ten commands at home (`/model` `/resume` `/folder` `/attach` `/files` `/crew` `/permissions` `/connect` `/harness` `/subharness`) opened an overlay home cannot draw and the keyboard cannot reach, which then appeared over the conversation the next `esc` landed in. `showPage` closes every one of them; `/model` draws its list in home's own body; `/resume` answers in one line; `/folder` and a bare `/attach` are the two bullets below; the rest open a conversation at the target and run there."
  - "`/folder`, `/place` and `/dir` at home answered in one line — `alt+w moves the next conversation · or type a path` — which named two gestures and drew neither; the owner did not notice it was there. All three open the FOLDER BROWSER now, aimed at the target: the sheet is titled `the next conversation's folder`, its action row reads `open the next conversation in · <path>` instead of `add this folder`, it never offers `remove this folder`, `enter` pins `homeTarget.where` and lands back on home saying `next conversation opens in <path>`, and `esc` lands back on home having changed nothing. `/folder <path>` from home opens the same target sheet rather than the conversation's."
  - "`/attach <path>` at home wrote a chip onto the tray and said nothing; `/image <path>` OPENED A CONVERSATION at the target first and ran there. Both stay on home now and say `attached · <name> · rides with the next conversation` — home's tray is already carried into the conversation home opens next. A bare `/attach` says `type the path after /attach · or drop the file here` instead of opening a conversation to hold a browser, and a FOLDER after `/attach` at home pins the target (`next conversation opens in <path>`) instead of being referred to the conversation behind the screen."
  - "A bare `/task` at home opened a conversation at the target and then opened the task page over it. It is a page like `/history` and opens as one, with no conversation started."
  - "Every row of home's `/` drop-up read `<word>   a command · <note>`. It reads `<word>   <fate> · <note>` now, where the fate is what `enter` will do ON HOME — `pins the next conversation's model`, `next conversation's folder`, `opens the page`, `this list is /resume`, `onto home's tray`, `opens a conversation here first`, `answers here`, `runs on the conversation behind home`, `a fresh conversation behind home`, `closes the conversation behind home`. `homeFate` in homeslash.go is the ONE table: the row draws it and `homeSlash` switches on it, so the list cannot promise a road the dispatch does not take, and a command added without a fate fails the build."
  - "`/new` at home replaced the conversation behind the screen and said `new session · <path>`, which on home reads as though it were about the conversation `enter` opens. It says `started a fresh conversation behind home`, through `renewRefusing` so a refusal still reaches the same line."
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

The wave's second half is the one the owner tested: a draft you cannot see the
consequences of is not a draft. Every command now says what it will do to that
draft before it runs, the two that were about files stopped opening
conversations to hold them, and the question the rule above the box asks —
which folder — is answered by the browser that already exists rather than by a
sentence naming a chord.
