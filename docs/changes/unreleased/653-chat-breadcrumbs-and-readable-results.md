---
kind: changed
title: Named chat breadcrumbs and readable expanded task answers
pr: 653
surface: [chat]
invalidates:
  - "Main chat had no pinned identity, and task headers named only main and the current task. The bar now names the conversation and actual task ancestry; ancestors and folds open their task, and the main chat name offers the conversation picker. Guest ancestry is shown without borrowing another conversation's task ids."
  - "Expanded task cards printed metadata and the report as a dim wall of raw Markdown. They now lead with delivery information, render the produced answer as Markdown, and group model, price and time afterward. Exact report previews of that answer are omitted. Failed delivery is labelled separately from accepted work; intentionally kept branches remain neutral."
---

The current task crumb is inert. Deep or narrow trails fold ancestors without losing
the nearest hidden parent's click target. Ctrl+k opens a deliberate preview, and mouse
users reach that same picker from the named chat header.
