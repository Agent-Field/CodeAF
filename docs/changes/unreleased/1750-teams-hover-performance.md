---
kind: fixed
title: Keep AI team hover responsive with large conversation previews
pr: 1750
surface: [chat]
invalidates:
  - "AI team hover and spinner paints reparsed every member's Markdown response, including cards below the screen. Unchanged fitted excerpts now reuse bounded rendered rows, and offscreen member cards keep navigation targets without formatting their previews or borders."
  - "Skipping offscreen cards can leave placeholders after keyboard scrolling or a shrinking overview. The final viewport now paints revealed cards in the same frame, while streamed text, theme, size and path facts invalidate excerpt reuse."
---

Physical scrolling, member click targets, manager previews and conversation drafts
keep their existing behavior. The rendered excerpt cache is cleared on page close.
