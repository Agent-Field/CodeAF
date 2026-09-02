---
kind: changed
title: a person's manual answer heads each section with a line no page can contain
pr: 446
surface: [chat, docs]
invalidates:
  - "`aforge manual \"<question>\"` and the chat's `/manual` note labelled sections `[page · heading]`, the same shape the model reads. A person's sections are now headed `## page · heading`; the model's `Render` still uses the bracketed label, so a test or a prompt that quotes the model's shape is unchanged."
  - "A grep on `^\\[` over `aforge manual` output counted quoted screen lines as labels — eight chat pages quote lines that begin with `[`. Only `## ` begins a label now, and by construction no section body line can start with it, because the splitter cuts pages at exactly that prefix."
---
