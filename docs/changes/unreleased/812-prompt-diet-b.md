---
kind: changed
title: A tool result is cut to a tenth of the model's window, and each description quotes its own cap
pr: 812
surface: [chat, engine]
invalidates:
  - "`read`, `bash`, `grep`, `find` and `ls` cut every result at 2000 lines or 50KB whatever model was in use. They now cut at a tenth of the model's context window, with 2000 lines / 50KB as the ceiling: a window of 128,000 tokens or more — every frontier model, and what aforge assumes when a model card says nothing — is byte-identical to what it was, while a 16k model is cut at 250 lines or 6.25KB. One `read` used to be 78% of everything a 16k model could hold."
  - "The truncation figures in the tool descriptions were literals typed into the string. They are rendered from the caps in force at belt build time, so a description can no longer promise a budget the tool is not applying. `internal/session/tools_pdf.go` kept its own copies of the numbers (`pdfMaxLines`, `pdfMaxBytes`); both are gone and the extracted-text path takes the same `bare.Caps` the file path does."
  - "`bash`'s description carried the routing law — work whose outcome is a deliverable belongs to `propose_task` — and said three times that a job reports itself and must not be polled. The routing law is the page's table and the manual's `A job is the wrong door for work whose result is a deliverable`; the arrival law is stated once. `read`, `bash` and `write` were cut to their contract in the same pass."
---

The caps live in `internal/ctxbudget.ToolResultBytes` so there is one formula, and
`bare.CapsFor` derives the line cap from the byte cap in pi's own proportion, so which of
the two binds a given output does not change with the window.
