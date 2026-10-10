---
kind: fixed
title: Desktop header attention opens this chat's first question
surface: [desktop, chat]
invalidates:
  - "The desktop needs-you header opened Tasks filtered to needs-you. It now says needs you here and opens this chat's tray on its first question."
---

Header attention remains scoped to the current session and its task questions.
The header accepts walking progress for a Next up chip beside the title, using
the measured iteration 2 geometry. The shell walker owns and supplies that progress.
