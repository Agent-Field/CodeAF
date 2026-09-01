---
kind: fixed
title: a typed drop lands on whichever box has the keyboard, and a start in a typed folder carries the tray
pr: 165
surface: [chat]
invalidates:
  - "dropkeys.go's fold was wired to the conversation's draft alone, and #160's own entry said so as a deliberate limit. The fold carries the box it is watching now (`dropFold.box`, `dropFold.chips`), so home's line at the foot and the `ask here` pane's take a keystroke-shaped drop while it arrives, exactly as the draft does."
  - "`app.dropWatch` took `(at, text)`. It takes `(box, chips, at, text)`: every router that inserts a character names the box it inserted into — input.go's, home.go's and homeexchange.go's."
  - "`app.spendDrop` converted into `app.input` unconditionally. It converts into the fold's box, and only while that box is still the one with the keyboard (`app.keyboardBox`) — a run left standing on a screen that closed is dropped rather than written into whatever took the keys."
  - "`app.startBeside` from home left the tray with the conversation being stepped aside from, and #160's entry recorded that as intended. `app.homeStart` lifts the chips out around the call now, so files dropped on home go with the person into the conversation the row opens. The DRAFT is unchanged: the stepped-aside conversation's own unsent sentence is still its own."
  - "Which box a paste lands in on home was decided inside `app.paste`. It is `app.keyboardBox` — one answer for the paste door and the keystroke fold both, and `app.dropLanded` is the follow-up either road owes."
---

Two follow-ups to #160, from issues #163 and #164.

**#163.** Some terminals and multiplexers deliver a dragged file as KEYSTROKES
rather than as a bracketed paste — the shape the owner met over `--host`. On the
conversation's draft that has been converted while it arrives since #160; on
home it sat in the box as raw text until `enter` caught it, and while it sat
there home filtered its list by a path no conversation on the machine matches,
so the column emptied and the action rows quoted the path back.

The fold now carries WHICH box it is watching as well as where the run began,
and it is one fold and one box at a time: a character landing in a different box
resets it, and it is checked again on the way out against the box that still has
the keyboard, so a run left standing on a screen that closed is never spent into
the box that took the keys. `enter` on home and in the `ask here` pane spends it
first, exactly as the composer's does — dropping a file and pressing enter inside
two frames still means the drop.

The perf ceilings keep their meaning and gain home's copies: prose typed on home
arms no timer and asks the disk nothing, a slash command costs what it always
cost, and a typed drop on home is one wakeup, one syscall per word, one drop.

**#164.** Starting in a folder typed on home goes through `startBeside`, which
stows the whole aside — draft and chips together — with the conversation being
stepped aside from. That is right for a switch and wrong here: the files were
dropped on HOME, for the conversation home is about to open. The tray is lifted
out around the call and put back after, which is the law `renew` already applies
on the other branch of the same door. Nothing is sent: what was typed named a
place and not a sentence, so the chips are on the new conversation's tray in
front of the person, waiting for the words they were dropped to go with.
