---
kind: added
title: a picture's step row names the image model, and opening it shows the prompt
pr: 1026
surface: [chat]
invalidates:
  - "A `generate_image` step row said nothing but the tool's name and a clock. It now reads `generate_image circle.png · seedream-5` — the file it wrote, and behind the middle dot, dim, the image model that drew it, in the short spelling (`modelui.ModelWord`) this surface uses for a model everywhere else. An unknown model draws nothing there rather than a placeholder."
  - "Opening a `generate_image` step showed the picture and the path and nothing else. It now leads with what went IN, as prose: the prompt quoted and wrapped (capped at twelve lines, lifted by the usual `… N more lines`), one dim line per other input the call gave, then `drawn with <model>` — or `asked for best · drawn with seedream-5` when the call asked in its own words. The picture and its whole path still close the block."
  - "A RUNNING `generate_image` step showed only its clock when opened. It now shows the same prompt block above the clock, the way a running `bash` call already shows its command."
  - "Nothing on the wire carried which image model a call used. Nothing on the wire carries it now either: the row reads the tail of the tool's own result line, `… , generated on <model>`, which is where the model that the request actually carried has always been written. There is no new event field and no second derivation — internal/session's `describeGeneratedImage` remains the one authority, and `internal/session/tools_image_test.go` pins that sentence's shape."
  - "`generate_image` and `view_image` rows had no target at all, so no path on them was clickable. Both now draw the file they are about, resolved by the same `app.picturePath` rule the preview uses (arguments first, then the result, absolute beating relative), and the path on the row is a door like every other path on this surface."
---

`generate_image` is the one hand whose model a person chooses per call, so the
step row is where "which image model is this?" gets asked — and it was the one
row that could not answer. Both halves ride roads the row already draws from: the
call's arguments for what was asked, and the tool's own result for what actually
drew. The model is resolved inside the tool against the live catalog, so the
result is the only place the answer exists; a surface that showed its own default
instead would be wrong the first time a hosted task room drew a row.
