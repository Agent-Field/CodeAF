---
kind: internal
title: Workstream pages render from editor JSON to markdown in internal/workstream/pagedoc
pr: 2277
surface: [app]
invalidates: []
---

`pagedoc.Markdown` renders a page's editor JSON by the page document format rules, the same bytes as the desktop app's renderer. Nothing calls it yet; the Factory plan step that reads pages for job briefs will.
