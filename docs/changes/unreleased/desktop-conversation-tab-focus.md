---
kind: fixed
title: Desktop conversation tabs keep keyboard focus after activation
surface: [desktop, chat]
invalidates:
  - "A saved conversation's composer could take focus after strip arrow navigation selected its tab. Focus now remains on the tab so repeated arrow navigation works after attachment."
---

Composer automatic focus respects an already focused tab control. Background
world summaries still update without attaching inactive conversations. Browser
coverage checks focus after attachment and repeated navigation in both themes
at narrow, medium and wide widths.
