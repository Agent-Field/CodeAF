---
kind: fixed
title: a picture dropped on home becomes a picture, on every box that can take one
pr: 160
surface: [chat]
invalidates:
  - "The drop door — a dragged file becoming a chip on the tray with `[image #n]` in the words — belonged to the conversation's draft alone. It is `app.pasteFilesInto(box, chips, text)` now and every box that can start a message takes it: the draft, home's line at the foot, and the `ask here` pane's. `app.pasteFiles` is the draft's call to it."
  - "A drop that named nothing on this machine was inserted as text in silence. It now says `<name> is not on this machine` — or `<n> files are not on this machine` — and the text still stays where it landed. The sentence is owed only to a paste that is plainly a drop (dropkeys.go's `droppedPathShape`: every word an absolute path), so a sentence that mentions a file notes nothing, as before."
  - "Home's box was read as its filter verbatim. `homeView.query` now strips `[image #n]` before matching, and `homeView.searching` asks whether anything is typed OR held on the tray (`homeView.carrying`) — so a file-only drop, which writes no word at all, still lights `start` and still sends."
  - "`app.editTags` and `app.spacedTokens` were bound to `app.input`. Both are `editor` methods now; the two `app` functions are thin calls to the draft's."
  - "A dropped path that reached `enter` on home was answered `unknown command: /var/folders/…`. Home falls into the same net chat's dispatcher does (`app.droppedLineInto`), after the command and typed-folder readings of that row and never before them, and `homeView.runLabel` no longer offers to run a line whose every word is a path."
  - "`errandSend` called `Submit`. It calls `SubmitImage` when the errand's tray is holding pictures, and names ordinary files through `remote.AttachedSentence` exactly as a local conversation does. `homeExchange` grew a `chips` field: an errand's tray is its own."
---

Drag a screenshot onto the terminal while home was up and the box filled with
`/Users/x/Desktop/Screenshot\ 2026-08-31\ at\ 5.21.40\ PM.png`. `app.paste` had
an early return for `pageHome` that inserted the text and stopped; the drop door
was on the fall-through below it, which is the conversation's draft. So the one
screen a person is most likely to be looking at when they drag something in was
the one screen that could not take it — and `enter` then handed the path to the
slash router, which said `unknown command` about a file that plainly existed.

The door is parametrized by the box now rather than copied, so there is one
answer to what a dropped file is and one tagging system behind it. Home draws the
tray directly over its own box, `start` counts a file on it as something typed,
and the pictures ride `app.renew` into the conversation the row opens — which is
the same law that already carried the draft: the message goes with the person.

Two things this deliberately does not do. **A keystroke-shaped drop on home is
caught at `enter` and not while it is arriving**: `dropkeys.go`'s fold is still
wired to the conversation's editor alone, because moving it would mean the fold
carrying a box as well as a range, and the enter net gives home the same safety
chat has. **A tray held while `start`ing in a typed folder stays with the
conversation being stepped aside from**, not with the person — `app.startBeside`
stows the whole aside, draft and chips together, and both come back when you
switch back to it.
