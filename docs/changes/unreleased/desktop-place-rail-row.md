---
kind: fixed
title: Desktop place rows share navigation geometry and keyboard focus
surface: [desktop, chat]
invalidates:
  - "Place rows were inline controls with touch-visible close buttons and a selected lift that replaced the keyboard focus ring. Now and place rows now share RailRow; touch uses the existing action menu, and selected rows keep their keyboard ring."
---

Open place rows use the shared close tooltip with the real tab count and current
place shortcut. Names fade under a mask, and the tint square, attention dot,
muted parent and closed-but-running state keep the Places 10a geometry.
