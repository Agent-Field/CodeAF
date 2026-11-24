---
kind: fixed
title: A held issue says why it waits, and the queue says when GitHub is not connected
pr: 2788
surface: [engine, factory, chat]
invalidates:
  - "An issue held at import and one held because Pick up issues is off both read a bare Backlog. The first now reads Held at import, the second Held: pick-up is off (a factory on your own machine only), each with what releases it, and the job carries hold: import | pick_up_off."
  - "The manual said the runner on your own machine turns on only once GitHub is connected. Since #2746 it runs without GitHub; what waits is reading issues and cloning a repository with no checkout, and GET /v1/queue now says github: not_connected so the desk can say so."
  - "make app-sidecar built a sidecar with no GitHub sign-in in silence when CODEAF_GITHUB_CLIENT_ID was unset. It now records the ID beside the binary (codeaf-<triple>.github-client-id) and warns; the desk's bundle refuses an empty record."
---

Blackmac team run, FACTORY.md "Still needs a fix" rows 2–4 (X11).
