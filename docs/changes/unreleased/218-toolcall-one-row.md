---
kind: fixed
title: a tool call in the transcript is one row, and the row is measured in the bytes it draws
pr: 218
surface: [chat]
invalidates:
  - "A tool call's expansion was said to truncate rather than wrap. It did not: the block hanging under an open call was laid out to the whole frame and then moved two columns right by the indent pass, so every row of it was two cells over the frame and the terminal folded each one onto a second visual row. One open `bash` call ate four or five rows. The block now subtracts `workIndentCols` before it is sized, and nothing a tool call draws overhangs the frame at any width."
  - "Only a background job's log was cleaned of somebody else's control bytes (`jobLogLine`). A tool call's own text was not, and a tab, a carriage return or an escape sequence in it measures nothing and draws something — so a command with tabs was fitted to the frame and wrapped anyway, a progress bar's carriage return made a row overwrite its own head, and a command that printed colour painted the transcript. That rule is now `drawableLine` and it covers the call's target, its name, its arguments, its command block, its output, and a `read`'s or `write`'s source."
  - "A tool's own colours no longer reach the frame. The syntax colouring on a `read`, a `write` or a `bash` command is aforge's own, applied after the cleaning."
---

The clamp to one row was never the missing part — `app.toolLine` has fitted itself
to its width since the right column was rebuilt. What was missing was that the
width it was handed was the width it is drawn in, and that the text it measured was
the text the terminal draws. Both halves are now stated where they can be checked:
the indent law is subtracted by the block as well as by the line, and everything a
tool puts on the screen goes through one function that makes it drawable.
